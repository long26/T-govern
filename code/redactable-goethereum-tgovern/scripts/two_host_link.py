#!/usr/bin/env python3
"""Two-host T-govern link smoke: node1 + committee local, SGX node2 over SSH.

Install the same tgovern-hw package on both hosts. This intentionally does not
run registration, DistKG, redaction, or the L5 performance benchmark.
"""

from __future__ import annotations

import argparse
import datetime
import ipaddress
import json
import os
import re
import secrets
import shlex
import signal
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

from eth_account import Account

ROOT = Path(__file__).resolve().parents[1]
NETWORK_ID = "739205"
RPC_PORTS = {1: 18551, 2: 18552}
P2P_PORTS = {1: 30311, 2: 30312}
RPC_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
RUN_ID_RE = re.compile(r"[a-zA-Z0-9_-]{1,80}\Z")


def run_dir(root: Path, run_id: str) -> Path:
    if not RUN_ID_RE.fullmatch(run_id):
        raise ValueError("invalid run id")
    return root / ".tmp" / "two-host" / run_id


def rpc(url: str, method: str, params: object = None) -> object:
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method,
                       "params": [] if params is None else params}).encode()
    req = urllib.request.Request(url, body, {"Content-Type": "application/json"})
    with RPC_OPENER.open(req, timeout=5) as response:
        result = json.load(response)
    if result.get("error"):
        raise RuntimeError(f"{method}: {result['error']}")
    return result.get("result")


def local_rpc(method: str, params: object = None, *, port: int = 18551) -> object:
    return rpc(f"http://127.0.0.1:{port}", method, params)


def wait_until(label: str, action, timeout: int = 90):
    deadline = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < deadline:
        try:
            result = action()
            if result is not None and result is not False:
                return result
        except (OSError, ValueError, RuntimeError, KeyError) as exc:
            last_error = exc
        time.sleep(1)
    raise TimeoutError(f"{label} not ready after {timeout}s (last error: {last_error})")


def write_private(path: Path, text: str) -> None:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as out:
        out.write(text)


def worker_prepare(config: dict) -> dict:
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    index = config["node"]
    if index not in (1, 2):
        raise ValueError("node must be 1 or 2")
    geth = root / "bin" / ("geth" if index == 1 else "geth-sgx")
    if not geth.is_file():
        raise FileNotFoundError(geth)
    if not (root / "helper.json").is_file():
        raise FileNotFoundError(root / "helper.json")
    if index == 2:
        if not (root / "sgx" / "enclave.signed.so").is_file():
            raise FileNotFoundError(root / "sgx" / "enclave.signed.so")
        if config.get("tee_sgx_mode", "HW") == "HW":
            for binary in (geth, root / "sgx" / "libsgx_module.so"):
                result = subprocess.run(["ldd", str(binary)], capture_output=True,
                                        text=True, check=True)
                if "libsgx_urts_sim.so" in result.stdout or "libsgx_urts.so" not in result.stdout:
                    raise RuntimeError(f"HW requested but {binary} is not linked to SGX HW runtime; "
                                       "use a HW build, or explicitly select --tee-sgx-mode SIM "
                                       "for network-only testing")
    work.mkdir(parents=True, exist_ok=False)
    datadir = work / "chain" / f"node{index}"
    nodework = work / "node-work" / f"node{index}"
    datadir.mkdir(parents=True)
    nodework.mkdir(parents=True)
    (work / "logs").mkdir()
    (work / "run").mkdir()
    (work / "genesis.json").write_text(json.dumps(config["genesis"], indent=2) + "\n")
    (nodework / "helper.json").write_bytes((root / "helper.json").read_bytes())
    write_private(nodework / "privatekey.txt", config["key"] + "\n")
    write_private(nodework / "password.txt", "1\n")
    write_private(nodework / "storage.json", json.dumps({
        "address": "", "privateKey": config["key"], "port": str(RPC_PORTS[index]),
    }, indent=2) + "\n")
    cache = work / "ethash-cache"
    cache.mkdir()
    for block in (0, 30000):
        subprocess.run([str(geth), "makecache", str(block), str(cache)], check=True,
                       stdout=subprocess.DEVNULL)
    subprocess.run([str(geth), "--datadir", str(datadir), "init", str(work / "genesis.json")],
                   check=True, stdout=subprocess.DEVNULL)
    subprocess.run([str(geth), "--datadir", str(datadir), "account", "import",
                    "--password", str(nodework / "password.txt"), str(nodework / "privatekey.txt")],
                   check=True, stdout=subprocess.DEVNULL)
    return {"work": str(work), "node": index}


def process_start(pid: int) -> str:
    # /proc/<pid>/stat field 22; the executable name can contain spaces.
    fields = Path(f"/proc/{pid}/stat").read_text().rsplit(") ", 1)[1].split()
    return fields[19]


def spawn(work: Path, name: str, args: list[str], cwd: Path, env: dict | None = None) -> int:
    pidfile = work / "run" / f"{name}.json"
    if pidfile.exists():
        raise FileExistsError(f"process record already exists: {pidfile}")
    with (work / "logs" / f"{name}.log").open("x") as output:
        proc = subprocess.Popen(args, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
    write_private(pidfile, json.dumps({"pid": proc.pid, "start": process_start(proc.pid),
                                       "datadir": str(work / "chain")}) + "\n")
    return proc.pid


def worker_start(config: dict) -> dict:
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    index = config["node"]
    if index not in (1, 2) or not (work / "genesis.json").is_file():
        raise ValueError("prepare this run before starting")
    nodework = work / "node-work" / f"node{index}"
    datadir = work / "chain" / f"node{index}"
    storage = nodework / "storage.json"
    data = json.loads(storage.read_text())
    if index == 2:
        contract = config.get("contract", "")
        if not re.fullmatch(r"0x[0-9a-fA-F]{40}", contract):
            raise ValueError("node2 requires deployed contract address")
        data["address"] = contract
        storage.write_text(json.dumps(data, indent=2) + "\n")
    geth = root / "bin" / ("geth" if index == 1 else "geth-sgx")
    args = [str(geth), "--datadir", str(datadir),
            "--networkid", NETWORK_ID, "--port", str(P2P_PORTS[index]),
            "--rpc", "--rpcaddr", "127.0.0.1", "--rpcport", str(RPC_PORTS[index]),
            "--rpcapi", "eth,net,web3,personal,admin,debug,miner,txpool",
            "--ipcpath", str(datadir / "geth.ipc"), "--nodiscover", "--maxpeers", "50",
            "--etherbase", config["address"], "--ethash.cachedir", str(work / "ethash-cache"),
            "--allow-insecure-unlock", "--unlock", config["address"],
            "--password", str(nodework / "password.txt"), "--verbosity", "4"]
    env = os.environ.copy()
    env.update({"SGX_MODE": config.get("tee_sgx_mode", "HW") if index == 2
                else config.get("ordinary_sgx_mode", "SIM"),
                "E2E_TIMING_NODE_ROLE": "tee" if index == 2 else "on",
                "EPOCH_LENGTH": "100", "STEADY_EPOCH_LENGTH": "100",
                "TEE_ACTIVE_LIMIT": "4", "TEE_ACTIVE_THRESHOLD": "3"})
    if index == 1:
        if env["SGX_MODE"] == "SIM":
            env["SGX_SIM_ID_SEED"] = "sgx-two-host-node-1"
        args += ["--mine", "--miner.threads", "1"]
    else:
        args += ["--teehost", "--tee.eventpoll", "1s", "--tee.confirmations", "0"]
        if env["SGX_MODE"] == "SIM":
            env["SGX_SIM_ID_SEED"] = "sgx-two-host-node-2"
        env["SGX_ENCLAVE_PATH"] = str(root / "sgx" / "enclave.signed.so")
    sgx_sdk = env.get("SGX_SDK", "/opt/intel/sgxsdk")
    env["LD_LIBRARY_PATH"] = ":".join(filter(None, [str(root / "sgx"),
        f"{sgx_sdk}/sdk_libs", f"{sgx_sdk}/lib64", env.get("LD_LIBRARY_PATH", "")]))
    return {"pid": spawn(work, f"geth-node{index}", args, nodework, env)}


def worker_committee(config: dict) -> dict:
    root = Path(config["root"])
    work = run_dir(root, config["run_id"])
    ipc = work / "chain" / "node1" / "geth.ipc"
    if not ipc.exists():
        raise RuntimeError("node1 IPC must be ready before committee")
    count = config.get("committee_count", 4)
    threshold = config.get("committee_threshold", 3)
    if not (1 <= threshold <= count <= 16):
        raise ValueError("invalid committee threshold/count")
    if not (root / "bin" / "committee-mvp").is_file():
        raise FileNotFoundError(root / "bin" / "committee-mvp")
    configs = work / "committee-configs"
    configs.mkdir(exist_ok=True)
    for i in range(1, count + 1):
        name = f"committee-node{i}"
        cfg = {"node_id": f"node-{i}", "listen_addr": f"127.0.0.1:{3400+i}",
               "static_nodes": [f"node-{j}" for j in range(1, count + 1)],
               "static_node_addrs": {f"node-{j}": f"127.0.0.1:{3400+j}"
                                     for j in range(1, count + 1)},
               "control_addr": f"127.0.0.1:{4400+i}", "chain_rpc_mode": "ipc",
               "geth_ipc_path": str(ipc), "committee_size": count,
               "threshold": threshold, "coordinator_id": "node-1",
               "domain_separation": "committee-sig/mvp/v1", "message_version": "v1",
               "attestation_provider": "noop"}
        cfg_path = configs / f"node-{i}.json"
        cfg_path.write_text(json.dumps(cfg, indent=2) + "\n")
        spawn(work, name, [str(root / "bin" / "committee-mvp"), "node", "--config",
                            str(cfg_path)], root)
    return {"committee_count": count}


def worker_rpc(config: dict) -> dict:
    port = 4401 if config.get("committee") else RPC_PORTS[config["node"]]
    url = f"http://127.0.0.1:{port}" + ("/rpc" if config.get("committee") else "")
    return {"result": rpc(url, config["method"], config.get("params", []))}


def worker_contract(config: dict) -> dict:
    work = run_dir(Path(config["root"]), config["run_id"])
    storage = work / "node-work" / "node1" / "storage.json"
    address = json.loads(storage.read_text()).get("address", "")
    if not re.fullmatch(r"0x[0-9a-fA-F]{40}", address):
        raise RuntimeError("contract not deployed")
    if local_rpc("eth_getCode", [address, "latest"], port=18551) in (None, "0x"):
        raise RuntimeError("contract code not visible")
    return {"contract": address}


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
            status[record.stem] = process_start(pid) == info["start"] and str(work) in cmdline
        except FileNotFoundError:
            status[record.stem] = False
    return {"processes": status}


def worker_stop(config: dict) -> dict:
    work = run_dir(Path(config["root"]), config["run_id"])
    if not (work / "run").is_dir():
        raise FileNotFoundError(f"run not prepared: {work}")
    stopped = []
    errors = []
    records = list((work / "run").glob("committee-node*.json"))
    records += list((work / "run").glob("geth-node*.json"))
    for record in records:
        info = json.loads(record.read_text())
        pid = int(info["pid"])
        try:
            if process_start(pid) != info["start"]:
                raise RuntimeError(f"PID {pid} reused; refusing to kill")
            cmdline = Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\x00", b" ").decode()
            if str(work) not in cmdline:
                raise RuntimeError(f"PID {pid} does not belong to run {work}")
            os.kill(pid, signal.SIGTERM)
            stopped.append(record.stem)
        except FileNotFoundError:
            pass
        except RuntimeError as exc:
            errors.append(str(exc))
            continue
        record.unlink()
    if errors:
        raise RuntimeError("; ".join(errors))
    return {"stopped": stopped}


WORKER_ACTIONS = {"prepare": worker_prepare, "start": worker_start,
                  "committee": worker_committee, "rpc": worker_rpc,
                  "contract": worker_contract, "status": worker_status,
                  "stop": worker_stop}


def worker_main() -> None:
    request = json.load(sys.stdin)
    try:
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
        request = {"action": action, "root": str(self.root), **fields}
        script = self.root / "scripts" / "two_host_link.py"
        if self.ssh_host:
            argv = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                    self.ssh_host, f"python3 {shlex.quote(str(script))} --worker"]
        else:
            argv = [sys.executable, str(script), "--worker"]
        completed = subprocess.run(argv, input=json.dumps(request), text=True,
                                   capture_output=True, timeout=300)
        try:
            response = json.loads(completed.stdout)
        except ValueError as exc:
            raise RuntimeError(f"{action} on {self.ssh_host or 'local'} failed: "
                               f"{completed.stderr[-1200:]}") from exc
        if completed.returncode or not response.get("ok"):
            raise RuntimeError(f"{action} on {self.ssh_host or 'local'}: "
                               f"{response.get('error', completed.stderr[-1200:])}")
        return response


def enode_for_ip(raw: str, ip: str, port: int) -> str:
    addr = ipaddress.ip_address(ip)
    host = f"[{addr}]" if addr.version == 6 else str(addr)
    prefix = raw.split("@", 1)[0]
    if not re.fullmatch(r"enode://[0-9a-fA-F]{128}", prefix):
        raise ValueError("invalid enode public key")
    return f"{prefix}@{host}:{port}"


def peer_matches(peers: object, node_id: str) -> bool:
    # admin_peers.id can be a 32-byte discovery ID, not the enode public key.
    expected_id = node_id.removeprefix("0x").lower()
    return any(str(peer.get("id", "")).removeprefix("0x").lower() == expected_id
               for peer in (peers or []) if isinstance(peer, dict))


def command_start(args) -> None:
    if not (1 <= args.committee_threshold <= args.committee_count <= 16):
        raise ValueError("invalid committee threshold/count")
    for address in (args.local_ip, args.remote_ip):
        ipaddress.ip_address(address)
    if not args.local_root.is_absolute() or not args.remote_root.is_absolute():
        raise ValueError("both package roots must be absolute paths")
    local = Host(args.local_root)
    remote = Host(args.remote_root, args.remote_ssh)
    run_id = args.run_id or datetime.datetime.now(datetime.timezone.utc).strftime("link-%Y%m%d-%H%M%S") + "-" + secrets.token_hex(3)
    if not RUN_ID_RE.fullmatch(run_id):
        raise ValueError("invalid run id")
    accounts = [Account.create() for _ in range(2)]
    genesis = json.loads((args.local_root / "genesis.json").read_text())
    genesis["alloc"] = {a.address.removeprefix("0x").lower():
                       {"balance": "1000000000000000000000"} for a in accounts}
    print(f"run_id={run_id}", flush=True)
    try:
        for index, host in ((1, local), (2, remote)):
            host.call("prepare", run_id=run_id, node=index, genesis=genesis,
                      tee_sgx_mode=args.tee_sgx_mode,
                      key=accounts[index - 1].key.hex().removeprefix("0x"))
        local.call("start", run_id=run_id, node=1, address=accounts[0].address,
                   ordinary_sgx_mode=args.ordinary_sgx_mode)
        wait_until("node1 RPC", lambda: local.call("rpc", node=1, method="eth_blockNumber")["result"])
        contract = wait_until("contract deployment", lambda: local.call("contract", run_id=run_id)["contract"], 240)
        print(f"contract={contract}", flush=True)
        local.call("committee", run_id=run_id, contract=contract, address=accounts[0].address,
                   committee_count=args.committee_count, committee_threshold=args.committee_threshold)
        remote.call("start", run_id=run_id, node=2, address=accounts[1].address,
                    tee_sgx_mode=args.tee_sgx_mode, contract=contract)
        wait_until("node2 RPC", lambda: remote.call("rpc", node=2, method="eth_blockNumber")["result"])
        info1 = local.call("rpc", node=1, method="admin_nodeInfo")["result"]
        info2 = remote.call("rpc", node=2, method="admin_nodeInfo")["result"]
        enode1 = enode_for_ip(info1["enode"], args.local_ip, P2P_PORTS[1])
        enode2 = enode_for_ip(info2["enode"], args.remote_ip, P2P_PORTS[2])
        if not info1.get("id") or not info2.get("id"):
            raise ValueError("admin_nodeInfo missing node ID")
        if local.call("rpc", node=1, method="admin_addPeer", params=[enode2])["result"] is False:
            raise RuntimeError("node1 rejected node2 enode")
        if remote.call("rpc", node=2, method="admin_addPeer", params=[enode1])["result"] is False:
            raise RuntimeError("node2 rejected node1 enode")
        wait_until("node1 peer", lambda: peer_matches(
            local.call("rpc", node=1, method="admin_peers")["result"], info2["id"]))
        wait_until("node2 peer", lambda: peer_matches(
            remote.call("rpc", node=2, method="admin_peers")["result"], info1["id"]))
        def synced():
            first = int(local.call("rpc", node=1, method="eth_blockNumber")["result"], 16)
            second = int(remote.call("rpc", node=2, method="eth_blockNumber")["result"], 16)
            return (first, second) if first >= 2 and second >= first - 2 else None
        heights = wait_until("block synchronization", synced, 120)
        def tee_identity():
            value = remote.call("rpc", node=2, method="eth_getTEEIdentity")["result"]
            required = ("HardwareID", "SigPubKey", "VRFPubKey", "EnclavePK")
            return value if isinstance(value, dict) and all(
                isinstance(value.get(field), str) and
                any(ch != "0" for ch in value[field].removeprefix("0x"))
                for field in required) else None
        identity = wait_until("SGX identity", tee_identity, 180)
        local_status = local.call("status", run_id=run_id)["processes"]
        remote_status = remote.call("status", run_id=run_id)["processes"]
        if len(local_status) != args.committee_count + 1 or not all(local_status.values()):
            raise RuntimeError(f"ordinary host processes not healthy: {local_status}")
        if remote_status.get("geth-node2") is not True:
            raise RuntimeError(f"TEE host process not healthy: {remote_status}")
        print(f"link ready: heights={heights}, tee_identity={identity}, run_id={run_id}")
        print(f"committee on ordinary host; logs: {run_dir(args.local_root, run_id) / 'logs'}")
        print("processes remain running; use 'stop --run-id ...' to stop them")
    except Exception:
        print(f"start failed; inspect logs, then run 'stop --run-id {run_id}'", file=sys.stderr)
        raise


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", action="store_true", help=argparse.SUPPRESS)
    commands = parser.add_subparsers(dest="command")
    start = commands.add_parser("start")
    stop = commands.add_parser("stop")
    for cmd in (start, stop):
        cmd.add_argument("--remote-ssh", required=True, help="SSH alias or user@host")
        cmd.add_argument("--local-root", type=Path, default=ROOT)
        cmd.add_argument("--remote-root", type=Path, default=Path("/opt/tgovern"))
        cmd.add_argument("--run-id", help="run identifier; required for stop")
    start.add_argument("--local-ip", required=True, help="ordinary host reachable LAN IP")
    start.add_argument("--remote-ip", required=True, help="TEE host reachable LAN IP")
    start.add_argument("--ordinary-sgx-mode", choices=("SIM", "HW"), default="SIM")
    start.add_argument("--tee-sgx-mode", choices=("HW", "SIM"), default="HW",
                       help="HW by default; SIM only for explicit network smoke")
    start.add_argument("--committee-count", type=int, default=4)
    start.add_argument("--committee-threshold", type=int, default=3)
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
    main()
