#!/usr/bin/env python3
"""Generate PermissionManagerV3 stake/registerTEE payloads.

Current registerTEE message format:
messageHash = keccak256(
    abi.encode(owner, pendingNonce, MR_ENCLAVE, HardwareId, EnclavePubKey)
)
"""

import argparse
import hashlib
import json
import secrets
import sys
from typing import Tuple

from Crypto.Hash import keccak


def normalize_bytes32(name: str, value: str) -> bytes:
    v = value.strip()
    if v.startswith("0x") or v.startswith("0X"):
        v = v[2:]
    if len(v) != 64:
        raise ValueError(f"{name} must be 32 bytes (64 hex chars), got {len(v)}")
    try:
        return bytes.fromhex(v)
    except ValueError as exc:
        raise ValueError(f"{name} is not valid hex") from exc


def normalize_address(value: str) -> bytes:
    v = value.strip()
    if v.startswith("0x") or v.startswith("0X"):
        v = v[2:]
    if len(v) != 40:
        raise ValueError(f"Owner must be 20 bytes (40 hex chars), got {len(v)}")
    try:
        raw = bytes.fromhex(v)
    except ValueError as exc:
        raise ValueError("Owner is not valid hex") from exc
    return raw


def encode_uint256(value: int) -> bytes:
    if value < 0:
        raise ValueError("pending nonce must be non-negative")
    return value.to_bytes(32, byteorder="big")


def keccak256(data: bytes) -> bytes:
    k = keccak.new(digest_bits=256)
    k.update(data)
    return k.digest()


def generate_ids(mr: str, hardware: str, enclave: str) -> Tuple[bytes, bytes, bytes]:
    mr_enclave = normalize_bytes32("MR_ENCLAVE", mr) if mr else secrets.token_bytes(32)
    hardware_id = normalize_bytes32("HardwareId", hardware) if hardware else secrets.token_bytes(32)
    enclave_pubkey = normalize_bytes32("EnclavePubKey", enclave) if enclave else secrets.token_bytes(32)
    return mr_enclave, hardware_id, enclave_pubkey


def to_hex(b: bytes) -> str:
    return "0x" + b.hex()


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Generate registerTEE IDs and the exact message hash that must be signed "
            "for BLS aggregated signature verification."
        )
    )
    parser.add_argument("--mr-enclave", default="", help="Optional bytes32 hex for MR_ENCLAVE")
    parser.add_argument("--hardware-id", default="", help="Optional bytes32 hex for HardwareId")
    parser.add_argument("--enclave-pubkey", default="", help="Optional bytes32 hex for EnclavePubKey")
    parser.add_argument("--owner", required=True, help="TEE owner address used in stakeTEE/registerTEE")
    parser.add_argument("--pending-nonce", required=True, help="Pending nonce returned by stakeTEE/getPendingTEE")
    parser.add_argument("--value", default="1", help="Deposit value as base-10 string in stakeTEE JSON")
    parser.add_argument(
        "--json-only",
        action="store_true",
        help="Only print a JSON object with IDs and signable message fields",
    )
    args = parser.parse_args()

    try:
        mr_enclave, hardware_id, enclave_pubkey = generate_ids(
            args.mr_enclave, args.hardware_id, args.enclave_pubkey
        )
        owner = normalize_address(args.owner)
        pending_nonce = int(args.pending_nonce, 0)
        encoded_pending_nonce = encode_uint256(pending_nonce)
    except ValueError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1

    # abi.encode(address, uint256, bytes32, bytes32, bytes32)
    abi_encoded = (b"\x00" * 12) + owner + encoded_pending_nonce + mr_enclave + hardware_id + enclave_pubkey
    message_hash = keccak256(abi_encoded)

    # Contract and committee-sig both use dst="committee-sig/mvp/v1" in hashToG1:
    # sha256(message || dst) % BN256_R.
    committee_dst = b"committee-sig/mvp/v1"
    hash_to_g1_preimage = message_hash + committee_dst
    hash_to_g1_scalar_sha256 = hashlib.sha256(hash_to_g1_preimage).hexdigest()

    stake_payload = {
        "Value": args.value,
    }

    payload = {
        "Owner": "0x" + owner.hex(),
        "PendingNonce": str(pending_nonce),
        "MR_ENCLAVE": to_hex(mr_enclave),
        "HardwareId": to_hex(hardware_id),
        "EnclavePubKey": to_hex(enclave_pubkey),
        "Signature": "0x",  # Fill with aggregated BN256 G1 signature (64-byte uncompressed x||y)
    }

    sign_input = {
        "abi_encode_bytes": to_hex(abi_encoded),
        "messageHash_keccak256": to_hex(message_hash),
        "hashToG1_preimage": to_hex(hash_to_g1_preimage),
        "hashToG1_scalar_sha256_hex": "0x" + hash_to_g1_scalar_sha256,
        "committeeDst": committee_dst.decode("ascii"),
        "warning": (
            "committee-sig CLI/control path signs UTF-8 text bytes by default. "
            "For registerTEE, sign the raw 32-byte messageHash bytes, not the hex text (e.g. '0x...')."
        ),
    }

    if args.json_only:
        print(json.dumps({"stakeTEE": stake_payload, "registerTEE": payload, "signInput": sign_input}, indent=2))
        return 0

    print("# stakeTEE payload template")
    print(json.dumps(stake_payload, indent=2))
    print()
    print("# registerTEE payload template")
    print(json.dumps(payload, indent=2))
    print()
    print("# Sign this message hash for BLS aggregation")
    print(f"messageHash_keccak256: {sign_input['messageHash_keccak256']}")
    print()
    print("# Debug / cross-check fields")
    print(json.dumps(sign_input, indent=2))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
