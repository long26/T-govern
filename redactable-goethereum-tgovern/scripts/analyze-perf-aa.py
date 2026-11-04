#!/usr/bin/env python3
"""Compute predeclared A/A p50/p95 noise bands from independent SGX-L5 runs."""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


STAT_FIELDS = (
    "raw_count",
    "used_count",
    "excluded_count",
    "excluded_rate",
    "raw_mean",
    "filtered_mean",
    "p50",
    "p95",
    "p99",
    "max",
)
DELTA_FIELDS = ("p50", "p95", "raw_mean", "filtered_mean")


def numeric(value: str) -> float:
    return float(value)


def load_run(run_dir: Path) -> dict[tuple[str, str, str], dict[str, Any]]:
    stage_path = run_dir / "timing" / "l5-timing-stages.csv"
    rows: dict[tuple[str, str, str], dict[str, Any]] = {}
    with stage_path.open(newline="") as source:
        for raw in csv.DictReader(source):
            row: dict[str, Any] = dict(raw)
            for field in STAT_FIELDS:
                row[field] = numeric(raw[field])
            key = (raw["stage"], raw["stratum"], raw["headline"])
            rows[key] = row
    return rows


def symmetric_relative_percent(left: float, right: float) -> float:
    midpoint = (abs(left) + abs(right)) / 2
    return 0.0 if midpoint == 0 else abs(left - right) / midpoint * 100


def diagnostic_counts(run_dir: Path) -> dict[str, int]:
    summary = json.loads(
        (run_dir / "timing" / "l5-timing-summary.json").read_text()
    )
    return {
        name: len(records)
        for name, records in summary["diagnostics"].items()
        if records
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence_dir", type=Path)
    args = parser.parse_args()
    evidence_dir = args.evidence_dir.resolve()
    run_dirs = sorted((evidence_dir / "runs").glob("run-*"))
    if len(run_dirs) < 4 or len(run_dirs) % 2:
        parser.error("A/A analysis requires an even number of runs and at least four")

    run_rows = {run_dir.name: load_run(run_dir) for run_dir in run_dirs}
    all_keys = sorted(set.intersection(*(set(rows) for rows in run_rows.values())))
    pairs = list(zip(run_dirs[::2], run_dirs[1::2]))

    flattened = []
    for run_name, rows in run_rows.items():
        for key in all_keys:
            row = rows[key]
            flattened.append({
                "run": run_name,
                "stage": key[0],
                "stratum": key[1],
                "headline": key[2],
                **{field: row[field] for field in STAT_FIELDS},
                "basis": row["basis"],
                "note": row["note"],
            })

    pair_deltas = []
    for pair_number, (left_dir, right_dir) in enumerate(pairs, start=1):
        left_rows = run_rows[left_dir.name]
        right_rows = run_rows[right_dir.name]
        for key in all_keys:
            for metric in DELTA_FIELDS:
                left = float(left_rows[key][metric])
                right = float(right_rows[key][metric])
                pair_deltas.append({
                    "pair": pair_number,
                    "left_run": left_dir.name,
                    "right_run": right_dir.name,
                    "stage": key[0],
                    "stratum": key[1],
                    "headline": key[2],
                    "metric": metric,
                    "left_value_ms": left,
                    "right_value_ms": right,
                    "absolute_delta_ms": round(abs(left - right), 6),
                    "symmetric_relative_delta_percent": round(
                        symmetric_relative_percent(left, right), 6
                    ),
                })

    bands = []
    for key in all_keys:
        for metric in DELTA_FIELDS:
            relevant = [
                row for row in pair_deltas
                if (row["stage"], row["stratum"], row["headline"]) == key
                and row["metric"] == metric
            ]
            bands.append({
                "stage": key[0],
                "stratum": key[1],
                "headline": key[2],
                "metric": metric,
                "pair_count": len(relevant),
                "noise_band_absolute_ms": max(
                    row["absolute_delta_ms"] for row in relevant
                ),
                "noise_band_symmetric_relative_percent": max(
                    row["symmetric_relative_delta_percent"] for row in relevant
                ),
            })

    diagnostics = [
        {"run": run_dir.name, **diagnostic_counts(run_dir)}
        for run_dir in run_dirs
    ]
    payload = {
        "definition": {
            "pairs": [
                {"left": left.name, "right": right.name}
                for left, right in pairs
            ],
            "absolute": "abs(left-right)",
            "relative": "abs(left-right)/mean(abs(left),abs(right))*100",
            "band": "maximum across the two independent adjacent A/A pairs",
        },
        "run_statistics": flattened,
        "pair_deltas": pair_deltas,
        "noise_bands": bands,
        "diagnostic_counts": diagnostics,
    }
    (evidence_dir / "aa-noise-analysis.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2)
    )

    def write_csv(name: str, rows: list[dict[str, Any]]) -> None:
        with (evidence_dir / name).open("w", newline="") as output:
            writer = csv.DictWriter(output, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)

    write_csv("aa-run-statistics.csv", flattened)
    write_csv("aa-pair-deltas.csv", pair_deltas)
    write_csv("aa-noise-bands.csv", bands)
    write_csv("aa-diagnostic-counts.csv", diagnostics)
    print(evidence_dir / "aa-noise-bands.csv")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
