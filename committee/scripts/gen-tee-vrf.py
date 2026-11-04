#!/usr/bin/env python3
"""Generate PermissionManagerV3 submitTEESelectionVRF material."""

from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

from Crypto.Hash import keccak
from eth_account import Account
from eth_keys import keys as eth_keys_keys

try:
    from eth_abi import encode as abi_encode
except ImportError:
    from eth_abi import encode_abi as abi_encode


def keccak256(data: bytes) -> bytes:
    h = keccak.new(digest_bits=256)
    h.update(data)
    return h.digest()


VRF_DOMAIN = keccak256(b"T_GOVERN_TEE_VRF_V1")


def ensure_0x(value: str) -> str:
    return value if value.startswith(("0x", "0X")) else "0x" + value


def strip_0x(value: str) -> str:
    return value[2:] if value.startswith(("0x", "0X")) else value


def hex_bytes(name: str, value: str, length: int) -> bytes:
    raw = strip_0x(value.strip())
    if len(raw) != length * 2:
        raise ValueError(f"{name} must be {length} bytes ({length * 2} hex chars)")
    try:
        return bytes.fromhex(raw)
    except ValueError as exc:
        raise ValueError(f"{name} is not valid hex") from exc


def bytes32(name: str, value: str) -> bytes:
    return hex_bytes(name, value, 32)


def address_bytes(value: str) -> bytes:
    raw = strip_0x(value.strip())
    if len(raw) != 40:
        raise ValueError("contract address must be 20 bytes")
    try:
        return bytes.fromhex(raw)
    except ValueError as exc:
        raise ValueError("contract address is not valid hex") from exc


def is_hex_address(value: str) -> bool:
    raw = strip_0x(value.strip())
    if len(raw) != 40:
        return False
    try:
        bytes.fromhex(raw)
    except ValueError:
        return False
    return True


def to_hex(value: bytes) -> str:
    return "0x" + value.hex()


def selector(signature: str) -> bytes:
    return keccak256(signature.encode())[:4]


def calldata(signature: str, types: list[str], values: list[Any]) -> bytes:
    return selector(signature) + abi_encode(types, values)


def rpc(url: str, method: str, params: list[Any], timeout: int = 15) -> Any:
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = json.loads(resp.read().decode())
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"{method} HTTP {exc.code}: {exc.read().decode(errors='replace')}") from exc
    if payload.get("error"):
        raise RuntimeError(f"{method} RPC error: {payload['error']}")
    return payload.get("result")


def eth_call(url: str, contract: str, data: bytes) -> bytes:
    result = rpc(url, "eth_call", [{"to": ensure_0x(contract), "data": to_hex(data)}, "latest"])
    return bytes.fromhex(strip_0x(result or "0x"))


def load_material(path: str) -> dict[str, Any]:
    if not path:
        return {}
    with Path(path).open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise ValueError("material file must contain a JSON object")
    return data


def get_arg(args: argparse.Namespace, material: dict[str, Any], attr: str, key: str) -> str:
    value = getattr(args, attr)
    if value:
        return value
    loaded = material.get(key, "")
    if isinstance(loaded, str):
        return loaded
    return ""


def fetch_chain_id(args: argparse.Namespace) -> int:
    if args.chain_id:
        return int(args.chain_id, 0)
    if not args.rpc_url:
        raise ValueError("--chain-id is required when --rpc-url is not set")
    return int(rpc(args.rpc_url, "eth_chainId", []), 16)


def fetch_seed(args: argparse.Namespace, contract: str, epoch: int) -> bytes:
    if args.seed:
        return bytes32("seed", args.seed)
    if not args.rpc_url:
        raise ValueError("--seed is required when --rpc-url is not set")
    out = eth_call(args.rpc_url, contract, calldata("seedByEpoch(uint256)", ["uint256"], [epoch]))
    seed = out[-32:] if len(out) >= 32 else out.rjust(32, b"\x00")
    if seed == b"\x00" * 32:
        raise ValueError(f"seedByEpoch({epoch}) is zero; call prepareRotation first")
    return seed


def sign_raw_hash(private_key: bytes, msg_hash: bytes) -> tuple[bytes, bytes, int]:
    pk = eth_keys_keys.PrivateKey(private_key)
    sig = pk.sign_msg_hash(msg_hash)
    v = sig.v + 27
    return sig.r.to_bytes(32, "big"), sig.s.to_bytes(32, "big"), v


def build_output(args: argparse.Namespace) -> dict[str, Any]:
    material = load_material(args.material)
    contract = ensure_0x(get_arg(args, material, "contract", "contract_address"))
    if not contract or not is_hex_address(contract):
        raise ValueError("--contract must be a valid address")

    epoch = int(args.epoch, 0)
    if epoch <= 0:
        raise ValueError("--epoch must be positive")

    hardware_id = bytes32("hardware-id", get_arg(args, material, "hardware_id", "hardware_id"))
    vrf_pubkey = bytes32("vrf-pubkey", get_arg(args, material, "vrf_pubkey", "vrf_pubkey"))
    vrf_private_key = hex_bytes(
        "vrf-private-key",
        get_arg(args, material, "vrf_private_key", "vrf_private_key"),
        32,
    )
    owner = get_arg(args, material, "from_address", "owner_address")

    chain_id = fetch_chain_id(args)
    seed = fetch_seed(args, contract, epoch)
    digest = keccak256(
        abi_encode(
            ["bytes32", "uint256", "address", "uint256", "bytes32", "bytes32", "bytes32"],
            [VRF_DOMAIN, chain_id, address_bytes(contract), epoch, hardware_id, vrf_pubkey, seed],
        )
    )
    r, s, v = sign_raw_hash(vrf_private_key, digest)
    vrf_proof = r + s + bytes([v])
    vrf_result = keccak256(VRF_DOMAIN + seed + vrf_pubkey + r + s + bytes([v]))
    call_data = calldata(
        "submitTEESelectionVRF(uint256,bytes32,bytes32,bytes)",
        ["uint256", "bytes32", "bytes32", "bytes"],
        [epoch, hardware_id, vrf_result, vrf_proof],
    )

    signer = Account.from_key(vrf_private_key).address
    output: dict[str, Any] = {
        "contract_address": contract,
        "chain_id": chain_id,
        "epoch": str(epoch),
        "hardware_id": to_hex(hardware_id),
        "vrf_pubkey": to_hex(vrf_pubkey),
        "seed": to_hex(seed),
        "vrf_domain": to_hex(VRF_DOMAIN),
        "vrf_digest": to_hex(digest),
        "vrf_result": to_hex(vrf_result),
        "vrf_proof": to_hex(vrf_proof),
        "vrf_signer_address": signer,
        "submitTEESelectionVRF": {
            "epoch": str(epoch),
            "hardware_id": to_hex(hardware_id),
            "vrf_result": to_hex(vrf_result),
            "vrf_proof": to_hex(vrf_proof),
            "calldata": to_hex(call_data),
        },
        "eth_submitTEESelectionVRF": {
            "Epoch": str(epoch),
            "HardwareId": to_hex(hardware_id),
            "VrfResult": to_hex(vrf_result),
            "VrfProof": to_hex(vrf_proof),
        },
        "eth_sendTransaction": {
            "from": owner,
            "to": contract,
            "gas": hex(args.gas),
            "data": to_hex(call_data),
        },
    }
    if not owner:
        output["eth_sendTransaction"]["from"] = "<TEE owner address>"
    return output


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate VRF proof/result and submitTEESelectionVRF calldata for manual tests."
    )
    parser.add_argument("--material", default="", help="JSON from gen-tee-material.py.")
    parser.add_argument("--rpc-url", default="", help="Optional geth RPC URL used to fetch chainId/seed.")
    parser.add_argument("--contract", default="", help="PermissionManagerV3 contract address.")
    parser.add_argument("--chain-id", default="", help="Chain ID, e.g. 12345. Fetched from --rpc-url if omitted.")
    parser.add_argument("--epoch", required=True, help="Target epoch, normally currentEpoch + 1.")
    parser.add_argument("--seed", default="", help="seedByEpoch(epoch). Fetched from --rpc-url if omitted.")
    parser.add_argument("--hardware-id", default="", help="TEE hardwareId bytes32.")
    parser.add_argument("--vrf-pubkey", default="", help="TEE VRF public key bytes32.")
    parser.add_argument("--vrf-private-key", default="", help="TEE VRF private key bytes32.")
    parser.add_argument("--from-address", default="", help="TEE owner address for eth_sendTransaction template.")
    parser.add_argument("--gas", type=int, default=500000, help="Gas limit for the tx template.")
    parser.add_argument("--out", default="", help="Optional path to write JSON output.")
    args = parser.parse_args()

    try:
        output = build_output(args)
    except (ValueError, RuntimeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    text = json.dumps(output, indent=2, sort_keys=True)
    if args.out:
        Path(args.out).write_text(text + "\n", encoding="utf-8")
    print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
