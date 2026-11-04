#!/usr/bin/env python3
"""Generate manual-test TEE quote and VRF material.

The generated quote layout matches the fake SGX quote accepted by the
committee MVP noop/local verifier and by the real e2e scripts.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path
from typing import Any

from eth_account import Account


QUOTE_HEADER_SIZE = 48
SGX_REPORT_BODY_SIZE = 384
MR_ENCLAVE_OFFSET = 64
MR_SIGNER_OFFSET = 128
ISVSVN_OFFSET = 258
REPORT_DATA_OFFSET = 320


def ensure_0x(value: str) -> str:
    return value if value.startswith(("0x", "0X")) else "0x" + value


def strip_0x(value: str) -> str:
    return value[2:] if value.startswith(("0x", "0X")) else value


def hex_bytes(name: str, value: str, length: int) -> bytes:
    raw = value.strip()
    raw = strip_0x(raw)
    if len(raw) != length * 2:
        raise ValueError(f"{name} must be {length} bytes ({length * 2} hex chars)")
    try:
        return bytes.fromhex(raw)
    except ValueError as exc:
        raise ValueError(f"{name} is not valid hex") from exc


def bytes32(name: str, value: str) -> bytes:
    return hex_bytes(name, value, 32)


def address(value: str) -> str:
    raw = value.strip()
    if not raw:
        raise ValueError("owner address is empty")
    raw = strip_0x(raw)
    if len(raw) != 40:
        raise ValueError(f"invalid owner address: {value}")
    try:
        bytes.fromhex(raw)
    except ValueError as exc:
        raise ValueError(f"invalid owner address: {value}") from exc
    return "0x" + raw


def default_bytes32(byte_value: int) -> bytes:
    if byte_value < 0 or byte_value > 255:
        raise ValueError("default byte value is outside uint8 range")
    return bytes([byte_value]) * 32


def derive_vrf_private_key(index: int, seed: str) -> bytes:
    material = seed if seed else f"vrf-key-{index}"
    return hashlib.sha256(material.encode()).digest()


def derive_enclave_pk(index: int) -> bytes:
    return hashlib.sha512(f"enclave-pk-{index}".encode()).digest()


def vrf_pubkey_from_private(private_key: bytes) -> bytes:
    acct = Account.from_key(private_key)
    return int(acct.address, 16).to_bytes(32, "big")


def build_quote(
    mr_enclave: bytes,
    mr_signer: bytes,
    report_data_hash: bytes,
    isv_svn: int,
) -> bytes:
    if isv_svn < 0 or isv_svn > 0xFFFF:
        raise ValueError("isv-svn must fit in uint16")
    quote = bytearray(QUOTE_HEADER_SIZE + SGX_REPORT_BODY_SIZE)
    report_base = QUOTE_HEADER_SIZE
    quote[report_base + MR_ENCLAVE_OFFSET:report_base + MR_ENCLAVE_OFFSET + 32] = mr_enclave
    quote[report_base + MR_SIGNER_OFFSET:report_base + MR_SIGNER_OFFSET + 32] = mr_signer
    quote[report_base + REPORT_DATA_OFFSET:report_base + REPORT_DATA_OFFSET + 32] = report_data_hash
    quote[report_base + ISVSVN_OFFSET] = isv_svn & 0xFF
    quote[report_base + ISVSVN_OFFSET + 1] = (isv_svn >> 8) & 0xFF
    return bytes(quote)


def to_hex(value: bytes) -> str:
    return "0x" + value.hex()


def build_output(args: argparse.Namespace) -> dict[str, Any]:
    index = args.index
    if index < 0 or index > 127:
        raise ValueError("--index must be between 0 and 127 for default byte patterns")

    owner = address(args.owner) if args.owner else ""
    committee_pubkey = hex_bytes("committee-pubkey", args.committee_pubkey, 128)
    mr_enclave = bytes32("mr-enclave", args.mr_enclave) if args.mr_enclave else default_bytes32(0x40 + index)
    mr_signer = bytes32("mr-signer", args.mr_signer) if args.mr_signer else default_bytes32(0x80 + index)
    hardware_id = bytes32("hardware-id", args.hardware_id) if args.hardware_id else index.to_bytes(32, "big")
    sig_pubkey = (
        bytes32("sig-pubkey", args.sig_pubkey)
        if args.sig_pubkey
        else (int(owner, 16).to_bytes(32, "big") if owner else (1000 + index).to_bytes(32, "big"))
    )
    vrf_private_key = (
        hex_bytes("vrf-private-key", args.vrf_private_key, 32)
        if args.vrf_private_key
        else derive_vrf_private_key(index, args.vrf_seed)
    )
    vrf_pubkey = (
        bytes32("vrf-pubkey", args.vrf_pubkey)
        if args.vrf_pubkey
        else vrf_pubkey_from_private(vrf_private_key)
    )
    enclave_pk = hex_bytes("enclave-pk", args.enclave_pk, 64) if args.enclave_pk else derive_enclave_pk(index)
    if not any(enclave_pk):
        raise ValueError("enclave-pk must be non-zero")
    report_data_hash = hashlib.sha256(hardware_id + committee_pubkey + sig_pubkey + vrf_pubkey + enclave_pk).digest()
    quote = build_quote(mr_enclave, mr_signer, report_data_hash, args.isv_svn)
    report_data = {
        "hardware_id": to_hex(hardware_id),
        "committee_pub_key": to_hex(committee_pubkey),
        "sig_pub_key": to_hex(sig_pubkey),
        "vrf_pub_key": to_hex(vrf_pubkey),
        "enclave_pk": to_hex(enclave_pk),
    }

    output: dict[str, Any] = {
        "index": index,
        "owner_address": owner,
        "quote": to_hex(quote),
        "mr_enclave": to_hex(mr_enclave),
        "mr_signer": to_hex(mr_signer),
        "hardware_id": to_hex(hardware_id),
        "sig_pubkey": to_hex(sig_pubkey),
        "report_data": report_data,
        "report_data_hash": to_hex(report_data_hash),
        "isv_svn": args.isv_svn,
        "vrf_private_key": to_hex(vrf_private_key),
        "vrf_pubkey": to_hex(vrf_pubkey),
        "enclave_pk": to_hex(enclave_pk),
        "stakeTEE": {
            "Value": str(args.value),
        },
    }
    if owner:
        output["committee_registerTEE"] = {
            "owner_address": owner,
            "quote": to_hex(quote),
            "report_data": report_data,
        }
    return output


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate fake SGX quote and VRF material for manual tests.")
    parser.add_argument("--index", type=int, required=True, help="Deterministic TEE index used by the e2e scripts.")
    parser.add_argument("--owner", default="", help="Optional TEE owner address for committee_registerTEE output.")
    parser.add_argument("--value", default="1", help="stakeTEE deposit value in wei.")
    parser.add_argument("--vrf-seed", default="", help="Optional text seed for deterministic VRF private key.")
    parser.add_argument("--vrf-private-key", default="", help="Optional explicit 32-byte VRF private key hex.")
    parser.add_argument("--vrf-pubkey", default="", help="Optional explicit bytes32 VRF public key hex.")
    parser.add_argument("--enclave-pk", default="", help="Optional explicit 64-byte enclave EPK hex.")
    parser.add_argument("--mr-enclave", default="", help="Optional bytes32 MR_ENCLAVE hex.")
    parser.add_argument("--mr-signer", default="", help="Optional bytes32 MRSIGNER hex.")
    parser.add_argument("--hardware-id", default="", help="Optional bytes32 HardwareId hex.")
    parser.add_argument("--committee-pubkey", required=True, help="Committee public key as 128-byte hex.")
    parser.add_argument("--sig-pubkey", default="", help="Optional bytes32 signature public key hex.")
    parser.add_argument("--isv-svn", type=int, default=9, help="SGX ISVSVN value written into the quote.")
    parser.add_argument("--out", default="", help="Optional path to write JSON output.")
    args = parser.parse_args()

    try:
        output = build_output(args)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    text = json.dumps(output, indent=2, sort_keys=True)
    if args.out:
        Path(args.out).write_text(text + "\n", encoding="utf-8")
    print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
