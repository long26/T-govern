#!/usr/bin/env python3
"""Collect and correlate SGX L5 timing anchors.

edit_execution headline is the redaction request lifecycle: from the request
being proposed by node1 to the committee
(script committee_redact_request_submitted) to the first TEE-host that completes
the edit (aggregates the partial adaptations into the new r' and broadcasts the
modify message), i.e.
committee_redact_request_submitted -> redaction_broadcast_modify_msg.  The later
chain-gated submission of the on-chain authorization is reported separately as
execute_redaction_confirmation and does not inflate this span.

ledger_update headline is the ON node's pure-local apply cost
(lu_getblock + lu_validate + lu_update); the full tx_modify_received ->
local_block_updated anchor span, dominated by the lu_wait_visible
block-visibility wait, is reported separately as ledger_update_total.

rotation_flow headline is the epoch rotation end to end from the chain: from
the mint time of the block that triggered the epoch change
(ActiveSetRotated -> node1 trigger_block; node1 is the miner, and the time is
its seal time, NOT the later import time on a TEE host) to cluster-wide DKRS
completion (the last active TEE emits rotation_dkg_complete after applying its
new share).  Both start口径 are reported: rotation_flow uses the
ActiveSetRotated block (headline), rotation_flow_from_prep uses the earlier
proposer-side TEERotationPreparationRequested block (secondary).  The node's own
dkrs_start remains a fallback start when no on-chain trigger block is available.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import shlex
from datetime import datetime
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / ".tmp" / "sgx-l5-sim"
DEFAULT_LOGS = WORK / "logs"
DEFAULT_PROGRESS = WORK / "e2e-progress.log"
DEFAULT_OUT = WORK / "timing"

GETH_TS_RE = re.compile(r"\[(\d{2}-\d{2})\|(\d{2}:\d{2}:\d{2}\.\d{3})\]")
PROGRESS_RE = re.compile(r"\[sgx-l5\] \[(\d{2}-\d{2}\|\d{2}:\d{2}:\d{2}\.\d{3})\] \[\+(\d+)ms\] (.*)")
TX_HASH_RE = re.compile(r"^0x[0-9a-fA-F]{64}$")

# System-init segments, in the order the flow runs them. The orchestrator owns
# chain/datadir prep, node startup and contract deployment, so it never emits
# sgx_l5's single-machine anchors for those; they are optional (see below).
SYSTEM_SEGMENTS = (
    ("chain_datadir_init", "chain_init_begin", "chain_init_complete", True),
    ("node_startup", "node_startup_begin", "node_startup_complete", True),
    ("contract_deployment", "contract_deploy_begin", "contract_deployed", True),
    ("committee_dkg_pubkey_registration", "committee_dkg_begin",
     "committee_pubkey_registered", False),
    ("tee_pkch_generation", "tee_pkch_begin", "pkch_ready", False),
)
STAGE_ORDER = (
    "system_init",
    "tee_registration",
    "tee_registration_cluster",
    "block_generation",
    "block_validation",
    "edit_execution",
    "execute_redaction_confirmation",
    "ledger_update",
    "ledger_update_total",
    "rotation_flow",
    "rotation_flow_from_prep",
)


def parse_value(raw: str) -> Any:
    value = raw.strip()
    if value in ("<nil>", "nil", ""):
        return None
    if value in ("true", "false"):
        return value == "true"
    try:
        return int(value)
    except ValueError:
        return value


def normalize_join_key(value: Any) -> str | None:
    if value is None:
        return None
    key = str(value).strip().lower()
    if key.startswith("0x"):
        key = key[2:]
    return key or None


def normalize_tx_hash(value: Any) -> str | None:
    if value is None:
        return None
    tx_hash = str(value).strip()
    if not TX_HASH_RE.fullmatch(tx_hash):
        return None
    normalized = tx_hash.lower()
    if normalized == "0x" + "0" * 64:
        return None
    return normalized


def _kv_from_message(message: str) -> dict[str, Any]:
    out: dict[str, Any] = {}
    try:
        tokens = shlex.split(message)
    except ValueError:
        tokens = message.split()
    for token in tokens:
        if "=" in token:
            key, value = token.split("=", 1)
            out[key] = parse_value(value)
    return out


def parse_geth_ts(line: str, year: int) -> float | None:
    match = GETH_TS_RE.search(line)
    if not match:
        return None
    dt = datetime.strptime(f"{year}-{match.group(1)} {match.group(2)}", "%Y-%m-%d %H:%M:%S.%f")
    return dt.timestamp()


def parse_geth_timing_line(line: str, source_file: str, year: int) -> dict[str, Any] | None:
    marker = "E2E_TIMING"
    if marker not in line:
        return None
    record: dict[str, Any] = {
        "source": "geth",
        "source_file": source_file,
        "stage": "unknown",
        "event": "unknown",
    }
    ts = parse_geth_ts(line, year)
    if ts is not None:
        record["timestamp_s"] = ts
    record.update(_kv_from_message(line.split(marker, 1)[1].strip()))
    # The in-process eth executor labels the redaction request req_id, while
    # teehost timing anchors use join_key.  Treat the two identifiers as the
    # same correlation key when the canonical field is absent.
    if "join_key" not in record and "req_id" in record:
        record["join_key"] = record["req_id"]
    if "join_key" in record:
        record["join_key"] = normalize_join_key(record["join_key"])
    if "duration_ns" in record:
        try:
            record["duration_ns"] = int(record["duration_ns"])
            record["duration_ms"] = record["duration_ns"] / 1_000_000
        except (TypeError, ValueError):
            record.pop("duration_ns", None)
    # The in-process ledger observer emits elapsed_ns alongside duration_ns:
    # nanoseconds since the tx_modify_received anchor.  It is an absolute
    # offset, not a per-span duration, so keep it under its own key instead of
    # letting it masquerade as a span.
    if "elapsed_ns" in record:
        try:
            record["elapsed_ns"] = int(record["elapsed_ns"])
        except (TypeError, ValueError):
            record.pop("elapsed_ns", None)
    return record


RELEVANT_GETH_MARKERS = (
    "miner_task_queued_for_sealing",
    "insert_chain_total",
    "stage=edit_execution",
    "stage=ledger_update",
    "stage=rotation_flow",
)


def load_geth_records(logs_dir: Path, year: int) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for path in sorted(logs_dir.glob("geth-node-*.log")):
        with path.open(errors="replace") as fp:
            for line in fp:
                if "E2E_TIMING" not in line:
                    continue
                if not any(marker in line for marker in RELEVANT_GETH_MARKERS):
                    continue
                record = parse_geth_timing_line(line, path.name, year)
                if record is not None:
                    records.append(record)
    return records


def load_progress_records(path: Path, year: int) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    if not path.exists():
        return records
    with path.open(errors="replace") as fp:
        for line in fp:
            match = PROGRESS_RE.search(line)
            if not match:
                continue
            ts_text, mono_ms, message = match.groups()
            dt = datetime.strptime(f"{year}-{ts_text}", "%Y-%m-%d|%H:%M:%S.%f")
            record: dict[str, Any] = {
                "source": "script",
                "stage": "script",
                "event": "progress",
                "message": message.strip(),
                "mono_ms": int(mono_ms),
                "timestamp_s": dt.timestamp(),
            }
            if "PERF_ANCHOR" in message:
                fields = _kv_from_message(message)
                record.update(fields)
                record["stage"] = str(fields.get("stage", "unknown"))
                record["event"] = str(fields.get("event", "anchor"))
                if "join_key" in record:
                    record["join_key"] = normalize_join_key(record["join_key"])
            records.append(record)
    return records


def load_teehost_records(logs_dir: Path, year: int) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for path in sorted(logs_dir.glob("geth-node-*.log")):
        current_dkrs_epoch: int | None = None
        with path.open(errors="replace") as fp:
            for line in fp:
                if "TEE host starting DKRS" in line:
                    fields = _kv_from_message(line)
                    try:
                        current_dkrs_epoch = int(fields.get("newEpoch"))
                    except (TypeError, ValueError):
                        current_dkrs_epoch = None
                    records.append({
                        "source": "teehost",
                        "source_file": path.name,
                        "stage": "rotation_flow",
                        "event": "dkrs_start",
                        "epoch": current_dkrs_epoch,
                        "timestamp_s": parse_geth_ts(line, year),
                    })
                elif "TEE host extracted DKRS" in line:
                    fields = _kv_from_message(line)
                    records.append({
                        "source": "teehost",
                        "source_file": path.name,
                        "stage": "rotation_flow",
                        "event": "dkrs_extracted",
                        "epoch": current_dkrs_epoch,
                        "receiverIndex": fields.get("receiverIndex"),
                        "timestamp_s": parse_geth_ts(line, year),
                    })
    return records


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    values = sorted(values)
    if len(values) == 1:
        return values[0]
    rank = (len(values) - 1) * pct
    lower = math.floor(rank)
    upper = math.ceil(rank)
    if lower == upper:
        return values[int(rank)]
    return values[lower] + (values[upper] - values[lower]) * (rank - lower)


def summarize(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    groups: dict[tuple[str, str, str], list[float]] = {}
    for record in records:
        if record.get("duration_ms") is None:
            continue
        key = (
            str(record.get("source", "unknown")),
            str(record.get("stage", "unknown")),
            str(record.get("event", "unknown")),
        )
        groups.setdefault(key, []).append(float(record["duration_ms"]))
    rows: list[dict[str, Any]] = []
    for (source, stage, event), values in sorted(groups.items()):
        rows.append({
            "source": source,
            "stage": stage,
            "event": event,
            "count": len(values),
            "min_ms": round(min(values), 3),
            "mean_ms": round(sum(values) / len(values), 3),
            "p50_ms": round(percentile(values, 0.50), 3),
            "p95_ms": round(percentile(values, 0.95), 3),
            "max_ms": round(max(values), 3),
        })
    return rows


def event_time_ms(record: dict[str, Any], prefer_mono: bool = False) -> float | None:
    if prefer_mono and record.get("mono_ms") is not None:
        return float(record["mono_ms"])
    if record.get("timestamp_s") is not None:
        return float(record["timestamp_s"]) * 1000
    return None


def sorted_events(records: list[dict[str, Any]], event: str) -> list[dict[str, Any]]:
    return sorted(
        (record for record in records if record.get("event") == event and event_time_ms(record) is not None),
        key=lambda record: (event_time_ms(record) or 0, str(record.get("source_file", ""))),
    )


def first_anchor(progress: list[dict[str, Any]], event: str) -> dict[str, Any] | None:
    hits = [
        record for record in progress
        if record.get("event") == event and event_time_ms(record, prefer_mono=True) is not None
    ]
    return min(hits, key=lambda record: event_time_ms(record, prefer_mono=True) or 0) if hits else None


def compute_measurement_window(
    progress: list[dict[str, Any]],
    diagnostics: dict[str, list[dict[str, Any]]] | None = None,
) -> tuple[float, float] | None:
    begin = first_anchor(progress, "measurement_begin")
    end = first_anchor(progress, "measurement_end")
    missing = []
    if begin is None:
        missing.append("measurement_begin")
    if end is None:
        missing.append("measurement_end")
    if missing:
        if diagnostics is not None:
            diagnostics["missing_anchors"].append({
                "stage": "measurement_window", "events": missing,
            })
        return None
    begin_s = begin.get("timestamp_s")
    end_s = end.get("timestamp_s")
    if begin_s is None or end_s is None or float(end_s) < float(begin_s):
        if diagnostics is not None:
            diagnostics["invalid_spans"].append({"stage": "measurement_window"})
        return None
    return float(begin_s), float(end_s)


def in_measurement_window(
    record: dict[str, Any], window: tuple[float, float] | None
) -> bool:
    if window is None or record.get("timestamp_s") is None:
        return False
    timestamp = float(record["timestamp_s"])
    return window[0] <= timestamp <= window[1]


def compute_initialization_samples(
    progress: list[dict[str, Any]], diagnostics: dict[str, list[dict[str, Any]]]
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    segments: list[dict[str, Any]] = []
    missing: list[str] = []
    unavailable: list[str] = []
    for name, start_event, end_event, optional in SYSTEM_SEGMENTS:
        start = first_anchor(progress, start_event)
        end = first_anchor(progress, end_event)
        if start is None or end is None:
            # An optional segment is one whose anchors the multi-host
            # orchestrator owns and therefore never emits into this progress
            # log; record it as unavailable instead of voiding the whole stage.
            if optional:
                unavailable.append(name)
            else:
                missing.extend(event for event, record in
                               ((start_event, start), (end_event, end))
                               if record is None)
            continue
        start_ms = event_time_ms(start, prefer_mono=True)
        end_ms = event_time_ms(end, prefer_mono=True)
        if start_ms is None or end_ms is None or end_ms < start_ms:
            diagnostics["invalid_spans"].append({"stage": "system_init", "segment": name})
            continue
        segments.append({
            "name": name,
            "start_event": start_event,
            "end_event": end_event,
            "duration_ms": round(end_ms - start_ms, 3),
        })
    system_samples: list[dict[str, Any]] = []
    if missing:
        diagnostics["missing_anchors"].append({"stage": "system_init", "events": sorted(set(missing))})
    elif segments:
        # The anchor span is what the flow itself reports; it deliberately
        # excludes tee_registration (measured separately below) and any
        # orchestrator-owned startup segment the multi-host driver replaced.
        system_samples.append({
            "sample_id": "system_init",
            "duration_ms": round(sum(float(item["duration_ms"]) for item in segments), 3),
            "segments": segments,
            "excluded": ["tee_registration"] + sorted(set(unavailable)),
            "unavailable_segments": sorted({name for name, _, _, optional in SYSTEM_SEGMENTS
                                            if optional and name not in
                                            {segment["name"] for segment in segments}}),
        })

    registration_samples: list[dict[str, Any]] = []
    reg_start = first_anchor(progress, "tee_registration_begin")
    reg_end = first_anchor(progress, "active_set_bootstrapped")
    if reg_start is None or reg_end is None:
        missing_reg = []
        if reg_start is None:
            missing_reg.append("tee_registration_begin")
        if reg_end is None:
            missing_reg.append("active_set_bootstrapped")
        diagnostics["missing_anchors"].append({"stage": "tee_registration", "events": missing_reg})
    else:
        start_ms = event_time_ms(reg_start, prefer_mono=True)
        end_ms = event_time_ms(reg_end, prefer_mono=True)
        if start_ms is not None and end_ms is not None and end_ms >= start_ms:
            # The headline tee_registration number is the registration of ONE
            # TEE, not the wall-clock span in which all TEEs registered. Every
            # candidate registers on its own in parallel with the others, so the
            # cluster span (~12 min for 16 TEEs) is a schedule length, not a
            # per-TEE latency. Each TEE emits its own tee_registered duration;
            # that is what we report, one sample per TEE.
            per_tee = collect_tee_registered_samples(progress)
            if per_tee:
                registration_samples.extend(per_tee)
            else:
                diagnostics["invalid_spans"].append({
                    "stage": "tee_registration", "reason": "no per-TEE tee_registered durations"})
        else:
            diagnostics["invalid_spans"].append({"stage": "tee_registration"})
    return system_samples, registration_samples


def compute_registration_span(progress: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Whole-cluster span: registration start until the active set is bootstrapped."""
    start = first_anchor(progress, "tee_registration_begin")
    end = first_anchor(progress, "active_set_bootstrapped")
    if start is None or end is None:
        return []
    start_ms = event_time_ms(start, prefer_mono=True)
    end_ms = event_time_ms(end, prefer_mono=True)
    if start_ms is None or end_ms is None or end_ms < start_ms:
        return []
    registered = [record for record in progress if record.get("event") == "tee_registered"]
    return [{
        "sample_id": "tee_registration_cluster",
        "duration_ms": round(end_ms - start_ms, 3),
        "registered_count": len(registered),
        "start_event": "tee_registration_begin",
        "end_event": "active_set_bootstrapped",
    }]


def collect_tee_registered_samples(progress: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """One sample per registered TEE: that TEE's own begin-to-confirmed latency."""
    samples: list[dict[str, Any]] = []
    for record in progress:
        if record.get("stage") != "tee_registration" or record.get("event") != "tee_registered":
            continue
        try:
            duration_ms = float(record.get("duration_ns")) / 1_000_000.0
        except (TypeError, ValueError):
            continue
        if not math.isfinite(duration_ms) or duration_ms < 0:
            continue
        node = record.get("node", "")
        samples.append({
            "sample_id": f"tee_registered:{node}",
            "node": node,
            "tee_index": record.get("tee_index"),
            "duration_ms": round(duration_ms, 3),
            "start_event": "tee_registration_begin",
            "end_event": "tee_registered",
            "status": "ok",
        })
    return samples


def compute_tee_registration_substages(
    progress: list[dict[str, Any]],
) -> dict[str, list[dict[str, Any]]]:
    """Collect per-TEE registration substage durations from PERF_ANCHOR records."""
    result: dict[str, list[dict[str, Any]]] = {}
    event_names = (
        "tee_material_produced",
        "tee_mr_chain_confirmed",
        "tee_pending_ready",
        "tee_committee_review",
        "tee_chain_submit",
        "tee_chain_confirmed",
    )
    for record in progress:
        if record.get("stage") != "tee_registration" or record.get("event") not in event_names:
            continue
        try:
            duration_ms = float(record.get("duration_ns")) / 1_000_000.0
        except (TypeError, ValueError):
            continue
        event = str(record["event"])
        result.setdefault(event, []).append({
            "sample_id": f"{event}:{record.get('node', '')}",
            "node": record.get("node"),
            "tee_index": record.get("tee_index"),
            "attempt": record.get("attempt"),
            "duration_ms": round(duration_ms, 3),
            "status": "ok",
        })
    return result


def status_is_success(record: dict[str, Any]) -> bool:
    try:
        return int(record.get("status")) == 1
    except (TypeError, ValueError):
        return False


def group_by_join_key(records: list[dict[str, Any]]) -> dict[str, list[dict[str, Any]]]:
    groups: dict[str, list[dict[str, Any]]] = {}
    for record in records:
        join_key = normalize_join_key(record.get("join_key"))
        if join_key:
            record["join_key"] = join_key
            groups.setdefault(join_key, []).append(record)
    return groups


def compute_edit_samples(
    geth: list[dict[str, Any]], diagnostics: dict[str, list[dict[str, Any]]],
    window: tuple[float, float] | None,
    progress: list[dict[str, Any]] | None = None,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    all_relevant = [record for record in geth if record.get("stage") == "edit_execution"]
    relevant = [record for record in all_relevant if in_measurement_window(record, window)]
    diagnostics["prefilter_exclusions"].extend(
        {
            "stage": "edit_execution",
            "event": record.get("event"),
            "join_key": normalize_join_key(record.get("join_key")),
            "reason": "outside_measurement_window",
        }
        for record in all_relevant if record not in relevant
    )
    groups = group_by_join_key(relevant)
    # Proposer-side anchor: the instant node1 hands the redaction request to the
    # committee (script PERF_ANCHOR committee_redact_request_submitted).  It
    # lives in the progress stream, not the geth E2E_TIMING stream, and carries
    # the same join_key, so group it separately and attach it per request.
    proposer_anchors = group_by_join_key([
        record for record in (progress or [])
        if record.get("stage") == "edit_execution"
        and record.get("event") == "committee_redact_request_submitted"
        and in_measurement_window(record, window)
    ])
    edit_samples: list[dict[str, Any]] = []
    confirmation_samples: list[dict[str, Any]] = []
    for join_key, events in sorted(groups.items()):
        # Start = the redaction request being proposed by node1 to the committee.
        # Fall back to the ON node's material load only if the proposer anchor is
        # absent (e.g. an older run); the script-side redaction_begin anchor is
        # never usable because it brackets the target-tx mining wait.
        proposer = sorted_events(proposer_anchors.get(join_key, []),
                                 "committee_redact_request_submitted")
        starts = proposer or sorted_events(events, "material_load_start")
        start_source = "committee_redact_request_submitted" if proposer else "material_load_start"
        # Redaction execution has two equivalent submission anchors: the
        # teehost contract backend emits execute_redaction_tx_submitted,
        # while the in-process eth executor emits
        # redaction_submit_execute_onchain.  Correlate both so the timing
        # stage remains implementation-independent.
        named_submissions = sorted(
            (
                record
                for event_name in (
                    "execute_redaction_tx_submitted",
                    "redaction_submit_execute_onchain",
                )
                for record in events
                if record.get("event") == event_name
            ),
            key=lambda record: event_time_ms(record) or float("inf"),
        )
        submissions: list[dict[str, Any]] = []
        for record in named_submissions:
            tx_hash = normalize_tx_hash(
                record.get("tx_hash", record.get("tx"))
            )
            if tx_hash is None:
                diagnostics["invalid_submissions"].append(record)
                continue
            normalized = dict(record)
            normalized["tx_hash"] = tx_hash
            submissions.append(normalized)

        submit_failures = [
            record for record in events if record.get("event") == "execute_redaction_submit_failed"
        ]
        receipt_failures = [
            record for record in events if record.get("event") == "execute_redaction_receipt_failed"
        ]
        diagnostics["submission_failures"].extend(submit_failures)
        diagnostics["receipt_failures"].extend(receipt_failures)

        named_successes = sorted_events(events, "execute_redaction_receipt_success")
        successes: list[dict[str, Any]] = []
        for record in named_successes:
            tx_hash = normalize_tx_hash(record.get("tx_hash"))
            if not status_is_success(record) or tx_hash is None:
                diagnostics["invalid_success_receipts"].append(record)
                continue
            normalized = dict(record)
            normalized["tx_hash"] = tx_hash
            successes.append(normalized)

        for event_name, records in (
            (start_source, starts),
            ("execute_redaction_tx_submitted", submissions),
            ("redaction_submit_execute_onchain", [
                record for record in submissions
                if record.get("event") == "redaction_submit_execute_onchain"
            ]),
            ("execute_redaction_receipt_success", successes),
        ):
            if len(records) > 1:
                diagnostics["duplicate_events"].append({
                    "stage": "edit_execution", "join_key": join_key,
                    "event": event_name, "count": len(records),
                })

        # The edit flow's meaningful completion is the TEE-host that finishes
        # the redaction: it aggregates the partial adaptations into the new r'
        # (RPrime) and then broadcasts the resulting modify message to the
        # cluster, emitted as redaction_broadcast_modify_msg.  Everything after
        # it -- the on-chain authorization submission and its receipt -- is a
        # separate, chain-gated concern, so it must not inflate the
        # edit-execution latency.  The earliest such broadcast across the
        # cluster is the first TEE to complete the edit.
        broadcast_ends = sorted_events(events, "redaction_broadcast_modify_msg")

        if not starts:
            if submissions:
                diagnostics["unmatched_completions"].append({
                    "stage": "edit_execution", "join_key": join_key,
                    "event": "execute_redaction_tx_submitted",
                })
        else:
            start = starts[0]
            start_ms = event_time_ms(start)
            eligible_broadcast = [
                record for record in broadcast_ends
                if (event_time_ms(record) or -1) >= (start_ms or 0)
            ]
            if not eligible_broadcast:
                diagnostics["unmatched_starts"].append({
                    "stage": "edit_execution", "join_key": join_key,
                    "event": start_source, "reason": "no_broadcast_modify_msg",
                })
            else:
                end = eligible_broadcast[0]
                end_ms = event_time_ms(end)
                # Keep the first on-chain submission as traceability metadata;
                # it is not part of the measured span.
                eligible_submissions = [
                    record for record in submissions
                    if (event_time_ms(record) or -1) >= (start_ms or 0)
                ]
                first_submission = eligible_submissions[0] if eligible_submissions else None
                edit_samples.append({
                    "join_key": join_key,
                    "duration_ms": round((end_ms or 0) - (start_ms or 0), 3),
                    "start_event": start_source,
                    "start_host": start.get("node") or start.get("source"),
                    "end_host": end.get("tee_host") or end.get("tee"),
                    "submit_host": (first_submission or {}).get("tee_host"),
                    "tx_hash": (first_submission or {}).get("tx_hash"),
                    "end_event": "redaction_broadcast_modify_msg",
                    "status": "ok",
                    "start_timestamp_s": start.get("timestamp_s"),
                    "end_timestamp_s": end.get("timestamp_s"),
                })

        matched_confirmation: tuple[dict[str, Any], dict[str, Any]] | None = None
        for receipt in successes:
            receipt_ms = event_time_ms(receipt)
            matching_submissions = [
                record for record in submissions
                if record["tx_hash"] == receipt["tx_hash"]
                and (event_time_ms(record) or -1) <= (receipt_ms or 0)
            ]
            if matching_submissions:
                matched_confirmation = (matching_submissions[0], receipt)
                break
        if matched_confirmation is not None:
            submitted, receipt = matched_confirmation
            submitted_ms = event_time_ms(submitted)
            receipt_ms = event_time_ms(receipt)
            confirmation_samples.append({
                "join_key": join_key,
                "duration_ms": round((receipt_ms or 0) - (submitted_ms or 0), 3),
                "tx_hash": receipt["tx_hash"],
                "submit_host": submitted.get("tee_host"),
                "receipt_host": receipt.get("tee_host"),
                "receipt_block_number": receipt.get("receipt_block_number"),
                "status": "ok",
                "start_timestamp_s": submitted.get("timestamp_s"),
                "end_timestamp_s": receipt.get("timestamp_s"),
            })
        elif successes:
            diagnostics["unmatched_completions"].append({
                "stage": "execute_redaction_confirmation", "join_key": join_key,
                "event": "execute_redaction_receipt_success",
                "reason": "no_matching_submission_tx_hash",
            })

        if submissions and matched_confirmation is None:
            diagnostics["edit_integrity_alerts"].append({
                "join_key": join_key,
                "reason": "submitted_without_successful_receipt",
                "submitted_tx_hashes": sorted({record["tx_hash"] for record in submissions}),
                "receipt_failure_count": len(receipt_failures),
            })
    return edit_samples, confirmation_samples


def same_ledger_target(start: dict[str, Any], end: dict[str, Any]) -> bool:
    return (
        start.get("block_number") == end.get("block_number")
        and start.get("tx_index") == end.get("tx_index")
    )


def load_node_role_strata(progress: list[dict[str, Any]]) -> dict[str, str]:
    """Map geth log file name -> ledger stratum from the e2e topology anchor.

    The e2e emits PERF_ANCHOR stage=topology event=node_roles on=<i>
    on_observers=<i,j> miners=<...> once nodes are up. Returns {} when absent
    (pre-observer runs keep the legacy single-stratum behavior)."""
    strata: dict[str, str] = {}
    for record in progress:
        if record.get("stage") != "topology" or record.get("event") != "node_roles":
            continue
        try:
            on_index = int(str(record.get("on", "")).strip())
        except ValueError:
            on_index = 0
        if on_index > 0:
            strata[f"geth-node-{on_index}.log"] = "on"
        for part in str(record.get("on_observers", "")).split(","):
            part = part.strip()
            if part.isdigit():
                strata[f"geth-node-{part}.log"] = "on_observer"
        break
    return strata


LEDGER_LOCAL_EVENTS = ("lu_getblock", "lu_validate", "lu_update")


def local_ledger_spans(events: list[dict[str, Any]]) -> tuple[float | None, dict[str, float]]:
    """Pure-local ledger-apply time: the parts of the ON observer's ledger
    update that actually run on the node, excluding the block-visibility wait.

    The headline `tx_modify_received -> local_block_updated` span is dominated
    by `lu_wait_visible` (waiting for the mined block to become visible), which
    is chain timing, not the node's local work.  The per-span `duration_ns` of
    lu_getblock/lu_validate/lu_update is exactly the local apply cost and is
    timing-window independent.

    Returns (sum_ms_or_None, {event: duration_ms})."""
    per_event: dict[str, float] = {}
    for event in LEDGER_LOCAL_EVENTS:
        durations = [
            float(record["duration_ns"]) / 1_000_000
            for record in events
            if record.get("event") == event and record.get("duration_ns") is not None
        ]
        if durations:
            per_event[event] = round(sum(durations), 3)
    if not per_event:
        return None, {}
    return round(sum(per_event.values()), 3), per_event


def compute_ledger_samples(
    geth: list[dict[str, Any]], diagnostics: dict[str, list[dict[str, Any]]],
    window: tuple[float, float] | None,
    node_strata: dict[str, str] | None = None,
) -> list[dict[str, Any]]:
    all_stage_events = [record for record in geth if record.get("stage") == "ledger_update"]
    stage_events = [
        record for record in all_stage_events if in_measurement_window(record, window)
    ]
    diagnostics["prefilter_exclusions"].extend(
        {
            "stage": "ledger_update",
            "event": record.get("event"),
            "join_key": normalize_join_key(record.get("join_key")),
            "reason": "outside_measurement_window",
        }
        for record in all_stage_events if record not in stage_events
    )
    diagnostics["non_on_ledger_events"].extend(
        record for record in stage_events if str(record.get("node_role", "")).lower() != "on"
    )
    on_events = [
        record for record in stage_events
        if str(record.get("node_role", "")).lower() == "on"
    ]
    if node_strata:
        # 双 ON（miner-ON + observer-ON）场景：按日志文件分层配对，避免跨节点错配
        def _stratum(record: dict[str, Any]) -> str:
            return node_strata.get(str(record.get("source_file", "")), "on")

        grouped: dict[tuple[str, str], list[dict[str, Any]]] = {}
        for record in on_events:
            grouped.setdefault((normalize_join_key(record.get("join_key")), _stratum(record)), []).append(record)
        pairs = [(key[0], key[1], events) for key, events in grouped.items()]
        pairs.sort(key=lambda item: (item[0], item[1]))
    else:
        pairs = [
            (join_key, "on", events)
            for join_key, events in sorted(group_by_join_key(on_events).items())
        ]
    samples: list[dict[str, Any]] = []
    for join_key, stratum, events in pairs:
        starts = sorted_events(events, "tx_modify_received")
        completions = sorted_events(events, "local_block_updated")
        failures = [record for record in events if record.get("event") == "tx_modify_failed"]
        duplicates = [record for record in events if record.get("event") == "tx_modify_duplicate"]
        diagnostics["ledger_failures"].extend(failures)
        diagnostics["duplicate_events"].extend(duplicates)
        if len(starts) > 1:
            diagnostics["duplicate_events"].append({
                "stage": "ledger_update", "join_key": join_key,
                "event": "tx_modify_received", "count": len(starts),
            })
        if not starts:
            if completions:
                diagnostics["unmatched_completions"].append({
                    "stage": "ledger_update", "join_key": join_key,
                    "event": "local_block_updated",
                })
            continue
        start = starts[0]
        start_ms = event_time_ms(start)
        eligible = [
            record for record in completions
            if (event_time_ms(record) or -1) >= (start_ms or 0)
            and same_ledger_target(start, record)
        ]
        if not eligible:
            diagnostics["unmatched_starts"].append({
                "stage": "ledger_update", "join_key": join_key,
                "event": "tx_modify_received", "reason": "no_successful_local_update",
            })
            continue
        end = eligible[0]
        end_ms = event_time_ms(end)
        local_ms, local_breakdown = local_ledger_spans(events)
        if local_ms is None:
            diagnostics["invalid_spans"].append({
                "stage": "ledger_update", "join_key": join_key,
                "reason": "missing_local_apply_spans",
                "events": sorted({str(record.get("event")) for record in events}),
            })
        samples.append({
            "join_key": join_key,
            # Headline: pure-local apply cost on the node (getblock + validate
            # + local ledger write).  Chain-visibility waiting is excluded.
            "duration_ms": local_ms if local_ms is not None else 0.0,
            "status": "ok" if local_ms is not None else "missing_local_span",
            "local_duration_ms": local_ms,
            "local_breakdown_ms": local_breakdown,
            # Kept separately: the full anchor span, dominated by lu_wait_visible.
            "total_duration_ms": round((end_ms or 0) - (start_ms or 0), 3),
            "node_id": start.get("node_id"),
            "node_role": "on",
            "stratum": stratum,
            "source_file": start.get("source_file"),
            "block_number": start.get("block_number"),
            "tx_index": start.get("tx_index"),
            "start_timestamp_s": start.get("timestamp_s"),
            "end_timestamp_s": end.get("timestamp_s"),
        })
    return samples


def compute_block_samples(
    geth: list[dict[str, Any]], stage: str, event: str,
    window: tuple[float, float] | None,
    diagnostics: dict[str, list[dict[str, Any]]],
) -> list[dict[str, Any]]:
    samples: list[dict[str, Any]] = []
    relevant = [
        record for record in geth
        if record.get("stage") == stage and record.get("event") == event
    ]
    for record in relevant:
        reason = None
        if not in_measurement_window(record, window):
            reason = "outside_measurement_window"
        elif str(record.get("status", "")).lower() != "ok":
            reason = "status_not_ok"
        elif record.get("duration_ms") is None:
            reason = "missing_duration"
        elif float(record["duration_ms"]) < 0:
            reason = "negative_duration"
        try:
            txs = int(record.get("txs"))
        except (TypeError, ValueError):
            txs = -1
        if reason is None and txs < 0:
            reason = "missing_txs_stratum"
        if reason is not None:
            diagnostics["prefilter_exclusions"].append({
                "stage": stage,
                "event": event,
                "source_file": record.get("source_file"),
                "block": record.get("block"),
                "reason": reason,
            })
            continue
        sample = dict(record)
        sample["status"] = "ok"
        sample["txs"] = txs
        sample["stratum"] = "txs>0" if txs > 0 else "txs=0"
        samples.append(sample)
    return samples


def _rotation_trigger_blocks(
    geth: list[dict[str, Any]], anchor_event: str, epoch_field: str,
) -> dict[int, int]:
    """Map rotation epoch -> the node1 block that minted the driving log event.

    anchor_event is the Go rotation_flow anchor and epoch_field the field on it
    that names the epoch:
      rotation_prep_requested  (TEERotationPreparationRequested) -> nextEpoch
      rotation_trigger_seen    (ActiveSetRotated)                 -> epoch
    Both carry trigger_block = the on-chain block number of the event log.
    """
    blocks: dict[int, int] = {}
    for record in geth:
        if record.get("stage") != "rotation_flow" or record.get("event") != anchor_event:
            continue
        try:
            epoch = int(record.get(epoch_field))
            block = int(record.get("trigger_block"))
        except (TypeError, ValueError):
            continue
        blocks.setdefault(epoch, block)
    return blocks


def _block_mint_times(geth: list[dict[str, Any]]) -> dict[int, float]:
    """Map block number -> miner (node1) seal time in ms.

    node1 is the only miner; its `miner_task_queued_for_sealing` anchor carries
    `block`, and the geth log line prefix is the mint wall-clock (ms).  We take
    the earliest such record per block.  Deliberately NOT built from
    insert_chain_total: that is the import/validation path and on a TEE host it
    is the (later) receive time, not the mint time.
    """
    mint: dict[int, float] = {}
    for record in geth:
        if record.get("event") != "miner_task_queued_for_sealing":
            continue
        try:
            block = int(record.get("block"))
        except (TypeError, ValueError):
            continue
        ts = event_time_ms(record)
        if ts is None:
            continue
        if block not in mint or ts < mint[block]:
            mint[block] = ts
    return mint


def _rotation_end(
    dkg_complete: list[dict[str, Any]], window: tuple[float, float] | None,
) -> dict[str, Any] | None:
    """Cluster-wide DKG completion: the LATEST per-TEE rotation_dkg_complete in
    the measurement window (the instant the last active TEE applied its new
    share).  Returns None when no completion was observed."""
    ends = [
        record for record in dkg_complete
        if event_time_ms(record) is not None and in_measurement_window(record, window)
    ]
    if not ends:
        return None
    return max(ends, key=lambda record: event_time_ms(record) or 0)


# Two start口径 for the rotation headline, both ending at cluster-wide DKG
# completion.  Kept as separate stages so the measured span is never ambiguous.
ROTATION_START_ROTATED = "rotated"
ROTATION_START_PREP = "prep"
ROTATION_START_ANCHORS = {
    # epoch change landed on chain (ActiveSetRotated); DKRS begins from here
    ROTATION_START_ROTATED: ("rotation_trigger_seen", "epoch"),
    # the epoch's rotation was requested (TEERotationPreparationRequested)
    ROTATION_START_PREP: ("rotation_prep_requested", "nextEpoch"),
}


def compute_rotation_samples(
    teehost: list[dict[str, Any]], geth: list[dict[str, Any]], threshold: int,
    window: tuple[float, float] | None,
    diagnostics: dict[str, list[dict[str, Any]]],
    start_mode: str = ROTATION_START_ROTATED,
) -> list[dict[str, Any]]:
    """Rotation span = an epoch-triggering block's mint time on node1 (the
    miner) -> cluster-wide DKG completion.

    Start is chosen by start_mode (see ROTATION_START_ANCHORS):
      ROTATION_START_ROTATED -> ActiveSetRotated block (epoch change lands on chain)
      ROTATION_START_PREP    -> TEERotationPreparationRequested block (epoch requested)
    Its mint time is taken from node1's `miner_task_queued_for_sealing`, not from
    any host's block-import time (a TEE host imports the block later).  Falls
    back to the node's own `dkrs_start` (-> `start_source=dkrs_start`) only when
    the on-chain trigger block is unavailable (e.g. anchors emitted before this
    change).
    End: the last active TEE's `rotation_dkg_complete` (all new shares applied).
    """
    if threshold < 1:
        raise ValueError("rotation threshold must be at least 1")
    anchor_event, epoch_field = ROTATION_START_ANCHORS[start_mode]
    dkg_complete = [record for record in geth if record.get("event") == "rotation_dkg_complete"]
    blocks = _rotation_trigger_blocks(geth, anchor_event, epoch_field)
    mint = _block_mint_times(geth)

    dkrs_start: dict[int, dict[str, Any]] = {}
    for record in teehost:
        if (record.get("event") != "dkrs_start" or event_time_ms(record) is None
                or not in_measurement_window(record, window)):
            continue
        try:
            epoch = int(record.get("epoch"))
        except (TypeError, ValueError):
            continue
        previous = dkrs_start.get(epoch)
        if previous is None or (event_time_ms(record) or 0) < (event_time_ms(previous) or 0):
            dkrs_start[epoch] = record

    samples: list[dict[str, Any]] = []
    epochs = sorted(set(blocks) | set(dkrs_start) | {
        int(record["epoch"]) for record in dkg_complete
        if str(record.get("epoch", "")).lstrip("-").isdigit()
    })
    for epoch in epochs:
        start_ms: float | None = None
        start_source = f"{anchor_event}_mint"
        start_block: int | None = None
        block = blocks.get(epoch)
        if block is not None and block in mint:
            start_ms = mint[block]
            start_block = block
        else:
            fallback = dkrs_start.get(epoch)
            if fallback is None:
                diagnostics["missing_anchors"].append({
                    "stage": "rotation_flow", "epoch": epoch, "anchor": "start",
                    "start_mode": start_mode,
                    "reason": "no_onchain_trigger_block_and_no_dkrs_start",
                })
                continue
            start_ms = event_time_ms(fallback)
            start_source = "dkrs_start"
        end = _rotation_end(dkg_complete, window)
        if end is None:
            diagnostics["missing_anchors"].append({
                "stage": "rotation_flow", "epoch": epoch, "anchor": "rotation_dkg_complete",
                "start_mode": start_mode, "reason": "no DKG completion anchor in window",
            })
            continue
        end_ms = event_time_ms(end)
        if start_ms is None or end_ms is None or end_ms < start_ms:
            continue
        parties = end.get("parties")
        samples.append({
            "epoch": epoch,
            "duration_ms": round(end_ms - start_ms, 3),
            "threshold": threshold,
            "parties": parties,
            "status": "ok",
            "start_source": start_source,
            "start_block": start_block,
            "end_source_file": end.get("source_file"),
            "end_receiver_index": end.get("receiverIndex"),
            "start_timestamp_s": start_ms / 1000.0,
            "end_timestamp_s": end.get("timestamp_s"),
        })
    return samples


def stage_row(
    stage: str, samples: list[dict[str, Any]], basis: str, note: str,
    diagnostics: dict[str, list[dict[str, Any]]], stratum: str = "all",
    headline: bool = True,
) -> dict[str, Any]:
    values = [float(sample["duration_ms"]) for sample in samples]
    excluded_indices: set[int] = set()
    lower = None
    upper = None
    if len(values) >= 30:
        q1 = percentile(values, 0.25)
        q3 = percentile(values, 0.75)
        iqr = q3 - q1
        lower = q1 - 3 * iqr
        upper = q3 + 3 * iqr
        excluded_indices = {
            index for index, value in enumerate(values)
            if value < lower or value > upper
        }
    used_values = [
        value for index, value in enumerate(values) if index not in excluded_indices
    ]
    for index in sorted(excluded_indices):
        sample = samples[index]
        sample["statistical_outlier"] = True
        sample["outlier_bounds_ms"] = {
            "lower": round(float(lower), 3),
            "upper": round(float(upper), 3),
        }
        diagnostics["statistical_outliers"].append({
            "stage": stage,
            "stratum": stratum,
            "duration_ms": values[index],
            "source_file": sample.get("source_file"),
            "join_key": sample.get("join_key"),
            "block": sample.get("block"),
            "lower_ms": round(float(lower), 3),
            "upper_ms": round(float(upper), 3),
        })
    raw_count = len(values)
    excluded_count = len(excluded_indices)
    excluded_rate = excluded_count / raw_count if raw_count else 0.0
    if excluded_rate > 0.05:
        diagnostics["outlier_warnings"].append({
            "stage": stage,
            "stratum": stratum,
            "excluded_count": excluded_count,
            "excluded_rate": round(excluded_rate, 6),
            "reason": "tukey_far_outlier_rate_above_5_percent",
        })
    if not values:
        return {
            "stage": stage, "stratum": stratum, "headline": headline,
            "raw_count": 0, "used_count": 0, "excluded_count": 0,
            "excluded_rate": 0.0, "raw_mean": "", "filtered_mean": "",
            "p50": "", "p95": "", "p99": "", "max": "",
            "basis": basis, "note": note + "; stage not present in data",
        }
    return {
        "stage": stage,
        "stratum": stratum,
        "headline": headline,
        "raw_count": raw_count,
        "used_count": len(used_values),
        "excluded_count": excluded_count,
        "excluded_rate": round(excluded_rate, 6),
        "raw_mean": round(sum(values) / raw_count, 3),
        "filtered_mean": round(sum(used_values) / len(used_values), 3),
        "p50": round(percentile(values, 0.50), 3),
        "p95": round(percentile(values, 0.95), 3),
        "p99": round(percentile(values, 0.99), 3),
        "max": round(max(values), 3),
        "basis": basis,
        "note": note,
    }


def compute_timing(
    geth: list[dict[str, Any]], progress: list[dict[str, Any]],
    teehost: list[dict[str, Any]] | None = None, threshold: int = 3,
) -> tuple[dict[str, list[dict[str, Any]]], dict[str, list[dict[str, Any]]], list[dict[str, Any]]]:
    diagnostics: dict[str, list[dict[str, Any]]] = {
        "missing_anchors": [],
        "invalid_spans": [],
        "unmatched_starts": [],
        "unmatched_completions": [],
        "submission_failures": [],
        "invalid_submissions": [],
        "receipt_failures": [],
        "invalid_success_receipts": [],
        "edit_integrity_alerts": [],
        "ledger_failures": [],
        "duplicate_events": [],
        "non_on_ledger_events": [],
        "prefilter_exclusions": [],
        "statistical_outliers": [],
        "outlier_warnings": [],
    }
    teehost = teehost or []
    window = compute_measurement_window(progress, diagnostics)
    system, registration = compute_initialization_samples(progress, diagnostics)
    registration_span = compute_registration_span(progress)
    registration_substages = compute_tee_registration_substages(progress)
    block_generation = compute_block_samples(
        geth, "block_generation", "miner_task_queued_for_sealing", window, diagnostics
    )
    block_validation = compute_block_samples(
        geth, "block_validation", "insert_chain_total", window, diagnostics
    )
    edits, confirmations = compute_edit_samples(geth, diagnostics, window, progress)
    node_strata = load_node_role_strata(progress)
    ledger = compute_ledger_samples(geth, diagnostics, window, node_strata=node_strata)
    rotation = compute_rotation_samples(
        teehost, geth, threshold, window, diagnostics, ROTATION_START_ROTATED)
    rotation_from_prep = compute_rotation_samples(
        teehost, geth, threshold, window, diagnostics, ROTATION_START_PREP)
    samples = {
        "system_init": system,
        "tee_registration": registration,
        # Whole-cluster span kept as a secondary stage (was the old headline).
        "tee_registration_cluster": registration_span,
        "tee_registration_substages": registration_substages,
        "block_generation": block_generation,
        "block_validation": block_validation,
        "edit_execution": edits,
        "execute_redaction_confirmation": confirmations,
        "ledger_update": ledger,
        # Full anchor span kept as a secondary stage (was the old headline).
        "ledger_update_total": ledger,
        "rotation_flow": rotation,
        "rotation_flow_from_prep": rotation_from_prep,
    }
    # Headline uses only samples whose local apply spans were all present; the
    # rest are already recorded under diagnostics["invalid_spans"].
    ledger_local = [sample for sample in ledger if sample.get("status") == "ok"]
    ledger_strata = sorted({str(sample.get("stratum", "on")) for sample in ledger_local})
    ledger_stratum_rows = []
    if len(ledger_strata) > 1:
        for stratum in ledger_strata:
            ledger_stratum_rows.append(
                stage_row("ledger_update",
                          [sample for sample in ledger_local if str(sample.get("stratum", "on")) == stratum],
                          "ON geth:lu_getblock + lu_validate + lu_update",
                          f"ledger_update on {stratum} node(s) only (miner-ON vs observer-ON)",
                          diagnostics, stratum=stratum))
    rows = [
        stage_row("system_init", system, "sum of non-registration PERF_ANCHOR segments",
                  "chain/datadir + node startup + contract + committee + pkCH", diagnostics),
        stage_row("tee_registration", registration,
                  "script:tee_registration_begin -> tee_registered (per TEE)",
                  "one sample per registered TEE; per-TEE latency, not the 16-TEE wall-clock span",
                  diagnostics),
        *[
            stage_row("tee_registration", substage_samples,
                      f"per-TEE {event}",
                      "one sample per TEE/attempt; duration is emitted by the runner",
                      diagnostics, stratum=event, headline=False)
            for event, substage_samples in sorted(registration_substages.items())
        ],
        # Wall-clock span for ALL TEEs to reach the active set. Kept as a
        # secondary stage: it is a schedule length, not a per-TEE latency.
        stage_row("tee_registration", registration_span,
                  "script:tee_registration_begin -> active_set_bootstrapped",
                  "wall-clock span for the whole TEE cluster; not the per-TEE headline",
                  diagnostics, stratum="cluster", headline=False),
        stage_row("block_generation",
                  [sample for sample in block_generation if sample["stratum"] == "txs>0"],
                  "geth:miner_task_queued_for_sealing",
                  "non-empty blocks; cross-implementation headline", diagnostics,
                  stratum="txs>0", headline=True),
        stage_row("block_generation",
                  [sample for sample in block_generation if sample["stratum"] == "txs=0"],
                  "geth:miner_task_queued_for_sealing",
                  "empty blocks reported separately", diagnostics,
                  stratum="txs=0", headline=False),
        stage_row("block_validation",
                  [sample for sample in block_validation if sample["stratum"] == "txs>0"],
                  "geth:insert_chain_total",
                  "non-empty blocks; cross-implementation headline", diagnostics,
                  stratum="txs>0", headline=True),
        stage_row("block_validation",
                  [sample for sample in block_validation if sample["stratum"] == "txs=0"],
                  "geth:insert_chain_total",
                  "empty blocks reported separately", diagnostics,
                  stratum="txs=0", headline=False),
        stage_row("edit_execution", edits,
                  "script:committee_redact_request_submitted -> first TEE redaction_broadcast_modify_msg",
                  "one proposer-to-first-TEE-completion sample per normalized req_id; excludes the later chain-gated submission", diagnostics),
        stage_row("execute_redaction_confirmation", confirmations,
                  "geth:execute_redaction_tx_submitted -> matching status=1 receipt",
                  "req_id and tx_hash must both match", diagnostics),
        stage_row("ledger_update", ledger_local,
                  "ON geth:lu_getblock + lu_validate + lu_update",
                  "pure-local apply on node_role=on; excludes lu_wait_visible (block-visibility wait)", diagnostics),
        *ledger_stratum_rows,
        stage_row("ledger_update_total",
                  [{**sample, "duration_ms": sample["total_duration_ms"]} for sample in ledger],
                  "ON geth:tx_modify_received -> local_block_updated",
                  "full anchor span (kept for continuity); dominated by lu_wait_visible block-visibility wait",
                  diagnostics, headline=False),
        stage_row("rotation_flow", rotation,
                  "node1:ActiveSetRotated block mint -> all TEEs rotation_dkg_complete",
                  "epoch-change block's mint time on node1 -> last active TEE applies its new share", diagnostics),
        stage_row("rotation_flow_from_prep", rotation_from_prep,
                  "node1:TEERotationPreparationRequested block mint -> all TEEs rotation_dkg_complete",
                  "proposer-side trigger block (earlier) -> last active TEE applies its new share; longer span, kept separate",
                  diagnostics, headline=False, stratum="from_prep"),
    ]
    return samples, diagnostics, rows


def write_csv(path: Path, rows: list[dict[str, Any]], fields: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as fp:
        writer = csv.DictWriter(fp, fieldnames=fields)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Collect SGX L5 timing data")
    parser.add_argument("--logs-dir", type=Path, default=DEFAULT_LOGS)
    parser.add_argument("--progress", type=Path, default=DEFAULT_PROGRESS)
    parser.add_argument("--out-dir", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--threshold", type=int, default=3)
    parser.add_argument("--year", type=int, default=datetime.now().year)
    args = parser.parse_args()

    geth_records = load_geth_records(args.logs_dir, args.year)
    progress_records = load_progress_records(args.progress, args.year)
    teehost_records = load_teehost_records(args.logs_dir, args.year)
    records = geth_records + progress_records + teehost_records
    summary = summarize(records)
    samples, diagnostics, stages = compute_timing(
        geth_records, progress_records, teehost_records, args.threshold
    )
    measurement_window = compute_measurement_window(progress_records)

    args.out_dir.mkdir(parents=True, exist_ok=True)
    out_json = args.out_dir / "l5-timing-summary.json"
    out_summary_csv = args.out_dir / "l5-timing-summary.csv"
    out_stages_csv = args.out_dir / "l5-timing-stages.csv"
    out_json.write_text(json.dumps({
        "records": records,
        "summary": summary,
        "samples": samples,
        "diagnostics": diagnostics,
        "stages": stages,
        "measurement_window": ({
            "start_timestamp_s": measurement_window[0],
            "end_timestamp_s": measurement_window[1],
        } if measurement_window is not None else None),
    }, ensure_ascii=False, indent=2))
    write_csv(out_summary_csv, summary,
              ["source", "stage", "event", "count", "min_ms", "mean_ms", "p50_ms", "p95_ms", "max_ms"])
    write_csv(out_stages_csv, stages,
              ["stage", "stratum", "headline", "raw_count", "used_count",
               "excluded_count", "excluded_rate", "raw_mean", "filtered_mean",
               "p50", "p95", "p99", "max", "basis", "note"])

    print(f"records: {len(records)}")
    print(f"summary_json: {out_json}")
    print(f"summary_csv: {out_summary_csv}")
    print(f"stages_csv: {out_stages_csv}")
    print("\n=== L5 timing stages ===")
    print(f"{'stage':<32}{'stratum':<10}{'raw':>8}{'used':>8}{'p50_ms':>12}{'p95_ms':>12}  basis")
    print("-" * 120)
    for row in stages:
        p50 = row["p50"] if row["p50"] != "" else "-"
        p95 = row["p95"] if row["p95"] != "" else "-"
        print(f"{row['stage']:<32}{row['stratum']:<10}{row['raw_count']:>8}"
              f"{row['used_count']:>8}{str(p50):>12}{str(p95):>12}  {row['basis']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
