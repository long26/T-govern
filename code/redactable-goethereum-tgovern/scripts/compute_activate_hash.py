#!/usr/bin/env python3
"""
Compute activateExtraTEEs messageHash = keccak256(abi.encode(bytes32[]))

Usage:
  python3 scripts/compute_activate_hash.py --ids '["0xaaa..","0xbbb.."]'
  python3 scripts/compute_activate_hash.py --file hardware_ids.json

Requires:
  pip install eth-abi eth-utils

Outputs the 32-byte message hash (hex, 0x...)
"""
import argparse
import json
import sys
from eth_abi import encode_abi
from eth_utils import keccak


def normalize_bytes32(hexstr: str) -> bytes:
    if hexstr.startswith("0x"):
        hexstr = hexstr[2:]
    if len(hexstr) > 64:
        raise ValueError(f"input too long for bytes32: {hexstr}")
    hexstr = hexstr.rjust(64, "0")
    return bytes.fromhex(hexstr)


def compute_hash(hardware_ids):
    # hardware_ids: list of hex strings
    arr = [normalize_bytes32(h) for h in hardware_ids]
    encoded = encode_abi(["bytes32[]"], [arr])
    h = keccak(encoded)
    return h


def main():
    parser = argparse.ArgumentParser(description="Compute keccak256(abi.encode(bytes32[])) for activateExtraTEEs")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--ids", help="JSON array of hardwareId hex strings, e.g. '[\"0x..\",\"0x..\"]'")
    group.add_argument("--file", help="Path to JSON file containing an array of hardwareId hex strings")
    parser.add_argument("--raw-out", help="Write raw 32 bytes to file (binary)")
    args = parser.parse_args()

    if args.ids:
        try:
            hardware_ids = json.loads(args.ids)
        except Exception as e:
            print("failed to parse --ids as JSON array", e, file=sys.stderr)
            sys.exit(2)
    else:
        with open(args.file, "r") as f:
            hardware_ids = json.load(f)

    if not isinstance(hardware_ids, list):
        print("hardware ids must be a JSON array", file=sys.stderr)
        sys.exit(2)

    try:
        message_hash = compute_hash(hardware_ids)
    except Exception as e:
        print("error computing hash:", e, file=sys.stderr)
        sys.exit(3)

    hexhash = "0x" + message_hash.hex()
    print(hexhash)

    if args.raw_out:
        with open(args.raw_out, "wb") as f:
            f.write(message_hash)


if __name__ == '__main__':
    main()
