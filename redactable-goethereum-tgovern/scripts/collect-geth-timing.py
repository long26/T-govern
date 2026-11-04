#!/usr/bin/env python3
"""Collect E2E timing records from geth logs and the timing interactive script."""

from __future__ import annotations

import argparse
import csv
import fnmatch
import json
import math
import shlex
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / ".tmp" / "e2e-interactive"
DEFAULT_LOGS = WORK / "logs"
DEFAULT_TIMING_JSONL = WORK / "timing" / "interactive-e2e-timing.jsonl"


# Per-stage duration specifications.
#
# The per-event summary (timing-summary.csv) cannot answer "how long did each
# stage take" on its own: a stage is a collection of events, some of which are
# nested or are pre-trigger waits that must be excluded. STAGE_SPECS encodes the
# agreed semantics so the stage duration is computed directly from raw records
# with a fixed definition.
#
# method:
#   "representative_mean" -- mean duration_ms of one end-to-end event.
#   "sum_segments"         -- sum of mean duration_ms across a list of
#                             non-overlapping segment events (e.g. the 4 init
#                             phases that together cover the whole stage).
#   "segment_span"         -- from start_glob start_ns to end_event end_ns per
#                             round (needs script-side start_ns/end_ns), then
#                             mean across rounds.
STAGE_SPECS: list[dict[str, Any]] = [
    {"stage": "system_init", "method": "sum_segments", "source": "script",
     "events": ["phase1_prepare_total", "phase2_start_nodes_total",
                "phase3_committee_pubkey_total",
                "phase4_register_tees_and_pkch_total"]},
    {"stage": "transaction_generation", "method": "representative_mean", "source": "geth",
     "event": "geth_submit_transaction_total"},
    {"stage": "transaction_validation", "method": "representative_mean", "source": "geth",
     "event": "received_p2p_transactions"},
    {"stage": "block_generation", "method": "representative_mean", "source": "geth",
     "event": "miner_task_queued_for_sealing"},
    {"stage": "block_validation", "method": "representative_mean", "source": "geth",
     "event": "insert_chain_total"},
    {"stage": "edit_execution", "method": "representative_mean", "source": "script",
     "event": "total_on_submit_to_executed"},
    {"stage": "rotation_flow", "method": "segment_span", "source": "script",
     "start_glob": "round*.prepareRotation.*", "end_event": "vrf_receipt"},
]


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


def parse_geth_timing_line(line: str, source_file: str) -> dict[str, Any] | None:
    marker = "E2E_TIMING"
    if marker not in line:
        return None
    rest = line.split(marker, 1)[1].strip()
    record: dict[str, Any] = {"source": "geth", "source_file": source_file}
    try:
        tokens = shlex.split(rest)
    except ValueError:
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


def load_geth_records(logs_dir: Path) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for path in sorted(logs_dir.glob("geth-node-*.log")):
        with path.open(errors="replace") as fp:
            for line in fp:
                record = parse_geth_timing_line(line, path.name)
                if record is not None:
                    records.append(record)
    return records


def load_script_records(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    records: list[dict[str, Any]] = []
    with path.open(errors="replace") as fp:
        for line in fp:
            line = line.strip()
            if not line:
                continue
            record = json.loads(line)
            record["source"] = "script"
            if "duration_ms" not in record and "duration_ns" in record:
                record["duration_ms"] = int(record["duration_ns"]) / 1_000_000
            records.append(record)
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
        source = str(record.get("source", "unknown"))
        stage = str(record.get("stage", "unknown"))
        event = str(record.get("event", "unknown"))
        duration_ms = float(record.get("duration_ms", 0.0))
        groups.setdefault((source, stage, event), []).append(duration_ms)

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


def write_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as fp:
        writer = csv.DictWriter(fp, fieldnames=["source", "stage", "event", "count", "min_ms", "mean_ms", "p50_ms", "p95_ms", "max_ms"])
        writer.writeheader()
        writer.writerows(rows)


STAGE_CSV_FIELDS = ["stage", "method", "source", "duration_ms", "duration_s",
                    "basis", "count", "components", "note"]


def _round_of(record: dict[str, Any]) -> int | None:
    """Group rotation records into a round: prefer the `round` field, else parse
    a `roundN.` prefix out of the event name."""
    rnd = record.get("round")
    if rnd is not None:
        try:
            return int(rnd)
        except (TypeError, ValueError):
            return None
    event = str(record.get("event", ""))
    for n in (1, 2, 3, 4, 5):
        if event.startswith(f"round{n}."):
            return n
    return None


def _mean(values: list[float]) -> float:
    return sum(values) / len(values) if values else 0.0


def _stage_representative_mean(records: list[dict[str, Any]], source: str,
                               event: str) -> tuple[float | None, int]:
    vals = [float(r["duration_ms"]) for r in records
            if str(r.get("source")) == source and str(r.get("event")) == event
            and r.get("duration_ms") is not None]
    if not vals:
        return None, 0
    return _mean(vals), len(vals)


def _stage_sum_segments(records: list[dict[str, Any]], source: str,
                        events: list[str]) -> tuple[float | None, int]:
    total = 0.0
    seen = 0
    for event in events:
        vals = [float(r["duration_ms"]) for r in records
                if str(r.get("source")) == source and str(r.get("event")) == event
                and r.get("duration_ms") is not None]
        if not vals:
            continue
        total += _mean(vals)
        seen += 1
    if seen == 0:
        return None, 0
    return total, seen


def _stage_segment_span(records: list[dict[str, Any]], source: str,
                        start_glob: str, end_event: str) -> tuple[float | None, int, list[float]]:
    """Per round: start = min(start_ns) of start_glob events, end = max(end_ns)
    of end_event events. Returns (mean_span_ms, n_rounds, per_round_spans_ms)."""
    stage_records = [r for r in records
                     if str(r.get("source")) == source
                     and r.get("start_ns") is not None and r.get("end_ns") is not None]
    per_round: dict[int, list[dict[str, Any]]] = {}
    for r in stage_records:
        rnd = _round_of(r)
        if rnd is None:
            continue
        per_round.setdefault(rnd, []).append(r)

    spans: list[float] = []
    for rnd in sorted(per_round):
        starts = [int(r["start_ns"]) for r in per_round[rnd]
                  if fnmatch.fnmatch(str(r.get("event", "")), start_glob)]
        ends = [int(r["end_ns"]) for r in per_round[rnd]
                if str(r.get("event")) == end_event]
        if not starts or not ends:
            continue
        spans.append((max(ends) - min(starts)) / 1_000_000)
    if not spans:
        return None, 0, []
    return _mean(spans), len(spans), spans


def compute_stage_durations(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Collapse raw records into one duration per stage per STAGE_SPECS."""
    rows: list[dict[str, Any]] = []
    for spec in STAGE_SPECS:
        stage = spec["stage"]
        method = spec["method"]
        source = spec.get("source", "")
        duration: float | None = None
        count = 0
        basis = ""
        components = ""
        note = ""

        if method == "representative_mean":
            duration, count = _stage_representative_mean(records, source, spec["event"])
            basis = "per call"
            components = spec["event"]
            if stage == "edit_execution":
                note = "submit -> executed; excludes multi-node sync (redaction_sync_confirm)"
            elif stage == "block_generation":
                note = "sealing cycle incl. queue wait; high variance (queue-dominated)"

        elif method == "sum_segments":
            duration, count = _stage_sum_segments(records, source, spec["events"])
            basis = "single run (segments summed)"
            components = "+".join(e.replace("_total", "") for e in spec["events"]) + "_total"

        elif method == "segment_span":
            duration, count, spans = _stage_segment_span(
                records, source, spec["start_glob"], spec["end_event"])
            basis = "per round, mean"
            components = f"{spec['start_glob']} -> {spec['end_event']}"
            note = ("GSC broadcast rotation event -> all TEE registered VRF; "
                    "excludes prepare_window_wait and post-VRF steps")
            if spans:
                note += f"; per-round={[round(s, 2) for s in spans]}s"

        if duration is None:
            note = (note + "; " if note else "") + "stage not present in data"

        rows.append({
            "stage": stage,
            "method": method,
            "source": source,
            "duration_ms": round(duration, 3) if duration is not None else "",
            "duration_s": round(duration / 1000, 3) if duration is not None else "",
            "basis": basis,
            "count": count,
            "components": components,
            "note": note,
        })
    return rows


def write_stages_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as fp:
        writer = csv.DictWriter(fp, fieldnames=STAGE_CSV_FIELDS)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Collect E2E timing data from geth logs")
    parser.add_argument("--logs-dir", type=Path, default=DEFAULT_LOGS, help="Directory containing geth-node-*.log")
    parser.add_argument("--script-timing", type=Path, default=DEFAULT_TIMING_JSONL, help="interactive-e2e-timing JSONL file")
    parser.add_argument("--out-json", type=Path, default=WORK / "timing" / "timing-summary.json")
    parser.add_argument("--out-csv", type=Path, default=WORK / "timing" / "timing-summary.csv")
    parser.add_argument("--out-stages-csv", type=Path, default=WORK / "timing" / "timing-stages.csv")
    args = parser.parse_args()

    records = load_geth_records(args.logs_dir)
    records.extend(load_script_records(args.script_timing))
    rows = summarize(records)
    stage_rows = compute_stage_durations(records)

    args.out_json.parent.mkdir(parents=True, exist_ok=True)
    args.out_json.write_text(json.dumps({"records": records, "summary": rows, "stages": stage_rows}, ensure_ascii=False, indent=2))
    write_csv(args.out_csv, rows)
    write_stages_csv(args.out_stages_csv, stage_rows)

    print(f"records: {len(records)}")
    print(f"summary_json: {args.out_json}")
    print(f"summary_csv: {args.out_csv}")
    print(f"stages_csv: {args.out_stages_csv}")

    print("\n=== per-stage duration ===")
    print(f"{'stage':<22}{'duration_s':>12}{'method':>20}{'count':>7}  components")
    print("-" * 100)
    for row in stage_rows:
        dur = row["duration_s"] if row["duration_s"] != "" else "-"
        print(f"{row['stage']:<22}{str(dur):>12}{row['method']:>20}{row['count']:>7}  {row['components']}")

    print("\nsource stage event count mean_ms p95_ms")
    for row in rows:
        print(f"{row['source']} {row['stage']} {row['event']} {row['count']} {row['mean_ms']} {row['p95_ms']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
