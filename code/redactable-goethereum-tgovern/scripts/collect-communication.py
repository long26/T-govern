#!/usr/bin/env python3
"""Collect protocol sender payloads; keep client-observed RPC/IPC separate."""
import argparse
import collections
import csv
import gzip
import json
import re
from pathlib import Path

NETWORK = {"geth_eth", "committee_p2p"}
ANSI = re.compile(r"\x1b\[[0-9;]*m")
KV = re.compile(r'(\w+)=\s*("(?:\\.|[^"\\])*"|\S+)')
DIMENSIONS = ("transport", "stage", "component", "business_component", "msg_type", "protocol_kind", "session", "epoch", "request_id", "join_key", "node", "source")
COUNTERS = ("messages", "successful_messages", "failed_messages", "payload_bytes", "attempted_payload_bytes", "failed_attempted_payload_bytes", "partial_written_bytes", "failed_writes_unknown_partial_bytes")


def canonical(value):
    text = str(value or "")
    if re.fullmatch(r"(?:0x)?[0-9a-fA-F]{64}", text):
        return text.removeprefix("0x").lower()
    return text


def parse_line(line):
    line = ANSI.sub("", line)
    if "E2E_COMM " not in line:
        return None
    body = line.split("E2E_COMM ", 1)[1].strip()
    if body.startswith("{"):
        record = json.loads(body)
    else:
        record = {}
        for key, value in KV.findall(body):
            record[key] = json.loads(value) if value.startswith('"') else value
    if not isinstance(record, dict):
        raise ValueError("record must be an object")
    if int(record.get("marker_schema", 0)) != 2:
        raise ValueError("unsupported marker schema")
    if record.get("event") not in ("enabled", "association", "send", "rpc_exchange"):
        raise ValueError("unknown event")
    if record["event"] in ("enabled", "association") and record.get("transport") not in NETWORK:
        raise ValueError("invalid source/association transport")
    if record["event"] == "association" and not record.get("session"):
        raise ValueError("association requires session")
    for key in ("session", "request_id"):
        if key in record:
            record[key] = canonical(record[key])
    if record["event"] == "send":
        if record.get("transport") not in NETWORK or record.get("observation") != "sender_write":
            raise ValueError("invalid sender transport/observation")
        if record.get("status") not in ("ok", "encode_failed", "write_failed"):
            raise ValueError("invalid send status")
        record["written_bytes_observed"] = "written_bytes" in record
        for key in ("payload_bytes", "attempted_payload_bytes", "written_bytes"):
            record[key] = int(str(record.get(key, 0)).replace(",", ""))
            if record[key] < 0:
                raise ValueError("negative bytes")
        sent, attempted = record["payload_bytes"], record["attempted_payload_bytes"]
        if (record["status"] == "ok" and sent != attempted) or (record["status"] != "ok" and sent != 0):
            raise ValueError("inconsistent byte/status fields")
        if record["written_bytes"] > attempted:
            raise ValueError("written bytes exceed attempted bytes")
    if record["event"] == "rpc_exchange":
        if record.get("transport") not in ("driver_http_rpc", "committee_http_rpc", "local_ipc"):
            raise ValueError("invalid auxiliary transport")
        if record.get("status") not in ("ok", "io_failed"):
            raise ValueError("invalid RPC status")
        if record.get("observation") != "client_observed":
            raise ValueError("RPC must be client observed")
        for key in ("request_body_bytes_observed", "response_body_bytes_observed"):
            record[key] = int(record.get(key, 0))
            if record[key] < 0:
                raise ValueError("negative RPC bytes")
    return record


def open_log(path):
    return gzip.open(path, "rt", errors="replace") if path.suffix == ".gz" else path.open(errors="replace")


def iter_records(paths, root, invalid=None):
    for path in paths:
        with open_log(path) as handle:
            for number, line in enumerate(handle, 1):
                try:
                    record = parse_line(line)
                    if record is not None:
                        record["source"] = str(path.relative_to(root))
                        record["line_number"] = number
                        record["node"] = record.get("node") or path.name.split(".")[0]
                        yield record
                except (ValueError, TypeError, KeyError) as exc:
                    if invalid is not None:
                        invalid.write(json.dumps({"source": str(path.relative_to(root)), "line_number": number, "error": str(exc)}) + "\n")


def blank():
    return dict.fromkeys(COUNTERS, 0)


def increment(total, record):
    ok = record["status"] == "ok"
    total["messages"] += 1
    total["successful_messages"] += int(ok)
    total["failed_messages"] += int(not ok)
    total["payload_bytes"] += record["payload_bytes"]
    total["attempted_payload_bytes"] += record["attempted_payload_bytes"]
    if not ok:
        total["failed_attempted_payload_bytes"] += record["attempted_payload_bytes"]
        total["partial_written_bytes"] += record["written_bytes"]
        total["failed_writes_unknown_partial_bytes"] += int(record["status"] == "write_failed" and not record["written_bytes_observed"])


def write_csv(path, rows, fallback):
    columns = list(dict.fromkeys(key for row in rows for key in row)) or fallback
    with path.open("w", newline="") as handle:
        writer = csv.DictWriter(handle, columns)
        writer.writeheader()
        writer.writerows(rows)


def collect(logs_dir, out_dir, progress=None, expected_file=None, target_epoch=None):
    out_dir.mkdir(parents=True, exist_ok=True)
    # Only source logs: never recursively ingest a previous collector's records.
    paths = sorted(p for p in logs_dir.rglob("*") if p.is_file() and (p.name.endswith(".log") or p.name.endswith(".log.gz")))
    associations, block_requests = {}, {}
    enabled = set()
    for record in iter_records(paths, logs_dir):
        if record["event"] == "association":
            key = (record["transport"], record.get("session", ""))
            value = {k: record[k] for k in ("stage", "component", "request_id", "epoch") if k in record}
            if key in associations and associations[key] != value:
                raise ValueError(f"conflicting session association: {key}")
            associations[key] = value
        if record["event"] == "enabled":
            enabled.add(record["source"])
        if record.get("request_id") and record.get("join_key"):
            prior = block_requests.setdefault(record["join_key"], record["request_id"])
            if prior != record["request_id"]:
                # A block/index may be edited more than once: never guess a request.
                block_requests[record["join_key"]] = ""
    completed, target_epochs, completed_rotations = set(), set(), set()
    if target_epoch is not None:
        target_epochs.add(str(target_epoch))
    if progress and progress.is_file():
        for line in progress.read_text(errors="replace").splitlines():
            if "event=communication_edit_verified " in line and "status=ok" in line:
                f = dict(KV.findall(line))
                completed.add(canonical(f["join_key"]))
            if "event=communication_rotation_target " in line:
                target_epochs.add(str(int(dict(KV.findall(line))["epoch"])))
            if "event=communication_rotation_verified " in line and "status=ok" in line:
                epoch = str(int(dict(KV.findall(line))["epoch"]))
                completed_rotations.add(epoch)
                target_epochs.add(epoch)
    details = collections.defaultdict(blank)
    rpc = collections.defaultdict(lambda: {"exchanges": 0, "failed_exchanges": 0, "request_body_bytes_observed": 0, "response_body_bytes_observed": 0})
    total, valid = blank(), 0
    invalid_path = out_dir / "communication-invalid.jsonl"
    with invalid_path.open("w") as bad, (out_dir / "communication-records.jsonl").open("w") as raw:
        for r in iter_records(paths, logs_dir, bad):
            valid += 1
            if r["event"] == "send":
                association = associations.get((r["transport"], r.get("session", "")), {})
                if association:
                    # Preserve the concrete wire-message class, attach business attribution.
                    r["stage"] = association.get("stage", r.get("stage", "unknown"))
                    r["business_component"] = association.get("component", "")
                    for key in ("request_id", "epoch"):
                        if association.get(key):
                            r[key] = association[key]
                if not r.get("request_id") and r.get("join_key"):
                    r["request_id"] = block_requests.get(r["join_key"], "")
                increment(total, r)
                increment(details[tuple(str(r.get(k, "")) for k in DIMENSIONS)], r)
            elif r["event"] == "rpc_exchange":
                row = rpc[(r["transport"], r.get("component", ""), r["node"], r["source"])]
                row["exchanges"] += 1
                row["failed_exchanges"] += int(r["status"] != "ok")
                for k in ("request_body_bytes_observed", "response_body_bytes_observed"):
                    row[k] += r[k]
            raw.write(json.dumps(r, sort_keys=True) + "\n")
    invalid_count = sum(1 for _ in invalid_path.open())
    rows = [{"scope": "all_run", **total}]
    detail_rows = [{"scope": "detail", **dict(zip(DIMENSIONS, key)), **value} for key, value in sorted(details.items())]
    for scope, keys in (("transport", ("transport",)), ("stage", ("transport", "stage")), ("component", ("transport", "stage", "component")), ("session", ("transport", "stage", "session", "epoch")), ("request", ("transport", "stage", "request_id")), ("business_component", ("transport", "stage", "business_component"))):
        grouped = collections.defaultdict(blank)
        for r in detail_rows:
            key = tuple(r[k] for k in keys)
            for counter in COUNTERS:
                grouped[key][counter] += r[counter]
        rows += [{"scope": scope, **dict(zip(keys, key)), **value} for key, value in sorted(grouped.items())]
    rows += detail_rows
    # Only progress-confirmed edits enter the completed-operation denominator.
    for transport, stage in sorted({(r["transport"], r["stage"]) for r in detail_rows if r["request_id"] in completed}):
        value = blank()
        for r in detail_rows:
            if r["transport"] == transport and r["stage"] == stage and r["request_id"] in completed:
                for k in COUNTERS:
                    value[k] += r[k]
        matched = {r["request_id"] for r in detail_rows if r["transport"] == transport and r["stage"] == stage and r["request_id"] in completed}
        rows.append({"scope": "completed_edit_mean", "transport": transport, "stage": stage,
                     "operations": len(completed), "matched_requests": len(matched),
                     "unmatched_completed_requests": len(completed - matched), "attribution": "attributed_only",
                     "mean_bytes_per_operation": value["payload_bytes"] / len(completed), **value})
    rotation_transports = sorted({r["transport"] for r in detail_rows if r["stage"] == "rotation_flow"})
    for epoch in sorted(target_epochs, key=int):
        for transport in rotation_transports:
            value = blank()
            for r in detail_rows:
                if r["transport"] == transport and r["stage"] == "rotation_flow" and r["epoch"] == epoch:
                    for k in COUNTERS:
                        value[k] += r[k]
            rows.append({"scope": "target_epoch", "transport": transport, "stage": "rotation_flow",
                         "epoch": epoch, "operation_verified": epoch in completed_rotations, **value})
    if completed_rotations:
        for transport in rotation_transports:
            value = blank()
            matched = set()
            for r in detail_rows:
                if r["transport"] == transport and r["stage"] == "rotation_flow" and r["epoch"] in completed_rotations:
                    matched.add(r["epoch"])
                    for k in COUNTERS:
                        value[k] += r[k]
            rows.append({"scope": "completed_rotation_mean", "transport": transport,
                         "stage": "rotation_flow", "operations": len(completed_rotations),
                         "matched_epochs": len(matched), "attribution": "attributed_only",
                         "mean_bytes_per_operation": value["payload_bytes"] / len(completed_rotations), **value})
    expected = json.loads(expected_file.read_text())["expected_sources"] if expected_file else []
    if not isinstance(expected, list) or any(not isinstance(x, str) or not x for x in expected):
        raise ValueError("expected_sources must be a list of nonempty source paths")
    missing = sorted(set(expected) - enabled)
    status = "unavailable" if valid == 0 else ("available" if expected and not missing and invalid_count == 0 else "partial")
    assert sum(r["payload_bytes"] for r in detail_rows) == total["payload_bytes"]
    rpc_rows = [{"transport": key[0], "method": key[1], "node": key[2], "source": key[3], **value} for key, value in sorted(rpc.items())]
    summary = {"schema": 2, "status": status, "coverage": "expected_sources_checked" if expected else "unknown_expected_sources", "expected_sources": expected, "enabled_sources": sorted(enabled), "missing_sources": missing, "valid_records": valid, "invalid_records": invalid_count, "completed_edits": len(completed), "target_epochs": sorted(target_epochs, key=int), "completed_rotations": len(completed_rotations), "all_run": total, "summary": rows, "rpc_auxiliary": rpc_rows, "scope_note": "Protocol sender RLP/JSON payloads only; RPC/IPC client observations are separate. available is logging coverage, not delivery proof."}
    (out_dir / "communication-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    write_csv(out_dir / "communication-summary.csv", rows, ["scope", *COUNTERS])
    write_csv(out_dir / "communication-rpc-auxiliary.csv", rpc_rows, ["transport", "method", "exchanges"])
    print(f"communication: {status}; sends={total['messages']}; payload_bytes={total['payload_bytes']}; invalid={invalid_count}; missing={missing}")
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--logs-dir", type=Path, required=True)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--progress", type=Path)
    parser.add_argument("--expected-file", type=Path)
    parser.add_argument("--target-epoch", type=int)
    args = parser.parse_args()
    collect(args.logs_dir, args.out_dir, args.progress, args.expected_file, args.target_epoch)


if __name__ == "__main__":
    main()
