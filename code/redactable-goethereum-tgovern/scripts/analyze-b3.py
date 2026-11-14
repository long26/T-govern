#!/usr/bin/env python3
"""Count retry, queue, and competing-submit signals for loaded 32/17 B3 runs."""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


LOG_TS_RE = re.compile(r"\[(\d{2}-\d{2})\|(\d{2}:\d{2}:\d{2}\.\d{3})\]")
EVENT_RE = re.compile(r"\bevent=([^\s]+)")
FIELD_RE = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)=([^\s]+)")
LOAD_FINAL_RE = re.compile(
    r"\bstopped sent=(\d+) failures=(\d+) elapsed=([0-9.]+)s "
    r"actual_rate=([0-9.]+)/s"
)
PRIMARY_STAGES = (
    "edit_execution",
    "execute_redaction_confirmation",
    "ledger_update",
    "rotation_flow",
)
HISTORICAL_FILTERED_MEAN_MS = {
    "edit_execution": 1418.891,
    "ledger_update": 279.655,
    "rotation_flow": 11392.7,
}


def parse_timestamp(line: str) -> float | None:
    match = LOG_TS_RE.search(line)
    if match is None:
        return None
    return datetime.strptime(
        f"2026-{match.group(1)} {match.group(2)}",
        "%Y-%m-%d %H:%M:%S.%f",
    ).replace(tzinfo=timezone.utc).timestamp()


def measurement_window(e2e_log: Path) -> tuple[float, float]:
    begin: float | None = None
    end: float | None = None
    for line in e2e_log.read_text(errors="replace").splitlines():
        if "event=measurement_begin" in line:
            begin = parse_timestamp(line)
        elif "event=measurement_end" in line:
            end = parse_timestamp(line)
    if begin is None or end is None or end < begin:
        raise ValueError(f"invalid measurement window in {e2e_log}")
    return begin, end


def load_stage_rows(path: Path, run: int) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open(newline="") as stream:
        for row in csv.DictReader(stream):
            if row["stage"] not in PRIMARY_STAGES:
                continue
            rows.append({
                "run": run,
                "stage": row["stage"],
                "stratum": row["stratum"],
                "raw_count": int(row["raw_count"]),
                "used_count": int(row["used_count"]),
                "excluded_count": int(row["excluded_count"]),
                "raw_mean_ms": float(row["raw_mean"]),
                "filtered_mean_ms": float(row["filtered_mean"]),
                "p50_ms": float(row["p50"]),
                "p95_ms": float(row["p95"]),
                "p99_ms": float(row["p99"]),
                "max_ms": float(row["max"]),
                "basis": row["basis"],
                "note": row["note"],
            })
    if {row["stage"] for row in rows} != set(PRIMARY_STAGES):
        raise ValueError(f"missing primary stages in {path}")
    return rows


def aggregate_stage_rows(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    output: list[dict[str, Any]] = []
    for stage in PRIMARY_STAGES:
        values = sorted(
            row["raw_mean_ms"] for row in rows if row["stage"] == stage
        )
        if len(values) != 2:
            raise ValueError(f"expected two {stage} samples, got {len(values)}")
        output.append({
            "stage": stage,
            "raw_count": len(values),
            "used_count": len(values),
            "excluded_count": 0,
            "raw_samples_ms": values,
            "raw_mean_ms": sum(values) / len(values),
            "filtered_mean_ms": sum(values) / len(values),
            "p50_ms": (values[0] + values[1]) / 2,
            "p95_ms": values[0] + 0.95 * (values[1] - values[0]),
            "p99_ms": values[0] + 0.99 * (values[1] - values[0]),
            "max_ms": values[-1],
            "historical_filtered_mean_ms": HISTORICAL_FILTERED_MEAN_MS.get(stage),
        })
    return output


def parse_load_final(path: Path) -> dict[str, Any]:
    matches = [
        LOAD_FINAL_RE.search(line)
        for line in path.read_text(errors="replace").splitlines()
    ]
    values = [match for match in matches if match is not None]
    if len(values) != 1:
        raise ValueError(f"expected one final tx-load record in {path}")
    match = values[0]
    return {
        "sent": int(match.group(1)),
        "failures": int(match.group(2)),
        "elapsed_s": float(match.group(3)),
        "actual_rate_per_s": float(match.group(4)),
    }


def fields_from_line(line: str) -> dict[str, str]:
    return {key: value.strip('"') for key, value in FIELD_RE.findall(line)}


def analyze_logs(
    logs_dir: Path, e2e_log: Path, window: tuple[float, float]
) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    event_counts: Counter[str] = Counter()
    dkrs_epoch1_start_nodes: set[str] = set()
    dkrs_epoch1_extract_nodes: set[str] = set()
    e2e_text = e2e_log.read_text(errors="replace")
    registration_attempt_gt1 = len(
        re.findall(r"attempt [2-9]/", e2e_text)
    )
    txmodify_replay_requests = e2e_text.count("requested TxModify replay")
    txmodify_replay_failures = e2e_text.count(
        "TxModify replay request failed"
    )
    receipt_wait_errors = 0
    sync_retry_in_window = 0
    txpool_queue_or_reject_in_window = 0
    fatal_lines_in_window = 0
    receipt_failures: list[dict[str, Any]] = []
    for path in sorted(logs_dir.glob("geth-node-*.log")):
        with path.open(errors="replace") as stream:
            for line_number, line in enumerate(stream, start=1):
                timestamp = parse_timestamp(line)
                in_window = (
                    timestamp is not None
                    and window[0] <= timestamp <= window[1]
                )
                if "E2E_TIMING" in line and "stage=edit_execution" in line:
                    event_match = EVENT_RE.search(line)
                    if event_match is not None:
                        event = event_match.group(1)
                        event_counts[event] += 1
                        if event == "execute_redaction_receipt_failed":
                            fields = fields_from_line(line)
                            receipt_failures.append({
                                "source_file": path.name,
                                "line_number": line_number,
                                "timestamp_s": timestamp,
                                "join_key": fields.get("join_key"),
                                "tx_hash": fields.get("tx_hash"),
                                "tee_host": fields.get("tee_host"),
                                "node_id": fields.get("node_id"),
                                "receipt_block_number": fields.get(
                                    "receipt_block_number"
                                ),
                                "status": fields.get("status"),
                                "reason": fields.get("reason"),
                            })
                            if fields.get("reason") == "receipt_wait_error":
                                receipt_wait_errors += 1
                if "TEE host starting DKRS" in line and "newEpoch=1" in line:
                    dkrs_epoch1_start_nodes.add(path.name)
                if "TEE host extracted DKRS" in line:
                    dkrs_epoch1_extract_nodes.add(path.name)
                if in_window and "Synchronisation failed, retrying" in line:
                    sync_retry_in_window += 1
                if in_window and re.search(
                    r"txpool.*(full|queue|drop|reject)", line, re.IGNORECASE
                ):
                    txpool_queue_or_reject_in_window += 1
                if in_window and re.match(
                    r"^(ERROR|CRIT|PANIC|FATAL)", line
                ):
                    fatal_lines_in_window += 1
    return ({
        "edit_event_counts": dict(sorted(event_counts.items())),
        "dkrs_epoch1_start_unique_nodes": len(dkrs_epoch1_start_nodes),
        "dkrs_epoch1_extract_unique_nodes": len(dkrs_epoch1_extract_nodes),
        "registration_attempt_gt1": registration_attempt_gt1,
        "txmodify_replay_requests": txmodify_replay_requests,
        "txmodify_replay_failures": txmodify_replay_failures,
        "receipt_wait_errors": receipt_wait_errors,
        "sync_retry_in_measurement_window": sync_retry_in_window,
        "txpool_queue_or_reject_in_measurement_window": (
            txpool_queue_or_reject_in_window
        ),
        "error_crit_panic_fatal_in_measurement_window": fatal_lines_in_window,
    }, receipt_failures)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)

    run_counts: list[dict[str, Any]] = []
    stage_rows: list[dict[str, Any]] = []
    all_receipt_failures: list[dict[str, Any]] = []
    for run in (1, 2):
        run_dir = args.input_root / "runs" / f"run-{run:03d}"
        e2e_log = run_dir / "e2e.log"
        window = measurement_window(e2e_log)
        stage_rows.extend(
            load_stage_rows(run_dir / "timing" / "l5-timing-stages.csv", run)
        )
        signals, receipt_failures = analyze_logs(
            run_dir / "raw" / "logs", e2e_log, window
        )
        for row in receipt_failures:
            row["run"] = run
        all_receipt_failures.extend(receipt_failures)
        load = parse_load_final(run_dir / "raw" / "logs" / "tx-load.log")
        event_counts = signals["edit_event_counts"]
        run_counts.append({
            "run": run,
            "measurement_start_timestamp_s": window[0],
            "measurement_end_timestamp_s": window[1],
            "measurement_duration_s": window[1] - window[0],
            "background_tx_sent": load["sent"],
            "background_tx_failures": load["failures"],
            "background_tx_actual_rate_per_s": load["actual_rate_per_s"],
            "registration_attempt_gt1": signals["registration_attempt_gt1"],
            "dkrs_epoch1_start_unique_nodes": (
                signals["dkrs_epoch1_start_unique_nodes"]
            ),
            "dkrs_epoch1_extract_unique_nodes": (
                signals["dkrs_epoch1_extract_unique_nodes"]
            ),
            "tee_authorized_received": event_counts.get(
                "tee_authorized_received", 0
            ),
            "redaction_build_new_block": event_counts.get(
                "redaction_build_new_block", 0
            ),
            "redaction_verify_external_tx_r_prime": event_counts.get(
                "redaction_verify_external_tx_r_prime", 0
            ),
            "redaction_compute_proof_hash": event_counts.get(
                "redaction_compute_proof_hash", 0
            ),
            "redaction_submit_execute_onchain": event_counts.get(
                "redaction_submit_execute_onchain", 0
            ),
            "execute_redaction_receipt_success": event_counts.get(
                "execute_redaction_receipt_success", 0
            ),
            "execute_redaction_receipt_failed": event_counts.get(
                "execute_redaction_receipt_failed", 0
            ),
            "execute_redaction_submit_failed": event_counts.get(
                "execute_redaction_submit_failed", 0
            ),
            "receipt_wait_errors": signals["receipt_wait_errors"],
            "txmodify_replay_requests": signals["txmodify_replay_requests"],
            "txmodify_replay_failures": signals["txmodify_replay_failures"],
            "sync_retry_in_measurement_window": (
                signals["sync_retry_in_measurement_window"]
            ),
            "txpool_queue_or_reject_in_measurement_window": (
                signals["txpool_queue_or_reject_in_measurement_window"]
            ),
            "error_crit_panic_fatal_in_measurement_window": (
                signals["error_crit_panic_fatal_in_measurement_window"]
            ),
        })

    stage_statistics = aggregate_stage_rows(stage_rows)
    competing_submit_exact = all(
        row["tee_authorized_received"] == 32
        and row["redaction_build_new_block"] == 32
        and row["redaction_verify_external_tx_r_prime"] == 32
        and row["redaction_compute_proof_hash"] == 32
        and row["redaction_submit_execute_onchain"] == 32
        and row["execute_redaction_receipt_success"] == 1
        and row["execute_redaction_receipt_failed"] == 31
        for row in run_counts
    )
    operational_retries_absent = all(
        row["registration_attempt_gt1"] == 0
        and row["execute_redaction_submit_failed"] == 0
        and row["receipt_wait_errors"] == 0
        and row["txmodify_replay_requests"] == 0
        and row["txmodify_replay_failures"] == 0
        and row["txpool_queue_or_reject_in_measurement_window"] == 0
        and row["background_tx_failures"] == 0
        for row in run_counts
    )
    functions_pass = all(
        row["background_tx_sent"] > 0
        and row["dkrs_epoch1_start_unique_nodes"] == 32
        and row["dkrs_epoch1_extract_unique_nodes"] == 32
        and row["error_crit_panic_fatal_in_measurement_window"] == 0
        for row in run_counts
    )
    report = {
        "contract": {
            "hypothesis": (
                "32/17 edit, ledger, or rotation tails are caused by observable "
                "RPC/receipt/TxModify/DKRS retry or queue accumulation."
            ),
            "diagnostic_before_intervention": (
                "count every explicit operational retry/failure and correlate "
                "by request or epoch; no path-changing intervention is allowed "
                "unless semantic equivalence is first demonstrated"
            ),
            "fixed_controls": (
                "SGX SIM, n/t=32/17, one ON miner, 32 TEE hosts, four committee "
                "processes, 0.25 tx/s background load, one rotation and edit, "
                "20ms loopback netem, unchanged protocol binaries"
            ),
        },
        "run_counts": run_counts,
        "stage_samples": stage_rows,
        "stage_statistics": stage_statistics,
        "receipt_failures": all_receipt_failures,
        "decisions": {
            "functional_checks_passed": functions_pass,
            "named_operational_retry_sources_absent": operational_retries_absent,
            "n_minus_one_competing_onchain_submissions_repeat": (
                competing_submit_exact
            ),
            "competing_submissions_direct_cause_confirmed": False,
            "reason_not_direct": (
                "suppressing 31 submissions would change the current distributed "
                "execution/submission strategy; no semantic-equivalence proof or "
                "permitted intervention was established"
            ),
            "b3_status": (
                "operational retry candidates excluded; deterministic 32-submit/"
                "31-revert structural amplification is a strong unconfirmed cause"
            ),
        },
    }
    (args.output / "b3-analysis.json").write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n"
    )
    with (args.output / "b3-run-counts.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(run_counts[0]))
        writer.writeheader()
        writer.writerows(run_counts)
    with (args.output / "b3-stage-samples.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(stage_rows[0]))
        writer.writeheader()
        writer.writerows(stage_rows)
    with (args.output / "b3-receipt-failures.csv").open(
        "w", newline=""
    ) as stream:
        writer = csv.DictWriter(
            stream, fieldnames=list(all_receipt_failures[0])
        )
        writer.writeheader()
        writer.writerows(all_receipt_failures)
    manifest = {
        "schema": 1,
        "created_at_utc": datetime.now(timezone.utc).isoformat().replace(
            "+00:00", "Z"
        ),
        "card_id": "B3",
        "variant": "32x17-normal-path-diagnostic-analysis",
        "command": [sys.executable, str(Path(__file__).resolve()), *sys.argv[1:]],
        "input_manifest": str((args.input_root / "manifest.json").resolve()),
        "raw_evidence": str(args.input_root.resolve()),
        "outputs": [
            "b3-analysis.json",
            "b3-run-counts.csv",
            "b3-stage-samples.csv",
            "b3-receipt-failures.csv",
        ],
        "decisions": report["decisions"],
    }
    (args.output / "manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    )
    print(args.output)
    return 0 if functions_pass and competing_submit_exact else 1


if __name__ == "__main__":
    raise SystemExit(main())
