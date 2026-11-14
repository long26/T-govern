#!/usr/bin/env python3
"""Compare A2D nitch controls with A2I hash-hint interventions."""

from __future__ import annotations

import argparse
import csv
import importlib.util
import json
from pathlib import Path
from typing import Any


def load_a2d_module() -> Any:
    path = Path(__file__).with_name("analyze-a2d.py")
    spec = importlib.util.spec_from_file_location("analyze_a2d_shared", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def p50(run: dict[str, Any], group: str, metric: str) -> float:
    value = run[group][metric]["p50_ms"]
    if value is None:
        raise ValueError(f"missing p50 for {group}/{metric}")
    return float(value)


def compare_metric(
    control: dict[str, Any],
    intervention: dict[str, Any],
    group: str,
    metric: str,
    role: str,
) -> dict[str, Any]:
    before = p50(control, group, metric)
    after = p50(intervention, group, metric)
    return {
        "pair": control["run"],
        "role": role,
        "group": group,
        "metric": metric,
        "control_ms": before,
        "intervention_ms": after,
        "delta_ms": after - before,
        "reduction_percent": 100 * (before - after) / before if before else None,
        "control_count": control[group][metric]["raw_count"],
        "intervention_count": intervention[group][metric]["raw_count"],
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--control-root", type=Path, required=True)
    parser.add_argument("--intervention-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    shared = load_a2d_module()

    controls: list[dict[str, Any]] = []
    interventions: list[dict[str, Any]] = []
    for run in (1, 2):
        control_dir = args.control_root / f"run-{run:03d}"
        controls.append(
            shared.analyze_run(
                "nitch-control",
                run,
                control_dir / "analysis-v2" / "block-event-samples.csv",
                control_dir / "logs" / "geth-node-2.log",
            )
        )
        intervention_dir = args.intervention_root / f"run-{run:03d}"
        interventions.append(
            shared.analyze_run(
                "nitch-intervention",
                run,
                intervention_dir / "analysis" / "block-event-samples.csv",
                intervention_dir / "logs" / "geth-node-2.log",
            )
        )

    declared = [
        ("header_worker", "incoming_header_hash_ns", "primary"),
        ("write", "batch_build_ns", "primary"),
        ("write", "canonical_head_ns", "primary"),
        ("core", "write_block_with_state", "primary"),
        ("core", "insert_chain_total", "primary"),
        ("header_worker", "rules_and_seal_ns", "negative-control"),
        ("core", "process_block", "negative-control"),
        ("core", "validate_state", "negative-control"),
        ("write", "batch_write_ns", "negative-control"),
        ("write", "state_commit_ns", "negative-control"),
    ]
    comparisons = [
        compare_metric(control, intervention, group, metric, role)
        for control, intervention in zip(controls, interventions)
        for group, metric, role in declared
    ]
    by_metric: dict[tuple[str, str], list[dict[str, Any]]] = {}
    for row in comparisons:
        by_metric.setdefault((row["group"], row["metric"]), []).append(row)

    decisions = {
        "incoming_header_hash": all(
            row["reduction_percent"] >= 50
            for row in by_metric[("header_worker", "incoming_header_hash_ns")]
        ),
        "batch_build": all(
            row["reduction_percent"] >= 50
            for row in by_metric[("write", "batch_build_ns")]
        ),
        "canonical_head": all(
            row["reduction_percent"] >= 50
            for row in by_metric[("write", "canonical_head_ns")]
        ),
        "write_total": all(
            row["reduction_percent"] >= 50
            for row in by_metric[("core", "write_block_with_state")]
        ),
        "insert_total_same_direction_and_material": all(
            row["delta_ms"] < 0
            for row in by_metric[("core", "insert_chain_total")]
        )
        and any(
            -row["delta_ms"] > 0.204
            for row in by_metric[("core", "insert_chain_total")]
        ),
    }
    report = {
        "contract": {
            "intervention": "reuse Block.Hash as a non-encoded hint on three immutable Header copies",
            "primary_pass": "header hash, batch build, canonical head, and write total each reduce >=50% in both pairs; insert total same direction and material",
            "validation_aa_p50_ms": 0.204,
        },
        "controls": controls,
        "interventions": interventions,
        "comparisons": comparisons,
        "decisions": decisions,
        "all_primary_passed": all(decisions.values()),
    }
    args.output.mkdir(parents=True, exist_ok=False)
    (args.output / "a2i-analysis.json").write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n"
    )
    with (args.output / "a2i-comparison.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(comparisons[0]))
        writer.writeheader()
        writer.writerows(comparisons)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
