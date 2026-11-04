#!/usr/bin/env python3
"""Offline tests for collect-l5-timing.py's stage definitions.

Never starts the real flow or contacts hosts: the progress log is synthesised
from the anchor lines the multi-host orchestrator actually emits.
"""

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().with_name("collect-l5-timing.py")
SPEC = importlib.util.spec_from_file_location("collect_l5_timing", SCRIPT)
assert SPEC and SPEC.loader
COLLECTOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COLLECTOR)


def progress_log(*lines):
    """Build a run-scoped e2e-progress.log from raw anchor messages."""
    body = "".join(
        f"[sgx-l5] [10-12|19:12:20.392] [+{index * 1000}ms] {line}\n"
        for index, line in enumerate(lines)
    )
    directory = tempfile.TemporaryDirectory()
    path = Path(directory.name) / "e2e-progress.log"
    path.write_text(body)
    return directory, path


def compute(*lines):
    directory, path = progress_log(*lines)
    records = COLLECTOR.load_progress_records(path, 2026)
    diagnostics = {"missing_anchors": [], "invalid_spans": []}
    system, registration = COLLECTOR.compute_initialization_samples(records, diagnostics)
    return system, registration, diagnostics, directory


def system_missing(diagnostics):
    return [item for item in diagnostics["missing_anchors"] if item["stage"] == "system_init"]


class SystemInitTest(unittest.TestCase):
    def test_optional_orchestrator_segments_do_not_void_system_init(self):
        # The multi-host orchestrator owns chain init, node startup and contract
        # deployment, so sgx_l5 never emits those anchors into the run's progress
        # log. The remaining segments must still produce a system_init sample
        # instead of a blank "stage not present in data" row.
        system, registration, diagnostics, _ = compute(
            "PERF_ANCHOR stage=system_init event=chain_init_complete nodes=17",
            "PERF_ANCHOR stage=system_init event=contract_deploy_begin",
            "PERF_ANCHOR stage=system_init event=committee_dkg_begin members=16",
            "PERF_ANCHOR stage=system_init event=committee_pubkey_registered threshold=11",
            "PERF_ANCHOR stage=tee_registration event=tee_registration_begin candidates=16",
            "PERF_ANCHOR stage=system_init event=tee_pkch_begin block=67",
            "PERF_ANCHOR stage=system_init event=pkch_ready version=0",
        )
        self.assertEqual(system_missing(diagnostics), [])
        self.assertEqual(len(system), 1)
        sample = system[0]
        # The synthetic log advances 1s per line, so each surviving segment is
        # 1s and the total is the sum of those two spans.
        self.assertEqual(sample["duration_ms"], 2000.0)
        self.assertEqual([segment["name"] for segment in sample["segments"]],
                         ["committee_dkg_pubkey_registration", "tee_pkch_generation"])
        self.assertIn("chain_datadir_init", sample["unavailable_segments"])
        self.assertIn("tee_registration", sample["excluded"])

    def test_a_required_segment_missing_still_voids_system_init(self):
        # committee_dkg is required: if its end anchor never arrives the stage is
        # genuinely incomplete and must stay absent with a recorded reason.
        system, _, diagnostics, _ = compute(
            "PERF_ANCHOR stage=system_init event=committee_dkg_begin members=16",
            "PERF_ANCHOR stage=system_init event=tee_pkch_begin block=67",
            "PERF_ANCHOR stage=system_init event=pkch_ready version=0",
        )
        self.assertEqual(system, [])
        self.assertEqual(system_missing(diagnostics),
                         [{"stage": "system_init", "events": ["committee_pubkey_registered"]}])


class TeeRegistrationTest(unittest.TestCase):
    def anchors(self):
        return [
            "PERF_ANCHOR stage=tee_registration event=tee_registration_begin candidates=16",
            "PERF_ANCHOR stage=tee_registration event=tee_registered node=node2 tee_index=1 duration_ns=44121510224",
            "PERF_ANCHOR stage=tee_registration event=tee_registered node=node3 tee_index=2 duration_ns=52133828338",
            "PERF_ANCHOR stage=tee_registration event=active_set_bootstrapped active_count=16 block=67",
        ]

    def test_headline_is_one_sample_per_tee_not_the_cluster_span(self):
        _, registration, _, _ = compute(*self.anchors())
        # Two TEEs -> two samples, each that TEE's own begin-to-confirmed latency;
        # the 3s cluster span is NOT what the headline reports.
        self.assertEqual(len(registration), 2)
        self.assertEqual([sample["duration_ms"] for sample in registration],
                         [round(44121510224 / 1e6, 3), round(52133828338 / 1e6, 3)])
        self.assertEqual([sample["node"] for sample in registration], ["node2", "node3"])
        self.assertTrue(all(sample["end_event"] == "tee_registered" for sample in registration))

    def test_cluster_span_is_reported_separately(self):
        directory, path = progress_log(*self.anchors())
        records = COLLECTOR.load_progress_records(path, 2026)
        span = COLLECTOR.compute_registration_span(records)
        self.assertEqual(len(span), 1)
        self.assertEqual(span[0]["duration_ms"], 3000.0)
        self.assertEqual(span[0]["registered_count"], 2)

    def test_missing_per_tee_durations_are_diagnosed_not_silently_empty(self):
        _, registration, diagnostics, _ = compute(
            "PERF_ANCHOR stage=tee_registration event=tee_registration_begin candidates=16",
            "PERF_ANCHOR stage=tee_registration event=active_set_bootstrapped active_count=16 block=67",
        )
        self.assertEqual(registration, [])
        self.assertTrue(any(item.get("reason") == "no per-TEE tee_registered durations"
                            for item in diagnostics["invalid_spans"]))


class StageRowTest(unittest.TestCase):
    def test_stage_order_and_csv_columns_stay_stable(self):
        self.assertIn("tee_registration_cluster", COLLECTOR.STAGE_ORDER)
        self.assertEqual(COLLECTOR.STAGE_ORDER.index("tee_registration") + 1,
                         COLLECTOR.STAGE_ORDER.index("tee_registration_cluster"))


if __name__ == "__main__":
    unittest.main()
