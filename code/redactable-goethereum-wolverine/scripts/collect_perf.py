#!/usr/bin/env python3
"""collect_perf.py — redactable-geth 性能测试统计层（perf_test_plan.md §6）。

基于 tgovern 的 collect-l5-timing.py 改输入路径与锚点格式，逻辑沿用：
  - 输入：nodelogs/geth-node-*.log（E2E_TIMING，也兼容 node{i}.log）+ e2e-progress.log（PERF_ANCHOR）
  - 解析：marker + shlex 取 k=v；duration_ns→ms；geth logger 行首壁钟 [MM-DD|HH:MM:SS.mmm] → timestamp_s
  - 关联：mean 型按 (source,stage,event) 分箱；span 型按 join_key 配对 start/end；
          edit 端到端按 join_key=reqID 合并 submit 与各节点 edit_applied，取 max(ts)−min(ts)
  - 聚合：每 stage 一行 count/min/mean/p50/p95/max_ms（summary）+ 每 stage 一行代表耗时（stages）
  - 输出（nitch timing schema v3；历史 marker 不可混入 SGX L5-aligned headline）：
        timing-stages.csv  (stage, duration_ms, duration_s, count, basis, note)
        timing-summary.csv (source, stage, event, count, min_ms, mean_ms, p50_ms, p95_ms, max_ms)
        timing-summary.json({records, summary, stages})
        timing-report.md   (人读结论 + 完整性告警)
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


# ---------- 路径与正则 ----------
ROOT = Path(__file__).resolve().parents[1]
DEFAULT_LOGS = ROOT / "nodelogs"
DEFAULT_PROGRESS = ROOT / "e2e-progress.log"
DEFAULT_OUT = ROOT / "perf" / "out" / "latest"

GETH_TS_RE = re.compile(r"\[(\d{2}-\d{2})\|(\d{2}:\d{2}:\d{2}\.\d{3})\]")
# 脚本侧锚点：[MM-DD|HH:MM:SS.mmm] [+Nms] <message ...>（message 内含 PERF_ANCHOR + k=v）
PROGRESS_RE = re.compile(r"\[(\d{2}-\d{2}\|\d{2}:\d{2}:\d{2}\.\d{3})\] \[\+(\d+)ms\] (.*)")
JOIN_RE = re.compile(r"join_key=([0-9A-Za-z_\-]+)")
RUNID_RE = re.compile(r"run_id=([0-9A-Za-z_\-]+)")

# timing schema v4 headline/diagnostic rows，与 stages.yaml 对齐。
TIMING_SCHEMA_VERSION = 6
ALIGNED_MARKER_SCHEMA = 3
STAGE_HEADLINE = {
    "system_init":            ("distkg_extract_apply",        "segment_span",
                               "script span: phase_start→全员 dkgStatus().extracted"),
    "transaction_generation": ("geth_submit_transaction_total","mean",
                               "geth:geth_submit_transaction_total"),
    "transaction_validation": ("txpool_add_txs",              "mean",
                               "geth:txpool_add_txs"),
    "block_generation":       ("miner_task_queued_for_sealing", "aligned_block",
                               "geth v3: build start→taskCh send success（非空块）"),
    "block_finalize_cpu":     ("miner_finalize_assemble_block", "aligned_block",
                               "geth v3: FinalizeAndAssemble CPU（非空块）"),
    "miner_sealing_queue_wait": ("miner_sealing_queue_wait", "aligned_block",
                               "geth v3: taskCh enqueue→taskLoop Seal（非空块）"),
    "block_validation":       ("insert_chain_total",          "mean",
                               "geth:insert_chain_total"),
    "edit_execution":         ("redaction_edit_request_to_broadcast", "mean",
                               "ProposeModification entry→P2P ModifyMsg broadcast (shared edit endpoint; excludes local apply/chain confirmation)"),
    "ledger_update":          ("lu_getblock + lu_validate + lu_update", "receiver_update",
                               "receiver: lu_getblock + lu_validate + lu_update（排除 proposer）"),
    "ledger_update_total":    ("redaction_edit_applied", "receiver_update_total",
                               "receiver: 修改消息处理开始→本地更新完成（完整 span；排除 proposer）"),
    "ledger_update_e2e":      ("verified_all",                "ledger_e2e",
                               "script span: proposal_submitted→verified_all"),
    "ledger_convergence":     ("verified_all",                "ledger_convergence",
                               "script span: apply_observed→verified_all"),
    "rotation_flow":          ("reshare_extract_apply",       "rotation_global",
                               "geth: 全局最早成功 reshare_start→全局最后成功 reshare_extract_apply（全员完成墙钟口径）"),
    "rotation_flow_span":     ("reshare_extract_apply",       "rotation_quorum",
                               "geth: earliest reshare_start→required_shares unique reshare_extract_apply（跨节点 span，含触发错峰；legacy 口径）"),
    "rotation_flow_round":    ("reshare_extract_apply",       "rotation_round",
                               "geth: 每轮取最慢节点自身 span（全员完成口径；轮完成=最后一个节点 apply）"),
}


# ---------- 基础解析 ----------
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
    rest = line.split(marker, 1)[1].strip()
    record: dict[str, Any] = {"source": "geth", "source_file": source_file}
    ts = parse_geth_ts(line, year)
    if ts is not None:
        record["timestamp_s"] = ts
    tokens = rest.split()
    for token in tokens:
        if "=" not in token:
            continue
        key, value = token.split("=", 1)
        record[key] = parse_value(value)
    if "duration_ns" not in record:
        return None
    record.setdefault("stage", "unknown")
    record.setdefault("event", "unknown")
    try:
        record["duration_ns"] = int(record["duration_ns"])
    except (TypeError, ValueError):
        return None
    record["duration_ms"] = record["duration_ns"] / 1_000_000
    return record


def iter_geth_log_files(logs_dir: Path):
    """统计层 glob：优先 geth-node-*.log（编排层归档命名），兼容旧 node{i}.log。"""
    seen = set()
    for pat in ("geth-node-*.log", "node*.log"):
        for path in sorted(logs_dir.glob(pat)):
            if path.name not in seen:
                seen.add(path.name)
                yield path


def load_geth_records(logs_dir: Path, year: int) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for path in iter_geth_log_files(logs_dir):
        with path.open(errors="replace") as fp:
            for line in fp:
                record = parse_geth_timing_line(line, path.name, year)
                if record is not None:
                    records.append(record)
    return records


def collect_relay_stats(logs_dir: Path) -> dict[str, dict[str, int]]:
    """Aggregate bounded private-protocol relay diagnostics from geth logs."""
    stats = {
        name: {"relay_events": 0, "sent": 0, "failed": 0, "duplicates": 0,
               "ttl_exhausted": 0, "max_hop": 0, "unique_messages": 0}
        for name in ("distkg", "reshare", "modify")
    }
    message_ids = {name: set() for name in stats}
    for path in iter_geth_log_files(logs_dir):
        with path.open(errors="replace") as fp:
            for line in fp:
                if "DistKG" in line:
                    protocol = "distkg"
                elif "Reshare" in line:
                    protocol = "reshare"
                elif any(marker in line for marker in ("Modify proposal", "Key share", "TxModify")):
                    protocol = "modify"
                else:
                    continue
                if "relay" not in line.lower() and "duplicate modify message" not in line:
                    continue
                fields = _kv_from_message(line)
                message_id = fields.get("message_id")
                if message_id:
                    message_ids[protocol].add(message_id)
                try:
                    stats[protocol]["max_hop"] = max(
                        stats[protocol]["max_hop"], int(fields.get("hop", 0)))
                except ValueError:
                    pass
                if "TTL exhausted" in line:
                    stats[protocol]["ttl_exhausted"] += 1
                elif "duplicate dropped" in line or "duplicate modify message" in line:
                    stats[protocol]["duplicates"] += 1
                elif "relayed" in line:
                    stats[protocol]["relay_events"] += 1
                    for key in ("sent", "failed"):
                        field = "success" if key == "sent" else key
                        try:
                            stats[protocol][key] += int(fields.get(field, 0))
                        except ValueError:
                            pass
    for protocol in stats:
        stats[protocol]["unique_messages"] = len(message_ids[protocol])
    return stats


def load_run_metadata(path: Path, relay_stats: dict[str, dict[str, int]]) -> dict[str, Any]:
    if not path.exists():
        return {"relay": relay_stats}
    manifest = json.loads(path.read_text(encoding="utf-8"))
    nodes = manifest.get("nodes") or []
    light_nodes = [node for node in nodes if node.get("role") == "light"]
    return {
        "manifest_schema_version": manifest.get("schema_version"),
        "timing_schema_version": manifest.get("timing_schema_version"),
        "run_id": manifest.get("run_id"),
        "counts": manifest.get("counts") or {},
        "threshold": manifest.get("threshold") or {},
        "topology": manifest.get("topology") or {},
        "relay_ttl": manifest.get("relay_ttl") or {},
        "verification_nodes": manifest.get("verification_nodes") or [],
        "ledger_observer": manifest.get("ledger_observer") or {},
        "ledger_update_e2e": manifest.get("ledger_update_e2e") or {},
        "nodes": nodes,
        "light_total": len(light_nodes),
        "light_verified": sum(bool(node.get("verification")) for node in light_nodes),
        "relay": relay_stats,
    }


def _kv_from_message(message: str) -> dict[str, str]:
    out: dict[str, str] = {}
    try:
        tokens = shlex.split(message)
    except ValueError:
        tokens = message.split()
    for token in tokens:
        if "=" in token:
            key, value = token.split("=", 1)
            out[key] = value
    return out


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
                kv = _kv_from_message(message)
                record["stage"] = kv.get("stage", "script")
                record["event"] = kv.get("event", "anchor")
                if "join_key" in kv:
                    record["join_key"] = kv["join_key"]
                if "run_id" in kv:
                    record["run_id"] = kv["run_id"]
                for key, value in kv.items():
                    record.setdefault(key, parse_value(value))
            records.append(record)
    return records


# ---------- 聚合 ----------
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


def sample_identifier(record: dict[str, Any], fallback: str = "sample") -> str:
    source = str(record.get("source_file") or record.get("source") or "unknown")
    identity = (record.get("join_key") or record.get("block_hash") or
                record.get("block") or record.get("timestamp_s") or fallback)
    return f"{source}:{identity}"


def tukey_statistics(samples: list[dict[str, Any]]) -> dict[str, Any]:
    """SGX L5-compatible far-outlier policy; raw percentiles are never filtered."""
    ordered = sorted(samples, key=lambda sample: float(sample["duration_ms"]))
    values = [float(sample["duration_ms"]) for sample in ordered]
    if not values:
        return {
            "raw_count": 0, "used_count": 0, "excluded_count": 0, "excluded_rate": 0.0,
            "min_ms": "", "raw_mean_ms": "", "filtered_mean_ms": "",
            "p50_ms": "", "p95_ms": "", "p99_ms": "", "max_ms": "",
            "lower_fence_ms": None, "upper_fence_ms": None, "excluded_samples": [],
        }
    lower_fence = upper_fence = None
    used = ordered
    excluded: list[dict[str, Any]] = []
    if len(values) >= 30:
        q1 = percentile(values, 0.25)
        q3 = percentile(values, 0.75)
        iqr = q3 - q1
        lower_fence = q1 - 3 * iqr
        upper_fence = q3 + 3 * iqr
        used = [sample for sample in ordered
                if lower_fence <= float(sample["duration_ms"]) <= upper_fence]
        excluded = [sample for sample in ordered
                    if float(sample["duration_ms"]) < lower_fence or
                    float(sample["duration_ms"]) > upper_fence]
    used_values = [float(sample["duration_ms"]) for sample in used]
    raw_count = len(values)
    excluded_records = [{
        "sample_id": sample.get("sample_id") or sample_identifier(sample),
        "duration_ms": round(float(sample["duration_ms"]), 6),
    } for sample in excluded]
    return {
        "raw_count": raw_count,
        "used_count": len(used_values),
        "excluded_count": len(excluded_records),
        "excluded_rate": round(len(excluded_records) / raw_count, 6),
        "min_ms": round(min(values), 3),
        "raw_mean_ms": round(sum(values) / raw_count, 3),
        "filtered_mean_ms": round(sum(used_values) / len(used_values), 3) if used_values else "",
        "p50_ms": round(percentile(values, 0.50), 3),
        "p95_ms": round(percentile(values, 0.95), 3),
        "p99_ms": round(percentile(values, 0.99), 3),
        "max_ms": round(max(values), 3),
        "lower_fence_ms": round(lower_fence, 6) if lower_fence is not None else None,
        "upper_fence_ms": round(upper_fence, 6) if upper_fence is not None else None,
        "excluded_samples": excluded_records,
    }


def measurement_window(progress: list[dict[str, Any]]) -> tuple[float, float] | None:
    starts = [float(record["timestamp_s"]) for record in progress
              if record.get("stage") == "transaction_generation" and
              record.get("event") == "tx_phase_start" and record.get("timestamp_s") is not None]
    ends = [float(record["timestamp_s"]) for record in progress
            if record.get("stage") == "transaction_generation" and
            record.get("event") == "tx_phase_end" and record.get("timestamp_s") is not None]
    if not starts or not ends or max(ends) < min(starts):
        return None
    return min(starts), max(ends)


def annotate_record_context(records: list[dict[str, Any]], progress: list[dict[str, Any]],
                            run_metadata: dict[str, Any]) -> None:
    nodes = {int(node["index"]): node for node in run_metadata.get("nodes") or []
             if node.get("index") is not None}
    window = measurement_window(progress)
    for record in records:
        match = re.search(r"(?:geth-)?node-(\d+)\.log$", str(record.get("source_file", "")))
        if not match:
            match = re.search(r"node(\d+)\.log$", str(record.get("source_file", "")))
        if match:
            index = int(match.group(1))
            record.setdefault("node_id", index)
            if index in nodes:
                record.setdefault("node_role", nodes[index].get("role"))
        ts = record.get("timestamp_s")
        record["measurement_window"] = bool(
            window and ts is not None and window[0] <= float(ts) <= window[1])


def timing_schema_status(records: list[dict[str, Any]], run_metadata: dict[str, Any]) -> dict[str, Any]:
    marker_versions = sorted({int(record["marker_schema"]) for record in records
                              if record.get("marker_schema") is not None})
    block_v3 = any(record.get("event") == "miner_task_queued_for_sealing" and
                   int(record.get("marker_schema") or 0) == ALIGNED_MARKER_SCHEMA
                   for record in records)
    ledger_samples = compute_ledger_update_samples(records)
    ledger_v3 = bool(ledger_samples) and all(
        record.get("event") == "local_apply_sum" and
        int(record.get("marker_schema") or 0) == ALIGNED_MARKER_SCHEMA
        for record in ledger_samples)
    comparable = block_v3 and ledger_v3
    return {
        "requested_version": run_metadata.get("timing_schema_version"),
        "collector_version": TIMING_SCHEMA_VERSION,
        "observed_marker_versions": marker_versions,
        "block_generation_v3": block_v3,
        "ledger_update_v3": ledger_v3,
        "ledger_update_receiver_count": len(ledger_samples),
        "sgx_l5_headline_comparable": comparable,
        "compatibility_note": ("SGX L5-aligned v3 markers verified" if comparable else
                               "legacy/unversioned markers: not directly comparable with SGX L5 v2 headline"),
    }


def record_stratum(record: dict[str, Any]) -> str:
    fields: dict[str, Any] = {}
    if record.get("txs") is not None:
        txs = int(record["txs"])
        fields["batch_class"] = "empty" if txs == 0 else "nonempty"
        fields["batch_size"] = txs
    if record.get("node_role"):
        fields["node_role"] = record["node_role"]
    if record.get("measurement_window") is not None:
        fields["run_phase"] = "measurement" if record["measurement_window"] else "outside"
    return json.dumps(fields, sort_keys=True, separators=(",", ":")) if fields else "{}"


def successful_record(record: dict[str, Any]) -> bool:
    return str(record.get("status", "ok")) == "ok" and record.get("duration_ms") is not None


def summarize(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """按 event + workload/role/phase 分层，并同时输出 raw 与 Tukey filtered 统计。"""
    groups: dict[tuple[str, str, str, str], list[dict[str, Any]]] = {}
    for record in records:
        if not successful_record(record):
            continue
        key = (str(record.get("source", "unknown")), str(record.get("stage", "unknown")),
               str(record.get("event", "unknown")), record_stratum(record))
        sample = dict(record)
        sample["sample_id"] = sample_identifier(record)
        groups.setdefault(key, []).append(sample)
    rows: list[dict[str, Any]] = []
    for (source, stage, event, stratum), samples in sorted(groups.items()):
        stats = tukey_statistics(samples)
        rows.append({"source": source, "stage": stage, "event": event,
                     "stratum": stratum, "count": stats["raw_count"],
                     "mean_ms": stats["raw_mean_ms"], **stats})
    return rows


def event_samples(records: list[dict[str, Any]], event: str) -> list[dict[str, Any]]:
    return [{**record, "sample_id": sample_identifier(record)} for record in records
            if record.get("source") == "geth" and record.get("event") == event and
            successful_record(record)]


def compute_segment_spans(progress: list[dict[str, Any]], stage: str,
                          start_event: str, end_event: str) -> list[float]:
    """配对脚本锚点（同 join_key）的 start/end，返回各 span 的毫秒数。"""
    by_key: dict[str, dict[str, float]] = {}
    for record in progress:
        if record.get("stage") != stage:
            continue
        event = str(record.get("event"))
        ts = record.get("timestamp_s")
        if ts is None:
            continue
        join = record.get("join_key", stage)  # system_init 固定 dkg-1；rotation 用 sessionID
        slot = by_key.setdefault(str(join), {})
        if event == start_event and "start" not in slot:
            slot["start"] = float(ts)
        elif event == end_event and str(record.get("status", "ok")) == "ok":
            slot["end"] = float(ts)
    spans: list[float] = []
    for slot in by_key.values():
        start, end = slot.get("start"), slot.get("end")
        if start is not None and end is not None and end >= start:
            spans.append((end - start) * 1000)
    return spans


def compute_rotation_samples(records: list[dict[str, Any]], required_shares: int) -> list[dict[str, Any]]:
    """Match SGX L5 rotation timing from geth business-event logs.

    Each session starts at its earliest successful ``reshare_start`` timestamp and
    ends at the ``required_shares``-th successful extraction on a distinct node.
    Script-side round anchors deliberately do not participate: they describe RPC
    orchestration and all-node polling rather than DKRS protocol completion.
    """
    if required_shares < 1:
        raise ValueError("rotation required_shares must be at least 1")

    starts: dict[str, dict[str, Any]] = {}
    extractions: dict[str, list[dict[str, Any]]] = {}
    for record in records:
        if (record.get("source") != "geth" or record.get("stage") != "rotation_flow" or
                not successful_record(record) or not bool(record.get("measurement_window"))):
            continue
        session = record.get("join_key")
        timestamp = record.get("timestamp_s")
        if session is None or timestamp is None:
            continue
        session = str(session)
        if record.get("event") == "reshare_start":
            previous = starts.get(session)
            if previous is None or float(timestamp) < float(previous["timestamp_s"]):
                starts[session] = record
        elif record.get("event") == "reshare_extract_apply":
            extractions.setdefault(session, []).append(record)

    samples: list[dict[str, Any]] = []
    for session, start in sorted(starts.items()):
        start_ts = float(start["timestamp_s"])
        candidates = sorted(
            (record for record in extractions.get(session, [])
             if float(record["timestamp_s"]) >= start_ts),
            key=lambda record: (float(record["timestamp_s"]), str(record.get("source_file", "")),
                                str(record.get("node_id", record.get("node", "")))),
        )
        unique: list[dict[str, Any]] = []
        seen_nodes: set[str] = set()
        for record in candidates:
            # node_id comes from the archived geth filename and remains stable even
            # when legacy log lines do not carry the node field.
            node_key = str(record.get("node_id", record.get("node", record.get("source_file", ""))))
            if node_key in seen_nodes:
                continue
            seen_nodes.add(node_key)
            unique.append(record)
        if len(unique) < required_shares:
            continue
        end = unique[required_shares - 1]
        samples.append({
            "duration_ms": (float(end["timestamp_s"]) - start_ts) * 1000,
            "sample_id": session,
            "session_id": session,
            "required_shares": required_shares,
            "extracted_count": len(unique),
            "start_source_file": start.get("source_file"),
            "end_source_file": end.get("source_file"),
            "start_timestamp_s": start_ts,
            "end_timestamp_s": float(end["timestamp_s"]),
        })
    return samples


def compute_rotation_global_samples(records: list[dict[str, Any]],
                                    progress: list[dict[str, Any]] | None = None) -> list[dict[str, Any]]:
    """Global rotation wall-clock: new-epoch boundary arrival to latest extraction.

    Start = the new epoch's boundary BLOCK time (chain-authoritative, read by the
    orchestrator from eth_getBlockByNumber), carried on the script-side
    round_start anchor as boundary_block_ts. Nitch has no contract: the boundary
    is block initial + target*length. Falls back to the earliest engine
    reshare_start when boundary_block_ts is absent (older runs)."""
    # Map session_id -> boundary block timestamp (epoch arrival).
    boundary_ts: dict[str, float] = {}
    for rec in progress or []:
        if rec.get("stage") != "rotation_flow" or rec.get("event") != "round_start":
            continue
        sid = rec.get("join_key")
        raw = rec.get("boundary_block_ts")
        if sid is None or raw in (None, ""):
            continue
        try:
            boundary_ts[str(sid)] = float(raw)
        except (TypeError, ValueError):
            pass
    sessions: dict[str, dict[str, list[dict[str, Any]]]] = {}
    for record in records:
        if (record.get("source") != "geth" or record.get("stage") != "rotation_flow" or
                not successful_record(record) or not bool(record.get("measurement_window"))):
            continue
        session = record.get("join_key")
        timestamp = record.get("timestamp_s")
        if session is None or timestamp is None:
            continue
        bucket = sessions.setdefault(str(session), {"starts": [], "applies": []})
        if record.get("event") == "reshare_start":
            bucket["starts"].append(record)
        elif record.get("event") == "reshare_extract_apply":
            bucket["applies"].append(record)

    samples: list[dict[str, Any]] = []
    for session, bucket in sorted(sessions.items()):
        starts, applies = bucket["starts"], bucket["applies"]
        if not starts or not applies:
            continue
        start = min(starts, key=lambda record: float(record["timestamp_s"]))
        end = max(applies, key=lambda record: float(record["timestamp_s"]))
        end_ts = float(end["timestamp_s"])
        if session in boundary_ts:
            start_ts = boundary_ts[session]          # chain-authoritative epoch arrival
            start_source = "boundary_block"
        else:
            start_ts = float(start["timestamp_s"])   # fallback: earliest engine reshare_start
            start_source = "reshare_start"
        if end_ts < start_ts:
            continue
        samples.append({
            "duration_ms": (end_ts - start_ts) * 1000,
            "sample_id": session,
            "session_id": session,
            "start_source_file": start.get("source_file"),
            "end_source_file": end.get("source_file"),
            "start_timestamp_s": start_ts,
            "end_timestamp_s": end_ts,
            "start_source": start_source,
            "start_node_count": len({str(r.get("node_id", r.get("node", r.get("source_file")))) for r in starts}),
            "apply_node_count": len({str(r.get("node_id", r.get("node", r.get("source_file")))) for r in applies}),
        })
    return samples


def compute_rotation_node_samples(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Single-node rotation observations.

    Each sample is ONE committee node's own pipeline within ONE session: its
    first successful ``reshare_start`` -> its own first successful
    ``reshare_extract_apply``. Deliberately no cross-node mixing: trigger
    propagation skew and the threshold-crossing quantile are cross-node
    artifacts and stay in ``rotation_flow_span`` (legacy quorum口径).
    """
    slots: dict[tuple[str, str], dict[str, Any]] = {}
    for record in records:
        if (record.get("source") != "geth" or record.get("stage") != "rotation_flow" or
                not successful_record(record) or not bool(record.get("measurement_window"))):
            continue
        session = record.get("join_key")
        timestamp = record.get("timestamp_s")
        node = record.get("node_id", record.get("node", record.get("source_file")))
        if session is None or timestamp is None or node is None:
            continue
        slot = slots.setdefault((str(session), str(node)), {})
        event = record.get("event")
        if event == "reshare_start":
            if "start" not in slot or float(timestamp) < float(slot["start"]["timestamp_s"]):
                slot["start"] = record
        elif event == "reshare_extract_apply":
            if "end" not in slot or float(timestamp) < float(slot["end"]["timestamp_s"]):
                slot["end"] = record
    samples: list[dict[str, Any]] = []
    for (session, node), slot in sorted(slots.items()):
        start, end = slot.get("start"), slot.get("end")
        if start is None or end is None:
            continue
        start_ts, end_ts = float(start["timestamp_s"]), float(end["timestamp_s"])
        if end_ts < start_ts:
            continue
        samples.append({
            "duration_ms": (end_ts - start_ts) * 1000,
            "sample_id": f"{session}#{node}",
            "session_id": session,
            "node_id": node,
            "start_source_file": start.get("source_file"),
            "end_source_file": end.get("source_file"),
            "start_timestamp_s": start_ts,
            "end_timestamp_s": end_ts,
        })
    return samples


def compute_rotation_round_samples(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Round-completion rotation timing (all-nodes-done口径).

    One sample per session: the SLOWEST committee node's own span
    (own reshare_start -> own reshare_extract_apply). A round is only
    finished when every node has applied, so the round's duration is the
    max over nodes, not the median experience and not the quorum point.
    ``convergence_ms`` additionally records wall-clock
    (earliest start -> last apply) for reference.
    """
    slots: dict[tuple[str, str], dict[str, Any]] = {}
    for record in records:
        if (record.get("source") != "geth" or record.get("stage") != "rotation_flow" or
                not successful_record(record) or not bool(record.get("measurement_window"))):
            continue
        session = record.get("join_key")
        timestamp = record.get("timestamp_s")
        node = record.get("node_id", record.get("node", record.get("source_file")))
        if session is None or timestamp is None or node is None:
            continue
        slot = slots.setdefault((str(session), str(node)), {})
        event = record.get("event")
        if event == "reshare_start":
            if "start" not in slot or float(timestamp) < float(slot["start"]["timestamp_s"]):
                slot["start"] = record
        elif event == "reshare_extract_apply":
            if "end" not in slot or float(timestamp) < float(slot["end"]["timestamp_s"]):
                slot["end"] = record
    rounds: dict[str, dict[str, Any]] = {}
    for (session, node), slot in sorted(slots.items()):
        start, end = slot.get("start"), slot.get("end")
        if start is None or end is None:
            continue
        start_ts, end_ts = float(start["timestamp_s"]), float(end["timestamp_s"])
        if end_ts < start_ts:
            continue
        span_ms = (end_ts - start_ts) * 1000
        round_ = rounds.setdefault(session, {"nodes": [], "start_ts": [], "apply_ts": []})
        round_["nodes"].append((span_ms, node))
        round_["start_ts"].append(start_ts)
        round_["apply_ts"].append(end_ts)
    samples: list[dict[str, Any]] = []
    for session, round_ in sorted(rounds.items()):
        if not round_["nodes"]:
            continue
        span_ms, node = max(round_["nodes"])
        first_start = min(round_["start_ts"])
        last_apply = max(round_["apply_ts"])
        samples.append({
            "duration_ms": span_ms,
            "sample_id": session,
            "session_id": session,
            "slowest_node": node,
            "node_count": len(round_["nodes"]),
            "convergence_ms": (last_apply - first_start) * 1000,
        })
    return samples


def compute_ledger_spans(progress: list[dict[str, Any]]) -> tuple[list[float], list[float]]:
    by_key: dict[str, dict[str, float]] = {}
    for record in progress:
        if record.get("stage") != "ledger_update_e2e" or not record.get("join_key"):
            continue
        event = str(record.get("event"))
        ts = record.get("timestamp_s")
        if ts is None:
            continue
        slot = by_key.setdefault(str(record["join_key"]), {})
        if event == "proposal_submitted" and "submitted" not in slot:
            slot["submitted"] = float(ts)
        elif event == "apply_observed" and "observed" not in slot:
            slot["observed"] = float(ts)
        elif event == "verified_all" and str(record.get("status", "ok")) == "ok":
            slot["verified"] = float(ts)
    end_to_end: list[float] = []
    convergence: list[float] = []
    for slot in by_key.values():
        submitted, observed, verified = slot.get("submitted"), slot.get("observed"), slot.get("verified")
        if submitted is not None and verified is not None and verified >= submitted:
            end_to_end.append((verified - submitted) * 1000)
        if observed is not None and verified is not None and verified >= observed:
            convergence.append((verified - observed) * 1000)
    return end_to_end, convergence


LEDGER_LOCAL_EVENTS = ("lu_getblock", "lu_validate", "lu_update")


def compute_ledger_update_samples(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Sum complete local stages for each successfully applied receiver/request."""
    stages: dict[tuple[str, str], dict[str, dict[str, Any]]] = {}
    for record in records:
        if (not record.get("measurement_window") or record.get("status") != "ok" or
                record.get("node") == "proposer" or not record.get("join_key") or
                record.get("stage") != "ledger_update" or
                int(record.get("marker_schema") or 0) != ALIGNED_MARKER_SCHEMA or
                record.get("event") not in LEDGER_LOCAL_EVENTS):
            continue
        node = record.get("node_id") or record.get("source_file")
        duration = record.get("duration_ms")
        if (not node or duration is None or not math.isfinite(float(duration)) or
                float(duration) < 0 or record.get("block") is None or
                record.get("tx_index") is None):
            continue
        key = (str(node), str(record["join_key"]))
        stages.setdefault(key, {}).setdefault(record["event"], record)
    samples = []
    for completed in compute_ledger_update_total_samples(records):
        key = (str(completed.get("node_id") or completed.get("source_file")),
               str(completed["join_key"]))
        parts = stages.get(key, {})
        if not all(event in parts for event in LEDGER_LOCAL_EVENTS):
            continue
        targets = {(part["block"], part["tx_index"]) for part in parts.values()}
        if len(targets) != 1:
            continue
        breakdown = {event: float(parts[event]["duration_ms"]) for event in LEDGER_LOCAL_EVENTS}
        samples.append({**completed, "event": "local_apply_sum",
                        "marker_schema": ALIGNED_MARKER_SCHEMA,
                        "duration_ms": sum(breakdown.values()),
                        "local_breakdown_ms": breakdown})
    return samples


def compute_ledger_update_total_samples(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Receiver-local handling spans, independent of committee/observer role."""
    samples: dict[tuple[str, str], dict[str, Any]] = {}
    starts: dict[tuple[str, str], dict[str, Any]] = {}
    ends: dict[tuple[str, str], dict[str, Any]] = {}
    for record in records:
        if (not record.get("measurement_window") or
                str(record.get("status", "")) != "ok" or
                record.get("node") == "proposer" or not record.get("join_key")):
            continue
        node = record.get("node_id") or record.get("source_file")
        if not node:
            continue
        key = (str(node), str(record["join_key"]))
        event = record.get("event")
        if record.get("stage") == "edit_execution" and event == "redaction_edit_applied":
            # Receiver duration starts at modStart in TxModifyMsg. The proposer
            # uses the same event but starts at RPC entry and must not enter here.
            if record.get("node") is None or record.get("duration_ms") is None:
                continue
            duration = float(record["duration_ms"])
            if math.isfinite(duration) and duration >= 0:
                samples.setdefault(key, {**record, "sample_id": f"receiver:{key[0]}:{key[1]}"})
        elif (record.get("stage") == "ledger_update" and
              int(record.get("marker_schema") or 0) == ALIGNED_MARKER_SCHEMA):
            if any(record.get(field) is None for field in ("block", "tx_index", "timestamp_s")):
                continue
            if event == "tx_modify_received":
                starts.setdefault(key, record)
            elif event == "local_block_updated" and record.get("block_hash"):
                ends.setdefault(key, record)
    # Versioned start/end markers also cover receivers without the legacy event.
    # Join by node AND request: the same request is applied at every receiver.
    for key, start in starts.items():
        end = ends.get(key)
        if key in samples or not end or end["timestamp_s"] < start["timestamp_s"]:
            continue
        if end.get("block") != start.get("block") or end.get("tx_index") != start.get("tx_index"):
            continue
        duration = end.get("duration_ms")
        if duration is None:
            duration = (float(end["timestamp_s"]) - float(start["timestamp_s"])) * 1000
        duration = float(duration)
        if math.isfinite(duration) and duration >= 0:
            samples[key] = {**end, "duration_ms": duration,
                            "sample_id": f"receiver:{key[0]}:{key[1]}"}
    return list(samples.values())


def compute_edit_spans(records: list[dict[str, Any]], progress: list[dict[str, Any]]) -> list[float]:
    """编辑端到端：每个 reqID，submit(script submit 锚点) → 各节点 edit_applied 的 max(ts)，span=max−min。"""
    submit_ts: dict[str, float] = {}
    for record in progress:
        if record.get("stage") == "edit_execution" and record.get("event") == "submit":
            join = record.get("join_key")
            ts = record.get("timestamp_s")
            if join and ts is not None:
                submit_ts.setdefault(str(join), float(ts))
    applied: dict[str, list[float]] = {}
    for record in records:
        if record.get("stage") == "edit_execution" and record.get("event") == "redaction_edit_applied":
            join = record.get("join_key")
            ts = record.get("timestamp_s")
            if join and ts is not None:
                applied.setdefault(str(join), []).append(float(ts))
    spans: list[float] = []
    for join, start in submit_ts.items():
        ends = applied.get(join, [])
        if not ends:
            continue
        end = max(ends)
        if end >= start:
            spans.append((end - start) * 1000)
    return spans


def stage_row(stage: str, samples: list[dict[str, Any]], basis: str, note: str,
              stratum: str = "{}") -> dict[str, Any]:
    stats = tukey_statistics(samples)
    raw_mean = stats["raw_mean_ms"]
    duration_s = round(float(raw_mean) / 1000, 6) if raw_mean != "" else ""
    return {
        "stage": stage,
        "duration_ms": raw_mean,
        "duration_s": duration_s,
        "count": stats["raw_count"],
        "stratum": stratum,
        "basis": basis,
        "note": note if samples else f"{note}; stage not present in comparable data",
        **stats,
    }


def numeric_samples(values: list[float], prefix: str) -> list[dict[str, Any]]:
    return [{"duration_ms": value, "sample_id": f"{prefix}:{index}"}
            for index, value in enumerate(values)]


def aligned_block_samples(records: list[dict[str, Any]], event: str) -> tuple[list[dict[str, Any]], str]:
    candidates = [record for record in event_samples(records, event)
                  if int(record.get("marker_schema") or 0) == ALIGNED_MARKER_SCHEMA and
                  bool(record.get("measurement_window")) and int(record.get("txs") or 0) > 0 and
                  record.get("block_hash") and record.get("block") is not None]
    # Aggregate every successful non-empty block, matching the tgovern L5
    # collector. Workload dimensions remain in the per-event diagnostic rows;
    # the headline must not silently discard valid tx-count/role strata.
    stratum = json.dumps({"batch_class": "nonempty", "node_role": "all",
                          "run_phase": "measurement"},
                         sort_keys=True, separators=(",", ":"))
    return candidates, stratum


def compute_stage_rows(records: list[dict[str, Any]], progress: list[dict[str, Any]],
                       run_metadata: dict[str, Any] | None = None) -> list[dict[str, Any]]:
    metadata = run_metadata or {}
    rows: list[dict[str, Any]] = []
    for stage, (headline, agg, basis) in STAGE_HEADLINE.items():
        if agg == "mean":
            samples = event_samples(records, headline)
            rows.append(stage_row(stage, samples, basis, f"headline event={headline}"))
        elif agg == "aligned_block":
            samples, stratum = aligned_block_samples(records, headline)
            rows.append(stage_row(stage, samples, basis,
                                  f"event={headline}; all successful nonempty blocks", stratum))
        elif agg == "segment_span":
            spans = compute_segment_spans(progress, stage, "phase_start", "phase_end")
            rows.append(stage_row(stage, numeric_samples(spans, stage), basis,
                                  f"{len(spans)} paired spans"))
        elif agg == "rotation_quorum":
            required_shares = int((metadata.get("threshold") or {}).get("required_shares") or 0)
            samples = compute_rotation_samples(records, required_shares) if required_shares else []
            rows.append(stage_row(stage, samples, basis,
                                  f"{len(samples)} sessions; required_shares={required_shares}; "
                                  "script round_start/round_end retained only as orchestration diagnostics"))
        elif agg == "rotation_global":
            samples = compute_rotation_global_samples(records, progress)
            rows.append(stage_row(stage, samples, basis,
                                  f"{len(samples)} sessions; earliest start→latest apply"))
        elif agg == "rotation_node":
            samples = compute_rotation_node_samples(records)
            rows.append(stage_row(stage, samples, basis,
                                  f"{len(samples)} node observations; per-node own start→own apply"))
        elif agg == "rotation_round":
            samples = compute_rotation_round_samples(records)
            rows.append(stage_row(stage, samples, basis,
                                  f"{len(samples)} rounds; slowest node per round (all-done)"))
        elif agg == "e2e_span":
            spans = compute_edit_spans(records, progress)
            rows.append(stage_row(stage, numeric_samples(spans, stage), basis,
                                  f"{len(spans)} reqIDs end-to-end"))
        elif agg in ("receiver_update", "receiver_update_total"):
            totals = compute_ledger_update_total_samples(records)
            samples = compute_ledger_update_samples(records) if agg == "receiver_update" else totals
            stratum = json.dumps({"node_role": "all_receivers", "run_phase": "measurement"},
                                 sort_keys=True, separators=(",", ":"))
            note = f"{len(samples)} receiver-local updates; proposer excluded"
            if agg == "receiver_update":
                note += f"; missing local stages={len(totals) - len(samples)}; no total-span fallback"
            rows.append(stage_row(stage, samples, basis, note, stratum))
        elif agg in ("ledger_e2e", "ledger_convergence"):
            spans, convergence = compute_ledger_spans(progress)
            selected = spans if agg == "ledger_e2e" else convergence
            polling = (metadata.get("ledger_update_e2e") or {}).get("polling_interval_ms")
            rows.append(stage_row(stage, numeric_samples(selected, stage), basis,
                                  f"{len(selected)} verified reqIDs; polling_interval_ms={polling}"))
    return rows


# ---------- 完整性告警（验收 1/2） ----------
def completeness_report(records: list[dict[str, Any]], progress: list[dict[str, Any]],
                        stage_rows: list[dict[str, Any]], summary: list[dict[str, Any]],
                        run_metadata: dict[str, Any] | None = None) -> list[str]:
    alerts: list[str] = []

    metadata = run_metadata or {}
    observer = metadata.get("ledger_observer") or {}

    # 1) 每 stage 期望 headline event 齐全 + count>0，包括所有接收节点的本地更新。
    for stage_row_ in stage_rows:
        stage = stage_row_["stage"]
        if stage_row_["count"] == 0 or stage_row_["duration_ms"] == "":
            alerts.append(f"[count=0] stage={stage} 无可用样本（headline={STAGE_HEADLINE[stage][0]}）— 可能上一轮遗漏打点")

    # 2) anchor 成对（phase_start/phase_end、round_start/round_end、submit/phase_end）
    anchor_pairs = [("system_init", "phase_start", "phase_end"),
                    ("rotation_flow", "round_start", "round_end"),
                    ("ledger_update_e2e", "proposal_submitted", "verified_all")]
    for stage, ev_s, ev_e in anchor_pairs:
        starts = [r for r in progress if r.get("stage") == stage and r.get("event") == ev_s]
        ends = [r for r in progress if r.get("stage") == stage and r.get("event") == ev_e]
        if len(starts) != len(ends):
            alerts.append(f"[anchor unpaired] stage={stage} {ev_s}×{len(starts)} vs {ev_e}×{len(ends)} 不成对")

    failed_updates = [r for r in progress if r.get("stage") == "ledger_update_e2e"
                      and r.get("event") in ("verification_failed", "target_selection_failed")]
    if failed_updates:
        alerts.append(f"[ledger verification failed] {len(failed_updates)} 个账本更新/目标选择失败")

    # 3) run_id 一致
    run_ids = {r.get("run_id") for r in progress if r.get("run_id")}
    if len(run_ids) > 1:
        alerts.append(f"[run_id inconsistent] 出现多个 run_id: {sorted(run_ids)}")

    observer_failures = [record for record in records
                         if record.get("stage") == "ledger_update" and
                         record.get("event") == "local_block_update_failed"]
    if observer_failures:
        alerts.append(f"[ledger observer failed] {len(observer_failures)} 个 ON 本地更新失败")

    threshold = metadata.get("threshold") or {}
    expected_shares = threshold.get("required_shares")
    if expected_shares is not None:
        for record in progress:
            if (record.get("stage") in ("system_init", "rotation_flow") and
                    record.get("event") in ("phase_end", "round_end") and
                    str(record.get("status", "ok")) == "ok"):
                if int(record.get("required_shares", -1)) != int(expected_shares):
                    alerts.append(f"[threshold mismatch] {record.get('stage')} "
                                  f"required_shares={record.get('required_shares')} "
                                  f"expected={expected_shares}")
    topology = metadata.get("topology") or {}
    if topology and (topology.get("acceptance") or {}).get("status") != "ok":
        alerts.append("[topology acceptance failed] 稀疏拓扑未通过预期邻居验收")
    relay_ttl = metadata.get("relay_ttl") or {}
    diameter = topology.get("diameter")
    if diameter is not None:
        for protocol in ("distkg", "reshare", "modify"):
            ttl = relay_ttl.get(protocol)
            if ttl is not None and int(ttl) < int(diameter) + 1:
                alerts.append(f"[relay TTL too small] {protocol}={ttl} < diameter+1={int(diameter)+1}")
    if topology.get("mode") == "sparse" and int(topology.get("node_count", 0)) > 1:
        relay = metadata.get("relay") or {}
        for protocol in ("distkg", "reshare", "modify"):
            if int((relay.get(protocol) or {}).get("relay_events", 0)) == 0:
                alerts.append(f"[relay missing] sparse run 未观察到 {protocol} relay 日志")

    return list(dict.fromkeys(alerts))


def outlier_integrity_warnings(summary: list[dict[str, Any]],
                               stage_rows: list[dict[str, Any]]) -> list[str]:
    warnings: list[str] = []
    for row in list(summary) + list(stage_rows):
        rate = float(row.get("excluded_rate") or 0)
        if rate > 0.05:
            warnings.append(f"[outlier exclusion >5%] stage={row.get('stage')} "
                            f"event={row.get('event', STAGE_HEADLINE.get(row.get('stage'), ('',))[0])} "
                            f"stratum={row.get('stratum', '{}')} excluded_rate={rate:.2%}")
    return list(dict.fromkeys(warnings))


# ---------- 输出 ----------
def write_csv(path: Path, rows: list[dict[str, Any]], fields: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as fp:
        writer = csv.DictWriter(fp, fieldnames=fields)
        writer.writeheader()
        writer.writerows({field: row.get(field, "") for field in fields} for row in rows)


def write_report(path: Path, stage_rows: list[dict[str, Any]], summary: list[dict[str, Any]],
                 alerts: list[str], integrity_warnings: list[str], counts: dict[str, int],
                 run_metadata: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    lines: list[str] = ["# 性能测试报告 (redactable-geth)", ""]
    lines.append(f"- geth E2E_TIMING 记录数：{counts['geth']}")
    lines.append(f"- 脚本 PERF_ANCHOR 记录数：{counts['progress']}")
    topology = run_metadata.get("topology") or {}
    run_counts = run_metadata.get("counts") or {}
    ttl = run_metadata.get("relay_ttl") or {}
    threshold = run_metadata.get("threshold") or {}
    observer = run_metadata.get("ledger_observer") or {}
    schema = run_metadata.get("timing_schema") or {}
    if run_metadata.get("run_id"):
        lines.append(f"- run_id：`{run_metadata['run_id']}`")
    if run_counts:
        lines.append(f"- 节点：total={run_counts.get('total', 0)}, core={run_counts.get('core', 0)}, "
                     f"light={run_counts.get('light', 0)}, committee={run_counts.get('committee', 0)}, "
                     f"ledger_observer={run_counts.get('ledger_observer', 0)}")
        lines.append(f"- 验收节点：{run_metadata.get('verification_nodes', [])}；"
                     f"light_verified={run_metadata.get('light_verified', 0)}/{run_metadata.get('light_total', 0)}")
    if threshold:
        lines.append(f"- 门限：required_shares={threshold.get('required_shares')}，"
                     f"polynomial_degree={threshold.get('polynomial_degree')}")
    lines.append(f"- ON-equivalent observer：index={observer.get('index')}，role={observer.get('role')}，"
                 f"headline_comparable={observer.get('headline_comparable', False)}")
    polling = (run_metadata.get("ledger_update_e2e") or {}).get("polling_interval_ms")
    lines.append(f"- 全网账本轮询间隔：{polling} ms；E2E/收敛指标分辨率受此间隔限制")
    lines.append(f"- Timing schema：collector=v{TIMING_SCHEMA_VERSION}，"
                 f"markers={schema.get('observed_marker_versions')}，"
                 f"SGX L5 headline comparable={schema.get('sgx_l5_headline_comparable', False)}")
    lines.append(f"- Schema 说明：{schema.get('compatibility_note', 'unknown')}")
    if topology:
        lines.append(f"- 拓扑：mode={topology.get('mode')}, edges={len(topology.get('edges') or [])}, "
                     f"max_degree={topology.get('max_degree')}, diameter={topology.get('diameter')}, "
                     f"acceptance={(topology.get('acceptance') or {}).get('status')}")
        lines.append(f"- Relay TTL：distkg={ttl.get('distkg')}, reshare={ttl.get('reshare')}, "
                     f"modify={ttl.get('modify')}（minimum={ttl.get('minimum')}）")
    lines.append("")
    lines.append("## 1. 对齐后的 headline 与诊断指标（timing-stages.csv, schema v3）")
    lines.append("")
    lines.append("| stage | raw | used | excluded | raw_mean_ms | filtered_mean_ms | p50_ms | p95_ms | p99_ms | max_ms |")
    lines.append("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
    for row in stage_rows:
        lines.append(f"| {row['stage']} | {row['raw_count']} | {row['used_count']} | "
                     f"{row['excluded_count']} | {row['raw_mean_ms'] or '-'} | "
                     f"{row['filtered_mean_ms'] or '-'} | {row['p50_ms'] or '-'} | "
                     f"{row['p95_ms'] or '-'} | {row['p99_ms'] or '-'} | {row['max_ms'] or '-'} |")
    if any(row["stage"] == "ledger_update" and row["count"] == 0 for row in stage_rows):
        lines.append("")
        lines.append("- 未形成接收节点的 `ledger_update` headline；请检查接收处理完成标记。")
    lines.append("")
    lines.append("- `ledger_update` 对每个成功应用的接收节点/请求，求 "
                 "`lu_getblock + lu_validate + lu_update` 的 duration_ns 之和，排除 proposer；"
                 "必须三段齐全，不使用完整 span 回填。`ledger_update_total` 保留原接收端 "
                 "`redaction_edit_applied.duration_ns` 完整处理区间。"
                 "`ledger_update_e2e` 与 `ledger_convergence` 另列为全网验收/收敛诊断。")
    lines.append("- `block_generation` 使用正式窗口内全部成功、字段完整的 v3 非空块，与 SGX L5 collector 口径一致；tx 数量和节点角色保留在明细分层中。")
    lines.append("")
    lines.append("## 2. 私有协议 Relay / TTL")
    lines.append("")
    lines.append("| protocol | relay_events | sent | failed | duplicates | ttl_exhausted | max_hop | unique_messages |")
    lines.append("|---|---:|---:|---:|---:|---:|---:|---:|")
    for protocol in ("distkg", "reshare", "modify"):
        item = (run_metadata.get("relay") or {}).get(protocol) or {}
        lines.append(f"| {protocol} | {item.get('relay_events', 0)} | {item.get('sent', 0)} | "
                     f"{item.get('failed', 0)} | {item.get('duplicates', 0)} | "
                     f"{item.get('ttl_exhausted', 0)} | {item.get('max_hop', 0)} | "
                     f"{item.get('unique_messages', 0)} |")
    lines.append("")
    lines.append("- 以上为归档日志中可观察到的事件；低 verbosity 轻节点可能抑制 DEBUG relay 行。"
                 "`ttl_exhausted` 表示环路末端被有界丢弃，不等同于协议失败。")
    lines.append("")
    lines.append("## 3. 完整性告警")
    lines.append("")
    if not alerts:
        lines.append("- ✅ 无致命完整性错误：event/anchor/角色/门限/拓扑/账本验收通过。")
    else:
        for a in alerts:
            lines.append(f"- ⚠️ {a}")
    if integrity_warnings:
        lines.extend(["", "### 统计完整性告警（不使协议验收失败）", ""])
        for warning in integrity_warnings:
            lines.append(f"- ⚠️ {warning}")
    lines.append("")
    lines.append("## 4. 全 event 分层明细（timing-summary.csv 摘要）")
    lines.append("")
    lines.append("| source | stage | event | stratum | raw | used | excluded | raw_mean_ms | filtered_mean_ms | p50_ms | p95_ms | p99_ms | max_ms |")
    lines.append("|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
    for row in summary:
        lines.append(f"| {row['source']} | {row['stage']} | {row['event']} | `{row['stratum']}` | "
                     f"{row['raw_count']} | {row['used_count']} | {row['excluded_count']} | "
                     f"{row['raw_mean_ms']} | {row['filtered_mean_ms']} | {row['p50_ms']} | "
                     f"{row['p95_ms']} | {row['p99_ms']} | {row['max_ms']} |")
    excluded = [(row.get("stage"), row.get("event", STAGE_HEADLINE.get(row.get("stage"), ("",))[0]),
                 sample) for row in list(stage_rows) + list(summary)
                for sample in row.get("excluded_samples") or []]
    lines.extend(["", "## 5. Tukey far-outlier 排除清单", ""])
    if not excluded:
        lines.append("- 无排除样本；少于 30 个样本的分层按规范不执行排除。")
    else:
        for stage, event, sample in excluded:
            lines.append(f"- `{stage}/{event}` `{sample['sample_id']}`：{sample['duration_ms']} ms")
    lines.append("")
    path.write_text("\n".join(lines), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description="Collect redactable-geth perf timing data")
    parser.add_argument("--logs-dir", type=Path, default=DEFAULT_LOGS)
    parser.add_argument("--progress", type=Path, default=DEFAULT_PROGRESS)
    parser.add_argument("--out-dir", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--manifest", type=Path,
                        help="run manifest（默认 <out-dir>/run-manifest.json）")
    parser.add_argument("--year", type=int, default=datetime.now().year)
    parser.add_argument("--strict", action="store_true", help="完整性告警时返回非零")
    args = parser.parse_args()

    relay_stats = collect_relay_stats(args.logs_dir)
    manifest_path = args.manifest or args.out_dir / "run-manifest.json"
    run_metadata = load_run_metadata(manifest_path, relay_stats)
    geth_records = load_geth_records(args.logs_dir, args.year)
    progress_records = load_progress_records(args.progress, args.year)
    annotate_record_context(geth_records, progress_records, run_metadata)
    run_metadata["timing_schema"] = timing_schema_status(geth_records, run_metadata)
    records = geth_records + progress_records

    summary = summarize(geth_records)
    stage_rows = compute_stage_rows(geth_records, progress_records, run_metadata)
    alerts = completeness_report(geth_records, progress_records, stage_rows, summary, run_metadata)
    integrity_warnings = outlier_integrity_warnings(summary, stage_rows)

    args.out_dir.mkdir(parents=True, exist_ok=True)
    out_json = args.out_dir / "timing-summary.json"
    out_summary_csv = args.out_dir / "timing-summary.csv"
    out_stages_csv = args.out_dir / "timing-stages.csv"
    out_report = args.out_dir / "timing-report.md"
    out_outliers = args.out_dir / "timing-outliers.json"

    out_json.write_text(json.dumps({
        "schema_version": TIMING_SCHEMA_VERSION,
        "records": records,
        "summary": summary,
        "stages": stage_rows,
        "alerts": alerts,
        "integrity_warnings": integrity_warnings,
        "run_metadata": run_metadata,
    }, ensure_ascii=False, indent=2), encoding="utf-8")
    out_outliers.write_text(json.dumps({
        "schema_version": TIMING_SCHEMA_VERSION,
        "policy": "Tukey far-outlier, 3*IQR, disabled for strata with raw_count < 30",
        "excluded": [{
            "stage": row.get("stage"),
            "event": row.get("event", STAGE_HEADLINE.get(row.get("stage"), ("",))[0]),
            "stratum": row.get("stratum", "{}"),
            "excluded_rate": row.get("excluded_rate", 0),
            "samples": row.get("excluded_samples") or [],
        } for row in list(stage_rows) + list(summary) if row.get("excluded_samples")],
    }, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    write_csv(out_summary_csv, summary,
              ["source", "stage", "event", "stratum", "count", "raw_count", "used_count",
               "excluded_count", "excluded_rate", "min_ms", "mean_ms", "raw_mean_ms",
               "filtered_mean_ms", "p50_ms", "p95_ms", "p99_ms", "max_ms",
               "lower_fence_ms", "upper_fence_ms", "excluded_samples"])
    write_csv(out_stages_csv, stage_rows,
              ["stage", "duration_ms", "duration_s", "count", "stratum", "raw_count",
               "used_count", "excluded_count", "excluded_rate", "raw_mean_ms",
               "filtered_mean_ms", "p50_ms", "p95_ms", "p99_ms", "max_ms",
               "lower_fence_ms", "upper_fence_ms", "basis", "note", "excluded_samples"])
    write_report(out_report, stage_rows, summary, alerts, integrity_warnings,
                 {"geth": len(geth_records), "progress": len(progress_records)}, run_metadata)

    print(f"records: {len(records)} (geth={len(geth_records)}, script={len(progress_records)})")
    print(f"summary_json: {out_json}")
    print(f"summary_csv:  {out_summary_csv}")
    print(f"stages_csv:   {out_stages_csv}")
    print(f"report_md:    {out_report}")
    print(f"outliers_json:{out_outliers}")
    print("\n=== per-stage duration ===")
    print(f"{'stage':<24}{'duration_s':>12}{'count':>8}  basis")
    print("-" * 100)
    for row in stage_rows:
        dur = row["duration_s"] if row["duration_s"] != "" else "-"
        print(f"{row['stage']:<24}{str(dur):>12}{row['count']:>8}  {row['basis']}")
    if alerts:
        print("\n=== completeness alerts ===")
        for a in alerts:
            print(f"⚠️  {a}")
    else:
        print("\n✅ completeness: all stages present, anchors paired, run_id consistent")
    if integrity_warnings:
        print("\n=== statistical integrity warnings (non-fatal) ===")
        for warning in integrity_warnings:
            print(f"⚠️  {warning}")
    return 2 if args.strict and alerts else 0


if __name__ == "__main__":
    raise SystemExit(main())
