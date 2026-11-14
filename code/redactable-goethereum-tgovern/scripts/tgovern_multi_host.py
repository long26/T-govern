#!/usr/bin/env python3
"""T-govern (tee-connect) multi-host orchestration.

Runs the existing L5 flow (registration -> DistKG -> baseline edits -> contract
epoch rotations -> post-rotation edits) across two machines, with the ordinary
host (master) running node1 + committee-mvp and the TEE host running the TEE
nodes. Rotation follows the ORIGINAL tee-connect flow -- the contract rotates
epochs by itself; the orchestrator only waits/nudges (wait_rotation). No PoW
election is involved on this side.

Design:
- Reuses sgx_l5_sim_e2e.py as a library: its business functions (register_tees,
  wait_rotation, run_interval_redactions, verify_*) are env-driven and address
  nodes via node_rpc(i) = http://127.0.0.1:{18550+i}.
- The TEE host's nodes are exposed on the master via SSH local port-forwards so
  every RPC still looks like 127.0.0.1 to the reused code.
- Process/keystore/bootstrap management uses the same JSON-over-SSH worker
  pattern as nitch_multi_host_perf.py.

Topology (default TEE_COUNT=4):
  host A (ordinary, master): node1 (ON miner) + committee-mvp (IPC to node1)
  host B (TEE):             TEE nodes (indices TEE_START_INDEX..)

Usage (on host A):
  python3 scripts/tgovern_multi_host.py start \
    --remote-ssh root@<B> --local-ip <A> --remote-ip <B> \
    --remote-root /opt/tgovern --tee-count 4 --active-limit 4 --active-threshold 3 \
    --committee-count 4 --committee-threshold 3 --rotations 1 --baseline-edits 1
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
from concurrent.futures import ThreadPoolExecutor, as_completed
import datetime
import importlib.util
import ipaddress
import json
import os
import re
import secrets
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path

import topology
import communication_stats

ROOT = Path(__file__).resolve().parents[1]
RUN_ID_RE = re.compile(r"[a-zA-Z0-9_-]{1,80}\Z")
# Remote node RPCs are forwarded to these master-local ports so the reused
# sgx_l5 code (node_rpc = 18550+i) keeps working through a patched resolver.
TEE_FWD_BASE = 28550
GETH_SGX = "geth-sgx"
# Epoch budgets sized for the current PoW difficulty (~10s blocks), not the
# historical 1-2s chain the earlier defaults assumed. Worst case over four
# 16-TEE slow-chain runs: registration+bootstrap+baseline needed 101 blocks and
# a rotated epoch's five edits needed 93. Add the contract's 20-block
# RotationPreparation window plus margin -> 128 bootstrap, 120 steady.
DEFAULT_BOOTSTRAP_EPOCH = 128
DEFAULT_STEADY_EPOCH = 120


def node_rpc_port(index: int) -> int:
    return 18550 + index


def fwd_rpc_port(index: int) -> int:
    return TEE_FWD_BASE + index


def p2p_port(index: int) -> int:
    return 30310 + index


def committee_port(index: int) -> int:
    """committee-mvp node<index> listen address; same-host, so loopback-bound."""
    return 3400 + index


def run_dir(root: Path, run_id: str) -> Path:
    if not RUN_ID_RE.fullmatch(run_id):
        raise ValueError("invalid run id")
    return root / ".tmp" / "multi-host" / run_id


def rpc(port: int, method: str, params: object = None, timeout: int = 5) -> object:
    import urllib.request
    payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method,
                          "params": [] if params is None else params}).encode()
    request = urllib.request.Request(f"http://127.0.0.1:{port}", payload,
                                     {"Content-Type": "application/json"})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    log_path = globals().get("COMM_RPC_LOG")
    if log_path is None:
        with opener.open(request, timeout=timeout) as response:
            result = json.load(response)
    else:
        result = json.loads(communication_stats.rpc_read(
            opener, request, timeout, log_path, method))
    if result.get("error"):
        raise RuntimeError(f"{method}: {result['error']}")
    return result.get("result")


def wait_until(label: str, action, timeout: int = 120, check=None):
    deadline = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < deadline:
        # Permanent failures (e.g. a dead SSH tunnel) must not be retried.
        if check is not None:
            check()
        try:
            result = action()
            if result is not None and result is not False:
                return result
        except (OSError, ValueError, RuntimeError, KeyError, TypeError) as exc:
            last_error = exc
        time.sleep(1)
    raise TimeoutError(f"{label} not ready in {timeout}s (last error: {last_error})")


def write_private(path: Path, text: str) -> None:
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as output:
        output.write(text)


def assert_port_free(port: int, address: str = "0.0.0.0") -> None:
    with socket.socket() as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            sock.bind((address, port))
        except OSError as exc:
            raise RuntimeError(f"port {port} is unavailable: {exc}") from exc


# --------------------------------------------------------------------------- #
# worker actions (run on each host via --worker, JSON on stdin)                #
# --------------------------------------------------------------------------- #
def worker_prepare(config: dict) -> dict:
    """Create the run workspace, datadirs and imported accounts for the nodes
    this host owns. Mirrors sgx_l5 prepare() but per-host and without its
    single-machine assumptions."""
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    owned = config["nodes"]
    if work.exists():
        raise FileExistsError(f"run dir exists: {work}")
    geth_sgx = root / "bin" / GETH_SGX
    if not geth_sgx.is_file():
        raise FileNotFoundError(geth_sgx)
    for f in ("genesis.json", "helper.json"):
        if not (root / f).is_file():
            raise FileNotFoundError(root / f)
    for index in owned:
        assert_port_free(node_rpc_port(index))
        assert_port_free(p2p_port(index))
    work.mkdir(parents=True)
    write_private(work / "owner.json", json.dumps({"owner": config.get("owner")}) + "\n")
    for name in ("chain", "node-work", "committee-configs", "logs", "run", "accounts"):
        (work / name).mkdir()
    (work / "genesis.json").write_text(json.dumps(config["genesis"], indent=2) + "\n")
    cache = work / "ethash-cache"
    cache.mkdir()
    for block in (0, 30000):
        subprocess.run([str(geth_sgx), "makecache", str(block), str(cache)],
                       check=True, stdout=subprocess.DEVNULL)
    from eth_account import Account
    for index in owned:
        acct = Account.from_key(config["keys"][str(index)])
        datadir = work / "chain" / f"node{index}"
        nodework = work / "node-work" / f"node{index}"
        datadir.mkdir()
        nodework.mkdir()
        key_file = nodework / "privatekey.txt"
        write_private(key_file, config["keys"][str(index)] + "\n")
        (nodework / "password.txt").write_text("1\n")
        subprocess.run([str(geth_sgx), "--datadir", str(datadir), "init", str(work / "genesis.json")],
                       check=True, stdout=subprocess.DEVNULL)
        subprocess.run([str(geth_sgx), "--datadir", str(datadir), "account", "import",
                        "--password", str(nodework / "password.txt"), str(key_file)],
                       check=True, stdout=subprocess.DEVNULL)
        (work / "accounts" / f"node{index}.json").write_text(
            json.dumps({"address": acct.address,
                        "key": config["keys"][str(index)]}) + "\n")
    return {"work": str(work), "nodes": owned}


def process_info(pid: int) -> tuple[str, str]:
    fields = Path(f"/proc/{pid}/stat").read_text().rsplit(") ", 1)[1].split()
    return fields[19], fields[0]


def spawn(work: Path, name: str, args: list[str], env: dict, cwd: Path) -> int:
    record = work / "run" / f"{name}.json"
    if record.exists():
        raise FileExistsError(record)
    with (work / "logs" / f"{name}.log").open("x") as output:
        proc = subprocess.Popen(args, env=env, cwd=str(cwd), stdin=subprocess.DEVNULL,
                                stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        write_private(record, json.dumps({"pid": proc.pid, "start": process_info(proc.pid)[0]}) + "\n")
    except BaseException:
        if proc.poll() is None:
            proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
        raise
    return proc.pid


def worker_start(config: dict) -> dict:
    """Start one node. teehost=True nodes run geth-sgx --teehost with SGX env;
    node1 runs the ordinary miner that deploys the contract."""
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    index = config["node"]
    info = json.loads((work / "accounts" / f"node{index}.json").read_text())
    address, datadir = info["address"], work / "chain" / f"node{index}"
    nodework = work / "node-work" / f"node{index}"
    import shutil
    shutil.copyfile(root / "helper.json", nodework / "helper.json")
    (nodework / "storage.json").write_text(json.dumps({
        "address": config.get("contract", ""),
        "privateKey": info["key"],
        "port": str(node_rpc_port(index)),
    }, indent=2) + "\n")
    geth = root / "bin" / GETH_SGX
    # Background ON tx load accounts (node1 only): imported into node1's keystore
    # and unlocked so tx.py can drive eth_sendTransaction during the measurement
    # window. The library's own start_background_load is stubbed out in multi-host
    # mode, so this orchestrator owns the load end to end.
    unlock_extra = []
    if index == 1 and config.get("on_load_accounts"):
        load_work = work / "node-work" / "on-load"
        load_work.mkdir(parents=True, exist_ok=True)
        (load_work / "password.txt").write_text("1\n")
        for n, acct in enumerate(config["on_load_accounts"], start=1):
            key_file = load_work / f"key{n}.txt"
            write_private(key_file, acct["privateKey"].removeprefix("0x") + "\n")
            subprocess.run([str(geth), "--datadir", str(datadir), "account", "import",
                            "--password", str(load_work / "password.txt"), str(key_file)],
                           check=True, stdout=subprocess.DEVNULL)
            unlock_extra.append(acct["address"])
        (nodework / "password.txt").write_text("1\n" * (1 + len(unlock_extra)))
    args = [str(geth), "--datadir", str(datadir),
            "--networkid", str(config["network_id"]), "--port", str(p2p_port(index)),
            "--maxpeers", str(config.get("maxpeers", 50)), "--syncmode", "full",
            "--ethash.cachedir", str(work / "ethash-cache"), "--cache", str(config.get("core_cache_mb", 128)),
            "--rpc", "--rpcaddr", "127.0.0.1", "--rpcport", str(node_rpc_port(index)),
            "--rpcapi", "eth,net,web3,personal,admin,debug,miner,txpool",
            "--ipcpath", str(datadir / "geth.ipc"),
            "--allow-insecure-unlock", "--unlock", ",".join([address] + unlock_extra),
            "--password", str(nodework / "password.txt"),
            "--nodiscover", "--verbosity", "4"]
    env = os.environ.copy()
    env.update({"TGOVERN_COMM_STATS": "1" if config.get("comm_stats") else "0",
                "TGOVERN_COMM_NODE": f"geth-node-{index}",
                "SGX_MODE": config.get("sgx_mode", "HW"),
                "SGX_SIM_ID_SEED": f"multi-host-node-{index}",
                "GOMAXPROCS": str(config.get("gomaxprocs", 1)),
                "EPOCH_LENGTH": str(config["epoch_length"]),
                "STEADY_EPOCH_LENGTH": str(config["steady_epoch_length"]),
                "TEE_STEADY_EPOCH_LENGTH": str(config["steady_epoch_length"]),
                "TEE_ACTIVE_LIMIT": str(config["active_limit"]),
                "TEE_ACTIVE_THRESHOLD": str(config["active_threshold"])})
    if config.get("teehost"):
        args += ["--teehost", "--tee.eventpoll", config.get("eventpoll", "200ms"),
                 "--tee.confirmations", "0", "--tee.dkrs-relay",
                 "--tee.dkrs-relay-max-peers", str(config.get("dkrs_relay_max_peers", 64))]
        env["SGX_ENCLAVE_PATH"] = str(root / "sgx" / "enclave.signed.so")
        env["E2E_TIMING_NODE_ROLE"] = "tee"
    elif config.get("light"):
        # Lightweight ordinary node (matches sgx_l5 start_geth_node(lightweight)):
        # smaller cache, no IPC, restricted RPC API. On the TEE host it still runs
        # geth-sgx but without --teehost.
        args = [str(geth), "--datadir", str(datadir),
                "--networkid", str(config["network_id"]), "--port", str(p2p_port(index)),
                "--maxpeers", str(config.get("maxpeers", 50)), "--syncmode", "full",
                "--ethash.cachedir", str(work / "ethash-cache"), "--cache",
                str(config.get("light_cache_mb", 16)),
                "--rpc", "--rpcaddr", "127.0.0.1", "--rpcport", str(node_rpc_port(index)),
                "--rpcapi", "eth,net,admin", "--ipcdisable", "--nousb",
                "--cache.noprefetch", "--nodiscover",
                "--verbosity", str(max(3 if config.get("comm_stats") else 0,
                                        config.get("light_verbosity", 2)))]
        env["GOMAXPROCS"] = str(config.get("light_gomaxprocs", 1))
        env["E2E_TIMING_NODE_ROLE"] = "light"
    else:
        env["E2E_TIMING_NODE_ROLE"] = "on"
    broadcast = config.get("block_broadcast_limit", 0)
    if config.get("teehost") and config.get("block_broadcast_limit_tee", -1) >= 0:
        broadcast = config["block_broadcast_limit_tee"]
    args += ["--p2p.msg-delay-ms", str(config.get("p2p_msg_delay_ms", 0)),
             "--p2p.block-broadcast-limit", str(broadcast), "--etherbase", address,
             "--tee.distkg-relay-ttl", str(config.get("distkg_relay_ttl", 4)),
             "--tee.dkrs-relay-ttl", str(config.get("dkrs_relay_ttl", 4)),
             "--txmodify.relay-ttl", str(config.get("txmodify_relay_ttl", 4))]
    if config.get("teehost") and config.get("headwatch"):
        args += ["--tee.headwatch"]
    env.update(config.get("geth_env", {}))
    if config.get("mine"):
        args += ["--mine", "--miner.threads", "1"]
    sgx_sdk = env.get("SGX_SDK", "/opt/intel/sgxsdk")
    env["LD_LIBRARY_PATH"] = ":".join(filter(None, [
        str(root / "sgx"), f"{sgx_sdk}/sdk_libs", f"{sgx_sdk}/lib64",
        env.get("LD_LIBRARY_PATH", "")]))
    return {"pid": spawn(work, f"node{index}", args, env, nodework)}


def qdisc_roots(dev: str) -> list[dict]:
    result = subprocess.run(["tc", "-j", "qdisc", "show", "dev", dev],
                            check=True, capture_output=True, text=True)
    return [q for q in json.loads(result.stdout) if q.get("root")]


def default_qdisc(roots: list[dict]) -> bool:
    return all(q.get("handle") == "0:" and q.get("kind") in
               ("mq", "noqueue", "fq_codel", "pfifo_fast") for q in roots)


def netem_specs(config: dict) -> list[dict]:
    """One spec per delayed device.

    The cross-host device filters on the peer address, because that host's P2P
    sockets all carry the peer's IP.  Loopback does not: a same-host socket
    keeps the host's own address (``ip route get <own-ip>`` -> ``dev lo``), so
    an ``ip dst <peer>`` filter would select nothing.  Loopback therefore
    matches on port alone, which is what actually separates the same-host geth
    P2P and committee sockets from RPC and other must-not-delay traffic.
    """
    specs = [{"dev": config["dev"], "dst": config["peer"],
              "ports": [int(p) for p in config["p2p_ports"]]}]
    loopback_ports = [int(p) for p in config.get("loopback_ports", [])]
    if config.get("loopback_dev") and loopback_ports:
        specs.append({"dev": config["loopback_dev"], "dst": None,
                      "ports": loopback_ports})
    return specs


def _netem_handles(run_id: str, dev: str) -> tuple[str, str]:
    """Deterministic qdisc handles, so a plan can be written before it is applied."""
    import hashlib
    major = int(hashlib.sha256(f"{run_id}/{dev}".encode()).hexdigest()[:4], 16) or 1
    return f"{major:x}:", f"{(major ^ 0x8000) or 2:x}:"


def _netem_install(entry: dict, config: dict, work: Path) -> None:
    dev = entry["dev"]
    if not re.fullmatch(r"[a-zA-Z0-9_.:-]+", dev):
        raise ValueError(f"invalid netem device: {dev}")
    roots = qdisc_roots(dev)
    if not default_qdisc(roots):
        raise RuntimeError(f"refusing to replace existing qdisc on {dev}: {roots}")
    filters = subprocess.run(["tc", "-j", "filter", "show", "dev", dev],
                             check=True, capture_output=True, text=True)
    if json.loads(filters.stdout):
        raise RuntimeError(f"refusing to replace {dev} with existing filters")
    lock = work.parent / f"netem-{dev}.json"
    # A device lock can outlive its run after a crash or host power loss, since
    # neither can be intercepted to release it.  The device is provably free
    # here (default qdisc, no filters), so a leftover lock from another run is
    # stale bookkeeping rather than a live owner -- an active owner would have
    # left its prio root qdisc in place.  Reclaim it instead of aborting the
    # whole run; a live lock is still protected by the non-default-qdisc check
    # above.
    if lock.exists():
        try:
            stale = json.loads(lock.read_text())
        except (OSError, ValueError):
            stale = None
        if not isinstance(stale, dict) or stale.get("run_id") != config["run_id"]:
            lock.unlink()
    major = entry["handle"].rstrip(":")
    netem = ["netem", "delay", f"{config['delay_ms']}ms"]
    if config.get("jitter_ms"):
        netem += [f"{config['jitter_ms']}ms"]
    if config.get("loss_percent"):
        netem += ["loss", f"{config['loss_percent']}%"]
    netem += ["limit", str(config.get("limit", 10000))]
    commands = [
        ["tc", "qdisc", "replace", "dev", dev, "root", "handle", entry["handle"],
         "prio", "bands", "3", "priomap", *(["0"] * 16)],
        ["tc", "qdisc", "add", "dev", dev, "parent", f"{major}:3",
         "handle", entry["child"], *netem],
    ]
    # Exact destinations avoid masks which silently miss ports across a boundary.
    for port in entry["ports"]:
        match = ["match", "ip", "dport", str(port), "0xffff"]
        if entry["dst"]:
            match = ["match", "ip", "dst", entry["dst"]] + match
        commands.append(["tc", "filter", "add", "dev", dev, "protocol", "ip",
                         "parent", entry["handle"], "prio", "1", "u32", *match,
                         "flowid", f"{major}:3"])
    write_private(lock, json.dumps({"run_id": config["run_id"], "dev": dev,
                                    "handle": entry["handle"]}) + "\n")
    try:
        for command in commands:
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
    except BaseException:
        _netem_remove(entry, work, config["run_id"])
        raise


def _netem_remove(entry: dict, work: Path, run_id: str) -> str:
    dev = entry["dev"]
    lock = work.parent / f"netem-{dev}.json"
    if lock.exists():
        try:
            owner_run = json.loads(lock.read_text()).get("run_id")
        except (OSError, ValueError):
            owner_run = None
        if owner_run is not None and owner_run != run_id:
            raise RuntimeError(f"netem ownership mismatch on {dev}; refusing removal")
    roots = qdisc_roots(dev)
    if roots and roots[0].get("handle") == entry["handle"] and roots[0].get("kind") == "prio":
        subprocess.run(["tc", "qdisc", "del", "dev", dev, "root"], check=True)
    elif not default_qdisc(roots):
        raise RuntimeError(f"netem root changed; refusing removal of {dev}: {roots}")
    lock.unlink(missing_ok=True)
    return dev


def worker_netem(config: dict) -> dict:
    """Own one run-specific qdisc per delayed device; never delete an unrelated
    root qdisc.  Devices are applied as a set: if any fails, the ones already
    applied are rolled back and the marker is cleared, so a run either gets the
    whole delay profile or none of it."""
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    marker = work / "netem.json"
    if config.get("owner"):
        try:
            owner = json.loads((work / "owner.json").read_text())["owner"]
        except FileNotFoundError:
            return {"netem": "not owned"}
        if owner != config["owner"]:
            return {"netem": "not owned"}
    if config.get("off"):
        if not marker.exists():
            return {"netem": "not owned"}
        try:
            info = json.loads(marker.read_text())
        except (OSError, ValueError):
            info = None
        if not isinstance(info, dict):
            raise RuntimeError(f"netem marker is unreadable on {config.get('dev')}; "
                               "refusing removal")
        if info.get("run_id") != config["run_id"]:
            raise RuntimeError("netem ownership mismatch; refusing removal")
        # A marker written by an older build carried the single-device lock shape
        # ({"run_id", "dev", "handle"}) instead of the device list.  Accept it so
        # an interrupted run can still be unwound, but never raise a raw KeyError
        # that would abort the whole teardown.
        devices = info.get("devices")
        if devices is None and "dev" in info and "handle" in info:
            devices = [{"dev": info["dev"], "handle": info["handle"],
                        "dst": info.get("dst"), "ports": info.get("ports", [])}]
        if not isinstance(devices, list):
            raise RuntimeError(f"netem marker lacks a device list on "
                               f"{config.get('dev')}; refusing removal")
        removed, errors = [], []
        for entry in devices:
            try:
                removed.append(_netem_remove(entry, work, config["run_id"]))
            except (OSError, ValueError, KeyError, RuntimeError,
                    subprocess.SubprocessError) as exc:
                errors.append(f"{entry['dev']}: {exc!r}")
        if errors:
            raise RuntimeError("; ".join(errors))
        marker.unlink()
        return {"netem": "off", "devices": removed}
    # A marker can outlive its run after a crash.  The install below proves each
    # device is free (default qdisc, no filters), so a leftover marker from
    # another run is stale bookkeeping rather than a live owner.
    if marker.exists():
        try:
            stale = json.loads(marker.read_text())
        except (OSError, ValueError):
            stale = None
        if not isinstance(stale, dict) or stale.get("run_id") != config["run_id"]:
            marker.unlink()
    entries = []
    for spec in netem_specs(config):
        if spec["dst"] is not None and ipaddress.ip_address(spec["dst"]).version != 4:
            raise ValueError("P2P netem currently requires IPv4")
        handle, child = _netem_handles(config["run_id"], spec["dev"])
        entries.append({**spec, "handle": handle, "child": child})
    if not entries:
        return {"netem": "off", "reason": "no devices matched"}
    # Write the plan before applying it, so a hard kill mid-install still leaves a
    # marker that the next teardown can find and unwind.
    write_private(marker, json.dumps({"run_id": config["run_id"],
                                      "devices": [{"dev": e["dev"], "handle": e["handle"],
                                                   "dst": e["dst"], "ports": e["ports"]}
                                                  for e in entries]}) + "\n")
    installed: list[dict] = []
    try:
        for entry in entries:
            _netem_install(entry, config, work)
            installed.append(entry)
    except BaseException:
        for entry in installed:
            try:
                _netem_remove(entry, work, config["run_id"])
            except Exception:
                pass
        marker.unlink(missing_ok=True)
        raise
    return {"netem": "on", "run_id": config["run_id"],
            "devices": [{"dev": e["dev"], "handle": e["handle"],
                         "dst": e["dst"] or "loopback", "ports": len(e["ports"])}
                        for e in entries],
            "delay_ms": config["delay_ms"], "jitter_ms": config.get("jitter_ms", 0),
            "loss": config.get("loss_percent", 0)}


def worker_rpc(config: dict) -> dict:
    return {"result": rpc(node_rpc_port(config["node"]), config["method"],
                          config.get("params", []),
                          timeout=120 if config["method"].startswith("eth_start") else 5)}


def worker_committee(config: dict) -> dict:
    """Run committee-mvp nodes on the ordinary host, IPC-attached to node1.
    committee-mvp only supports chain_rpc_mode=ipc, so it MUST be co-located
    with the geth whose IPC it uses (node1 on the master)."""
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    ipc = work / "chain" / "node1" / "geth.ipc"
    if not ipc.exists():
        raise RuntimeError("node1 IPC not present; start node1 first")
    committee_bin = root / "bin" / "committee-mvp"
    if not committee_bin.is_file():
        raise FileNotFoundError(committee_bin)
    count = config["committee_count"]
    start_block = int(rpc(node_rpc_port(1), "eth_blockNumber"), 16)
    started = []
    for i in range(1, count + 1):
        cfg = {"node_id": f"node-{i}", "listen_addr": f"127.0.0.1:{3400+i}",
               "static_nodes": [f"node-{j}" for j in range(1, count + 1)],
               "static_node_addrs": {f"node-{j}": f"127.0.0.1:{3400+j}" for j in range(1, count + 1)},
               "control_addr": f"127.0.0.1:{4400+i}", "chain_rpc_mode": "ipc",
               "geth_ipc_path": str(ipc), "vrf_contract_address": config["contract"],
               "vrf_submitter_address": config["submitter"], "vrf_start_block": start_block,
               "committee_size": count, "threshold": config["committee_threshold"],
               "coordinator_id": "node-1", "domain_separation": "committee-sig/mvp/v1",
               "message_version": "v1", "attestation_provider": config.get("attestation", "noop")}
        cfg_path = work / "committee-configs" / f"node-{i}.json"
        cfg_path.write_text(json.dumps(cfg, indent=2) + "\n")
        env = {**os.environ,
               "TGOVERN_COMM_STATS": "1" if config.get("comm_stats") else "0",
               "TGOVERN_COMM_NODE": f"committee-{i}"}
        spawn(work, f"committee-{i}",
              [str(committee_bin), "node", "--config", str(cfg_path)], env, root)
        started.append(i)
    return {"committee": started}


# The merged geth-node-*.log files are consumed by exactly two scrapers:
#   * sgx_l5.dkrs_extract_progress -> the two "TEE host ... DKRS" markers;
#   * collect-l5-timing.load_geth_records -> "E2E_TIMING" lines carrying one of
#     its RELEVANT_GETH_MARKERS, and load_teehost_records -> the DKRS markers.
# A run under load (e.g. --on-tx-rate 20, ~10s blocks) writes ~3GB of per-tx /
# per-block INFO telemetry of which those lines are <1%; the rest is discarded
# by both readers. Ship only the relevant lines (one compact copy of the
# oracle, no filter drift) so neither the transfer nor the archive scales with
# raw log volume -- the per-host originals stay untouched.
LOG_TRANSFER_MARKERS = (
    "TEE host starting DKRS",
    "TEE host extracted DKRS",
    "E2E_TIMING",
)
LOG_TRANSFER_RELEVANT = (
    "miner_task_queued_for_sealing",
    "insert_chain_total",
    "stage=edit_execution",
    "stage=ledger_update",
    "stage=rotation_flow",
)


def log_line_relevant(line: str) -> bool:
    if any(marker in line for marker in LOG_TRANSFER_MARKERS[:2]):
        return True
    if "E2E_TIMING" in line and any(marker in line for marker in LOG_TRANSFER_RELEVANT):
        return True
    return False


def worker_fetch_logs(config: dict) -> dict:
    """Return the collector-relevant subset of this run's node logs (base64) so
    the master can assemble one dir for sgx_l5's log-scraping readiness checks
    and the timing collector (both read geth-node-*.log). Streamed line-by-line:
    only relevant lines are buffered, never the raw multi-GB log."""
    import base64
    work = run_dir(Path(config["root"]), config["run_id"])
    logs = {}
    for path in sorted((work / "logs").glob("node*.log")):
        index = path.stem.removeprefix("node")
        kept = bytearray()
        with path.open("r", encoding="utf-8", errors="ignore") as handle:
            for line in handle:
                if log_line_relevant(line):
                    kept += line.encode("utf-8")
        logs[f"geth-node-{index}.log"] = base64.b64encode(bytes(kept)).decode()
    return {"logs": logs}


def aggregate_logs(local: Host, remote: Host, run_id: str, dest: Path) -> None:
    """Refresh `dest` with both hosts' relevant node-log lines named
    geth-node-{i}.log, so the timing collector sees every node, including the
    TEE nodes that run on the second host. A remote transfer error propagates
    rather than silently leaving a stale/partial view (which once masked a full
    DKRS success as extracted=0)."""
    import base64
    dest.mkdir(parents=True, exist_ok=True)
    for host in (local, remote):
        payload = host.call("fetch_logs", run_id=run_id)["logs"]
        for name, blob in payload.items():
            (dest / name).write_bytes(base64.b64decode(blob))


def _grep_matching_files(files: list[str], args: list[str]) -> set[str]:
    if not files:
        return set()
    completed = subprocess.run(["grep", *args, "--", *files], stdout=subprocess.PIPE,
                               stderr=subprocess.DEVNULL, text=True)
    # grep exits 1 (not 0) when nothing matches, which is the common early-poll
    # case; anything else is a real failure that must not look like "not ready".
    if completed.returncode not in (0, 1):
        raise RuntimeError(f"grep {' '.join(args)} failed with code {completed.returncode}")
    return {line for line in completed.stdout.splitlines() if line}


def worker_dkrs_progress(config: dict) -> dict:
    """Count this run's nodes that reached the epoch's DKRS start / extract
    markers. Mirrors sgx_l5.dkrs_extract_progress semantics (a node counts as
    extracted only if it also logged the epoch's start), but via grep -l on the
    node logs. The readiness poll runs every few seconds for minutes; scanning
    every line in Python there costs minutes of CPU and -- by base64-ing the
    whole log set back to the master -- is what OOM-killed a run. grep -l is
    ~1s for the whole topology and transfers only filenames."""
    work = run_dir(Path(config["root"]), config["run_id"])
    epoch = int(config["epoch"])
    files = sorted(str(path) for path in (work / "logs").glob("node*.log"))
    started = _grep_matching_files(
        files, ["-lE", f"TEE host starting DKRS.*oldEpoch={epoch - 1} newEpoch={epoch}"])
    extracted = _grep_matching_files(files, ["-lF", "TEE host extracted DKRS"])
    return {"started": len(started), "extracted": len(started & extracted)}


def worker_status(config: dict) -> dict:
    work = run_dir(Path(config["root"]), config["run_id"])
    if not (work / "run").is_dir():
        raise FileNotFoundError(f"run not prepared: {work}")
    status = {}
    for record in (work / "run").glob("*.json"):
        info = json.loads(record.read_text())
        pid = int(info["pid"])
        try:
            cmdline = Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\x00", b" ").decode()
            start, state = process_info(pid)
            status[record.stem] = start == info["start"] and state != "Z" and str(work) + "/" in cmdline
        except FileNotFoundError:
            status[record.stem] = False
    return {"processes": status}


def worker_stop(config: dict) -> dict:
    work = run_dir(Path(config["root"]), config["run_id"])
    if config.get("owner"):
        try:
            owner = json.loads((work / "owner.json").read_text())["owner"]
        except FileNotFoundError:
            return {"stopped": [], "skipped": "workspace not owned"}
        if owner != config["owner"]:
            return {"stopped": [], "skipped": "workspace not owned"}
    stopped, errors = [], []
    records = sorted((work / "run").glob("committee-*.json"), reverse=True)
    records += sorted((work / "run").glob("node*.json"), reverse=True)
    records += sorted((work / "run").glob("tunnel-*.json"), reverse=True)
    for record in records:
        try:
            info = json.loads(record.read_text())
            pid = int(info["pid"])
            start, state = process_info(pid)
            if start != info["start"]:
                raise RuntimeError(f"PID {pid} reused; refusing to kill")
            if state != "Z":
                cmdline = Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\x00", b" ").decode()
                if str(work) + "/" not in cmdline:
                    raise RuntimeError(f"PID {pid} does not belong to run {work}")
                for sig, budget in ((signal.SIGTERM, 8), (signal.SIGKILL, 4)):
                    try:
                        os.kill(pid, sig)
                    except ProcessLookupError:
                        break
                    deadline = time.monotonic() + budget
                    while time.monotonic() < deadline:
                        try:
                            new_start, new_state = process_info(pid)
                            if new_start != start or new_state == "Z":
                                break
                        except FileNotFoundError:
                            break
                        time.sleep(0.1)
                    else:
                        continue
                    break
                else:
                    raise RuntimeError(f"PID {pid} did not exit after SIGKILL")
                stopped.append(record.stem)
        # FileNotFoundError: /proc entry gone. ProcessLookupError (ESRCH): the
        # process exited between the process_info read and the /proc/<pid>/cmdline
        # read, so the kernel rejects the cmdline fetch. Both mean "already gone".
        except (FileNotFoundError, ProcessLookupError):
            pass
        except (OSError, ValueError, KeyError, TypeError, RuntimeError) as exc:
            errors.append(f"{record.name}: {exc}")
            continue
        record.unlink()
    if (work / "netem.json").exists():
        try:
            worker_netem({**config, "off": True})
        except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as exc:
            errors.append(f"netem cleanup: {exc}")
    if errors:
        raise RuntimeError("; ".join(errors))
    return {"stopped": stopped}


def worker_communication_prepare(config: dict) -> dict:
    work = run_dir(Path(config["root"]), config["run_id"])
    if not work.is_dir():
        raise FileNotFoundError(work)
    return communication_stats.prepare_archive(work, config["run_id"])


def worker_communication_chunk(config: dict) -> dict:
    import base64
    name = config["name"]
    if not re.fullmatch(r"(?:node\d+|committee-\d+|driver[\w-]*)\.log\.gz", name):
        raise ValueError("invalid communication snapshot name")
    offset, size = int(config["offset"]), int(config["size"])
    if offset < 0 or not 1 <= size <= 1024 * 1024:
        raise ValueError("invalid communication chunk range")
    work = run_dir(Path(config["root"]), config["run_id"])
    with (work / "communication-snapshots" / config["run_id"] / name).open("rb") as handle:
        handle.seek(offset)
        data = handle.read(size)
    return {"data": base64.b64encode(data).decode()}


WORKER_ACTIONS = {"prepare": worker_prepare, "start": worker_start,
                  "rpc": worker_rpc, "committee": worker_committee,
                  "fetch_logs": worker_fetch_logs, "netem": worker_netem,
                  "dkrs_progress": worker_dkrs_progress,
                  "communication_prepare": worker_communication_prepare,
                  "communication_chunk": worker_communication_chunk,
                  "status": worker_status, "stop": worker_stop}


def worker_main() -> None:
    try:
        request = json.load(sys.stdin)
        os.environ["TGOVERN_COMM_STATS"] = "1" if request.get("comm_stats") else "0"
        if request.get("run_id") and request.get("comm_stats"):
            globals()["COMM_RPC_LOG"] = run_dir(Path(request["root"]), request["run_id"]) / "logs" / "driver-worker-comm.log"
        result = WORKER_ACTIONS[request.pop("action")](request)
        print(json.dumps({"ok": True, **result}))
    except Exception as exc:
        print(json.dumps({"ok": False, "error": str(exc)}))
        sys.exit(1)


class Host:
    def __init__(self, root: Path, ssh_host: str | None = None):
        self.root = root
        self.ssh_host = ssh_host

    def call(self, action: str, **fields):
        script = self.root / "scripts" / "tgovern_multi_host.py"
        request = {"action": action, "root": str(self.root),
                   "comm_stats": communication_stats.enabled(), **fields}
        if self.ssh_host:
            argv = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                    self.ssh_host, f"python3 {shlex.quote(str(script))} --worker"]
        else:
            argv = [sys.executable, str(script), "--worker"]
        completed = subprocess.run(argv, input=json.dumps(request), text=True,
                                   capture_output=True, timeout=600)
        try:
            response = json.loads(completed.stdout)
        except ValueError as exc:
            raise RuntimeError(f"{action} on {self.ssh_host or 'local'} failed: "
                               f"{completed.stderr[-1500:]}") from exc
        if completed.returncode or not response.get("ok"):
            raise RuntimeError(f"{action} on {self.ssh_host or 'local'}: "
                               f"{response.get('error', completed.stderr[-1500:])}")
        return response


def ssh_tunnel(ssh_host: str, forwards: list[tuple[int, int]],
               log_path: Path) -> subprocess.Popen:
    """Forward all remote RPC ports through one SSH connection."""
    if not forwards:
        raise ValueError("SSH tunnel needs at least one port forward")
    argv = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
            "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15",
            "-o", "ServerAliveCountMax=3", "-E", str(log_path.resolve()), "-N"]
    for forward_port, remote_port in forwards:
        assert_port_free(forward_port, "127.0.0.1")
        argv += ["-L", f"127.0.0.1:{forward_port}:127.0.0.1:{remote_port}"]
    argv.append(ssh_host)
    with log_path.open("w") as log:
        return subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=log,
                                stderr=subprocess.STDOUT, start_new_session=True)


def check_ssh_tunnel(tunnel: subprocess.Popen, log_path: Path) -> None:
    returncode = tunnel.poll()
    if returncode is not None:
        detail = log_path.read_text(errors="replace")[-2000:].strip()
        raise RuntimeError(f"SSH RPC tunnel exited with code {returncode}; "
                           f"inspect {log_path}: {detail}")


def stop_ssh_tunnel(tunnel: subprocess.Popen) -> None:
    if tunnel.poll() is None:
        tunnel.terminate()
    try:
        tunnel.wait(timeout=5)
    except subprocess.TimeoutExpired:
        tunnel.kill()
        tunnel.wait(timeout=5)


def enode_for_ip(raw: str, ip: str, port: int) -> str:
    address = ipaddress.ip_address(ip)
    host = f"[{address}]" if address.version == 6 else str(address)
    prefix = raw.split("@", 1)[0]
    if not re.fullmatch(r"enode://[0-9a-fA-F]{128}", prefix):
        raise ValueError("invalid enode public key")
    return f"{prefix}@{host}:{port}"


def peer_matches(peers: object, node_id: str) -> bool:
    expected = node_id.removeprefix("0x").lower()
    return any(str(peer.get("id", "")).removeprefix("0x").lower() == expected
               for peer in (peers or []) if isinstance(peer, dict))


def connect_topology(topo: dict, node_port, info: dict, enodes: dict, check) -> None:
    """Dial only planned neighbors, then verify their identities (not counts)."""
    def add_peer(src, dst):
        if rpc(node_port(src), "admin_addPeer", [enodes[dst]]) is False:
            raise RuntimeError(f"node{src} rejected node{dst}")

    with ThreadPoolExecutor(max_workers=32) as pool:
        futures = [pool.submit(add_peer, src, dst)
                   for a, b in topo["edges"] for src, dst in ((a, b), (b, a))]
        try:
            for future in as_completed(futures):
                future.result()
        finally:
            for future in futures:
                future.cancel()
    for i, targets in topo["neighbors"].items():
        def ready(i=i, targets=targets):
            peers = rpc(node_port(i), "admin_peers")
            return all(peer_matches(peers, info[j]["id"]) for j in targets)
        wait_until(f"node{i} peers", ready, 120 + len(targets) * 15, check=check)


def resolve_runtime(args) -> dict:
    """Resolve bootstrap/steady and geth controls once using the original driver."""
    if args.light_nodes < 0:
        raise ValueError("light node count must be nonnegative")
    topo = topology.build_topology(
        1 + args.tee_count + args.light_nodes, mode=getattr(args, "topology", "sparse"),
        radius=getattr(args, "topology_radius", 2),
        max_degree=getattr(args, "topology_max_degree", 9),
        relay_nodes=range(2, 2 + args.tee_count))
    relay_ttl = max(4, topo["relay_ttl_min"])
    env = dict(os.environ)
    env.update({
        "TGOVERN_ROOT": str(args.local_root),
        "TEE_CANDIDATE_COUNT": str(args.tee_count),
        "TEE_ACTIVE_LIMIT": str(args.active_limit),
        "TEE_ACTIVE_THRESHOLD": str(args.active_threshold),
        "TEE_EXTRA_MINERS": "0", "TEE_ON_OBSERVERS": "0",
        "LIGHT_NODE_COUNT": str(args.light_nodes),
        "COMMITTEE_COUNT": str(args.committee_count),
        "COMMITTEE_THRESHOLD": str(args.committee_threshold),
        "EPOCH_LENGTH": str(args.epoch_length),
        # Steady epochs default to DEFAULT_STEADY_EPOCH so post-rotation waits
        # track the same ~10s-block budget as bootstrap. An explicit
        # TEE_STEADY_EPOCH_LENGTH/STEADY_EPOCH_LENGTH in the environment still wins.
        **({"STEADY_EPOCH_LENGTH": str(DEFAULT_STEADY_EPOCH)}
           if not (env.get("TEE_STEADY_EPOCH_LENGTH", "").strip()
                   or env.get("STEADY_EPOCH_LENGTH", "").strip()) else {}),
        # TEE_BLOCKS_PER_REGISTERED_NODE is what sets MIN_EPOCH_LENGTH, i.e. the
        # floor a short --epoch-length is raised to. 0 keeps sgx_l5's own default
        # so historical invocations stay byte-identical.
        **({"TEE_BLOCKS_PER_REGISTERED_NODE": str(getattr(args, "blocks_per_registered_node", 0))}
           if getattr(args, "blocks_per_registered_node", 0) else {}),
        # Record and apply the operator's block-time estimate so the probe's
        # reported effective value matches what drive_l5 hands the library.
        **({"EST_SECONDS_PER_BLOCK": str(args.est_seconds_per_block)}
           if getattr(args, "est_seconds_per_block", 0.0) else {}),
        "SGX_MODE": args.sgx_mode,
        "P2P_MESSAGE_RELAY_TTL": str(max(relay_ttl, int(env.get("P2P_MESSAGE_RELAY_TTL", relay_ttl)))),
    })
    spec = importlib.util.spec_from_file_location(
        "l5_probe", args.local_root / "scripts" / "sgx_l5_sim_e2e.py")
    module = importlib.util.module_from_spec(spec)
    with patch_environ(env):
        spec.loader.exec_module(module)
    if module.TEE_CORE_ISOLATION:
        raise ValueError("TEE_CORE_ISOLATION is not supported across hosts; unset it")
    return {
        "epoch_length": max(module.EPOCH_LENGTH, module.MIN_EPOCH_LENGTH),
        "min_epoch_length": module.MIN_EPOCH_LENGTH,
        "blocks_per_registered_node": module.BLOCKS_PER_REGISTERED_NODE,
        "steady_epoch_length": module.STEADY_EPOCH_LENGTH,
        "est_seconds_per_block": module.EST_SECONDS_PER_BLOCK,
        "active_limit": args.active_limit, "active_threshold": args.active_threshold,
        "sgx_mode": args.sgx_mode, "gomaxprocs": args.gomaxprocs,
        "topology": topo,
        # Both endpoints register each edge as static: allow every neighbor to dial.
        "maxpeers": max(module.GETH_MAX_PEERS, 3 * topo["max_degree"]),
        "p2p_msg_delay_ms": module.GETH_P2P_MSG_DELAY_MS,
        "block_broadcast_limit": module.GETH_BLOCK_BROADCAST_LIMIT,
        "block_broadcast_limit_tee": module.GETH_BLOCK_BROADCAST_LIMIT_TEE,
        "distkg_relay_ttl": max(relay_ttl, module.DISTKG_RELAY_TTL),
        "dkrs_relay_ttl": max(relay_ttl, module.DKRS_RELAY_TTL),
        "txmodify_relay_ttl": max(relay_ttl, module.TXMODIFY_RELAY_TTL),
        "dkrs_relay_max_peers": max(1, topo["max_degree"]),
        "core_cache_mb": module.CORE_NODE_CACHE_MB,
        "headwatch": os.environ.get("TEE_HEADWATCH") == "1",
        "geth_env": {k: os.environ[k] for k in ("SGX_SDK", "GOGC") if k in os.environ},
    }


def validate_system_params(runtime: dict) -> dict:
    params = rpc(node_rpc_port(1), "eth_getSystemParams")
    expected = {"ConfiguredEpochLength": runtime["epoch_length"],
                "ActiveLimit": runtime["active_limit"],
                "ActiveThreshold": runtime["active_threshold"]}
    for field, value in expected.items():
        raw = params[field]
        actual = int(raw, 16 if str(raw).lower().startswith("0x") else 10) if isinstance(raw, str) else int(raw)
        if actual != value:
            raise AssertionError(f"deployed {field}: actual={actual}, expected={value}; params={params}")
    return params


class RunCancelled(BaseException):
    def __init__(self, signum):
        self.signum = signum
        super().__init__(f"run cancelled by signal {signum}")


@contextmanager
def cancellation_handlers():
    def cancel(signum, _frame):
        raise RunCancelled(signum)
    saved = {sig: signal.signal(sig, cancel) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for sig, handler in saved.items():
            signal.signal(sig, handler)


def command_start(args) -> None:
    with cancellation_handlers():
        _command_start(args)


def _command_start(args) -> None:
    if not args.local_root.is_absolute() or not args.remote_root.is_absolute():
        raise ValueError("package roots must be absolute paths")
    for ip in (args.local_ip, args.remote_ip):
        ipaddress.ip_address(ip)
    if not args.tee_count >= args.active_limit >= args.active_threshold >= 1:
        raise ValueError("need tee_count >= active_limit >= active_threshold >= 1")

    from eth_account import Account
    tee_indices = list(range(2, 2 + args.tee_count))  # node2.. on the TEE host
    light_start = 2 + args.tee_count
    light_indices = list(range(light_start, light_start + args.light_nodes))
    # Light ordinary nodes default to host A (the ordinary master), keeping the
    # TEE host's CPU for the SGX-sensitive work; --light-remote puts them on B.
    light_on_local = not args.light_remote
    local_lights = light_indices if light_on_local else []
    remote_lights = [] if light_on_local else light_indices
    all_nodes = [1] + tee_indices + light_indices
    # Nodes must use the epoch length sgx_l5 will actually run with (it raises
    # short values to its safe floor), else ConfiguredEpochLength mismatches.
    runtime = resolve_runtime(args)
    if args.blocks_per_registered_node:
        print(f"blocks-per-registered-node override {args.blocks_per_registered_node} "
              f"(sgx_l5 default 12); minEpochLength={runtime['min_epoch_length']}", flush=True)
    if runtime["epoch_length"] != args.epoch_length:
        print(f"epoch_length raised {args.epoch_length} -> {runtime['epoch_length']} "
              f"(sgx_l5 floor {runtime['min_epoch_length']}); "
              f"steady={runtime['steady_epoch_length']}", flush=True)
    args.runtime = runtime
    if not args.committee_count >= args.committee_threshold >= 1:
        raise ValueError("need committee_count >= committee_threshold >= 1")

    local = Host(args.local_root)
    remote = Host(args.remote_root, args.remote_ssh)
    run_id = args.run_id or (datetime.datetime.now(datetime.timezone.utc).strftime("tee-%Y%m%d-%H%M%S")
                             + "-" + secrets.token_hex(3))
    work = run_dir(args.local_root, run_id)
    out_dir = args.local_root / "out" / run_id
    out_dir.mkdir(parents=True, exist_ok=False)
    (out_dir / "configuration.json").write_text(json.dumps(
        {"requested_epoch_length": args.epoch_length, "effective": runtime,
         "pow": {"genesis_difficulty": str(args.genesis_difficulty),
                 "on_tx_rate": args.on_tx_rate, "tx_per_block": args.tx_per_block,
                 "est_seconds_per_block": args.est_seconds_per_block}},
        indent=2) + "\n")
    owner = secrets.token_hex(16)
    attempted_hosts = []
    primary_error = None
    cleanup_errors = []
    keys = {str(i): Account.create().key.hex().removeprefix("0x") for i in all_nodes}
    addresses = {i: Account.from_key(keys[str(i)]).address for i in all_nodes}
    # Background ON tx load: one load account + TEE addresses as receivers. Only
    # materialised when the load is enabled, so the historical (load-off) genesis
    # alloc stays byte-identical.
    on_load_accounts: list[dict[str, str]] = []
    if args.on_tx_rate > 0:
        acct = Account.create()
        on_load_accounts.append({"address": acct.address,
                                 "privateKey": acct.key.hex().removeprefix("0x")})
    genesis = json.loads((args.local_root / "genesis.json").read_text())
    genesis["alloc"] = {addresses[i].removeprefix("0x").lower():
                        {"balance": "1000000000000000000000"} for i in all_nodes}
    for acct in on_load_accounts:
        genesis["alloc"][acct["address"].removeprefix("0x").lower()] = {
            "balance": "1000000000000000000000"}
    if str(args.genesis_difficulty) not in ("", "0"):
        raw = str(args.genesis_difficulty)
        try:
            difficulty = int(raw, 16) if raw.lower().startswith("0x") else int(raw, 10)
        except ValueError as err:
            raise ValueError(f"--genesis-difficulty must be hex 0x.. or decimal, got {raw!r}") from err
        if difficulty < 1:
            raise ValueError(f"--genesis-difficulty must be >= 1, got {difficulty}")
        genesis["difficulty"] = hex(difficulty)
        print(f"genesis difficulty -> {genesis['difficulty']} ({difficulty})", flush=True)
    network_id = genesis.get("config", {}).get("chainId", 739205)
    tunnels: list[subprocess.Popen] = []
    print(f"run_id={run_id} tees={tee_indices} committee={args.committee_count}/"
          f"{args.committee_threshold} active={args.active_limit}/{args.active_threshold} "
          f"rotations={args.rotations} baseline_edits={args.baseline_edits} "
          f"sgx={args.sgx_mode}", flush=True)

    if communication_stats.enabled():
        expected = [f"local/node{i}.log.gz" for i in [1] + local_lights]
        expected += [f"remote/node{i}.log.gz" for i in tee_indices + remote_lights]
        expected += [f"local/committee-{i}.log.gz" for i in range(1, args.committee_count + 1)]
        (out_dir / "communication-expected.json").write_text(
            json.dumps({"expected_sources": expected}, indent=2) + "\n")
    try:
        # 1) prepare both hosts (light nodes follow their host assignment)
        attempted_hosts.append(local)
        local.call("prepare", run_id=run_id, owner=owner, nodes=[1] + local_lights, genesis=genesis,
                   keys={str(i): keys[str(i)] for i in [1] + local_lights},
                   on_load_accounts=on_load_accounts)
        globals()["COMM_RPC_LOG"] = work / "logs" / "driver-master-comm.log"
        attempted_hosts.append(remote)
        remote.call("prepare", run_id=run_id, owner=owner, nodes=tee_indices + remote_lights, genesis=genesis,
                    keys={str(i): keys[str(i)] for i in tee_indices + remote_lights})

        # 2) start node1 (ordinary miner, contract deployer)
        local.call("start", run_id=run_id, node=1, network_id=network_id, mine=True,
                   on_load_accounts=on_load_accounts, **runtime)
        wait_until("node1 RPC", lambda: rpc(node_rpc_port(1), "eth_blockNumber"), 90)
        contract = wait_until("contract deploy", lambda: (
            json.loads((work / "node-work" / "node1" / "storage.json").read_text()).get("address")
            or None), 240)
        if not re.fullmatch(r"0x[0-9a-fA-F]{40}", contract):
            raise RuntimeError(f"bad contract address: {contract}")
        wait_until("contract bytecode", lambda: rpc(node_rpc_port(1), "eth_getCode",
                   [contract, "latest"]) not in (None, "0x", "0x0", ""), 120)
        params = validate_system_params(runtime)
        print(f"contract={contract}; deployed params={params}", flush=True)

        # 3) committee-mvp on the ordinary host, IPC to node1 (committee-mvp only
        # supports chain_rpc_mode=ipc, so it must share the node1 machine).
        local.call("committee", run_id=run_id, contract=contract,
                   submitter=addresses[1],
                   committee_count=args.committee_count,
                   committee_threshold=args.committee_threshold,
                   attestation=args.attestation)
        def committee_pubkey():
            payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "committee_getPublicKey",
                                  "params": {}}).encode()
            import urllib.request
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            request = urllib.request.Request("http://127.0.0.1:4401/rpc", payload,
                                             {"Content-Type": "application/json"})
            try:
                with opener.open(request, timeout=5) as response:
                    result = json.load(response)
                return result.get("result") or None
            except (OSError, ValueError):
                return None
        wait_until("committee RPC (DKG aggregate key)", committee_pubkey, 180)

        # 4) TEE nodes on the TEE host
        for i in tee_indices:
            remote.call("start", run_id=run_id, node=i, network_id=network_id,
                        teehost=True, contract=contract, eventpoll=args.eventpoll, **runtime)
        # 4b) light nodes on their assigned host
        for i in local_lights:
            local.call("start", run_id=run_id, node=i, network_id=network_id, light=True,
                       contract=contract, light_gomaxprocs=args.light_gomaxprocs,
                       light_cache_mb=args.light_cache_mb, **runtime)
        for i in remote_lights:
            remote.call("start", run_id=run_id, node=i, network_id=network_id, light=True,
                        contract=contract, light_gomaxprocs=args.light_gomaxprocs,
                        light_cache_mb=args.light_cache_mb, **runtime)

        # 5) One SSH connection carries all remote-host RPC port forwards,
        # avoiding sshd MaxStartups throttling from parallel authentication.
        same_host = args.remote_ip == args.local_ip
        remote_all = tee_indices + remote_lights
        tunnel_log = work / "logs" / "tunnel-rpc.log"
        if not same_host and remote_all:
            forwards = [(fwd_rpc_port(i), node_rpc_port(i)) for i in remote_all]
            tunnels.append(ssh_tunnel(args.remote_ssh, forwards, tunnel_log))
            write_private(work / "run" / "tunnel-rpc.json", json.dumps({
                "pid": tunnels[-1].pid, "start": process_info(tunnels[-1].pid)[0]}) + "\n")
            print(f"SSH RPC tunnel: 1 connection, {len(forwards)} port forwards; "
                  f"log={tunnel_log}", flush=True)

        def check_tunnels():
            for tunnel in tunnels:
                check_ssh_tunnel(tunnel, tunnel_log)

        remote_set = set(remote_all)
        def node_port(i: int) -> int:
            check_tunnels()
            if i == 1 or same_host or i not in remote_set:
                return node_rpc_port(i)
            return fwd_rpc_port(i)

        for i in all_nodes:
            wait_until(f"node{i} RPC", lambda p=node_port(i): rpc(p, "eth_blockNumber"),
                       120, check=check_tunnels)

        # 6) bounded-degree peering (full mesh remains an explicit option)
        topo = runtime["topology"]
        print(f"topology={topo['mode']} nodes={topo['total']} edges={topo['edge_count']} "
              f"max_degree={topo['max_degree']} diameter={topo['diameter']} "
              f"tee_diameter={topo['relay_diameter']} "
              f"relay_ttl={runtime['distkg_relay_ttl']}/{runtime['dkrs_relay_ttl']}/"
              f"{runtime['txmodify_relay_ttl']}", flush=True)
        info = {i: rpc(node_port(i), "admin_nodeInfo") for i in all_nodes}
        enodes = {i: enode_for_ip(info[i]["enode"],
                                  args.local_ip if i in [1] + local_lights else args.remote_ip,
                                  p2p_port(i)) for i in all_nodes}
        connect_topology(topo, node_port, info, enodes, check_tunnels)

        # 6b) netem on BOTH hosts: cross-host on the ethernet device matched by
        # peer IP, same-host on loopback matched by port.  Only geth P2P and
        # committee sockets are delayed; RPC, control and SSH stay untouched.
        if args.netem_delay > 0:
            loopback_dev = None if args.no_netem_lo else args.netem_lo_dev
            # Dedup: on a single-host topology both matches would share a device.
            loopback_ports = sorted({p2p_port(i) for i in all_nodes}
                                    | {committee_port(i) for i in range(1, args.committee_count + 1)})
            for host, peer_ip in ((local, args.remote_ip), (remote, args.local_ip)):
                result = host.call("netem", run_id=run_id, dev=args.netem_dev, peer=peer_ip,
                                   loopback_dev=loopback_dev, loopback_ports=loopback_ports,
                                   delay_ms=args.netem_delay, jitter_ms=args.netem_jitter,
                                   loss_percent=args.netem_loss, limit=args.netem_limit,
                                   p2p_ports=[p2p_port(i) for i in all_nodes])
                print(f"netem: {host.ssh_host or 'local'} {result}", flush=True)

        # 7) drive the L5 flow with sgx_l5_sim_e2e as a library
        print(">>> [l5] registration -> pkch -> edits -> rotations (original flow)", flush=True)
        load_proc = None
        if args.on_tx_rate > 0:
            load_proc = start_on_tx_load(args, run_id, work, all_nodes, addresses, on_load_accounts)
        try:
            rc = drive_l5(args, run_id, work, out_dir, tee_indices, node_port, addresses, contract,
                          local, remote)
        finally:
            if load_proc is not None:
                stop_owned_process(load_proc)
        flow_ok = rc == 0
        # Keep failed-flow evidence, but never mask its error with collection.
        if not flow_ok:
            if not args.no_collect:
                try:
                    collect_timing(args, local, remote, run_id, work, out_dir)
                except Exception as exc:
                    print(f"failed-flow collection: {exc!r}", file=sys.stderr)
            raise RuntimeError(f"L5 flow returned {rc}")
        if not args.no_collect:
            collect_timing(args, local, remote, run_id, work, out_dir)
    except BaseException as exc:
        primary_error = exc
        raise
    finally:
        saved_signals = {sig: signal.signal(sig, lambda *_: None)
                         for sig in (signal.SIGINT, signal.SIGTERM)}
        try:
            if not args.keep_running:
                print(">>> [stop] tearing down owned resources on both hosts", flush=True)
                cleanup_errors.extend(stop_hosts(local, remote, run_id, owner=owner,
                                                 hosts=attempted_hosts))
            else:
                print(f"nodes kept running; stop with 'stop --run-id {run_id}'", flush=True)
                if args.netem_delay > 0:
                    for host in attempted_hosts:
                        try:
                            host.call("netem", run_id=run_id, owner=owner, off=True)
                        except Exception as exc:
                            cleanup_errors.append(f"netem {host.ssh_host or 'local'}: {exc!r}")
            if communication_stats.enabled() and not args.no_collect:
                try:
                    collect_communication(args, local, remote, run_id, work, out_dir)
                except Exception as exc:
                    print(f"communication collection failed: {exc!r}", file=sys.stderr)
                    (out_dir / "communication-error.json").write_text(
                        json.dumps({"status": "unavailable", "error": repr(exc)}) + "\n")
            for tunnel in tunnels:
                try:
                    stop_ssh_tunnel(tunnel)
                except Exception as exc:
                    cleanup_errors.append(f"tunnel: {exc!r}")
            status = {"run_id": run_id,
                      "success": primary_error is None and not cleanup_errors and not args.keep_running,
                      "error": repr(primary_error) if primary_error else None,
                      "cleanup_errors": cleanup_errors}
            try:
                (out_dir / "run-status.json").write_text(json.dumps(status, indent=2) + "\n")
            except OSError as exc:
                cleanup_errors.append(f"status persistence: {exc!r}")
        finally:
            for sig, handler in saved_signals.items():
                signal.signal(sig, handler)
        if cleanup_errors:
            print(f"cleanup incomplete: {cleanup_errors}", file=sys.stderr)
            if primary_error is None:
                raise RuntimeError(f"cleanup incomplete: {cleanup_errors}")
    print(f"tee multi-host run complete: run_id={run_id}; artifacts: {out_dir}", flush=True)


def stop_hosts(local: "Host", remote: "Host", run_id: str, *, owner=None, hosts=None) -> list[str]:
    """Attempt every owned host, reporting all cleanup failures to the caller."""
    errors = []
    for host in reversed(hosts if hosts is not None else [local, remote]):
        try:
            result = host.call("stop", run_id=run_id, owner=owner)
            print(f"stopped {host.ssh_host or 'local'}: {result}", flush=True)
        except Exception as exc:
            errors.append(f"{host.ssh_host or 'local'}: {exc!r}")
    return errors


def stop_owned_process(process: subprocess.Popen) -> None:
    """Terminate only the Popen we started, never a broad process match."""
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def start_on_tx_load(args, run_id: str, work: Path, all_nodes: list[int],
                     addresses: dict, on_load_accounts: list[dict]) -> subprocess.Popen:
    """Run the background ON tx generator against node1 for the whole L5 flow.

    Multi-host stubs out the library's start_background_load, so the load is owned
    here: one unlocked load account drives normal txs to the TEE addresses, paced
    either by a fixed --on-tx-rate or by tx.py's closed loop (--tx-per-block) that
    re-measures the live block interval so the density tracks the target."""
    load_work = work / "node-work" / "on-load"
    accounts_path = load_work / "accounts.json"
    receivers_path = load_work / "receivers.json"
    accounts_path.write_text(json.dumps(on_load_accounts, indent=2) + "\n")
    receivers_path.write_text(json.dumps(
        [addresses[i] for i in all_nodes if i != 1], indent=2) + "\n")
    cmd = [sys.executable, str(args.local_root / "tx.py"),
           "--rpc", f"http://127.0.0.1:{node_rpc_port(1)}",
           "--accounts", str(accounts_path), "--receivers", str(receivers_path),
           "--rate", str(args.on_tx_rate),
           "--log", str(load_work / "on-tx-load.jsonl")]
    if args.tx_per_block > 0:
        cmd += ["--tx-per-block", str(args.tx_per_block)]
        # Give the loop the real block time so it does not clamp-and-saturate on
        # a slow PoW chain.
        hint = args.est_seconds_per_block or args.runtime["est_seconds_per_block"]
        if hint > 0:
            cmd += ["--block-interval-hint", str(hint)]
    # start_new_session detaches the load into its own process group so we can
    # stop it by pid, but that also lets it outlive a SIGKILL of this orchestrator
    # (e.g. the OOM) and keep sending txs at the next run's node. Hand it our pid
    # so it exits on its own if we disappear.
    cmd += ["--parent-pid", str(os.getpid())]
    output = (work / "logs" / "tx-load.log").open("w")
    process = subprocess.Popen(cmd, cwd=str(args.local_root), stdin=subprocess.DEVNULL,
                               stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
    print(f"background ON tx load: rate={args.on_tx_rate}/s "
          f"tx_per_block={args.tx_per_block or 'off'} pid={process.pid}", flush=True)
    return process


def collect_communication(args, local: "Host", remote: "Host", run_id: str,
                          work: Path, out_dir: Path) -> None:
    """One final snapshot per host; no communication payload in readiness polls."""
    import base64
    logs = out_dir / "communication-raw" / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    transfer_errors = []
    for label, host in (("local", local), ("remote", remote)):
        dest = logs / label
        dest.mkdir(exist_ok=True)
        try:
            snapshot = host.call("communication_prepare", run_id=run_id)
            for entry in snapshot["files"]:
                name, length = entry["name"], int(entry["size"])
                if Path(name).name != name or not name.endswith(".log.gz") or length < 0:
                    raise ValueError("invalid snapshot metadata")
                partial = dest / (name + ".part")
                with partial.open("wb") as handle:
                    offset = 0
                    while offset < length:
                        size = min(1024 * 1024, length - offset)
                        reply = host.call("communication_chunk", run_id=run_id,
                                          name=name, offset=offset, size=size)
                        data = base64.b64decode(reply["data"], validate=True)
                        if len(data) != size:
                            raise IOError(f"short snapshot chunk: {label}/{name}")
                        handle.write(data)
                        offset += len(data)
                partial.replace(dest / name)
        except Exception as exc:
            transfer_errors.append(f"{label}: {exc!r}")
    # Embedded L5 driver RPC telemetry is local and is not part of worker logs.
    driver = work / "aggregated-logs" / "driver-comm.log"
    if driver.is_file():
        shutil.copyfile(driver, logs / "driver-comm.log")
    command = [sys.executable, str(args.local_root / "scripts" / "collect-communication.py"),
               "--logs-dir", str(logs), "--out-dir", str(out_dir / "communication"),
               "--progress", str(work / "l5" / "e2e-progress.log"),
               "--expected-file", str(out_dir / "communication-expected.json")]
    with (out_dir / "communication-collector.log").open("w") as output:
        subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=True)
    if transfer_errors:
        (out_dir / "communication-transfer-errors.json").write_text(
            json.dumps(transfer_errors, indent=2) + "\n")
        print(f"partial communication transfer: {transfer_errors}", file=sys.stderr)


def collect_timing(args, local: "Host", remote: "Host", run_id: str,
                   work: Path, out_dir: Path) -> None:
    """Archive fresh raw evidence and require the timing collector to succeed."""
    collector = args.local_root / "scripts" / "collect-l5-timing.py"
    if not collector.is_file():
        raise FileNotFoundError(collector)
    raw_dir = out_dir / "raw"
    logs_dir = raw_dir / "logs"
    logs_dir.mkdir(parents=True, exist_ok=True)
    # Merge both hosts' node logs as geth-node-{i}.log (collector's expected name).
    aggregate_logs(local, remote, run_id, logs_dir)
    # The embedded driver uses a run-scoped progress file, not global scratch.
    progress_src = work / "l5" / "e2e-progress.log"
    progress_dst = raw_dir / "e2e-progress.log"
    if progress_src.is_file():
        shutil.copyfile(progress_src, progress_dst)
    else:
        raise FileNotFoundError(progress_src)
    timing_dir = out_dir / "timing"
    command = [sys.executable, str(collector),
               "--logs-dir", str(logs_dir), "--progress", str(progress_dst),
               "--out-dir", str(timing_dir), "--threshold", str(args.active_threshold)]
    try:
        with (out_dir / "collector.log").open("w") as output:
            subprocess.run(command, cwd=str(args.local_root), stdout=output,
                           stderr=subprocess.STDOUT, check=True)
        print(f"aggregated timing -> {timing_dir}", flush=True)
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(f"collect-l5-timing.py failed: {exc}; see {out_dir / 'collector.log'}") from exc


def drive_l5(args, run_id: str, work: Path, out_dir: Path, tee_indices: list[int],
             node_port, addresses: dict, contract: str,
             local: "Host", remote: "Host") -> int:
    """Import sgx_l5_sim_e2e as a library and run its business flow in-process.

    The nodes are already running (we started them), so prepare(), start_nodes()
    and cleanup() are stubbed out; node_rpc() is patched so TEE-node indices
    resolve to the SSH-tunnel ports on this master. Everything else --
    register_committee_pubkey, register_tees, wait_pkch, redactions,
    wait_rotation, post-rotation verification -- runs unchanged."""
    env = dict(os.environ)
    env.update({
        "TGOVERN_ROOT": str(args.local_root),
        "TEE_CANDIDATE_COUNT": str(args.tee_count),
        "TEE_ACTIVE_LIMIT": str(args.active_limit),
        "TEE_ACTIVE_THRESHOLD": str(args.active_threshold),
        "TEE_EXTRA_MINERS": "0",
        "TEE_ON_OBSERVERS": "0",
        "LIGHT_NODE_COUNT": str(args.light_nodes),
        "COMMITTEE_COUNT": str(args.committee_count),
        "COMMITTEE_THRESHOLD": str(args.committee_threshold),
        "TEE_ROTATIONS": str(args.rotations),
        "TEE_BASELINE_EDITS": str(args.baseline_edits),
        "TEE_EDITS_PER_EPOCH": str(args.edits_per_epoch),
        "ON_ACCOUNT_COUNT": "1",
        "ON_TX_PERS": str(args.on_tx_rate),
        "ON_TX_PER_BLOCK": str(args.tx_per_block),
        "EST_SECONDS_PER_BLOCK": str(args.est_seconds_per_block or args.runtime["est_seconds_per_block"]),
        "EPOCH_LENGTH": str(args.runtime["epoch_length"]),
        # Keep the in-process library's safe floor identical to the one
        # resolve_runtime measured, else it would re-raise the epoch length.
        # Unset means "no override": sgx_l5's default, or an inherited value the
        # probe also saw, stays in charge.
        **({"TEE_BLOCKS_PER_REGISTERED_NODE": str(args.blocks_per_registered_node)}
           if getattr(args, "blocks_per_registered_node", 0) else {}),
        "TEE_STEADY_EPOCH_LENGTH": str(args.runtime["steady_epoch_length"]),
        "STEADY_EPOCH_LENGTH": str(args.runtime["steady_epoch_length"]),
        "GETH_MAX_PEERS": str(args.runtime["maxpeers"]),
        "TEE_DISTKG_RELAY_TTL": str(args.runtime["distkg_relay_ttl"]),
        "TEE_DKRS_RELAY_TTL": str(args.runtime["dkrs_relay_ttl"]),
        "TXMODIFY_RELAY_TTL": str(args.runtime["txmodify_relay_ttl"]),
        "SGX_MODE": args.sgx_mode,
        "TEE_QUOTE_MODE": args.quote_mode,
        "TEE_ATTESTATION_PROVIDER": args.attestation,
    })
    spec = importlib.util.spec_from_file_location(
        "sgx_l5_sim_e2e", args.local_root / "scripts" / "sgx_l5_sim_e2e.py")
    l5 = importlib.util.module_from_spec(spec)
    with patch_environ(env):
        spec.loader.exec_module(l5)
    # Stub boot/cleanup: nodes are managed by this orchestrator, not by l5.
    # The stubbed prepare() would also have created l5.WORK; without it, log()
    # silently swallows the progress-file write (FileNotFoundError), leaving
    # collect-l5-timing without its progress input. Create it per run instead,
    # truncating so repeated runs never mix anchors from different executions.
    l5.WORK = work / "l5"
    l5.WORK.mkdir(parents=True, exist_ok=True)
    (l5.WORK / "e2e-progress.log").write_text("")
    l5.prepare = lambda: None
    l5.start_nodes = lambda: None
    l5.cleanup = lambda: None
    l5.apply_netem = lambda: None
    l5.start_background_load = lambda: None
    l5.stop_background_load = lambda: None
    # committee-mvp was already started by this orchestrator (IPC to node1); do
    # not let l5 regenerate configs / re-launch it from empty globals.
    l5.start_committee_nodes = lambda: None
    l5.launch_committee_background = lambda: None
    l5.join_committee_background = lambda: None
    # Seed the globals l5's business functions read, from what we started.
    l5.accounts = [{"address": addresses.get(i, ""), "privateKey": ""}
                   for i in range(1, l5.TOTAL_NODE_COUNT + 1)]
    l5.on_load_accounts = []
    l5.trusted_mrs = []
    l5.contract_address = contract
    # Tunnel-aware RPC resolver: TEE nodes live on forwarded ports.
    l5.node_rpc = lambda index: f"http://127.0.0.1:{node_port(index)}"
    # sgx_l5's readiness checks scrape geth-node-*.log under LOGS. TEE nodes run
    # on the other host, so replace dkrs_extract_progress with a cheap remote
    # grep over both hosts' per-node logs (only LOGS' geth-node-*.log files are
    # read via LOGS in sgx_l5, i.e. exclusively here). LOGS itself points at the
    # merged relevant-lines dir that collect_timing builds for the collector.
    agg = work / "aggregated-logs"
    agg.mkdir(parents=True, exist_ok=True)
    l5.LOGS = agg

    def dkrs_progress_remote(epoch, *_args, **_kwargs):
        started = 0
        extracted = 0
        for host in (local, remote):
            progress = host.call("dkrs_progress", run_id=run_id, epoch=epoch)
            started += progress["started"]
            extracted += progress["extracted"]
        return extracted, started
    l5.dkrs_extract_progress = dkrs_progress_remote
    # committee RPC stays local (committee-mvp runs on this host).
    with patch_environ(env):
        try:
            return l5.main(manage_lifecycle=False)
        except SystemExit as exc:
            return int(exc.code or 0)
        except Exception as exc:
            print(f"l5 flow failed: {exc!r}", file=sys.stderr)
            return 1


class patch_environ:
    """Context manager temporarily overlaying os.environ."""

    def __init__(self, overlay: dict):
        self.overlay = overlay

    def __enter__(self):
        self.saved = {k: os.environ.get(k) for k in self.overlay}
        os.environ.update(self.overlay)
        return self

    def __exit__(self, *exc):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        return False


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", action="store_true", help=argparse.SUPPRESS)
    sub = parser.add_subparsers(dest="command")
    start = sub.add_parser("start")
    stop = sub.add_parser("stop")
    for command in (start, stop):
        command.add_argument("--remote-ssh", required=True)
        command.add_argument("--local-root", type=Path, default=ROOT)
        command.add_argument("--remote-root", type=Path, default=Path("/opt/tgovern"))
        command.add_argument("--run-id")
    start.add_argument("--local-ip", required=True)
    start.add_argument("--remote-ip", required=True)
    start.add_argument("--tee-count", type=int, default=4)
    start.add_argument("--active-limit", type=int, default=4)
    start.add_argument("--active-threshold", type=int, default=3)
    start.add_argument("--committee-count", type=int, default=4)
    start.add_argument("--committee-threshold", type=int, default=3)
    start.add_argument("--rotations", type=int, default=1)
    start.add_argument("--baseline-edits", type=int, default=1)
    start.add_argument("--edits-per-epoch", type=int, default=1)
    start.add_argument("--epoch-length", type=int, default=DEFAULT_BOOTSTRAP_EPOCH,
                       help=f"bootstrap epoch length in blocks (default: {DEFAULT_BOOTSTRAP_EPOCH}, "
                            "sized for ~10s blocks and not inflated by the registration heuristic)")
    start.add_argument("--blocks-per-registered-node", type=int, default=1,
                       help="registration blocks budgeted per TEE in the sgx_l5 "
                            "min-epoch-length floor; 0 keeps its default (12). The "
                            "default 1 keeps the floor from inflating the epoch.")
    start.add_argument("--eventpoll", default="200ms")
    start.add_argument("--sgx-mode", choices=("HW", "SIM"), default="HW")
    start.add_argument("--quote-mode", choices=("real", "mock", "auto"), default="mock")
    start.add_argument("--attestation", default="noop")
    start.add_argument("--gomaxprocs", type=int, default=4)
    start.add_argument("--topology", choices=("sparse", "full"), default="sparse",
                       help="P2P graph (default: bounded-degree sparse ring with shortcuts)")
    start.add_argument("--topology-radius", type=int, default=2,
                       help="sparse ring neighbor radius (default: 2)")
    start.add_argument("--topology-max-degree", type=int, default=9,
                       help="maximum sparse node degree (default: 9; ignored in full mode)")
    # Light ordinary nodes (non-TEE, non-committee): extra P2P population.
    start.add_argument("--light-nodes", type=int, default=0, help="lightweight ordinary nodes")
    start.add_argument("--light-remote", action="store_true",
                       help="place light nodes on the TEE host instead of host A "
                            "(default: host A, keeping the TEE host's CPU free)")
    start.add_argument("--light-gomaxprocs", type=int, default=1)
    start.add_argument("--light-cache-mb", type=int, default=16)
    # netem one-way delay applied on BOTH NICs to the peer's geth P2P ports.
    start.add_argument("--netem-delay", type=int, default=0,
                       help="one-way delay in ms (0=off); applied A->B and B->A on geth P2P only")
    start.add_argument("--netem-jitter", type=int, default=0)
    start.add_argument("--netem-loss", type=float, default=0.0)
    start.add_argument("--netem-limit", type=int, default=10000,
                       help="netem queue length in packets; raise for large topologies "
                            "(64-node DKRS/distkg bursts can exceed the default)")
    start.add_argument("--netem-dev", default="eth0",
                       help="NIC name (probe: ip route get <peer> | grep -oP 'dev \\K\\S+')")
    start.add_argument("--netem-lo-dev", default="lo",
                       help="device carrying same-host traffic; delayed by port so "
                            "geth P2P and committee sockets match (default: lo)")
    start.add_argument("--no-netem-lo", action="store_true",
                       help="cross-host delay only; leave same-host traffic undelayed "
                            "(use when the run documents the ethernet path in isolation)")
    # PoW block-time / tx-density knobs. The chain is ethash, so block time is set
    # by the genesis difficulty; tx/block is then tps x block_time, paced by tx.py's
    # closed loop. 0 means "keep the genesis.json difficulty" / "no background load".
    start.add_argument("--genesis-difficulty", default="0",
                       help="override genesis difficulty (hex 0x.. or decimal); 0 keeps genesis.json")
    start.add_argument("--est-seconds-per-block", type=float, default=15.0,
                       help="block-time estimate for epoch/confirmation deadlines; "
                            "default 15 matches the slow chain's MEAN interval "
                            "(median ~10s, slow tail ~121s); 0 keeps the library default (4)")
    start.add_argument("--on-tx-rate", type=float, default=0.0,
                       help="background ON transactions per second; 0 disables the load")
    start.add_argument("--tx-per-block", type=float, default=0.0,
                       help="background ON tx density per block (closed loop); 0 disables; capped at 200")
    start.add_argument("--keep-running", action="store_true",
                       help="do not stop nodes after the run (default: auto-stop)")
    start.add_argument("--no-collect", action="store_true",
                       help="skip the timing aggregation step")
    args = parser.parse_args()
    if args.worker:
        worker_main()
    elif args.command == "start":
        command_start(args)
    elif args.command == "stop":
        if not args.run_id:
            parser.error("stop requires --run-id")
        errors = []
        for host in (Host(args.remote_root, args.remote_ssh), Host(args.local_root)):
            try:
                print(host.call("stop", run_id=args.run_id))
            except (OSError, RuntimeError, subprocess.TimeoutExpired) as exc:
                errors.append(str(exc))
        if errors:
            raise RuntimeError("; ".join(errors))
    else:
        parser.print_help()


if __name__ == "__main__":
    try:
        main()
    except RunCancelled as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(128 + exc.signum)
