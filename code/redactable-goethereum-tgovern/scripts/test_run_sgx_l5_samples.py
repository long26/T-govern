#!/usr/bin/env python3

import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().with_name("run-sgx-l5-samples.py")
SPEC = importlib.util.spec_from_file_location("run_sgx_l5_samples", SCRIPT)
assert SPEC and SPEC.loader
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)


class RunSgxL5SamplesTest(unittest.TestCase):
    def test_stage_stats_filters_far_outlier_mean_with_three_iqr(self) -> None:
        samples = [{"duration_ms": float(value)} for value in range(1, 31)]
        samples.append({"duration_ms": 1_000.0})

        stats = RUNNER.stage_stats(samples)

        self.assertEqual(stats["count"], 31)
        self.assertEqual(stats["used_count"], 30)
        self.assertEqual(stats["excluded_count"], 1)
        self.assertEqual(stats["raw_mean_ms"], 47.258)
        self.assertEqual(stats["mean_ms"], 15.5)
        self.assertGreater(stats["raw_mean_ms"], stats["mean_ms"])
        self.assertTrue(samples[-1]["statistical_outlier"])

    def test_stage_stats_keeps_single_sample(self) -> None:
        samples = [{"duration_ms": 528.0}]

        stats = RUNNER.stage_stats(samples)

        self.assertEqual(stats["count"], 1)
        self.assertEqual(stats["used_count"], 1)
        self.assertEqual(stats["excluded_count"], 0)
        self.assertEqual(stats["raw_mean_ms"], 528.0)
        self.assertEqual(stats["mean_ms"], 528.0)


if __name__ == "__main__":
    unittest.main()
