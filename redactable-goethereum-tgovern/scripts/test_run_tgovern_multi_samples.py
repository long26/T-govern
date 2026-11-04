#!/usr/bin/env python3
"""Offline focused tests; never start the real orchestrator or contact hosts."""

import argparse
import contextlib
import copy
import importlib.util
import io
import json
import signal
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).resolve().with_name("run-tgovern-multi-samples.py")
SPEC = importlib.util.spec_from_file_location("run_tgovern_multi_samples", SCRIPT)
assert SPEC and SPEC.loader
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)


def parameters(**overrides):
    return argparse.Namespace(rotations=1, baseline_edits=1, edits_per_epoch=5,
                              **overrides)


def summary(edits=10, rotations=1):
    # Real collector semantics: submission and receipt samples share a join_key
    # but carry DIFFERENT tx hashes (submit tx vs receipt tx).
    edit_samples = [{"join_key": f"request-{index}", "duration_ms": index + 0.5,
                     "tx_hash": "0x" + f"{index + 1:064x}", "status": "ok"}
                    for index in range(edits)]
    confirmation_samples = [{"join_key": entry["join_key"], "duration_ms": entry["duration_ms"],
                             "tx_hash": "0x" + f"{index + 1000:064x}", "status": "ok"}
                            for index, entry in enumerate(edit_samples)]
    return {
        "samples": {
            "edit_execution": edit_samples,
            "execute_redaction_confirmation": confirmation_samples,
            "rotation_flow": [{"epoch": index + 1, "duration_ms": 2.0,
                               "parties": 4, "start_source": "rotation_trigger_seen_mint",
                               "start_block": index + 100, "status": "ok",
                               "end_source_file": "tee-node-2.log"}
                              for index in range(rotations)],
            "tee_registration_substages": {"tee_material_produced": [
                {"duration_ms": 1.0, "source": "script", "tee_index": 2}]},
        },
        "diagnostics": {name: [] for name in (
            "submission_failures", "receipt_failures", "invalid_submissions",
            "invalid_success_receipts", "edit_integrity_alerts", "ledger_failures")},
    }


def write_artifacts(run_out, run_id, payload=None, status=None):
    (run_out / "timing").mkdir(parents=True)
    (run_out / "timing" / "l5-timing-summary.json").write_text(
        json.dumps(payload if payload is not None else summary()))
    (run_out / "run-status.json").write_text(json.dumps(
        status if status is not None else
        {"run_id": run_id, "success": True, "cleanup_errors": []}))


class SampleValidationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.run_out = Path(self.temp.name) / "out" / "explicit-id"
        self.payload = summary()

    def load(self, payload=None, status=None):
        write_artifacts(self.run_out, "explicit-id", payload or self.payload, status)
        return RUNNER.load_run_samples(self.run_out, run_id="explicit-id",
                                       expected=RUNNER.expected_counts(parameters()), threshold=3)

    def test_exact_original_configuration_and_final_batch_counts(self):
        self.assertEqual(RUNNER.expected_counts(parameters()), {
            "rotation_flow": 1, "edit_execution": 10,
            "execute_redaction_confirmation": 10})
        self.assertEqual(RUNNER.expected_counts(parameters(), 5), {
            "rotation_flow": 5, "edit_execution": 50,
            "execute_redaction_confirmation": 50})
        self.load()

    def test_nested_flattening_preserves_source_without_mutating_inputs(self):
        original = copy.deepcopy(self.payload["samples"])
        merged = {}
        RUNNER.merge_run_samples(merged, 2, self.payload["samples"],
                                 run_id="explicit-id", run_out_dir=self.run_out)
        entry = merged["tee_registration_substages:tee_material_produced"][0]
        self.assertEqual(entry["source"], "script")
        self.assertEqual(entry["tee_index"], 2)
        self.assertEqual(entry["run"], 2)
        self.assertEqual(entry["run_id"], "explicit-id")
        self.assertEqual(Path(entry["summary_path"]).parent.name, "timing")
        self.assertEqual(self.payload["samples"], original)
        self.assertIsNot(entry, original["tee_registration_substages"]["tee_material_produced"][0])

    def test_bad_sample_containers_are_rejected(self):
        for value in (None, [], {"stage": 2}, {"stage": ["not a sample"]},
                      {"stage": {"bad:name": []}}, {"bad:name": []}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                RUNNER.flatten_samples(value)

    def test_missing_negative_nonfinite_boolean_and_string_durations_are_rejected(self):
        for value in (None, -1, float("nan"), float("inf"), True, "1.0"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                RUNNER.flatten_samples({"stage": [{"duration_ms": value}]})

    def test_missing_required_stage_and_incomplete_counts_are_rejected(self):
        self.payload["samples"]["edit_execution"].pop()
        with self.assertRaisesRegex(ValueError, "expected 10 samples, got 9"):
            self.load()

    def test_required_stages_cannot_hide_in_nested_objects(self):
        self.payload["samples"]["rotation_flow"] = {"nested": []}
        with self.assertRaisesRegex(ValueError, "required collector stage"):
            self.load()

    def test_extra_core_samples_are_rejected(self):
        self.payload["samples"]["rotation_flow"].append(
            {"epoch": 2, "duration_ms": 1, "parties": 4,
             "start_source": "rotation_trigger_seen_mint", "start_block": 101, "status": "ok"})
        with self.assertRaisesRegex(ValueError, "expected 1 samples, got 2"):
            self.load()

    def test_different_submission_and_receipt_transactions_are_accepted(self):
        # Same join_key with a different (receipt) tx hash is the normal shape.
        self.payload["samples"]["execute_redaction_confirmation"][0]["tx_hash"] = "0x" + "f" * 64
        self.load()

    def test_receipt_without_matching_submission_is_rejected(self):
        self.payload["samples"]["execute_redaction_confirmation"][0]["join_key"] = "request-999"
        with self.assertRaisesRegex(ValueError, "do not match successful receipts"):
            self.load()

    def test_submission_success_without_successful_receipts_is_rejected(self):
        self.payload["samples"]["execute_redaction_confirmation"] = []
        with self.assertRaisesRegex(ValueError, "execute_redaction_confirmation"):
            self.load()

    def test_duplicate_requests_are_rejected(self):
        self.payload["samples"]["edit_execution"][1]["join_key"] = "request-0"
        with self.assertRaisesRegex(ValueError, "duplicate request"):
            self.load()

    def test_invalid_edit_success_evidence_is_rejected(self):
        for field, value in (("status", "failed"), ("join_key", ""),
                             ("tx_hash", "0x" + "0" * 64), ("tx_hash", "0x123")):
            with self.subTest(field=field, value=value):
                payload = copy.deepcopy(self.payload)
                payload["samples"]["edit_execution"][0][field] = value
                with tempfile.TemporaryDirectory() as directory:
                    run_out = Path(directory)
                    write_artifacts(run_out, "id", payload)
                    with self.assertRaisesRegex(ValueError, "success/join_key/tx_hash evidence"):
                        RUNNER.load_run_samples(run_out, run_id="id",
                                                expected=RUNNER.expected_counts(parameters()), threshold=3)

    def test_rotation_without_a_trigger_source_is_rejected(self):
        # rotation_flow now spans from the epoch-triggering block; a sample with
        # no start_source cannot be attributed to a trigger, so it is rejected.
        self.payload["samples"]["rotation_flow"][0]["start_source"] = ""
        with self.assertRaisesRegex(ValueError, "malformed trigger evidence"):
            self.load()

    def test_rotation_with_a_negative_trigger_block_is_rejected(self):
        self.payload["samples"]["rotation_flow"][0]["start_block"] = -1
        with self.assertRaisesRegex(ValueError, "malformed trigger evidence"):
            self.load()

    def test_duplicate_rotation_epochs_are_rejected(self):
        self.payload = summary(edits=15, rotations=2)
        self.payload["samples"]["rotation_flow"][1]["epoch"] = 1
        write_artifacts(self.run_out, "explicit-id", self.payload)
        with self.assertRaisesRegex(ValueError, "duplicate epoch"):
            RUNNER.load_run_samples(self.run_out, run_id="explicit-id",
                                    expected={"rotation_flow": 2, "edit_execution": 15,
                                              "execute_redaction_confirmation": 15}, threshold=3)

    def test_failure_diagnostics_are_rejected(self):
        self.payload["diagnostics"]["edit_integrity_alerts"] = [{"reason": "missing receipt"}]
        with self.assertRaisesRegex(ValueError, "edit_integrity_alerts"):
            self.load()

    def test_retried_ledger_and_receipt_failures_are_not_integrity_failures(self):
        # A tx_modify_failed / failed receipt that the flow retried to success is
        # diagnostic noise, not an integrity failure; the samples stay complete.
        self.payload["diagnostics"]["ledger_failures"] = [
            {"event": "tx_modify_failed", "reason": "execution_not_visible"}]
        self.payload["diagnostics"]["receipt_failures"] = [{"reason": "status_0"}]
        self.load()

    def test_per_tee_registration_and_system_init_reach_the_aggregate(self):
        # tee_registration is one sample per TEE (per-TEE latency), system_init
        # is the orchestrator's surviving init segments, and the 16-TEE
        # wall-clock span rides alongside as its own secondary stage.
        self.payload["samples"]["system_init"] = [
            {"sample_id": "system_init", "duration_ms": 35036.0}]
        self.payload["samples"]["tee_registration"] = [
            {"sample_id": f"tee_registered:node{index}", "node": f"node{index}",
             "duration_ms": 40000.0 + index, "status": "ok"} for index in range(2, 18)]
        self.payload["samples"]["tee_registration_cluster"] = [
            {"sample_id": "tee_registration_cluster", "duration_ms": 723040.0,
             "registered_count": 16}]
        samples = self.load()
        merged = {}
        RUNNER.merge_run_samples(merged, 1, samples, run_id="explicit-id",
                                 run_out_dir=self.run_out)
        # Sixteen TEEs -> sixteen per-TEE samples, not one 16-TEE span.
        self.assertEqual(len(merged["tee_registration"]), 16)
        self.assertEqual({entry["duration_ms"] for entry in merged["tee_registration"]},
                         {40000.0 + index for index in range(2, 18)})
        self.assertEqual(merged["tee_registration_cluster"][0]["duration_ms"], 723040.0)
        self.assertEqual(merged["system_init"][0]["duration_ms"], 35036.0)

    def test_run_status_must_prove_this_run_and_clean_success(self):
        for status in ({"run_id": "other", "success": True, "cleanup_errors": []},
                       {"run_id": "id", "success": False, "cleanup_errors": []},
                       {"run_id": "id", "success": 1, "cleanup_errors": []},
                       {"run_id": "id", "success": True, "cleanup_errors": ["stop failed"]},
                       {"run_id": "id", "success": True}, []):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                (path / "run-status.json").write_text(json.dumps(status))
                with self.assertRaises(ValueError):
                    RUNNER.validate_success_status(path, "id")

    def test_missing_run_status_is_not_success(self):
        with self.assertRaises(OSError):
            RUNNER.validate_success_status(self.run_out, "explicit-id")

    def test_actual_collector_fields_are_accepted_not_invented_req_id_fields(self):
        collector_spec = importlib.util.spec_from_file_location(
            "collector_schema_evidence", SCRIPT.with_name("collect-l5-timing.py"))
        collector = importlib.util.module_from_spec(collector_spec)
        collector_spec.loader.exec_module(collector)
        tx_hash = "0x" + "1" * 64
        geth = [{"stage": "edit_execution", "event": event, "join_key": "0xabc",
                 "timestamp_s": timestamp, "tx_hash": tx_hash, "status": 1}
                for event, timestamp in (("material_load_start", 1),
                                         ("tee_authorized_received", 2),
                                         ("redaction_broadcast_modify_msg", 3),
                                         ("execute_redaction_tx_submitted", 3),
                                         ("execute_redaction_receipt_success", 4))]
        # rotation now spans node1 mint(trigger_block) -> rotation_dkg_complete,
        # both read from the geth (E2E_TIMING) stream.
        geth += [
            {"stage": "block_generation", "event": "miner_task_queued_for_sealing",
             "block": 9, "timestamp_s": 2, "source_file": "geth-node-1.log"},
            {"stage": "rotation_flow", "event": "rotation_trigger_seen", "epoch": 1,
             "trigger_block": 9, "timestamp_s": 2.5, "source_file": "geth-node-1.log"},
            {"stage": "rotation_flow", "event": "rotation_dkg_complete", "epoch": 1,
             "receiverIndex": 4, "parties": 4, "timestamp_s": 5, "source_file": "geth-node-5.log"},
        ]
        progress = [{"event": "measurement_begin", "timestamp_s": 1},
                    {"event": "measurement_end", "timestamp_s": 10},
                    {"stage": "edit_execution", "event": "committee_redact_request_submitted",
                     "join_key": "0xabc", "timestamp_s": 1.5}]
        tees = [{"event": "dkrs_start", "epoch": 1, "timestamp_s": 2,
                 "source_file": "tee-2.log"}]
        samples, diagnostics, _ = collector.compute_timing(geth, progress, tees, threshold=3)
        write_artifacts(self.run_out, "explicit-id", {"samples": samples, "diagnostics": diagnostics})
        actual = RUNNER.load_run_samples(self.run_out, run_id="explicit-id",
                                        expected={"rotation_flow": 1, "edit_execution": 1,
                                                  "execute_redaction_confirmation": 1}, threshold=3)
        self.assertEqual(actual["edit_execution"][0]["join_key"], "abc")
        self.assertNotIn("req_id", actual["edit_execution"][0])


class ManagedProcessesTest(unittest.TestCase):
    def test_popen_is_managed_and_reaped_on_success(self):
        process = mock.Mock()
        process.wait.return_value = 0
        with mock.patch.object(RUNNER.subprocess, "Popen", return_value=process) as popen:
            self.assertEqual(RUNNER.run_orchestrator(["fake", "start"], mock.sentinel.output), 0)
        self.assertTrue(popen.call_args.kwargs["start_new_session"])
        self.assertEqual(popen.call_args.kwargs["stdin"], subprocess.DEVNULL)
        process.wait.assert_called_once_with(timeout=None)
        process.send_signal.assert_not_called()

    def test_sigint_and_sigterm_are_forwarded_and_child_reaped(self):
        for signum in (signal.SIGINT, signal.SIGTERM):
            with self.subTest(signum=signum):
                process = mock.Mock()
                process.poll.return_value = None
                process.wait.side_effect = [RUNNER.RunCancelled(signum), 0]
                with mock.patch.object(RUNNER.subprocess, "Popen", return_value=process):
                    with self.assertRaises(RUNNER.RunCancelled):
                        RUNNER.run_orchestrator(["fake", "start"], mock.sentinel.output)
                process.send_signal.assert_called_once_with(signum)
                self.assertEqual(process.wait.call_count, 2)
                process.wait.assert_called_with(timeout=RUNNER.CANCEL_GRACE_SECONDS)
                process.kill.assert_not_called()

    def test_dummy_child_receives_cancellation_signal_and_is_reaped(self):
        # A local inert child only: no project flow, hosts, binaries or network.
        real_popen = subprocess.Popen
        child_code = (
            "import signal,sys,time\n"
            "def stop(signum, frame):\n"
            " print('signal=' + str(signum), flush=True)\n"
            " raise SystemExit(0)\n"
            "signal.signal(signal.SIGINT, stop)\n"
            "signal.signal(signal.SIGTERM, stop)\n"
            "print('ready', flush=True)\n"
            "time.sleep(30)\n"
        )
        for signum in (signal.SIGINT, signal.SIGTERM):
            with self.subTest(signum=signum):
                child = real_popen([sys.executable, "-I", "-c", child_code],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
                try:
                    self.assertEqual(child.stdout.readline().strip(), "ready")
                    real_wait = child.wait
                    calls = 0

                    def interrupted_wait(timeout=None):
                        nonlocal calls
                        calls += 1
                        if calls == 1:
                            raise RUNNER.RunCancelled(signum)
                        return real_wait(timeout=timeout)

                    with mock.patch.object(RUNNER.subprocess, "Popen", return_value=child), \
                            mock.patch.object(child, "wait", side_effect=interrupted_wait):
                        with self.assertRaises(RUNNER.RunCancelled):
                            RUNNER.run_orchestrator(["inert-child"], mock.sentinel.output)
                    self.assertEqual(child.returncode, 0)
                    self.assertEqual(child.stdout.read().strip(), f"signal={signum}")
                    self.assertEqual(calls, 2)
                finally:
                    if child.poll() is None:
                        child.kill()
                    child.wait()
                    child.stdout.close()
                    child.stderr.close()

    def test_timeout_escalation_kills_only_owned_child_then_reaps(self):
        process = mock.Mock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired("fake", 1),
                                    subprocess.TimeoutExpired("fake", 300), -9]
        with mock.patch.object(RUNNER.subprocess, "Popen", return_value=process):
            with self.assertRaises(subprocess.TimeoutExpired):
                RUNNER.run_orchestrator(["fake", "stop"], mock.sentinel.output, timeout=1)
        process.send_signal.assert_called_once_with(signal.SIGTERM)
        process.kill.assert_called_once_with()
        process.wait.assert_called_with()

    def test_baseexception_from_wait_still_signals_and_reaps(self):
        process = mock.Mock()
        process.poll.return_value = None
        process.wait.side_effect = [SystemExit(7), 0]
        with mock.patch.object(RUNNER.subprocess, "Popen", return_value=process):
            with self.assertRaises(SystemExit):
                RUNNER.run_orchestrator(["fake"], mock.sentinel.output)
        process.send_signal.assert_called_once_with(signal.SIGTERM)
        self.assertEqual(process.wait.call_count, 2)

    def test_signal_handlers_raise_cancellation_and_restore_handlers(self):
        before = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
        with RUNNER.cancellation_handlers():
            for sig in before:
                with self.assertRaises(RUNNER.RunCancelled) as caught:
                    signal.getsignal(sig)(sig, None)
                self.assertEqual(caught.exception.signum, sig)
        self.assertEqual(before, {sig: signal.getsignal(sig) for sig in before})

    def test_cleanup_deferral_does_not_leak_ignored_signals_into_stop_children(self):
        code = ("import signal; "
                "assert signal.getsignal(signal.SIGINT) != signal.SIG_IGN; "
                "assert signal.getsignal(signal.SIGTERM) != signal.SIG_IGN")
        before = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
        with RUNNER.defer_cancellation():
            for sig in before:
                signal.getsignal(sig)(sig, None)  # repeated cancellation must not abort cleanup
            subprocess.run([sys.executable, "-I", "-c", code], check=True, timeout=5)
        self.assertEqual(before, {sig: signal.getsignal(sig) for sig in before})

    def test_nested_cleanup_deferral_retains_first_controller_signal(self):
        pending = []
        with RUNNER.cancellation_handlers(pending), RUNNER.defer_cancellation(), RUNNER.defer_cancellation():
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
            signal.getsignal(signal.SIGINT)(signal.SIGINT, None)
        self.assertEqual(pending, [signal.SIGTERM])

    def test_stop_is_run_scoped_and_success_exit_required(self):
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(remote_ssh="fake-host", local_root=Path(directory),
                                      remote_root=Path("/remote"))
            for returncode in (0, 2):
                with self.subTest(returncode=returncode), mock.patch.object(
                        RUNNER, "run_orchestrator", return_value=returncode) as run:
                    log = Path(directory) / f"stop-{returncode}.log"
                    if returncode:
                        with self.assertRaisesRegex(RuntimeError, "run-scoped stop failed"):
                            RUNNER.stop_run(args, "owned-id", log)
                    else:
                        RUNNER.stop_run(args, "owned-id", log)
                    command = run.call_args.args[0]
                    self.assertEqual(command[2], "stop")
                    self.assertEqual(command[command.index("--run-id") + 1], "owned-id")
                    self.assertEqual(run.call_args.kwargs["timeout"], RUNNER.STOP_TIMEOUT_SECONDS)
                    self.assertNotIn("pkill", command)


class BatchTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.batch = self.root / "results" / "test-batch"
        self.argv = [str(SCRIPT), "--runs", "5", "--output-root", str(self.root / "results"),
                     "--tag", "test-batch", "--local-root", str(self.root),
                     "--remote-ssh", "never-contact", "--local-ip", "192.0.2.1",
                     "--remote-ip", "192.0.2.2", "--rotations", "1",
                     "--baseline-edits", "1", "--edits-per-epoch", "5"]

    def invoke(self, runner, extra=(), stop=None):
        with mock.patch.object(sys, "argv", self.argv + list(extra)), \
                mock.patch.object(RUNNER, "run_orchestrator", side_effect=runner) as run, \
                mock.patch.object(RUNNER, "stop_run", side_effect=stop) as cleanup, \
                contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            result = RUNNER.main()
        return result, run, cleanup

    def successful(self, command, _output):
        run_id = command[command.index("--run-id") + 1]
        write_artifacts(self.root / "out" / run_id, run_id)
        return 0

    def test_complete_batch_exact_ids_provenance_and_fifty_edits(self):
        def successful_with_unrelated_output(command, output):
            self.successful(command, output)
            unrelated = self.root / "out" / "newest-unrelated-directory"
            unrelated.mkdir(exist_ok=True)
            (unrelated / "wrong.json").write_text("{}")
            return 0
        result, run, cleanup = self.invoke(successful_with_unrelated_output)
        aggregate = json.loads((self.batch / "aggregate.json").read_text())
        manifest = json.loads((self.batch / "manifest.json").read_text())
        self.assertEqual(result, 0)
        self.assertEqual(run.call_count, 5)
        cleanup.assert_not_called()
        self.assertTrue(aggregate["complete"])
        self.assertEqual(aggregate["sample_counts"]["rotation_flow"], 5)
        self.assertEqual(aggregate["sample_counts"]["edit_execution"], 50)
        self.assertEqual(len(aggregate["successful_runs"]), 5)
        self.assertEqual(len({run["run_id"] for run in manifest["planned_runs"]}), 5)
        self.assertEqual(len(aggregate["samples"]["tee_registration_substages:tee_material_produced"]), 5)
        self.assertTrue(all(entry["source"] == "script" for entry in aggregate["samples"][
            "tee_registration_substages:tee_material_produced"]))
        for index, provenance in enumerate(aggregate["successful_runs"], 1):
            self.assertTrue(provenance["success"])
            self.assertEqual(provenance["run"], index)
            self.assertEqual(provenance["run_id"], manifest["planned_runs"][index - 1]["run_id"])
            self.assertTrue((self.batch / "runs" / f"run-{index:03d}" / "e2e.log").is_file())
        self.assertEqual(json.loads((self.batch / "failures.json").read_text()), [])

    def test_slow_chain_epoch_defaults_reach_the_orchestrator(self):
        # ~10s-block budgets must be the defaults; the old 1-2s-chain values
        # (epoch 200, no registration-budget cap, 4s estimate) would leave the
        # orchestrator oversized waits and the library's estimate mismatched.
        result, run, _ = self.invoke(self.successful)
        self.assertEqual(result, 0)
        command = run.call_args_list[0].args[0]
        self.assertEqual(command[command.index("--epoch-length") + 1], "128")
        self.assertEqual(command[command.index("--blocks-per-registered-node") + 1], "1")
        self.assertEqual(command[command.index("--est-seconds-per-block") + 1], "15.0")

    def test_pow_knobs_are_forwarded_to_the_orchestrator(self):
        result, run, _ = self.invoke(self.successful, extra=[
            "--genesis-difficulty", "0x100000", "--est-seconds-per-block", "10",
            "--on-tx-rate", "20", "--tx-per-block", "200"])
        self.assertEqual(result, 0)
        command = run.call_args_list[0].args[0]
        self.assertEqual(command[command.index("--genesis-difficulty") + 1], "0x100000")
        self.assertEqual(command[command.index("--est-seconds-per-block") + 1], "10.0")
        self.assertEqual(command[command.index("--on-tx-rate") + 1], "20.0")
        self.assertEqual(command[command.index("--tx-per-block") + 1], "200.0")

    def test_topology_parameters_and_manifest_are_forwarded(self):
        result, run, _ = self.invoke(self.successful, extra=[
            "--topology", "full", "--topology-radius", "1", "--topology-max-degree", "3"])
        self.assertEqual(result, 0)
        command = run.call_args_list[0].args[0]
        self.assertEqual(command[command.index("--topology") + 1], "full")
        self.assertEqual(command[command.index("--topology-radius") + 1], "1")
        self.assertEqual(command[command.index("--topology-max-degree") + 1], "3")
        topo = json.loads((self.batch / "manifest.json").read_text())["topology"]
        self.assertEqual(topo["mode"], "full")
        self.assertEqual(topo["edge_count"], 10)

    def test_32_tee_dry_run_records_sparse_graph_without_processes(self):
        output = io.StringIO()
        with mock.patch.object(sys, "argv", self.argv + ["--dry-run", "--tee-count", "32",
                                                       "--active-limit", "32", "--active-threshold", "17"]), \
                mock.patch.object(RUNNER, "run_orchestrator") as run, \
                contextlib.redirect_stdout(output):
            self.assertEqual(RUNNER.main(), 0)
        manifest, _ = json.JSONDecoder().raw_decode(output.getvalue())
        topo = manifest["topology"]
        self.assertEqual(topo["total"], 33)
        self.assertEqual(topo["max_degree"], 8)
        self.assertEqual(topo["relay_diameter"], 3)
        self.assertEqual(topo["relay_ttl_min"], 4)
        for run_plan in manifest["planned_runs"]:
            command = run_plan["command"]
            self.assertEqual(command[command.index("--topology") + 1], "sparse")
        run.assert_not_called()
        self.assertFalse(self.batch.exists())

    def test_invalid_topology_is_rejected_before_writes_or_launch(self):
        for extra in (["--topology-radius", "0"], ["--topology-max-degree", "1"],
                      ["--topology-radius", "2", "--topology-max-degree", "3"],
                      ["--light-nodes", "-1"]):
            with self.subTest(extra=extra), self.assertRaises(SystemExit):
                self.invoke(self.successful, extra=extra)
            self.assertFalse(self.batch.exists())

    def test_tx_per_block_above_cap_is_rejected_before_launch(self):
        with self.assertRaises(SystemExit):
            self.invoke(self.successful, extra=["--tx-per-block", "201"])
        with self.assertRaises(SystemExit):
            self.invoke(self.successful, extra=["--genesis-difficulty", "0xzz"])

    def test_nonzero_runs_stop_safely_continue_and_never_aggregate_stale_success(self):
        def failed(command, output):
            self.successful(command, output)
            return 7
        result, run, cleanup = self.invoke(failed)
        aggregate = json.loads((self.batch / "aggregate.json").read_text())
        self.assertEqual(result, 1)
        self.assertEqual(run.call_count, 5)
        cleanup.assert_not_called()  # valid status already proves cleanup
        self.assertFalse(aggregate["complete"])
        self.assertEqual(aggregate["samples"], {})
        self.assertEqual(aggregate["successful_runs"], [])

    def test_failed_status_requires_verified_stop_even_if_summary_exists(self):
        def failed_status(command, _output):
            run_id = command[command.index("--run-id") + 1]
            write_artifacts(self.root / "out" / run_id, run_id,
                            status={"run_id": run_id, "success": False, "cleanup_errors": []})
            return 0
        result, run, cleanup = self.invoke(failed_status)
        self.assertEqual(result, 1)
        self.assertEqual(cleanup.call_count, 5)
        self.assertEqual(run.call_count, 5)
        for call in cleanup.call_args_list:
            self.assertTrue(call.args[1].startswith("batch-"))
        aggregate = json.loads((self.batch / "aggregate.json").read_text())
        self.assertEqual(aggregate["samples"], {})

    def test_stop_on_failure_is_retained(self):
        result, run, cleanup = self.invoke(lambda *_: 9, extra=["--stop-on-failure"])
        self.assertEqual(result, 1)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(cleanup.call_count, 1)
        self.assertFalse((self.batch / "runs" / "run-002").exists())

    def test_failed_cleanup_aborts_batch_instead_of_starting_another_run(self):
        result, run, cleanup = self.invoke(lambda *_: 9, stop=RuntimeError("stop verification failed"))
        self.assertEqual(result, 1)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(cleanup.call_count, 1)
        attempt = json.loads((self.batch / "runs" / "run-001" / "run.json").read_text())
        self.assertEqual(attempt["cleanup_errors"], ["RuntimeError: stop verification failed"])
        self.assertFalse(attempt["success"])

    def test_missing_collection_is_failed_despite_successful_cleanup(self):
        def no_summary(command, _output):
            run_id = command[command.index("--run-id") + 1]
            run_out = self.root / "out" / run_id
            run_out.mkdir(parents=True)
            (run_out / "run-status.json").write_text(json.dumps(
                {"run_id": run_id, "success": True, "cleanup_errors": []}))
            return 0
        result, run, cleanup = self.invoke(no_summary)
        self.assertEqual(result, 1)
        self.assertEqual(run.call_count, 5)
        cleanup.assert_not_called()
        self.assertFalse(json.loads((self.batch / "aggregate.json").read_text())["complete"])

    def test_incomplete_samples_fail_run_and_final_batch_validation(self):
        def incomplete(command, _output):
            run_id = command[command.index("--run-id") + 1]
            write_artifacts(self.root / "out" / run_id, run_id, summary(edits=9))
            return 0
        result, _, cleanup = self.invoke(incomplete)
        self.assertEqual(result, 1)
        cleanup.assert_not_called()
        failures = json.loads((self.batch / "failures.json").read_text())
        self.assertIn("expected 10 samples, got 9", failures[0]["error"])
        self.assertIn("batch_validation_error", failures[-1])

    def test_final_batch_counts_checked_independently_of_per_run_validation(self):
        with mock.patch.object(RUNNER, "merge_run_samples", return_value=None):
            result, _, _ = self.invoke(self.successful)
        self.assertEqual(result, 1)
        failures = json.loads((self.batch / "failures.json").read_text())
        self.assertIn("expected 5 samples, got 0", failures[-1]["batch_validation_error"])
        self.assertFalse(json.loads((self.batch / "aggregate.json").read_text())["complete"])

    def test_startup_error_is_recorded_and_cleanup_attempted(self):
        result, run, cleanup = self.invoke(OSError("cannot spawn"), extra=["--stop-on-failure"])
        self.assertEqual(result, 1)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(cleanup.call_count, 1)
        attempt = json.loads((self.batch / "runs" / "run-001" / "run.json").read_text())
        self.assertIn("cannot spawn", attempt["error"])
        self.assertTrue(attempt["fallback_stop_verified"])

    def test_sigint_sigterm_and_keyboardinterrupt_finalize_and_cleanup(self):
        for exception, code in ((RUNNER.RunCancelled(signal.SIGINT), 130),
                                (RUNNER.RunCancelled(signal.SIGTERM), 143),
                                (KeyboardInterrupt(), 130)):
            with self.subTest(exception=exception), tempfile.TemporaryDirectory() as directory:
                self.argv[self.argv.index("--tag") + 1] = Path(directory).name
                result, run, cleanup = self.invoke(exception)
                batch = self.root / "results" / Path(directory).name
                self.assertEqual(result, code)
                self.assertEqual(run.call_count, 1)
                self.assertEqual(cleanup.call_count, 1)
                self.assertFalse(json.loads((batch / "aggregate.json").read_text())["complete"])
                attempt = json.loads((batch / "runs" / "run-001" / "run.json").read_text())
                self.assertFalse(attempt["success"])
                self.assertTrue(attempt["fallback_stop_verified"])

    def test_first_sigterm_during_fallback_cleanup_is_honored_after_safe_stop(self):
        def cleanup(*_args):
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
        result, run, stopped = self.invoke(lambda *_: 9, stop=cleanup)
        self.assertEqual(result, 143)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(stopped.call_count, 1)
        self.assertFalse(json.loads((self.batch / "aggregate.json").read_text())["complete"])
        failures = json.loads((self.batch / "failures.json").read_text())
        self.assertTrue(any("cancellation" in failure for failure in failures))

    def test_first_sigterm_during_final_writes_cannot_leave_complete_batch(self):
        real_write = RUNNER.write_aggregate
        writes = 0

        def interrupted_write(*args, **kwargs):
            nonlocal writes
            writes += 1
            if writes == 1:
                signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
            return real_write(*args, **kwargs)

        with mock.patch.object(RUNNER, "write_aggregate", side_effect=interrupted_write):
            result, run, stopped = self.invoke(self.successful)
        self.assertEqual(result, 143)
        self.assertEqual(run.call_count, 5)
        stopped.assert_not_called()
        self.assertFalse(json.loads((self.batch / "aggregate.json").read_text())["complete"])
        self.assertEqual(writes, 2)

    def test_resume_rejected_without_modifying_previous_attempts_or_logs(self):
        self.batch.mkdir(parents=True)
        files = {"manifest.json": "old manifest", "runs/run-001/e2e.log": "previous log",
                 "runs/run-001/run.json": "previous attempt", "aggregate.json": "old aggregate"}
        for name, content in files.items():
            path = self.batch / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        with self.assertRaises(SystemExit) as caught:
            self.invoke(self.successful, extra=["--resume"])
        self.assertEqual(caught.exception.code, 2)
        self.assertEqual({name: (self.batch / name).read_text() for name in files}, files)

    def test_existing_output_is_preserved_without_resume(self):
        self.batch.mkdir(parents=True)
        (self.batch / "manifest.json").write_text("original")
        with self.assertRaises(SystemExit):
            self.invoke(self.successful)
        self.assertEqual((self.batch / "manifest.json").read_text(), "original")

    def test_dry_run_builds_explicit_commands_without_writes_or_processes(self):
        result, run, cleanup = self.invoke(self.successful, extra=["--dry-run"])
        self.assertEqual(result, 0)
        run.assert_not_called()
        cleanup.assert_not_called()
        self.assertFalse(self.batch.exists())

    def test_invalid_counts_reject_before_execution(self):
        for option, value in (("--runs", "0"), ("--rotations", "-1"),
                              ("--baseline-edits", "-1"), ("--edits-per-epoch", "0")):
            with self.subTest(option=option), self.assertRaises(SystemExit):
                self.invoke(self.successful, extra=[option, value])
            self.assertFalse(self.batch.exists())


if __name__ == "__main__":
    unittest.main()
