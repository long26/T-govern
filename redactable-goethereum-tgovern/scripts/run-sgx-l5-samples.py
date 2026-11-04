#!/usr/bin/env python3
"""Run fixed-scale SGX L5 experiments and aggregate timing samples.

Each invocation is deliberately a fresh process because sgx_l5_sim_e2e.py
recreates .tmp/sgx-l5-sim.  This makes system_init measurements independent.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import shutil
import shlex
import signal
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from statistics import mean
from typing import Any

import communication_stats


ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / ".tmp" / "sgx-l5-sim"
E2E = ROOT / "scripts" / "sgx_l5_sim_e2e.py"
COLLECTOR = ROOT / "scripts" / "collect-l5-timing.py"
CONTROLLED_KEYS = {"TEE_ROTATIONS", "TEE_EDITS_PER_EPOCH", "TEE_BASELINE_EDITS"}
REQUIRED_STAGES = ("system_init", "edit_execution", "rotation_flow")
CAPTURE_ENV_PREFIXES = ("TEE_", "P2P_", "GETH_", "LIGHT_", "CORE_", "ON_", "COMMITTEE_", "NETEM_", "TGOVERN_COMM_")
CAPTURE_ENV_NAMES = {
    "SGX_MODE", "EXTRA_MINER_COUNT", "EPOCH_LENGTH", "STEADY_EPOCH_LENGTH",
    "ETHASH_CACHE_DIR", "TXMODIFY_RELAY_TTL",
}


def read_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line_number, raw_line in enumerate(path.read_text().splitlines(), start=1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        key, separator, raw_value = line.partition("=")
        if not separator or not key or not key.replace("_", "").isalnum() or not key[0].isalpha():
            raise ValueError(f"{path}:{line_number}: expected KEY=VALUE")
        try:
            tokens = shlex.split(raw_value, comments=True, posix=True)
        except ValueError as exc:
            raise ValueError(f"{path}:{line_number}: invalid value: {exc}") from exc
        if len(tokens) != 1:
            raise ValueError(f"{path}:{line_number}: value must be one shell token")
        values[key] = tokens[0]
    forbidden = sorted(CONTROLLED_KEYS.intersection(values))
    if forbidden:
        raise ValueError(f"{path}: controlled values belong on the command line: {', '.join(forbidden)}")
    return values


def captured_environment() -> dict[str, str]:
    """Record SGX L5 configuration supplied through the command environment."""
    return {
        key: value
        for key, value in sorted(os.environ.items())
        if key not in CONTROLLED_KEYS
        and (key in CAPTURE_ENV_NAMES or key.startswith(CAPTURE_ENV_PREFIXES))
    }


def percentile(values: list[float], p: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = (len(ordered) - 1) * p
    lower, upper = math.floor(index), math.ceil(index)
    if lower == upper:
        return round(ordered[lower], 3)
    return round(ordered[lower] + (ordered[upper] - ordered[lower]) * (index - lower), 3)


def filtered_mean(durations: list[float], iqr_multiplier: float) -> float | None:
    """Mean after Tukey IQR filtering, mirroring collect-l5-timing.py stage_row().

    Values outside [q1 - multiplier*IQR, q3 + multiplier*IQR] are excluded;
    filtering only applies once a stage has at least 30 samples.
    """
    if not durations:
        return None
    used = durations
    if len(durations) >= 30:
        q1 = percentile(durations, 0.25)
        q3 = percentile(durations, 0.75)
        iqr = q3 - q1
        lower = q1 - iqr_multiplier * iqr
        upper = q3 + iqr_multiplier * iqr
        used = [value for value in durations if lower <= value <= upper]
    return round(mean(used), 3)


def stage_stats(samples: list[dict[str, Any]]) -> dict[str, float | int | None]:
    durations = [float(sample["duration_ms"]) for sample in samples]
    return {
        "count": len(durations),
        "min_ms": round(min(durations), 3) if durations else None,
        "mean_ms": round(mean(durations), 3) if durations else None,
        "filtered_mean_1_5iqr_ms": filtered_mean(durations, 1.5),
        "filtered_mean_3iqr_ms": filtered_mean(durations, 3.0),
        "p50_ms": percentile(durations, 0.50),
        "p95_ms": percentile(durations, 0.95),
        "p99_ms": percentile(durations, 0.99),
        "max_ms": round(max(durations), 3) if durations else None,
    }


def collect_run(run_dir: Path, threshold: int) -> dict[str, list[dict[str, Any]]]:
    raw_dir = run_dir / "raw"
    timing_dir = run_dir / "timing"
    command = [
        sys.executable, str(COLLECTOR), "--logs-dir", str(raw_dir / "logs"),
        "--progress", str(raw_dir / "e2e-progress.log"), "--out-dir", str(timing_dir),
        "--threshold", str(threshold),
    ]
    with (run_dir / "collector.log").open("w") as output:
        subprocess.run(command, cwd=ROOT, stdout=output, stderr=subprocess.STDOUT, check=True)
    return json.loads((timing_dir / "l5-timing-summary.json").read_text())["samples"]


def archive_run(run_dir: Path, archive_workdir: bool) -> None:
    """Preserve the collector inputs before the next E2E run clears WORK."""
    if not WORK.exists():
        return
    raw_dir = run_dir / "raw"
    if archive_workdir:
        shutil.copytree(WORK, raw_dir)
        return
    raw_dir.mkdir()
    logs = WORK / "logs"
    if logs.is_dir():
        shutil.copytree(logs, raw_dir / "logs")
    progress = WORK / "e2e-progress.log"
    if progress.is_file():
        shutil.copy2(progress, raw_dir / progress.name)


def process_group_exists(pgid: int) -> bool:
    try:
        os.killpg(pgid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def cleanup_process_group(pgid: int, grace_seconds: float = 8.0) -> None:
    """Stop processes left behind by one E2E run without touching other runs."""
    if not process_group_exists(pgid):
        return
    try:
        os.killpg(pgid, signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.monotonic() + grace_seconds
    while process_group_exists(pgid) and time.monotonic() < deadline:
        time.sleep(0.1)
    if process_group_exists(pgid):
        try:
            os.killpg(pgid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def run_e2e(command: list[str], env: dict[str, str], output: Any) -> int:
    """Run one E2E in an isolated process group and always reap its children."""
    process: subprocess.Popen[Any] | None = None
    try:
        process = subprocess.Popen(
            command,
            cwd=ROOT,
            env=env,
            stdout=output,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        return process.wait()
    finally:
        if process is not None:
            cleanup_process_group(process.pid)


def merge_run_samples(
    all_samples: dict[str, list[dict[str, Any]]],
    run_number: int,
    run_samples: dict[str, Any],
) -> None:
    """Merge one run's samples; dict-valued stages (e.g. tee_registration_substages
    from the collector) flatten to one aggregate stage per sub-stage."""
    for stage, stage_samples in run_samples.items():
        if isinstance(stage_samples, dict):
            for substage, sub_samples in stage_samples.items():
                all_samples.setdefault(f"{stage}:{substage}", []).extend(
                    {"run": run_number, **sample} for sample in sub_samples
                )
            continue
        all_samples.setdefault(stage, []).extend(
            {"run": run_number, **sample} for sample in stage_samples
        )


def write_aggregate(output_dir: Path, samples: dict[str, list[dict[str, Any]]]) -> None:
    stats = {stage: stage_stats(stage_samples) for stage, stage_samples in samples.items()}
    payload = {"stage_statistics": stats, "samples": samples}
    (output_dir / "aggregate.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2))
    with (output_dir / "aggregate.csv").open("w", newline="") as output:
        writer = csv.DictWriter(
            output,
            fieldnames=[
                "stage", "count", "min_ms", "mean_ms",
                "filtered_mean_1_5iqr_ms", "filtered_mean_3iqr_ms",
                "p50_ms", "p95_ms", "p99_ms", "max_ms",
            ],
        )
        writer.writeheader()
        for stage in sorted(stats):
            writer.writerow({"stage": stage, **stats[stage]})


def main() -> int:
    parser = argparse.ArgumentParser(description="Repeat a fixed-scale SGX L5 run and aggregate timing samples")
    parser.add_argument("--base-env", type=Path, help="optional fixed-scale KEY=VALUE file; command environment is used otherwise")
    parser.add_argument("--runs", type=int, default=100, help="fresh E2E executions (default: 100)")
    parser.add_argument("--rotations", type=int, default=1, help="rotations per execution (default: 1)")
    parser.add_argument("--edits", type=int, default=1, help="edits after each rotation (default: 1)")
    parser.add_argument("--baseline-edits", type=int, default=0,
                        help="baseline edits in epoch 0 before rotations (default: 0); "
                        "with --rotations 0 this is the only source of edit samples")
    parser.add_argument("--threshold", type=int, help="DKRS threshold; defaults to TEE_ACTIVE_THRESHOLD in --base-env")
    parser.add_argument("--output-root", type=Path, default=ROOT / "results" / "sgx-l5")
    parser.add_argument("--tag", help="result directory name; default is UTC timestamp")
    failure_policy = parser.add_mutually_exclusive_group()
    failure_policy.add_argument(
        "--keep-going", dest="stop_on_failure", action="store_false",
        help="archive failed runs and continue (default; retained for compatibility)",
    )
    failure_policy.add_argument(
        "--stop-on-failure", action="store_true",
        help="stop the batch at the first failed run",
    )
    parser.set_defaults(stop_on_failure=False)
    parser.add_argument("--archive-workdir", action="store_true", help="also retain chain and node datadirs; default stores timing inputs only")
    parser.add_argument(
        "--resume", action="store_true",
        help="continue an existing output directory: reload samples from completed runs' "
             "timing summaries, rerun runs whose timing summary is missing, then re-aggregate",
    )
    parser.add_argument("--dry-run", action="store_true", help="validate parameters and print the command only")
    args = parser.parse_args()
    if args.runs < 1 or args.rotations < 0 or args.edits < 1 or args.baseline_edits < 0:
        parser.error("--runs must be positive; --rotations/--baseline-edits may be 0; --edits must be positive")
    if args.rotations * args.edits + args.baseline_edits == 0:
        parser.error("no edits would run: use --baseline-edits > 0 when --rotations is 0")
    conflicting_env = sorted(key for key in CONTROLLED_KEYS if key in os.environ)
    if conflicting_env:
        parser.error(
            "remove controlled environment variables and use --rotations/--edits instead: "
            + ", ".join(conflicting_env)
        )
    if args.base_env is not None and not args.base_env.is_file():
        parser.error(f"--base-env does not exist: {args.base_env}")
    try:
        fixed_env = captured_environment()
        if args.base_env is not None:
            fixed_env.update(read_env_file(args.base_env))
        threshold = args.threshold or int(fixed_env.get("TEE_ACTIVE_THRESHOLD", "3"))
    except ValueError as exc:
        parser.error(str(exc))
    if threshold < 1:
        parser.error("--threshold must be positive")

    tag = args.tag or datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    output_dir = args.output_root / tag
    if output_dir.exists() and not args.resume:
        parser.error(f"result directory already exists: {output_dir}")
    if args.resume and not output_dir.exists():
        parser.error(f"--resume target does not exist: {output_dir}")
    controlled = {
        "TEE_ROTATIONS": str(args.rotations),
        "TEE_EDITS_PER_EPOCH": str(args.edits),
        "TEE_BASELINE_EDITS": str(args.baseline_edits),
    }
    manifest = {
        "created_at": datetime.now(timezone.utc).isoformat(),
        "base_env_file": str(args.base_env.resolve()) if args.base_env is not None else None,
        "fixed_scale_parameters": fixed_env,
        "controlled_parameters": controlled,
        "runs": args.runs,
        "rotation_threshold": threshold,
        "stop_on_failure": args.stop_on_failure,
        "archive_workdir": args.archive_workdir,
        "expected_samples": {
            "system_init": args.runs,
            "rotation_flow": args.runs * args.rotations,
            "edit_execution": args.runs * args.edits * (args.rotations + args.baseline_edits),
        },
    }
    command = [sys.executable, str(E2E)]
    if args.dry_run:
        print(json.dumps(manifest, ensure_ascii=False, indent=2))
        print("command:", " ".join(shlex.quote(part) for part in command))
        return 0

    # Collection must use the same effective switch as the child, including --base-env.
    os.environ["TGOVERN_COMM_STATS"] = fixed_env.get("TGOVERN_COMM_STATS", "0")
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    all_samples: dict[str, list[dict[str, Any]]] = {}
    failures: list[dict[str, Any]] = []
    for run_number in range(1, args.runs + 1):
        run_dir = output_dir / "runs" / f"run-{run_number:03d}"
        summary_path = run_dir / "timing" / "l5-timing-summary.json"
        if args.resume and summary_path.is_file():
            try:
                reloaded = json.loads(summary_path.read_text())["samples"]
            except (OSError, ValueError, KeyError) as exc:
                print(f"[{run_number}/{args.runs}] resume: cannot reload {summary_path}: {exc}; rerunning",
                      file=sys.stderr, flush=True)
            else:
                merge_run_samples(all_samples, run_number, reloaded)
                print(f"[{run_number}/{args.runs}] resumed from {summary_path}", flush=True)
                continue
        run_dir.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update(fixed_env)
        env.update(controlled)
        print(f"[{run_number}/{args.runs}] starting", flush=True)
        returncode: int | None = None
        run_error: OSError | None = None
        try:
            with (run_dir / "e2e.log").open("w") as output:
                returncode = run_e2e(command, env, output)
        except OSError as exc:
            run_error = exc
        archive_error: OSError | None = None
        try:
            archive_run(run_dir, args.archive_workdir)
            if communication_stats.enabled():
                try:
                    communication_stats.collect_single(run_dir / "raw", run_dir / "communication", ROOT / "scripts")
                except Exception as exc:
                    (run_dir / "communication-error.json").write_text(json.dumps({"error": repr(exc)}))
                    print(f"communication collection failed: {exc}", file=sys.stderr)
        except OSError as exc:
            archive_error = exc
        if run_error is not None or returncode != 0 or archive_error is not None:
            failure: dict[str, Any] = {"run": run_number, "returncode": returncode}
            if run_error is not None:
                failure["runner_error"] = str(run_error)
            if archive_error is not None:
                failure["archive_error"] = str(archive_error)
            failures.append(failure)
            archive_status = (
                "artifacts archived"
                if archive_error is None
                else f"artifact archive failed: {archive_error}"
            )
            print(
                f"[{run_number}/{args.runs}] failed; cleanup finished; {archive_status} "
                f"(see {run_dir / 'e2e.log'})",
                file=sys.stderr, flush=True,
            )
            if args.stop_on_failure:
                break
            continue
        try:
            run_samples = collect_run(run_dir, threshold)
        except (
            subprocess.CalledProcessError,
            OSError,
            json.JSONDecodeError,
            KeyError,
            TypeError,
            ValueError,
        ) as exc:
            failures.append({"run": run_number, "collector_error": str(exc)})
            print(f"[{run_number}/{args.runs}] collection failed: {exc}", file=sys.stderr, flush=True)
            if args.stop_on_failure:
                break
            continue
        merge_run_samples(all_samples, run_number, run_samples)
        print(f"[{run_number}/{args.runs}] complete", flush=True)

    write_aggregate(output_dir, all_samples)
    if communication_stats.enabled():
        try:
            communication_stats.aggregate_batch(output_dir, [
                output_dir / "runs" / f"run-{i:03d}" / "communication" / "communication-summary.json"
                for i in range(1, args.runs + 1)])
        except Exception as exc:
            print(f"communication aggregation failed: {exc!r}", file=sys.stderr)
    (output_dir / "failures.json").write_text(json.dumps(failures, ensure_ascii=False, indent=2))
    print(f"results: {output_dir}")
    print(f"aggregate: {output_dir / 'aggregate.csv'}")
    return 0 if not failures else 1


if __name__ == "__main__":
    raise SystemExit(main())
