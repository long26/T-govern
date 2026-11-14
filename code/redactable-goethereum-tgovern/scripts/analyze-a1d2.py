#!/usr/bin/env python3
"""Compare A1D2 header/context subphases across nitch and SGX-L5 SIM."""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


SUBPHASES = (
    ("miner_future_timestamp_wait", "H1a"),
    ("miner_engine_header_prepare", "H1b"),
    ("miner_make_current", "H1c"),
    ("miner_uncle_context_prepare", "H1d"),
)
TOTAL = "miner_header_context_prepare"
STRATUM = "unknown"


def load(path: Path) -> dict[tuple[str, str], dict[str, Any]]:
    payload = json.loads(path.read_text())
    return {
        (row["event"], row["stratum"]): row
        for row in payload["statistics"]
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nitch-root", required=True, type=Path)
    parser.add_argument("--tgovern-root", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)

    nitch = [
        load(
            args.nitch_root / f"run-{run:03d}" / "analysis"
            / "block-event-analysis.json"
        )
        for run in (1, 2)
    ]
    tgovern = [
        load(
            args.tgovern_root / "runs" / f"run-{run:03d}"
            / "block-analysis" / "block-event-analysis.json"
        )
        for run in (1, 2)
    ]

    rows: list[dict[str, Any]] = []
    for pair in range(2):
        for percentile in ("p50_ms", "p95_ms"):
            total_gap = (
                float(nitch[pair][(TOTAL, STRATUM)][percentile])
                - float(tgovern[pair][(TOTAL, STRATUM)][percentile])
            )
            for event, candidate in SUBPHASES:
                left = nitch[pair][(event, STRATUM)]
                right = tgovern[pair][(event, STRATUM)]
                gap = float(left[percentile]) - float(right[percentile])
                rows.append({
                    "pair": pair + 1,
                    "candidate": candidate,
                    "event": event,
                    "percentile": percentile.removesuffix("_ms"),
                    "nitch_ms": round(float(left[percentile]), 6),
                    "tgovern_ms": round(float(right[percentile]), 6),
                    "nitch_minus_tgovern_ms": round(gap, 6),
                    "header_context_gap_ms": round(total_gap, 6),
                    "subphase_gap_over_header_context_gap_percent": round(
                        gap / total_gap * 100, 6
                    ) if total_gap else None,
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
            and row["subphase_gap_over_header_context_gap_percent"] >= 50
            for row in relevant
        )

    decisions = {
        candidate: "confirmed" if passes(candidate) else "rejected"
        for _, candidate in SUBPHASES
    }
    payload = {
        "contract": {
            "stratum": STRATUM,
            "denominator": TOTAL,
            "pass_rule": (
                "both p50 pairs have at least 1ms same-direction subphase gap "
                "and subphase gap is at least 50% of header/context gap"
            ),
        },
        "decisions": decisions,
        "comparison": rows,
    }
    (args.output / "a1d2-analysis.json").write_text(
        json.dumps(payload, indent=2) + "\n"
    )
    with (args.output / "a1d2-comparison.csv").open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
