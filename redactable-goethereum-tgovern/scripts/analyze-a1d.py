#!/usr/bin/env python3
"""Compare A1D generation phase splits across nitch and SGX-L5 SIM."""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


EVENTS = (
    ("miner_header_context_prepare", "unknown", "H1"),
    ("miner_commit_state_copy", "one_plain_transfer", "H2"),
    ("miner_task_send", "one_plain_transfer", "H3"),
    ("miner_txpool_pending", "unknown", "diagnostic"),
    ("miner_finalize_assemble_block", "one_plain_transfer", "diagnostic"),
    ("miner_task_queued_for_sealing", "one_plain_transfer", "headline"),
)


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
        headline_key = ("miner_task_queued_for_sealing", "one_plain_transfer")
        for percentile in ("p50_ms", "p95_ms"):
            total_gap = (
                float(nitch[pair][headline_key][percentile])
                - float(tgovern[pair][headline_key][percentile])
            )
            for event, stratum, candidate in EVENTS:
                key = (event, stratum)
                left = nitch[pair][key]
                right = tgovern[pair][key]
                gap = float(left[percentile]) - float(right[percentile])
                rows.append({
                    "pair": pair + 1,
                    "candidate": candidate,
                    "event": event,
                    "stratum": stratum,
                    "percentile": percentile.removesuffix("_ms"),
                    "nitch_ms": round(float(left[percentile]), 6),
                    "tgovern_ms": round(float(right[percentile]), 6),
                    "nitch_minus_tgovern_ms": round(gap, 6),
                    "headline_gap_ms": round(total_gap, 6),
                    "phase_gap_over_headline_gap_percent": round(
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
            and row["phase_gap_over_headline_gap_percent"] >= 50
            for row in relevant
        )

    payload = {
        "contract": {
            "pairs": [
                {"nitch": "run-001", "tgovern": "run-001"},
                {"nitch": "run-002", "tgovern": "run-002"},
            ],
            "target_content": (
                "one plain transfer for block-bound phases; invocation-level "
                "unknown stratum for header/context and txpool"
            ),
            "pass_rule": (
                "both p50 pairs have at least 1ms same-direction phase gap "
                "and phase gap is at least 50% of headline gap"
            ),
        },
        "decisions": {
            "H1_header_context_is_main_source": (
                "confirmed" if passes("H1") else "rejected"
            ),
            "H2_state_copy_is_main_source": (
                "confirmed" if passes("H2") else "rejected"
            ),
            "H3_task_send_is_main_source": (
                "confirmed" if passes("H3") else "rejected"
            ),
            "full_task_hook": (
                "no samples in either implementation because the hook was nil "
                "for these commits"
            ),
        },
        "comparison": rows,
    }
    (args.output / "a1d-analysis.json").write_text(
        json.dumps(payload, indent=2) + "\n"
    )
    with (args.output / "a1d-comparison.csv").open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
