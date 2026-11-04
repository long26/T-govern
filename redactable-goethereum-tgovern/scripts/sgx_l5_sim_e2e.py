#!/usr/bin/env python3
"""L5 SGX SIM multi-node E2E runner.

This follows committee-mvp's interactive-e2e committee RPC shape, but uses
geth-sgx TEE hosts and eth_sendTEERedactionMaterial for the final TEE cluster
path instead of the deprecated local-redaction shortcut.
"""

from __future__ import annotations

import atexit
import concurrent.futures
import datetime
import hashlib
import json
import math
import os
import random
import shlex
import shutil
import signal
import subprocess
import sys
import threading
import time
import urllib.request
from pathlib import Path
from typing import Any

import communication_stats

from eth_account import Account
from eth_utils import keccak

PACKAGE_ROOT = Path(os.environ.get("TGOVERN_ROOT", Path(__file__).resolve().parents[1])).resolve()
GETH_DIR = PACKAGE_ROOT


def env_path(name: str) -> Path | None:
    raw = os.environ.get(name, "").strip()
    return Path(raw).expanduser().resolve() if raw else None


def first_path(name: str, candidates: list[Path]) -> Path:
    override = env_path(name)
    if override is not None:
        return override
    for candidate in candidates:
        if candidate.exists():
            return candidate
    return candidates[0]


GETH_BIN = first_path("GETH_BIN", [
    PACKAGE_ROOT / "bin" / "geth-sgx",
    PACKAGE_ROOT / "build" / "bin" / "geth-sgx",
])
COMMITTEE_BIN = first_path("COMMITTEE_BIN", [
    PACKAGE_ROOT / "bin" / "committee-mvp",
    PACKAGE_ROOT / "committee-mvp" / "build" / "committee-mvp",
])
COMMITTEE_SRC_DIR = first_path("COMMITTEE_SRC_DIR", [
    PACKAGE_ROOT / "committee-mvp",
])
COMMITTEE_CWD = env_path("COMMITTEE_CWD") or (COMMITTEE_SRC_DIR if COMMITTEE_SRC_DIR.exists() else PACKAGE_ROOT)
GENESIS = first_path("GENESIS_JSON", [PACKAGE_ROOT / "genesis.json"])
HELPER_JSON = first_path("HELPER_JSON", [PACKAGE_ROOT / "helper.json"])
SGX_ENCLAVE_PATH = first_path("SGX_ENCLAVE_PATH", [PACKAGE_ROOT / "sgx" / "enclave.signed.so"])
SGX_MODULE_DIR = first_path("SGX_MODULE_DIR", [PACKAGE_ROOT / "sgx"])


def detect_sgx_mode() -> str:
    raw = os.environ.get("SGX_MODE", "").strip().upper()
    if raw:
        return raw
    if GETH_BIN.exists():
        try:
            out = subprocess.run(
                ["ldd", str(GETH_BIN)],
                check=False,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
            ).stdout
            if "libsgx_urts_sim.so" in out:
                return "SIM"
            if "libsgx_urts.so" in out or "libsgx_urts.so." in out:
                return "HW"
        except OSError:
            pass
    return "SIM"


SGX_MODE = detect_sgx_mode()
TEE_QUOTE_MODE = os.environ.get("TEE_QUOTE_MODE", "real" if SGX_MODE == "HW" else "mock").strip().lower()
if TEE_QUOTE_MODE not in {"real", "mock", "auto"}:
    raise ValueError(f"TEE_QUOTE_MODE must be one of real, mock, auto; got {TEE_QUOTE_MODE!r}")
ATTESTATION_PROVIDER = os.environ.get("TEE_ATTESTATION_PROVIDER", "noop").strip().lower() or "noop"

WORK = GETH_DIR / ".tmp" / "sgx-l5-sim"
CHAIN = WORK / "chain"
NODE_WORK = WORK / "node-work"
COMMITTEE_CONFIGS = WORK / "committee-configs"
LOGS = WORK / "logs"
ETHASH_CACHE_DIR = env_path("ETHASH_CACHE_DIR") or (WORK / "ethash-cache")

NETWORK_ID = "739205"
PASSWORD = "1"


def env_int(name: str, default: int, minimum: int) -> int:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw, 10)
    except ValueError as err:
        raise ValueError(f"{name} must be an integer, got {raw!r}") from err
    if value < minimum:
        raise ValueError(f"{name} must be >= {minimum}, got {value}")
    return value


def env_int_any(names: tuple[str, ...], default: int, minimum: int) -> int:
    for name in names:
        raw = os.environ.get(name, "").strip()
        if raw:
            return env_int(name, default, minimum)
    return default


def env_float(name: str, default: float, minimum: float) -> float:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError as err:
        raise ValueError(f"{name} must be a number, got {raw!r}") from err
    if value < minimum:
        raise ValueError(f"{name} must be >= {minimum}, got {value}")
    return value


def env_text(name: str) -> str:
    return os.environ.get(name, "").strip()


def env_list(name: str) -> list[str]:
    raw = env_text(name)
    if not raw:
        return []
    if raw.startswith("["):
        value = json.loads(raw)
        if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
            raise ValueError(f"{name} must be a JSON string array")
        return value
    return shlex.split(raw)


def is_power_of_two(value: int) -> bool:
    return value > 0 and value & (value - 1) == 0


ACTIVE_LIMIT = env_int("TEE_ACTIVE_LIMIT", 4, 1)
if not is_power_of_two(ACTIVE_LIMIT):
    raise ValueError(f"TEE_ACTIVE_LIMIT must be a power of two for Go NITCH combine, got {ACTIVE_LIMIT}")
ACTIVE_THRESHOLD = env_int("TEE_ACTIVE_THRESHOLD", 3, 1)
if ACTIVE_THRESHOLD > ACTIVE_LIMIT:
    raise ValueError(f"TEE_ACTIVE_THRESHOLD ({ACTIVE_THRESHOLD}) must be <= TEE_ACTIVE_LIMIT ({ACTIVE_LIMIT})")
TEE_COUNT = env_int("TEE_CANDIDATE_COUNT", 4, ACTIVE_LIMIT)
MAX_TEE_HOST_COUNT = 64
if TEE_COUNT > MAX_TEE_HOST_COUNT:
    raise ValueError(
        f"TEE_CANDIDATE_COUNT ({TEE_COUNT}) must be <= {MAX_TEE_HOST_COUNT}; "
        "use LIGHT_NODE_COUNT for large non-TEE populations"
    )
ON_INDEX = 1       # ON 普通节点：同时负责 mining/contract/RPC，不开 --teehost
EXTRA_MINER_COUNT = env_int_any(("TEE_EXTRA_MINERS", "EXTRA_MINER_COUNT", "GETH_EXTRA_MINERS"), 0, 0)
MINER_NODE_INDICES = list(range(ON_INDEX, ON_INDEX + EXTRA_MINER_COUNT + 1))
# ON 观察者：最后一个 miner 之后的普通 geth，同样注入 node_role=on（Go 侧 emitLedgerTiming
# 门只认角色不认编号），与 nitch 的 ledger_observer 同口径。编号 = miner+1（多矿工时顺延）。
ON_OBSERVER_COUNT = env_int("TEE_ON_OBSERVERS", 1, 0)
ON_OBSERVER_INDICES = list(range(
    MINER_NODE_INDICES[-1] + 1,
    MINER_NODE_INDICES[-1] + 1 + ON_OBSERVER_COUNT,
))
TEE_START_INDEX = ON_OBSERVER_INDICES[-1] + 1 if ON_OBSERVER_INDICES else MINER_NODE_INDICES[-1] + 1
TEE_NODE_INDICES = list(range(TEE_START_INDEX, TEE_START_INDEX + TEE_COUNT))
CORE_NODE_COUNT = 1 + EXTRA_MINER_COUNT + ON_OBSERVER_COUNT + TEE_COUNT
CORE_NODE_INDICES = list(range(1, CORE_NODE_COUNT + 1))
LIGHT_NODE_COUNT = env_int_any(("LIGHT_NODE_COUNT", "GETH_LIGHT_NODE_COUNT"), 0, 0)
LIGHT_START_INDEX = CORE_NODE_COUNT + 1
LIGHT_NODE_INDICES = list(range(LIGHT_START_INDEX, LIGHT_START_INDEX + LIGHT_NODE_COUNT))
TOTAL_NODE_COUNT = CORE_NODE_COUNT + LIGHT_NODE_COUNT
if 30310 + TOTAL_NODE_COUNT > 65535 or 18550 + TOTAL_NODE_COUNT > 65535:
    raise ValueError(f"TOTAL_NODE_COUNT ({TOTAL_NODE_COUNT}) exceeds the localhost port range")


def ring_neighbor_edges(nodes: list[int], radius: int) -> list[tuple[int, int]]:
    """Connect each node to a bounded number of nearby ring positions."""
    edges: list[tuple[int, int]] = []
    seen: set[tuple[int, int]] = set()
    count = len(nodes)
    if count < 2:
        return edges
    radius = min(radius, count // 2)
    for position, source in enumerate(nodes):
        for step in range(1, radius + 1):
            target = nodes[(position + step) % count]
            key = (min(source, target), max(source, target))
            if key in seen:
                continue
            seen.add(key)
            edges.append((source, target))
    return edges


def gateway_bridge_edges(gateways: list[int], tee_nodes: list[int], peer_count: int) -> list[tuple[int, int]]:
    """Attach every ordinary gateway to well-spaced TEE hosts."""
    if not gateways or not tee_nodes or peer_count <= 0:
        return []
    peer_count = min(peer_count, len(tee_nodes))
    stride = max(1, len(tee_nodes) // peer_count)
    edges: list[tuple[int, int]] = []
    for gateway_position, gateway in enumerate(gateways):
        for peer_position in range(peer_count):
            tee_position = (gateway_position + peer_position * stride) % len(tee_nodes)
            edges.append((gateway, tee_nodes[tee_position]))
    return edges


def power_ring_edges(nodes: list[int], levels: int) -> list[tuple[int, int]]:
    """Build a deterministic connected overlay with power-of-two shortcuts."""
    if len(nodes) < 2 or levels <= 0:
        return []
    edges: list[tuple[int, int]] = []
    seen: set[tuple[int, int]] = set()
    count = len(nodes)
    for position, source in enumerate(nodes):
        for level in range(levels):
            offset = 1 << level
            if offset >= count:
                break
            target = nodes[(position + offset) % count]
            key = (min(source, target), max(source, target))
            if key in seen:
                continue
            seen.add(key)
            edges.append((source, target))
    return edges


def merge_topology_edges(*groups: list[tuple[int, int]]) -> list[tuple[int, int]]:
    edges: list[tuple[int, int]] = []
    seen: set[tuple[int, int]] = set()
    for group in groups:
        for source, target in group:
            key = (min(source, target), max(source, target))
            if source == target or key in seen:
                continue
            seen.add(key)
            edges.append((source, target))
    return edges


def power_ring_hop_bound(node_count: int, levels: int) -> int:
    """Conservative diameter bound for an undirected power-of-two ring."""
    if node_count <= 1:
        return 0
    largest_offset = 1 << max(0, levels - 1)

    def steps(distance: int) -> int:
        return distance // largest_offset + (distance % largest_offset).bit_count()

    return max(min(steps(distance), steps(node_count - distance)) for distance in range(1, node_count))


# TEE hosts use a relatively dense but non-complete overlay: bounded ring
# neighbours provide path redundancy while power-of-two shortcuts keep the
# diameter logarithmic. Ordinary gateways and light nodes use a separate, lower
# degree overlay so DistKG/DKRS traffic never depends on light nodes.
LIGHT_OVERLAY_NODE_INDICES = MINER_NODE_INDICES + ON_OBSERVER_INDICES + LIGHT_NODE_INDICES
DEFAULT_TEE_NEIGHBOR_RADIUS = min(8, max(1, TEE_COUNT // 4))
TEE_P2P_NEIGHBOR_RADIUS = env_int("TEE_P2P_NEIGHBOR_RADIUS", DEFAULT_TEE_NEIGHBOR_RADIUS, 1)
TEE_P2P_NEIGHBOR_RADIUS = min(TEE_P2P_NEIGHBOR_RADIUS, max(1, TEE_COUNT // 2))
DEFAULT_TEE_SHORTCUT_LEVELS = max(1, math.ceil(math.log2(max(2, TEE_COUNT))))
TEE_P2P_SHORTCUT_LEVELS = env_int("TEE_P2P_SHORTCUT_LEVELS", DEFAULT_TEE_SHORTCUT_LEVELS, 1)
TEE_P2P_SHORTCUT_LEVELS = min(TEE_P2P_SHORTCUT_LEVELS, DEFAULT_TEE_SHORTCUT_LEVELS)
TEE_GATEWAY_PEERS = min(TEE_COUNT, env_int("TEE_GATEWAY_PEERS", min(8, TEE_COUNT), 1))
DEFAULT_SPARSE_LEVELS = max(1, math.ceil(math.log2(max(2, len(LIGHT_OVERLAY_NODE_INDICES)))))
P2P_SPARSE_LEVELS = env_int("P2P_SPARSE_LEVELS", DEFAULT_SPARSE_LEVELS, 1)
P2P_SPARSE_LEVELS = min(P2P_SPARSE_LEVELS, DEFAULT_SPARSE_LEVELS)
TEE_TOPOLOGY_EDGES = merge_topology_edges(
    ring_neighbor_edges(TEE_NODE_INDICES, TEE_P2P_NEIGHBOR_RADIUS),
    power_ring_edges(TEE_NODE_INDICES, TEE_P2P_SHORTCUT_LEVELS),
)
GATEWAY_BRIDGE_EDGES = gateway_bridge_edges(
    MINER_NODE_INDICES, TEE_NODE_INDICES, TEE_GATEWAY_PEERS
)
# P2P_SEALER_ORDINARY_PEERS>0 时把 miner 摘出普通 overlay 环，改为每个 sealer 显式连 N 个
# well-spaced 普通入口：配合 TEE_GATEWAY_PEERS=TEE_COUNT + GETH_BLOCK_BROADCAST_LIMIT 全推，
# T0 全块名单被钉在「TEE 全量 + N 个入口」，避免盲推把全部级联入口压进同一窗口。默认 0
# 保持 miner 在 overlay 内的历史行为。
SEALER_ORDINARY_PEERS = env_int("P2P_SEALER_ORDINARY_PEERS", 0, 0)
ORDINARY_OVERLAY_NODE_INDICES = (
    [index for index in LIGHT_OVERLAY_NODE_INDICES if index not in set(MINER_NODE_INDICES)]
    if SEALER_ORDINARY_PEERS > 0
    else LIGHT_OVERLAY_NODE_INDICES
)
ORDINARY_TOPOLOGY_EDGES = power_ring_edges(
    ORDINARY_OVERLAY_NODE_INDICES, P2P_SPARSE_LEVELS
)
SEALER_ENTRY_EDGES = (
    gateway_bridge_edges(MINER_NODE_INDICES, ORDINARY_OVERLAY_NODE_INDICES, SEALER_ORDINARY_PEERS)
    if SEALER_ORDINARY_PEERS > 0
    else []
)
P2P_TOPOLOGY = (
    "tee-dense-power-ring+ordinary-power-ring-nosealer+sealer-entries"
    if SEALER_ORDINARY_PEERS > 0
    else "tee-dense-power-ring+ordinary-power-ring"
)
TOPOLOGY_EDGES = merge_topology_edges(
    TEE_TOPOLOGY_EDGES,
    GATEWAY_BRIDGE_EDGES,
    ORDINARY_TOPOLOGY_EDGES,
    SEALER_ENTRY_EDGES,
)
TEE_DIAMETER_BOUND = power_ring_hop_bound(TEE_COUNT, TEE_P2P_SHORTCUT_LEVELS)
ORDINARY_DIAMETER_BOUND = power_ring_hop_bound(
    len(ORDINARY_OVERLAY_NODE_INDICES), P2P_SPARSE_LEVELS
)
P2P_DIAMETER_BOUND = TEE_DIAMETER_BOUND + 1 + ORDINARY_DIAMETER_BOUND

EXPECTED_PEERS: dict[int, set[int]] = {index: set() for index in range(1, TOTAL_NODE_COUNT + 1)}
OUTBOUND_PEERS: dict[int, int] = {index: 0 for index in range(1, TOTAL_NODE_COUNT + 1)}
for edge_source, edge_target in TOPOLOGY_EDGES:
    EXPECTED_PEERS[edge_source].add(edge_target)
    EXPECTED_PEERS[edge_target].add(edge_source)
    OUTBOUND_PEERS[edge_source] += 1
MAX_TOPOLOGY_DEGREE = max((len(peers) for peers in EXPECTED_PEERS.values()), default=0)
MAX_TOPOLOGY_OUTBOUND = max(OUTBOUND_PEERS.values(), default=0)
# Geth's default DialRatio=3 caps outbound/static dials to MaxPeers/3.
REQUIRED_GETH_MAX_PEERS = max(50, MAX_TOPOLOGY_DEGREE, MAX_TOPOLOGY_OUTBOUND * 3)
GETH_MAX_PEERS = env_int("GETH_MAX_PEERS", REQUIRED_GETH_MAX_PEERS, REQUIRED_GETH_MAX_PEERS)
GETH_P2P_MSG_DELAY_MS = env_int("GETH_P2P_MSG_DELAY_MS", 0, 0)
# Cap on peers receiving a full block on propagation; 0 = legacy sqrt(N) subset,
# set >= peer count (e.g. 999) to propagate every block to all peers.
GETH_BLOCK_BROADCAST_LIMIT = env_int("GETH_BLOCK_BROADCAST_LIMIT", 0, 0)
# 按角色发 limit：fetcher 收到 wholesale 块后会用同一 BroadcastBlock(limit) 全宽再传播
# (block_fetcher.go:679)，全局 999 会让 TEE overlay 自放大、watcher eth_getLogs 排队。
# >=0 时 TEE host 改用该值（0 = 恢复 legacy sqrt），-1 = 关闭按角色覆盖；sealer 保持全局值。
GETH_BLOCK_BROADCAST_LIMIT_TEE = env_int("GETH_BLOCK_BROADCAST_LIMIT_TEE", -1, -1)

# tc/netem loopback delay (packets queued, not throttled; closer to real WAN).
# Requires root (CAP_NET_ADMIN). Set NETEM_DELAY_MS=0 to disable.
NETEM_DELAY_MS = env_int("NETEM_DELAY_MS", 0, 0)
NETEM_JITTER_MS = env_int("NETEM_JITTER_MS", 0, 0)
NETEM_LOSS_PERCENT = env_float("NETEM_LOSS_PERCENT", 0.0, 0.0)
NETEM_RATE = env_text("NETEM_RATE")
NETEM_INTERFACE = env_text("NETEM_INTERFACE") or "lo"
# netem qdisc queue length in packets. tc netem default is 1000, which drops
# packets once 20ms of loopback traffic from dozens of nodes exceeds the queue;
# 10000 keeps the delay without the artificial loss at 32-node scale.
NETEM_LIMIT = env_int("NETEM_LIMIT", 10000, 1)

# Duplicate suppression makes a margin above the graph diameter useful when
# concurrent paths arrive out of order. Both custom gossip protocols use the same
# default but can be tuned independently for experiments.
DEFAULT_MESSAGE_RELAY_TTL = max(4, P2P_DIAMETER_BOUND * 2 + 2)
MESSAGE_RELAY_TTL = env_int("P2P_MESSAGE_RELAY_TTL", DEFAULT_MESSAGE_RELAY_TTL, 0)
DISTKG_RELAY_TTL = env_int("TEE_DISTKG_RELAY_TTL", MESSAGE_RELAY_TTL, 0)
DKRS_RELAY_TTL = env_int("TEE_DKRS_RELAY_TTL", MESSAGE_RELAY_TTL, 0)
TXMODIFY_RELAY_TTL = env_int("TXMODIFY_RELAY_TTL", MESSAGE_RELAY_TTL, 0)
ORCHESTRATION_WORKERS = env_int("P2P_ORCHESTRATION_WORKERS", min(32, max(4, os.cpu_count() or 4)), 1)
CORE_NODE_CACHE_MB = env_int("CORE_NODE_CACHE_MB", 128, 16)
CORE_NODE_GOMAXPROCS = env_int("CORE_NODE_GOMAXPROCS", 2, 1)
# 单节点 GOMAXPROCS 覆盖(隔离测量用:目标节点解除 P 竞争,其余节点保持基线配置)
GOMAX_OVERRIDE_NODE = env_int("GOMAX_OVERRIDE_NODE", -1, -1)
GOMAX_OVERRIDE_PROCS = env_int("GOMAX_OVERRIDE_PROCS", 0, 0)
# 单节点 GOGC 覆盖(隔离测量用:推迟目标节点 GC 周期,避免 assist 咬进 chameleon 窗口)
GOGC_OVERRIDE_NODE = env_int("GOGC_OVERRIDE_NODE", -1, -1)
GOGC_OVERRIDE_VALUE = env_int("GOGC_OVERRIDE_VALUE", 0, 0)
# CPU 核隔离配置: 将核心节点(16 TEE host + node 1)钉在隔离核集(默认 0-23)，
# 其余进程(普通 miner、观察者、轻节点、committee-mvp、tx.py、python harness)钉在其余核集(默认 24-31)
TEE_CORE_ISOLATION = env_int("TEE_CORE_ISOLATION", 0, 0) == 1
TEE_CORE_ISO_CORES = env_text("TEE_CORE_ISO_CORES") or "0-23"
TEE_CORE_OTHER_CORES = env_text("TEE_CORE_OTHER_CORES") or "16-23"
OBSERVER_CORE_CORES = env_text("OBSERVER_CORE_CORES") or "24-31"


def parse_cpuset(spec: str) -> set[int]:
    cpus: set[int] = set()
    for part in spec.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            lo, hi = map(int, part.split("-"))
            cpus.update(range(lo, hi + 1))
        else:
            cpus.add(int(part))
    return cpus


def apply_runner_affinity() -> None:
    if not TEE_CORE_ISOLATION:
        return
    try:
        os.sched_setaffinity(0, parse_cpuset(TEE_CORE_OTHER_CORES))
        log(f"pinned runner process to other cores: {TEE_CORE_OTHER_CORES}")
    except Exception as exc:
        log(f"failed to set runner process affinity: {exc}")

LIGHT_NODE_START_BATCH = env_int("LIGHT_NODE_START_BATCH", 64, 1)
LIGHT_NODE_CACHE_MB = env_int("LIGHT_NODE_CACHE_MB", 16, 1)
LIGHT_NODE_GOMAXPROCS = env_int("LIGHT_NODE_GOMAXPROCS", 1, 1)
LIGHT_NODE_VERBOSITY = env_int("LIGHT_NODE_VERBOSITY", 2, 0)
LIGHT_NODE_VERIFY_COUNT = min(LIGHT_NODE_COUNT, env_int("LIGHT_NODE_VERIFY_COUNT", 3, 0))
TEE_IDENTITY_READY_TIMEOUT = env_int("TEE_IDENTITY_READY_TIMEOUT", 180, 10)
P2P_CONNECT_TIMEOUT = env_int("P2P_CONNECT_TIMEOUT", max(60, P2P_DIAMETER_BOUND * 5), 10)
COMMITTEE_COUNT = env_int_any(("COMMITTEE_COUNT", "TEE_COMMITTEE_COUNT"), 4, 1)
COMMITTEE_THRESHOLD = env_int_any(("COMMITTEE_THRESHOLD", "TEE_COMMITTEE_THRESHOLD"), 3, 1)
if COMMITTEE_THRESHOLD > COMMITTEE_COUNT:
    raise ValueError(
        f"COMMITTEE_THRESHOLD ({COMMITTEE_THRESHOLD}) must be <= "
        f"COMMITTEE_COUNT ({COMMITTEE_COUNT})"
    )
ROTATIONS = env_int("TEE_ROTATIONS", 0, 0)
# The contract resets lastRotationBlock when the initial active set bootstraps.
# Epoch-1 rotation timing therefore starts from the bootstrap block, not from
# genesis or contract deployment. The script records that block and waits by
# chain progress so slow 64-node runs do not time out before the prep window.
EPOCH_LENGTH = env_int("EPOCH_LENGTH", 100, 10)
STEADY_EPOCH_LENGTH = env_int_any(("TEE_STEADY_EPOCH_LENGTH", "STEADY_EPOCH_LENGTH"), EPOCH_LENGTH, 10)
# Conservative block-time estimate used to size epoch/confirmation wall-clock
# deadlines. It is env-driven so raising the PoW difficulty (longer blocks) does
# not let those deadlines fire before the chain has actually advanced. Default 4
# matches the historical ethash diff 0x4000 / minerthreads 1 (incl. idle gaps).
EST_SECONDS_PER_BLOCK = env_float("EST_SECONDS_PER_BLOCK", 4.0, 0.1)
# Number of TEE-cluster edits performed inside each epoch interval (baseline epoch 0
# and every post-rotation epoch). TEE_EDITS_PER_EPOCH fixes the count; the MIN/MAX
# pair keeps the previous randomized behavior for repeated runs.
if os.environ.get("TEE_EDITS_PER_EPOCH", "").strip():
    EDITS_PER_EPOCH_MIN = env_int("TEE_EDITS_PER_EPOCH", 2, 1)
    EDITS_PER_EPOCH_MAX = EDITS_PER_EPOCH_MIN
else:
    EDITS_PER_EPOCH_MIN = env_int_any(("TEE_EDITS_PER_EPOCH_MIN", "TEE_REDACTIONS_PER_EPOCH_MIN"), 2, 1)
    EDITS_PER_EPOCH_MAX = env_int_any(("TEE_EDITS_PER_EPOCH_MAX", "TEE_REDACTIONS_PER_EPOCH_MAX"), 3, EDITS_PER_EPOCH_MIN)
if EDITS_PER_EPOCH_MAX < EDITS_PER_EPOCH_MIN:
    raise ValueError(
        f"TEE_EDITS_PER_EPOCH_MAX ({EDITS_PER_EPOCH_MAX}) must be >= "
        f"TEE_EDITS_PER_EPOCH_MIN ({EDITS_PER_EPOCH_MIN})"
    )
# A baseline edit verifies the cluster before the first rotation.  It is useful
# for the normal E2E flow, but experiments that need exactly one edit for each
# rotation can disable it without changing topology or committee size.
BASELINE_EDITS = env_int("TEE_BASELINE_EDITS", 1, 0)
# Quiet gap inserted before each edit's material submission (seconds). The edit
# flow itself bursts hard — authorization + 8 racing execute txs (losers revert
# on chain) + ModifyMsg flood — and back-to-back edits let one edit's burst land
# inside the next edit's validate window, inflating lu_v_chameleon spans up to
# ~3x vs the quiet-window floor (~1.9ms/op in-situ). TEE_EDIT_GAP_S>0 lets the
# pool/chain settle so ledger-update samples measure the protocol, not the
# collision with the previous edit. 0 (default) keeps historical pacing.
EDIT_GAP_S = env_float("TEE_EDIT_GAP_S", 0.0, 0.0)
# Background transaction load. A single ON node keeps sending normal txs during the
# measurement window so block density stays roughly uniform across edit and rotation flows.
LEGACY_TARGET_TXS_PER_BLOCK = env_int("TEE_TARGET_TXS_PER_BLOCK", 0, 0)
ON_ACCOUNT_COUNT = env_int("ON_ACCOUNT_COUNT", 1, 1)
ON_TX_PERS = env_float("ON_TX_PERS", 0.0, 0.0)
if ON_TX_PERS == 0.0:
    ON_TX_PERS = env_float("TEE_ON_TX_RATE", 0.0, 0.0)
if ON_TX_PERS == 0.0 and LEGACY_TARGET_TXS_PER_BLOCK > 0:
    ON_TX_PERS = LEGACY_TARGET_TXS_PER_BLOCK / EST_SECONDS_PER_BLOCK
# Background tx density per block, applied as tx.py's closed-loop --tx-per-block
# (it re-measures the live block interval and re-paces, so the achieved density
# tracks the target as block time drifts). 0 disables the closed loop. Clamped to
# 200 so the ON load cannot fill blocks past the experiment's intended band.
ON_TX_PER_BLOCK = env_float("ON_TX_PER_BLOCK", 0.0, 0.0)
if ON_TX_PER_BLOCK > 200:
    raise ValueError(f"ON_TX_PER_BLOCK must be <= 200, got {ON_TX_PER_BLOCK}")
RNG = random.Random(20260622)  # fixed seed -> reproducible 2/3 variation across intervals
ROTATION_PREP_WINDOW = 20            # contract fires RotationPreparation in last 20 blocks of an epoch
if STEADY_EPOCH_LENGTH <= ROTATION_PREP_WINDOW:
    raise ValueError(
        f"STEADY_EPOCH_LENGTH ({STEADY_EPOCH_LENGTH}) must be greater than "
        f"ROTATION_PREP_WINDOW ({ROTATION_PREP_WINDOW})"
    )
REGISTRATION_BASE_BLOCKS = env_int("TEE_REGISTRATION_BASE_BLOCKS", 48, 0)  # contract deploy + committee boot + pkCH DistKG + baseline redaction
BLOCKS_PER_REGISTERED_NODE = env_int("TEE_BLOCKS_PER_REGISTERED_NODE", 12, 1)  # ~3 txs/node (setTrustedMREnclave + stakeTEE + registerTEE) + attestation latency
SAFETY_MARGIN_BLOCKS = env_int("TEE_EPOCH_SAFETY_MARGIN_BLOCKS", 24, 0)
MIN_EPOCH_LENGTH = (REGISTRATION_BASE_BLOCKS
                    + BLOCKS_PER_REGISTERED_NODE * TEE_COUNT
                    + ROTATION_PREP_WINDOW
                    + SAFETY_MARGIN_BLOCKS)
ROTATION_WAIT_MARGIN_BLOCKS = env_int("TEE_ROTATION_WAIT_MARGIN_BLOCKS", max(60, ROTATION_PREP_WINDOW * 6), 1)
# TEE host contract-event polling interval passed to every TEE geth. 1s default
# matches the historical runs; shorter intervals cut rotation reaction latency
# (each TEE's watcher ticker phase staggers DKRS starts by up to one interval).
TEE_EVENTPOLL = os.environ.get("TEE_EVENTPOLL", "").strip() or "1s"
ROTATION_NO_PROGRESS_TIMEOUT = env_int("TEE_ROTATION_NO_PROGRESS_TIMEOUT", 600, 60)
TX_CONFIRMATIONS = env_int("TEE_TX_CONFIRMATIONS", 1, 0)
# Redaction targets must be well buried before they are edited. Editing a fresh
# block races the TxModifyMsg against the block's own propagation; a receiver
# that gets the message before the block drops it and then rejects the block
# (nonce mismatch), stalling that node. 15 successors make the block present
# everywhere before the edit is proposed.
REDACTION_CONFIRMATIONS = env_int("TEE_REDACTION_CONFIRMATIONS", 15, 0)
# Extra miners improve throughput but also make short reorgs more likely. TEE
# registration depends on the stakeTEE challenge, so wait for stable pending state.
TEE_REGISTRATION_CONFIRMATIONS = env_int(
    "TEE_REGISTRATION_CONFIRMATIONS",
    6 if EXTRA_MINER_COUNT > 0 else TX_CONFIRMATIONS,
    0,
)
TEE_REGISTRATION_RETRIES = env_int("TEE_REGISTRATION_RETRIES", 5, 1)
ZERO_BYTES32 = "0x" + ("0" * 64)
TEE_STATUS_CANDIDATE = 2
TEE_STATUS_ACTIVE = 3
DKRS_READY_MIN_EXTRACTED = env_int("TEE_DKRS_READY_MIN_EXTRACTED", ACTIVE_LIMIT, 1)
if DKRS_READY_MIN_EXTRACTED > ACTIVE_LIMIT:
    raise ValueError(
        f"TEE_DKRS_READY_MIN_EXTRACTED ({DKRS_READY_MIN_EXTRACTED}) "
        f"must be <= TEE_ACTIVE_LIMIT ({ACTIVE_LIMIT})"
    )
DKRS_READY_TIMEOUT = env_int("TEE_DKRS_READY_TIMEOUT", max(300, ACTIVE_LIMIT * 6), 0)
DKRS_READY_FALLBACK_WAIT = env_int("TEE_DKRS_READY_FALLBACK_WAIT", 0, 0)

QUOTE_HEADER_SIZE = 48
SGX_REPORT_BODY_SIZE = 384
MR_ENCLAVE_OFFSET = 64
MR_SIGNER_OFFSET = 128
ISVSVN_OFFSET = 258
REPORT_DATA_OFFSET = 320

processes: list[subprocess.Popen[Any]] = []
tx_send_lock = threading.Lock()
accounts: list[dict[str, str]] = []
on_load_accounts: list[dict[str, str]] = []
trusted_mrs: list[str] = []
rotation_epoch_start_blocks: dict[int, int] = {}
contract_address = ""
committee_pubkey = ""
load_proc: subprocess.Popen[Any] | None = None
URL_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


# 性能打点基准：monotonic 算相对耗时(ms)，datetime 时间戳与 geth/teehost 日志
# （同为 [MM-DD|HH:MM:SS.mmm] 格式、且 teehost 与 geth 同进程同日志）按时间对齐
_PERF_T0 = time.monotonic()


def log(message: str) -> None:
    ts = datetime.datetime.now().strftime("%m-%d|%H:%M:%S.%f")[:-3]
    mono_ms = int((time.monotonic() - _PERF_T0) * 1000)
    line = f"[sgx-l5] [{ts}] [+{mono_ms}ms] {message}"
    print(line, flush=True)
    # run_in_background 下 shell stdout 重定向不可靠，额外写绝对路径进度文件兜底
    try:
        with open(WORK / "e2e-progress.log", "a") as fh:
            fh.write(line + "\n")
    except OSError:
        pass


def perf_anchor(stage: str, event: str, **fields: Any) -> None:
    """Emit one machine-readable orchestration timing boundary."""
    suffix = " ".join(f"{key}={value}" for key, value in fields.items())
    message = f"PERF_ANCHOR stage={stage} event={event}"
    if suffix:
        message += " " + suffix
    log(message)


def node_rpc(index: int) -> str:
    return f"http://127.0.0.1:{18550 + index}"


def committee_rpc_url(index: int = 1) -> str:
    return f"http://127.0.0.1:{4400 + index}/rpc"


def rpc(url: str, method: str, params: Any, timeout: int = 20) -> Any:
    payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    out = json.loads(communication_stats.rpc_read(
        URL_OPENER, req, timeout, LOGS / "driver-comm.log", method).decode())
    if out.get("error"):
        raise RuntimeError(f"{method} failed: {out['error']}")
    return out.get("result")


def committee_rpc(method: str, params: Any, timeout: int = 30) -> Any:
    return rpc(committee_rpc_url(1), method, params, timeout=timeout)


def wait_for_rpc(url: str, timeout: int = 60) -> None:
    deadline = time.time() + timeout
    last_err: Exception | None = None
    while time.time() < deadline:
        try:
            rpc(url, "eth_blockNumber", [], timeout=3)
            return
        except Exception as err:
            last_err = err
            time.sleep(0.5)
    raise TimeoutError(f"RPC not ready: {url}; last error: {last_err}")


def tee_identity_is_ready(identity: Any) -> bool:
    if not isinstance(identity, dict):
        return False
    for field in ("HardwareID", "SigPubKey", "VRFPubKey", "EnclavePK"):
        value = str(identity.get(field, "")).removeprefix("0x")
        try:
            raw = bytes.fromhex(value)
        except ValueError:
            return False
        if not raw or not any(raw):
            return False
    return True


def wait_for_tee_identity(
    index: int,
    timeout: int = TEE_IDENTITY_READY_TIMEOUT,
) -> dict[str, Any]:
    deadline = time.time() + timeout
    last_state = "identity not returned"
    while time.time() < deadline:
        try:
            identity = rpc(node_rpc(index), "eth_getTEEIdentity", [], timeout=10)
            if tee_identity_is_ready(identity):
                return identity
            last_state = f"incomplete identity: {identity!r}"
        except Exception as err:
            last_state = str(err)
        time.sleep(0.5)
    raise TimeoutError(
        f"TEE identity not ready for node{index} after {timeout}s; last state: {last_state}"
    )


def parallel_for(items: list[Any], action: Any, workers: int | None = None) -> None:
    """Run independent orchestration calls with bounded concurrency."""
    if not items:
        return
    worker_count = min(workers or ORCHESTRATION_WORKERS, len(items))
    with concurrent.futures.ThreadPoolExecutor(max_workers=worker_count) as executor:
        futures = {executor.submit(action, item): item for item in items}
        for future in concurrent.futures.as_completed(futures):
            item = futures[future]
            try:
                future.result()
            except Exception as err:
                raise RuntimeError(f"parallel orchestration failed for {item}: {err}") from err


def parse_rpc_int(value: Any) -> int:
    if isinstance(value, int):
        return value
    text = str(value)
    if text.startswith("0x"):
        return int(text, 16)
    return int(text, 10)


class TxReceiptError(RuntimeError):
    def __init__(self, tx_hash: str, receipt: dict[str, Any]) -> None:
        self.tx_hash = tx_hash
        self.receipt = receipt
        super().__init__(
            f"transaction failed: {tx_hash} "
            f"status={receipt.get('status')} block={receipt.get('blockNumber')}"
        )


def receipt_failed(receipt: dict[str, Any]) -> bool:
    status = receipt.get("status")
    if status is None:
        return False
    if isinstance(status, int):
        return status == 0
    return str(status).lower() in {"0", "0x0"}


def receipt_block_number(receipt: dict[str, Any]) -> int | None:
    block_number = receipt.get("blockNumber")
    if block_number in (None, ""):
        return None
    return parse_rpc_int(block_number)


# Poll backoff for waits whose completion time is reported as a measurement.
# A flat sleep(1) quantizes every sample to whole seconds, which hides the real
# latency whenever the underlying operation finishes in far less than a second
# (the committee threshold signature completes in ~70ms, but every sample read
# 1047-1122ms because of the 1s step).
#
# The cap matters more than the starting point for a poll whose completion time
# IS the measurement: with a 1s cap the probe times land at 4/8/14/21/32/52/88/
# 155/287/546ms, so a signature finishing at 160ms is only observed at 287ms and
# the sample carries up to ~250ms of pure quantization error. A 10ms cap puts the
# probes ~14ms apart, bounding that error while still costing under 100 RPCs for
# a second-long wait.
POLL_INITIAL_BACKOFF_S = 0.001
POLL_MAX_BACKOFF_S = 1.0
# Cap for polls that feed a reported latency sample (see poll_backoff),
# as opposed to polls that merely wait for a long-running condition.
POLL_MEASURED_MAX_BACKOFF_S = 0.010


def poll_backoff(attempt: int, cap_s: float = POLL_MAX_BACKOFF_S) -> float:
    """Delay before poll attempt `attempt` (0-based): 1ms doubling, capped at cap_s.

    The exponent is clamped before evaluation: a long wait (the harness polls
    for minutes) makes `2 ** attempt` an arbitrarily large int, and the
    min(int, float) comparison then raises OverflowError converting it. Once
    the delay exceeds the cap the value is constant anyway.
    """
    if POLL_INITIAL_BACKOFF_S >= cap_s:
        return cap_s
    # Smallest attempt whose delay already reaches the cap.
    max_useful = int(math.log2(cap_s / POLL_INITIAL_BACKOFF_S)) + 1
    if attempt >= max_useful:
        return cap_s
    return min(POLL_INITIAL_BACKOFF_S * (2 ** attempt), cap_s)


def wait_for_receipt(
    url: str,
    tx_hash: str,
    timeout: int = 90,
    confirmations: int = TX_CONFIRMATIONS,
    require_success: bool = True,
) -> dict[str, Any]:
    deadline = time.time() + timeout
    last_state = "not seen"
    while time.time() < deadline:
        receipt = rpc(url, "eth_getTransactionReceipt", [tx_hash], timeout=20)
        if receipt:
            block_number = receipt_block_number(receipt)
            if confirmations > 0:
                if block_number is None:
                    last_state = f"status={receipt.get('status')} block=None"
                    time.sleep(1)
                    continue
                head = current_block()
                seen_confirmations = max(0, head - block_number + 1)
                last_state = (
                    f"status={receipt.get('status')} block={block_number} "
                    f"head={head} confirmations={seen_confirmations}/{confirmations}"
                )
                if seen_confirmations < confirmations:
                    time.sleep(1)
                    continue
            if require_success and receipt_failed(receipt):
                raise TxReceiptError(tx_hash, receipt)
            return receipt
        time.sleep(1)
    raise TimeoutError(f"receipt timeout: {tx_hash}; {last_state}")


def wait_redaction_authorized(req_id: str, tx_hash: str = "", timeout: int = 90) -> None:
    deadline = time.time() + timeout
    attempt = 0
    while time.time() < deadline:
        if tx_hash:
            receipt = rpc(node_rpc(1), "eth_getTransactionReceipt", [tx_hash], timeout=5)
            if receipt:
                return
        try:
            record = rpc(node_rpc(1), "eth_getRedactRequest", [req_id], timeout=10)
            status = int(str(record.get("Status", 0)))
            authorized_at = int(str(record.get("AuthorizedAt", 0)))
            executed_at = int(str(record.get("ExecutedAt", 0)))
            if status in (1, 2) or authorized_at > 0 or executed_at > 0:
                return
        except Exception:
            pass
        try:
            if rpc(node_rpc(1), "eth_isRedactionExecuted", [req_id], timeout=10):
                return
        except Exception:
            pass
        time.sleep(poll_backoff(attempt))
        attempt += 1
    if tx_hash:
        raise TimeoutError(f"authorization visibility timeout: req={req_id} tx={tx_hash}")
    raise TimeoutError(f"authorization visibility timeout: req={req_id}")


def cleanup() -> None:
    teardown_netem()
    stop_background_load()
    live = [proc for proc in reversed(processes) if proc.poll() is None]
    for proc in live:
        try:
            proc.terminate()
        except ProcessLookupError:
            pass
    deadline = time.time() + 8
    while live and time.time() < deadline:
        live = [proc for proc in live if proc.poll() is None]
        if live:
            time.sleep(0.1)
    for proc in live:
        try:
            proc.kill()
        except ProcessLookupError:
            pass
    for proc in live:
        if proc.poll() is None:
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                pass


def _run_tc(args: list[str], *, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["tc", *args],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=check,
    )


def _netem_active() -> bool:
    try:
        result = _run_tc(["qdisc", "show", "dev", NETEM_INTERFACE], check=False)
    except FileNotFoundError:
        return False
    return "netem" in (result.stdout or "")


def apply_netem() -> None:
    """Install a tc/netem root qdisc to add real, non-throttling network delay.

    No-op when NETEM_DELAY_MS<=0. Idempotent: replaces any existing root qdisc.
    Delays all traffic on NETEM_INTERFACE (default lo) for the duration of the run.
    """
    if NETEM_DELAY_MS <= 0:
        return
    if os.geteuid() != 0:
        raise RuntimeError(
            f"NETEM_DELAY_MS={NETEM_DELAY_MS} requires root (CAP_NET_ADMIN) to run tc; "
            "either run the harness as root or unset NETEM_DELAY_MS"
        )
    parts = [
        "qdisc", "replace", "dev", NETEM_INTERFACE, "root", "netem",
        "limit", str(NETEM_LIMIT),
        "delay", f"{NETEM_DELAY_MS}ms",
    ]
    if NETEM_JITTER_MS > 0:
        parts.append(f"{NETEM_JITTER_MS}ms")
    if NETEM_LOSS_PERCENT > 0:
        parts += ["loss", f"{NETEM_LOSS_PERCENT:g}%"]
    if NETEM_RATE:
        parts += ["rate", NETEM_RATE]
    try:
        _run_tc(parts, check=True)
    except FileNotFoundError as err:
        raise RuntimeError("tc binary not found; install iproute2 to use netem") from err
    except subprocess.CalledProcessError as err:
        raise RuntimeError(
            f"failed to apply netem ({shlex.join(parts)}): {(err.output or '').strip()}"
        ) from err
    log(
        f"netem enabled on {NETEM_INTERFACE}: delay={NETEM_DELAY_MS}ms "
        f"jitter={NETEM_JITTER_MS}ms loss={NETEM_LOSS_PERCENT:g}% "
        f"rate={NETEM_RATE or 'unlimited'} limit={NETEM_LIMIT}; "
        f"delays all traffic on the interface"
    )
    try:
        log(
            "tc qdisc: "
            + _run_tc(["qdisc", "show", "dev", NETEM_INTERFACE], check=False).stdout.strip()
        )
    except FileNotFoundError:
        pass


def teardown_netem() -> None:
    """Remove the netem root qdisc if present. Safe no-op otherwise."""
    if NETEM_DELAY_MS <= 0 or not _netem_active():
        return
    try:
        _run_tc(["qdisc", "del", "dev", NETEM_INTERFACE, "root"], check=False)
        log(f"netem removed from {NETEM_INTERFACE}")
    except FileNotFoundError:
        pass


def prepare() -> None:
    global accounts, on_load_accounts, trusted_mrs
    cleanup()
    if WORK.exists():
        shutil.rmtree(WORK)
    for path in (CHAIN, NODE_WORK, COMMITTEE_CONFIGS, LOGS, WORK / "bin"):
        path.mkdir(parents=True, exist_ok=True)
    if communication_stats.enabled():
        expected = [f"geth-node-{i}.log" for i in range(1, TOTAL_NODE_COUNT + 1)]
        expected += [f"committee-node-{i}.log" for i in range(1, COMMITTEE_COUNT + 1)]
        (LOGS / "communication-expected.json").write_text(
            json.dumps({"expected_sources": expected}, indent=2) + "\n")
    perf_anchor("system_init", "chain_init_begin")
    if not GETH_BIN.exists():
        raise FileNotFoundError(f"missing {GETH_BIN}; set GETH_BIN or place geth-sgx under {PACKAGE_ROOT / 'bin'}")
    if not COMMITTEE_BIN.exists() and not COMMITTEE_SRC_DIR.exists():
        raise FileNotFoundError(
            f"missing {COMMITTEE_BIN}; set COMMITTEE_BIN or place committee-mvp under {PACKAGE_ROOT / 'bin'}"
        )
    if not GENESIS.exists():
        raise FileNotFoundError(GENESIS)
    if not HELPER_JSON.exists():
        raise FileNotFoundError(HELPER_JSON)
    if not SGX_ENCLAVE_PATH.exists():
        raise FileNotFoundError(SGX_ENCLAVE_PATH)

    # This legacy Ethash implementation uses the same deterministic temporary
    # filename in every process. Pre-create the current and look-ahead caches so
    # hundreds of nodes can safely mmap the shared files instead of racing to
    # truncate one another's temporary file.
    ETHASH_CACHE_DIR.mkdir(parents=True, exist_ok=True)
    for block in (0, 30_000):
        subprocess.run(
            [str(GETH_BIN), "makecache", str(block), str(ETHASH_CACHE_DIR)],
            cwd=str(GETH_DIR),
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.STDOUT,
        )
    log(f"prewarmed shared Ethash caches under {ETHASH_CACHE_DIR}")

    accounts = []
    on_load_accounts = []
    trusted_mrs = []
    for i in range(1, TOTAL_NODE_COUNT + 1):
        acct = Account.create(f"sgx-l5-{i}-{time.time_ns()}")
        accounts.append({"address": acct.address, "privateKey": acct.key.hex().removeprefix("0x")})
    for i in range(1, ON_ACCOUNT_COUNT + 1):
        acct = Account.create(f"sgx-l5-on-load-{i}-{time.time_ns()}")
        on_load_accounts.append({
            "address": acct.address,
            "privateKey": acct.key.hex().removeprefix("0x"),
            "role": "on-load",
        })

    genesis = json.loads(GENESIS.read_text())
    genesis["alloc"] = {
        acct["address"].removeprefix("0x").lower(): {"balance": "1000000000000000000000"}
        for acct in accounts + on_load_accounts
    }
    (WORK / "genesis.json").write_text(json.dumps(genesis, indent=2))
    (WORK / "accounts.json").write_text(json.dumps(accounts, indent=2))
    (WORK / "on-load-accounts.json").write_text(json.dumps(on_load_accounts, indent=2))
    (WORK / "on-load-receivers.json").write_text(json.dumps([
        accounts[i - 1]["address"] for i in TEE_NODE_INDICES
    ], indent=2))

    def init_datadir(i: int) -> None:
        datadir = CHAIN / f"node{i}"
        datadir.mkdir(parents=True, exist_ok=True)
        subprocess.run([str(GETH_BIN), "--datadir", str(datadir), "init", str(WORK / "genesis.json")],
                       cwd=str(GETH_DIR), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
    parallel_for(list(range(1, TOTAL_NODE_COUNT + 1)), init_datadir)

    # keystore 账户导入前置到 init 阶段并全并行：scrypt 加密每次 ~1.4s，
    # 若留在 start_geth_node 内会串行垫进 node_startup 计时窗口（18 次 ≈ 26s）。
    # 每个账户一个任务（含 ON 的 8 个负载账户），27 任务 × 32 worker → 全平面并行。
    def run_import(datadir: Path, workdir: Path, filename: str, pk: str) -> None:
        key_file = workdir / filename
        key_file.write_text(pk + "\n")
        subprocess.run([
            str(GETH_BIN), "--datadir", str(datadir), "account", "import",
            "--password", str(workdir / "password.txt"), str(key_file),
        ], cwd=str(GETH_DIR), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)

    import_tasks: list[tuple[int, str, str]] = []  # (node_index, filename, private_key)
    for i in range(1, TOTAL_NODE_COUNT + 1):
        if i in LIGHT_NODE_INDICES:
            continue
        workdir = NODE_WORK / f"node{i}"
        workdir.mkdir(parents=True, exist_ok=True)
        (workdir / "password.txt").write_text(PASSWORD + "\n")
        import_tasks.append((i, "privatekey.txt", accounts[i - 1]["privateKey"]))
        if i == ON_INDEX:
            for load_index, load_acct in enumerate(on_load_accounts, start=1):
                import_tasks.append((i, f"on-load-privatekey-{load_index}.txt", load_acct["privateKey"]))

    def import_one(task: tuple[int, str, str]) -> None:
        node_index, filename, pk = task
        run_import(CHAIN / f"node{node_index}", NODE_WORK / f"node{node_index}", filename, pk)

    parallel_for(import_tasks, import_one)
    log(
        f"prepared {TOTAL_NODE_COUNT} accounts and datadirs under {WORK} "
        f"(core={CORE_NODE_COUNT}, light={LIGHT_NODE_COUNT}); "
        f"keystores pre-imported in parallel: {len(import_tasks)} accounts"
    )


def start_geth_node(
    index: int,
    contract: str | None = None,
    teehost: bool = True,
    lightweight: bool = False,
) -> None:
    acct = accounts[index - 1]
    datadir = CHAIN / f"node{index}"
    workdir = NODE_WORK / f"node{index}"
    workdir.mkdir(parents=True, exist_ok=True)
    (workdir / "privatekey.txt").write_text(acct["privateKey"] + "\n")
    (workdir / "password.txt").write_text(PASSWORD + "\n")
    shutil.copyfile(HELPER_JSON, workdir / "helper.json")
    (workdir / "storage.json").write_text(json.dumps({
        "address": contract or "",
        "privateKey": acct["privateKey"],
        "port": str(18550 + index),
    }, indent=2))
    unlock_addresses = [acct["address"]]
    if not lightweight:
        # 节点账户已在 prepare() 阶段并行导入 keystore（import_keystore），
        # 这里只补写 unlock 用的多行 password 文件，不再串行 import。
        if index == ON_INDEX:
            unlock_addresses.extend(acct["address"] for acct in on_load_accounts)
        if len(unlock_addresses) > 1:
            (workdir / "password.txt").write_text((PASSWORD + "\n") * len(unlock_addresses))

    args = [
        str(GETH_BIN),
        "--datadir", str(datadir),
        "--networkid", NETWORK_ID,
        "--port", str(30310 + index),
        "--rpc", "--rpcport", str(18550 + index), "--rpcaddr", "127.0.0.1",
        "--nodiscover",
        "--maxpeers", str(GETH_MAX_PEERS),
        "--p2p.msg-delay-ms", str(GETH_P2P_MSG_DELAY_MS),
        "--p2p.block-broadcast-limit", str(
            GETH_BLOCK_BROADCAST_LIMIT_TEE
            if GETH_BLOCK_BROADCAST_LIMIT_TEE >= 0 and index in TEE_NODE_INDICES
            else GETH_BLOCK_BROADCAST_LIMIT
        ),
        "--etherbase", acct["address"],
        "--tee.distkg-relay-ttl", str(DISTKG_RELAY_TTL),
        "--tee.dkrs-relay-ttl", str(DKRS_RELAY_TTL),
        "--txmodify.relay-ttl", str(TXMODIFY_RELAY_TTL),
        "--ethash.cachedir", str(ETHASH_CACHE_DIR),
        "--cache", str(LIGHT_NODE_CACHE_MB if lightweight else CORE_NODE_CACHE_MB),
        "--verbosity", str(max(3 if communication_stats.enabled() else 0,
                                LIGHT_NODE_VERBOSITY if lightweight else 4)),
    ]
    if lightweight:
        args += [
            "--rpcapi", "eth,net,admin",
            "--ipcdisable",
            "--nousb",
            "--syncmode", "full",
            "--cache.noprefetch",
        ]
    else:
        args += [
            "--rpcapi", "eth,net,web3,personal,admin,debug,miner,txpool",
            "--ipcpath", str(datadir / "geth.ipc"),
            "--allow-insecure-unlock",
            "--unlock", ",".join(unlock_addresses),
            "--password", str(workdir / "password.txt"),
        ]
    if teehost:
        args += ["--teehost", "--tee.eventpoll", TEE_EVENTPOLL, "--tee.confirmations", "0"]
        if os.environ.get("TEE_HEADWATCH") == "1":
            args += ["--tee.headwatch"]
    if index in MINER_NODE_INDICES:
        args += ["--mine", "--minerthreads", "1"]
    env = os.environ.copy()
    env["TGOVERN_COMM_NODE"] = f"geth-node-{index}"
    if index == ON_INDEX:
        env["E2E_TIMING_NODE_ROLE"] = "on"
    elif index in ON_OBSERVER_INDICES:
        # 观察者与 ON 同角色：emitLedgerTiming 在此节点也发射，事后按 node_id/日志文件分层
        env["E2E_TIMING_NODE_ROLE"] = "on"
    elif teehost:
        env["E2E_TIMING_NODE_ROLE"] = "tee"
    elif lightweight:
        env["E2E_TIMING_NODE_ROLE"] = "light"
    else:
        env["E2E_TIMING_NODE_ROLE"] = "ordinary"
    env["SGX_SDK"] = env.get("SGX_SDK", "/opt/intel/sgxsdk")
    sgx_mode = env.get("SGX_MODE", SGX_MODE).upper()
    env["SGX_MODE"] = sgx_mode
    if sgx_mode != "HW":
        env["SGX_SIM_ID_SEED"] = f"sgx-l5-node-{index}"
    env["EPOCH_LENGTH"] = str(EPOCH_LENGTH)
    env["STEADY_EPOCH_LENGTH"] = str(STEADY_EPOCH_LENGTH)
    env["TEE_ACTIVE_LIMIT"] = str(ACTIVE_LIMIT)
    env["TEE_ACTIVE_THRESHOLD"] = str(ACTIVE_THRESHOLD)
    if lightweight:
        env["GOMAXPROCS"] = str(LIGHT_NODE_GOMAXPROCS)
    else:
        env["GOMAXPROCS"] = str(CORE_NODE_GOMAXPROCS)
        if index == GOMAX_OVERRIDE_NODE:
            env["GOMAXPROCS"] = str(GOMAX_OVERRIDE_PROCS)
        if index == GOGC_OVERRIDE_NODE:
            env["GOGC"] = str(GOGC_OVERRIDE_VALUE)
    if teehost:
        env["SGX_ENCLAVE_PATH"] = str(SGX_ENCLAVE_PATH)
    sgx_sdk = env["SGX_SDK"]
    ld_entries = [
        str(SGX_MODULE_DIR),
        f"{sgx_sdk}/sdk_libs",
        f"{sgx_sdk}/lib64",
        env.get("LD_LIBRARY_PATH", ""),
    ]
    env["LD_LIBRARY_PATH"] = ":".join(entry for entry in ld_entries if entry)
    out = open(LOGS / f"geth-node-{index}.log", "w")
    if TEE_CORE_ISOLATION:
        # 三组 CPU 归组：观察节点(记录 ledger_update)→observer 核；
        # 16 个 TEE host→iso 核；node1 proposer / light / tx→other 核。
        cores = (OBSERVER_CORE_CORES if (ON_OBSERVER_INDICES and index in ON_OBSERVER_INDICES)
                 else TEE_CORE_ISO_CORES if (index == ON_INDEX or index in TEE_NODE_INDICES)
                 else TEE_CORE_OTHER_CORES)
        args = ["taskset", "-c", cores] + args
    proc = subprocess.Popen(args, cwd=str(workdir), env=env, stdout=out, stderr=subprocess.STDOUT)
    processes.append(proc)


def start_nodes() -> None:
    global contract_address
    log(f"starting geth-sgx node{ON_INDEX} ON miner")
    start_geth_node(ON_INDEX, teehost=False)
    wait_for_rpc(node_rpc(1))
    storage = NODE_WORK / "node1" / "storage.json"
    deadline = time.time() + 240
    while time.time() < deadline:
        data = json.loads(storage.read_text())
        addr = data.get("address", "")
        if addr:
            code = rpc(node_rpc(1), "eth_getCode", [addr, "latest"], timeout=5)
            if code and code != "0x":
                contract_address = addr
                break
        time.sleep(1)
    if not contract_address:
        raise TimeoutError("contract deployment timeout")
    log(f"contract deployed at {contract_address}")
    perf_anchor("system_init", "contract_deployed", contract=contract_address)
    perf_anchor("system_init", "node_startup_begin")
    # committee 只依赖 node1 RPC + 合约地址 + node1 IPC，此刻均已就绪：
    # 后台启动，与下方 TEE/轻节点启动并行。
    launch_committee_background()

    extra_miners = [i for i in MINER_NODE_INDICES if i != ON_INDEX]
    if extra_miners:
        log(f"starting extra miner nodes: {extra_miners}")
    for i in extra_miners:
        start_geth_node(i, contract_address, teehost=False)
    for i in extra_miners:
        wait_for_rpc(node_rpc(i))

    if ON_OBSERVER_INDICES:
        log(f"starting ON observer nodes: {ON_OBSERVER_INDICES}")
        for i in ON_OBSERVER_INDICES:
            start_geth_node(i, contract_address, teehost=False)
        for i in ON_OBSERVER_INDICES:
            wait_for_rpc(node_rpc(i))
    perf_anchor("topology", "node_roles",
                on=ON_INDEX,
                on_observers=",".join(str(i) for i in ON_OBSERVER_INDICES),
                miners=",".join(str(i) for i in MINER_NODE_INDICES))

    for i in TEE_NODE_INDICES:
        start_geth_node(i, contract_address)
    parallel_for(TEE_NODE_INDICES, lambda i: wait_for_rpc(node_rpc(i)))
    parallel_for(TEE_NODE_INDICES, wait_for_tee_identity)
    log(f"all {TEE_COUNT} TEE host identities ready")

    if LIGHT_NODE_INDICES:
        log(
            f"starting {LIGHT_NODE_COUNT} lightweight ordinary nodes "
            f"in batches of {LIGHT_NODE_START_BATCH} "
            f"(cache={LIGHT_NODE_CACHE_MB}MiB, GOMAXPROCS={LIGHT_NODE_GOMAXPROCS})"
        )
    for offset in range(0, LIGHT_NODE_COUNT, LIGHT_NODE_START_BATCH):
        batch = LIGHT_NODE_INDICES[offset:offset + LIGHT_NODE_START_BATCH]
        for i in batch:
            start_geth_node(i, contract_address, teehost=False, lightweight=True)
        parallel_for(batch, lambda i: wait_for_rpc(node_rpc(i), timeout=120))
        log(f"lightweight nodes RPC ready: {offset + 1}-{offset + len(batch)}/{LIGHT_NODE_COUNT}")

    enodes: dict[int, str] = {}

    def load_enode(i: int) -> None:
        info = rpc(node_rpc(i), "admin_nodeInfo", [])
        enodes[i] = force_local_enode(info["enode"], 30310 + i)

    parallel_for(list(range(1, TOTAL_NODE_COUNT + 1)), load_enode)

    def add_topology_edge(edge: tuple[int, int]) -> None:
        source, target = edge
        added = rpc(node_rpc(source), "admin_addPeer", [enodes[target]], timeout=20)
        if added is False:
            raise RuntimeError(f"admin_addPeer returned false for node{source}->node{target}")

    log(
        f"connecting P2P topology={P2P_TOPOLOGY} edges={len(TOPOLOGY_EDGES)} "
        f"maxDegree={MAX_TOPOLOGY_DEGREE} maxOutbound={MAX_TOPOLOGY_OUTBOUND} "
        f"diameterBound={P2P_DIAMETER_BOUND}"
    )
    parallel_for(TOPOLOGY_EDGES, add_topology_edge)
    deadline = time.time() + P2P_CONNECT_TIMEOUT
    peer_counts: dict[int, int] = {}
    while time.time() < deadline:
        def load_peer_count(i: int) -> None:
            peer_counts[i] = len(rpc(node_rpc(i), "admin_peers", [], timeout=5))

        peer_counts.clear()
        parallel_for(list(range(1, TOTAL_NODE_COUNT + 1)), load_peer_count)
        deficits = {
            i: (peer_counts.get(i, 0), len(EXPECTED_PEERS[i]))
            for i in range(1, TOTAL_NODE_COUNT + 1)
            if peer_counts.get(i, 0) < len(EXPECTED_PEERS[i])
        }
        if not deficits:
            break
        time.sleep(1)
    else:
        sample = list(deficits.items())[:20]
        raise TimeoutError(
            f"sparse peer topology timeout after {P2P_CONNECT_TIMEOUT}s: "
            f"deficits={len(deficits)}/{TOTAL_NODE_COUNT}, sample={sample}"
        )
    log(
        f"geth-sgx nodes connected (node{ON_INDEX} ON/miner + "
        f"{EXTRA_MINER_COUNT} extra miner(s) + {TEE_COUNT} TEE + "
        f"{LIGHT_NODE_COUNT} light ordinary; edges={len(TOPOLOGY_EDGES)})"
    )
    perf_anchor("system_init", "node_startup_complete", nodes=TOTAL_NODE_COUNT)


def force_local_enode(enode: str, port: int) -> str:
    prefix = enode.split("@", 1)[0]
    return f"{prefix}@127.0.0.1:{port}"


def write_committee_configs() -> None:
    start_block = int(rpc(node_rpc(1), "eth_blockNumber", []), 16)
    for i in range(1, COMMITTEE_COUNT + 1):
        cfg = {
            "node_id": f"node-{i}",
            "listen_addr": f"127.0.0.1:{3400 + i}",
            "static_nodes": [f"node-{j}" for j in range(1, COMMITTEE_COUNT + 1)],
            "static_node_addrs": {f"node-{j}": f"127.0.0.1:{3400 + j}" for j in range(1, COMMITTEE_COUNT + 1)},
            "control_addr": f"127.0.0.1:{4400 + i}",
            "chain_rpc_mode": "ipc",
            "geth_ipc_path": str(CHAIN / "node1" / "geth.ipc"),
            "vrf_contract_address": contract_address,
            "vrf_submitter_address": accounts[0]["address"],
            "vrf_start_block": start_block,
            "committee_size": COMMITTEE_COUNT,
            "threshold": COMMITTEE_THRESHOLD,
            "coordinator_id": "node-1",
            "domain_separation": "committee-sig/mvp/v1",
            "message_version": "v1",
            "attestation_provider": ATTESTATION_PROVIDER,
        }
        if ATTESTATION_PROVIDER == "azure":
            cfg["azure_attestation_endpoint"] = env_text("AZURE_ATTESTATION_ENDPOINT")
            cfg["azure_attestation_api_key"] = env_text("AZURE_ATTESTATION_API_KEY")
            cfg["azure_attestation_api_version"] = env_text("AZURE_ATTESTATION_API_VERSION")
            timeout = env_text("AZURE_ATTESTATION_TIMEOUT_MILLIS")
            if timeout:
                cfg["azure_attestation_timeout_millis"] = int(timeout)
        elif ATTESTATION_PROVIDER == "aliyun":
            command = env_text("ALIYUN_QUOTE_VERIFY_COMMAND")
            if command:
                cfg["aliyun_quote_verify_command"] = command
            args = env_list("ALIYUN_QUOTE_VERIFY_ARGS")
            if args:
                cfg["aliyun_quote_verify_args"] = args
            timeout = env_text("ALIYUN_QUOTE_VERIFY_TIMEOUT_MILLIS")
            if timeout:
                cfg["aliyun_quote_verify_timeout_millis"] = int(timeout)
        (COMMITTEE_CONFIGS / f"node-{i}.json").write_text(json.dumps(cfg, indent=2))


def start_committee_nodes() -> None:
    if not COMMITTEE_BIN.exists():
        log("building committee-mvp")
        if not COMMITTEE_SRC_DIR.exists():
            raise FileNotFoundError(
                f"missing committee source dir {COMMITTEE_SRC_DIR}; set COMMITTEE_BIN to a packaged binary"
            )
        subprocess.run(["go", "build", "-o", str(COMMITTEE_BIN), "./cmd/committee-mvp"],
                       cwd=str(COMMITTEE_SRC_DIR), check=True)
    write_committee_configs()
    for i in range(1, COMMITTEE_COUNT + 1):
        out = open(LOGS / f"committee-node-{i}.log", "w")
        cmd = [str(COMMITTEE_BIN), "node", "--config", str(COMMITTEE_CONFIGS / f"node-{i}.json")]
        if TEE_CORE_ISOLATION:
            cmd = ["taskset", "-c", TEE_CORE_OTHER_CORES] + cmd
        env = {**os.environ, "TGOVERN_COMM_NODE": f"committee-node-{i}"}
        proc = subprocess.Popen(cmd, cwd=str(COMMITTEE_CWD), env=env,
                                stdout=out, stderr=subprocess.STDOUT)
        processes.append(proc)
    deadline = time.time() + 60
    while time.time() < deadline:
        try:
            committee_rpc("committee_getPublicKey", {}, timeout=5)
            log("committee RPC ready")
            return
        except Exception:
            time.sleep(1)
    raise TimeoutError("committee RPC not ready")


# committee 后台启动：合约部署完成后（node_startup_begin 时刻）即可开跑，
# 与 TEE/轻节点启动并行；线程内完成 configs 写盘、进程拉起、RPC/公钥就绪等待。
_committee_thread: threading.Thread | None = None


def launch_committee_background() -> None:
    """在后台线程启动 committee（在 start_nodes 的 node_startup_begin 处调用）。"""
    global _committee_thread

    def runner() -> None:
        start_committee_nodes()
        log("committee background startup: RPC ready")

    _committee_thread = threading.Thread(target=runner, name="committee-boot", daemon=True)
    _committee_thread.start()
    log("committee nodes launching in background (parallel with geth node startup)")


def join_committee_background() -> None:
    """等待后台 committee 启动完成（在进入 tee_registration 前调用）。"""
    if _committee_thread is None:
        # 兼容旧调用路径：未走后台启动时在此串行启动
        start_committee_nodes()
        return
    _committee_thread.join()
    log("committee background startup joined")


def wait_committee_pubkey() -> str:
    deadline = time.time() + 90
    while time.time() < deadline:
        result = committee_rpc("committee_getPublicKey", {}, timeout=5)
        pk = str(result.get("committee_pub_key", "")).removeprefix("0x")
        if len(pk) == 256:
            return "0x" + pk
        time.sleep(1)
    raise TimeoutError("committee public key timeout")


def register_committee_pubkey() -> None:
    global committee_pubkey
    committee_pubkey = wait_committee_pubkey()
    tx = rpc(node_rpc(1), "eth_registerCommitteePubKey", [committee_pubkey], timeout=20)
    if not isinstance(tx, str) or not tx.startswith("0x"):
        raise RuntimeError(f"eth_registerCommitteePubKey returned invalid tx hash: {tx!r}")
    wait_for_receipt(
        node_rpc(1),
        tx,
        timeout=180,
        confirmations=TEE_REGISTRATION_CONFIRMATIONS,
    )
    log(f"committee pubkey registered: {committee_pubkey[:42]}...")


def build_quote(identity: dict[str, str], index: int) -> tuple[str, str, dict[str, str]]:
    hardware = bytes.fromhex(identity["HardwareID"].removeprefix("0x"))
    sig_pub = bytes.fromhex(identity["SigPubKey"].removeprefix("0x"))
    vrf_pub = bytes.fromhex(identity["VRFPubKey"].removeprefix("0x"))
    enclave_pk = bytes.fromhex(identity["EnclavePK"].removeprefix("0x"))
    committee = bytes.fromhex(committee_pubkey.removeprefix("0x"))
    if len(enclave_pk) != 64 or not any(enclave_pk):
        raise ValueError(f"invalid EnclavePK from eth_getTEEIdentity for node{index}")
    mr = hashlib.sha256(f"sgx-l5-mrenclave-{index}".encode()).digest()
    signer = hashlib.sha256(f"sgx-l5-mrsigner-{index}".encode()).digest()
    report_hash = hashlib.sha256(hardware + committee + sig_pub + vrf_pub + enclave_pk).digest()
    quote = bytearray(QUOTE_HEADER_SIZE + SGX_REPORT_BODY_SIZE)
    base = QUOTE_HEADER_SIZE
    quote[base + MR_ENCLAVE_OFFSET:base + MR_ENCLAVE_OFFSET + 32] = mr
    quote[base + MR_SIGNER_OFFSET:base + MR_SIGNER_OFFSET + 32] = signer
    quote[base + ISVSVN_OFFSET] = 9
    quote[base + REPORT_DATA_OFFSET:base + REPORT_DATA_OFFSET + 32] = report_hash
    report_data = {
        "hardware_id": "0x" + hardware.hex(),
        "committee_pub_key": committee_pubkey,
        "sig_pub_key": "0x" + sig_pub.hex(),
        "vrf_pub_key": "0x" + vrf_pub.hex(),
        "enclave_pk": "0x" + enclave_pk.hex(),
    }
    return "0x" + quote.hex(), "0x" + mr.hex(), report_data


def parse_mrenclave_from_quote(quote_hex: str) -> str:
    raw = bytes.fromhex(str(quote_hex).removeprefix("0x"))
    min_len = QUOTE_HEADER_SIZE + MR_ENCLAVE_OFFSET + 32
    if len(raw) < min_len:
        raise ValueError(f"SGX quote too short: got {len(raw)} bytes, need at least {min_len}")
    base = QUOTE_HEADER_SIZE
    return "0x" + raw[base + MR_ENCLAVE_OFFSET:base + MR_ENCLAVE_OFFSET + 32].hex()


def report_data_from_quote_result(result: dict[str, Any]) -> dict[str, str]:
    required = ("HardwareID", "CommitteePubKey", "SigPubKey", "VRFPubKey", "EnclavePK")
    missing = [name for name in required if not result.get(name)]
    if missing:
        raise ValueError(f"eth_getTEEQuote missing fields: {missing}")
    if str(result["CommitteePubKey"]).lower() != committee_pubkey.lower():
        raise ValueError("eth_getTEEQuote returned committee public key different from registered committee key")
    return {
        "hardware_id": str(result["HardwareID"]),
        "committee_pub_key": str(result["CommitteePubKey"]),
        "sig_pub_key": str(result["SigPubKey"]),
        "vrf_pub_key": str(result["VRFPubKey"]),
        "enclave_pk": str(result["EnclavePK"]),
    }


def get_tee_quote(url: str, node_index: int) -> tuple[str, str, dict[str, str]]:
    result = rpc(url, "eth_getTEEQuote", [committee_pubkey], timeout=90)
    if not isinstance(result, dict) or not result.get("Quote"):
        raise RuntimeError(f"eth_getTEEQuote returned invalid result for node{node_index}: {result!r}")
    quote = str(result["Quote"])
    mr = parse_mrenclave_from_quote(quote)
    report_data = report_data_from_quote_result(result)
    return quote, mr, report_data


def get_tee_attestation(url: str, node_index: int) -> tuple[str, str, dict[str, str]]:
    if TEE_QUOTE_MODE == "mock":
        identity = wait_for_tee_identity(node_index)
        return build_quote(identity, node_index)
    try:
        return get_tee_quote(url, node_index)
    except Exception:
        if TEE_QUOTE_MODE != "auto":
            raise
        log(f"eth_getTEEQuote failed for node{node_index}; falling back to mock quote")
        identity = wait_for_tee_identity(node_index)
        return build_quote(identity, node_index)


def rpc_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        return value != 0
    return str(value).strip().lower() in {"1", "0x1", "true", "yes"}


def pending_tee_exists(pending: Any) -> bool:
    return isinstance(pending, dict) and rpc_bool(pending.get("Exists", False))


def tee_owner_status(owner: str) -> tuple[int, str]:
    status = rpc(node_rpc(1), "eth_getTEEStatusByOwner", [owner], timeout=10)
    if not isinstance(status, dict):
        return 0, ""
    # Older geth API names the returned registered sigPubKey as HardwareId.
    registered_sig = str(status.get("SigPubKey") or status.get("HardwareId") or "").lower()
    return parse_rpc_int(status.get("Status", 0)), registered_sig


def tee_is_registered(owner: str, sig_pub: str) -> bool:
    status, registered_sig = tee_owner_status(owner)
    if status not in (TEE_STATUS_CANDIDATE, TEE_STATUS_ACTIVE):
        return False
    expected_sig = sig_pub.lower()
    if registered_sig not in ("", ZERO_BYTES32) and registered_sig != expected_sig:
        raise RuntimeError(
            f"owner {owner} registered different sigPubKey: got {registered_sig}, expected {expected_sig}"
        )
    return True


def wait_for_pending_tee(owner: str, timeout: int = 45) -> dict[str, Any]:
    deadline = time.time() + timeout
    last_pending: Any = None
    while time.time() < deadline:
        pending = rpc(node_rpc(1), "eth_getPendingTEE", [owner], timeout=10)
        last_pending = pending
        if pending_tee_exists(pending):
            return pending
        time.sleep(1)
    raise TimeoutError(f"pending TEE not visible for owner {owner}: {last_pending}")


def ensure_pending_tee(url: str, owner: str) -> dict[str, Any]:
    pending = rpc(node_rpc(1), "eth_getPendingTEE", [owner], timeout=10)
    if pending_tee_exists(pending):
        return pending
    stake = rpc(url, "eth_stakeTEE", [json.dumps({"Value": "1"})], timeout=20)
    if not isinstance(stake, str) or not stake.startswith("0x"):
        raise RuntimeError(f"eth_stakeTEE returned invalid tx hash: {stake!r}")
    wait_for_receipt(
        node_rpc(1),
        stake,
        timeout=180,
        confirmations=TEE_REGISTRATION_CONFIRMATIONS,
    )
    return wait_for_pending_tee(owner)


def submit_tee_registration(
    owner: str,
    quote: str,
    report_data: dict[str, str],
    pending: dict[str, Any],
) -> str:
    params: dict[str, Any] = {
        "owner_address": owner,
        "quote": quote,
        "report_data": report_data,
    }
    if pending_tee_exists(pending):
        params["pending_nonce"] = str(pending.get("PendingNonce", ""))
        params["challenge_hash"] = str(pending.get("ChallengeHash", ""))
    result = committee_rpc("committee_registerTEE", params, timeout=60)
    tx_hash = result.get("tx_hash", "") if isinstance(result, dict) else ""
    if not isinstance(tx_hash, str) or not tx_hash.startswith("0x"):
        raise RuntimeError(f"committee_registerTEE returned invalid result: {result!r}")
    return tx_hash


def register_tees() -> None:
    seen: set[str] = set()
    for tee_ord, node_index in enumerate(TEE_NODE_INDICES, start=1):
        url = node_rpc(node_index)
        owner = accounts[node_index - 1]["address"]
        tee_started_at = time.monotonic()
        material_started_at = tee_started_at
        quote, mr, report_data = get_tee_attestation(url, node_index)
        hw = report_data["hardware_id"]
        perf_anchor("tee_registration", "tee_material_produced",
                    node=f"node{node_index}", tee_index=tee_ord,
                    duration_ns=int((time.monotonic() - material_started_at) * 1e9))
        sig_pub = report_data["sig_pub_key"]
        if sig_pub in seen:
            raise RuntimeError(f"duplicate sigPubKey: {sig_pub}")
        seen.add(sig_pub)
        trusted_mrs.append(mr)

        last_err: Exception | None = None
        for attempt in range(1, TEE_REGISTRATION_RETRIES + 1):
            if tee_is_registered(owner, sig_pub):
                log(f"registered TEE node{node_index} (tee#{tee_ord}): hardware={hw} sigPubKey={sig_pub}")
                break
            try:
                tx = rpc(node_rpc(1), "eth_setTrustedMREnclave", [mr, True], timeout=20)
                if not isinstance(tx, str) or not tx.startswith("0x"):
                    raise RuntimeError(f"eth_setTrustedMREnclave returned invalid tx hash: {tx!r}")
                mr_confirmation_started_at = time.monotonic()
                wait_for_receipt(
                    node_rpc(1),
                    tx,
                    timeout=180,
                    confirmations=TEE_REGISTRATION_CONFIRMATIONS,
                )
                perf_anchor("tee_registration", "tee_mr_chain_confirmed",
                            node=f"node{node_index}", tee_index=tee_ord, attempt=attempt,
                            duration_ns=int((time.monotonic() - mr_confirmation_started_at) * 1e9))

                pending_started_at = time.monotonic()
                pending = ensure_pending_tee(url, owner)
                perf_anchor("tee_registration", "tee_pending_ready",
                            node=f"node{node_index}", tee_index=tee_ord, attempt=attempt,
                            duration_ns=int((time.monotonic() - pending_started_at) * 1e9))
                log(
                    f"registering TEE node{node_index} (tee#{tee_ord}) "
                    f"attempt {attempt}/{TEE_REGISTRATION_RETRIES}: "
                    f"pendingNonce={pending.get('PendingNonce')} "
                    f"challenge={pending.get('ChallengeHash')}"
                )
                committee_review_started_at = time.monotonic()
                tx_hash = submit_tee_registration(owner, quote, report_data, pending)
                perf_anchor("tee_registration", "tee_committee_review",
                            node=f"node{node_index}", tee_index=tee_ord, attempt=attempt,
                            duration_ns=int((time.monotonic() - committee_review_started_at) * 1e9))
                perf_anchor("tee_registration", "tee_chain_submit",
                            node=f"node{node_index}", tee_index=tee_ord, attempt=attempt,
                            tx_hash=tx_hash)
                confirmation_started_at = time.monotonic()
                wait_for_receipt(
                    node_rpc(1),
                    tx_hash,
                    timeout=180,
                    confirmations=TEE_REGISTRATION_CONFIRMATIONS,
                )
                perf_anchor("tee_registration", "tee_chain_confirmed",
                            node=f"node{node_index}", tee_index=tee_ord, attempt=attempt,
                            duration_ns=int((time.monotonic() - confirmation_started_at) * 1e9))
                if not tee_is_registered(owner, sig_pub):
                    status, registered_sig = tee_owner_status(owner)
                    raise RuntimeError(
                        f"registration tx succeeded but owner status={status} sigPubKey={registered_sig}"
                    )
                log(f"registered TEE node{node_index} (tee#{tee_ord}): hardware={hw} sigPubKey={sig_pub}")
                break
            except Exception as err:
                last_err = err
                if tee_is_registered(owner, sig_pub):
                    log(f"registered TEE node{node_index} (tee#{tee_ord}): hardware={hw} sigPubKey={sig_pub}")
                    break
                if attempt >= TEE_REGISTRATION_RETRIES:
                    raise RuntimeError(
                        f"register TEE node{node_index} failed after "
                        f"{TEE_REGISTRATION_RETRIES} attempts"
                    ) from err
                log(
                    f"TEE node{node_index} registration attempt {attempt} failed: "
                    f"{err}; retrying"
                )
                time.sleep(min(5, attempt))
        else:
            raise RuntimeError(f"register TEE node{node_index} failed: {last_err}")
        perf_anchor(
            "tee_registration",
            "tee_registered",
            node=f"node{node_index}",
            tee_index=tee_ord,
            hardware_id=hw,
            sig_pub_key=sig_pub,
            duration_ns=int((time.monotonic() - tee_started_at) * 1e9),
        )

    deadline = time.time() + 120
    last_active_count = 0
    last_candidate_count = 0
    while time.time() < deadline:
        active = rpc(node_rpc(1), "eth_getActiveTEEs", [], timeout=10)
        candidates = rpc(node_rpc(1), "eth_getCandidateTEEs", [], timeout=10)
        last_active_count = len(active) if isinstance(active, list) else -1
        last_candidate_count = len(candidates) if isinstance(candidates, list) else -1
        if isinstance(active, list) and len(active) >= ACTIVE_LIMIT:
            bootstrap_block = current_block()
            rotation_epoch_start_blocks[0] = bootstrap_block
            log(f"active set bootstrapped: {len(active)} TEEs at block {bootstrap_block}")
            perf_anchor(
                "tee_registration",
                "active_set_bootstrapped",
                active_count=len(active),
                block=bootstrap_block,
            )
            perf_anchor("system_init", "tee_pkch_begin", block=bootstrap_block)
            return
        time.sleep(2)
    raise TimeoutError(
        f"active TEE bootstrap timeout: active={last_active_count} "
        f"candidates={last_candidate_count} expected={ACTIVE_LIMIT}"
    )

def assert_system_params() -> dict[str, Any]:
    params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    epoch_len = parse_rpc_int(params["ConfiguredEpochLength"])
    active_threshold = parse_rpc_int(params["ActiveThreshold"])
    active_limit = parse_rpc_int(params["ActiveLimit"])
    if epoch_len != EPOCH_LENGTH:
        raise AssertionError(f"ConfiguredEpochLength={epoch_len}, expected {EPOCH_LENGTH}")
    if active_threshold != ACTIVE_THRESHOLD:
        raise AssertionError(f"ActiveThreshold={active_threshold}, expected {ACTIVE_THRESHOLD}")
    if active_limit != ACTIVE_LIMIT:
        raise AssertionError(f"ActiveLimit={active_limit}, expected {ACTIVE_LIMIT}")
    log(f"system params verified: epochLength={epoch_len} threshold={active_threshold} activeLimit={active_limit}")
    return params


def event_topic(signature: str) -> str:
    return "0x" + keccak(text=signature).hex()


def uint_topic(value: int) -> str:
    return "0x" + value.to_bytes(32, "big").hex()


def normalize_topic(value: str) -> str:
    text = value.lower()
    if text.startswith("0x"):
        text = text[2:]
    return "0x" + text.rjust(64, "0")


def collect_event_logs(topic0: str, from_block: int = 0, topics: list[str | None] | None = None) -> list[dict[str, Any]]:
    query_topics: list[str | None] = [topic0]
    if topics:
        query_topics.extend(topics)
    return rpc(node_rpc(1), "eth_getLogs", [{
        "fromBlock": hex(from_block),
        "toBlock": "latest",
        "address": contract_address,
        "topics": query_topics,
    }], timeout=20)


def current_block() -> int:
    return int(rpc(node_rpc(1), "eth_blockNumber", [], timeout=10), 16)


def current_epoch() -> int:
    params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    return parse_rpc_int(params["Epoch"])


def epoch_length_for_target(target_epoch: int) -> int:
    return EPOCH_LENGTH if target_epoch <= 1 else STEADY_EPOCH_LENGTH


def rotation_deadline_block_for_target(target_epoch: int) -> int | None:
    start_block = rotation_epoch_start_blocks.get(target_epoch - 1)
    if start_block is None:
        return None
    return start_block + epoch_length_for_target(target_epoch)


def snapshot_active_set() -> dict[str, Any]:
    params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    epoch = parse_rpc_int(params["Epoch"])
    info = rpc(node_rpc(1), "eth_getActiveSetInfo", [str(epoch)], timeout=10)
    members = [str(hw).lower() for hw in info.get("ActiveTEEs", [])]
    if len(members) != ACTIVE_LIMIT:
        raise AssertionError(f"active set size={len(members)}, expected {ACTIVE_LIMIT}: {members}")
    return {
        "epoch": epoch,
        "members": members,
        "active_set_hash": str(info["ActiveSetHash"]).lower(),
        "pkch_hash": str(params["PkCHHash"]).lower(),
    }


def trigger_rotation_write() -> None:
    if not trusted_mrs:
        raise RuntimeError("no trusted MR_ENCLAVE available to trigger rotation")
    tx = rpc(node_rpc(1), "eth_setTrustedMREnclave", [trusted_mrs[0], True], timeout=20)
    if isinstance(tx, str) and tx.startswith("0x"):
        try:
            wait_for_receipt(node_rpc(1), tx, timeout=30)
        except TimeoutError:
            log(f"rotation trigger receipt not visible yet: {tx}")


def wait_rotation(target_epoch: int, timeout: int = 0) -> dict[str, Any]:
    target_deadline_block = rotation_deadline_block_for_target(target_epoch)
    wall_deadline = None
    if timeout > 0:
        wall_deadline = time.time() + timeout
    elif target_deadline_block is None:
        wall_deadline = time.time() + max(360, epoch_length_for_target(target_epoch) * EST_SECONDS_PER_BLOCK + 120)
    if target_deadline_block is None:
        log(f"waiting for rotation to epoch {target_epoch}")
    else:
        log(
            f"waiting for rotation to epoch {target_epoch}: "
            f"deadlineBlock={target_deadline_block} "
            f"stopBlock={target_deadline_block + ROTATION_WAIT_MARGIN_BLOCKS}"
        )
    # Rotation preparation only fires on a contract write inside the prep window
    # [deadline_block - ROTATION_PREP_WINDOW, deadline_block), and the rotation itself executes
    # on the first write at/after the deadline. Keep nudging with a write until the rotation
    # lands; use block height, not only wall-clock time, so slow local 64-node runs do not
    # fail before the chain reaches the contract's actual deadline.
    next_trigger = 0.0
    last_block = -1
    last_progress = time.time()
    while True:
        now = time.time()
        block = current_block()
        if block != last_block:
            last_block = block
            last_progress = now
        snapshot = snapshot_active_set()
        epoch = snapshot["epoch"]
        if epoch >= target_epoch:
            if epoch != target_epoch:
                raise AssertionError(f"rotation skipped target epoch {target_epoch}; current epoch={epoch}")
            params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
            current_len = parse_rpc_int(params["ConfiguredEpochLength"])
            expected_len = epoch_length_for_target(epoch + 1)
            if current_len != expected_len:
                raise AssertionError(
                    f"current epoch length={current_len}, expected {expected_len} "
                    f"after reaching epoch {epoch}"
                )
            rotation_epoch_start_blocks[epoch] = target_deadline_block or block
            log(
                f"rotation reached epoch {epoch} at block {block} "
                f"epochStartBlock={rotation_epoch_start_blocks[epoch]} "
                f"nextEpochLength={current_len}"
            )
            return snapshot
        if wall_deadline is not None and now >= wall_deadline:
            break
        if target_deadline_block is not None and block >= target_deadline_block + ROTATION_WAIT_MARGIN_BLOCKS:
            break
        if now - last_progress >= ROTATION_NO_PROGRESS_TIMEOUT:
            raise TimeoutError(
                f"rotation wait made no block progress for {ROTATION_NO_PROGRESS_TIMEOUT}s; "
                f"current epoch={current_epoch()} block={block}"
            )
        if now >= next_trigger:
            trigger_rotation_write()
            next_trigger = time.time() + 3
        time.sleep(2)
    detail = ""
    if target_deadline_block is not None:
        detail = (
            f" deadlineBlock={target_deadline_block}"
            f" stopBlock={target_deadline_block + ROTATION_WAIT_MARGIN_BLOCKS}"
        )
    raise TimeoutError(f"rotation to epoch {target_epoch} timeout; current epoch={current_epoch()} block={current_block()}{detail}")


def verify_vrf_submitted(epoch: int) -> None:
    logs = collect_event_logs(
        event_topic("TEEVRFSubmitted(uint256,bytes32,bytes32,bytes32,bytes32,uint256,uint256)"),
        topics=[uint_topic(epoch)],
    )
    hardware_ids = {entry["topics"][2].lower() for entry in logs if len(entry.get("topics", [])) >= 3}
    if len(hardware_ids) < ACTIVE_LIMIT:
        raise AssertionError(f"epoch {epoch} VRF submissions={len(hardware_ids)}, expected at least {ACTIVE_LIMIT}")
    log(f"epoch {epoch} VRF submissions observed: {len(hardware_ids)}")


def verify_active_set(epoch: int, prev_members: set[str]) -> tuple[set[str], set[str]]:
    snapshot = snapshot_active_set()
    if snapshot["epoch"] < epoch:
        raise AssertionError(f"current epoch={snapshot['epoch']}, expected at least {epoch}")
    rotated_logs = collect_event_logs(
        event_topic("ActiveSetRotated(uint256,bytes32,uint256,uint256,bytes32,bytes32,bytes32[])"),
        topics=[uint_topic(snapshot["epoch"])],
    )
    if not rotated_logs:
        raise AssertionError(f"missing ActiveSetRotated event for epoch {snapshot['epoch']}")
    cur_members = set(snapshot["members"])
    dropped = prev_members - cur_members
    log(f"epoch {snapshot['epoch']} active set verified: active={len(cur_members)} dropped={len(dropped)}")
    return cur_members, dropped


def dkrs_extract_progress(epoch: int) -> tuple[int, int]:
    old_epoch = epoch - 1
    started = 0
    extracted = 0
    start_old = f"oldEpoch={old_epoch}"
    start_new = f"newEpoch={epoch}"
    for path in sorted(LOGS.glob("geth-node-*.log")):
        saw_start = False
        saw_extract = False
        try:
            with path.open("r", encoding="utf-8", errors="ignore") as handle:
                for line in handle:
                    if (
                        not saw_start
                        and "TEE host starting DKRS" in line
                        and start_old in line
                        and start_new in line
                    ):
                        saw_start = True
                        continue
                    if saw_start and "TEE host extracted DKRS" in line:
                        saw_extract = True
                        break
        except OSError:
            continue
        if saw_start:
            started += 1
        if saw_extract:
            extracted += 1
    return extracted, started


def wait_dkrs_ready(epoch: int) -> None:
    if epoch <= 0:
        return
    if DKRS_READY_TIMEOUT == 0 and DKRS_READY_FALLBACK_WAIT == 0:
        log(f"epoch {epoch} DKRS readiness wait disabled")
        return
    required = min(DKRS_READY_MIN_EXTRACTED, ACTIVE_LIMIT)
    deadline = time.time() + DKRS_READY_TIMEOUT if DKRS_READY_TIMEOUT > 0 else None
    last_report = 0.0
    log(
        f"waiting for epoch {epoch} DKRS readiness: "
        f"requiredExtracted={required} timeout={DKRS_READY_TIMEOUT}s"
    )
    while True:
        extracted, started = dkrs_extract_progress(epoch)
        if extracted >= required:
            log(f"epoch {epoch} DKRS ready: extracted={extracted} started={started}")
            return
        now = time.time()
        if now - last_report >= 15:
            log(f"epoch {epoch} DKRS not ready yet: extracted={extracted}/{required} started={started}")
            last_report = now
        if deadline is not None and now >= deadline:
            if DKRS_READY_FALLBACK_WAIT > 0:
                log(
                    f"epoch {epoch} DKRS readiness timeout: extracted={extracted}/{required} "
                    f"started={started}; fallback sleep {DKRS_READY_FALLBACK_WAIT}s"
                )
                time.sleep(DKRS_READY_FALLBACK_WAIT)
                extracted, started = dkrs_extract_progress(epoch)
                if extracted >= required:
                    log(f"epoch {epoch} DKRS ready after fallback: extracted={extracted} started={started}")
                    return
            raise TimeoutError(
                f"epoch {epoch} DKRS readiness timeout: "
                f"extracted={extracted}/{required} started={started}; inspect logs in {LOGS}"
            )
        time.sleep(3)


def refunded_deposit(log_entry: dict[str, Any]) -> int:
    data = str(log_entry.get("data", "0x")).removeprefix("0x")
    if len(data) < 64 * 3:
        return 0
    return int(data[64 * 2:64 * 3], 16)


def verify_erase_exited(epoch: int, dropped_hwids: set[str], timeout: int = 240) -> None:
    if not dropped_hwids:
        log(f"epoch {epoch} had no dropped active TEEs; erase check skipped")
        return
    topic0 = event_topic("TEEExited(bytes32,address,uint256,bytes32,uint256,uint256)")
    deadline = time.time() + timeout
    missing = set(dropped_hwids)
    refunds: dict[str, int] = {}
    while time.time() < deadline:
        for hwid in list(missing):
            logs = collect_event_logs(topic0, topics=[normalize_topic(hwid), None, uint_topic(epoch)])
            paid = [refunded_deposit(entry) for entry in logs]
            if paid and max(paid) > 0:
                refunds[hwid] = max(paid)
                missing.remove(hwid)
        if not missing:
            log(f"epoch {epoch} erase proofs observed for {len(dropped_hwids)} dropped TEEs")
            return
        time.sleep(3)
    raise TimeoutError(f"epoch {epoch} missing TEEExited refund events for: {sorted(missing)} refunds={refunds}")


def verify_no_revocations(epoch: int) -> None:
    logs = collect_event_logs(event_topic("TEERevoked(bytes32,address,bytes32,uint256,uint256)"))
    if logs:
        raise AssertionError(f"TEERevoked events observed by epoch {epoch}: {len(logs)}")


def verify_pkch_unchanged(expected_hash: str) -> str:
    params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    current = str(params["PkCHHash"]).lower()
    if current != expected_hash.lower():
        raise AssertionError(f"pkCH hash changed: {current} != {expected_hash}")
    return current


def wait_pkch() -> None:
    deadline = time.time() + 180
    zero = "0x" + "0" * 64
    empty_hash = "0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    while time.time() < deadline:
        params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
        pk_hash = str(params.get("PkCHHash", zero))
        pk_ver = str(params.get("CurrentPkCHVersion", "0"))
        if pk_hash not in (zero, empty_hash):
            log(f"pkCH ready: version={pk_ver} hash={pk_hash}")
            perf_anchor("system_init", "pkch_ready", version=pk_ver, pkch_hash=pk_hash)
            return
        time.sleep(2)
    raise TimeoutError("pkCH generation timeout; inspect geth-node logs for DistKG")


def start_background_load() -> None:
    global load_proc
    if ON_TX_PERS <= 0:
        log("background ON tx load disabled")
        return
    if load_proc and load_proc.poll() is None:
        return
    out = open(LOGS / "tx-load.log", "w")
    cmd = [
        sys.executable, str(GETH_DIR / "tx.py"),
        "--rpc", node_rpc(ON_INDEX),
        "--accounts", str(WORK / "on-load-accounts.json"),
        "--receivers", str(WORK / "on-load-receivers.json"),
        "--rate", str(ON_TX_PERS),
        "--log", str(WORK / "on-tx-load.jsonl"),
    ]
    tx_per_block = ON_TX_PER_BLOCK
    if tx_per_block > 0:
        cmd += ["--tx-per-block", str(tx_per_block)]
        if EST_SECONDS_PER_BLOCK > 0:
            cmd += ["--block-interval-hint", str(EST_SECONDS_PER_BLOCK)]
    if TEE_CORE_ISOLATION:
        cmd = ["taskset", "-c", TEE_CORE_OTHER_CORES] + cmd
    load_proc = subprocess.Popen(cmd, cwd=str(GETH_DIR), stdout=out, stderr=subprocess.STDOUT)
    log(
        f"background ON tx load started: source=node{ON_INDEX} "
        f"accounts={ON_ACCOUNT_COUNT} onTxPers={ON_TX_PERS:.3f}/s "
        f"txPerBlock={tx_per_block or 'off'} "
        f"pid={load_proc.pid}"
    )


def stop_background_load() -> None:
    global load_proc
    if load_proc and load_proc.poll() is None:
        load_proc.terminate()
        try:
            load_proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            load_proc.kill()
            load_proc.wait(timeout=5)
    load_proc = None


def send_target_tx() -> tuple[int, int, str, str, str]:
    old_payload = b"sgx-l5-before"
    recipient_index = TEE_NODE_INDICES[0]
    with tx_send_lock:
        target_tx = rpc(node_rpc(ON_INDEX), "eth_sendTransaction", [{
            "from": accounts[ON_INDEX - 1]["address"],
            "to": accounts[recipient_index - 1]["address"],
            "value": "0x0",
            "data": "0x" + old_payload.hex(),
            "gas": "0x100000",
        }], timeout=20)
    receipt = wait_for_receipt(node_rpc(1), target_tx)
    tx_obj = rpc(node_rpc(1), "eth_getTransactionByHash", [target_tx], timeout=10)
    if not tx_obj or not tx_obj.get("chameleonR") or int(tx_obj["chameleonR"], 16) == 0:
        raise AssertionError(f"target tx missing chameleon randomizer after pkCH ready: {tx_obj}")
    block_num = int(receipt["blockNumber"], 16)
    tx_index = int(receipt["transactionIndex"], 16)
    # Wait for the target block to gain REDACTION_CONFIRMATIONS successors before
    # editing it. Editing a block that is still propagating races the TxModifyMsg
    # against the block itself: if the modify message arrives before the block,
    # the receiving node drops it ("block not found") and later rejects the block
    # (nonce mismatch), stalling that node's chain. Waiting for confirmations
    # guarantees the block is already on every node when the edit is proposed.
    _wait_confirmations(block_num, REDACTION_CONFIRMATIONS)
    block = rpc(node_rpc(1), "eth_getBlockByNumber", [hex(block_num), False], timeout=10)
    block_txs = len(block.get("transactions", [])) if isinstance(block, dict) else 0
    log(
        f"target tx mined: block={block_num} index={tx_index} tx={target_tx} "
        f"source=node{ON_INDEX} block_txs={block_txs} "
        f"confirmations={REDACTION_CONFIRMATIONS} "
        f"on_tx_pers={ON_TX_PERS:.3f}/s on_account_count={ON_ACCOUNT_COUNT}"
    )
    return block_num, tx_index, target_tx, old_payload.decode(), "sgx-l5-after"


def _wait_confirmations(block_num: int, confirmations: int) -> None:
    """Block until block_num has `confirmations` successors on node1, so the
    block is already on every peer before it is used as a redaction target."""
    if confirmations <= 0:
        return
    target_height = block_num + confirmations
    deadline = time.time() + max(120, confirmations * EST_SECONDS_PER_BLOCK * 4)
    while time.time() < deadline:
        if current_block() >= target_height:
            return
        time.sleep(1)
    raise TimeoutError(
        f"target block {block_num} did not reach {confirmations} confirmations "
        f"(height {current_block()} < {target_height})"
    )


def submit_redaction_material(block_num: int, tx_index: int, new_payload: str) -> dict[str, Any]:
    payload = {
        "BlockNum": str(block_num),
        "Index": tx_index,
        "ModifyType": 1,
        "DataAfter": new_payload,
        "RedactReason": "sgx-l5-sim",
        "Meta": '{"rule":"allow","case":"sgx-l5-sim"}',
    }
    result = rpc(node_rpc(1), "eth_sendTEERedactionMaterial", [json.dumps(payload)], timeout=20)
    log(f"TEE redaction material broadcast: req={result['ReqID']}")
    return result


_AUTH_RECORD_POOL = concurrent.futures.ThreadPoolExecutor(max_workers=4)
_AUTH_RECORD_FUTURES: list[concurrent.futures.Future] = []


def authorize_redaction(material: dict[str, Any]) -> str:
    join_key = str(material["ReqID"]).removeprefix("0x").lower()
    params = rpc(node_rpc(1), "eth_getSystemParams", [], timeout=10)
    epoch = params.get("Epoch", params.get("CurrentEpoch"))
    if epoch is None:
        raise KeyError("Epoch")
    chain_id = int(rpc(node_rpc(1), "eth_chainId", []), 16)
    body_hashes = dict(zip(
        ["tx_id", "m_hash", "m_prime_hash", "pos_hash", "meta_hash", "active_set_hash", "pk_ch_hash"],
        material["BodyHashes"],
    ))
    submit_params = {
        "chain_id": str(chain_id),
        "contract_address": contract_address,
        "committee_pub_key_version": str(params["CommitteeVersion"]),
        "expires_at": str(int(time.time()) + 3600),
        "policy_context": {"rule": "allow", "case": "sgx-l5-sim"},
        "skip_active_set_check": True,
        "req": {
            "version": 1,
            "epoch": int(str(epoch)),
            "req_id": material["ReqID"],
            "body_hashes": body_hashes,
        },
    }
    # Block on the committee's completion channel rather than polling: the
    # server closes a per-session channel when the certificate is stored, so
    # this returns on the exact completion instant. Polling would only observe
    # it on the poll cadence and quantize the committee-to-broadcast latency.
    # Anchor the instant the proposer (node1) hands the redaction request to the
    # committee, so edit_execution can be measured from the proposal rather than
    # from when the TEE hosts receive the authorization.
    perf_anchor("edit_execution", "committee_redact_request_submitted", join_key=join_key)
    submit = committee_rpc("committee_submitRedactRequest", submit_params, timeout=30)
    req_id = submit["request_id"]
    auth = committee_rpc(
        "committee_getRedactAuthorization",
        {"request_id": req_id, "wait": True, "wait_timeout_ms": 90000},
        timeout=100,
    )
    if auth.get("status") != "authorized" or not auth.get("cert_req_auth"):
        raise TimeoutError(
            f"committee authorization timeout (status={auth.get('status')})"
        )

    broadcast = committee_rpc("committee_broadcastRedactAuthorization", {"request_id": req_id}, timeout=30)
    log(f"redaction authorized broadcast via P2P: req={req_id} status={broadcast.get('status')}")

    def record_onchain() -> None:
        onchain = committee_rpc("committee_submitRedactAuthorizationOnChain", {"request_id": req_id}, timeout=30)
        tx_hash = onchain.get("tx_hash", "")
        if not tx_hash:
            raise RuntimeError(f"redaction authorization submission missing tx hash: {onchain}")
        log(f"async redaction authorization submitted on chain: req={req_id} tx={tx_hash}")
        wait_for_receipt(node_rpc(1), tx_hash, timeout=180, confirmations=1, require_success=True)
        perf_anchor("edit_execution", "authorization_recorded_onchain", join_key=join_key, tx=tx_hash)

    _AUTH_RECORD_FUTURES.append(_AUTH_RECORD_POOL.submit(record_onchain))
    return req_id


def wait_redaction_executed(req_id: str, new_payload: str, block_num: int = None, tx_index: int = None) -> None:
    # Tight cap as well: this wait is bracketed by the "gsc_executed" anchor, so
    # a coarse poll inflates the reported edit latency.
    expected_input = "0x" + new_payload.encode().hex()
    deadline = time.time() + 180
    attempt = 0
    while time.time() < deadline:
        try:
            if block_num is not None and tx_index is not None:
                tx = rpc(node_rpc(1), "eth_getTransactionByBlockNumberAndIndex",
                         [hex(block_num), hex(tx_index)], timeout=10)
                if tx and tx.get("input", "").lower() == expected_input.lower():
                    log("redaction execution verified on node1")
                    return
            if rpc(node_rpc(1), "eth_isRedactionExecuted", [req_id], timeout=10):
                log("redaction execution visible on chain")
                return
        except Exception:
            pass
        time.sleep(poll_backoff(attempt, POLL_MEASURED_MAX_BACKOFF_S))
        attempt += 1
    raise TimeoutError(f"TEE cluster redaction did not execute; inspect logs in {LOGS}")


def verify_redaction_on_node(
    node_index: int,
    role: str,
    req_id: str,
    block_num: int,
    tx_index: int,
    old_tx_hash: str,
    new_payload: str,
) -> None:
    """验证普通节点通过 TxModifyMsg 接受了编辑：
    1) 该位置交易内容已变为 new_payload（ON 收到并应用了编辑后的区块）；
    2) 交易 hash 保持不变 —— chameleon 等式 CH(pkCH,m,r)=CH(pkCH,m',r') 成立 ⇒ r' 正确。
    若 hash 变了，说明 r' 错误，编辑在密码学上不成立。"""
    expected_input = "0x" + new_payload.encode().hex()
    deadline = time.time() + 150
    tx = None
    last_pull = 0.0
    while time.time() < deadline:
        try:
            tx = rpc(node_rpc(node_index), "eth_getTransactionByBlockNumberAndIndex",
                     [hex(block_num), hex(tx_index)], timeout=10)
            if tx and tx.get("input", "").lower() == expected_input.lower():
                break
        except Exception:
            pass
        now = time.time()
        if now - last_pull >= 9:
            last_pull = now
            try:
                rpc(node_rpc(node_index), "eth_requestTxModify", [req_id, str(block_num), tx_index], timeout=10)
                log(
                    f"requested TxModify replay on {role} node{node_index}: "
                    f"req={req_id} block={block_num} index={tx_index}"
                )
            except Exception as exc:
                log(f"TxModify replay request failed on {role} node{node_index} (will retry): {exc}")
        time.sleep(3)
    else:
        raise TimeoutError(
            f"{role} node{node_index} did not observe redacted tx content at block={block_num} "
            f"index={tx_index} (last input={tx.get('input') if tx else None}); "
            "普通节点未收到/未应用 TxModifyMsg 编辑")
    if tx["hash"].lower() != old_tx_hash.lower():
        raise AssertionError(
            f"{role} node{node_index} tx hash changed after redaction: {tx['hash']} != {old_tx_hash} "
            "(chameleon 等式不成立 → r' 错误)")
    log(f"{role} node{node_index} verified redaction: content='{new_payload}', tx hash preserved")


def light_verification_nodes() -> list[int]:
    if LIGHT_NODE_VERIFY_COUNT <= 0:
        return []
    if LIGHT_NODE_VERIFY_COUNT == 1:
        return [LIGHT_NODE_INDICES[-1]]
    last = len(LIGHT_NODE_INDICES) - 1
    positions = {
        round(sample * last / (LIGHT_NODE_VERIFY_COUNT - 1))
        for sample in range(LIGHT_NODE_VERIFY_COUNT)
    }
    return [LIGHT_NODE_INDICES[position] for position in sorted(positions)]


def verify_on_nodes(req_id: str, block_num: int, tx_index: int, old_tx_hash: str, new_payload: str) -> None:
    verify_redaction_on_node(
        ON_INDEX, "ON", req_id, block_num, tx_index, old_tx_hash, new_payload
    )
    samples = light_verification_nodes()
    if not samples:
        return
    def safe_verify_light(node_index: int):
        try:
            verify_redaction_on_node(
                node_index,
                "light",
                req_id,
                block_num,
                tx_index,
                old_tx_hash,
                new_payload,
            )
        except Exception as e:
            log(f"WARN: light node{node_index} verification failed/timed out, skipping without terminating process: {e}")

    parallel_for(
        samples,
        safe_verify_light,
    )
    log(f"TxModify propagation verified on light-node samples: {samples}")


def run_one_redaction(tag: str) -> None:
    log(f"PERF_ANCHOR stage=edit_execution event=redaction_begin tag={tag}")
    block_num, tx_index, old_tx_hash, _old_payload, new_payload = send_target_tx()
    material = submit_redaction_material(block_num, tx_index, f"{new_payload}-{tag}")
    # join_key = geth/teehost 日志里 redaction 的 req_id/reqID（material.ReqID 去 0x 小写），
    # 用于把脚本的锚点和 geth 的 E2E_TIMING、teehost 的 PartialAdapt/RPrime 日志按编辑请求对齐
    join_key = str(material["ReqID"]).removeprefix("0x").lower()
    log(f"PERF_ANCHOR stage=edit_execution event=material_submitted join_key={join_key} tx={old_tx_hash}")
    req_id = authorize_redaction(material)
    log(f"PERF_ANCHOR stage=edit_execution event=authorization_broadcast join_key={join_key}")
    wait_redaction_executed(req_id, f"{new_payload}-{tag}", block_num, tx_index)
    log(f"PERF_ANCHOR stage=edit_execution event=gsc_executed join_key={join_key}")
    verify_on_nodes(req_id, block_num, tx_index, old_tx_hash, f"{new_payload}-{tag}")
    if communication_stats.enabled():
        perf_anchor("edit_execution", "communication_edit_verified",
                    join_key=join_key, status="ok", verification="configured_nodes")
    log(f"redaction {tag} passed")


def run_interval_redactions(tag_prefix: str) -> None:
    count = RNG.randint(EDITS_PER_EPOCH_MIN, EDITS_PER_EPOCH_MAX)
    if tag_prefix.startswith("baseline") and os.environ.get("TEE_BASELINE_EDIT_COUNT", "").strip():
        count = env_int("TEE_BASELINE_EDIT_COUNT", count, 1)
    max_attempts = env_int("TEE_EDIT_MAX_ATTEMPTS", count, count)
    log(f"interval {tag_prefix}: running {count} successful edit(s), max attempts {max_attempts}, edit gap {EDIT_GAP_S}s")
    completed = 0
    for attempt in range(1, max_attempts + 1):
        if EDIT_GAP_S > 0:
            time.sleep(EDIT_GAP_S)
        try:
            run_one_redaction(f"{tag_prefix}-r{attempt}")
        except Exception as exc:
            log(f"redaction {tag_prefix}-r{attempt} failed: {exc!r}")
            if max_attempts == count:
                raise
            continue
        completed += 1
        log(f"interval {tag_prefix}: verified edits {completed}/{count}, attempts {attempt}/{max_attempts}")
        if completed == count:
            return
    raise RuntimeError(f"interval {tag_prefix}: only {completed}/{count} verified edits after {max_attempts} attempts")


def main(*, manage_lifecycle: bool = True) -> int:
    if manage_lifecycle:
        signal.signal(signal.SIGTERM, lambda *_: cleanup())
        atexit.register(cleanup)
    apply_runner_affinity()
    global EPOCH_LENGTH
    if EPOCH_LENGTH < MIN_EPOCH_LENGTH:
        log(f"EPOCH_LENGTH {EPOCH_LENGTH} below safe floor {MIN_EPOCH_LENGTH} "
            f"for {TEE_COUNT} TEE nodes; raising to floor so registration finishes "
            f"before the epoch-1 rotation boundary")
        EPOCH_LENGTH = MIN_EPOCH_LENGTH
    log(
        f"config: teeCandidates={TEE_COUNT}/{MAX_TEE_HOST_COUNT} "
        f"lightNodes={LIGHT_NODE_COUNT} totalNodes={TOTAL_NODE_COUNT} "
        f"on=node{ON_INDEX} onObservers={ON_OBSERVER_INDICES} minerNodes={MINER_NODE_INDICES} teeNodes={TEE_NODE_INDICES} "
        f"sgxMode={SGX_MODE} quoteMode={TEE_QUOTE_MODE} attestationProvider={ATTESTATION_PROVIDER} "
        f"activeLimit={ACTIVE_LIMIT} "
        f"threshold={ACTIVE_THRESHOLD} rotations={ROTATIONS} "
        f"coreIsolation={'iso=' + TEE_CORE_ISO_CORES + ',other=' + TEE_CORE_OTHER_CORES if TEE_CORE_ISOLATION else 'off'} "
        f"bootstrapEpochLength={EPOCH_LENGTH} steadyEpochLength={STEADY_EPOCH_LENGTH} "
        f"committeeCount={COMMITTEE_COUNT} committeeThreshold={COMMITTEE_THRESHOLD} "
        f"p2pTopology={P2P_TOPOLOGY} p2pEdges={len(TOPOLOGY_EDGES)} "
        f"teeEdges={len(TEE_TOPOLOGY_EDGES)} teeNeighborRadius={TEE_P2P_NEIGHBOR_RADIUS} "
        f"p2pMaxDegree={MAX_TOPOLOGY_DEGREE} teeDiameterBound={TEE_DIAMETER_BOUND} "
        f"p2pDiameterBound={P2P_DIAMETER_BOUND} gethMaxPeers={GETH_MAX_PEERS} "
        f"gethP2PMsgDelayMs={GETH_P2P_MSG_DELAY_MS} "
        f"gethBlockBroadcastLimit={GETH_BLOCK_BROADCAST_LIMIT} "
        f"distKGRelayTTL={DISTKG_RELAY_TTL} dkrsRelayTTL={DKRS_RELAY_TTL} "
        f"txModifyRelayTTL={TXMODIFY_RELAY_TTL} "
        f"coreCacheMiB={CORE_NODE_CACHE_MB} coreGOMAXPROCS={CORE_NODE_GOMAXPROCS} "
        f"lightCacheMiB={LIGHT_NODE_CACHE_MB} lightVerifyCount={LIGHT_NODE_VERIFY_COUNT} "
        f"txConfirmations={TX_CONFIRMATIONS} teeRegistrationConfirmations={TEE_REGISTRATION_CONFIRMATIONS} "
        f"teeRegistrationRetries={TEE_REGISTRATION_RETRIES} "
        f"baselineEdits={BASELINE_EDITS} editsPerEpoch={EDITS_PER_EPOCH_MIN}-{EDITS_PER_EPOCH_MAX} "
        f"onAccountCount={ON_ACCOUNT_COUNT} onTxPers={ON_TX_PERS:.3f}/s"
    )
    prepare()
    perf_anchor("system_init", "chain_init_complete", nodes=TOTAL_NODE_COUNT)
    perf_anchor("system_init", "contract_deploy_begin")
    apply_netem()
    start_nodes()
    perf_anchor("system_init", "committee_dkg_begin", members=COMMITTEE_COUNT)
    # committee 已在 node_startup_begin 时后台启动，这里 join 等待其 RPC 就绪
    # （多数情况下 TEE/轻节点启动期间它已跑完，join 近似零等待）。
    join_committee_background()
    register_committee_pubkey()
    perf_anchor(
        "system_init",
        "committee_pubkey_registered",
        threshold=COMMITTEE_THRESHOLD,
    )
    perf_anchor("tee_registration", "tee_registration_begin", candidates=TEE_COUNT)
    register_tees()
    # Sanity-check the epoch-1 prep window against the contract clock, which starts at
    # initial active-set bootstrap rather than genesis.
    _epoch0_start_block = rotation_epoch_start_blocks.get(0, current_block())
    _guard_block = current_block()
    _prep_start_block = _epoch0_start_block + EPOCH_LENGTH - ROTATION_PREP_WINDOW
    if _guard_block >= _prep_start_block:
        raise AssertionError(
            f"registration finished at block {_guard_block}, but the epoch-1 rotation prep "
            f"window starts at block {_prep_start_block} "
            f"(epoch0StartBlock={_epoch0_start_block}, EPOCH_LENGTH={EPOCH_LENGTH}); "
            f"increase EPOCH_LENGTH or reduce baseline work"
        )
    assert_system_params()
    wait_pkch()
    start_background_load()
    perf_anchor("measurement_window", "measurement_begin")
    try:
        snapshot = snapshot_active_set()
        pkch_hash = snapshot["pkch_hash"]
        for baseline_index in range(1, BASELINE_EDITS + 1):
            run_interval_redactions(f"baseline-{baseline_index}")
        for round_index in range(1, ROTATIONS + 1):
            prev_epoch = snapshot["epoch"]
            target_epoch = snapshot["epoch"] + 1
            prev_members = set(snapshot["members"])
            if communication_stats.enabled():
                perf_anchor("rotation_flow", "communication_rotation_target",
                            epoch=target_epoch, round=round_index)
            snapshot = wait_rotation(target_epoch)
            verify_vrf_submitted(target_epoch)
            cur_members, dropped = verify_active_set(target_epoch, prev_members)
            verify_erase_exited(prev_epoch, dropped)
            verify_no_revocations(target_epoch)
            pkch_hash = verify_pkch_unchanged(pkch_hash)
            wait_dkrs_ready(target_epoch)
            if communication_stats.enabled():
                perf_anchor("rotation_flow", "communication_rotation_verified",
                            epoch=target_epoch, round=round_index, status="ok")
            run_interval_redactions(f"rotation-{round_index}-epoch-{target_epoch}")
            snapshot = snapshot_active_set()
            if set(snapshot["members"]) != cur_members:
                raise AssertionError("active set changed while verifying post-rotation redaction")
    finally:
        try:
            for future in _AUTH_RECORD_FUTURES:
                future.result(timeout=220)
        finally:
            perf_anchor("measurement_window", "measurement_end")
            stop_background_load()
    log(
        "L5 passed: TEE cluster redaction executed + ordinary-node samples "
        "verified content redacted & tx hash preserved"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    finally:
        if communication_stats.enabled() and LOGS.is_dir():
            cleanup()
            try:
                communication_stats.collect_single(WORK, WORK / "communication", Path(__file__).parent)
            except Exception as exc:
                print(f"communication collection failed: {exc!r}", file=sys.stderr)
