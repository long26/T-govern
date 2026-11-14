#!/usr/bin/env python3
"""Compare A1D3 makeCurrent internals across nitch and SGX-L5 SIM."""

from __future__ import annotations

import argparse
import csv
import json
import math
from collections import Counter
from pathlib import Path
from typing import Any


DETAILS = (
    ("miner_make_current_state_at", "H1"),
    ("miner_make_current_ancestor_lookup", "H2"),
    ("miner_make_current_ancestor_hash_first", "H3-first-hash"),
    ("miner_make_current_ancestor_hash_cached", "H3-cached-hash"),
    ("miner_make_current_env_alloc", "diagnostic"),
    ("miner_make_current_set_add", "diagnostic"),
)
TOTAL = "miner_make_current"
STRATUM = "unknown"


def percentile(values: list[float], pct: float) -> float:
    ordered = sorted(values)
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


def count_seven_statistics(path: Path) -> tuple[Counter[int], dict[str, dict[str, float]]]:
    with path.open(newline="") as source:
        rows = list(csv.DictReader(source))
    lookup = [
        row for row in rows
        if row["event"] == "miner_make_current_ancestor_lookup"
    ]
    distribution = Counter(int(row["ancestor_count"]) for row in lookup)
    by_event: dict[str, dict[str, float]] = {}
    for event, _ in DETAILS:
        values = [
            float(row["duration_ms"])
            for row in rows
            if row["event"] == event and row["ancestor_count"] == "7"
        ]
        if values:
            by_event[event] = {
                "raw_count": len(values),
                "p50_ms": percentile(values, 0.50),
                "p95_ms": percentile(values, 0.95),
            }
    return distribution, by_event


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nitch-root", required=True, type=Path)
    parser.add_argument("--tgovern-root", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)

    nitch_stats = []
    nitch_count7 = []
    nitch_distributions = []
    tgovern_stats = []
    tgovern_count7 = []
    tgovern_distributions = []
    for run in (1, 2):
        nitch_dir = args.nitch_root / f"run-{run:03d}" / "analysis-v2"
        tgovern_dir = (
            args.tgovern_root / "runs" / f"run-{run:03d}"
            / "block-analysis-v2"
        )
        nitch_stats.append(load_statistics(
            nitch_dir / "block-event-analysis.json"
        ))
        distribution, count7 = count_seven_statistics(
            nitch_dir / "block-event-samples.csv"
        )
        nitch_distributions.append(distribution)
        nitch_count7.append(count7)
        tgovern_stats.append(load_statistics(
            tgovern_dir / "block-event-analysis.json"
        ))
        distribution, count7 = count_seven_statistics(
            tgovern_dir / "block-event-samples.csv"
        )
        tgovern_distributions.append(distribution)
        tgovern_count7.append(count7)

    rows: list[dict[str, Any]] = []
    for pair in range(2):
        for percentile_name in ("p50_ms", "p95_ms"):
            total_gap = (
                float(nitch_stats[pair][(TOTAL, STRATUM)][percentile_name])
                - float(tgovern_stats[pair][(TOTAL, STRATUM)][percentile_name])
            )
            for event, candidate in DETAILS:
                left = nitch_stats[pair][(event, STRATUM)]
                right = tgovern_stats[pair][(event, STRATUM)]
                gap = float(left[percentile_name]) - float(
                    right[percentile_name]
                )
                left7 = nitch_count7[pair].get(event)
                right7 = tgovern_count7[pair].get(event)
                rows.append({
                    "pair": pair + 1,
                    "candidate": candidate,
                    "event": event,
                    "percentile": percentile_name.removesuffix("_ms"),
                    "nitch_ms": round(float(left[percentile_name]), 6),
                    "tgovern_ms": round(float(right[percentile_name]), 6),
                    "nitch_minus_tgovern_ms": round(gap, 6),
                    "make_current_gap_ms": round(total_gap, 6),
                    "detail_gap_over_make_current_gap_percent": round(
                        gap / total_gap * 100, 6
                    ) if total_gap else None,
                    "nitch_count7_ms": (
                        round(float(left7[percentile_name]), 6)
                        if left7 else None
                    ),
                    "tgovern_count7_ms": (
                        round(float(right7[percentile_name]), 6)
                        if right7 else None
                    ),
                    "nitch_raw_count": left["raw_count"],
                    "tgovern_raw_count": right["raw_count"],
                })

    def passes(candidate: str) -> bool:
        relevant = [
            row for row in rows
            if row["candidate"] == candidate and row["percentile"] == "p50"
        ]
        return all(
            row["nitch_minus_tgovern_ms"] >= 1
            and row["detail_gap_over_make_current_gap_percent"] >= 50
            for row in relevant
        )

    payload = {
        "contract": {
            "denominator": TOTAL,
            "pass_rule": (
                "both p50 pairs have at least 1ms same-direction detail gap "
                "and detail gap is at least 50% of makeCurrent gap"
            ),
            "ancestor_count_control": (
                "report all counts and repeat detailed percentiles at count=7"
            ),
        },
        "ancestor_count_distributions": {
            "nitch": [
                dict(sorted(distribution.items()))
                for distribution in nitch_distributions
            ],
            "tgovern": [
                dict(sorted(distribution.items()))
                for distribution in tgovern_distributions
            ],
        },
        "decisions": {
            "H1_state_at": "confirmed" if passes("H1") else "rejected",
            "H2_ancestor_lookup": (
                "confirmed" if passes("H2") else "rejected"
            ),
            "H3_first_hash": (
                "confirmed" if passes("H3-first-hash") else "rejected"
            ),
            "H3_cached_hash": (
                "confirmed" if passes("H3-cached-hash") else "rejected"
            ),
        },
        "comparison": rows,
    }
    (args.output / "a1d3-analysis.json").write_text(
        json.dumps(payload, indent=2) + "\n"
    )
    with (args.output / "a1d3-comparison.csv").open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
