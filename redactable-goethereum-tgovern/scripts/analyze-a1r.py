#!/usr/bin/env python3
"""Evaluate the A1R nitch entry-vs-aligned generation timer intervention."""

from __future__ import annotations

import argparse
import csv
import json
import math
from pathlib import Path
from typing import Any


ORIGINAL = "miner_task_queued_for_sealing"
ALIGNED = "miner_task_queued_from_aligned_anchor"
PREAMBLE = "miner_build_preamble"
TARGET_STRATUM = "one_plain_transfer"


def percentile(values: list[float], pct: float) -> float:
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    rank = (len(ordered) - 1) * pct
    low, high = math.floor(rank), math.ceil(rank)
    if low == high:
        return ordered[low]
    return ordered[low] + (ordered[high] - ordered[low]) * (rank - low)


def load_statistics(path: Path) -> dict[tuple[str, str], dict[str, Any]]:
    payload = json.loads(path.read_text())
    return {
        (row["event"], row["stratum"]): row
        for row in payload["statistics"]
    }


def sample_closure(path: Path, run: int) -> list[dict[str, Any]]:
    with path.open(newline="") as source:
        rows = sorted(
            csv.DictReader(source),
            key=lambda row: int(row["line_number"]),
        )
    invocation = 0
    preamble_ms: float | None = None
    tasks: dict[tuple[int, str, str], dict[str, Any]] = {}
    for row in rows:
        event = row["event"]
        if event == PREAMBLE:
            invocation += 1
            preamble_ms = float(row["duration_ms"])
            continue
        if event not in {ORIGINAL, ALIGNED}:
            continue
        key = (
            invocation,
            row["logged_block_hash"],
            row["stratum"],
        )
        task = tasks.setdefault(key, {
            "run": run,
            "invocation": invocation,
            "logged_block_hash": row["logged_block_hash"],
            "block_number": int(row["block_number"]),
            "stratum": row["stratum"],
            "preamble_ms": preamble_ms,
        })
        task[event] = float(row["duration_ms"])

    result = []
    for task in tasks.values():
        if ORIGINAL not in task or ALIGNED not in task:
            continue
        task["original_minus_aligned_ms"] = task[ORIGINAL] - task[ALIGNED]
        task["closure_error_ms"] = (
            task["original_minus_aligned_ms"] - float(task["preamble_ms"])
        )
        result.append(task)
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--a1r-root", required=True, type=Path)
    parser.add_argument("--tgovern-root", required=True, type=Path)
    args = parser.parse_args()

    output_json = args.a1r_root / "a1r-analysis.json"
    output_summary = args.a1r_root / "a1r-summary.csv"
    output_closure = args.a1r_root / "a1r-sample-closure.csv"
    for output in (output_json, output_summary, output_closure):
        if output.exists():
            parser.error(f"refusing to overwrite existing output: {output}")

    closure: list[dict[str, Any]] = []
    summaries: list[dict[str, Any]] = []
    for run in (1, 2):
        nitch_analysis_dir = (
            args.a1r_root / "nitch" / f"run-{run:03d}" / "analysis"
        )
        closure.extend(sample_closure(
            nitch_analysis_dir / "block-event-samples.csv",
            run,
        ))
        nitch = load_statistics(
            nitch_analysis_dir / "block-event-analysis.json"
        )
        tgovern = load_statistics(
            args.tgovern_root / "runs" / f"run-{run:03d}"
            / "block-analysis" / "block-event-analysis.json"
        )
        preamble = nitch[(PREAMBLE, "unknown")]
        original = nitch[(ORIGINAL, TARGET_STRATUM)]
        aligned = nitch[(ALIGNED, TARGET_STRATUM)]
        control = tgovern[(ORIGINAL, TARGET_STRATUM)]
        for percentile_name in ("p50_ms", "p95_ms"):
            original_gap = float(original[percentile_name]) - float(
                control[percentile_name]
            )
            aligned_gap = float(aligned[percentile_name]) - float(
                control[percentile_name]
            )
            summaries.append({
                "run": run,
                "percentile": percentile_name.removesuffix("_ms"),
                "nitch_preamble_ms": round(
                    float(preamble[percentile_name]), 6
                ),
                "nitch_original_ms": round(
                    float(original[percentile_name]), 6
                ),
                "nitch_aligned_ms": round(
                    float(aligned[percentile_name]), 6
                ),
                "tgovern_existing_aligned_ms": round(
                    float(control[percentile_name]), 6
                ),
                "original_gap_ms": round(original_gap, 6),
                "aligned_gap_ms": round(aligned_gap, 6),
                "gap_reduction_percent": round(
                    (1 - abs(aligned_gap) / abs(original_gap)) * 100,
                    6,
                ) if original_gap else None,
                "passes_50_percent_gap_reduction": (
                    abs(aligned_gap) <= abs(original_gap) * 0.5
                ),
                "nitch_raw_count": original["raw_count"],
                "tgovern_raw_count": control["raw_count"],
            })

    target_closure = [
        row for row in closure if row["stratum"] == TARGET_STRATUM
    ]
    closure_errors = [
        abs(float(row["closure_error_ms"])) for row in target_closure
    ]
    preambles = [
        float(row["preamble_ms"]) for row in target_closure
    ]
    conclusion = {
        "hypothesis": (
            "nitch entry-to-aligned preamble explains at least 50% of the "
            "loaded one-transfer generation headline gap"
        ),
        "pair_count": 2,
        "all_p50_pairs_pass_50_percent_reduction": all(
            row["passes_50_percent_gap_reduction"]
            for row in summaries
            if row["percentile"] == "p50"
        ),
        "target_sample_count": len(target_closure),
        "maximum_absolute_closure_error_ms": max(closure_errors),
        "preamble_p50_across_target_tasks_ms": percentile(preambles, 0.50),
        "preamble_p95_across_target_tasks_ms": percentile(preambles, 0.95),
        "decision": "confirmed" if all(
            row["passes_50_percent_gap_reduction"]
            for row in summaries
            if row["percentile"] == "p50"
        ) else "rejected",
    }
    payload = {
        "contract": {
            "target_stratum": TARGET_STRATUM,
            "original_event": ORIGINAL,
            "aligned_event": ALIGNED,
            "preamble_event": PREAMBLE,
            "tgovern_control": (
                "existing A1 miner_task_queued_for_sealing, whose source "
                "anchor is after its preamble"
            ),
        },
        "summary": summaries,
        "conclusion": conclusion,
    }

    with output_closure.open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(closure[0]))
        writer.writeheader()
        writer.writerows(closure)
    with output_summary.open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(summaries[0]))
        writer.writeheader()
        writer.writerows(summaries)
    output_json.write_text(json.dumps(payload, indent=2) + "\n")
    print(output_json)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
