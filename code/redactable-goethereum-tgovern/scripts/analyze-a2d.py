#!/usr/bin/env python3
"""Analyze the predeclared A2D loaded validation subphase experiment."""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
from pathlib import Path
from statistics import mean
from typing import Any


FIELD_RE = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)=(\"[^\"]*\"|\S+)")
CORE_EVENTS = (
    "insert_chain_total",
    "verify_headers_first",
    "verify_header_result_wait",
    "validate_body",
    "process_block",
    "validate_state",
    "write_block_with_state",
    "write_block_subphases",
)
HEADER_FIELDS = (
    "duration_ns",
    "parent_lookup_ns",
    "incoming_header_hash_ns",
    "known_header_lookup_ns",
    "rules_and_seal_ns",
)
WRITE_FIELDS = (
    "duration_ns",
    "td_lookup_ns",
    "batch_build_ns",
    "batch_write_ns",
    "state_commit_ns",
    "trie_maintenance_ns",
    "canonical_head_ns",
    "event_dispatch_ns",
)


def percentile(values: list[float], q: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    position = (len(ordered) - 1) * q
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    return ordered[lower] + (ordered[upper] - ordered[lower]) * (position - lower)


def summarize(values: list[float]) -> dict[str, Any]:
    return {
        "raw_count": len(values),
        "raw_mean_ms": mean(values) if values else None,
        "p50_ms": percentile(values, 0.50),
        "p95_ms": percentile(values, 0.95),
        "p99_ms": percentile(values, 0.99),
        "max_ms": max(values) if values else None,
    }


def load_samples(path: Path) -> tuple[list[dict[str, str]], set[tuple[str, str]]]:
    with path.open(newline="") as stream:
        rows = list(csv.DictReader(stream))
    targets = {
        (row["block_number"], row["logged_block_hash"])
        for row in rows
        if row["event"] == "write_block_with_state"
        and row["stratum"] == "one_plain_transfer"
        and not row["excluded_reason"]
    }
    if not targets:
        raise ValueError(f"{path}: no exact one_plain_transfer validation targets")
    return rows, targets


def core_summaries(rows: list[dict[str, str]]) -> dict[str, dict[str, Any]]:
    output: dict[str, dict[str, Any]] = {}
    for event in CORE_EVENTS:
        values = [
            float(row["duration_ms"])
            for row in rows
            if row["event"] == event
            and row["stratum"] == "one_plain_transfer"
            and not row["excluded_reason"]
        ]
        output[event] = summarize(values)
    return output


def parse_fields(line: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    for key, raw in FIELD_RE.findall(line):
        fields[key] = raw[1:-1] if raw.startswith('"') and raw.endswith('"') else raw
    return fields


def aggregate_subphases(
    log_path: Path,
    targets: set[tuple[str, str]],
    event: str,
    names: tuple[str, ...],
) -> dict[str, dict[str, Any]]:
    values: dict[str, list[float]] = {name: [] for name in names}
    matched: set[tuple[str, str]] = set()
    for line in log_path.read_text(errors="replace").splitlines():
        if f"event={event}" not in line:
            continue
        fields = parse_fields(line)
        key = (fields.get("block", ""), fields.get("block_hash", ""))
        if key not in targets:
            continue
        matched.add(key)
        for name in names:
            if name not in fields:
                raise ValueError(f"{log_path}: {event} missing {name}: {line}")
            values[name].append(int(fields[name]) / 1_000_000)
    if matched != targets:
        missing = sorted(targets - matched)
        raise ValueError(
            f"{log_path}: {event} matched {len(matched)}/{len(targets)} targets; "
            f"missing={missing[:5]}"
        )
    return {name: summarize(samples) for name, samples in values.items()}


def analyze_run(
    implementation: str,
    run: int,
    sample_csv: Path,
    observer_log: Path,
) -> dict[str, Any]:
    rows, targets = load_samples(sample_csv)
    return {
        "implementation": implementation,
        "run": run,
        "sample_csv": str(sample_csv),
        "observer_log": str(observer_log),
        "target_count": len(targets),
        "targets": [
            {"block": int(number), "logged_block_hash": block_hash}
            for number, block_hash in sorted(targets, key=lambda item: int(item[0]))
        ],
        "core": core_summaries(rows),
        "header_worker": aggregate_subphases(
            observer_log, targets, "verify_header_worker_subphases", HEADER_FIELDS
        ),
        "write": aggregate_subphases(
            observer_log, targets, "write_block_subphases", WRITE_FIELDS
        ),
    }


def p50(run: dict[str, Any], group: str, metric: str) -> float:
    value = run[group][metric]["p50_ms"]
    if value is None:
        raise ValueError(f"missing p50: {run['implementation']} run {run['run']} {group}/{metric}")
    return float(value)


def comparison(nitch: dict[str, Any], tgovern: dict[str, Any]) -> dict[str, Any]:
    header_gap = p50(nitch, "core", "verify_header_result_wait") - p50(
        tgovern, "core", "verify_header_result_wait"
    )
    write_gap = p50(nitch, "core", "write_block_with_state") - p50(
        tgovern, "core", "write_block_with_state"
    )
    header: dict[str, Any] = {}
    for metric in HEADER_FIELDS:
        gap = p50(nitch, "header_worker", metric) - p50(tgovern, "header_worker", metric)
        header[metric] = {
            "nitch_p50_ms": p50(nitch, "header_worker", metric),
            "tgovern_p50_ms": p50(tgovern, "header_worker", metric),
            "gap_ms": gap,
            "header_wait_gap_coverage_percent": 100 * gap / header_gap if header_gap else None,
        }
    write: dict[str, Any] = {}
    for metric in WRITE_FIELDS:
        gap = p50(nitch, "write", metric) - p50(tgovern, "write", metric)
        write[metric] = {
            "nitch_p50_ms": p50(nitch, "write", metric),
            "tgovern_p50_ms": p50(tgovern, "write", metric),
            "gap_ms": gap,
            "write_gap_coverage_percent": 100 * gap / write_gap if write_gap else None,
        }
    return {
        "pair": nitch["run"],
        "header_wait_gap_ms": header_gap,
        "write_gap_ms": write_gap,
        "header": header,
        "write": write,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--nitch-root", type=Path, required=True)
    parser.add_argument("--tgovern-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    runs: list[dict[str, Any]] = []
    for run in (1, 2):
        nitch_dir = args.nitch_root / f"run-{run:03d}"
        runs.append(
            analyze_run(
                "nitch",
                run,
                nitch_dir / "analysis-v2" / "block-event-samples.csv",
                nitch_dir / "logs" / "geth-node-2.log",
            )
        )
        tgovern_dir = args.tgovern_root / "runs" / f"run-{run:03d}" / "raw"
        runs.append(
            analyze_run(
                "tgovern",
                run,
                tgovern_dir / "block-analysis-v2" / "block-event-samples.csv",
                tgovern_dir / "logs" / "geth-node-2.log",
            )
        )

    by_key = {(run["implementation"], run["run"]): run for run in runs}
    comparisons = [
        comparison(by_key[("nitch", run)], by_key[("tgovern", run)])
        for run in (1, 2)
    ]
    hash_pass = all(
        item["header"]["incoming_header_hash_ns"]["gap_ms"] >= 1
        and item["header"]["incoming_header_hash_ns"]["header_wait_gap_coverage_percent"] >= 50
        for item in comparisons
    )
    write_candidates = {
        metric: all(
            item["write"][metric]["gap_ms"] >= 1
            and item["write"][metric]["write_gap_coverage_percent"] >= 50
            for item in comparisons
        )
        for metric in WRITE_FIELDS[1:]
    }
    report = {
        "contract": {
            "header_hash_pass": "both p50 pairs: gap >=1ms and >=50% of header-result-wait gap",
            "write_subphase_pass": "same single subphase in both p50 pairs: gap >=1ms and >=50% of writeBlockWithState gap",
            "content_stratum": "txs=1,input_bytes=0,gas_used=21000 canonical observer blocks",
        },
        "runs": runs,
        "comparisons": comparisons,
        "decisions": {
            "A2D-H1-incoming-header-hash-main-candidate": "confirmed-diagnostic"
            if hash_pass
            else "rejected-as-main-candidate",
            "A2D-H2-single-write-subphase-main-candidate": [
                metric for metric, passed in write_candidates.items() if passed
            ]
            or "no-single-subphase-passed",
        },
        "write_candidate_pass": write_candidates,
    }
    args.output.mkdir(parents=True, exist_ok=False)
    (args.output / "a2d-analysis.json").write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n"
    )

    rows: list[dict[str, Any]] = []
    for item in comparisons:
        for group, metrics in (("header", item["header"]), ("write", item["write"])):
            for metric, values in metrics.items():
                row = {"pair": item["pair"], "group": group, "metric": metric}
                row.update(values)
                rows.append(row)
    with (args.output / "a2d-comparison.csv").open("w", newline="") as stream:
        fieldnames = [
            "pair",
            "group",
            "metric",
            "nitch_p50_ms",
            "tgovern_p50_ms",
            "gap_ms",
            "header_wait_gap_coverage_percent",
            "write_gap_coverage_percent",
        ]
        writer = csv.DictWriter(stream, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)
    print(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
