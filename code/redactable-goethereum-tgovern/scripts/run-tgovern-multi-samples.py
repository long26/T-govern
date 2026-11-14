#!/usr/bin/env python3
"""Repeat the original two-host T-govern flow and validate timing batches.

Every execution has an explicit, unique --run-id. Only executions with a
successful run-status.json, complete requested samples and matching successful
redaction receipts enter the aggregate. Existing batches are never overwritten;
--resume is deliberately unsupported until a versioned resume contract exists.
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
import csv
import json
import math
import re
import shlex
import signal
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
import uuid

import topology
import communication_stats

ROOT = Path(__file__).resolve().parents[1]
ORCHESTRATOR = ROOT / "scripts" / "tgovern_multi_host.py"
# Kept in sync with tgovern_multi_host.DEFAULT_BOOTSTRAP_EPOCH: budgets sized for
# the current PoW difficulty (~10s blocks) rather than the old 1-2s chain.
DEFAULT_BOOTSTRAP_EPOCH = 128
CANCEL_GRACE_SECONDS = 300
STOP_TIMEOUT_SECONDS = 600
# Inspected collect-l5-timing.py: compute_edit_samples, compute_rotation_samples,
# and compute_timing. edit_execution is committee_redact_request_submitted ->
# first TEE redaction_broadcast_modify_msg; tx_hash is retained only as
# submission traceability, NOT as the measured endpoint (receipt success lives in
# execute_redaction_confirmation).
COLLECTOR_SCHEMA = {
    "source": "scripts/collect-l5-timing.py",
    "edit_execution": ["join_key", "duration_ms", "start_event", "end_host", "tx_hash", "status"],
    "execute_redaction_confirmation": ["join_key", "duration_ms", "tx_hash", "status"],
    # rotation_flow span = node1 mint time of the epoch-triggering block ->
    # cluster-wide DKG completion (rotation_dkg_complete).  The proposer-side
    # start is reported as a separate secondary stage (rotation_flow_from_prep).
    "rotation_flow": ["epoch", "duration_ms", "parties", "start_source", "start_block", "status"],
    "rotation_flow_from_prep": ["epoch", "duration_ms", "parties", "start_source", "start_block", "status"],
    "nested_stage": "tee_registration_substages:<event>",
    "receipt_evidence": "matching join_key and tx_hash in execute_redaction_confirmation",
    # ledger_update headline is pure-local apply (lu_getblock+lu_validate+
    # lu_update); the full anchor span is reported separately as
    # ledger_update_total. Neither gates batch acceptance.
    "ledger_update": ["join_key", "duration_ms", "local_breakdown_ms", "total_duration_ms", "status"],
}


class RunCancelled(BaseException):
    def __init__(self, signum: int):
        self.signum = signum
        super().__init__(f"cancelled by {signal.Signals(signum).name}")


@contextmanager
def cancellation_handlers(pending: list[int] | None = None):
    def cancel(signum, _frame):
        if pending is not None and not pending:
            pending.append(signum)
        raise RunCancelled(signum)

    cancel.pending_signals = pending
    saved = {sig: signal.signal(sig, cancel) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for sig, handler in saved.items():
            signal.signal(sig, handler)


@contextmanager
def defer_cancellation(pending: list[int] | None = None):
    """Finish cleanup; record a first interrupt for the controller to honor."""
    # Nested reaping must retain the controller's deferred first interrupt.
    if pending is None:
        pending = getattr(signal.getsignal(signal.SIGINT), "pending_signals", None)
    # A caught handler resets to SIG_DFL at exec; SIG_IGN would leak into the
    # fallback stop subprocess and make its termination signals ineffective.
    def defer(signum, _frame):
        if pending is not None and not pending:
            pending.append(signum)

    defer.pending_signals = pending
    saved = {sig: signal.signal(sig, defer) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for sig, handler in saved.items():
            signal.signal(sig, handler)


def reap_owned_process(process: subprocess.Popen, signum: int) -> None:
    """Signal only our unreaped Popen child, never a broad process match/group."""
    with defer_cancellation():
        if process.poll() is None:
            process.send_signal(signum)
        try:
            process.wait(timeout=CANCEL_GRACE_SECONDS)
        except subprocess.TimeoutExpired:
            if process.poll() is None:
                process.kill()
            process.wait()


def run_orchestrator(command: list[str], output: Any, timeout: int | None = None) -> int:
    process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output,
                               stderr=subprocess.STDOUT, start_new_session=True)
    try:
        return process.wait(timeout=timeout)
    except BaseException as exc:
        signum = exc.signum if isinstance(exc, RunCancelled) else signal.SIGINT
        if not isinstance(exc, (RunCancelled, KeyboardInterrupt)):
            signum = signal.SIGTERM
        reap_owned_process(process, signum)
        raise


def stop_run(args: argparse.Namespace, run_id: str, log_path: Path) -> None:
    """The stop command verifies run-owned PIDs on BOTH hosts; rc=0 is required."""
    command = [sys.executable, str(ORCHESTRATOR), "stop", "--run-id", run_id,
               "--remote-ssh", args.remote_ssh, "--local-root", str(args.local_root),
               "--remote-root", str(args.remote_root)]
    with log_path.open("x") as output:
        returncode = run_orchestrator(command, output, timeout=STOP_TIMEOUT_SECONDS)
    if returncode != 0:
        raise RuntimeError(f"run-scoped stop failed (rc={returncode}); see {log_path}")


def percentile(values: list[float], q: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = q * (len(ordered) - 1)
    low = int(index)
    high = min(low + 1, len(ordered) - 1)
    frac = index - low
    return round(ordered[low] * (1 - frac) + ordered[high] * frac, 3)


def filtered_mean(values: list[float], iqr_mult: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    q1 = percentile(ordered, 0.25)
    q3 = percentile(ordered, 0.75)
    if q1 is None or q3 is None:
        return None
    iqr = q3 - q1
    kept = [v for v in ordered if q1 - iqr_mult * iqr <= v <= q3 + iqr_mult * iqr]
    return round(sum(kept) / len(kept), 3) if kept else None


def stage_stats(durations: list[float]) -> dict[str, Any]:
    return {
        "count": len(durations),
        "mean_ms": round(sum(durations) / len(durations), 3) if durations else None,
        "filtered_mean_1_5iqr_ms": filtered_mean(durations, 1.5),
        "filtered_mean_3iqr_ms": filtered_mean(durations, 3.0),
        "p50_ms": percentile(durations, 0.50),
        "p95_ms": percentile(durations, 0.95),
        "p99_ms": percentile(durations, 0.99),
        "max_ms": round(max(durations), 3) if durations else None,
    }


def flatten_samples(samples: Any) -> dict[str, list[dict[str, Any]]]:
    """Validate collector containers/durations without mutating source records."""
    if not isinstance(samples, dict):
        raise ValueError("collector samples must be an object")
    flattened = {}

    def visit(stage: str, value: Any):
        if isinstance(value, dict):
            for substage, entries in value.items():
                if not isinstance(substage, str) or not substage or ":" in substage:
                    raise ValueError(f"invalid substage name under {stage}")
                visit(f"{stage}:{substage}", entries)
        elif isinstance(value, list):
            for entry in value:
                if not isinstance(entry, dict):
                    raise ValueError(f"{stage}: samples must be objects")
                duration = entry.get("duration_ms")
                if (isinstance(duration, bool) or not isinstance(duration, (int, float))
                        or not math.isfinite(duration) or duration < 0):
                    raise ValueError(f"{stage}: duration_ms must be finite and nonnegative")
            flattened[stage] = value
        else:
            raise ValueError(f"{stage}: expected sample list or nested stage object")

    for stage, value in samples.items():
        if not isinstance(stage, str) or not stage or ":" in stage:
            raise ValueError("invalid collector stage name")
        visit(stage, value)
    return flattened


def merge_run_samples(all_samples: dict[str, list[dict[str, Any]]], run_number: int,
                      run_samples: dict[str, Any], *, run_id: str,
                      run_out_dir: Path) -> None:
    for stage, entries in flatten_samples(run_samples).items():
        all_samples.setdefault(stage, []).extend(
            {**entry, "run": run_number, "run_id": run_id,
             "run_out_dir": str(run_out_dir),
             "summary_path": str(run_out_dir / "timing" / "l5-timing-summary.json")}
            for entry in entries
        )


def expected_counts(args: argparse.Namespace, runs: int = 1) -> dict[str, int]:
    # baseline-edits is a count of intervals, each with edits-per-epoch edits.
    edits = runs * args.edits_per_epoch * (args.rotations + args.baseline_edits)
    return {"rotation_flow": runs * args.rotations, "edit_execution": edits,
            "execute_redaction_confirmation": edits}


def validate_counts(samples: dict[str, list[dict[str, Any]]], expected: dict[str, int]) -> None:
    for stage, count in expected.items():
        actual = len(samples.get(stage, []))
        if actual != count:
            raise ValueError(f"{stage}: expected {count} samples, got {actual}")


def validate_success_status(run_out_dir: Path, run_id: str) -> None:
    status = json.loads((run_out_dir / "run-status.json").read_text())
    if (not isinstance(status, dict) or status.get("run_id") != run_id
            or status.get("success") is not True or status.get("cleanup_errors") != []):
        raise ValueError("run-status does not prove flow/collection/cleanup success for this run_id")


def load_run_samples(run_out_dir: Path, *, run_id: str,
                     expected: dict[str, int], threshold: int) -> dict[str, Any]:
    validate_success_status(run_out_dir, run_id)
    payload = json.loads((run_out_dir / "timing" / "l5-timing-summary.json").read_text())
    if not isinstance(payload, dict):
        raise ValueError("collector summary must be an object")
    samples = payload.get("samples")
    if isinstance(samples, dict):
        samples = dict(samples)
        for stage in ("rotation_flow", "rotation_flow_from_prep"):
            if isinstance(samples.get(stage), list):
                # Epoch zero is bootstrap, not a measured committee rotation.
                samples[stage] = [entry for entry in samples[stage]
                                  if not isinstance(entry, dict) or entry.get("epoch") != 0]
    flattened = flatten_samples(samples)
    for stage in expected:
        if not isinstance(samples.get(stage), list):
            raise ValueError(f"{stage}: required collector stage must be a list")
    validate_counts(flattened, expected)

    def edit_keys(stage):
        # Submission and receipt samples carry the SAME request (join_key) but
        # different tx hashes (submit tx vs receipt tx), so pair by join_key and
        # only require a valid, stage-unique tx hash.
        keys = set()
        transactions = set()
        for entry in flattened[stage]:
            join_key, tx_hash = entry.get("join_key"), entry.get("tx_hash")
            if (entry.get("status") != "ok" or not isinstance(join_key, str)
                    or not join_key or join_key != join_key.strip().lower()
                    or not isinstance(tx_hash, str)
                    or not re.fullmatch(r"0x[0-9a-f]{64}", tx_hash)
                    or tx_hash == "0x" + "0" * 64):
                raise ValueError(f"{stage}: missing valid collector success/join_key/tx_hash evidence")
            if join_key in keys or tx_hash in transactions:
                raise ValueError(f"{stage}: duplicate request or transaction")
            keys.add(join_key)
            transactions.add(tx_hash)
        return keys

    if edit_keys("edit_execution") != edit_keys("execute_redaction_confirmation"):
        raise ValueError("edit submissions do not match successful receipts by join_key")
    seen_epochs = {"rotation_flow": set(), "rotation_flow_from_prep": set()}
    for stage in ("rotation_flow", "rotation_flow_from_prep"):
        # rotation_flow_from_prep is a secondary start口径 and may be absent
        # (e.g. a run with no proposer-side anchor); it is not a required stage.
        for entry in flattened.get(stage, []):
            epoch, parties, source, block = (entry.get("epoch"), entry.get("parties"),
                                             entry.get("start_source"), entry.get("start_block"))
            if (entry.get("status") != "ok" or type(epoch) is not int or epoch < 1
                    or epoch in seen_epochs[stage]
                    or not isinstance(source, str) or not source
                    or (parties is not None and (type(parties) is not int or parties < 1))
                    or (block is not None and (type(block) is not int or block < 0))):
                raise ValueError(f"{stage}: invalid/duplicate epoch or malformed trigger evidence")
            seen_epochs[stage].add(epoch)
    diagnostics = payload.get("diagnostics")
    if not isinstance(diagnostics, dict):
        raise ValueError("collector diagnostics must be an object")
    # receipt_failures and ledger_failures count per-attempt/per-host failures and
    # are expected to be nonempty whenever a registration/redaction/ledger apply
    # retried (e.g. an ON node receives tx_modify before the redaction is visible
    # on-chain); edit_integrity_alerts is the real invariant (a submitted edit with
    # no successful receipt). A permanently unrecovered failure cannot slip through:
    # it would leave ledger_update/edit_execution short of the exact expected
    # count and be rejected by validate_counts. Only genuine integrity signals
    # must be empty.
    for name in ("submission_failures", "invalid_submissions", "invalid_success_receipts",
                 "edit_integrity_alerts"):
        if diagnostics.get(name) != []:
            raise ValueError(f"collector diagnostics {name} missing or nonempty")
    return samples


def write_aggregate(output_dir: Path, samples: dict[str, list[dict[str, Any]]], *,
                    successful_runs: list[dict[str, Any]], expected: dict[str, int],
                    complete: bool) -> None:
    if communication_stats.enabled():
        try:
            manifest = json.loads((output_dir / "manifest.json").read_text())
            communication_stats.aggregate_batch(output_dir, [
                Path(run["run_out_dir"]) / "communication" / "communication-summary.json"
                for run in manifest["planned_runs"]])
        except Exception as exc:
            print(f"communication aggregation failed: {exc!r}", file=sys.stderr)
    payload = {
        "created_at": datetime.now(timezone.utc).isoformat(),
        "complete": complete,
        "expected_samples": expected,
        "successful_runs": successful_runs,
        "samples": samples,
        "runs": {stage: stage_stats([float(s["duration_ms"]) for s in entries])
                 for stage, entries in samples.items()},
        "sample_counts": {stage: len(entries) for stage, entries in samples.items()},
    }
    (output_dir / "aggregate.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2))
    with (output_dir / "aggregate.csv").open("w", newline="") as output:
        writer = csv.writer(output)
        writer.writerow(["stage", "count", "mean_ms", "filtered_mean_1_5iqr_ms",
                         "p50_ms", "p95_ms", "p99_ms", "max_ms"])
        for stage, stats in payload["runs"].items():
            writer.writerow([stage, stats["count"], stats["mean_ms"],
                             stats["filtered_mean_1_5iqr_ms"], stats["p50_ms"],
                             stats["p95_ms"], stats["p99_ms"], stats["max_ms"]])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runs", type=int, default=10, help="fresh multi-host executions")
    parser.add_argument("--output-root", type=Path, default=ROOT / "results" / "tgovern-multi")
    parser.add_argument("--tag", help="result directory name; default UTC timestamp")
    parser.add_argument("--resume", action="store_true", help="unsupported; existing results are preserved")
    parser.add_argument("--stop-on-failure", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--remote-ssh", required=True)
    parser.add_argument("--local-ip", required=True)
    parser.add_argument("--remote-ip", required=True)
    parser.add_argument("--remote-root", type=Path, default=Path("/opt/tgovern"))
    parser.add_argument("--local-root", type=Path, default=ROOT)
    parser.add_argument("--tee-count", type=int, default=4)
    parser.add_argument("--active-limit", type=int, default=4)
    parser.add_argument("--active-threshold", type=int, default=3)
    parser.add_argument("--committee-count", type=int, default=4)
    parser.add_argument("--committee-threshold", type=int, default=3)
    parser.add_argument("--rotations", type=int, default=1)
    parser.add_argument("--baseline-edits", type=int, default=1)
    parser.add_argument("--edits-per-epoch", type=int, default=1)
    parser.add_argument("--epoch-length", type=int, default=DEFAULT_BOOTSTRAP_EPOCH,
                        help=f"bootstrap epoch length (default: {DEFAULT_BOOTSTRAP_EPOCH}, "
                             "sized for the current ~10s-block difficulty)")
    parser.add_argument("--blocks-per-registered-node", type=int, default=1,
                        help="registration blocks budgeted per TEE in the sgx_l5 "
                             "min-epoch-length floor (0 keeps its default 12); the "
                             "default 1 keeps a short --epoch-length from being raised")
    parser.add_argument("--sgx-mode", choices=("HW", "SIM"), default="HW")
    parser.add_argument("--quote-mode", choices=("real", "mock", "auto"), default="mock")
    parser.add_argument("--attestation", default="noop")
    parser.add_argument("--gomaxprocs", type=int, default=4)
    parser.add_argument("--topology", choices=("sparse", "full"), default="sparse")
    parser.add_argument("--topology-radius", type=int, default=2)
    parser.add_argument("--topology-max-degree", type=int, default=9)
    parser.add_argument("--netem-delay", type=int, default=0)
    parser.add_argument("--netem-jitter", type=int, default=0)
    parser.add_argument("--netem-loss", type=float, default=0.0)
    parser.add_argument("--netem-limit", type=int, default=10000)
    parser.add_argument("--netem-dev", default="eth0")
    parser.add_argument("--netem-lo-dev", default="lo",
                        help="device carrying same-host traffic (default: lo); delayed "
                             "alongside the ethernet device so geth P2P and committee "
                             "sockets see the delay whether or not they cross hosts")
    parser.add_argument("--no-netem-lo", action="store_true",
                        help="cross-host delay only; leave same-host traffic undelayed")
    parser.add_argument("--light-nodes", type=int, default=0)
    parser.add_argument("--light-remote", action="store_true",
                        help="place light nodes on the TEE host (default: host A)")
    parser.add_argument("--genesis-difficulty", default="0",
                        help="PoW difficulty override (hex 0x.. or decimal); 0 keeps genesis.json")
    parser.add_argument("--est-seconds-per-block", type=float, default=15.0,
                        help="block-time estimate for deadlines; default 15 matches the "
                             "slow chain's mean interval; 0 keeps the library default (4)")
    parser.add_argument("--on-tx-rate", type=float, default=0.0,
                        help="background ON transactions per second; 0 disables the load")
    parser.add_argument("--tx-per-block", type=float, default=0.0,
                        help="background ON tx density per block; 0 disables; capped at 200")
    args = parser.parse_args()
    if args.resume:
        parser.error("--resume is unsupported: existing attempts/logs are unchanged; use a new --tag")
    if (args.runs < 1 or args.rotations < 0 or args.baseline_edits < 0
            or args.edits_per_epoch < 1 or args.rotations + args.baseline_edits == 0):
        parser.error("require positive --runs/--edits-per-epoch and nonnegative intervals with at least one edit interval")
    if not (1 <= args.active_threshold <= args.active_limit <= args.tee_count):
        parser.error("require 1 <= active-threshold <= active-limit <= tee-count")
    if not (1 <= args.committee_threshold <= args.committee_count):
        parser.error("require 1 <= committee-threshold <= committee-count")
    if args.tx_per_block > 200:
        parser.error("--tx-per-block must be <= 200")
    if args.on_tx_rate < 0 or args.tx_per_block < 0 or args.est_seconds_per_block < 0:
        parser.error("--on-tx-rate/--tx-per-block/--est-seconds-per-block must be >= 0")
    if str(args.genesis_difficulty) not in ("", "0"):
        raw = str(args.genesis_difficulty)
        try:
            if (int(raw, 16) if raw.lower().startswith("0x") else int(raw, 10)) < 1:
                raise ValueError
        except ValueError:
            parser.error("--genesis-difficulty must be hex 0x.. or decimal >= 1")
    if args.light_nodes < 0:
        parser.error("--light-nodes must be nonnegative")
    try:
        topo = topology.build_topology(
            1 + args.tee_count + args.light_nodes, mode=args.topology,
            radius=args.topology_radius, max_degree=args.topology_max_degree,
            relay_nodes=range(2, 2 + args.tee_count))
    except ValueError as exc:
        parser.error(str(exc))
    stamp = args.tag or datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    if stamp in (".", "..") or not stamp or Path(stamp).name != stamp:
        parser.error("--tag must be a single directory name")
    output_dir = args.output_root / stamp
    if output_dir.exists():
        parser.error(f"result directory already exists (preserved): {output_dir}")
    command = [sys.executable, str(ORCHESTRATOR), "start"]
    for name in ("remote_ssh", "local_ip", "remote_ip", "remote_root", "local_root",
                 "tee_count", "active_limit", "active_threshold", "committee_count",
                 "committee_threshold", "rotations", "baseline_edits", "edits_per_epoch",
                 "epoch_length", "sgx_mode", "quote_mode", "attestation", "gomaxprocs",
                 "netem_delay", "netem_jitter", "netem_loss", "netem_limit", "netem_dev",
                 "netem_lo_dev", "light_nodes", "topology", "topology_radius", "topology_max_degree",
                 "genesis_difficulty", "est_seconds_per_block", "on_tx_rate", "tx_per_block"):
        command.extend(["--" + name.replace("_", "-"), str(getattr(args, name))])
    if args.blocks_per_registered_node:
        command.extend(["--blocks-per-registered-node",
                        str(args.blocks_per_registered_node)])
    if args.light_remote:
        command.append("--light-remote")
    # Same-host delay is on by default, so only the opt-out needs forwarding.
    if args.no_netem_lo:
        command.append("--no-netem-lo")
    batch_id = uuid.uuid4().hex
    planned_runs = [{"run": number, "run_id": f"batch-{batch_id}-{number:03d}"}
                    for number in range(1, args.runs + 1)]
    for run in planned_runs:
        run["run_out_dir"] = str(args.local_root / "out" / run["run_id"])
        run["command"] = command + ["--run-id", run["run_id"]]
    expected = expected_counts(args, args.runs)
    manifest = {"created_at": datetime.now(timezone.utc).isoformat(), "batch_id": batch_id,
                "runs": args.runs, "command": command, "planned_runs": planned_runs,
                "topology": topo,
                "communication_stats": communication_stats.enabled(),
                "expected_samples": expected, "collector_schema_evidence": COLLECTOR_SCHEMA}
    if args.dry_run:
        print(json.dumps(manifest, ensure_ascii=False, indent=2))
        for run in planned_runs:
            print("command:", " ".join(shlex.quote(part) for part in run["command"]))
        return 0
    output_dir.mkdir(parents=True, exist_ok=False)
    (output_dir / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    all_samples: dict[str, list[dict[str, Any]]] = {}
    successful_runs: list[dict[str, Any]] = []
    failures: list[dict[str, Any]] = []
    cancellation: BaseException | None = None
    pending_signals: list[int] = []
    with cancellation_handlers(pending_signals):
        try:
            for run in planned_runs:
                number, run_id = run["run"], run["run_id"]
                run_out = Path(run["run_out_dir"])
                run_dir = output_dir / "runs" / f"run-{number:03d}"
                run_dir.mkdir(parents=True, exist_ok=False)
                run_log = run_dir / "e2e.log"
                result = {**run, "success": False, "returncode": None,
                          "log_path": str(run_log), "cleanup_errors": []}
                started = time.monotonic()
                launched = False
                run_samples = None
                try:
                    if run_out.exists():
                        raise ValueError(f"refusing existing run output: {run_out}")
                    print(f"[{number}/{args.runs}] starting run_id={run_id}", flush=True)
                    with run_log.open("x") as output:
                        launched = True
                        result["returncode"] = run_orchestrator(run["command"], output)
                    if result["returncode"] != 0:
                        raise RuntimeError(f"orchestrator returned {result['returncode']}")
                    run_samples = load_run_samples(run_out, run_id=run_id,
                                                   expected=expected_counts(args),
                                                   threshold=args.active_threshold)
                    result["success"] = True
                except BaseException as exc:
                    result["error"] = f"{type(exc).__name__}: {exc}"
                    if not isinstance(exc, Exception):
                        cancellation = exc
                finally:
                    with defer_cancellation(pending_signals):
                        # Valid run-status certifies cleanup even if timing validation fails.
                        if launched:
                            try:
                                validate_success_status(run_out, run_id)
                            except (OSError, ValueError):
                                try:
                                    stop_run(args, run_id, run_dir / "cleanup.log")
                                    result["fallback_stop_verified"] = True
                                except BaseException as exc:
                                    result["cleanup_errors"].append(f"{type(exc).__name__}: {exc}")
                                    result["success"] = False
                                    if not isinstance(exc, Exception) and cancellation is None:
                                        cancellation = exc
                        result["elapsed_seconds"] = round(time.monotonic() - started, 1)
                        (run_dir / "run.json").write_text(json.dumps(result, ensure_ascii=False, indent=2))
                if result["success"]:
                    with defer_cancellation(pending_signals):
                        merge_run_samples(all_samples, number, run_samples,
                                          run_id=run_id, run_out_dir=run_out)
                        successful_runs.append(result)
                    print(f"[{number}/{args.runs}] complete out={run_out}", flush=True)
                else:
                    failures.append(result)
                    print(f"[{number}/{args.runs}] FAILED; see {run_dir}", file=sys.stderr, flush=True)
                if pending_signals and cancellation is None:
                    cancellation = RunCancelled(pending_signals[0])
                if (cancellation is not None or result["cleanup_errors"]
                        or (failures and args.stop_on_failure)):
                    break
        except BaseException as exc:
            cancellation = exc
            failures.append({"error": f"{type(exc).__name__}: {exc}"})
        finally:
            with defer_cancellation(pending_signals):
                if pending_signals and cancellation is None:
                    cancellation = RunCancelled(pending_signals[0])
                if cancellation is not None:
                    failures.append({"cancellation": f"{type(cancellation).__name__}: {cancellation}"})
                try:
                    if len(successful_runs) != args.runs:
                        raise ValueError(f"complete batch requires {args.runs} successful runs, got {len(successful_runs)}")
                    validate_counts(all_samples, expected)
                except ValueError as exc:
                    failures.append({"batch_validation_error": str(exc)})
                complete = not failures and cancellation is None
                write_aggregate(output_dir, all_samples, successful_runs=successful_runs,
                                expected=expected, complete=complete)
                (output_dir / "failures.json").write_text(json.dumps(failures, ensure_ascii=False, indent=2))
            # An interrupt during the final artifact write must not be silently
            # swallowed or leave a batch marked complete with a cancellation exit.
            if pending_signals and cancellation is None:
                cancellation = RunCancelled(pending_signals[0])
                with defer_cancellation(pending_signals):
                    failures.append({"cancellation": str(cancellation)})
                    write_aggregate(output_dir, all_samples, successful_runs=successful_runs,
                                    expected=expected, complete=False)
                    (output_dir / "failures.json").write_text(json.dumps(failures, ensure_ascii=False, indent=2))
    print(f"results: {output_dir}")
    print(f"aggregate: {output_dir / 'aggregate.csv'}")
    if isinstance(cancellation, RunCancelled):
        return 128 + cancellation.signum
    if isinstance(cancellation, KeyboardInterrupt):
        return 130
    if cancellation is not None:
        raise cancellation
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
