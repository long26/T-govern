#!/usr/bin/env python3
"""Compare matched loaded-block events across nitch and SGX-L5 SIM runs."""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


METRICS = (
    {
        "stage": "generation",
        "event": "miner_apply_tx",
        "stratum": "transaction_first_plain_transfer",
        "aa_stratum": "transaction_first_other",
        "note": "first execution of exact plain transfer",
    },
    {
        "stage": "generation",
        "event": "miner_finalize_assemble_block",
        "stratum": "one_plain_transfer",
        "aa_stratum": "unknown",
        "note": "one plain transfer; protocol-specific full block header",
    },
    {
        "stage": "generation",
        "event": "miner_sealing_queue_wait",
        "stratum": "one_plain_transfer",
        "aa_stratum": None,
        "note": "queue/scheduler wait; no pre-instrumentation A/A event",
    },
    {
        "stage": "generation",
        "event": "miner_task_queued_for_sealing",
        "stratum": "one_plain_transfer",
        "aa_stratum": "unknown",
        "note": "generation headline through taskCh enqueue",
    },
    {
        "stage": "validation",
        "event": "process_block",
        "stratum": "one_plain_transfer",
        "aa_stratum": "one_plain_transfer",
        "note": "aligned processor.Process boundary",
    },
    {
        "stage": "validation",
        "event": "validate_state",
        "stratum": "one_plain_transfer",
        "aa_stratum": "unknown",
        "note": "aligned ValidateState boundary",
    },
    {
        "stage": "validation",
        "event": "write_block_with_state",
        "stratum": "one_plain_transfer",
        "aa_stratum": "unknown",
        "note": "aligned writeBlockWithState call boundary",
    },
    {
        "stage": "validation",
        "event": "insert_chain_total",
        "stratum": "one_plain_transfer",
        "aa_stratum": "unknown",
        "note": "outer validation headline",
    },
)

CONTENT_EVENTS = (
    "miner_finalize_assemble_block",
    "miner_sealing_queue_wait",
    "miner_task_queued_for_sealing",
    "insert_chain_total",
    "process_block",
    "validate_state",
    "write_block_with_state",
)


def load_statistics(path: Path) -> dict[tuple[str, str], dict[str, Any]]:
    payload = json.loads(path.read_text())
    return {
        (row["event"], row["stratum"]): row
        for row in payload["statistics"]
    }


def maximum_aa_noise(
    runs: list[dict[tuple[str, str], dict[str, Any]]],
    event: str,
    stratum: str | None,
    percentile: str,
) -> float | None:
    if stratum is None:
        return None
    key = (event, stratum)
    if any(key not in run for run in runs):
        return None
    return max(
        abs(float(runs[left][key][percentile]) - float(runs[right][key][percentile]))
        for left, right in ((0, 1), (2, 3))
    )


def write_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    with path.open("w", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nitch-root", required=True, type=Path)
    parser.add_argument("--tgovern-root", required=True, type=Path)
    parser.add_argument("--aa-root", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    nitch_paths = [
        args.nitch_root / f"run-{number:03d}" / "analysis"
        / "block-event-analysis.json"
        for number in (1, 2)
    ]
    tgovern_paths = [
        args.tgovern_root / "runs" / f"run-{number:03d}" / "block-analysis"
        / "block-event-analysis.json"
        for number in (1, 2)
    ]
    aa_paths = [
        args.aa_root / "runs" / f"run-{number:03d}" / "block-analysis"
        / "block-event-analysis.json"
        for number in (1, 2, 3, 4)
    ]
    for path in nitch_paths + tgovern_paths + aa_paths:
        if not path.is_file():
            parser.error(f"missing analysis input: {path}")

    nitch = [load_statistics(path) for path in nitch_paths]
    tgovern = [load_statistics(path) for path in tgovern_paths]
    aa = [load_statistics(path) for path in aa_paths]

    comparison: list[dict[str, Any]] = []
    for pair_index in range(2):
        for definition in METRICS:
            key = (definition["event"], definition["stratum"])
            if key not in nitch[pair_index] or key not in tgovern[pair_index]:
                parser.error(f"missing matched stratum: {key}")
            left = nitch[pair_index][key]
            right = tgovern[pair_index][key]
            for percentile in ("p50_ms", "p95_ms"):
                nitch_value = float(left[percentile])
                tgovern_value = float(right[percentile])
                gap = nitch_value - tgovern_value
                noise = maximum_aa_noise(
                    aa,
                    definition["event"],
                    definition["aa_stratum"],
                    percentile,
                )
                comparison.append({
                    "pair": pair_index + 1,
                    "stage": definition["stage"],
                    "event": definition["event"],
                    "stratum": definition["stratum"],
                    "percentile": percentile.removesuffix("_ms"),
                    "nitch_ms": round(nitch_value, 6),
                    "tgovern_ms": round(tgovern_value, 6),
                    "nitch_minus_tgovern_ms": round(gap, 6),
                    "nitch_over_tgovern": round(
                        nitch_value / tgovern_value, 6
                    ) if tgovern_value else None,
                    "aa_noise_absolute_ms": (
                        round(noise, 6) if noise is not None else None
                    ),
                    "exceeds_aa_noise": (
                        abs(gap) > noise if noise is not None else None
                    ),
                    "aa_noise_basis": (
                        f"{definition['event']}:{definition['aa_stratum']}"
                        if definition["aa_stratum"] is not None
                        else "not available"
                    ),
                    "nitch_raw_count": left["raw_count"],
                    "tgovern_raw_count": right["raw_count"],
                    "note": definition["note"],
                })

    content_effect: list[dict[str, Any]] = []
    for implementation, runs in (("nitch", nitch), ("tgovern", tgovern)):
        for run_index, statistics in enumerate(runs, start=1):
            for event in CONTENT_EVENTS:
                empty = statistics.get((event, "empty"))
                one_tx = statistics.get((event, "one_plain_transfer"))
                if empty is None or one_tx is None:
                    continue
                for percentile in ("p50_ms", "p95_ms"):
                    empty_value = float(empty[percentile])
                    one_tx_value = float(one_tx[percentile])
                    content_effect.append({
                        "implementation": implementation,
                        "run": run_index,
                        "event": event,
                        "percentile": percentile.removesuffix("_ms"),
                        "empty_ms": round(empty_value, 6),
                        "one_plain_transfer_ms": round(one_tx_value, 6),
                        "one_tx_minus_empty_ms": round(
                            one_tx_value - empty_value, 6
                        ),
                        "empty_raw_count": empty["raw_count"],
                        "one_tx_raw_count": one_tx["raw_count"],
                        "evidence_class": (
                            "observational within loaded run; not independent A3 intervention"
                        ),
                    })

    payload = {
        "comparison_contract": {
            "load": "0.25 transactions/s",
            "network": "loopback netem delay 20ms, jitter 0, loss 0",
            "tgovern_execution": "SGX_MODE=SIM, quote mock, attestation noop",
            "matched_content": "exactly one plain transfer, input_bytes=0, gas_used=21000",
            "full_block_equivalence": (
                "not claimed: protocol-specific headers make RLP bytes and block hash differ"
            ),
            "topology_limitation": (
                "nitch miner is one of 8 committee geth nodes; tgovern has one ON miner "
                "plus 8 TEE hosts and separate committee processes"
            ),
            "aa_note": (
                "A/A predates the final metadata patch; rows marked unknown/other are "
                "conservative fallback strata, and queue-wait has no A/A event"
            ),
        },
        "inputs": {
            "nitch": [str(path) for path in nitch_paths],
            "tgovern": [str(path) for path in tgovern_paths],
            "aa": [str(path) for path in aa_paths],
        },
        "comparison": comparison,
        "content_effect": content_effect,
    }

    args.output.mkdir(parents=True, exist_ok=False)
    (args.output / "cross-implementation-comparison.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2) + "\n"
    )
    write_csv(args.output / "cross-implementation-comparison.csv", comparison)
    write_csv(args.output / "content-effect-observational.csv", content_effect)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
