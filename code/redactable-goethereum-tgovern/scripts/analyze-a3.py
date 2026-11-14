#!/usr/bin/env python3
"""Analyze independent empty versus loaded one-transfer A3 lifecycles."""

from __future__ import annotations

import argparse
import csv
import json
import statistics
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


METRICS = (
    ("miner", "miner_finalize_assemble_block"),
    ("miner", "miner_sealing_queue_wait"),
    ("miner", "miner_task_queued_for_sealing"),
    ("observer", "process_block"),
    ("observer", "validate_state"),
    ("observer", "write_block_with_state"),
    ("observer", "insert_chain_total"),
)
STAT_FIELDS = (
    "raw_count",
    "eligible_count",
    "excluded_count",
    "statistical_outlier_count",
    "used_count_for_filtered_mean",
    "raw_mean_ms",
    "filtered_mean_ms",
    "p50_ms",
    "p95_ms",
    "p99_ms",
    "max_ms",
)
HISTORICAL_SAME_EVENT_GAP_MS = {
    1: {
        "miner_task_queued_for_sealing": 28.281460,
        "insert_chain_total": 14.826187,
    },
    2: {
        "miner_task_queued_for_sealing": 30.297218,
        "insert_chain_total": 14.505427,
    },
}


def read_json(path: Path) -> Any:
    return json.loads(path.read_text())


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [
        json.loads(line)
        for line in path.read_text().splitlines()
        if line.strip()
    ]


def find_stat(
    analysis: dict[str, Any], role: str, event: str, stratum: str
) -> dict[str, Any]:
    matches = [
        row
        for row in analysis["statistics"]
        if row["role"] == role
        and row["event"] == event
        and row["stratum"] == stratum
    ]
    if len(matches) != 1:
        raise ValueError(
            f"expected one statistic for {role}/{event}/{stratum}, got {len(matches)}"
        )
    return matches[0]


def block_summary(blocks: list[dict[str, Any]], mode: str) -> dict[str, Any]:
    selected = [
        block
        for block in blocks
        if block["tx_count"] == (0 if mode == "empty" else 1)
    ]
    return {
        "all_block_count": len(blocks),
        "selected_block_count": len(selected),
        "tx_counts_all": sorted({block["tx_count"] for block in blocks}),
        "selected_gas_used": sorted({block["gas_used"] for block in selected}),
        "selected_block_bytes": sorted({block["block_bytes"] for block in selected}),
        "selected_hashes": [block["hash"] for block in selected],
        "selected_numbers": [block["number"] for block in selected],
        "selected_tx_hashes": [
            block["tx_hashes"][0]
            for block in selected
            if block["tx_hashes"]
        ],
    }


def stat_columns(prefix: str, stat: dict[str, Any]) -> dict[str, Any]:
    return {f"{prefix}_{field}": stat[field] for field in STAT_FIELDS}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--empty-root", type=Path, required=True)
    parser.add_argument("--loaded-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    args.output.mkdir(parents=True, exist_ok=False)
    pairs: list[dict[str, Any]] = []
    metadata: list[dict[str, Any]] = []
    pair_inputs: list[dict[str, Any]] = []
    for run in (1, 2):
        empty_dir = args.empty_root / f"run-{run:03d}"
        loaded_dir = args.loaded_root / f"run-{run:03d}"
        empty_pre = read_json(empty_dir / "manifest.pre.json")
        loaded_pre = read_json(loaded_dir / "manifest.pre.json")
        empty_run = read_json(empty_dir / "run-manifest.json")
        loaded_run = read_json(loaded_dir / "run-manifest.json")
        if not empty_run["success"] or not loaded_run["success"]:
            raise ValueError(f"pair {run} contains a failed lifecycle")
        empty_sha = empty_pre["binaries"]["geth"]["sha256"]
        loaded_sha = loaded_pre["binaries"]["geth"]["sha256"]
        if empty_sha != loaded_sha:
            raise ValueError(
                f"pair {run} binary mismatch empty={empty_sha} loaded={loaded_sha}"
            )
        empty_analysis = read_json(
            empty_dir / "analysis" / "block-event-analysis.json"
        )
        loaded_analysis = read_json(
            loaded_dir / "analysis" / "block-event-analysis.json"
        )
        empty_blocks = block_summary(
            read_jsonl(empty_dir / "blocks.jsonl"), "empty"
        )
        loaded_blocks = block_summary(
            read_jsonl(loaded_dir / "blocks.jsonl"), "loaded"
        )
        empty_exact = (
            empty_blocks["all_block_count"] == empty_blocks["selected_block_count"]
            and empty_blocks["tx_counts_all"] == [0]
            and empty_blocks["selected_gas_used"] == [0]
        )
        loaded_exact = (
            loaded_blocks["selected_block_count"] >= 15
            and loaded_blocks["selected_gas_used"] == [21000]
            and len(loaded_blocks["selected_tx_hashes"])
            == loaded_blocks["selected_block_count"]
        )
        metadata.append({
            "pair": run,
            "binary_sha256": empty_sha,
            "binary_identical": True,
            "empty": empty_blocks,
            "loaded_one_transfer": loaded_blocks,
            "empty_stratum_exact": empty_exact,
            "loaded_one_transfer_stratum_exact": loaded_exact,
            "empty_functional_checks": empty_run["functional_checks"],
            "loaded_functional_checks": loaded_run["functional_checks"],
        })
        pair_inputs.append({
            "pair": run,
            "empty_dir": str(empty_dir.resolve()),
            "loaded_dir": str(loaded_dir.resolve()),
            "empty_manifest_pre": empty_pre,
            "loaded_manifest_pre": loaded_pre,
            "empty_run_manifest": empty_run,
            "loaded_run_manifest": loaded_run,
        })

        for role, event in METRICS:
            empty_stat = find_stat(empty_analysis, role, event, "empty")
            loaded_stat = find_stat(
                loaded_analysis, role, event, "one_plain_transfer"
            )
            reference = HISTORICAL_SAME_EVENT_GAP_MS[run].get(event)
            delta_p50 = loaded_stat["p50_ms"] - empty_stat["p50_ms"]
            row = {
                "pair": run,
                "role": role,
                "event": event,
                "empty_stratum": "empty",
                "loaded_stratum": "one_plain_transfer",
                **stat_columns("empty", empty_stat),
                **stat_columns("loaded", loaded_stat),
                "delta_raw_mean_ms": (
                    loaded_stat["raw_mean_ms"] - empty_stat["raw_mean_ms"]
                ),
                "delta_filtered_mean_ms": (
                    loaded_stat["filtered_mean_ms"]
                    - empty_stat["filtered_mean_ms"]
                ),
                "delta_p50_ms": delta_p50,
                "delta_p95_ms": (
                    loaded_stat["p95_ms"] - empty_stat["p95_ms"]
                ),
                "delta_p99_ms": (
                    loaded_stat["p99_ms"] - empty_stat["p99_ms"]
                ),
                "delta_max_ms": loaded_stat["max_ms"] - empty_stat["max_ms"],
                "historical_same_event_gap_reference_ms": reference,
                "content_effect_reaches_historical_gap": (
                    abs(delta_p50) >= reference if reference is not None else None
                ),
            }
            pairs.append(row)

    def rows(event: str) -> list[dict[str, Any]]:
        return [row for row in pairs if row["event"] == event]

    directions = {
        event: all(row["delta_p50_ms"] > 0 for row in rows(event))
        for event in (
            "miner_finalize_assemble_block",
            "process_block",
            "validate_state",
        )
    }
    headline_not_explained = {
        event: all(
            not row["content_effect_reaches_historical_gap"]
            for row in rows(event)
        )
        for event in (
            "miner_task_queued_for_sealing",
            "insert_chain_total",
        )
    }
    metadata_exact = all(
        row["empty_stratum_exact"]
        and row["loaded_one_transfer_stratum_exact"]
        for row in metadata
    )
    process_deltas = [row["delta_p50_ms"] for row in rows("process_block")]
    validate_deltas = [row["delta_p50_ms"] for row in rows("validate_state")]
    report = {
        "contract": {
            "hypothesis": (
                "Changing only content from empty to the fixed one-transfer "
                "fixture raises content-sensitive work slightly but is too small "
                "to create the historical generation/validation gap."
            ),
            "unique_intervention": (
                "start the fixed 0.25 tx/s plain-transfer generator instead of "
                "starting no user-transaction generator"
            ),
            "fixed_controls": (
                "same nitch geth binary per pair, n/t=8/5, one miner, one full "
                "observer, full-mesh topology, cache=128MiB, one mining thread, "
                "20ms loopback netem, 240s measurement"
            ),
            "pass_criterion": (
                "two independent lifecycle pairs, exact content strata, "
                "same-direction Process/ValidateState increase, and headline "
                "content delta below the historical same-event gap"
            ),
        },
        "inputs": pair_inputs,
        "metadata": metadata,
        "comparisons": pairs,
        "decisions": {
            "metadata_exact": metadata_exact,
            "content_sensitive_directions_repeat": directions,
            "headline_content_effect_below_historical_gap": headline_not_explained,
            "process_p50_delta_ms_range": [
                min(process_deltas), max(process_deltas)
            ],
            "validate_state_p50_delta_ms_range": [
                min(validate_deltas), max(validate_deltas)
            ],
            "content_mixing_sufficient_for_historical_anomaly": not all(
                headline_not_explained.values()
            ),
        },
    }
    report["all_primary_passed"] = (
        metadata_exact
        and all(directions.values())
        and all(headline_not_explained.values())
    )

    (args.output / "a3-analysis.json").write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n"
    )
    with (args.output / "a3-comparison.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(pairs[0]))
        writer.writeheader()
        writer.writerows(pairs)
    (args.output / "a3-block-metadata.json").write_text(
        json.dumps(metadata, indent=2, sort_keys=True) + "\n"
    )
    manifest = {
        "schema": 1,
        "created_at_utc": datetime.now(timezone.utc).isoformat().replace(
            "+00:00", "Z"
        ),
        "card_id": "A3",
        "variant": "nitch-independent-empty-vs-loaded-one-transfer",
        "command": [sys.executable, str(Path(__file__).resolve()), *sys.argv[1:]],
        "n_t": "8/5",
        "node_topology": "8-node full mesh; node1 miner, node2 full observer",
        "sgx_hw_state": "not applicable to nitch; tgovern host mode is SGX SIM",
        "cpu_numa": "unbound control; identical policy across pairs",
        "netem_delay_ms": 20,
        "workload": {
            "control": "no user transactions for 240s",
            "intervention": "0.25/s plain transfer, value=1, gas=21000",
        },
        "fixed_controls": report["contract"]["fixed_controls"],
        "repositories_and_binaries": [
            {
                "pair": item["pair"],
                "empty_repositories": item["empty_manifest_pre"]["repositories"],
                "loaded_repositories": item["loaded_manifest_pre"]["repositories"],
                "binary": item["empty_manifest_pre"]["binaries"]["geth"],
            }
            for item in pair_inputs
        ],
        "block_metadata_file": "a3-block-metadata.json",
        "raw_sample_files": [
            str(
                (
                    args.empty_root
                    / f"run-{run:03d}"
                    / "analysis"
                    / "block-event-samples.csv"
                ).resolve()
            )
            for run in (1, 2)
        ]
        + [
            str(
                (
                    args.loaded_root
                    / f"run-{run:03d}"
                    / "analysis"
                    / "block-event-samples.csv"
                ).resolve()
            )
            for run in (1, 2)
        ],
        "result": report["decisions"],
        "functional_correctness": [
            {
                "pair": item["pair"],
                "empty": item["empty_run_manifest"]["functional_checks"],
                "loaded": item["loaded_run_manifest"]["functional_checks"],
            }
            for item in pair_inputs
        ],
    }
    (args.output / "manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    )
    print(args.output)
    return 0 if report["all_primary_passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
