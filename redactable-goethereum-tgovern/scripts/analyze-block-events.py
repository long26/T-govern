#!/usr/bin/env python3
"""Extract content-stratified block generation/validation timing samples."""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import shlex
from collections import defaultdict
from datetime import datetime
from pathlib import Path
from typing import Any


GETH_TS_RE = re.compile(r"\[(\d{2}-\d{2})\|(\d{2}:\d{2}:\d{2}\.\d{3})\]")
PROGRESS_TS_RE = re.compile(
    r"\[sgx-l5\] \[(\d{2}-\d{2}\|\d{2}:\d{2}:\d{2}\.\d{3})\].*"
)
BLOCK_EVENTS = {
    "miner_build_preamble",
    "miner_commit_state_copy",
    "miner_engine_header_prepare",
    "miner_finalize_assemble_block",
    "miner_future_timestamp_wait",
    "miner_full_task_hook",
    "miner_header_context_prepare",
    "miner_make_current",
    "miner_make_current_ancestor_hash_cached",
    "miner_make_current_ancestor_hash_first",
    "miner_make_current_ancestor_lookup",
    "miner_make_current_env_alloc",
    "miner_make_current_parent_hash",
    "miner_make_current_set_add",
    "miner_make_current_state_at",
    "miner_make_current_uncle_hash",
    "miner_task_send",
    "miner_task_queued_for_sealing",
    "miner_task_queued_from_aligned_anchor",
    "miner_txpool_pending",
    "miner_uncle_context_prepare",
    "miner_sealing_queue_wait",
    "process_block",
    "validate_state",
    "validate_body",
    "verify_header_result_wait",
    "verify_header_worker_subphases",
    "write_block_with_state",
    "write_block_subphases",
    "insert_chain_total",
    "insert_chain_internal_total",
    "verify_headers_first",
}
GENERATION_EVENTS = {
    "miner_apply_tx",
    "miner_build_preamble",
    "miner_commit_state_copy",
    "miner_engine_header_prepare",
    "miner_finalize_assemble_block",
    "miner_future_timestamp_wait",
    "miner_full_task_hook",
    "miner_header_context_prepare",
    "miner_make_current",
    "miner_make_current_ancestor_hash_cached",
    "miner_make_current_ancestor_hash_first",
    "miner_make_current_ancestor_lookup",
    "miner_make_current_env_alloc",
    "miner_make_current_parent_hash",
    "miner_make_current_set_add",
    "miner_make_current_state_at",
    "miner_make_current_uncle_hash",
    "miner_task_send",
    "miner_task_queued_for_sealing",
    "miner_task_queued_from_aligned_anchor",
    "miner_txpool_pending",
    "miner_uncle_context_prepare",
    "miner_sealing_queue_wait",
}
VALIDATION_EVENTS = {
    "process_block",
    "validate_state",
    "validate_body",
    "verify_header_result_wait",
    "verify_header_worker_subphases",
    "write_block_with_state",
    "write_block_subphases",
    "insert_chain_total",
    "insert_chain_internal_total",
    "verify_headers_first",
}


def parse_value(raw: str) -> Any:
    if raw in {"nil", "<nil>", ""}:
        return None
    if raw in {"true", "false"}:
        return raw == "true"
    try:
        return int(raw)
    except ValueError:
        return raw


def key_values(message: str) -> dict[str, Any]:
    try:
        tokens = shlex.split(message)
    except ValueError:
        tokens = message.split()
    values: dict[str, Any] = {}
    for token in tokens:
        if "=" in token:
            key, value = token.split("=", 1)
            values[key] = parse_value(value)
    return values


def timestamp_s(line: str, year: int) -> float | None:
    match = GETH_TS_RE.search(line)
    if not match:
        return None
    return datetime.strptime(
        f"{year}-{match.group(1)} {match.group(2)}",
        "%Y-%m-%d %H:%M:%S.%f",
    ).timestamp()


def measurement_window(progress: Path, year: int) -> tuple[float, float]:
    events: dict[str, float] = {}
    for line in progress.read_text(errors="replace").splitlines():
        if line.startswith("{"):
            record = json.loads(line)
            event = record.get("event")
            if event in {"measurement_begin", "measurement_end"}:
                events[str(event)] = float(record["timestamp_s"])
            continue
        if "measurement_begin" not in line and "measurement_end" not in line:
            continue
        match = PROGRESS_TS_RE.search(line)
        if not match:
            continue
        event = "measurement_begin" if "measurement_begin" in line else "measurement_end"
        events[event] = datetime.strptime(
            f"{year}-{match.group(1)}", "%Y-%m-%d|%H:%M:%S.%f"
        ).timestamp()
    if set(events) != {"measurement_begin", "measurement_end"}:
        raise ValueError(f"missing measurement anchors in {progress}: {events}")
    if events["measurement_end"] < events["measurement_begin"]:
        raise ValueError(f"invalid measurement window in {progress}: {events}")
    return events["measurement_begin"], events["measurement_end"]


def load_blocks(path: Path | None) -> dict[int, dict[str, Any]]:
    if path is None:
        return {}
    blocks: dict[int, dict[str, Any]] = {}
    for line in path.read_text().splitlines():
        record = json.loads(line)
        blocks[int(record["number"])] = record
    return blocks


def percentile(values: list[float], pct: float) -> float:
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    rank = (len(ordered) - 1) * pct
    low, high = math.floor(rank), math.ceil(rank)
    if low == high:
        return ordered[low]
    return ordered[low] + (ordered[high] - ordered[low]) * (rank - low)


def far_outlier_bounds(values: list[float]) -> tuple[float, float] | None:
    if len(values) < 30:
        return None
    q1, q3 = percentile(values, 0.25), percentile(values, 0.75)
    iqr = q3 - q1
    return q1 - 3 * iqr, q3 + 3 * iqr


def status_ok(record: dict[str, Any]) -> bool:
    status = record.get("status")
    return status in {None, "ok", 1, "1"}


def block_number(record: dict[str, Any]) -> int | None:
    for key in ("block", "number", "block_number"):
        try:
            return int(record[key])
        except (KeyError, TypeError, ValueError):
            continue
    return None


def stratum(record: dict[str, Any]) -> str:
    if record["event"] == "miner_apply_tx":
        return "transaction"
    try:
        txs = int(record.get("txs", 0))
        gas_used = int(record.get("gas_used", 0))
        if txs == 0:
            return "empty"
        if txs == 1 and gas_used == 21000:
            return "one_plain_transfer"
        return "nonempty_other"
    except (TypeError, ValueError):
        return "unknown"


def content_key(record: dict[str, Any]) -> str:
    if record["event"] == "miner_apply_tx":
        return (
            f"input={record.get('tx_input_bytes', 'na')};"
            f"gas_limit={record.get('tx_gas_limit', 'na')};"
            f"gas_used={record.get('gas_used', 'na')}"
        )
    return (
        f"txs={record.get('txs', 'na')};"
        f"gas={record.get('gas_used', 'na')};"
        f"bytes={record.get('block_bytes', 'na')}"
    )


def extract(
    logs_dir: Path,
    generation_source: str,
    validation_source: str,
    window: tuple[float, float],
    year: int,
    implementation: str,
    blocks: dict[int, dict[str, Any]],
) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    tx_occurrences: dict[tuple[str, str], int] = defaultdict(int)
    selected = {generation_source, validation_source}
    for path in sorted(logs_dir.glob("geth-node-*.log")):
        if path.name not in selected:
            continue
        role = "miner" if path.name == generation_source else "observer"
        with path.open(errors="replace") as fp:
            for line_number, line in enumerate(fp, start=1):
                if "E2E_TIMING" not in line:
                    continue
                ts = timestamp_s(line, year)
                if ts is None or not window[0] <= ts <= window[1]:
                    continue
                fields = key_values(line.split("E2E_TIMING", 1)[1])
                event = str(fields.get("event", ""))
                if (
                    event not in GENERATION_EVENTS
                    and event not in VALIDATION_EVENTS
                ):
                    continue
                if event in GENERATION_EVENTS and role != "miner":
                    continue
                if event in VALIDATION_EVENTS and role != "observer":
                    continue
                try:
                    duration_ns = int(fields["duration_ns"])
                except (KeyError, TypeError, ValueError):
                    continue
                number = block_number(fields)
                canonical = blocks.get(number or -1)
                record = {
                    "implementation": implementation,
                    "source_file": path.name,
                    "line_number": line_number,
                    "timestamp_s": ts,
                    "role": role,
                    "stage": fields.get("stage"),
                    "event": event,
                    "duration_ns": duration_ns,
                    "duration_ms": duration_ns / 1_000_000,
                    "status": fields.get("status"),
                    "block_number": number,
                    "logged_block_hash": fields.get(
                        "block_hash", fields.get("block")
                    ),
                    "tx_hash": fields.get("tx"),
                    "txs": fields.get("txs"),
                    "gas_used": fields.get("gas_used"),
                    "block_bytes": fields.get("block_bytes"),
                    "tx_input_bytes": fields.get("tx_input_bytes"),
                    "tx_gas_limit": fields.get("tx_gas_limit"),
                    "ancestor_count": fields.get("ancestor_count"),
                    "uncle_count": fields.get("uncle_count"),
                    "pending_accounts": fields.get("pending_accounts"),
                    "canonical_block_hash": (
                        canonical.get("hash") if canonical else None
                    ),
                    "canonical_txs": (
                        canonical.get("tx_count") if canonical else None
                    ),
                    "canonical_gas_used": (
                        canonical.get("gas_used") if canonical else None
                    ),
                    "canonical_block_bytes": (
                        canonical.get("block_bytes") if canonical else None
                    ),
                }
                record["stratum"] = stratum(record)
                if event == "miner_apply_tx":
                    tx_key = (path.name, str(record.get("tx_hash")))
                    tx_occurrences[tx_key] += 1
                    record["tx_occurrence"] = tx_occurrences[tx_key]
                    occurrence = (
                        "transaction_first"
                        if tx_occurrences[tx_key] == 1
                        else "transaction_repeat"
                    )
                    try:
                        plain = (
                            int(record.get("tx_input_bytes")) == 0
                            and int(record.get("tx_gas_limit")) == 21000
                            and int(record.get("gas_used")) == 21000
                        )
                    except (TypeError, ValueError):
                        plain = False
                    record["stratum"] = (
                        occurrence + "_plain_transfer"
                        if plain else occurrence + "_other"
                    )
                else:
                    record["tx_occurrence"] = None
                record["content_key"] = content_key(record)
                record["excluded_reason"] = (
                    "" if status_ok(record) else "non_success_status"
                )
                if canonical and event in VALIDATION_EVENTS:
                    mismatches = []
                    for logged_key, canonical_key in (
                        ("txs", "tx_count"),
                        ("gas_used", "gas_used"),
                        ("block_bytes", "block_bytes"),
                    ):
                        logged = record.get(logged_key)
                        expected = canonical.get(canonical_key)
                        if logged is not None and int(logged) != int(expected):
                            mismatches.append(
                                f"{logged_key}:{logged}!={expected}"
                            )
                    if mismatches:
                        record["excluded_reason"] = "canonical_metadata_mismatch:" + ",".join(mismatches)
                records.append(record)
    return records


def summaries(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    grouped: dict[tuple[str, str, str, str], list[dict[str, Any]]] = defaultdict(list)
    for record in records:
        grouped[
            (
                str(record["implementation"]),
                str(record["role"]),
                str(record["event"]),
                str(record["stratum"]),
            )
        ].append(record)
    rows: list[dict[str, Any]] = []
    for (implementation, role, event, sample_stratum), group in sorted(grouped.items()):
        candidates = [
            record for record in group if not record["excluded_reason"]
        ]
        raw_values = [float(record["duration_ms"]) for record in candidates]
        bounds = far_outlier_bounds(raw_values)
        used_values = list(raw_values)
        outlier_count = 0
        if bounds:
            used_values = [
                value for value in raw_values if bounds[0] <= value <= bounds[1]
            ]
            outlier_count = len(raw_values) - len(used_values)
        if not raw_values:
            continue
        rows.append({
            "implementation": implementation,
            "role": role,
            "event": event,
            "stratum": sample_stratum,
            "raw_count": len(group),
            "eligible_count": len(raw_values),
            "excluded_count": len(group) - len(raw_values),
            "statistical_outlier_count": outlier_count,
            "used_count_for_filtered_mean": len(used_values),
            "min_ms": min(raw_values),
            "raw_mean_ms": sum(raw_values) / len(raw_values),
            "filtered_mean_ms": sum(used_values) / len(used_values),
            "p50_ms": percentile(raw_values, 0.50),
            "p95_ms": percentile(raw_values, 0.95),
            "p99_ms": percentile(raw_values, 0.99),
            "max_ms": max(raw_values),
            "far_outlier_lower_ms": bounds[0] if bounds else None,
            "far_outlier_upper_ms": bounds[1] if bounds else None,
        })
    return rows


def write_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    if not rows:
        path.write_text("")
        return
    fields = list(rows[0])
    with path.open("w", newline="") as fp:
        writer = csv.DictWriter(fp, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--implementation", required=True)
    parser.add_argument("--logs-dir", required=True, type=Path)
    parser.add_argument("--progress", required=True, type=Path)
    parser.add_argument("--blocks", type=Path)
    parser.add_argument("--generation-source", default="geth-node-1.log")
    parser.add_argument("--validation-source", default="geth-node-2.log")
    parser.add_argument("--year", type=int, default=2026)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    args.output.mkdir(parents=True, exist_ok=True)
    window = measurement_window(args.progress, args.year)
    records = extract(
        args.logs_dir,
        args.generation_source,
        args.validation_source,
        window,
        args.year,
        args.implementation,
        load_blocks(args.blocks),
    )
    if not records:
        raise RuntimeError("no selected timing records in measurement window")
    summary = summaries(records)
    write_csv(args.output / "block-event-samples.csv", records)
    write_csv(args.output / "block-event-statistics.csv", summary)
    (args.output / "block-event-analysis.json").write_text(json.dumps({
        "implementation": args.implementation,
        "measurement_window": {"begin_s": window[0], "end_s": window[1]},
        "sources": {
            "generation": args.generation_source,
            "validation": args.validation_source,
        },
        "sample_count": len(records),
        "statistics": summary,
    }, indent=2, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
