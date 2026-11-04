import importlib.util
import json
import os
import signal
import sys
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, call, patch

SCRIPT = Path(__file__).with_name("tgovern_multi_host.py")
spec = importlib.util.spec_from_file_location("tgovern_multi_host", SCRIPT)
tmh = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tmh)


class TwoHostLinkTests(unittest.TestCase):
    def test_run_dir_rejects_escape(self):
        with self.assertRaises(ValueError):
            tmh.run_dir(Path("/opt/tgovern"), "../../etc")
        self.assertEqual(tmh.run_dir(Path("/opt/tgovern"), "r1"),
                         Path("/opt/tgovern/.tmp/multi-host/r1"))

    def test_port_maps_are_distinct(self):
        # local RPC (18550+i) and forward RPC (28550+i) must not collide, and
        # must agree with sgx_l5's node_rpc base.
        self.assertEqual({tmh.node_rpc_port(i) for i in range(2, 7)}, {18552, 18553, 18554, 18555, 18556})
        self.assertEqual({tmh.fwd_rpc_port(i) for i in range(2, 7)}, {28552, 28553, 28554, 28555, 28556})
        self.assertTrue(set(range(18552, 18560)).isdisjoint(range(28550, 28560)))

    def test_enode_replaces_advertised_address(self):
        raw = "enode://" + "ab" * 64 + "@[::]:30303"
        self.assertEqual(tmh.enode_for_ip(raw, "10.0.0.9", 30312),
                         "enode://" + "ab" * 64 + "@10.0.0.9:30312")
        with self.assertRaises(ValueError):
            tmh.enode_for_ip(raw, "host.example", 30312)

    def test_peer_matches_discovery_id(self):
        self.assertTrue(tmh.peer_matches([{"id": "AB" * 32}], "ab" * 32))
        self.assertFalse(tmh.peer_matches([{"id": "cd" * 32}], "ab" * 32))
        self.assertFalse(tmh.peer_matches(None, "ab" * 32))

    def test_patch_environ_restores(self):
        import os
        os.environ["TEE_MH_TEST"] = "orig"
        with tmh.patch_environ({"TEE_MH_TEST": "overlay", "TEE_MH_NEW": "x"}):
            self.assertEqual(os.environ["TEE_MH_TEST"], "overlay")
            self.assertEqual(os.environ["TEE_MH_NEW"], "x")
        self.assertEqual(os.environ["TEE_MH_TEST"], "orig")
        self.assertNotIn("TEE_MH_NEW", os.environ)

    def test_remote_host_uses_ssh_stdin(self):
        from unittest.mock import patch
        import json
        with patch.object(tmh.subprocess, "run") as run:
            run.return_value.returncode = 0
            run.return_value.stdout = '{"ok":true,"result":"0x1"}'
            host = tmh.Host(Path("/opt/tgovern"), "tee-host")
            self.assertEqual(host.call("rpc", node=5, method="eth_blockNumber")["result"], "0x1")
            self.assertEqual(run.call_args.args[0][0], "ssh")
            self.assertIn("BatchMode=yes", run.call_args.args[0])
            self.assertEqual(json.loads(run.call_args.kwargs["input"])["node"], 5)


class SSHTunnelTests(unittest.TestCase):
    def test_port_preflight_uses_requested_bind_address(self):
        for address in ("0.0.0.0", "127.0.0.1"):
            with self.subTest(address=address), patch.object(tmh.socket, "socket") as socket:
                if address == "0.0.0.0":
                    tmh.assert_port_free(28552)
                else:
                    tmh.assert_port_free(28552, address)
                socket.return_value.__enter__.return_value.bind.assert_called_once_with((address, 28552))

    def test_all_forwards_use_one_process(self):
        for indices in ([2], list(range(2, 18))):
            with self.subTest(nodes=len(indices)), \
                    patch.object(tmh, "assert_port_free") as port_free, \
                    patch.object(Path, "open") as log_open, \
                    patch.object(tmh.subprocess, "Popen") as popen:
                forwards = [(tmh.fwd_rpc_port(i), tmh.node_rpc_port(i)) for i in indices]
                tunnel = tmh.ssh_tunnel("tee-host", forwards, Path("tunnel-rpc.log"))
                self.assertIs(tunnel, popen.return_value)
                popen.assert_called_once()
                argv = popen.call_args.args[0]
                self.assertEqual(argv[0], "ssh")
                self.assertEqual(argv[-1], "tee-host")
                mappings = [argv[i + 1] for i, value in enumerate(argv) if value == "-L"]
                self.assertEqual(mappings, [f"127.0.0.1:{p}:127.0.0.1:{r}"
                                            for p, r in forwards])
                for option in ("BatchMode=yes", "ConnectTimeout=10", "ExitOnForwardFailure=yes",
                               "ServerAliveInterval=15", "ServerAliveCountMax=3", "-N"):
                    self.assertIn(option, argv)
                self.assertEqual([c.args for c in port_free.call_args_list],
                                 [(p, "127.0.0.1") for p, _ in forwards])
                kwargs = popen.call_args.kwargs
                self.assertEqual(kwargs["stdin"], tmh.subprocess.DEVNULL)
                self.assertEqual(kwargs["stderr"], tmh.subprocess.STDOUT)
                self.assertIs(kwargs["stdout"], log_open.return_value.__enter__.return_value)
                self.assertTrue(kwargs["start_new_session"])
                log_open.return_value.__exit__.assert_called_once()

    def test_empty_forwards_do_not_spawn(self):
        with patch.object(tmh.subprocess, "Popen") as popen:
            with self.assertRaisesRegex(ValueError, "at least one"):
                tmh.ssh_tunnel("tee-host", [], Path("tunnel-rpc.log"))
            popen.assert_not_called()

    def test_occupied_port_does_not_spawn(self):
        with patch.object(tmh, "assert_port_free", side_effect=RuntimeError("port busy")), \
                patch.object(tmh.subprocess, "Popen") as popen:
            with self.assertRaisesRegex(RuntimeError, "port busy"):
                tmh.ssh_tunnel("tee-host", [(28552, 18552)], Path("tunnel-rpc.log"))
            popen.assert_not_called()

    def test_exited_tunnel_fails_before_rpc_without_retry(self):
        for returncode in (0, 255):
            with self.subTest(returncode=returncode), \
                    patch.object(Path, "read_text", return_value="Connection closed"), \
                    patch.object(tmh.time, "sleep") as sleep:
                tunnel = Mock()
                tunnel.poll.return_value = returncode
                action = Mock(return_value="0x1")
                with self.assertRaisesRegex(RuntimeError, f"code {returncode}.*Connection closed"):
                    tmh.wait_until("node2 RPC", action, 120,
                                   check=lambda: tmh.check_ssh_tunnel(tunnel, Path("tunnel-rpc.log")))
                action.assert_not_called()
                sleep.assert_not_called()

    def test_exit_during_rpc_retry_fails_on_next_check(self):
        tunnel = Mock()
        tunnel.poll.side_effect = [None, 255]
        action = Mock(side_effect=OSError("not ready"))
        with patch.object(Path, "read_text", return_value="past MaxStartups"), \
                patch.object(tmh.time, "monotonic", side_effect=[0, 1, 2]), \
                patch.object(tmh.time, "sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "SSH RPC tunnel.*past MaxStartups"):
                tmh.wait_until("node2 RPC", action, 120,
                               check=lambda: tmh.check_ssh_tunnel(tunnel, Path("tunnel-rpc.log")))
            action.assert_called_once()
            sleep.assert_called_once_with(1)

    def test_live_tunnel_allows_rpc_retries(self):
        tunnel = Mock()
        tunnel.poll.return_value = None
        action = Mock(side_effect=[OSError("not ready"), "0x1"])
        with patch.object(tmh.time, "monotonic", side_effect=[0, 1, 2]), \
                patch.object(tmh.time, "sleep") as sleep:
            self.assertEqual(tmh.wait_until("node2 RPC", action, 120,
                             check=lambda: tmh.check_ssh_tunnel(tunnel, Path("tunnel-rpc.log"))), "0x1")
            self.assertEqual(action.call_count, 2)
            sleep.assert_called_once_with(1)

    def test_live_tunnel_does_not_hide_rpc_timeout(self):
        tunnel = Mock()
        tunnel.poll.return_value = None
        with patch.object(tmh.time, "monotonic", side_effect=[0, 1, 121]), \
                patch.object(tmh.time, "sleep"):
            with self.assertRaisesRegex(TimeoutError, "node2 RPC.*last error: not ready"):
                tmh.wait_until("node2 RPC", Mock(side_effect=OSError("not ready")), 120,
                               check=lambda: tmh.check_ssh_tunnel(tunnel, Path("tunnel-rpc.log")))

    def test_wait_without_check_preserves_retries(self):
        with patch.object(tmh.time, "monotonic", side_effect=[0, 1, 2]), \
                patch.object(tmh.time, "sleep"):
            self.assertEqual(tmh.wait_until("RPC", Mock(side_effect=[RuntimeError("retry"), "0x1"])),
                             "0x1")

    def test_stop_tunnel_terminates_and_reaps(self):
        tunnel = Mock()
        tunnel.poll.return_value = None
        tmh.stop_ssh_tunnel(tunnel)
        tunnel.terminate.assert_called_once()
        tunnel.wait.assert_called_once_with(timeout=5)
        tunnel.kill.assert_not_called()

    def test_stop_exited_tunnel_still_reaps(self):
        tunnel = Mock()
        tunnel.poll.return_value = 255
        tmh.stop_ssh_tunnel(tunnel)
        tunnel.terminate.assert_not_called()
        tunnel.wait.assert_called_once_with(timeout=5)

    def test_stop_tunnel_kills_after_timeout(self):
        tunnel = Mock()
        tunnel.poll.return_value = None
        tunnel.wait.side_effect = [tmh.subprocess.TimeoutExpired("ssh", 5), 0]
        tmh.stop_ssh_tunnel(tunnel)
        tunnel.terminate.assert_called_once()
        tunnel.kill.assert_called_once()
        self.assertEqual(tunnel.wait.call_count, 2)


class TopologyPeeringTests(unittest.TestCase):
    def test_sparse_dials_only_edges_and_waits_for_one_peer_snapshot(self):
        topo = tmh.topology.build_topology(33, relay_nodes=range(2, 34))
        info = {i: {"id": str(i)} for i in topo["neighbors"]}
        enodes = {i: f"enode-{i}" for i in info}
        waits = []

        def rpc(port, method, params=None):
            if method == "admin_peers":
                return [{"id": str(j)} for j in topo["neighbors"][port]]
            return True

        def wait(label, action, timeout, check):
            check()
            self.assertTrue(action())
            waits.append((label, timeout))

        with patch.object(tmh, "rpc", side_effect=rpc) as calls, \
                patch.object(tmh, "wait_until", side_effect=wait), \
                patch.object(tmh, "ThreadPoolExecutor", wraps=tmh.ThreadPoolExecutor) as pool:
            check = Mock()
            tmh.connect_topology(topo, lambda i: i, info, enodes, check)
        pool.assert_called_once_with(max_workers=32)
        dials = {(c.args[0], c.args[2][0]) for c in calls.call_args_list
                 if c.args[1] == "admin_addPeer"}
        expected = {(src, enodes[dst]) for a, b in topo["edges"]
                    for src, dst in ((a, b), (b, a))}
        self.assertEqual(dials, expected)
        self.assertEqual(len(dials), 264)
        snapshots = [c for c in calls.call_args_list if c.args[1] == "admin_peers"]
        self.assertEqual(len(snapshots), 33)
        self.assertEqual(waits, [(f"node{i} peers", 120 + len(peers) * 15)
                                for i, peers in topo["neighbors"].items()])
        self.assertEqual(check.call_count, 33)

    def test_add_peer_rejection_propagates_before_readiness(self):
        topo = tmh.topology.build_topology(2)
        with patch.object(tmh, "rpc", return_value=False), patch.object(tmh, "wait_until") as wait:
            with self.assertRaisesRegex(RuntimeError, "rejected"):
                tmh.connect_topology(topo, lambda i: i, {1: {"id": "1"}, 2: {"id": "2"}},
                                     {1: "one", 2: "two"}, Mock())
        wait.assert_not_called()

    def test_extra_peers_cannot_replace_a_missing_planned_neighbor(self):
        topo = tmh.topology.build_topology(3, mode="full")
        info = {i: {"id": str(i)} for i in topo["neighbors"]}

        def rpc(_port, method, _params=None):
            return [{"id": "unrelated"}, {"id": "2"}] if method == "admin_peers" else True

        def wait(_label, action, _timeout, check):
            self.assertFalse(action())
            raise TimeoutError("missing planned neighbor")

        with patch.object(tmh, "rpc", side_effect=rpc), patch.object(tmh, "wait_until", side_effect=wait):
            with self.assertRaisesRegex(TimeoutError, "planned neighbor"):
                tmh.connect_topology(topo, lambda i: i, info, {i: str(i) for i in info}, Mock())


class RunWorkspaceTests(unittest.TestCase):
    """All filesystem fixtures stay in the job's scratch directory."""

    def setUp(self):
        scratch = Path(os.environ.get("CLAUDE_JOB_DIR", str(SCRIPT.parent.parent / ".tmp"))) / "tmp"
        scratch.mkdir(parents=True, exist_ok=True)
        temporary = tempfile.TemporaryDirectory(prefix="multi-host-test-", dir=scratch)
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.run_id = "regression"
        self.work = tmh.run_dir(self.root, self.run_id)
        (self.work / "run").mkdir(parents=True)

    def record(self, name, pid=432101, start="1234"):
        path = self.work / "run" / f"{name}.json"
        path.write_text(json.dumps({"pid": pid, "start": start}))
        return path

    def owner(self, token="ours"):
        (self.work / "owner.json").write_text(json.dumps({"owner": token}))

    def config(self, **fields):
        return {"root": str(self.root), "run_id": self.run_id, **fields}


class RuntimeConfigurationTests(RunWorkspaceTests):
    def probe(self, **fields):
        values = dict(
            TEE_CORE_ISOLATION=False, EPOCH_LENGTH=100, MIN_EPOCH_LENGTH=240,
            STEADY_EPOCH_LENGTH=900, GETH_MAX_PEERS=4, EST_SECONDS_PER_BLOCK=4.0,
            GETH_P2P_MSG_DELAY_MS=7, GETH_BLOCK_BROADCAST_LIMIT=5,
            GETH_BLOCK_BROADCAST_LIMIT_TEE=2, DISTKG_RELAY_TTL=6,
            BLOCKS_PER_REGISTERED_NODE=12,
            DKRS_RELAY_TTL=8, TXMODIFY_RELAY_TTL=10, CORE_NODE_CACHE_MB=96,
        )
        values.update(fields)
        return SimpleNamespace(**values)

    def args(self):
        return SimpleNamespace(local_root=self.root, tee_count=4, active_limit=3,
                               active_threshold=2, light_nodes=2, committee_count=4,
                               committee_threshold=3, epoch_length=100,
                               blocks_per_registered_node=0,
                               sgx_mode="SIM", gomaxprocs=3)

    def test_runtime_resolves_bootstrap_steady_and_explicit_overrides(self):
        imported_env = {}
        loader = Mock()
        loader.exec_module.side_effect = lambda _module: imported_env.update(os.environ)
        module = self.probe()
        overlay = {"TEE_ACTIVE_LIMIT": "99", "TEE_STEADY_EPOCH_LENGTH": "900",
                   "TEE_HEADWATCH": "1", "SGX_SDK": "/test/sdk", "GOGC": "60"}
        with patch.dict(os.environ, overlay), \
                patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=loader)), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=module):
            runtime = tmh.resolve_runtime(self.args())
            self.assertEqual(os.environ["TEE_ACTIVE_LIMIT"], "99")
        self.assertEqual(runtime["epoch_length"], 240)
        self.assertEqual(runtime["steady_epoch_length"], 900)
        self.assertEqual(runtime["active_limit"], 3)
        self.assertEqual(runtime["active_threshold"], 2)
        self.assertEqual(runtime["maxpeers"], 12)
        self.assertEqual(runtime["topology"]["mode"], "sparse")
        self.assertEqual(runtime["dkrs_relay_max_peers"], 4)
        self.assertEqual(imported_env["P2P_MESSAGE_RELAY_TTL"], "4")
        self.assertEqual(runtime["geth_env"], {"SGX_SDK": "/test/sdk", "GOGC": "60"})
        self.assertTrue(runtime["headwatch"])
        self.assertEqual(imported_env["EPOCH_LENGTH"], "100")
        self.assertEqual(imported_env["TEE_ACTIVE_LIMIT"], "3")
        self.assertEqual(imported_env["TEE_ACTIVE_THRESHOLD"], "2")
        self.assertEqual(imported_env["TEE_EXTRA_MINERS"], "0")
        self.assertEqual(imported_env["LIGHT_NODE_COUNT"], "2")

    def test_32_tee_runtime_uses_sparse_degree_and_ttl_floor(self):
        args = self.args()
        args.tee_count = 32
        args.light_nodes = 0
        args.topology = "sparse"
        args.topology_radius = 2
        args.topology_max_degree = 9
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec",
                             return_value=self.probe(GETH_MAX_PEERS=50, DISTKG_RELAY_TTL=0,
                                                     DKRS_RELAY_TTL=1, TXMODIFY_RELAY_TTL=2)):
            runtime = tmh.resolve_runtime(args)
        self.assertEqual(runtime["topology"]["edge_count"], 132)
        self.assertEqual(runtime["topology"]["relay_diameter"], 3)
        self.assertEqual(runtime["maxpeers"], 50)
        self.assertEqual(runtime["distkg_relay_ttl"], 4)
        self.assertEqual(runtime["dkrs_relay_ttl"], 4)
        self.assertEqual(runtime["txmodify_relay_ttl"], 4)

    def test_full_mode_still_expands_the_dial_budget(self):
        args = self.args()
        args.tee_count = 32
        args.light_nodes = 0
        args.topology = "full"
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=self.probe(GETH_MAX_PEERS=50)):
            runtime = tmh.resolve_runtime(args)
        self.assertEqual(runtime["maxpeers"], 96)
        self.assertEqual(runtime["topology"]["edge_count"], 528)

    def test_ring_ttl_covers_tee_paths_without_ordinary_relays(self):
        args = self.args()
        args.tee_count = 32
        args.light_nodes = 0
        args.topology_radius = 1
        args.topology_max_degree = 2
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=self.probe()):
            runtime = tmh.resolve_runtime(args)
        self.assertEqual(runtime["topology"]["diameter"], 16)
        self.assertEqual(runtime["topology"]["relay_diameter"], 31)
        for key in ("distkg_relay_ttl", "dkrs_relay_ttl", "txmodify_relay_ttl"):
            self.assertEqual(runtime[key], 32)

    def test_blocks_per_registered_node_lowers_the_epoch_floor_only_when_requested(self):
        # Default: the flag forces nothing, so sgx_l5's own budget (here the
        # ambient one) decides the floor.
        imported_env = {}
        default_loader = Mock()
        default_loader.exec_module.side_effect = lambda _module: imported_env.update(os.environ)
        with patch.dict(os.environ, {"TEE_BLOCKS_PER_REGISTERED_NODE": "99"}), \
                patch.object(tmh.importlib.util, "spec_from_file_location",
                             return_value=SimpleNamespace(loader=default_loader)), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=self.probe()):
            tmh.resolve_runtime(self.args())
        self.assertEqual(imported_env["TEE_BLOCKS_PER_REGISTERED_NODE"], "99")

        # Requested: the budget is overridden, the floor it produces is what the
        # nodes are configured with, and the reported minimum matches.
        args = self.args()
        args.blocks_per_registered_node = 3
        floor = 48 + 3 * args.tee_count + 20 + 24
        requested = {}
        loader = Mock()
        loader.exec_module.side_effect = lambda _module: requested.update(os.environ)
        probe = self.probe(MIN_EPOCH_LENGTH=floor)
        probe.BLOCKS_PER_REGISTERED_NODE = 3
        with patch.object(tmh.importlib.util, "spec_from_file_location",
                          return_value=SimpleNamespace(loader=loader)), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=probe):
            runtime = tmh.resolve_runtime(args)
        self.assertEqual(requested["TEE_BLOCKS_PER_REGISTERED_NODE"], "3")
        self.assertEqual(runtime["min_epoch_length"], floor)
        self.assertEqual(runtime["blocks_per_registered_node"], 3)
        self.assertEqual(runtime["epoch_length"], floor)

    def test_runtime_rejects_core_isolation_and_restores_environment(self):
        before = dict(os.environ)
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=self.probe(TEE_CORE_ISOLATION=True)):
            with self.assertRaisesRegex(ValueError, "TEE_CORE_ISOLATION"):
                tmh.resolve_runtime(self.args())
        self.assertEqual(dict(os.environ), before)

    def test_slow_chain_defaults_are_not_the_old_fast_chain_budget(self):
        # ~10s blocks: the bootstrap/steady budgets and the block-time estimate
        # must not fall back to the historical 1-2s-chain values.
        self.assertEqual(tmh.DEFAULT_BOOTSTRAP_EPOCH, 128)
        self.assertEqual(tmh.DEFAULT_STEADY_EPOCH, 120)
        args = self.args()
        args.epoch_length = tmh.DEFAULT_BOOTSTRAP_EPOCH
        args.blocks_per_registered_node = 1
        args.est_seconds_per_block = 15.0
        imported_env = {}
        loader = Mock()
        loader.exec_module.side_effect = lambda _module: imported_env.update(os.environ)
        # STEADY_EPOCH_LENGTH is deliberately absent: the default must inject it.
        clean = {name: os.environ[name] for name in
                 ("TEE_STEADY_EPOCH_LENGTH", "STEADY_EPOCH_LENGTH") if name in os.environ}
        for name in clean:
            os.environ.pop(name)
        self.addCleanup(os.environ.update, clean)
        with patch.dict(os.environ, {"EPOCH_LENGTH": "128",
                                     "TEE_BLOCKS_PER_REGISTERED_NODE": "1",
                                     "EST_SECONDS_PER_BLOCK": "15.0"}), \
                patch.object(tmh.importlib.util, "spec_from_file_location",
                             return_value=SimpleNamespace(loader=loader)), \
                patch.object(tmh.importlib.util, "module_from_spec",
                             return_value=self.probe(MIN_EPOCH_LENGTH=124, EPOCH_LENGTH=128,
                                                     STEADY_EPOCH_LENGTH=120,
                                                     BLOCKS_PER_REGISTERED_NODE=1,
                                                     EST_SECONDS_PER_BLOCK=15.0)):
            runtime = tmh.resolve_runtime(args)
        # The floor for 32 TEEs at budget 1 is 124, so 128 survives untouched.
        self.assertEqual(runtime["epoch_length"], 128)
        self.assertEqual(runtime["steady_epoch_length"], 120)
        self.assertEqual(runtime["est_seconds_per_block"], 15.0)
        self.assertEqual(imported_env["STEADY_EPOCH_LENGTH"], "120")
        self.assertEqual(imported_env["EST_SECONDS_PER_BLOCK"], "15.0")

    def test_environment_steady_epoch_still_overrides_the_default(self):
        args = self.args()
        probe = self.probe(EPOCH_LENGTH=240, MIN_EPOCH_LENGTH=240, STEADY_EPOCH_LENGTH=900)
        imported_env = {}
        loader = Mock()
        loader.exec_module.side_effect = lambda _module: imported_env.update(os.environ)
        with patch.dict(os.environ, {"STEADY_EPOCH_LENGTH": "900"}), \
                patch.object(tmh.importlib.util, "spec_from_file_location",
                             return_value=SimpleNamespace(loader=loader)), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=probe):
            runtime = tmh.resolve_runtime(args)
        self.assertEqual(imported_env["STEADY_EPOCH_LENGTH"], "900")
        self.assertEqual(runtime["steady_epoch_length"], 900)

    def test_worker_start_passes_runtime_to_every_node_role(self):
        (self.root / "helper.json").write_text("{}")
        (self.work / "accounts").mkdir()
        runtime = dict(epoch_length=240, steady_epoch_length=900, active_limit=3,
                       active_threshold=2, maxpeers=18, sgx_mode="SIM", gomaxprocs=3,
                       p2p_msg_delay_ms=7, block_broadcast_limit=5,
                       block_broadcast_limit_tee=2, distkg_relay_ttl=6,
                       dkrs_relay_ttl=8, txmodify_relay_ttl=10, core_cache_mb=96,
                       headwatch=True, dkrs_relay_max_peers=9,
                       geth_env={"SGX_SDK": "/test/sdk", "GOGC": "60"})
        for index, role in ((1, {"mine": True}), (2, {"teehost": True}),
                            (3, {"light": True, "light_gomaxprocs": 2, "light_cache_mb": 12})):
            nodework = self.work / "node-work" / f"node{index}"
            nodework.mkdir(parents=True)
            account = {"address": "0x" + "ab" * 20, "key": "cd" * 32}
            (self.work / "accounts" / f"node{index}.json").write_text(json.dumps(account))
            with self.subTest(role=role), patch.object(tmh, "spawn", return_value=123) as spawn:
                result = tmh.worker_start(self.config(node=index, network_id=739205, **runtime, **role))
                self.assertEqual(result, {"pid": 123})
                work, name, argv, env, cwd = spawn.call_args.args
                self.assertEqual((work, name, cwd), (self.work, f"node{index}", nodework))
                self.assertEqual(env["EPOCH_LENGTH"], "240")
                self.assertEqual(env["STEADY_EPOCH_LENGTH"], "900")
                self.assertEqual(env["TEE_STEADY_EPOCH_LENGTH"], "900")
                self.assertEqual(env["TEE_ACTIVE_LIMIT"], "3")
                self.assertEqual(env["TEE_ACTIVE_THRESHOLD"], "2")
                self.assertEqual(env["GOGC"], "60")
                self.assertIn("/test/sdk/lib64", env["LD_LIBRARY_PATH"])
                for flag, expected in (("--maxpeers", "18"), ("--p2p.msg-delay-ms", "7"),
                                       ("--tee.distkg-relay-ttl", "6"), ("--tee.dkrs-relay-ttl", "8"),
                                       ("--txmodify.relay-ttl", "10")):
                    self.assertEqual(argv[argv.index(flag) + 1], expected)
                self.assertEqual(argv[argv.index("--p2p.block-broadcast-limit") + 1],
                                 "2" if role.get("teehost") else "5")
                if role.get("teehost"):
                    self.assertIn("--tee.headwatch", argv)
                    self.assertIn("--teehost", argv)
                    self.assertIn("--tee.dkrs-relay", argv)
                    self.assertEqual(argv[argv.index("--tee.dkrs-relay-max-peers") + 1], "9")
                    self.assertEqual(env["SGX_ENCLAVE_PATH"], str(self.root / "sgx" / "enclave.signed.so"))
                else:
                    self.assertNotIn("--tee.headwatch", argv)
                if role.get("light"):
                    self.assertIn("--ipcdisable", argv)
                    self.assertEqual(env["GOMAXPROCS"], "2")
                    self.assertEqual(argv[argv.index("--cache") + 1], "12")
                else:
                    self.assertEqual(env["GOMAXPROCS"], "3")
                    self.assertEqual(argv[argv.index("--cache") + 1], "96")

    def test_system_params_accepts_decimal_and_hex_rpc_values(self):
        runtime = {"epoch_length": 240, "active_limit": 3, "active_threshold": 2}
        for params in ({"ConfiguredEpochLength": 240, "ActiveLimit": 3, "ActiveThreshold": 2},
                       {"ConfiguredEpochLength": "0xf0", "ActiveLimit": "3", "ActiveThreshold": "0x2"}):
            with self.subTest(params=params), patch.object(tmh, "rpc", return_value=params) as rpc:
                self.assertIs(tmh.validate_system_params(runtime), params)
                rpc.assert_called_once_with(tmh.node_rpc_port(1), "eth_getSystemParams")

    def test_system_params_rejects_each_deployed_mismatch(self):
        runtime = {"epoch_length": 240, "active_limit": 3, "active_threshold": 2}
        correct = {"ConfiguredEpochLength": 240, "ActiveLimit": 3, "ActiveThreshold": 2}
        for field in correct:
            wrong = {**correct, field: correct[field] + 1}
            with self.subTest(field=field), patch.object(tmh, "rpc", return_value=wrong):
                with self.assertRaisesRegex(AssertionError, f"deployed {field}: actual=.*expected="):
                    tmh.validate_system_params(runtime)


class SafeWorkerStopTests(RunWorkspaceTests):
    def test_owner_mismatch_or_missing_owner_never_touches_resources(self):
        record = self.record("node1")
        (self.work / "netem.json").write_text("{}")
        for present in (False, True):
            if present:
                self.owner("someone-else")
            with self.subTest(owner_present=present), patch.object(tmh, "process_info") as process_info, \
                    patch.object(tmh.os, "kill") as kill, patch.object(tmh, "worker_netem") as netem:
                result = tmh.worker_stop(self.config(owner="ours"))
                self.assertEqual(result["skipped"], "workspace not owned")
                self.assertTrue(record.exists())
                self.assertTrue((self.work / "netem.json").exists())
                process_info.assert_not_called()
                kill.assert_not_called()
                netem.assert_not_called()

    def test_malformed_record_does_not_block_nodes_tunnels_or_netem_cleanup(self):
        malformed = self.work / "run" / "committee-1.json"
        malformed.write_text("not JSON")
        node = self.record("node1", pid=432101)
        tunnel = self.record("tunnel-rpc", pid=432102)
        (self.work / "netem.json").write_text(json.dumps({"dev": "eth0"}))
        calls = {}

        def state(pid):
            calls[pid] = calls.get(pid, 0) + 1
            return "1234", "S" if calls[pid] == 1 else "Z"

        with patch.object(tmh, "process_info", side_effect=state), \
                patch.object(Path, "read_bytes", return_value=str(self.work / "chain" / "node1").encode()), \
                patch.object(tmh.os, "kill") as kill, \
                patch.object(tmh.time, "monotonic", return_value=0), \
                patch.object(tmh, "worker_netem") as netem:
            with self.assertRaisesRegex(RuntimeError, "committee-1.json"):
                tmh.worker_stop(self.config())
            self.assertEqual(kill.call_args_list, [call(432101, signal.SIGTERM), call(432102, signal.SIGTERM)])
            netem.assert_called_once_with(self.config(off=True))
        self.assertTrue(malformed.exists())
        self.assertFalse(node.exists())
        self.assertFalse(tunnel.exists())

    def test_reused_or_unrelated_pid_is_retained_and_never_killed(self):
        for start, cmdline, error in (("changed", str(self.work), "reused"),
                                      ("1234", "/unrelated/run", "does not belong")):
            record = self.record("node1")
            with self.subTest(error=error), patch.object(tmh, "process_info", return_value=(start, "S")), \
                    patch.object(Path, "read_bytes", return_value=cmdline.encode()), \
                    patch.object(tmh.os, "kill") as kill:
                with self.assertRaisesRegex(RuntimeError, error):
                    tmh.worker_stop(self.config())
                kill.assert_not_called()
                self.assertTrue(record.exists())
            record.unlink()

    def test_dead_or_zombie_record_is_removed_without_signalling(self):
        for state in (FileNotFoundError("gone"), ("1234", "Z")):
            record = self.record("node1")
            with self.subTest(state=state), patch.object(tmh.os, "kill") as kill, \
                    patch.object(tmh, "process_info", **({"side_effect": state} if isinstance(state, Exception)
                                                         else {"return_value": state})):
                self.assertEqual(tmh.worker_stop(self.config()), {"stopped": []})
                kill.assert_not_called()
                self.assertFalse(record.exists())

    def test_process_exiting_during_cmdline_read_is_not_a_cleanup_error(self):
        # The kernel returns ESRCH for /proc/<pid>/cmdline once the process is
        # gone; that surfaces as ProcessLookupError and must be treated as
        # "already exited", not as a failed cleanup.
        record = self.record("committee-15")
        with patch.object(tmh, "process_info", return_value=("1234", "S")), \
                patch.object(Path, "read_bytes", side_effect=ProcessLookupError(3, "No such process")), \
                patch.object(tmh.os, "kill") as kill:
            self.assertEqual(tmh.worker_stop(self.config()), {"stopped": []})
            kill.assert_not_called()
            self.assertFalse(record.exists())

    def test_stop_hosts_attempts_every_host_and_reports_each_error(self):
        local = Mock(ssh_host=None)
        remote = Mock(ssh_host="tee-host")
        local.call.side_effect = OSError("local cleanup failed")
        remote.call.side_effect = RuntimeError("remote cleanup failed")
        errors = tmh.stop_hosts(local, remote, self.run_id, owner="ours")
        self.assertEqual(len(errors), 2)
        self.assertIn("remote cleanup failed", errors[0])
        self.assertIn("local cleanup failed", errors[1])
        local.call.assert_called_once_with("stop", run_id=self.run_id, owner="ours")
        remote.call.assert_called_once_with("stop", run_id=self.run_id, owner="ours")

    def test_stop_hosts_only_stops_attempted_hosts(self):
        local = Mock(ssh_host=None)
        remote = Mock(ssh_host="tee-host")
        self.assertEqual(tmh.stop_hosts(local, remote, self.run_id, owner="ours", hosts=[local]), [])
        local.call.assert_called_once_with("stop", run_id=self.run_id, owner="ours")
        remote.call.assert_not_called()


class NetemOwnershipTests(RunWorkspaceTests):
    def config(self, **fields):
        defaults = dict(dev="eth0", peer="10.0.0.2", delay_ms=12, jitter_ms=2,
                        loss_percent=0.5, p2p_ports=[30311, 30312])
        return super().config(**{**defaults, **fields})

    def netem_owner(self, run_id=None):
        rid = run_id or self.run_id
        device = {"dev": "eth0", "handle": "1234:", "dst": "10.0.0.2", "ports": [30311, 30312]}
        marker = {"run_id": rid, "devices": [device]}
        (self.work / "netem.json").write_text(json.dumps(marker))
        lock = self.work.parent / "netem-eth0.json"
        lock.write_text(json.dumps({"run_id": rid, "dev": "eth0", "handle": "1234:"}))
        return device, lock

    def test_unowned_netem_is_never_deleted(self):
        with patch.object(tmh, "qdisc_roots", return_value=[{"root": True, "kind": "prio", "handle": "1234:"}]), \
                patch.object(tmh.subprocess, "run") as run:
            self.assertEqual(tmh.worker_netem(self.config(off=True)), {"netem": "not owned"})
            run.assert_not_called()

    def test_failed_prepare_cannot_remove_netem_from_old_workspace_owner(self):
        self.owner("previous-owner")
        _info, lock = self.netem_owner()
        with self.assertRaises(FileExistsError):
            tmh.worker_prepare(self.config(nodes=[1], owner="new-owner"))
        with patch.object(tmh, "qdisc_roots") as roots, \
                patch.object(tmh.subprocess, "run") as run:
            result = tmh.worker_netem(self.config(owner="new-owner", off=True))
            self.assertEqual(result, {"netem": "not owned"})
            roots.assert_not_called()
            run.assert_not_called()
        self.assertTrue(lock.exists())
        self.assertTrue((self.work / "netem.json").exists())
        self.assertEqual(json.loads((self.work / "owner.json").read_text())["owner"], "previous-owner")

    def test_other_run_device_lock_prevents_removal(self):
        _info, lock = self.netem_owner()
        lock.write_text(json.dumps({"run_id": "other-run"}))
        with patch.object(tmh, "qdisc_roots", return_value=[{"kind": "prio", "handle": "1234:"}]), \
                patch.object(tmh.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "ownership mismatch"):
                tmh.worker_netem(self.config(off=True))
            run.assert_not_called()
        self.assertTrue(lock.exists())
        self.assertTrue((self.work / "netem.json").exists())

    def test_changed_root_is_never_deleted_and_ownership_is_retained(self):
        _info, lock = self.netem_owner()
        for roots in ([{"kind": "prio", "handle": "9999:"}],
                      [{"kind": "fq_codel", "handle": "9999:"}]):
            with self.subTest(roots=roots), patch.object(tmh, "qdisc_roots", return_value=roots), \
                    patch.object(tmh.subprocess, "run") as run:
                with self.assertRaisesRegex(RuntimeError, "root changed"):
                    tmh.worker_netem(self.config(off=True))
                run.assert_not_called()
                self.assertTrue(lock.exists())
                self.assertTrue((self.work / "netem.json").exists())

    def test_lock_shaped_marker_from_older_build_is_still_unwound(self):
        # A marker written by an older build held the single-device lock shape
        # ({"run_id", "dev", "handle"}) rather than {"devices": [...]}.  Teardown
        # must still release the owned qdisc instead of raising a raw KeyError
        # that aborts cleanup and strands the delay on the device.
        lock = self.work.parent / "netem-eth0.json"
        lock.write_text(json.dumps({"run_id": self.run_id, "dev": "eth0", "handle": "1234:"}))
        (self.work / "netem.json").write_text(
            json.dumps({"run_id": self.run_id, "dev": "eth0", "handle": "1234:"}))
        with patch.object(tmh, "qdisc_roots", return_value=[{"kind": "prio", "handle": "1234:"}]), \
                patch.object(tmh.subprocess, "run") as run:
            self.assertEqual(tmh.worker_netem(self.config(off=True))["netem"], "off")
            run.assert_called_once_with(["tc", "qdisc", "del", "dev", "eth0", "root"], check=True)
        self.assertFalse(lock.exists())
        self.assertFalse((self.work / "netem.json").exists())

    def test_marker_without_device_list_is_refused_not_keyerror(self):
        lock = self.work.parent / "netem-eth0.json"
        lock.write_text(json.dumps({"run_id": self.run_id, "dev": "eth0", "handle": "1234:"}))
        (self.work / "netem.json").write_text(json.dumps({"run_id": self.run_id, "junk": 1}))
        with patch.object(tmh, "qdisc_roots") as roots, patch.object(tmh.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "lacks a device list"):
                tmh.worker_netem(self.config(off=True))
            roots.assert_not_called()
            run.assert_not_called()
        self.assertTrue(lock.exists())
        self.assertTrue((self.work / "netem.json").exists())

    def test_owned_root_is_removed_and_records_are_released(self):
        _info, lock = self.netem_owner()
        with patch.object(tmh, "qdisc_roots", return_value=[{"kind": "prio", "handle": "1234:"}]), \
                patch.object(tmh.subprocess, "run") as run:
            self.assertEqual(tmh.worker_netem(self.config(off=True))["netem"], "off")
            run.assert_called_once_with(["tc", "qdisc", "del", "dev", "eth0", "root"], check=True)
        self.assertFalse(lock.exists())
        self.assertFalse((self.work / "netem.json").exists())

    def test_existing_qdisc_or_filters_are_not_replaced(self):
        with patch.object(tmh, "qdisc_roots", return_value=[{"kind": "fq_codel", "handle": "1:"}]), \
                patch.object(tmh.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "existing qdisc"):
                tmh.worker_netem(self.config())
            run.assert_not_called()
        with patch.object(tmh, "qdisc_roots", return_value=[{"kind": "fq_codel", "handle": "0:"}]), \
                patch.object(tmh.subprocess, "run", return_value=SimpleNamespace(stdout='[{"filter": 1}]')) as run:
            with self.assertRaisesRegex(RuntimeError, "existing filters"):
                tmh.worker_netem(self.config())
            run.assert_called_once_with(["tc", "-j", "filter", "show", "dev", "eth0"],
                                        check=True, capture_output=True, text=True)
        self.assertFalse((self.work / "netem.json").exists())

    def test_setup_persists_ownership_before_qdisc_failure_for_later_cleanup(self):
        failure = tmh.subprocess.CalledProcessError(1, "tc")
        with patch.object(tmh, "qdisc_roots", return_value=[]), \
                patch.object(tmh.subprocess, "run", side_effect=[SimpleNamespace(stdout="[]"), failure]):
            with self.assertRaises(tmh.subprocess.CalledProcessError) as raised:
                tmh.worker_netem(self.config())
        self.assertIs(raised.exception, failure)
        # The failed install rolls its own device back, so nothing is left
        # half-applied; the marker is cleared too.
        self.assertFalse((self.work / "netem.json").exists())
        self.assertFalse((self.work.parent / "netem-eth0.json").exists())

    def test_stale_device_lock_from_crashed_run_is_reclaimed(self):
        # A lock left by a crashed/powered-off run is stale bookkeeping: the
        # device is free (default qdisc, no filters), so setup must reclaim the
        # lock rather than abort the whole run.
        lock = self.work.parent / "netem-eth0.json"
        lock.write_text(json.dumps({"run_id": "crashed-run", "dev": "eth0", "handle": "dead:"}))
        with patch.object(tmh, "qdisc_roots", return_value=[]), \
                patch.object(tmh.subprocess, "run", return_value=SimpleNamespace(stdout="[]")) as run:
            result = tmh.worker_netem(self.config())
        self.assertEqual(result["netem"], "on")
        self.assertEqual(json.loads(lock.read_text())["run_id"], self.run_id)
        self.assertTrue((self.work / "netem.json").exists())

    def test_undecodable_device_lock_is_reclaimed(self):
        lock = self.work.parent / "netem-eth0.json"
        lock.write_text("not json")
        with patch.object(tmh, "qdisc_roots", return_value=[]), \
                patch.object(tmh.subprocess, "run", return_value=SimpleNamespace(stdout="[]")):
            result = tmh.worker_netem(self.config())
        self.assertEqual(result["netem"], "on")
        self.assertEqual(json.loads(lock.read_text())["run_id"], self.run_id)


class ControllerCleanupTests(RunWorkspaceTests):
    def setUp(self):
        super().setUp()
        (self.root / "genesis.json").write_text('{"config": {"chainId": 739205}}')
        self.args = SimpleNamespace(
            local_root=self.root, remote_root=self.root / "remote", remote_ssh="tee-host",
            local_ip="10.0.0.1", remote_ip="10.0.0.1", run_id=self.run_id,
            tee_count=1, active_limit=1, active_threshold=1, light_nodes=0, light_remote=False,
            committee_count=1, committee_threshold=1, epoch_length=240, rotations=1,
            blocks_per_registered_node=0,
            baseline_edits=1, sgx_mode="SIM", eventpoll="200ms", attestation="noop",
            netem_delay=0, netem_dev="eth0", netem_lo_dev="lo", no_netem_lo=False,
            keep_running=False, no_collect=True,
            genesis_difficulty="0", est_seconds_per_block=0.0, on_tx_rate=0.0, tx_per_block=0.0,
        )
        self.runtime = dict(epoch_length=240, steady_epoch_length=900, active_limit=1,
                            active_threshold=1, sgx_mode="SIM", maxpeers=6,
                            est_seconds_per_block=4.0,
                            topology=tmh.topology.build_topology(2, relay_nodes=[2]),
                            distkg_relay_ttl=4, dkrs_relay_ttl=4, txmodify_relay_ttl=4)
        self.local = Mock(ssh_host=None)
        self.remote = Mock(ssh_host="tee-host")
        self.local.call.return_value = self.remote.call.return_value = {"stopped": []}
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        account = Mock()
        account.create.return_value.key = bytes.fromhex("ab" * 32)
        account.from_key.return_value.address = "0x" + "cd" * 20
        self.stack.enter_context(patch.dict(sys.modules, {"eth_account": SimpleNamespace(Account=account)}))
        self.stack.enter_context(patch.object(tmh, "resolve_runtime", return_value=self.runtime))
        self.stack.enter_context(patch.object(tmh, "Host", side_effect=[self.local, self.remote]))
        self.stack.enter_context(patch.object(tmh, "print"))
        self.stack.enter_context(patch.object(tmh, "wait_until", side_effect=self.wait))
        self.stack.enter_context(patch.object(tmh, "rpc", side_effect=self.rpc))
        self.validate = self.stack.enter_context(patch.object(tmh, "validate_system_params", return_value={}))
        self.drive = self.stack.enter_context(patch.object(tmh, "drive_l5", return_value=0))
        self.collect = self.stack.enter_context(patch.object(tmh, "collect_timing"))

    def wait(self, label, *_args, **_kwargs):
        return "0x" + "ef" * 20 if label == "contract deploy" else True

    def rpc(self, port, method, *_args):
        if method == "admin_nodeInfo":
            return {"id": str(port), "enode": "enode://" + "ab" * 64 + "@127.0.0.1:30303"}
        return True

    def status(self):
        return json.loads((self.root / "out" / self.run_id / "run-status.json").read_text())

    def test_peer_rejection_stops_both_hosts_and_preserves_error(self):
        original_rpc = self.rpc
        def reject(port, method, *params):
            return False if method == "admin_addPeer" else original_rpc(port, method, *params)
        with patch.object(tmh, "rpc", side_effect=reject):
            with self.assertRaisesRegex(RuntimeError, "rejected"):
                tmh.command_start(self.args)
        for host in (self.local, self.remote):
            self.assertEqual(host.call.call_args.args, ("stop",))
        self.drive.assert_not_called()
        self.assertFalse(self.status()["success"])
        self.assertIn("rejected", self.status()["error"])

    def test_peer_timeout_stops_both_hosts(self):
        original_wait = self.wait
        def wait(label, *args, **kwargs):
            if label.endswith(" peers"):
                raise TimeoutError("peer readiness timed out")
            return original_wait(label, *args, **kwargs)
        with patch.object(tmh, "wait_until", side_effect=wait):
            with self.assertRaisesRegex(TimeoutError, "peer readiness"):
                tmh.command_start(self.args)
        for host in (self.local, self.remote):
            self.assertEqual(host.call.call_args.args, ("stop",))
        self.drive.assert_not_called()
        self.assertFalse(self.status()["success"])

    def test_keep_running_preserves_failed_peering_for_debugging(self):
        self.args.keep_running = True
        with patch.object(tmh, "connect_topology", side_effect=RuntimeError("peering failed")):
            with self.assertRaisesRegex(RuntimeError, "peering failed"):
                tmh.command_start(self.args)
        for host in (self.local, self.remote):
            self.assertNotIn("stop", [c.args[0] for c in host.call.call_args_list])
        self.assertFalse(self.status()["success"])
        self.assertIn("peering failed", self.status()["error"])

    def test_local_prepare_failure_cleans_only_attempted_host_with_owner_token(self):
        failure = RuntimeError("local preparation failed")
        self.local.call.side_effect = lambda action, **_fields: (_ for _ in ()).throw(failure) if action == "prepare" else {}
        with self.assertRaises(RuntimeError) as raised:
            tmh.command_start(self.args)
        self.assertIs(raised.exception, failure)
        self.remote.call.assert_not_called()
        self.assertEqual([c.args[0] for c in self.local.call.call_args_list], ["prepare", "stop"])
        prepare, stop = self.local.call.call_args_list
        self.assertTrue(prepare.kwargs["owner"])
        self.assertEqual(stop.kwargs["owner"], prepare.kwargs["owner"])
        self.assertFalse(self.status()["success"])
        self.assertIn("local preparation failed", self.status()["error"])

    def test_remote_prepare_failure_still_cleans_both_hosts(self):
        failure = RuntimeError("remote preparation failed")
        self.remote.call.side_effect = lambda action, **_fields: (_ for _ in ()).throw(failure) if action == "prepare" else {}
        with self.assertRaises(RuntimeError) as raised:
            tmh.command_start(self.args)
        self.assertIs(raised.exception, failure)
        self.assertEqual([c.args[0] for c in self.local.call.call_args_list], ["prepare", "stop"])
        self.assertEqual([c.args[0] for c in self.remote.call.call_args_list], ["prepare", "stop"])
        local_owner = self.local.call.call_args_list[0].kwargs["owner"]
        remote_owner = self.remote.call.call_args_list[0].kwargs["owner"]
        self.assertEqual(local_owner, remote_owner)
        self.assertEqual(self.remote.call.call_args.kwargs["owner"], local_owner)

    def test_startup_primary_error_is_not_replaced_by_cleanup_errors(self):
        primary = RuntimeError("node1 startup failed")

        def local_call(action, **_fields):
            if action == "start":
                raise primary
            if action == "stop":
                raise OSError("local stop failed")
            return {}

        self.local.call.side_effect = local_call
        self.remote.call.side_effect = lambda action, **_fields: (_ for _ in ()).throw(RuntimeError("remote stop failed")) if action == "stop" else {}
        with self.assertRaises(RuntimeError) as raised:
            tmh.command_start(self.args)
        self.assertIs(raised.exception, primary)
        status = self.status()
        self.assertFalse(status["success"])
        self.assertIn("node1 startup failed", status["error"])
        self.assertEqual(len(status["cleanup_errors"]), 2)
        self.assertIn("remote stop failed", status["cleanup_errors"][0])
        self.assertIn("local stop failed", status["cleanup_errors"][1])

    def test_controller_passes_runtime_to_workers_and_validates_before_committee(self):
        tmh.command_start(self.args)
        for host in (self.local, self.remote):
            start = next(c for c in host.call.call_args_list if c.args[0] == "start")
            for field, value in self.runtime.items():
                self.assertEqual(start.kwargs[field], value)
        self.validate.assert_called_once_with(self.runtime)
        self.drive.assert_called_once()
        self.assertTrue(self.status()["success"])
        self.assertIsNone(self.status()["error"])
        self.assertEqual(self.status()["cleanup_errors"], [])

    def test_keep_running_retains_processes_but_removes_owned_netem_on_both_hosts(self):
        self.args.keep_running = True
        self.args.netem_delay = 12
        self.args.netem_dev = "eth0"
        self.args.netem_jitter = 2
        self.args.netem_loss = 0.5
        self.args.committee_count = 2
        self.args.netem_limit = 10000
        tmh.command_start(self.args)
        for host in (self.local, self.remote):
            actions = [c.args[0] for c in host.call.call_args_list]
            self.assertNotIn("stop", actions)
            prepare = next(c for c in host.call.call_args_list if c.args[0] == "prepare")
            netem_calls = [c for c in host.call.call_args_list if c.args[0] == "netem"]
            self.assertEqual(len(netem_calls), 2)
            install, teardown = netem_calls
            self.assertEqual(install.kwargs["delay_ms"], 12)
            # cross-host on the ethernet device, same-host on loopback, so both
            # geth P2P and committee sockets are delayed on each host.
            self.assertEqual(install.kwargs["dev"], "eth0")
            self.assertEqual(install.kwargs["loopback_dev"], "lo")
            self.assertEqual(install.kwargs["p2p_ports"], [30311, 30312])
            self.assertEqual(install.kwargs["loopback_ports"],
                             [3401, 3402, 30311, 30312])
            self.assertEqual(teardown, call("netem", run_id=self.run_id,
                                            owner=prepare.kwargs["owner"], off=True))
        self.assertEqual(self.status()["cleanup_errors"], [])
        self.assertIsNone(self.status()["error"])

    def test_rpc_parameter_mismatch_stops_before_committee_or_tee_start(self):
        self.validate.side_effect = AssertionError("deployed ActiveThreshold mismatch")
        with self.assertRaisesRegex(AssertionError, "ActiveThreshold"):
            tmh.command_start(self.args)
        self.assertNotIn("committee", [c.args[0] for c in self.local.call.call_args_list])
        self.assertNotIn("start", [c.args[0] for c in self.remote.call.call_args_list])
        self.drive.assert_not_called()
        self.assertEqual(self.local.call.call_args.args, ("stop",))
        self.assertEqual(self.remote.call.call_args.args, ("stop",))

    def test_genesis_difficulty_override_reaches_both_prepares(self):
        self.args.genesis_difficulty = "0x100000"
        tmh.command_start(self.args)
        for host in (self.local, self.remote):
            prepare = next(c for c in host.call.call_args_list if c.args[0] == "prepare")
            self.assertEqual(prepare.kwargs["genesis"]["difficulty"], "0x100000")

    def test_default_difficulty_keeps_genesis_value(self):
        tmh.command_start(self.args)
        for host in (self.local, self.remote):
            prepare = next(c for c in host.call.call_args_list if c.args[0] == "prepare")
            self.assertNotIn("difficulty", prepare.kwargs["genesis"])

    def test_invalid_difficulty_rejected_before_prepare(self):
        self.args.genesis_difficulty = "0xzz"
        with self.assertRaisesRegex(ValueError, "genesis-difficulty"):
            tmh.command_start(self.args)
        self.assertNotIn("prepare", [c.args[0] for c in self.local.call.call_args_list])

    def test_on_tx_load_runs_during_flow_and_is_stopped_after(self):
        self.args.on_tx_rate = 20.0
        self.args.tx_per_block = 200.0
        started = []
        stopped = []
        fake = Mock()
        with patch.object(tmh, "start_on_tx_load",
                          side_effect=lambda *a, **k: started.append(a) or fake), \
                patch.object(tmh, "stop_owned_process",
                             side_effect=lambda p: stopped.append(p)):
            tmh.command_start(self.args)
        self.assertEqual(len(started), 1)
        self.assertEqual(stopped, [fake])
        # The load account is funded and handed to the host that runs node1.
        prepare = next(c for c in self.local.call.call_args_list if c.args[0] == "prepare")
        load_accounts = prepare.kwargs["on_load_accounts"]
        self.assertEqual(len(load_accounts), 1)
        self.assertIn(load_accounts[0]["address"].removeprefix("0x").lower(),
                      prepare.kwargs["genesis"]["alloc"])
        # node1's start must receive the same accounts so they can be unlocked.
        start = next(c for c in self.local.call.call_args_list if c.args[0] == "start")
        self.assertEqual(start.kwargs["on_load_accounts"], load_accounts)

    def test_no_load_account_or_generator_when_rate_is_zero(self):
        started = []
        with patch.object(tmh, "start_on_tx_load",
                          side_effect=lambda *a, **k: started.append(a) or Mock()), \
                patch.object(tmh, "stop_owned_process", side_effect=lambda p: None):
            tmh.command_start(self.args)
        self.assertEqual(started, [])
        prepare = next(c for c in self.local.call.call_args_list if c.args[0] == "prepare")
        self.assertEqual(prepare.kwargs["on_load_accounts"], [])

    def test_tx_load_command_includes_block_interval_hint(self):
        args = SimpleNamespace(local_root=self.root, on_tx_rate=20.0, tx_per_block=200.0,
                               est_seconds_per_block=10.0,
                               runtime={"est_seconds_per_block": 4.0})
        (self.work / "logs").mkdir(exist_ok=True)
        (self.work / "node-work" / "on-load").mkdir(parents=True, exist_ok=True)
        captured = {}

        def fake_popen(cmd, **kwargs):
            captured["cmd"] = cmd
            return Mock(pid=1234)

        with patch.object(tmh.subprocess, "Popen", side_effect=fake_popen):
            tmh.start_on_tx_load(args, self.run_id, self.work, [1, 2, 3], {2: "0xa", 3: "0xb"},
                                 [{"address": "0xload", "privateKey": "00" * 32}])
        cmd = captured["cmd"]
        self.assertIn("--tx-per-block", cmd)
        self.assertEqual(cmd[cmd.index("--tx-per-block") + 1], "200.0")
        self.assertIn("--block-interval-hint", cmd)
        self.assertEqual(cmd[cmd.index("--block-interval-hint") + 1], "10.0")

    def test_cleanup_only_failure_fails_run_and_is_written_to_status(self):
        self.remote.call.side_effect = lambda action, **_fields: (_ for _ in ()).throw(RuntimeError("remote stop failed")) if action == "stop" else {}
        with self.assertRaisesRegex(RuntimeError, "cleanup incomplete.*remote stop failed"):
            tmh.command_start(self.args)
        self.assertEqual(self.local.call.call_args.args, ("stop",))
        self.assertIsNone(self.status()["error"])
        self.assertFalse(self.status()["success"])
        self.assertEqual(len(self.status()["cleanup_errors"]), 1)

    def test_failed_flow_collection_error_does_not_replace_flow_failure(self):
        self.args.no_collect = False
        self.drive.return_value = 7
        self.collect.side_effect = OSError("collection unavailable")
        with self.assertRaisesRegex(RuntimeError, "L5 flow returned 7"):
            tmh.command_start(self.args)
        self.collect.assert_called_once()
        self.assertIn("L5 flow returned 7", self.status()["error"])
        self.assertEqual(self.local.call.call_args.args, ("stop",))
        self.assertEqual(self.remote.call.call_args.args, ("stop",))

    def test_cancellation_masks_repeated_signals_during_cleanup_and_restores_handlers(self):
        saved = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
        cleanup_masks = []

        def remote_call(action, **_fields):
            if action == "prepare":
                handler = signal.getsignal(signal.SIGTERM)
                self.assertTrue(callable(handler))
                handler(signal.SIGTERM, None)
            if action == "stop":
                handlers = tuple(signal.getsignal(sig) for sig in saved)
                cleanup_masks.append(handlers)
                # Repeated cancellation must be inert, but Python handlers are
                # preferable to SIG_IGN so child workers do not inherit it.
                for sig, handler in zip(saved, handlers):
                    self.assertTrue(callable(handler))
                    self.assertIsNone(handler(sig, None))
            return {}

        self.remote.call.side_effect = remote_call
        with self.assertRaises(tmh.RunCancelled) as raised:
            tmh.command_start(self.args)
        self.assertEqual(raised.exception.signum, signal.SIGTERM)
        self.assertEqual(len(cleanup_masks), 1)
        self.assertEqual({sig: signal.getsignal(sig) for sig in saved}, saved)
        self.assertIn("RunCancelled", self.status()["error"])
        self.assertEqual(self.local.call.call_args.args, ("stop",))
        self.assertEqual(self.remote.call.call_args.args, ("stop",))


class CancellationHandlerTests(unittest.TestCase):
    def test_signal_handlers_restore_after_normal_and_cancelled_exit(self):
        for cancelled in (False, True):
            saved = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
            with self.subTest(cancelled=cancelled):
                try:
                    with tmh.cancellation_handlers():
                        for sig in saved:
                            self.assertTrue(callable(signal.getsignal(sig)))
                        if cancelled:
                            signal.getsignal(signal.SIGINT)(signal.SIGINT, None)
                except tmh.RunCancelled as exc:
                    self.assertTrue(cancelled)
                    self.assertEqual(exc.signum, signal.SIGINT)
                self.assertEqual({sig: signal.getsignal(sig) for sig in saved}, saved)


class EmbeddedDriverTests(RunWorkspaceTests):
    def test_l5_owns_no_lifecycle_and_polls_dkrs_via_remote_grep(self):
        args = SimpleNamespace(local_root=self.root, tee_count=2, active_limit=2,
                               active_threshold=1, light_nodes=0, committee_count=2,
                               committee_threshold=1, rotations=1, baseline_edits=1,
                               edits_per_epoch=1, sgx_mode="SIM", quote_mode="mock",
                               attestation="noop",
                               blocks_per_registered_node=0,
                               genesis_difficulty="0",
                               est_seconds_per_block=0.0, on_tx_rate=0.0, tx_per_block=0.0,
                               runtime={"epoch_length": 240,
                               "steady_epoch_length": 900, "maxpeers": 6,
                               "est_seconds_per_block": 4.0, "distkg_relay_ttl": 6,
                               "dkrs_relay_ttl": 8, "txmodify_relay_ttl": 10})
        module = SimpleNamespace(TOTAL_NODE_COUNT=3)
        observed = []
        lifecycle = []
        original_lifecycle = Mock(side_effect=lambda: lifecycle.append("unexpected lifecycle"))
        for name in ("prepare", "start_nodes", "cleanup", "apply_netem", "start_background_load",
                     "stop_background_load", "start_committee_nodes", "launch_committee_background",
                     "join_committee_background"):
            setattr(module, name, original_lifecycle)
        # sgx_l5's original scrapes the aggregated LOGS dir; the driver replaces
        # it, so a bare stub is enough to detect replacement.
        module.dkrs_extract_progress = Mock(side_effect=AssertionError("local scrape used"))
        l5work = self.work / "l5"
        l5work.mkdir()
        (l5work / "e2e-progress.log").write_text("stale run progress")

        def main(*, manage_lifecycle):
            self.assertFalse(manage_lifecycle)
            self.assertEqual(os.environ["EPOCH_LENGTH"], "240")
            self.assertEqual(os.environ["TEE_STEADY_EPOCH_LENGTH"], "900")
            self.assertEqual(os.environ["STEADY_EPOCH_LENGTH"], "900")
            self.assertEqual(os.environ["TEE_DISTKG_RELAY_TTL"], "6")
            self.assertEqual(os.environ["TEE_DKRS_RELAY_TTL"], "8")
            self.assertEqual(os.environ["TXMODIFY_RELAY_TTL"], "10")
            # PoW knobs default to "off" and the block-time estimate falls back
            # to the runtime value when the flag is 0.
            self.assertEqual(os.environ["ON_TX_PERS"], "0.0")
            self.assertEqual(os.environ["ON_TX_PER_BLOCK"], "0.0")
            self.assertEqual(os.environ["EST_SECONDS_PER_BLOCK"], "4.0")
                # The library re-derives MIN_EPOCH_LENGTH at import, so the driver
            # must hand it the budget resolve_runtime used; unset means the
            # flag forces nothing, exactly as in resolve_runtime.
            self.assertNotIn("TEE_BLOCKS_PER_REGISTERED_NODE", os.environ)
            self.assertEqual((module.WORK / "e2e-progress.log").read_text(), "")
            self.assertEqual(module.node_rpc(2), "http://127.0.0.1:28552")
            self.assertEqual(module.contract_address, "0xcontract")
            self.assertEqual(module.accounts[1]["address"], "0xtee")
            for name in ("prepare", "start_nodes", "cleanup", "start_committee_nodes"):
                getattr(module, name)()
            observed.append(module.dkrs_extract_progress(2))
            observed.append(module.dkrs_extract_progress(3))
            return 0

        module.main = Mock(side_effect=main)
        polls = []

        def fake_call(self, action, **fields):
            assert action == "dkrs_progress", action
            polls.append((self.ssh_host, action, fields["run_id"], fields["epoch"]))
            # local host contributes one started node, remote one started+extracted.
            return {"started": 1, "extracted": 0 if self.ssh_host is None else 1}

        before = dict(os.environ)
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=module), \
                patch.object(tmh.Host, "call", fake_call):
            result = tmh.drive_l5(args, self.run_id, self.work, self.root / "out", [2, 3],
                                  lambda index: tmh.fwd_rpc_port(index), {2: "0xtee"},
                                  "0xcontract", tmh.Host(self.root),
                                  tmh.Host(self.root, "remote"))
        self.assertEqual(result, 0)
        module.main.assert_called_once_with(manage_lifecycle=False)
        original_lifecycle.assert_not_called()
        self.assertEqual(lifecycle, [])
        # Each poll hits both hosts with the epoch and run-id; the driver sums
        # started across hosts and intersects nothing (started&extracted) because
        # the counts are per-host aggregates.
        self.assertEqual(observed, [(1, 2), (1, 2)])
        self.assertEqual([p[3] for p in polls], [2, 2, 3, 3])
        self.assertTrue(all(p[1] == "dkrs_progress" and p[2] == self.run_id for p in polls))
        self.assertEqual(dict(os.environ), before)

    def test_driver_forwards_explicit_pow_knobs_and_block_estimate(self):
        args = SimpleNamespace(local_root=self.root, tee_count=2, active_limit=2,
                               active_threshold=1, light_nodes=0, committee_count=2,
                               committee_threshold=1, rotations=1, baseline_edits=1,
                               edits_per_epoch=1, sgx_mode="SIM", quote_mode="mock",
                               attestation="noop",
                               blocks_per_registered_node=0,
                               genesis_difficulty="0x100000",
                               est_seconds_per_block=10.0, on_tx_rate=20.0, tx_per_block=200.0,
                               runtime={"epoch_length": 240, "steady_epoch_length": 900,
                                        "maxpeers": 6, "est_seconds_per_block": 4.0,
                                        "distkg_relay_ttl": 4, "dkrs_relay_ttl": 4, "txmodify_relay_ttl": 4})
        module = SimpleNamespace(TOTAL_NODE_COUNT=3)
        seen = {}

        def main(*, manage_lifecycle):
            seen.update({k: os.environ[k] for k in
                         ("ON_TX_PERS", "ON_TX_PER_BLOCK", "EST_SECONDS_PER_BLOCK")})
            return 0

        module.main = Mock(side_effect=main)
        module.dkrs_extract_progress = lambda epoch: (epoch, "")
        l5work = self.work / "l5"
        l5work.mkdir()
        (l5work / "e2e-progress.log").write_text("")
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=module), \
                patch.object(tmh, "aggregate_logs", side_effect=lambda *a: None):
            tmh.drive_l5(args, self.run_id, self.work, self.root / "out", [2, 3],
                         lambda index: tmh.fwd_rpc_port(index), {2: "0xtee"},
                         "0xcontract", "local", "remote")
        self.assertEqual(seen, {"ON_TX_PERS": "20.0", "ON_TX_PER_BLOCK": "200.0",
                                "EST_SECONDS_PER_BLOCK": "10.0"})

    def test_driver_forwards_the_registration_block_budget(self):
        args = SimpleNamespace(local_root=self.root, tee_count=16, active_limit=16,
                               active_threshold=9, light_nodes=0, committee_count=16,
                               committee_threshold=11, rotations=1, baseline_edits=1,
                               edits_per_epoch=5, sgx_mode="HW", quote_mode="mock",
                               attestation="noop", genesis_difficulty="0",
                               blocks_per_registered_node=3,
                               est_seconds_per_block=10.0, on_tx_rate=20.0, tx_per_block=200.0,
                               runtime={"epoch_length": 150, "steady_epoch_length": 150,
                                        "maxpeers": 50, "est_seconds_per_block": 4.0,
                                        "distkg_relay_ttl": 4, "dkrs_relay_ttl": 4, "txmodify_relay_ttl": 4})
        module = SimpleNamespace(TOTAL_NODE_COUNT=17)
        seen = {}

        def main(*, manage_lifecycle):
            seen["budget"] = os.environ.get("TEE_BLOCKS_PER_REGISTERED_NODE")
            seen["epoch"] = os.environ["EPOCH_LENGTH"]
            return 0

        module.main = Mock(side_effect=main)
        module.dkrs_extract_progress = lambda epoch: (epoch, "")
        l5work = self.work / "l5"
        l5work.mkdir()
        (l5work / "e2e-progress.log").write_text("")
        with patch.object(tmh.importlib.util, "spec_from_file_location", return_value=SimpleNamespace(loader=Mock())), \
                patch.object(tmh.importlib.util, "module_from_spec", return_value=module):
            tmh.drive_l5(args, self.run_id, self.work, self.root / "out", [2, 3],
                         lambda index: tmh.fwd_rpc_port(index), {2: "0xtee"},
                         "0xcontract", "local", "remote")
        # The driver must not raise 150 back to the floor: the library's import
        # sees the same budget resolve_runtime used.
        self.assertEqual(seen, {"budget": "3", "epoch": "150"})


class LogTransferTests(RunWorkspaceTests):
    """worker_fetch_logs ships only the lines the DKRS readiness scraper and the
    timing collector read, so a multi-GB per-tx/per-block log is not buffered
    through base64 (the OOM that killed a run) nor archived whole."""

    def write_node_log(self, index, lines):
        logs = self.work / "logs"
        logs.mkdir(exist_ok=True)
        (logs / f"node{index}.log").write_text("".join(line + "\n" for line in lines))

    def test_fetch_logs_keeps_only_scraper_and_collector_lines(self):
        self.write_node_log(2, [
            "INFO [10-10|23:00:00.000] ENE_TIMING marker_schema=3 stage=block_generation event=miner_app",
            "DEBUG[10-10|23:00:00.001] Served eth_blockNumber conn=1.2.3.4:5 reqid=1 t=1.2µs",
            "INFO [10-10|23:29:38.268] TEE host starting DKRS oldEpoch=0 newEpoch=1 session=ab dealerIndex=1",
            "INFO [10-10|23:29:38.990] TEE host extracted DKRS receiverIndex=4 verificationKeyHash=4552be3536ab",
            "INFO [10-10|23:30:00.000] E2E_TIMING stage=block_generation event=miner_task_queued_for_sealing duration_ns=835238 block=1 txs=1 status=ok",
            "INFO [10-10|23:30:01.000] E2E_TIMING stage=ledger_update event=lu_getblock node_id=18551 duration_ns=489827",
            "INFO [10-10|23:30:02.000] E2E_TIMING stage=transaction_validation event=txpool_add_txs txs=200",
        ])
        result = tmh.worker_fetch_logs(self.config())
        import base64
        blob = base64.b64decode(result["logs"]["geth-node-2.log"]).decode()
        self.assertIn("TEE host starting DKRS", blob)
        self.assertIn("TEE host extracted DKRS", blob)
        self.assertIn("miner_task_queued_for_sealing", blob)
        self.assertIn("lu_getblock", blob)
        # Noise the collector discards must not be transferred/archived.
        self.assertNotIn("miner_app", blob)
        self.assertNotIn("eth_blockNumber", blob)
        self.assertNotIn("txpool_add_txs", blob)

    def test_fetch_logs_indexes_every_node_and_streams_without_full_buffer(self):
        self.write_node_log(1, ["INFO x TEE host extracted DKRS receiverIndex=1"])
        self.write_node_log(5, ["INFO x E2E_TIMING stage=rotation_flow event=dkrs_start epoch=1"])
        result = tmh.worker_fetch_logs(self.config())
        self.assertEqual(set(result["logs"]), {"geth-node-1.log", "geth-node-5.log"})

    def test_aggregate_logs_merges_both_hosts_and_propagates_fetch_errors(self):
        class FakeHost:
            def __init__(self, payload):
                self.payload = payload

            def call(self, action, **fields):
                assert action == "fetch_logs", action
                if isinstance(self.payload, Exception):
                    raise self.payload
                return {"logs": self.payload}

        import base64
        dest = self.root / "aggregated-logs"
        local = FakeHost({"geth-node-1.log": base64.b64encode(b"local").decode()})
        remote = FakeHost({"geth-node-2.log": base64.b64encode(b"remote").decode()})
        tmh.aggregate_logs(local, remote, self.run_id, dest)
        self.assertEqual((dest / "geth-node-1.log").read_bytes(), b"local")
        self.assertEqual((dest / "geth-node-2.log").read_bytes(), b"remote")
        # A remote failure must surface, not silently leave a stale/partial view
        # (which previously masked a full DKRS success as extracted=0).
        with self.assertRaises(RuntimeError):
            tmh.aggregate_logs(local, FakeHost(RuntimeError("ssh down")), self.run_id, dest)

    def test_worker_dkrs_progress_counts_start_and_extract_for_the_epoch(self):
        # node2: this epoch's start + extract. node3: an extract with no matching
        # start (an earlier epoch) must not count. node4: started, not yet done.
        self.write_node_log(2, ["INFO x TEE host starting DKRS oldEpoch=0 newEpoch=1 session=a",
                                "INFO x TEE host extracted DKRS receiverIndex=4"])
        self.write_node_log(3, ["INFO x TEE host extracted DKRS receiverIndex=2"])
        self.write_node_log(4, ["INFO x TEE host starting DKRS oldEpoch=0 newEpoch=1 session=b"])
        result = tmh.worker_dkrs_progress({"root": str(self.root), "run_id": self.run_id, "epoch": 1})
        self.assertEqual(result, {"started": 2, "extracted": 1})

    def test_worker_dkrs_progress_is_epoch_scoped(self):
        self.write_node_log(2, ["INFO x TEE host starting DKRS oldEpoch=1 newEpoch=2 session=a",
                                "INFO x TEE host extracted DKRS receiverIndex=4"])
        self.assertEqual(
            tmh.worker_dkrs_progress({"root": str(self.root), "run_id": self.run_id, "epoch": 1}),
            {"started": 0, "extracted": 0})
        self.assertEqual(
            tmh.worker_dkrs_progress({"root": str(self.root), "run_id": self.run_id, "epoch": 2}),
            {"started": 1, "extracted": 1})

    def test_log_transfer_filter_matches_collector_markers(self):
        # The transferred subset is a superset of what collect-l5-timing keeps,
        # so the collector must produce identical samples from it. Guard against
        # the collector's marker list drifting away from the transfer filter.
        collector = SCRIPT.with_name("collect-l5-timing.py")
        spec = importlib.util.spec_from_file_location("collect_l5_timing", collector)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertEqual(tuple(tmh.LOG_TRANSFER_RELEVANT), tuple(module.RELEVANT_GETH_MARKERS))
        self.assertIn("TEE host starting DKRS", tmh.LOG_TRANSFER_MARKERS)
        self.assertIn("TEE host extracted DKRS", tmh.LOG_TRANSFER_MARKERS)


if __name__ == "__main__":
    unittest.main()
