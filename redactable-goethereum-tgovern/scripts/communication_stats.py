"""Shared optional communication telemetry for the HW experiment drivers."""
import collections
import csv
import gzip
import json
import os
import statistics
import subprocess
import sys
import threading
from pathlib import Path

_LOCK = threading.Lock()


def enabled():
    return os.environ.get("TGOVERN_COMM_STATS") == "1"


def emit(path, record):
    if not enabled():
        return
    record = {"marker_schema": 2, **record}
    path = Path(path)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with _LOCK, path.open("a") as handle:
            handle.write("E2E_COMM " + json.dumps(record, sort_keys=True) + "\n")
    except OSError as exc:
        # Telemetry must not replace an RPC result/error or pollute worker JSON stdout.
        print(f"communication telemetry write failed ({path}): {exc}", file=sys.stderr)


def rpc_read(opener, request, timeout, path, method):
    """Observe bodies at the client; do not claim socket-write success on errors."""
    if not enabled():
        with opener.open(request, timeout=timeout) as response:
            return response.read()
    response_bytes, complete = 0, False
    try:
        with opener.open(request, timeout=timeout) as response:
            try:
                body = response.read()
            except Exception as exc:
                response_bytes = len(getattr(exc, "partial", b""))
                raise
            response_bytes, complete = len(body), True
            return body
    finally:
        emit(path, {"event": "rpc_exchange", "transport": "driver_http_rpc",
                    "node": "driver", "observation": "client_observed",
                    "stage": "rpc_auxiliary", "component": method,
                    "endpoint_role": "committee" if method.startswith("committee_") else "geth",
                    "call_class": "query_or_control" if method.startswith(("eth_get", "eth_call", "eth_is", "net_", "admin_", "web3_", "committee_get")) else "business_or_setup",
                    "request_body_bytes_supplied": len(request.data or b""),
                    "request_body_bytes_observed": len(request.data or b"") if complete else 0,
                    "response_body_bytes_observed": response_bytes,
                    "status": "ok" if complete else "io_failed"})


def prepare_archive(work, run_id):
    """Make a fresh compressed snapshot once; callers fetch bounded chunks."""
    work = Path(work)
    dest = work / "communication-snapshots" / run_id
    # Workspace ownership and run uniqueness are checked by the orchestrator.
    dest.mkdir(parents=True, exist_ok=True)
    sources = []
    for path in sorted((work / "logs").glob("*.log")):
        if not (path.name.startswith(("node", "committee", "geth-node", "driver"))):
            continue
        compressed = dest / (path.name + ".gz")
        with path.open("rb") as raw, gzip.open(compressed, "wb") as out:
            for line in raw:
                if b"E2E_COMM " in line:
                    out.write(line)
        sources.append({"name": compressed.name, "size": compressed.stat().st_size})
    return {"files": sources}


def collect_single(raw_dir, out_dir, script_dir):
    if not enabled():
        return
    raw_dir, out_dir = Path(raw_dir), Path(out_dir)
    logs = raw_dir / "logs"
    expected = [p.name for p in sorted(logs.glob("geth-node-*.log"))]
    expected += [p.name for p in sorted(logs.glob("committee-node-*.log"))]
    # A manifest, written by the driver before launch, includes processes that
    # failed before producing any log file. Do not infer coverage from survivors.
    src_manifest = logs / "communication-expected.json"
    if src_manifest.is_file():
        manifest = src_manifest
    else:
        manifest = raw_dir / "communication-expected.json"
        manifest.write_text(json.dumps({"expected_sources": expected, "inferred": True}))
        # No authoritative launch manifest means coverage must remain unknown.
        manifest = None
    command = [sys.executable, str(Path(script_dir) / "collect-communication.py"),
               "--logs-dir", str(logs), "--out-dir", str(out_dir),
               "--progress", str(raw_dir / "e2e-progress.log")]
    if manifest:
        command += ["--expected-file", str(manifest)]
    subprocess.run(command, check=True)


def aggregate_batch(output_dir, summaries):
    """Equal-weight run aggregation, no IQR or silent omission of missing runs."""
    output_dir = Path(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    payloads = []
    for path in summaries:
        path = Path(path)
        if not path.is_file():
            payloads.append({"path": str(path), "status": "unavailable"})
        else:
            try:
                d = json.loads(path.read_text())
                if d["schema"] != 2 or not isinstance(d["summary"], list):
                    raise ValueError("unsupported communication summary")
                payloads.append({"path": str(path), "status": d["status"], "data": d})
            except (OSError, ValueError, KeyError, TypeError) as exc:
                payloads.append({"path": str(path), "status": "unavailable", "error": str(exc)})
    groups = collections.defaultdict(list)
    for item in payloads:
        if item["status"] != "available":
            continue
        for row in item["data"]["summary"]:
            if row["scope"] in ("transport", "stage", "component", "completed_edit_mean", "completed_rotation_mean", "target_epoch", "all_run"):
                key = (row["scope"], row.get("transport", ""), row.get("stage", ""), row.get("component", ""), str(row.get("epoch", "")))
                groups[key].append(row)
    rows = []
    available = sum(x["status"] == "available" for x in payloads)
    for key, rs in sorted(groups.items()):
        # Missing a component in an otherwise covered run means no observed send,
        # not an unavailable run. Completed means require their own denominator.
        vals = [r.get("mean_bytes_per_operation", r["payload_bytes"]) for r in rs]
        if key[0] not in ("completed_edit_mean", "completed_rotation_mean", "target_epoch"):
            vals += [0] * (available - len(vals))
        mean = statistics.mean(vals)
        sd = statistics.stdev(vals) if len(vals) > 1 else 0
        basis = {"completed_edit_mean": "per_completed_edit", "completed_rotation_mean": "per_completed_rotation", "target_epoch": "per_target_epoch"}.get(key[0], "per_run")
        rows.append({"scope": key[0], "transport": key[1], "stage": key[2], "component": key[3], "epoch": key[4],
                     "runs": len(vals), "mean_bytes": mean, "sample_stddev_bytes": sd,
                     "cv": sd / mean if mean else None, "basis": basis})
    status = "available" if payloads and available == len(payloads) else "partial" if available else "unavailable"
    (output_dir / "communication-aggregate.json").write_text(json.dumps({"status": status, "runs": [{k:v for k,v in x.items() if k != "data"} for x in payloads], "summary": rows}, indent=2) + "\n")
    with (output_dir / "communication-aggregate.csv").open("w", newline="") as handle:
        columns = list(rows[0]) if rows else ["scope", "runs", "mean_bytes"]
        writer = csv.DictWriter(handle, columns)
        writer.writeheader()
        writer.writerows(rows)
