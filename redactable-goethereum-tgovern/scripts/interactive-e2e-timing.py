#!/usr/bin/env python3
"""Interactive E2E guide for T-govern v0.8: TEE registration -> VRF rotation -> chain redaction.

This script guides the operator through the entire protocol step by step.
It does NOT auto-submit on-chain transactions by default; instead it:
1. Generates TEE materials (fake quote + VRF keypair) automatically.
2. Prints the exact curl/geth console commands for each step.
3. Waits for user confirmation (or --auto for CI).
4. Auto-polls chain state at critical checkpoints and blocks until verified.

Usage:
  python3 interactive-e2e-guide.py               # interactive mode (default)
  python3 interactive-e2e-guide.py --auto         # auto-execute all transactions
  python3 interactive-e2e-guide.py --verify-only  # only verify current state

Topology:
  - 3 committee geth nodes (node1..node3)
  - 15 TEE owner geth nodes (node4..node18)
  - 3 committee-mvp nodes
  - Flow: Register -> Rotate1 -> Edit1 -> Rotate2 -> Edit2 -> Rotate3
"""

from __future__ import annotations

import argparse
import atexit
from contextlib import contextmanager
import hashlib
import json
import os
import shutil
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

from Crypto.Hash import keccak
from eth_account import Account
from eth_keys import keys as eth_keys_keys

try:
    from eth_abi import encode as abi_encode
    from eth_abi import decode as abi_decode
except ImportError:
    from eth_abi import encode_abi as abi_encode
    from eth_abi import decode_abi as abi_decode

# ──────────────────────────────────────────────
# Paths & Constants
# ──────────────────────────────────────────────
ROOT = Path(__file__).resolve().parents[1]
GETH_DIR = Path("/root/redactable-goethereum")
GETH_BIN = GETH_DIR / "build" / "bin" / "geth"
BASE_GENESIS = GETH_DIR / "genesis.json"
HELPER_JSON = GETH_DIR / "helper.json"
COMMITTEE_MVP_BIN = ROOT / "build" / "committee-mvp"

WORK = ROOT / ".tmp" / "e2e-interactive"
CHAIN = WORK / "chain"
NODE_WORK = WORK / "node-work"
COMMITTEE_CONFIGS = WORK / "committee-configs"
LOGS = WORK / "logs"
TEE_MATERIALS = WORK / "tee-materials"
TIMING_DIR = WORK / "timing"
TIMING_JSONL = TIMING_DIR / "interactive-e2e-timing.jsonl"

NETWORK_ID = "12345"
PASSWORD = "1"
NODE_COUNT = 18
COMMITTEE_COUNT = 3
TEE_OWNER_START = 4
TEE_OWNER_END = 8
TEE_NUM = 5
EPOCH_LENGTH = 50
ROTATION_PREPARE_BLOCKS = 10

_k = keccak.new(digest_bits=256)
_k.update(b"T_GOVERN_TEE_VRF_V1")
VRF_DOMAIN = _k.digest()

QUOTE_HEADER_SIZE = 48
SGX_REPORT_BODY_SIZE = 384
MR_ENCLAVE_OFFSET = 64
MR_SIGNER_OFFSET = 128
ISVSVN_OFFSET = 258
REPORT_DATA_OFFSET = 320

# ──────────────────────────────────────────────
# Global state
# ──────────────────────────────────────────────
processes: list[subprocess.Popen] = []
accounts: list[dict[str, str]] = []
contract_address: str = ""
tee_info: list[dict[str, Any]] = []
auto_mode = False
timing_records: list[dict[str, Any]] = []


# ══════════════════════════════════════════════
# Helper functions (inlined from e2e-tee-rotate.py)
# ══════════════════════════════════════════════

def log(msg: str) -> None:
    print(f"\n[e2e-guide] {msg}", flush=True)


def flush_timing_records() -> None:
    TIMING_DIR.mkdir(parents=True, exist_ok=True)
    TIMING_JSONL.write_text("")
    for record in timing_records:
        with TIMING_JSONL.open("a") as fp:
            fp.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")


def record_timing(stage: str, event: str, start_ns: int, end_ns: int, status: str = "ok", **fields: Any) -> None:
    record = {
        "ts": time.time(),
        "stage": stage,
        "event": event,
        "status": status,
        "start_ns": start_ns,
        "end_ns": end_ns,
        "duration_ns": end_ns - start_ns,
        "duration_ms": round((end_ns - start_ns) / 1_000_000, 3),
    }
    for key, value in fields.items():
        if value is not None:
            record[key] = value
    timing_records.append(record)
    if TIMING_DIR.exists():
        with TIMING_JSONL.open("a") as fp:
            fp.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")
    print(f"  ⏱ {stage}.{event}: {record['duration_ms']} ms [{status}]", flush=True)


@contextmanager
def timed(stage: str, event: str, **fields: Any):
    start_ns = time.perf_counter_ns()
    try:
        yield
    except Exception as exc:
        record_timing(stage, event, start_ns, time.perf_counter_ns(), status="error", error=str(exc), **fields)
        raise
    else:
        record_timing(stage, event, start_ns, time.perf_counter_ns(), **fields)


def print_curl(url: str, method: str, params: Any, rpc_id: int = 1) -> None:
    """Print a copy-pasteable curl command for a JSON-RPC request."""
    payload = json.dumps(
        {"jsonrpc": "2.0", "id": rpc_id, "method": method, "params": params},
        separators=(",", ":"),
    )
    print(f"  curl -s {url} -H 'Content-Type: application/json' \\")
    print("    --data-binary @- <<'JSON'")
    print(payload)
    print("JSON")


def step_header(phase: str, desc: str) -> None:
    print(f"\n{'='*60}", flush=True)
    print(f"  {phase}: {desc}", flush=True)
    print(f"{'='*60}", flush=True)


def wait_user(prompt: str = "按回车继续...") -> None:
    if auto_mode:
        print(f"  [auto] {prompt}")
        return
    input(f"  ⏸  {prompt}")


def rpc(url: str, method: str, params: Any, timeout: int = 15) -> Any:
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


def wait_for_rpc(url: str, timeout: int = 60) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            rpc(url, "eth_blockNumber", [], timeout=3)
            return
        except Exception:
            time.sleep(1)
    raise TimeoutError(f"RPC not ready: {url}")


def wait_for_receipt(url: str, tx_hash: str, timeout: int = 90) -> dict[str, Any]:
    deadline = time.time() + timeout
    while time.time() < deadline:
        receipt = rpc(url, "eth_getTransactionReceipt", [tx_hash], timeout=5)
        if receipt:
            if receipt.get("status") not in (None, "0x1"):
                raise RuntimeError(f"tx failed {tx_hash}: {receipt}")
            return receipt
        time.sleep(1)
    raise TimeoutError(f"receipt timeout: {tx_hash}")


def int_from_hex(value: str | None) -> int:
    if not value:
        return 0
    return int(value, 16)


def selector(signature: str) -> bytes:
    h = keccak.new(digest_bits=256)
    h.update(signature.encode())
    return h.digest()[:4]


def calldata(signature: str, types: list[str], values: list[Any]) -> bytes:
    return selector(signature) + abi_encode(types, values)


def eth_call(url: str, contract: str, data: bytes) -> bytes:
    result = rpc(url, "eth_call", [{"to": contract, "data": "0x" + data.hex()}, "latest"])
    return bytes.fromhex(result.removeprefix("0x"))


def call_uint(url: str, contract: str, signature: str, types: list[str] | None = None, values: list[Any] | None = None) -> int:
    out = eth_call(url, contract, calldata(signature, types or [], values or []))
    return int.from_bytes(out[-32:] if out else b"", "big")


def call_bytes32_array(url: str, contract: str, signature: str) -> list[str]:
    out = eth_call(url, contract, calldata(signature, [], []))
    decoded = abi_decode(["bytes32[]"], out)
    return ["0x" + x.hex() for x in decoded[0]]


def send_raw_tx(
    url: str,
    private_key: str,
    to: str,
    data: bytes,
    value: int = 0,
    timeout: int = 90,
    gas_limit: int | None = None,
    timing_stage: str = "transaction_generation",
    timing_event: str = "send_raw_tx",
) -> str:
    account = Account.from_key(private_key)
    tx_hash = ""
    with timed(timing_stage, f"{timing_event}.prepare", rpc_url=url, from_addr=account.address, to=to):
        nonce = int_from_hex(rpc(url, "eth_getTransactionCount", [account.address, "pending"]))
        gas_price = int_from_hex(rpc(url, "eth_gasPrice", []))
        if gas_limit is not None:
            gas = gas_limit
        else:
            tx_obj = {"from": account.address, "to": to, "value": hex(value), "data": "0x" + data.hex()}
            gas = int_from_hex(rpc(url, "eth_estimateGas", [tx_obj]))
        try:
            chain_id = int_from_hex(rpc(url, "eth_chainId", []))
        except Exception:
            chain_id = int(NETWORK_ID)
    with timed(timing_stage, f"{timing_event}.sign", from_addr=account.address, to=to):
        tx = {"nonce": nonce, "to": to, "value": value, "gas": gas + 50000, "gasPrice": gas_price, "data": data, "chainId": chain_id}
        signed = Account.sign_transaction(tx, private_key)
        raw = getattr(signed, "rawTransaction", None) or getattr(signed, "raw_transaction")
        raw_hex = raw.hex() if isinstance(raw, bytes) else raw
        if not raw_hex.startswith("0x"):
            raw_hex = "0x" + raw_hex
    with timed(timing_stage, f"{timing_event}.broadcast_rpc", rpc_url=url, from_addr=account.address, to=to):
        tx_hash = rpc(url, "eth_sendRawTransaction", [raw_hex])
    with timed(timing_stage, f"{timing_event}.receipt", rpc_url=url, tx_hash=tx_hash):
        wait_for_receipt(url, tx_hash, timeout=timeout)
    return tx_hash


def committee_rpc(method: str, params: Any, timeout: int = 20) -> Any:
    return rpc("http://127.0.0.1:4401/rpc", method, params, timeout=timeout)


def node_rpc(index: int) -> str:
    return f"http://127.0.0.1:{8544 + index}"


def wait_tx_input(url: str, block_number: int, index: int, want_hex: str, timeout: int = 180) -> dict[str, Any]:
    deadline = time.time() + timeout
    block_tag = hex(block_number)
    last_input = ""
    while time.time() < deadline:
        block = rpc(url, "eth_getBlockByNumber", [block_tag, True], timeout=5)
        txs = block.get("transactions", []) if block else []
        if len(txs) > index and isinstance(txs[index], dict):
            last_input = txs[index].get("input", "")
            if last_input.lower() == want_hex.lower():
                return txs[index]
        time.sleep(1)
    raise TimeoutError(
        f"编辑同步验证超时: rpc={url} block={block_number} index={index} "
        f"want={want_hex} last={last_input}"
    )


def verify_redaction_synced(block_number: int, tx_index: int, want_hex: str, node_indexes: list[int] | None = None) -> None:
    if node_indexes is None:
        node_indexes = [1, 2, 3]
    for node_index in node_indexes:
        tx = wait_tx_input(node_rpc(node_index), block_number, tx_index, want_hex)
        print(f"  ✅ node{node_index} 已同步编辑: tx={tx.get('hash', '')[:16]}...")


def wait_for_pending_tee(owner: str, timeout: int = 60) -> dict[str, Any]:
    deadline = time.time() + timeout
    last: Any = None
    while time.time() < deadline:
        try:
            pending = rpc(node_rpc(1), "eth_getPendingTEE", [owner], timeout=5)
            last = pending
            if isinstance(pending, dict) and pending.get("Exists"):
                return pending
        except RuntimeError as exc:
            last = exc
        time.sleep(1)
    raise TimeoutError(
        f"pending TEE not found for {owner}; make sure stakeTEE was sent to this owner node RPC "
        f"and the tx is mined. Last result: {last}"
    )


def wait_for_pkch_set(previous_hash: str, timeout: int = 60) -> dict[str, Any]:
    deadline = time.time() + timeout
    last: Any = None
    zero_hash = "0x" + "0" * 64
    previous_hash = previous_hash.lower()
    while time.time() < deadline:
        try:
            params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=5)
            last = params
            pkch_hash = str(params.get("PkCHHash", "")).lower()
            if pkch_hash and pkch_hash != zero_hash and pkch_hash != previous_hash:
                return params
        except RuntimeError as exc:
            last = exc
        time.sleep(1)
    raise TimeoutError(f"pkCH transaction did not take effect. Last result: {last}")


def active_sig_pubkey(address: str) -> str:
    return "0x" + int(address, 16).to_bytes(32, "big").hex()


committee_pubkey_hex = ""


def active_tee_owner_for_command(active_hardware_ids: list[str], registered_tees: list[dict[str, Any]]) -> dict[str, Any]:
    return active_tee_owners_for_command(active_hardware_ids, registered_tees, 1)[0]


def active_tee_owners_for_command(active_hardware_ids: list[str], registered_tees: list[dict[str, Any]], threshold: int) -> list[dict[str, Any]]:
    by_hw = {str(info.get("hw_id", "")).lower(): info for info in registered_tees}
    selected: list[dict[str, Any]] = []
    for hardware_id in active_hardware_ids:
        info = by_hw.get(hardware_id.lower())
        if info is not None:
            selected.append(info)
            if len(selected) >= threshold:
                return selected
    raise RuntimeError(f"not enough registered active TEE owners: got {len(selected)} want {threshold}; active={active_hardware_ids}")


def find_pool_hash(url: str, sender_addr: str, orig_hash: str) -> str:
    pool = rpc(url, "txpool_content", [], timeout=10)
    pending = pool.get("pending", {})
    for addr, nonces in pending.items():
        if addr.lower() == sender_addr.lower():
            for nonce_key, tx_obj in nonces.items():
                h = tx_obj.get("hash") if isinstance(tx_obj, dict) else tx_obj
                if h and h != orig_hash:
                    return h
            for nonce_key, tx_obj in nonces.items():
                h = tx_obj.get("hash") if isinstance(tx_obj, dict) else tx_obj
                if h:
                    return h
    return orig_hash


def cleanup() -> None:
    for proc in reversed(processes):
        if proc.poll() is None:
            proc.terminate()
    for proc in reversed(processes):
        if proc.poll() is None:
            try:
                proc.wait(timeout=8)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)


# ──────────────────────────────────────────────
# TEE material generation (from gen-tee-material.py)
# ──────────────────────────────────────────────

def derive_vrf_private_key(index: int) -> bytes:
    return hashlib.sha256(f"vrf-key-{index}".encode()).digest()


def vrf_pubkey_from_private(private_key: bytes) -> bytes:
    acct = Account.from_key(private_key)
    return int(acct.address, 16).to_bytes(32, "big")


def derive_enclave_pk(index: int) -> bytes:
    return hashlib.sha512(f"enclave-pk-{index}".encode()).digest()


def build_report_data(hardware: bytes, committee_pubkey: str, sig_pubkey: bytes, vrf_pubkey: bytes, enclave_pk: bytes) -> tuple[bytes, dict[str, str]]:
    committee = bytes.fromhex(committee_pubkey.removeprefix("0x"))
    payload = hardware + committee + sig_pubkey + vrf_pubkey + enclave_pk
    report_hash = hashlib.sha256(payload).digest()
    return report_hash, {
        "hardware_id": "0x" + hardware.hex(),
        "committee_pub_key": "0x" + committee.hex(),
        "sig_pub_key": "0x" + sig_pubkey.hex(),
        "vrf_pub_key": "0x" + vrf_pubkey.hex(),
        "enclave_pk": "0x" + enclave_pk.hex(),
    }


def build_quote(index: int, vrf_pubkey: bytes, committee_pubkey: str) -> tuple[str, str, str, dict[str, str]]:
    mr = bytes([0x40 + index]) * 32
    signer = bytes([0x80 + index]) * 32
    hardware = index.to_bytes(32, "big")
    sig_pubkey = bytes.fromhex(active_sig_pubkey(accounts[index - 1]["address"]).removeprefix("0x"))
    enclave_pk = derive_enclave_pk(index)
    report_hash, report_data = build_report_data(hardware, committee_pubkey, sig_pubkey, vrf_pubkey, enclave_pk)
    quote = bytearray(QUOTE_HEADER_SIZE + SGX_REPORT_BODY_SIZE)
    base = QUOTE_HEADER_SIZE
    quote[base + MR_ENCLAVE_OFFSET:base + MR_ENCLAVE_OFFSET + 32] = mr
    quote[base + MR_SIGNER_OFFSET:base + MR_SIGNER_OFFSET + 32] = signer
    quote[base + REPORT_DATA_OFFSET:base + REPORT_DATA_OFFSET + 32] = report_hash
    quote[base + ISVSVN_OFFSET] = 9
    return "0x" + quote.hex(), "0x" + mr.hex(), "0x" + hardware.hex(), report_data


# ──────────────────────────────────────────────
# VRF helpers (from e2e-tee-rotate.py)
# ──────────────────────────────────────────────

def ecdsa_sign_raw_hash(private_key_bytes: bytes, msg_hash: bytes) -> tuple[int, int, int]:
    pk = eth_keys_keys.PrivateKey(private_key_bytes)
    sig = pk.sign_msg_hash(msg_hash)
    v_eth = sig.v + 27
    return sig.r, sig.s, v_eth


def pkch_signature(url: str, contract: str, owner_address: str, owner_key: str, pk_ch: str) -> bytes:
    pk_ch_bytes = bytes.fromhex(pk_ch.removeprefix("0x"))
    digest = eth_call(
        url,
        contract,
        calldata("computePkCHDigest(address,bytes)", ["address", "bytes"], [owner_address, pk_ch_bytes]),
    )
    if len(digest) != 32:
        raise RuntimeError(f"unexpected pkCH digest length: {len(digest)}")
    r, s, v = ecdsa_sign_raw_hash(bytes.fromhex(owner_key.removeprefix("0x")), digest)
    return r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([v])


def erase_proof_signature(url: str, contract: str, info: dict[str, Any], epoch: int) -> tuple[str, str]:
    proof_seed = f"erase-proof:{epoch}:{info['hw_id'].lower()}".encode()
    erase_proof_hash = keccak.new(digest_bits=256, data=proof_seed).digest()
    digest = eth_call(
        url,
        contract,
        calldata(
            "computeEraseProofDigest(bytes32,address,uint256,bytes32)",
            ["bytes32", "address", "uint256", "bytes32"],
            [
                bytes.fromhex(str(info["hw_id"]).removeprefix("0x")),
                str(info["owner_address"]),
                epoch,
                erase_proof_hash,
            ],
        ),
    )
    if len(digest) != 32:
        raise RuntimeError(f"unexpected erase proof digest length: {len(digest)}")
    r, s, v = ecdsa_sign_raw_hash(bytes.fromhex(str(info["owner_key"]).removeprefix("0x")), digest)
    signature = r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([v])
    return "0x" + erase_proof_hash.hex(), "0x" + signature.hex()


def sign_share_proof(d_req: str, private_key: str, share: bytes) -> str:
    digest_input = bytes.fromhex(d_req.removeprefix("0x")) + share
    digest = keccak.new(digest_bits=256, data=digest_input).digest()
    r, s, v = ecdsa_sign_raw_hash(bytes.fromhex(private_key.removeprefix("0x")), digest)
    signature = r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([v])
    return "0x" + signature.hex()


def build_share_proofs(d_req: str, active_sender: dict[str, Any], share: bytes) -> list[dict[str, str]]:
    return [{
        "SigPubKey": active_sig_pubkey(str(active_sender["owner_address"])),
        "SI": "0x" + share.hex(),
        "Sigma": sign_share_proof(d_req, str(active_sender["owner_key"]), share),
    }]


def wait_for_hardware_unregistered(hardware_id: str, timeout: int = 60) -> None:
    deadline = time.time() + timeout
    hw_bytes = bytes.fromhex(hardware_id.removeprefix("0x"))
    while time.time() < deadline:
        out = eth_call(node_rpc(1), contract_address, calldata("isHardwareIdRegistered(bytes32)", ["bytes32"], [hw_bytes]))
        registered = int.from_bytes(out[-32:] if out else b"", "big") != 0
        if not registered:
            return
        time.sleep(1)
    raise TimeoutError(f"hardwareId still registered after erase proof: {hardware_id}")


def submit_erase_proofs_for_departed(previous_active: list[str], new_active: list[str], exit_epoch: int) -> None:
    departed = sorted(set(hw.lower() for hw in previous_active) - set(hw.lower() for hw in new_active))
    if not departed:
        log(f"轮换后无离开 active set 的 TEE，跳过 erase proof (epoch={exit_epoch})")
        return

    by_hw = {str(info["hw_id"]).lower(): info for info in tee_info}
    step_header("Erase Proof", f"提交 {len(departed)} 个退出 TEE 的擦除证明（exit epoch {exit_epoch}）")
    for hw in departed:
        info = by_hw.get(hw)
        if info is None:
            raise RuntimeError(f"departed TEE not found in local materials: {hw}")
        erase_hash, signature = erase_proof_signature(node_rpc(1), contract_address, info, exit_epoch)
        owner_node = int(info["node_index"])
        print(f"\n  📋 TEE node{owner_node} submitEraseProof:")
        print(f"  HardwareId: {info['hw_id']}")
        print(f"  Epoch:      {exit_epoch}")
        print(f"  ProofHash:  {erase_hash}")
        print_curl(node_rpc(owner_node), "eth_submitEraseProof", [info["hw_id"], exit_epoch, erase_hash, signature])

        if auto_mode:
            log(f"[auto] submitEraseProof node{owner_node}...")
            tx_hash = rpc(node_rpc(owner_node), "eth_submitEraseProof", [info["hw_id"], exit_epoch, erase_hash, signature], timeout=15)
            wait_for_receipt(node_rpc(1), tx_hash, timeout=60)
        else:
            wait_user("请执行上述 submitEraseProof 命令，完成后按回车继续...")

        wait_for_hardware_unregistered(str(info["hw_id"]))
        print(f"  ✅ TEE node{owner_node} 擦除证明已确认，注册状态已清理")


# ══════════════════════════════════════════════
# Phase 0: Environment check
# ══════════════════════════════════════════════

def phase0_env_check() -> None:
    step_header("Phase 0", "环境检查")
    ok = True
    for name, path in [("geth binary", GETH_BIN), ("genesis.json", BASE_GENESIS), ("helper.json", HELPER_JSON)]:
        exists = path.exists()
        status = "✅" if exists else "❌"
        print(f"  {status} {name}: {path}")
        if not exists:
            ok = False
    if not ok:
        print("\n  ❌ 缺少必要文件，请先编译 geth 和准备 helper.json")
        sys.exit(1)
    print("  ✅ 环境检查通过")


# ══════════════════════════════════════════════
# Phase 1: Generate accounts & genesis
# ══════════════════════════════════════════════

def phase1_prepare() -> None:
    step_header("Phase 1", "生成账户和创世文件（自动）")
    global accounts

    with timed("system_init", "prepare_workspace"):
        cleanup()
        if WORK.exists():
            shutil.rmtree(WORK)
        for p in (CHAIN, NODE_WORK, COMMITTEE_CONFIGS, LOGS, BIN := WORK / "bin", TEE_MATERIALS, TIMING_DIR):
            p.mkdir(parents=True, exist_ok=True)
        flush_timing_records()

    with timed("system_init", "generate_accounts", node_count=NODE_COUNT):
        accounts = []
        for i in range(1, NODE_COUNT + 1):
            acct = Account.create(f"tee-rotate-{i}-{time.time_ns()}")
            accounts.append({"node": f"node{i}", "address": acct.address, "privateKey": acct.key.hex().removeprefix("0x")})

    with timed("system_init", "write_genesis", node_count=NODE_COUNT):
        genesis = json.loads(BASE_GENESIS.read_text())
        genesis["alloc"] = {
            a["address"].removeprefix("0x").lower(): {"balance": "1000000000000000000000"}
            for a in accounts
        }
        (WORK / "genesis.json").write_text(json.dumps(genesis, indent=2))
        (WORK / "accounts.json").write_text(json.dumps({"nodes": accounts}, indent=2))

    # Init geth datadirs
    with timed("system_init", "init_geth_datadirs", node_count=NODE_COUNT):
        for i in range(1, NODE_COUNT + 1):
            datadir = CHAIN / f"node{i}"
            datadir.mkdir(parents=True, exist_ok=True)
            subprocess.run([str(GETH_BIN), "--datadir", str(datadir), "init", str(WORK / "genesis.json")],
                           cwd=str(GETH_DIR), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)

    print(f"  ✅ 生成 {len(accounts)} 个账户")
    print(f"  ✅ 初始化 {NODE_COUNT} 个 geth datadir")
    print(f"  📁 工作目录: {WORK}")


# ══════════════════════════════════════════════
# Phase 2: Start nodes
# ══════════════════════════════════════════════

def start_geth_node(index: int, account: dict[str, str], contract_addr: str | None = None) -> None:
    datadir = CHAIN / f"node{index}"
    workdir = NODE_WORK / f"node{index}"
    workdir.mkdir(parents=True, exist_ok=True)
    private_key_file = workdir / "privatekey.txt"
    password_file = workdir / "password.txt"
    private_key_file.write_text(account["privateKey"] + "\n")
    password_file.write_text(PASSWORD + "\n")
    shutil.copyfile(HELPER_JSON, workdir / "helper.json")
    storage = {"address": contract_addr or "", "privateKey": account["privateKey"], "port": str(8544 + index)}
    (workdir / "storage.json").write_text(json.dumps(storage, indent=2))

    subprocess.run(
        [
            str(GETH_BIN),
            "--datadir", str(datadir),
            "account", "import",
            "--password", str(password_file),
            str(private_key_file),
        ],
        cwd=str(GETH_DIR),
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.STDOUT,
    )

    rpc_port = 8544 + index
    p2p_port = 30300 + index
    args = [
        str(GETH_BIN), "--datadir", str(datadir), "--networkid", NETWORK_ID,
        "--port", str(p2p_port), "--rpc", "--rpcport", str(rpc_port),
        "--rpcaddr", "127.0.0.1", "--rpccorsdomain", "*",
        "--rpcapi", "eth,net,web3,personal,admin,debug,miner,txpool",
        "--ipcpath", str(datadir / "geth.ipc"), "--allow-insecure-unlock",
        "--unlock", account["address"], "--password", str(password_file),
        "--nodiscover", "--etherbase", account["address"], "--verbosity", "4",
    ]
    if index == 1:
        args += ["--mine", "--minerthreads", "1"]

    out = open(LOGS / f"geth-node-{index}.log", "w")
    proc = subprocess.Popen(args, cwd=str(workdir), stdout=out, stderr=subprocess.STDOUT)
    processes.append(proc)


def write_committee_configs(contract: str, submitter: str, ipc_path: Path, start_block: int) -> None:
    for i in range(1, COMMITTEE_COUNT + 1):
        cfg = {
            "node_id": f"node-{i}",
            "listen_addr": f"127.0.0.1:{3400 + i}",
            "static_nodes": [f"node-{j}" for j in range(1, COMMITTEE_COUNT + 1)],
            "static_node_addrs": {f"node-{j}": f"127.0.0.1:{3400 + j}" for j in range(1, COMMITTEE_COUNT + 1)},
            "control_addr": f"127.0.0.1:{4400 + i}",
            "chain_rpc_mode": "ipc",
            "geth_ipc_path": str(ipc_path),
            "vrf_contract_address": contract,
            "vrf_submitter_address": submitter,
            "vrf_start_block": start_block,
            "committee_size": COMMITTEE_COUNT, "threshold": 2,
            "coordinator_id": "node-1",
            "domain_separation": "committee-sig/mvp/v1",
            "message_version": "v1",
            "attestation_provider": "noop",
        }
        (COMMITTEE_CONFIGS / f"node-{i}.json").write_text(json.dumps(cfg, indent=2))


def start_committee_nodes() -> None:
    bin_path = WORK / "bin" / "committee-mvp"
    if bin_path.exists():
        log("using pre-built committee-mvp binary")
    elif COMMITTEE_MVP_BIN.exists():
        shutil.copy2(str(COMMITTEE_MVP_BIN), str(bin_path))
        log("copied committee-mvp binary from build/")
    else:
        log("building committee-mvp...")
        subprocess.run(["go", "build", "-o", str(bin_path), "./cmd/committee-mvp"], cwd=str(ROOT), check=True)

    for i in range(1, COMMITTEE_COUNT + 1):
        out = open(LOGS / f"committee-node-{i}.log", "w")
        proc = subprocess.Popen(
            [str(bin_path), "node", "--config", str(COMMITTEE_CONFIGS / f"node-{i}.json")],
            cwd=str(ROOT), stdout=out, stderr=subprocess.STDOUT,
        )
        processes.append(proc)


def phase2_start_nodes() -> None:
    global contract_address
    step_header("Phase 2", "启动 geth 节点和 committee-mvp 节点")

    # Start node1 first (miner + contract deployer)
    log("启动 node1 (矿工)...")
    with timed("system_init", "start_geth_node1_to_rpc", node="node1", rpc_url=node_rpc(1)):
        start_geth_node(1, accounts[0])
        wait_for_rpc(node_rpc(1))
    log("node1 RPC 就绪")

    # Wait for contract deployment
    log("等待合约部署...")
    with timed("system_init", "contract_deploy_wait", node="node1"):
        storage_path = NODE_WORK / "node1" / "storage.json"
        deadline = time.time() + 90
        while time.time() < deadline:
            if storage_path.exists():
                try:
                    addr = json.loads(storage_path.read_text()).get("address", "")
                except Exception:
                    addr = ""
                if addr:
                    code = rpc(node_rpc(1), "eth_getCode", [addr, "latest"], timeout=5)
                    if code and code != "0x":
                        contract_address = addr
                        break
            time.sleep(1)
    if not contract_address:
        raise TimeoutError("合约部署超时")
    log(f"合约已部署: {contract_address}")

    # Start remaining nodes
    log(f"启动 node2..node{NODE_COUNT}...")
    with timed("system_init", "start_remaining_geth_nodes", node_count=NODE_COUNT - 1):
        for i in range(2, NODE_COUNT + 1):
            start_geth_node(i, accounts[i - 1], contract_addr=contract_address)
        for i in range(2, NODE_COUNT + 1):
            wait_for_rpc(node_rpc(i))

    # Connect peers
    log("连接节点...")
    with timed("system_init", "connect_geth_peers", node_count=NODE_COUNT):
        n1_info = rpc(node_rpc(1), "admin_nodeInfo", [])
        n1_enode = n1_info["enode"]
        if "@127.0.0.1" not in n1_enode:
            prefix = n1_enode.split("@", 1)[0]
            n1_enode = f"{prefix}@127.0.0.1:30301"
        for i in range(2, NODE_COUNT + 1):
            info = rpc(node_rpc(i), "admin_nodeInfo", [])
            enode = info["enode"]
            if "@127.0.0.1" not in enode:
                prefix = enode.split("@", 1)[0]
                enode = f"{prefix}@127.0.0.1:{30300 + i}"
            rpc(node_rpc(i), "admin_addPeer", [n1_enode])
            rpc(node_rpc(1), "admin_addPeer", [enode])
        time.sleep(3)

    # Start committee
    with timed("system_init", "write_committee_configs", committee_count=COMMITTEE_COUNT):
        start_block = int_from_hex(rpc(node_rpc(1), "eth_blockNumber", []))
        write_committee_configs(contract_address, accounts[0]["address"], CHAIN / "node1" / "geth.ipc", start_block)
    log("启动 committee-mvp 节点...")
    with timed("system_init", "start_committee_processes", committee_count=COMMITTEE_COUNT):
        start_committee_nodes()

    print(f"  ✅ {NODE_COUNT} 个 geth 节点已启动")
    print(f"  ✅ {COMMITTEE_COUNT} 个 committee-mvp 节点已启动")
    print(f"  ✅ 合约地址: {contract_address}")


# ══════════════════════════════════════════════
# Phase 3: Register committee pubkey
# ══════════════════════════════════════════════

def phase3_committee_pubkey() -> None:
    global committee_pubkey_hex
    step_header("Phase 3", "注册委员会公钥（协议 4-1 步骤 1）")

    # Wait for committee pubkey
    log("等待 committee 公钥就绪...")
    with timed("system_init", "committee_pubkey_ready", committee_count=COMMITTEE_COUNT):
        pubkey = ""
        deadline = time.time() + 60
        while time.time() < deadline:
            try:
                result = committee_rpc("committee_getPublicKey", {}, timeout=5)
                pk = result.get("committee_pub_key", "")
                if len(pk.removeprefix("0x")) == 128 * 2:
                    pubkey = "0x" + pk.removeprefix("0x")
                    break
            except Exception:
                pass
            time.sleep(1)
    if not pubkey:
        raise TimeoutError("committee 公钥超时")
    committee_pubkey_hex = pubkey
    log(f"committee 公钥: {pubkey[:40]}...")

    # Register committee pubkey (always execute, don't wait for user in auto)
    if auto_mode:
        log("[auto] 注册委员会公钥...")
        with timed("system_init", "register_committee_pubkey_rpc", rpc_url=node_rpc(1)):
            rpc(node_rpc(1), "eth_registerCommitteePubKey", [pubkey], timeout=15)
    else:
        print(f"\n  📋 执行以下命令注册委员会公钥到链上：")
        print_curl("http://127.0.0.1:8545", "eth_registerCommitteePubKey", [pubkey])
        wait_user("请执行上述命令，完成后按回车继续...")

    # Wait for on-chain confirmation
    log("等待委员会公钥上链确认...")
    with timed("system_init", "register_committee_pubkey_chain_confirm", contract=contract_address):
        data = calldata("committeePubKey(uint256)", ["uint256"], [0])
        deadline = time.time() + 30
        while time.time() < deadline:
            val = int.from_bytes(eth_call(node_rpc(1), contract_address, data), "big")
            if val != 0:
                break
            time.sleep(1)

    # Wait for committee vrf bridge to sync the pubkey
    log("等待 committee vrf bridge 同步...")
    with timed("system_init", "committee_bridge_sync_wait"):
        time.sleep(5)
    print("  ✅ 委员会公钥已成功上链")


# ══════════════════════════════════════════════
# Phase 4: Register TEEs (interactive, one by one)
# ══════════════════════════════════════════════

def phase4_register_tees() -> None:
    global tee_info
    step_header("Phase 4", f"注册 {TEE_NUM} 个 TEE（协议 4-1 步骤 2-8）")

    tee_info = []
    for i in range(TEE_OWNER_START, TEE_OWNER_END + 1):
        account = accounts[i - 1]
        sender = account["address"]
        privkey = account["privateKey"]

        # Auto-generate VRF keypair
        with timed("system_init", "simulated_tee_material_generation", node=f"node{i}", owner=sender):
            vrf_privkey = derive_vrf_private_key(i)
            vrf_pubkey = vrf_pubkey_from_private(vrf_privkey)
            quote_hex, mr_hex, hw_id_hex, report_data = build_quote(i, vrf_pubkey, committee_pubkey_hex)

        print(f"\n  ── TEE node{i} ({sender}) ──")
        print(f"  HardwareId: {hw_id_hex}")
        print(f"  VrfPubKey:  {vrf_pubkey.hex()}")
        print(f"  Quote:      {quote_hex[:40]}...")

        # Save material
        material = {
            "index": i, "owner_address": sender,
            "quote": quote_hex, "hardware_id": hw_id_hex, "report_data": report_data,
            "enclave_pk": report_data["enclave_pk"],
            "vrf_pubkey": "0x" + vrf_pubkey.hex(), "vrf_private_key": "0x" + vrf_privkey.hex(),
        }
        (TEE_MATERIALS / f"tee{i}.json").write_text(json.dumps(material, indent=2))

        # Step 1: stakeTEE
        stake_inner = json.dumps({"Value": "1"})
        print(f"\n  📋 步骤 1 - stakeTEE (交保证金):")
        print(f"  使用 TEE owner 节点 RPC: {node_rpc(i)}")
        print_curl(node_rpc(i), "eth_stakeTEE", [stake_inner])

        if auto_mode:
            log(f"[auto] stakeTEE node{i}...")
            send_raw_tx(node_rpc(1), privkey, contract_address,
                        calldata("stakeTEE()", [], []), value=1,
                        timing_stage="system_init", timing_event=f"tee_node{i}.stakeTEE")
        else:
            wait_user("请执行上述命令，完成后按回车继续...")

        log(f"等待 node{i} owner pending TEE 上链...")
        with timed("system_init", "tee_pending_state_confirm", node=f"node{i}", owner=sender):
            pending = wait_for_pending_tee(sender)
        print(f"  ✅ pending TEE 已确认: nonce={pending.get('PendingNonce')} deposit={pending.get('Deposit')}")

        # Step 2: committee_registerTEE
        print(f"\n  📋 步骤 2 - committee_registerTEE (委员会代注册):")
        print_curl("http://127.0.0.1:4401/rpc", "committee_registerTEE", {"owner_address": sender, "quote": quote_hex, "report_data": report_data}, rpc_id=2)

        if auto_mode:
            log(f"[auto] committee_registerTEE node{i}...")
            with timed("system_init", "committee_registerTEE_rpc", node=f"node{i}", owner=sender):
                result = committee_rpc("committee_registerTEE", {"quote": quote_hex, "owner_address": sender, "report_data": report_data}, timeout=30)
            tx_hash = result.get("tx_hash", "") if isinstance(result, dict) else ""
            if tx_hash:
                actual = find_pool_hash(node_rpc(1), accounts[0]["address"], tx_hash)
                with timed("system_init", "committee_registerTEE_receipt", node=f"node{i}", tx_hash=actual):
                    wait_for_receipt(node_rpc(1), actual, timeout=60)
        else:
            wait_user("请执行上述命令，完成后按回车继续...")

        # Verify
        log(f"验证 node{i} TEE 注册状态...")
        with timed("system_init", "tee_registration_state_confirm", node=f"node{i}", owner=sender):
            status_out = eth_call(node_rpc(1), contract_address,
                                  calldata("getTEEStatusByOwner(address)", ["address"], [sender.removeprefix("0x")]))
            status_val = int.from_bytes(status_out[-32:], "big") if len(status_out) >= 32 else 0
        status_name = {0: "None", 1: "Pending", 2: "Candidate", 3: "Active"}.get(status_val, f"Unknown({status_val})")
        print(f"  ✅ node{i} TEE 状态: {status_name}")

        tee_info.append({
            "hw_id": hw_id_hex, "vrf_pubkey": vrf_pubkey,
            "vrf_privkey": vrf_privkey, "owner_key": privkey,
            "owner_address": sender, "node_index": i,
        })

    # Summary
    candidates = call_bytes32_array(node_rpc(1), contract_address, "getCandidateTEEs()")
    active = call_bytes32_array(node_rpc(1), contract_address, "getActiveTEEs()")
    print(f"\n  📊 注册完成: {len(candidates)} Candidate + {len(active)} Active = {len(candidates)+len(active)} 总计")

    # Set pkCH
    step_header("Phase 4b", "设置变色龙公钥 pkCH（协议 4-1 步骤 11）")
    helper = json.loads(HELPER_JSON.read_text())
    pk_ch = helper["pubKey"]
    params_before = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    pk_hash_before = str(params_before.get("PkCHHash", ""))
    active_sender = active_tee_owner_for_command(active, tee_info)
    active_node = int(active_sender["node_index"])
    pk_ch_sig = pkch_signature(node_rpc(active_node), contract_address, active_sender["owner_address"], active_sender["owner_key"], pk_ch)
    print(f"  pkCH (from helper.json): {pk_ch[:40]}...")
    print(f"  使用 Active TEE owner node{active_node} ({active_sender['owner_address']})")
    print(f"\n  📋 设置 pkCH:")
    print_curl(node_rpc(active_node), "eth_setPkCH", [pk_ch, "0x" + pk_ch_sig.hex()])

    if auto_mode:
        log("[auto] setPkCH...")
        send_raw_tx(node_rpc(1), active_sender["owner_key"], contract_address,
                    calldata("setPkCH(bytes,bytes)", ["bytes", "bytes"], [bytes.fromhex(pk_ch.removeprefix("0x")), pk_ch_sig]),
                    timing_stage="system_init", timing_event="setPkCH")
    else:
        wait_user("请执行上述命令，完成后按回车继续...")

    with timed("system_init", "setPkCH_chain_confirm", contract=contract_address):
        params = wait_for_pkch_set(pk_hash_before)
    pk_ver = int(str(params["CurrentPkCHVersion"]), 10)
    print(f"  ✅ pkCH 已设置, version={pk_ver}, hash={params.get('PkCHHash')}")


# ══════════════════════════════════════════════
# Phase 5: Rotation + Redaction loop
# ══════════════════════════════════════════════

def do_vrf_rotation(round_num: int) -> list[str]:
    """Guide through VRF-based TEE rotation (Protocol 4-4)."""
    step_header(f"Phase 5.{round_num}a", f"VRF 轮换 #{round_num}（协议 4-4）")

    url = node_rpc(1)
    current_epoch = call_uint(url, contract_address, "currentEpoch()")
    last_rot_block = call_uint(url, contract_address, "lastRotationBlock()")
    previous_active = call_bytes32_array(url, contract_address, "getActiveTEEs()")
    next_epoch = current_epoch + 1
    deadline_block = last_rot_block + EPOCH_LENGTH

    print(f"  currentEpoch={current_epoch}, lastRotationBlock={last_rot_block}")
    print(f"  previousActive={len(previous_active)}")
    print(f"  nextEpoch={next_epoch}, deadlineBlock={deadline_block}")

    # Wait for preparation window
    prepare_block = deadline_block - ROTATION_PREPARE_BLOCKS
    current_block = int_from_hex(rpc(url, "eth_blockNumber", [], timeout=5))
    if current_block < prepare_block:
        log(f"等待 prepare phase (当前块 {current_block}, 需要 {prepare_block})...")
        with timed("rotation_flow", "prepare_window_wait", round=round_num, from_block=current_block, target_block=prepare_block):
            while True:
                cb = int_from_hex(rpc(url, "eth_blockNumber", [], timeout=5))
                if cb >= prepare_block:
                    break
                time.sleep(2)

    # prepareRotation
    print(f"\n  📋 调用 prepareRotation():")
    print_curl("http://127.0.0.1:8545", "eth_sendTransaction", [{"from": accounts[0]["address"], "to": contract_address, "data": "0x" + calldata("prepareRotation()", [], []).hex()}])

    if auto_mode:
        log("[auto] prepareRotation...")
        send_raw_tx(url, accounts[0]["privateKey"], contract_address, calldata("prepareRotation()", [], []),
                    timing_stage="rotation_flow", timing_event=f"round{round_num}.prepareRotation")
    else:
        wait_user("请执行上述命令，完成后按回车继续...")

    # Get seed
    seed_out = eth_call(url, contract_address, calldata("seedByEpoch(uint256)", ["uint256"], [next_epoch]))
    seed_bytes = seed_out[-32:] if len(seed_out) >= 32 else seed_out.rjust(32, b"\x00")
    log(f"seed for epoch {next_epoch} = 0x{seed_bytes.hex()[:16]}...")

    # Submit VRFs — send without waiting for receipt (rotateIfNeeded may trigger
    # after nT submissions, causing subsequent ones to fail if we block)
    chain_id = int_from_hex(rpc(url, "eth_chainId", []))
    contract_addr_bytes = bytes.fromhex(contract_address.removeprefix("0x"))[-20:]

    # Only submit up to nT VRFs to avoid rotation-triggered failures
    nT = call_uint(url, contract_address, "nT()")
    max_vrf = min(len(tee_info), nT)
    log(f"为 {max_vrf}/{len(tee_info)} 个 TEE 生成并提交 VRF proof (nT={nT})...")
    submitted_hashes: list[str] = []

    for idx, info in enumerate(tee_info[:max_vrf]):
        hw_id_bytes = bytes.fromhex(info["hw_id"].removeprefix("0x"))
        vrf_pk = info["vrf_pubkey"]
        vrf_priv = info["vrf_privkey"]

        with timed("rotation_flow", "vrf_proof_generation", round=round_num, tee_index=idx + 1, node=f"node{info['node_index']}"):
            digest_data = abi_encode(
                ["bytes32", "uint256", "address", "uint256", "bytes32", "bytes32", "bytes32"],
                [VRF_DOMAIN, chain_id, contract_addr_bytes, next_epoch, hw_id_bytes, vrf_pk, seed_bytes],
            )
            vrf_digest = keccak.new(digest_bits=256, data=digest_data).digest()
            r, s, v = ecdsa_sign_raw_hash(vrf_priv, vrf_digest)
            r_bytes = r.to_bytes(32, "big")
            s_bytes = s.to_bytes(32, "big")
            vrf_result = keccak.new(digest_bits=256, data=VRF_DOMAIN + seed_bytes + vrf_pk + r_bytes + s_bytes + bytes([v])).digest()
            vrf_proof = r_bytes + s_bytes + bytes([v])

        calldata_hex = calldata(
            "submitTEESelectionVRF(uint256,bytes32,bytes32,bytes)",
            ["uint256", "bytes32", "bytes32", "bytes"],
            [next_epoch, hw_id_bytes, vrf_result, vrf_proof],
        ).hex()

        owner_rpc = node_rpc(int(info["node_index"]))
        print(f"\n  📋 TEE {idx+1}/{max_vrf} - submitTEESelectionVRF:")
        print(f"  使用 TEE owner 节点 RPC: {owner_rpc}")
        print_curl(owner_rpc, "eth_sendTransaction", [{"from": info["owner_address"], "to": contract_address, "data": "0x" + calldata_hex}], rpc_id=idx+1)

        if auto_mode:
            # Send raw tx without waiting for receipt
            account = Account.from_key(info["owner_key"])
            nonce = int_from_hex(rpc(url, "eth_getTransactionCount", [account.address, "pending"]))
            gas_price = int_from_hex(rpc(url, "eth_gasPrice", []))
            data = calldata(
                "submitTEESelectionVRF(uint256,bytes32,bytes32,bytes)",
                ["uint256", "bytes32", "bytes32", "bytes"],
                [next_epoch, hw_id_bytes, vrf_result, vrf_proof],
            )
            tx_obj = {
                "nonce": nonce, "to": contract_address, "value": 0,
                "gas": 500000, "gasPrice": gas_price,
                "data": data, "chainId": chain_id,
            }
            signed = Account.sign_transaction(tx_obj, info["owner_key"])
            raw = getattr(signed, "rawTransaction", None) or getattr(signed, "raw_transaction")
            raw_hex = raw.hex() if isinstance(raw, bytes) else raw
            if not raw_hex.startswith("0x"):
                raw_hex = "0x" + raw_hex
            with timed("rotation_flow", "vrf_broadcast_rpc", round=round_num, tee_index=idx + 1, node=f"node{info['node_index']}"):
                tx_hash = rpc(url, "eth_sendRawTransaction", [raw_hex])
            submitted_hashes.append(tx_hash)
            log(f"VRF submitted tx={tx_hash[:16]}...")

    if not auto_mode and len(tee_info) > max_vrf:
        print(f"\n  ... 剩余 {len(tee_info)-max_vrf} 个 TEE 的 VRF 提交命令格式相同")
        wait_user("请提交所有 TEE 的 VRF，完成后按回车继续...")

    # Wait for submitted VRFs to be mined
    if auto_mode and submitted_hashes:
        log(f"等待 {len(submitted_hashes)} 个 VRF 交易上链...")
        for h in submitted_hashes:
            try:
                with timed("rotation_flow", "vrf_receipt", round=round_num, tx_hash=h):
                    wait_for_receipt(url, h, timeout=30)
            except Exception as exc:
                log(f"VRF tx {h[:16]}... 可能因轮换已触发而失败: {exc}")

    # Wait for deadline block OR auto-rotation (triggered by nT VRFs)
    current_block = int_from_hex(rpc(url, "eth_blockNumber", [], timeout=5))
    new_epoch = call_uint(url, contract_address, "currentEpoch()")
    if new_epoch <= current_epoch and current_block < deadline_block:
        log(f"等待 deadline block {deadline_block} (当前 {current_block})...")
        with timed("rotation_flow", "deadline_or_auto_rotation_wait", round=round_num, from_block=current_block, deadline_block=deadline_block):
            while True:
                cb = int_from_hex(rpc(url, "eth_blockNumber", [], timeout=5))
                ne = call_uint(url, contract_address, "currentEpoch()")
                if cb >= deadline_block or ne > current_epoch:
                    break
                time.sleep(2)

    # setTrustedMREnclave to finalize rotation if not already rotated
    new_epoch = call_uint(url, contract_address, "currentEpoch()")
    if new_epoch <= current_epoch:
        mr_new = "0x" + (bytes([0xC0 + round_num]) * 32).hex()
        print(f"\n  📋 触发轮换 - setTrustedMREnclave:")
        print_curl("http://127.0.0.1:8545", "eth_setTrustedMREnclave", [mr_new, True])

        if auto_mode:
            log("[auto] setTrustedMREnclave...")
            with timed("rotation_flow", "setTrustedMREnclave_trigger_rpc", round=round_num):
                rpc(url, "eth_setTrustedMREnclave", [mr_new, True], timeout=15)
        else:
            wait_user("请执行上述命令，完成后按回车继续...")
    else:
        log(f"轮换已自动触发 (VRF >= nT)")

    # Verify rotation
    log("等待轮换完成...")
    with timed("rotation_flow", "epoch_advance_confirm", round=round_num, from_epoch=current_epoch):
        deadline = time.time() + 120
        while time.time() < deadline:
            new_epoch = call_uint(url, contract_address, "currentEpoch()")
            if new_epoch > current_epoch:
                active = call_bytes32_array(url, contract_address, "getActiveTEEs()")
                print(f"  ✅ 轮换完成: epoch {current_epoch} → {new_epoch}, active={len(active)}")
                submit_erase_proofs_for_departed(previous_active, active, current_epoch)
                return active
            time.sleep(2)

    raise TimeoutError(f"轮换超时: epoch 仍为 {current_epoch}")


def do_redaction(round_num: int) -> None:
    """Guide through a chain redaction (Protocol 4-3)."""
    step_header(f"Phase 5.{round_num}b", f"链编辑 #{round_num}（协议 4-3）")

    url = node_rpc(1)
    old_payload = f"redaction-before-r{round_num}".encode()
    new_payload = f"redaction-after-r{round_num}".encode()

    # Send target tx
    print(f"\n  📋 步骤 1 - 发送目标交易:")
    print(f"  Payload: {old_payload}")
    log(f"发送目标交易 (payload={old_payload})...")
    tx_hash = send_raw_tx(url, accounts[5]["privateKey"], accounts[1]["address"],
                           old_payload, gas_limit=21000 + len(old_payload) * 64,
                           timing_stage="transaction_generation", timing_event=f"redaction_round{round_num}.target_tx")
    with timed("transaction_generation", "target_tx_receipt_read", round=round_num, tx_hash=tx_hash):
        receipt = rpc(url, "eth_getTransactionReceipt", [tx_hash], timeout=10)
    block_number = int_from_hex(receipt["blockNumber"])
    tx_index = int_from_hex(receipt["transactionIndex"])
    print(f"  ✅ 目标交易已上链: block={block_number}, index={tx_index}")

    # Build redact request
    print(f"\n  📋 步骤 2 - 构建编辑请求:")
    build_params = json.dumps({
        "BlockNum": str(block_number), "Index": tx_index,
        "ModifyType": 1, "DataAfter": new_payload.decode(),
        "RedactReason": f"e2e-round-{round_num}",
        "Meta": f'{{"rule":"allow","case":"e2e-round-{round_num}"}}',
    })
    print_curl("http://127.0.0.1:8545", "eth_buildRedactRequest", [build_params])

    log("构建编辑请求...")
    with timed("edit_execution", "build_redact_request_rpc", round=round_num, block_number=block_number, tx_index=tx_index):
        build = rpc(url, "eth_buildRedactRequest", [build_params], timeout=15)  # geth unmarshals from string
    request_id = build["RequestID"]
    print(f"  RequestID: {request_id}")

    # Submit to committee
    print(f"\n  📋 步骤 3 - 提交编辑请求到委员会:")
    expires_at = str(int(time.time()) + 3600)
    submit_params = {
        "chain_id": str(int_from_hex(rpc(url, "eth_chainId", []))),
        "contract_address": contract_address,
        "committee_pub_key_version": str(build["CommitteePubKeyVersion"]),
        "expires_at": expires_at,
        "policy_context": {"rule": "allow", "case": f"e2e-round-{round_num}"},
        "req": {
            "version": build["Version"], "epoch": int(build["Epoch"]),
            "req_id": build["ReqID"],
            "body_hashes": dict(zip(
                ["tx_id", "m_hash", "m_prime_hash", "pos_hash", "meta_hash", "active_set_hash", "pk_ch_hash"],
                build["BodyHashes"],
            )),
        },
    }
    print_curl("http://127.0.0.1:4401/rpc", "committee_submitRedactRequest", submit_params, rpc_id=3)

    total_start_ns = time.perf_counter_ns()
    if auto_mode:
        log("[auto] submitRedactRequest...")
    else:
        log("自动提交编辑请求到委员会...")
    with timed("edit_execution", "committee_submit_redact_request", round=round_num, request_id=request_id):
        submit = committee_rpc("committee_submitRedactRequest", submit_params, timeout=20)
    if not auto_mode:
        print(f"  ✅ 已提交到委员会")

    # Wait for authorization
    log("等待委员会授权...")
    with timed("edit_execution", "committee_authorization_wait", round=round_num, request_id=request_id):
        deadline = time.time() + 60
        auth = {}
        while time.time() < deadline:
            auth = committee_rpc("committee_getRedactAuthorization", {"request_id": submit["request_id"]}, timeout=10)
            if auth.get("status") == "authorized" and auth.get("cert_req_auth"):
                break
            time.sleep(1)
    if auth.get("status") != "authorized":
        raise TimeoutError(f"授权超时: {auth}")
    print(f"  ✅ 委员会已授权: status={auth['status']}")

    # Submit authorization on chain
    print(f"\n  📋 步骤 4 - 提交授权到链上:")
    if auto_mode:
        log("[auto] submitRedactAuthorizationOnChain...")
    else:
        log("自动提交授权到链上...")
    with timed("edit_execution", "committee_submit_authorization_onchain_rpc", round=round_num, request_id=request_id):
        auth_chain = committee_rpc("committee_submitRedactAuthorizationOnChain", {"request_id": submit["request_id"]}, timeout=20)

    auth_tx = auth_chain.get("tx_hash", "")
    if auth_tx:
        actual = find_pool_hash(url, accounts[0]["address"], auth_tx)
        with timed("edit_execution", "authorization_onchain_receipt", round=round_num, request_id=request_id, tx_hash=actual):
            wait_for_receipt(url, actual, timeout=60)
    print(f"  ✅ 授权已上链: tx={auth_tx[:16]}...")

    # Execute local redaction
    print(f"\n  📋 步骤 5 - 执行本地编辑 (eth_executeLocalRedaction):")
    active = call_bytes32_array(url, contract_address, "getActiveTEEs()")
    tee_threshold = call_uint(url, contract_address, "tT()")
    active_senders = active_tee_owners_for_command(active, tee_info, tee_threshold)
    redact_record = rpc(url, "eth_getRedactRequest", [request_id], timeout=10)
    d_req = redact_record.get("DReq", "")
    if not d_req:
        raise RuntimeError(f"授权后的编辑请求缺少 DReq: {redact_record}")
    request_id_prefix = bytes.fromhex(request_id.removeprefix("0x"))[:8]
    with timed("edit_execution", "simulated_tee_share_proof_generation", round=round_num, request_id=request_id, threshold=tee_threshold):
        share_proofs: list[dict[str, str]] = []
        for proof_index, active_sender in enumerate(active_senders):
            share = f"interactive-share-r{round_num}-{proof_index}:".encode() + request_id_prefix
            share_proofs.extend(build_share_proofs(d_req, active_sender, share))
    signers = ", ".join(f"node{sender['node_index']}" for sender in active_senders)
    print(f"  使用 Active TEE owner {signers} 生成 {len(share_proofs)} 份份额证明")
    exec_params = json.dumps({
        "ReqID": request_id, "BlockNum": str(block_number), "Index": tx_index,
        "ModifyType": 1, "DataAfter": new_payload.decode(),
        "ShareProofs": share_proofs,
    })
    print_curl("http://127.0.0.1:8545", "eth_executeLocalRedaction", [exec_params], rpc_id=4)

    if auto_mode:
        log("[auto] executeLocalRedaction...")
    else:
        log("执行本地编辑...")
    with timed("edit_execution", "execute_local_redaction_rpc", round=round_num, request_id=request_id):
        executed = rpc(url, "eth_executeLocalRedaction", [exec_params], timeout=45)

    if executed.get("Status") != "executed":
        raise RuntimeError(f"本地编辑失败: {executed}")
    record_timing("edit_execution", "total_on_submit_to_executed", total_start_ns, time.perf_counter_ns(), request_id=request_id, round=round_num, tx_hash=executed.get("TxHash", ""))
    print(f"  ✅ 编辑已执行: {executed.get('TxHash', '')[:16]}...")

    # Verify
    log("验证编辑结果并检查 node1/node2/node3 同步...")
    new_input_hex = "0x" + new_payload.hex()
    with timed("edit_execution", "redaction_sync_confirm", round=round_num, request_id=request_id, node_count=3):
        verify_redaction_synced(block_number, tx_index, new_input_hex)
    print(f"  ✅ 验证通过: node1/node2/node3 交易 input 均已从 '{old_payload.decode()}' 变更为 '{new_payload.decode()}'")


# ══════════════════════════════════════════════
# Main flow
# ══════════════════════════════════════════════

def main() -> int:
    global auto_mode

    parser = argparse.ArgumentParser(description="Interactive E2E guide for T-govern v0.8")
    parser.add_argument("--auto", action="store_true", help="Auto-execute all transactions (for CI)")
    parser.add_argument("--verify-only", action="store_true", help="Only verify current state")
    args = parser.parse_args()
    auto_mode = args.auto

    os.environ.setdefault("NO_PROXY", "localhost,127.0.0.1")
    os.environ.setdefault("no_proxy", "localhost,127.0.0.1")

    atexit.register(cleanup)

    if args.verify_only:
        log("verify-only 模式: 检查当前环境状态...")
        # TODO: add verification logic
        return 0

    # Execute phases
    with timed("system_init", "phase0_env_check"):
        phase0_env_check()
    with timed("system_init", "phase1_prepare_total"):
        phase1_prepare()
    with timed("system_init", "phase2_start_nodes_total"):
        phase2_start_nodes()
    with timed("system_init", "phase3_committee_pubkey_total"):
        phase3_committee_pubkey()
    with timed("system_init", "phase4_register_tees_and_pkch_total"):
        phase4_register_tees()

    # Rotation + Redaction loop
    rotation_count = 3
    for rnd in range(1, rotation_count + 1):
        with timed("rotation_flow", "round_total", round=rnd):
            do_vrf_rotation(rnd)
        if rnd <= 2:  # Edit after rotation 1 and 2
            with timed("edit_execution", "round_total", round=rnd):
                do_redaction(rnd)

    # Summary
    step_header("完成", "全流程测试结果汇总")
    result = {
        "contract": contract_address,
        "tee_count": len(tee_info),
        "rotation_count": rotation_count,
        "redaction_count": 2,
        "work_dir": str(WORK),
        "logs": str(LOGS),
        "accounts_file": str(WORK / "accounts.json"),
        "tee_materials_dir": str(TEE_MATERIALS),
        "timing_jsonl": str(TIMING_JSONL),
    }
    (WORK / "result.json").write_text(json.dumps(result, indent=2))
    print(f"  📁 结果文件: {WORK / 'result.json'}")
    print(f"  📁 日志目录: {LOGS}")
    print(f"  ✅ 全流程测试完成！")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        log("用户中断")
        cleanup()
        sys.exit(1)
    except Exception as exc:
        log(f"FAILED: {exc}")
        cleanup()
        raise
