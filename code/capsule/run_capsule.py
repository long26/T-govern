#!/usr/bin/env python3
"""Offline Code Ocean entry point. SGX execution and mock attestation are distinct."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import uuid


from experiment_config import EXPERIMENTS
from result_export import export_results


ROOT = Path(__file__).resolve().parents[1]
MODULES = EXPERIMENTS['sgx'].modules


@dataclass(frozen=True)
class Config:
    experiment: str
    runs: int
    rotations: int
    edits: int
    timeout_seconds: int
    results_dir: str
    cache: str
    run_id: str
    sgx_mode: str | None = 'SIM'
    quote_mode: str | None = 'mock'
    attestation_provider: str | None = 'noop'
    steady_epoch_length: int | None = 200

    @classmethod
    def from_environment(cls) -> Config:
        def count(name: str, default: int, minimum: int) -> int:
            raw = os.environ.get(name, str(default))
            if not re.fullmatch(r'[0-9]+', raw) or int(raw) < minimum:
                raise ValueError(f'{name} must be an integer >= {minimum}')
            return int(raw)

        experiment = os.environ.get('CO_EXPERIMENT', 'sgx')
        if experiment not in EXPERIMENTS:
            raise ValueError('CO_EXPERIMENT must be sgx or shamir or nitch')
        if experiment != 'nitch':
            for name, expected in (('SGX_MODE', 'SIM'), ('TEE_QUOTE_MODE', 'mock'),
                                   ('TEE_ATTESTATION_PROVIDER', 'noop')):
                value = os.environ.get(name, expected)
                if value != expected:
                    raise ValueError(f'{name} must be {expected}, got {value!r}')
        steady_epoch_length = None
        if experiment == 'sgx':
            name = 'TEE_STEADY_EPOCH_LENGTH' if os.environ.get('TEE_STEADY_EPOCH_LENGTH', '').strip() else 'STEADY_EPOCH_LENGTH'
            steady_epoch_length = count(name, 200, 21)
        run_id = os.environ.get('CO_RUN_ID', datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8])
        if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,95}', run_id):
            raise ValueError('CO_RUN_ID must be a single safe directory name')
        return cls(experiment, count('CO_RUNS', 1, 1), count('CO_ROTATIONS', 1, 0),
                   count('CO_EDITS', 5, 1), count('CO_TIMEOUT_SECONDS', 3600, 1),
                   str(Path(os.environ.get('CO_RESULTS_DIR', '/results')).resolve()),
                   str(Path(os.environ.get('CO_GO_CACHE', '/opt/tgovern/cache/' + EXPERIMENTS[experiment].cache_name)).resolve()), run_id,
                   steady_epoch_length=steady_epoch_length,
                   **({'sgx_mode': None, 'quote_mode': None, 'attestation_provider': None} if experiment == 'nitch' else {}))


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def restore_cache(archive: Path, destination: Path, modules: tuple[str, ...] = MODULES) -> None:
    checksum = archive.with_name(archive.name + '.sha256').read_text().split()[0]
    if sha256(archive) != checksum:
        raise ValueError('Go cache archive checksum mismatch')
    with tarfile.open(archive, 'r:gz') as source:
        members = source.getmembers()
        for member in members:
            target = (destination / member.name).resolve()
            if not target.is_relative_to(destination.resolve()) or not (member.isfile() or member.isdir()):
                raise ValueError(f'unsafe Go cache member: {member.name}')
        manifest_member = source.extractfile('manifest.json')
        if manifest_member is None:
            raise ValueError('Go cache manifest missing')
        manifest = json.load(manifest_member)
        expected = {f'{module}/{name}': sha256(ROOT / module / name)
                    for module in modules for name in ('go.mod', 'go.sum')}
        if manifest != {'go_version': 'go1.20.14', 'target': 'linux/amd64', 'inputs': expected}:
            raise ValueError('Go cache is stale or incompatible; regenerate with prepare-go-cache.sh')
        source.extractall(destination, members=members)


def execute(command: list[str], cwd: Path, env: dict[str, str], log, timeout: int) -> None:
    print('$ ' + shlex.join(command), flush=True)
    log.write('$ ' + shlex.join(command) + '\n')
    log.flush()
    process = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                               stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = process.wait(timeout=timeout)
        if code:
            raise subprocess.CalledProcessError(code, command)
    finally:
        # Only this command's session belongs to us, including orphaned children.
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            pass
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()


def validate_samples(output: Path, config: Config) -> None:
    samples = json.loads((output / 'aggregate.json').read_text())['samples']
    required = {
        'system_init': config.runs,
        'edit_execution': config.runs * (config.rotations * config.edits if config.rotations else 1),
    }
    if config.rotations:
        required['rotation_flow'] = config.runs * config.rotations
    for stage, minimum in required.items():
        observed = len(samples.get(stage, []))
        if observed < minimum:
            raise RuntimeError(f'incomplete {stage} samples: got {observed}, need at least {minimum}')


def stage_sources(destination: Path, modules: tuple[str, ...] = MODULES) -> None:
    for module in modules:
        source = ROOT / module
        ignored_artifacts = shutil.ignore_patterns('.git', '.tmp', '__pycache__',
                                                   '*.so', '*.o', '*.a', '*.exe')

        def ignore(directory: str, names: list[str]) -> set[str]:
            excluded = ignored_artifacts(directory, names)
            if Path(directory) == source:
                excluded.update({'chain', 'results'})
                if module == 'redactable-goethereum-wolverine':
                    excluded.update({'nodelogs', 'e2e-progress.log', 'node_registry.json', 'node_registry.json.lock'})
                    excluded.update(name for name in names if re.fullmatch(r'node[0-9]+', name))
            return excluded

        shutil.copytree(source, destination / module, ignore=ignore)


def validate_nitch_timing(run_dir: Path, rotations: int) -> None:
    summary = json.loads((run_dir / 'timing' / 'timing-summary.json').read_text(encoding='utf-8'))
    rows = {row['stage']: row for row in summary['stages']}
    required = ['system_init', 'transaction_generation', 'transaction_validation', 'block_generation',
                'block_validation', 'edit_execution', 'ledger_update', 'ledger_update_e2e']
    if rotations:
        required.append('rotation_flow')
    for stage in required:
        row = rows.get(stage, {})
        if row.get('count', 0) < 1 or row.get('duration_ms', '') == '':
            raise RuntimeError(f'incomplete nitch timing stage: {stage}')
    ignored = {'rotation_flow', 'rotation_flow_span', 'rotation_flow_round'} if not rotations else set()
    alerts = [alert for alert in summary['alerts'] if not any(
        alert.startswith(f'[count=0] stage={stage} ') for stage in ignored)]
    if alerts:
        raise RuntimeError('nitch collector completeness failed: ' + '; '.join(alerts))


def run_nitch(config: Config, output: Path, work: Path, env: dict[str, str], log) -> None:
    project = work / 'redactable-goethereum-wolverine'
    binary = project / 'build' / 'bin' / 'geth'

    def call(command: list[str], cwd: Path = project) -> None:
        execute(command, cwd, env, log, config.timeout_seconds)

    call(['make', '-C', 'chameleon/nitchcore', 'selftest'])
    call(['go', 'build', '-o', str(binary), './cmd/geth'])
    linkage = subprocess.check_output(['ldd', str(binary)], env=env, text=True)
    (output / 'nitch-linkage.txt').write_text(linkage, encoding='utf-8')
    if 'not found' in linkage or 'libsgx' in linkage.lower():
        raise RuntimeError('nitch must resolve all libraries without an SGX runtime')
    call([str(binary), 'makedag', '0', str(Path(env['HOME']) / '.ethash')])
    target = output / 'nitch'
    target.mkdir()
    completed = []
    for number in range(1, config.runs + 1):
        run_dir = target / f'run-{number:03d}'
        call([sys.executable, str(project / 'scripts' / 'nitch_e2e.py'),
              '--geth', str(binary), '--workdir', str(work / f'nitch-run-{number:03d}'),
              '--output', str(run_dir), '--run-id', f'{config.run_id}-{number:03d}',
              '--rotations', str(config.rotations), '--edits', str(config.edits)])
        verified = json.loads((run_dir / 'verification.json').read_text(encoding='utf-8'))
        if (not verified.get('verified') or verified.get('rotation_count', 0) < config.rotations
                or verified.get('edit_count', 0) < max(1, config.rotations) * config.edits):
            raise RuntimeError(f'incomplete nitch verification in {run_dir}')
        call([sys.executable, str(project / 'scripts' / 'collect_perf.py'),
              '--logs-dir', str(run_dir / 'logs'), '--progress', str(run_dir / 'e2e-progress.log'),
              '--manifest', str(run_dir / 'run-manifest.json'), '--out-dir', str(run_dir / 'timing')])
        validate_nitch_timing(run_dir, config.rotations)
        completed.append({'run': number, 'path': run_dir.name, 'verification': verified})
        (target / 'index.json').write_text(json.dumps(completed, indent=2) + '\n', encoding='utf-8')


def run_experiments(config: Config, output: Path, log) -> None:
    if platform.system() != 'Linux' or platform.machine() not in {'x86_64', 'amd64'}:
        raise RuntimeError('Linux amd64 is required for capsule execution; use the supplied Docker environment')
    if subprocess.check_output(['go', 'env', 'GOVERSION'], text=True).strip() != 'go1.20.14':
        raise RuntimeError('Requires the supplied Go 1.20.14 toolchain')
    with tempfile.TemporaryDirectory(prefix='tgovern-capsule-') as directory:
        work = Path(directory)
        modules = EXPERIMENTS[config.experiment].modules
        restore_cache(Path(config.cache), work / 'modules', modules)
        stage_sources(work, modules)
        geth = work / 'redactable-goethereum-tgovern'
        committee = work / 'committee'
        sdk = Path(os.environ.get('SGX_SDK', '/opt/intel/sgxsdk'))
        dataset_dir = work / 'home' / '.ethash'
        dataset_dir.mkdir(parents=True)
        env = dict(os.environ, HOME=str(dataset_dir.parent), SGX_MODE='SIM', TEE_QUOTE_MODE='mock', TEE_ATTESTATION_PROVIDER='noop',
                   SGX_SDK=str(sdk), GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local',
                   GOMODCACHE=str(work / 'modules'), GOCACHE=str(work / 'go-build'),
                   GOPATH=str(work / 'gopath'), CGO_ENABLED='1', GOFLAGS='-mod=readonly',
                   PYTHONUNBUFFERED='1', PYTHONUTF8='1', NO_PROXY='localhost,127.0.0.1',
                   no_proxy='localhost,127.0.0.1')
        env['PKG_CONFIG_PATH'] = str(sdk / 'pkgconfig') + ':' + env.get('PKG_CONFIG_PATH', '')
        env['LD_LIBRARY_PATH'] = ':'.join((str(geth / 'sgx'), str(sdk / 'sdk_libs'),
                                            str(sdk / 'lib64'), env.get('LD_LIBRARY_PATH', '')))

        def call(command: list[str], cwd: Path = work) -> None:
            execute(command, cwd, env, log, config.timeout_seconds)

        call(['go', 'version'])
        call([sys.executable, '-m', 'pip', 'freeze'])
        if config.experiment == 'nitch':
            for name in ('SGX_MODE', 'SGX_SDK', 'SGX_ENCLAVE_PATH', 'SGX_MODULE_DIR',
                         'TEE_QUOTE_MODE', 'TEE_ATTESTATION_PROVIDER', 'PKG_CONFIG_PATH', 'LD_LIBRARY_PATH'):
                env.pop(name, None)
            run_nitch(config, output, work, env, log)
            return
        call(['go', 'test', './internal/share', '-run', 'TestSplitRecoverThreshold', '-v'], committee)
        if config.experiment == 'shamir':
            return
        call(['go', 'build', '-o', str(committee / 'build' / 'committee-mvp'), './cmd/committee-mvp'], committee)
        if config.experiment == 'sgx':
            miracl = work / 'miracl-build'
            shutil.copytree(geth / 'sgx' / 'lib' / 'miracl', miracl)
            option = subprocess.check_output(
                [sys.executable, '-c', 'import config64; c = config64.miracl_crypto; '
                 'print(next(i for i, curve in enumerate(c.np_curves + c.pf_curves, 1) if curve[2] == "BLS12381"))'],
                cwd=miracl, env=env, text=True).strip()
            # This archive links against enclave libc, which has no glibc __printf_chk.
            miracl_env = dict(env, CXX='g++', CXXFLAGS='-fPIC -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0')
            execute([sys.executable, 'config64.py', '--options=' + option], miracl,
                    miracl_env, log, config.timeout_seconds)
            shutil.copy2(miracl / 'core.a', geth / 'sgx' / 'lib' / 'miracl' / 'libcore.a')
            call(['make', 'geth-sgx', 'SGX_MODE=SIM', 'SGX_SDK=' + str(sdk)], geth)
            binary = geth / 'build' / 'bin' / 'geth-sgx'
            linkage = subprocess.check_output(['ldd', str(binary)], env=env, text=True)
            (output / 'sgx-linkage.txt').write_text(linkage)
            if 'not found' in linkage or 'libsgx_urts_sim.so' not in linkage:
                raise RuntimeError('geth-sgx must resolve all libraries and link the SIM runtime')
            env.update(GETH_BIN=str(binary), TGOVERN_ROOT=str(geth),
                       COMMITTEE_BIN=str(committee / 'build' / 'committee-mvp'),
                       COMMITTEE_SRC_DIR=str(committee), COMMITTEE_CWD=str(committee),
                       SGX_ENCLAVE_PATH=str(geth / 'sgx' / 'enclave.signed.so'), SGX_MODULE_DIR=str(geth / 'sgx'))
            for key in ('TEE_ROTATIONS', 'TEE_EDITS_PER_EPOCH', 'TEE_BASELINE_EDITS'):
                env.pop(key, None)
            env['TEE_STEADY_EPOCH_LENGTH'] = str(config.steady_epoch_length)
            env.setdefault('TEE_ACTIVE_LIMIT', '4')
            env.setdefault('TEE_ACTIVE_THRESHOLD', '3')
            env.setdefault('TEE_CANDIDATE_COUNT', '4')
            env.setdefault('COMMITTEE_COUNT', env.get('TEE_COMMITTEE_COUNT', '4'))
            env.setdefault('COMMITTEE_THRESHOLD', env.get('TEE_COMMITTEE_THRESHOLD', '3'))
            call(['go', 'test', '-tags', 'sgx', '-run', 'TestPartialAdaptContributionCrossLib', '-v', './sgx/'], geth)
            call([str(binary), 'makedag', '0', str(dataset_dir)], geth)
            try:
                call([sys.executable, str(geth / 'scripts' / 'run-sgx-l5-samples.py'),
                      '--runs', str(config.runs), '--rotations', str(config.rotations), '--edits', str(config.edits),
                      '--baseline-edits', '1' if config.rotations == 0 else '0', '--stop-on-failure',
                      '--output-root', str(output), '--tag', 'sgx-sim'], geth)
                validate_samples(output / 'sgx-sim', config)
            finally:
                source = geth / '.tmp' / 'sgx-l5-sim'
                diagnostics = output / 'sgx-last-run'
                diagnostics.mkdir()
                if (source / 'logs').is_dir():
                    shutil.copytree(source / 'logs', diagnostics / 'logs')
                if (source / 'e2e-progress.log').is_file():
                    shutil.copy2(source / 'e2e-progress.log', diagnostics / 'e2e-progress.log')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='print validated configuration without building or running')
    args = parser.parse_args()
    try:
        config = Config.from_environment()
    except ValueError as exc:
        parser.error(str(exc))
    if args.check:
        print(json.dumps(asdict(config), indent=2))
        return 0
    output = Path(config.results_dir) / config.run_id
    try:
        output.mkdir(parents=True, exist_ok=False)
    except FileExistsError:
        parser.error(f'result directory already exists: {output}')
    (output / 'manifest.json').write_text(json.dumps(asdict(config), indent=2) + '\n')
    status = {'state': 'running', 'exit_code': None, 'error': None}
    (output / 'status.json').write_text(json.dumps(status, indent=2) + '\n')
    print(f'Results: {output}', flush=True)
    code = 0
    with (output / 'run.log').open('w', encoding='utf-8') as log:
        def interrupted(signum, _frame):
            raise SystemExit(128 + signum)

        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        try:
            run_experiments(config, output, log)
            status.update(state='completed', exit_code=0)
        except (Exception, SystemExit) as exc:
            code = exc.code if isinstance(exc, SystemExit) else 1
            if isinstance(exc, subprocess.CalledProcessError):
                code = exc.returncode if exc.returncode > 0 else 128 - exc.returncode
            status.update(state='failed', exit_code=code, error=str(exc))
            log.write(f'error: {exc}\n')
            print(f'error: {exc}; see {output / "run.log"}', file=sys.stderr)
        finally:
            try:
                export_results(output, output, status=status, in_place=True)
            except Exception as export_error:
                log.write(f'result export error: {export_error}\n')
                if status['state'] == 'completed':
                    code = 1
                    status.update(state='failed', exit_code=code, error=f'result export failed: {export_error}')
                else:
                    status['export_error'] = str(export_error)
                summary_path = output / 'summary.json'
                if summary_path.exists():
                    summary = json.loads(summary_path.read_text(encoding='utf-8'))
                    summary['status'] = status
                    summary_path.write_text(json.dumps(summary, indent=2) + '\n', encoding='utf-8')
            (output / 'status.json').write_text(json.dumps(status, indent=2) + '\n')
    return code


if __name__ == '__main__':
    sys.exit(main())
