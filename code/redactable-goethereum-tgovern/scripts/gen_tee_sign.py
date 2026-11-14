#!/usr/bin/env python3
"""Generate PermissionManagerV3 stake/registerTEE payloads.

Current registerTEE message format:
messageHash = keccak256(
    abi.encode(
        owner, pendingNonce, MR_ENCLAVE, HardwareId, SigPubKey,
        VrfPubKey, EnclavePK, MrSigner, QuoteHash, RegisterCertHash, ChallengeHash
    )
)
"""

import argparse
import hashlib
import json
import secrets
import sys
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


def normalize_bytes64(name: str, value: str) -> bytes:
    v = value.strip()
    if v.startswith("0x") or v.startswith("0X"):
        v = v[2:]
    if len(v) != 128:
        raise ValueError(f"{name} must be 64 bytes (128 hex chars), got {len(v)}")
    try:
        raw = bytes.fromhex(v)
    except ValueError as exc:
        raise ValueError(f"{name} is not valid hex") from exc
    if not any(raw):
        raise ValueError(f"{name} must be non-zero")
    return raw


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


def generate_ids(mr: str, hardware: str) -> tuple[bytes, bytes]:
    mr_enclave = normalize_bytes32("MR_ENCLAVE", mr) if mr else secrets.token_bytes(32)
    hardware_id = normalize_bytes32("HardwareId", hardware) if hardware else secrets.token_bytes(32)
    return mr_enclave, hardware_id


def random_address_bytes32() -> bytes:
    return (0).to_bytes(12, "big") + secrets.token_bytes(20)


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
    parser.add_argument("--mr-signer", default="", help="Optional bytes32 hex for MRSIGNER")
    parser.add_argument("--hardware-id", default="", help="Optional bytes32 hex for HardwareId")
    parser.add_argument("--sig-pubkey", default="", help="Optional bytes32 hex for SigPubKey")
    parser.add_argument("--vrf-pubkey", default="", help="Optional bytes32 hex for VrfPubKey used in registerTEE evidence")
    parser.add_argument("--enclave-pk", default="", help="Optional 64-byte hex EnclavePK used in registerTEE evidence")
    parser.add_argument("--quote-hash", default="", help="Optional bytes32 hex quote hash")
    parser.add_argument("--register-cert-hash", default="", help="Optional bytes32 hex registration certificate hash")
    parser.add_argument("--challenge-hash", default="", help="Optional bytes32 hex challenge hash from getPendingTEE")
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
        mr_enclave, hardware_id = generate_ids(args.mr_enclave, args.hardware_id)
        mr_signer = normalize_bytes32("MrSigner", args.mr_signer) if args.mr_signer else secrets.token_bytes(32)
        sig_pubkey = normalize_bytes32("SigPubKey", args.sig_pubkey) if args.sig_pubkey else random_address_bytes32()
        vrf_pubkey = normalize_bytes32("VrfPubKey", args.vrf_pubkey) if args.vrf_pubkey else random_address_bytes32()
        enclave_pk = normalize_bytes64("EnclavePK", args.enclave_pk) if args.enclave_pk else secrets.token_bytes(64)
        quote_hash = normalize_bytes32("QuoteHash", args.quote_hash) if args.quote_hash else secrets.token_bytes(32)
        register_cert_hash = normalize_bytes32("RegisterCertHash", args.register_cert_hash) if args.register_cert_hash else secrets.token_bytes(32)
        challenge_hash = normalize_bytes32("ChallengeHash", args.challenge_hash) if args.challenge_hash else secrets.token_bytes(32)
        owner = normalize_address(args.owner)
        pending_nonce = int(args.pending_nonce, 0)
        encoded_pending_nonce = encode_uint256(pending_nonce)
    except ValueError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1

    # abi.encode(address, uint256, bytes32, bytes32, bytes32, bytes32, bytes32[2], bytes32 x 4)
    abi_encoded = (
        (b"\x00" * 12)
        + owner
        + encoded_pending_nonce
        + mr_enclave
        + hardware_id
        + sig_pubkey
        + vrf_pubkey
        + enclave_pk
        + mr_signer
        + quote_hash
        + register_cert_hash
        + challenge_hash
    )
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
        "SigPubKey": to_hex(sig_pubkey),
        "VrfPubKey": to_hex(vrf_pubkey),
        "EnclavePK": to_hex(enclave_pk),
        "MrSigner": to_hex(mr_signer),
        "QuoteHash": to_hex(quote_hash),
        "RegisterCertHash": to_hex(register_cert_hash),
        "ChallengeHash": to_hex(challenge_hash),
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
