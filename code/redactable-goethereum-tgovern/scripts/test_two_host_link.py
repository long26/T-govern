import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("two_host_link.py")
spec = importlib.util.spec_from_file_location("two_host_link", SCRIPT)
link = importlib.util.module_from_spec(spec)
spec.loader.exec_module(link)


class TwoHostLinkTests(unittest.TestCase):
    def test_run_id_cannot_escape_workspace(self):
        with self.assertRaises(ValueError):
            link.run_dir(Path("/tmp/package"), "../../other")
        self.assertEqual(link.run_dir(Path("/tmp/package"), "trial-1"),
                         Path("/tmp/package/.tmp/two-host/trial-1"))

    def test_enode_replaces_advertised_address_not_node_id(self):
        raw = "enode://" + "ab" * 64 + "@[::]:30303"
        self.assertEqual(link.enode_for_ip(raw, "10.1.2.3", 30312),
                         "enode://" + "ab" * 64 + "@10.1.2.3:30312")
        self.assertEqual(link.enode_for_ip(raw, "fd00::2", 30312),
                         "enode://" + "ab" * 64 + "@[fd00::2]:30312")
        with self.assertRaises(ValueError):
            link.enode_for_ip(raw, "host.example.com", 30312)

    def test_peer_checks_discovery_id_not_enode_key_or_count(self):
        # Geth's admin_peers.id may be a 32-byte discovery ID, not a 64-byte enode key.
        node_id = "ab" * 32
        self.assertFalse(link.peer_matches([{"id": "cd" * 32}], node_id))
        self.assertTrue(link.peer_matches([{"id": "AB" * 32}], node_id))
        self.assertFalse(link.peer_matches(None, node_id))

    def test_remote_ssh_uses_batch_mode_and_stdin_json(self):
        with patch.object(link.subprocess, "run") as run:
            run.return_value.returncode = 0
            run.return_value.stdout = '{"ok":true,"result":"0x2"}'
            host = link.Host(Path("/opt/tgovern"), "tee-host")
            self.assertEqual(host.call("rpc", node=2, method="eth_blockNumber")["result"], "0x2")
            cmd = run.call_args.args[0]
            self.assertIn("BatchMode=yes", cmd)
            self.assertEqual(cmd[0], "ssh")
            self.assertEqual(json.loads(run.call_args.kwargs["input"])["method"], "eth_blockNumber")

    def test_stop_never_kills_pid_from_different_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            work = link.run_dir(root, "trial-1")
            (work / "run").mkdir(parents=True)
            record = work / "run" / "geth-node2.json"
            record.write_text(json.dumps({"pid": 12345, "start": "42"}))
            with patch.object(link, "process_start", return_value="43"), \
                 patch.object(link.os, "kill") as kill:
                with self.assertRaisesRegex(RuntimeError, "reused"):
                    link.worker_stop({"root": directory, "run_id": "trial-1"})
                kill.assert_not_called()
                self.assertTrue(record.exists())

    def test_hw_preflight_rejects_sim_link_before_workspace_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            (root / "bin/geth-sgx").touch()
            (root / "helper.json").touch()
            (root / "sgx").mkdir()
            (root / "sgx/enclave.signed.so").touch()
            with patch.object(link.subprocess, "run") as run:
                run.return_value.stdout = "libsgx_urts_sim.so => /usr/lib/libsgx_urts_sim.so"
                with self.assertRaisesRegex(RuntimeError, "HW requested"):
                    link.worker_prepare({"root": directory, "run_id": "trial-1", "node": 2})
            self.assertFalse(link.run_dir(root, "trial-1").exists())

    def test_start_uses_ordinary_geth_and_hw_tee_only_on_node2(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            work = link.run_dir(root, "trial-1")
            (work / "chain" / "node1").mkdir(parents=True)
            (work / "chain" / "node2").mkdir()
            for node in (1, 2):
                nodework = work / "node-work" / f"node{node}"
                nodework.mkdir(parents=True)
                (nodework / "storage.json").write_text('{"address":""}')
            (work / "genesis.json").write_text("{}")
            with patch.object(link, "spawn", return_value=123) as spawn:
                link.worker_start({"root": directory, "run_id": "trial-1", "node": 1,
                                   "address": "0x" + "ab" * 20})
                normal_args = spawn.call_args.args[2]
                self.assertEqual(normal_args[0], str(root / "bin/geth"))
                self.assertNotIn("--teehost", normal_args)
                self.assertEqual(spawn.call_args.args[4]["SGX_MODE"], "SIM")
                link.worker_start({"root": directory, "run_id": "trial-1", "node": 2,
                                   "address": "0x" + "cd" * 20,
                                   "contract": "0x" + "ef" * 20})
                tee_args = spawn.call_args.args[2]
                self.assertEqual(tee_args[0], str(root / "bin/geth-sgx"))
                self.assertIn("--teehost", tee_args)
                self.assertEqual(spawn.call_args.args[4]["SGX_MODE"], "HW")
                self.assertEqual(json.loads((work / "node-work/node2/storage.json").read_text())["address"],
                                 "0x" + "ef" * 20)

    def test_node2_rejects_missing_contract_before_launch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            work = link.run_dir(root, "trial-1")
            nodework = work / "node-work" / "node2"
            nodework.mkdir(parents=True)
            (work / "genesis.json").write_text("{}")
            (nodework / "storage.json").write_text('{"address":""}')
            with self.assertRaisesRegex(ValueError, "contract"):
                link.worker_start({"root": directory, "run_id": "trial-1", "node": 2})


if __name__ == "__main__":
    unittest.main()
