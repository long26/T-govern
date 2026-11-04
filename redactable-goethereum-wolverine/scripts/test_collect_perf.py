#!/usr/bin/env python3
"""Regression coverage for receiver-local ledger update headlines."""

import unittest

import collect_perf as collector


def receiver(index=2, join="11-0", duration=5.0, **overrides):
    record = {
        "source": "geth", "source_file": f"geth-node-{index}.log",
        "stage": "edit_execution", "event": "redaction_edit_applied",
        "node_id": index, "node": 8544 + index, "node_role": "core",
        "join_key": join, "duration_ms": duration,
        "measurement_window": True, "status": "ok",
    }
    return {**record, **overrides}


def paired(node=2, join="11-0", **overrides):
    base = {
        "stage": "ledger_update", "marker_schema": 3,
        "node_id": node, "node_role": "core", "join_key": join,
        "block": 11, "tx_index": 0, "measurement_window": True, "status": "ok",
    }
    start = {**base, "event": "tx_modify_received", "timestamp_s": 100.0}
    end = {**base, "event": "local_block_updated", "timestamp_s": 100.005,
           "duration_ms": 5.0, "block_hash": "0xabc", **overrides}
    return [start, end]


class LedgerUpdateTests(unittest.TestCase):
    def test_all_receivers_without_observer(self):
        records = [receiver(2), receiver(3, duration=7.0)]
        row = next(r for r in collector.compute_stage_rows(records, [])
                   if r["stage"] == "ledger_update")
        self.assertEqual(row["count"], 2)
        self.assertEqual(row["filtered_mean_ms"], 6.0)

    def test_proposer_internal_and_unsuccessful_samples_excluded(self):
        records = [receiver(), receiver(1, node="proposer", duration=170),
                   receiver(3, status="failed"), receiver(4, measurement_window=False),
                   receiver(5, duration=-1), receiver(6, duration=float("nan")),
                   receiver(7, node=None), receiver(8, event="lu_u_total")]
        self.assertEqual(len(collector.compute_ledger_update_samples(records)), 1)

    def test_duplicate_legacy_and_versioned_markers_count_once(self):
        records = [receiver(), receiver(), *paired()]
        self.assertEqual(len(collector.compute_ledger_update_samples(records)), 1)

    def test_pairing_is_per_node_and_request_not_role(self):
        records = [*paired(2), *paired(3, node_role="ledger_observer")]
        samples = collector.compute_ledger_update_samples(records)
        self.assertEqual(len(samples), 2)
        self.assertEqual(len({r["sample_id"] for r in samples}), 2)
        self.assertEqual(collector.compute_ledger_update_samples([paired(2)[0], paired(3)[1]]), [])

    def test_incomplete_or_inconsistent_pairs_rejected(self):
        for records in [[paired()[0]], [paired()[1]], paired(block=12),
                        paired(tx_index=1), paired(timestamp_s=99.0), paired(block_hash=""),
                        paired(status="failed"), paired(duration_ms=-1)]:
            with self.subTest(records=records):
                self.assertEqual(collector.compute_ledger_update_samples(records), [])

    def test_pair_can_use_timestamp_when_duration_missing(self):
        samples = collector.compute_ledger_update_samples(paired(duration_ms=None))
        self.assertEqual(len(samples), 1)
        self.assertAlmostEqual(samples[0]["duration_ms"], 5.0)

    def test_tukey_filter_applied_to_receiver_headline(self):
        records = [receiver(join=f"{i}-0") for i in range(30)]
        records.append(receiver(join="99-0", duration=100.0))
        row = next(r for r in collector.compute_stage_rows(records, [])
                   if r["stage"] == "ledger_update")
        self.assertEqual((row["raw_count"], row["used_count"], row["excluded_count"]), (31, 30, 1))
        self.assertEqual(row["filtered_mean_ms"], 5.0)

    def test_no_observer_does_not_imply_versioned_ledger_data(self):
        self.assertFalse(collector.timing_schema_status([], {})["ledger_update_v3"])
        self.assertEqual(collector.timing_schema_status([receiver()], {})["ledger_update_receiver_count"], 1)

    def test_missing_ledger_samples_are_reported(self):
        rows = collector.compute_stage_rows([], [])
        alerts = collector.completeness_report([], [], rows, [], {})
        self.assertTrue(any("stage=ledger_update " in alert for alert in alerts))


if __name__ == "__main__":
    unittest.main()
