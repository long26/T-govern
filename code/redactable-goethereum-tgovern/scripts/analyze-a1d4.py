#!/usr/bin/env python3
"""Evaluate the A1D4 nitch block-cache read intervention."""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


METRICS = (
    ("miner_make_current_ancestor_lookup", "unknown", "primary"),
    ("miner_make_current", "unknown", "primary"),
    ("miner_task_queued_for_sealing", "one_plain_transfer", "primary"),
    ("miner_apply_tx", "transaction_first_plain_transfer", "negative-control"),
    ("miner_finalize_assemble_block", "one_plain_transfer", "negative-control"),
)


def load(path: Path) -> dict[tuple[str, str], dict[str, Any]]:
    payload = json.loads(path.read_text())
    return {
        (row["event"], row["stratum"]): row
        for row in payload["statistics"]
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--control-root", required=True, type=Path)
    parser.add_argument("--intervention-root", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)

    controls = [
        load(
            args.control_root / f"run-{run:03d}" / "analysis"
            / "block-event-analysis.json"
        )
        for run in (1, 2)
    ]
    interventions = [
        load(
            args.intervention_root / f"run-{run:03d}" / "analysis"
            / "block-event-analysis.json"
        )
        for run in (1, 2)
    ]

    rows: list[dict[str, Any]] = []
    for pair in range(2):
        for event, stratum, role in METRICS:
            left = controls[pair][(event, stratum)]
            right = interventions[pair][(event, stratum)]
            for percentile_name in ("p50_ms", "p95_ms"):
                control = float(left[percentile_name])
                intervention = float(right[percentile_name])
                rows.append({
                    "pair": pair + 1,
                    "role": role,
                    "event": event,
                    "stratum": stratum,
                    "percentile": percentile_name.removesuffix("_ms"),
                    "control_ms": round(control, 6),
                    "intervention_ms": round(intervention, 6),
                    "intervention_minus_control_ms": round(
                        intervention - control, 6
                    ),
                    "reduction_percent": round(
                        (control - intervention) / control * 100, 6
                    ) if control else None,
                    "control_raw_count": left["raw_count"],
                    "intervention_raw_count": right["raw_count"],
                })

    primary_p50 = [
        row for row in rows
        if row["role"] == "primary" and row["percentile"] == "p50"
    ]
    by_event: dict[str, list[dict[str, Any]]] = {}
    for row in primary_p50:
        by_event.setdefault(str(row["event"]), []).append(row)
    decisions = {
        event: (
            "confirmed" if len(event_rows) == 2 and all(
                float(row["reduction_percent"]) >= 50
                for row in event_rows
            ) else "rejected"
        )
        for event, event_rows in by_event.items()
    }
    payload = {
        "contract": {
            "control": "A1D3 cache read disabled",
            "intervention": (
                "A1D4 only restores the existing blockCache.Get(hash) "
                "short-circuit in nitch BlockChain.GetBlock"
            ),
            "pass_rule": (
                "both p50 pairs reduce ancestor lookup, makeCurrent, and "
                "one-transfer queued headline by at least 50%"
            ),
            "headline_aa_noise_p50_ms": 0.236,
        },
        "decisions": decisions,
        "all_primary_p50_decisions_confirmed": all(
            decision == "confirmed" for decision in decisions.values()
        ),
        "comparison": rows,
    }
    (args.output / "a1d4-analysis.json").write_text(
        json.dumps(payload, indent=2) + "\n"
    )
    with (args.output / "a1d4-comparison.csv").open("x", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
