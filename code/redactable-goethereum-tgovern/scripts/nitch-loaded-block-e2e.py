#!/usr/bin/env python3
"""Run a non-destructive loaded nitch block-generation/validation experiment.

All chain state is created below --output. Existing nitch node directories are
never read or modified. The formal experiment uses one miner, one full observer,
loopback tc/netem, and the same unlocked-account load generator used by SGX-L5.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_NITCH = ROOT.parent / "redactable-goethereum"
DEFAULT_GETH = DEFAULT_NITCH / "build" / "bin" / "geth"
LOAD_SCRIPT = ROOT / "tx.py"
SENDER_KEY = "0" * 63 + "1"
SENDER = "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"
RECEIVER = "0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF"
PASSWORD = "perf-fixture"
CHAIN_ID = 12345
URL_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
PROCESSES: list[subprocess.Popen[Any]] = []
NETEM_APPLIED = False
STOP = False


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fp:
        for chunk in iter(lambda: fp.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def command_output(args: list[str], cwd: Path | None = None) -> str:
    result = subprocess.run(
        args, cwd=cwd, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, check=False,
    )
    return (result.stdout or "").strip()


def repo_state(path: Path) -> dict[str, Any]:
    return {
        "path": str(path),
        "commit": command_output(["git", "rev-parse", "HEAD"], path),
        "branch": command_output(["git", "branch", "--show-current"], path),
        "status": command_output(["git", "status", "--short"], path).splitlines(),
        "diff_sha256": hashlib.sha256(
            command_output(["git", "diff", "--binary"], path).encode()
        ).hexdigest(),
    }


def rpc(url: str, method: str, params: Any, timeout: float = 20) -> Any:
    payload = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": method, "params": params,
    }).encode()
    request = urllib.request.Request(
        url, data=payload, headers={"Content-Type": "application/json"}
    )
    with URL_OPENER.open(request, timeout=timeout) as response:
        result = json.loads(response.read().decode())
    if result.get("error"):
        raise RuntimeError(f"{method} failed: {result['error']}")
    return result.get("result")


def wait_rpc(url: str, timeout: float = 60) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            rpc(url, "web3_clientVersion", [], timeout=3)
            return
        except Exception as err:  # noqa: BLE001 - preserve last RPC failure
            last_error = err
            time.sleep(0.5)
    raise TimeoutError(f"RPC not ready at {url}: {last_error}")


def wait_until(description: str, predicate: Any, timeout: float, interval: float = 0.5) -> Any:
    deadline = time.monotonic() + timeout
    last: Any = None
    while time.monotonic() < deadline:
        last = predicate()
        if last:
            return last
        time.sleep(interval)
    raise TimeoutError(f"timeout waiting for {description}; last={last!r}")


def qdisc(interface: str) -> str:
    return command_output(["tc", "qdisc", "show", "dev", interface])


def apply_netem(interface: str, delay_ms: int) -> str:
    global NETEM_APPLIED
    before = qdisc(interface)
    if "netem" in before:
        raise RuntimeError(f"refusing to replace pre-existing netem on {interface}: {before}")
    subprocess.run(
        [
            "tc", "qdisc", "replace", "dev", interface, "root", "netem",
            "delay", f"{delay_ms}ms",
        ],
        check=True,
    )
    NETEM_APPLIED = True
    actual = qdisc(interface)
    if "netem" not in actual or f"delay {delay_ms}ms" not in actual:
        raise RuntimeError(f"netem verification failed: {actual}")
    return actual


def teardown_netem(interface: str) -> None:
    global NETEM_APPLIED
    if NETEM_APPLIED:
        subprocess.run(
            ["tc", "qdisc", "del", "dev", interface, "root"],
            check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        NETEM_APPLIED = False


def stop_processes() -> None:
    for proc in reversed(PROCESSES):
        if proc.poll() is None:
            proc.terminate()
    deadline = time.monotonic() + 10
    for proc in reversed(PROCESSES):
        if proc.poll() is None:
            try:
                proc.wait(timeout=max(0.1, deadline - time.monotonic()))
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)


def handle_stop(_signum: int, _frame: Any) -> None:
    global STOP
    STOP = True


def normalize_enode(enode: str, port: int) -> str:
    if "@" not in enode:
        raise ValueError(f"invalid enode: {enode}")
    prefix = enode.split("@", 1)[0]
    return f"{prefix}@127.0.0.1:{port}"


def write_json(path: Path, value: Any) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def start_node(
    geth: Path,
    datadir: Path,
    log_path: Path,
    rpc_port: int,
    p2p_port: int,
    role: str,
    required_shares: int,
    password_file: Path | None = None,
) -> tuple[subprocess.Popen[Any], list[str]]:
    args = [
        str(geth),
        "--datadir", str(datadir),
        "--networkid", str(CHAIN_ID),
        "--port", str(p2p_port),
        "--rpc", "--rpcport", str(rpc_port), "--rpcaddr", "127.0.0.1",
        "--rpcapi", "eth,net,web3,personal,admin,debug,miner,txpool",
        "--nodiscover", "--maxpeers", "16", "--syncmode", "full",
        "--cache", "128", "--verbosity", "4", "--ipcdisable", "--nousb",
        "--etherbase", RECEIVER,
    ]
    if password_file is not None:
        args += [
            "--allow-insecure-unlock", "--unlock", SENDER,
            "--password", str(password_file),
        ]
    env = os.environ.copy()
    env["E2E_TIMING_NODE_ROLE"] = role
    env["NITCH_THRESHOLD"] = str(required_shares)
    log_fp = log_path.open("w")
    proc = subprocess.Popen(
        args, cwd=str(geth.parents[2]), env=env,
        stdout=log_fp, stderr=subprocess.STDOUT,
    )
    log_fp.close()
    PROCESSES.append(proc)
    return proc, args


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--nitch-repo", type=Path, default=DEFAULT_NITCH)
    parser.add_argument("--geth", type=Path, default=DEFAULT_GETH)
    parser.add_argument("--duration", type=float, default=180.0)
    parser.add_argument("--rate", type=float, default=0.25)
    parser.add_argument(
        "--fixture-mode", choices=("loaded", "empty"), default="loaded",
        help="loaded starts the fixed transfer generator; empty mines without user txs",
    )
    parser.add_argument("--netem-delay-ms", type=int, default=20)
    parser.add_argument("--netem-interface", default="lo")
    parser.add_argument("--node-count", type=int, default=8)
    parser.add_argument("--required-shares", type=int, default=5)
    parser.add_argument("--card-id", default="A1")
    parser.add_argument("--variant", default="nitch-loaded")
    args = parser.parse_args()

    output = args.output.resolve()
    nitch = args.nitch_repo.resolve()
    geth = args.geth.resolve()
    if output.exists() and any(output.iterdir()):
        raise FileExistsError(f"output must be absent or empty: {output}")
    if args.duration < 30:
        raise ValueError("--duration must be at least 30 seconds")
    if args.fixture_mode == "loaded" and args.rate <= 0:
        raise ValueError("--rate must be positive")
    if not 2 <= args.node_count <= 16:
        raise ValueError("--node-count must be between 2 and 16")
    if not 1 <= args.required_shares <= args.node_count:
        raise ValueError("--required-shares must be between 1 and --node-count")
    if args.netem_delay_ms != 20:
        raise ValueError("formal A1/A2 runs require exactly 20ms netem")
    if not geth.is_file():
        raise FileNotFoundError(geth)
    if not LOAD_SCRIPT.is_file():
        raise FileNotFoundError(LOAD_SCRIPT)

    output.mkdir(parents=True, exist_ok=True)
    logs = output / "logs"
    chain = output / "chain"
    logs.mkdir()
    chain.mkdir()
    progress_path = output / "progress.jsonl"
    load_records = output / "on-tx-load.jsonl"
    load_stdout = logs / "tx-load.log"
    password_file = output / "password.txt"
    private_key_file = output / "sender-private-key.txt"
    accounts_file = output / "on-load-accounts.json"
    receivers_file = output / "on-load-receivers.json"
    password_file.write_text(PASSWORD + "\n")
    private_key_file.write_text(SENDER_KEY + "\n")
    write_json(accounts_file, [SENDER])
    write_json(receivers_file, [RECEIVER])

    genesis = json.loads((nitch / "genesis.json").read_text())
    genesis["config"]["chainId"] = CHAIN_ID
    genesis["alloc"] = {
        SENDER[2:].lower(): {"balance": "0x3635c9adc5dea00000"},
        RECEIVER[2:].lower(): {"balance": "0x3635c9adc5dea00000"},
    }
    genesis_path = output / "genesis.json"
    write_json(genesis_path, genesis)

    node_dirs = [chain / f"node{index}" for index in range(1, args.node_count + 1)]
    registry_path = chain / "node_registry.json"
    write_json(registry_path, {
        "nodes": [
            {
                "node_id": str(datadir),
                "index": index,
                "is_committee": True,
                "timestamp": utc_now(),
            }
            for index, datadir in enumerate(node_dirs)
        ],
    })
    init_commands: list[list[str]] = []
    for datadir in node_dirs:
        datadir.mkdir()
        command = [str(geth), "--datadir", str(datadir), "init", str(genesis_path)]
        init_commands.append(command)
        subprocess.run(
            command, cwd=str(nitch), check=True,
            stdout=(logs / f"{datadir.name}-init.log").open("w"),
            stderr=subprocess.STDOUT,
        )
    import_command = [
        str(geth), "--datadir", str(node_dirs[0]), "account", "import",
        "--password", str(password_file), str(private_key_file),
    ]
    subprocess.run(
        import_command, cwd=str(nitch), check=True,
        stdout=(logs / "node1-account-import.log").open("w"),
        stderr=subprocess.STDOUT,
    )

    pre_manifest = {
        "schema": 1,
        "created_at_utc": utc_now(),
        "card_id": args.card_id,
        "variant": args.variant,
        "hypothesis": (
            "Changing only block content from empty to the fixed one-transfer "
            "fixture changes content-sensitive subphases but cannot reproduce "
            "the historical generation/validation gap."
            if args.card_id == "A3"
            else
            "After identical per-call boundaries and loaded content strata, "
            "nitch and SGX-L5 block gen/validation differences shrink."
        ),
        "predicted_observable": (
            "Empty-fixture blocks have txs=0/gas=0 while fixed-transfer blocks "
            "have txs=1/gas=21000; process/validate rise slightly, and the "
            "change remains below the historical implementation gap."
            if args.card_id == "A3"
            else
            "Per-transaction apply and per-block process/validate distributions "
            "converge relative to the former same-name headline gap."
        ),
        "pass_criterion": (
            "Two independent empty/loaded lifecycle pairs have zero metadata "
            "stratum leakage; the same-direction content effect repeats, and "
            "its magnitude is compared with the predeclared historical gap."
            if args.card_id == "A3"
            else
            "Two independent loaded comparisons show the declared direction "
            "beyond A/A noise, with no content-stratum leakage."
        ),
        "fixed_controls": {
            "fixture_mode": args.fixture_mode,
            "transaction_rate_per_s": args.rate if args.fixture_mode == "loaded" else 0,
            "transaction_value_wei": 1,
            "transaction_gas_limit": 21000,
            "transaction_input_bytes": 0,
            "miner_count": 1,
            "observer_count": 1,
            "nitch_committee_size": args.node_count,
            "nitch_required_shares": args.required_shares,
            "cache_mib": 128,
            "miner_threads": 1,
            "netem_delay_ms": args.netem_delay_ms,
            "netem_interface": args.netem_interface,
        },
        "repositories": {
            "nitch": repo_state(nitch),
            "tgovern_harness": repo_state(ROOT),
        },
        "binaries": {
            "geth": {"path": str(geth), "sha256": sha256(geth)},
        },
        "genesis": {
            "path": str(genesis_path),
            "sha256": sha256(genesis_path),
            "chain_id": CHAIN_ID,
            "difficulty": genesis.get("difficulty"),
            "gas_limit": genesis.get("gasLimit"),
        },
        "nitch_registry": {
            "path": str(registry_path),
            "sha256": sha256(registry_path),
        },
        "commands": {
            "harness": [sys.executable, str(Path(__file__).resolve()), *sys.argv[1:]],
            "init": init_commands,
            "account_import": import_command,
        },
        "host": {
            "uname": command_output(["uname", "-a"]),
            "cpu_count": os.cpu_count(),
            "qdisc_before": qdisc(args.netem_interface),
        },
    }
    write_json(output / "manifest.pre.json", pre_manifest)

    signal.signal(signal.SIGTERM, handle_stop)
    signal.signal(signal.SIGINT, handle_stop)
    result: dict[str, Any] = {
        "started_at_utc": utc_now(),
        "success": False,
        "failure": None,
    }
    progress_fp = progress_path.open("a")

    def progress(event: str, **fields: Any) -> None:
        record = {"timestamp_s": time.time(), "timestamp_utc": utc_now(), "event": event}
        record.update(fields)
        progress_fp.write(json.dumps(record, separators=(",", ":")) + "\n")
        progress_fp.flush()

    try:
        actual_qdisc = apply_netem(args.netem_interface, args.netem_delay_ms)
        progress("netem_verified", qdisc=actual_qdisc)
        node_commands: dict[str, list[str]] = {}
        rpc_urls: list[str] = []
        for index, datadir in enumerate(node_dirs, start=1):
            role = (
                "nitch_miner" if index == 1
                else "nitch_observer" if index == 2
                else "nitch_committee"
            )
            _proc, command = start_node(
                geth, datadir, logs / f"geth-node-{index}.log",
                28550 + index, 31310 + index, role,
                args.required_shares,
                password_file if index == 1 else None,
            )
            node_commands[f"node{index}"] = command
            rpc_urls.append(f"http://127.0.0.1:{28550 + index}")
        for url in rpc_urls:
            wait_rpc(url)
        enodes = {
            f"node{index}": normalize_enode(
                str(rpc(url, "admin_nodeInfo", [])["enode"]), 31310 + index
            )
            for index, url in enumerate(rpc_urls, start=1)
        }
        edges: list[list[str]] = []
        for left in range(args.node_count):
            for right in range(left + 1, args.node_count):
                rpc(
                    rpc_urls[left], "admin_addPeer",
                    [enodes[f"node{right + 1}"]],
                )
                rpc(
                    rpc_urls[right], "admin_addPeer",
                    [enodes[f"node{left + 1}"]],
                )
                edges.append([f"node{left + 1}", f"node{right + 1}"])

        def full_mesh_ready() -> Any:
            counts = [
                int(rpc(url, "net_peerCount", []), 16) for url in rpc_urls
            ]
            if all(count >= args.node_count - 1 for count in counts):
                return counts
            # admin_addPeer may transiently return false while another dial is
            # handshaking. Reassert the exact desired edges until all peers are
            # visible instead of silently accepting a partial mesh.
            for left in range(args.node_count):
                for right in range(left + 1, args.node_count):
                    rpc(
                        rpc_urls[left], "admin_addPeer",
                        [enodes[f"node{right + 1}"]],
                    )
                    rpc(
                        rpc_urls[right], "admin_addPeer",
                        [enodes[f"node{left + 1}"]],
                    )
            return None

        peer_counts = wait_until(
            "full-mesh peers",
            full_mesh_ready,
            timeout=180,
            interval=1,
        )
        progress("full_mesh_ready", peer_counts=peer_counts)
        # Starting one committee member broadcasts its deal. Receiving members
        # generate their own deal/share automatically; a second explicit RPC can
        # race with that path and correctly return "already sent".
        dkg_results = {
            "node1_start": rpc(rpc_urls[0], "eth_startDKG", []),
            "other_nodes": "automatic-on-receive",
        }

        def dkg_ready() -> Any:
            statuses = {
                f"node{index}": rpc(url, "eth_dkgStatus", [])
                for index, url in enumerate(rpc_urls, start=1)
            }
            if all(status.get("extracted") for status in statuses.values()):
                return statuses
            return None

        dkg_status = wait_until("all nitch DistKG extracts", dkg_ready, timeout=300)
        progress("dkg_ready", statuses=dkg_status)
        rpc1, rpc2 = rpc_urls[0], rpc_urls[1]
        rpc(rpc1, "miner_start", [1])
        initial_block = int(rpc(rpc1, "eth_blockNumber", []), 16)
        wait_until(
            "first mined block",
            lambda: int(rpc(rpc1, "eth_blockNumber", []), 16) > initial_block,
            timeout=90,
        )
        start_block = int(rpc(rpc1, "eth_blockNumber", []), 16)
        measurement_log_offsets = {
            f"node{index}": (logs / f"geth-node-{index}.log").stat().st_size
            for index in range(1, args.node_count + 1)
        }
        progress(
            "measurement_begin", start_block=start_block,
            qdisc=qdisc(args.netem_interface),
        )
        load_command: list[str] = []
        if args.fixture_mode == "loaded":
            load_command = [
                sys.executable, str(LOAD_SCRIPT),
                "--rpc", rpc1,
                "--accounts", str(accounts_file),
                "--receivers", str(receivers_file),
                "--rate", str(args.rate),
                "--duration", str(args.duration),
                "--gas", "0x5208",
                "--value", "0x1",
                "--log", str(load_records),
            ]
            with load_stdout.open("w") as load_fp:
                load_proc = subprocess.Popen(
                    load_command, cwd=str(ROOT), stdout=load_fp,
                    stderr=subprocess.STDOUT,
                )
            PROCESSES.append(load_proc)
            while load_proc.poll() is None:
                if STOP:
                    raise InterruptedError("received stop signal")
                time.sleep(0.5)
            if load_proc.returncode != 0:
                raise RuntimeError(f"load generator exit code {load_proc.returncode}")
            records = [
                json.loads(line)
                for line in load_records.read_text().splitlines()
                if line.strip()
            ]
        else:
            load_stdout.write_text(
                "fixture_mode=empty; no user transaction generator started\n"
            )
            load_records.write_text("")
            deadline = time.monotonic() + args.duration
            while time.monotonic() < deadline:
                if STOP:
                    raise InterruptedError("received stop signal")
                time.sleep(min(0.5, max(0.0, deadline - time.monotonic())))
            records = []
        sent_hashes = [str(item["tx_hash"]) for item in records if item.get("ok")]
        failures = [item for item in records if not item.get("ok")]
        if (
            args.fixture_mode == "loaded"
            and (
                failures
                or len(sent_hashes) < max(1, int(args.duration * args.rate * 0.85))
            )
        ):
            raise AssertionError(
                f"load integrity failed sent={len(sent_hashes)} failures={len(failures)}"
            )
        if args.fixture_mode == "empty" and (failures or sent_hashes):
            raise AssertionError(
                f"empty fixture emitted transactions sent={len(sent_hashes)} "
                f"failures={len(failures)}"
            )

        def receipts_ready() -> bool:
            for tx_hash in sent_hashes:
                if rpc(rpc1, "eth_getTransactionReceipt", [tx_hash]) is None:
                    return False
            return True

        wait_until("all load receipts", receipts_ready, timeout=120, interval=1)
        end_block = int(rpc(rpc1, "eth_blockNumber", []), 16)
        wait_until(
            "observer head catchup",
            lambda: int(rpc(rpc2, "eth_blockNumber", []), 16) >= end_block,
            timeout=90,
        )
        progress(
            "measurement_end", end_block=end_block,
            qdisc=qdisc(args.netem_interface),
        )
        measurement_errors: list[dict[str, Any]] = []
        for index in range(1, args.node_count + 1):
            name = f"node{index}"
            path = logs / f"geth-node-{index}.log"
            with path.open("rb") as fp:
                fp.seek(measurement_log_offsets[name])
                for raw_line in fp.read().decode(errors="replace").splitlines():
                    if re.match(r"^(ERROR|CRIT|PANIC|FATAL)", raw_line):
                        measurement_errors.append({"node": name, "line": raw_line})
        rpc(rpc1, "miner_stop", [])

        blocks: list[dict[str, Any]] = []
        included_hashes: set[str] = set()
        for number in range(start_block + 1, end_block + 1):
            block = rpc(rpc2, "eth_getBlockByNumber", [hex(number), True])
            if block is None:
                raise AssertionError(f"observer missing block {number}")
            transactions = block.get("transactions") or []
            tx_hashes = [
                str(tx.get("hash")) if isinstance(tx, dict) else str(tx)
                for tx in transactions
            ]
            included_hashes.update(item.lower() for item in tx_hashes)
            blocks.append({
                "number": number,
                "hash": block.get("hash"),
                "parent_hash": block.get("parentHash"),
                "tx_count": len(transactions),
                "tx_hashes": tx_hashes,
                "gas_used": int(str(block.get("gasUsed", "0x0")), 16),
                "gas_limit": int(str(block.get("gasLimit", "0x0")), 16),
                "block_bytes": int(str(block.get("size", "0x0")), 16),
                "timestamp": int(str(block.get("timestamp", "0x0")), 16),
            })
        missing = sorted(
            tx_hash for tx_hash in sent_hashes if tx_hash.lower() not in included_hashes
        )
        if missing:
            raise AssertionError(f"successful load txs missing from observer blocks: {missing[:5]}")
        with (output / "blocks.jsonl").open("w") as fp:
            for block in blocks:
                fp.write(json.dumps(block, separators=(",", ":")) + "\n")

        receipt_statuses = [
            int(rpc(rpc1, "eth_getTransactionReceipt", [tx_hash])["status"], 16)
            for tx_hash in sent_hashes
        ]
        if any(status != 1 for status in receipt_statuses):
            raise AssertionError(f"failed receipts: {receipt_statuses.count(0)}")
        nonempty = [block for block in blocks if block["tx_count"] > 0]
        if args.fixture_mode == "loaded" and len(nonempty) < 15:
            raise AssertionError(
                f"insufficient loaded nonempty blocks: {len(nonempty)} < 15"
            )
        if args.fixture_mode == "empty" and len(blocks) < 15:
            raise AssertionError(
                f"insufficient empty fixture blocks: {len(blocks)} < 15"
            )
        if args.fixture_mode == "empty" and nonempty:
            raise AssertionError(
                f"empty fixture contained {len(nonempty)} nonempty blocks"
            )
        if measurement_errors:
            raise AssertionError(
                f"new ERROR/CRIT/PANIC/FATAL lines in measurement window: "
                f"{len(measurement_errors)}"
            )
        result.update({
            "success": True,
            "actual_qdisc_during_run": actual_qdisc,
            "commands": {
                **node_commands,
                "load": load_command,
                "empty_fixture_wait_s": (
                    args.duration if args.fixture_mode == "empty" else None
                ),
            },
            "topology": {
                "roles": {
                    f"node{index}": (
                        "miner" if index == 1
                        else "full_observer" if index == 2
                        else "committee"
                    )
                    for index in range(1, args.node_count + 1)
                },
                "edges": edges,
                "enodes": enodes,
            },
            "distkg": {
                "start_results": dkg_results,
                "final_status": dkg_status,
            },
            "workload": {
                "fixture_mode": args.fixture_mode,
                "rate_per_s": args.rate if args.fixture_mode == "loaded" else 0,
                "duration_s": args.duration,
                "sent": len(sent_hashes),
                "failures": len(failures),
                "receipt_status_ok": receipt_statuses.count(1),
            },
            "blocks": {
                "start": start_block,
                "end": end_block,
                "count": len(blocks),
                "nonempty_count": len(nonempty),
                "empty_count": len(blocks) - len(nonempty),
                "tx_count": sum(block["tx_count"] for block in blocks),
            },
            "functional_checks": {
                "observer_caught_up": True,
                "all_sent_txs_in_observer_blocks": True,
                "all_receipts_success": True,
                "load_failures_zero": True,
                "fixture_blocks_at_least_15": True,
                "fixture_stratum_exact": (
                    len(nonempty) >= 15
                    if args.fixture_mode == "loaded"
                    else len(nonempty) == 0
                ),
                "measurement_errors_zero": True,
            },
            "measurement_errors": measurement_errors,
        })
    except Exception as err:  # noqa: BLE001 - persist exact formal failure
        result["failure"] = f"{type(err).__name__}: {err}"
        progress("experiment_failure", failure=result["failure"])
    finally:
        result["ended_at_utc"] = utc_now()
        result["process_exit_codes_before_cleanup"] = {
            f"pid_{proc.pid}": proc.poll() for proc in PROCESSES
        }
        stop_processes()
        result["process_exit_codes_after_cleanup"] = {
            f"pid_{proc.pid}": proc.poll() for proc in PROCESSES
        }
        result["qdisc_before_teardown"] = qdisc(args.netem_interface)
        teardown_netem(args.netem_interface)
        result["qdisc_after_teardown"] = qdisc(args.netem_interface)
        progress_fp.close()
        write_json(output / "run-manifest.json", result)

    return 0 if result["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
