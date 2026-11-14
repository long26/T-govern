"""Export both capsule systems without changing their measurement definitions."""
from __future__ import annotations

import argparse
import csv
import importlib.util
import json
import os
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
CORE = ('system_init', 'transaction_generation', 'transaction_validation', 'block_generation',
        'block_validation', 'edit_execution', 'ledger_update', 'rotation_flow')
METRIC_FIELDS = ('system', 'run_id', 'sample_run', 'stage', 'metric_definition', 'stratum',
                 'availability', 'unit', 'raw_count', 'used_count', 'excluded_count',
                 'raw_mean_ms', 'filtered_mean_ms', 'p50_ms', 'p95_ms', 'p99_ms', 'max_ms', 'basis', 'source')
SAMPLE_FIELDS = ('system', 'run_id', 'sample_run', 'stage', 'metric_definition', 'stratum',
                 'sample_id', 'duration_ms', 'epoch', 'request_id', 'node_id', 'status', 'source')
HIDDEN_STAGES = {'transaction_generation', 'transaction_validation', 'execute_redaction_confirmation',
                 'ledger_update_e2e', 'ledger_update_total', 'rotation_bootstrap'}
CSV_METRIC_FIELDS = tuple(field for field in METRIC_FIELDS
                          if field not in {'metric_definition', 'stratum', 'basis', 'source', 'availability'})
CSV_SAMPLE_FIELDS = tuple(field for field in SAMPLE_FIELDS
                          if field not in {'metric_definition', 'stratum', 'source', 'sample_id'}) + ('sample_index',)
FILES = ('summary.json', 'summary.csv', 'samples.csv', 'report.md')


def clean_detail_csvs(root: Path):
    removed_fields = {'metric_definition', 'stratum', 'basis', 'source', 'note', 'definition',
                      'headline', 'aggregation', 'aggregation_mode', 'event', 'sample_id'}
    for project in ('nitch', 'sgx-sim', 'sgx-last-run'):
        for path in (root / project).rglob('*.csv'):
            with path.open(encoding='utf-8', newline='') as stream:
                reader = csv.DictReader(stream)
                original = reader.fieldnames or []
                rows = list(reader)
            if 'stage' not in original:
                continue
            fields = [field for field in original if field not in removed_fields]
            selected = [row for row in rows if row.get('stage') not in HIDDEN_STAGES
                        and not (row.get('stage') == 'rotation_flow' and str(row.get('epoch')) == '0')]
            with path.open('w', encoding='utf-8', newline='') as stream:
                writer = csv.DictWriter(stream, fieldnames=fields, extrasaction='ignore')
                writer.writeheader()
                writer.writerows(selected)


def load(path):
    return json.loads(path.read_text(encoding='utf-8'))


def collector(system):
    relative = ('redactable-goethereum-wolverine/scripts/collect_perf.py' if system == 'nitch'
                else 'redactable-goethereum-tgovern/scripts/collect-l5-timing.py')
    spec = importlib.util.spec_from_file_location('export_' + system, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def definition(system, stage, basis):
    special = {
        ('nitch', 'system_init'): 'native_prepare_to_dkg_ready',
        ('tgovern', 'system_init'): 'initialization_segments_excluding_tee_registration',
        ('nitch', 'edit_execution'): 'rpc_entry_to_modify_broadcast',
        ('tgovern', 'edit_execution'): 'committee_submission_to_modify_broadcast',
        ('nitch', 'rotation_flow'): 'cluster_reshare_start_to_all_apply',
        ('tgovern', 'rotation_flow'): 'epoch_trigger_mint_to_cluster_dkrs_complete',
        ('tgovern', 'rotation_bootstrap'): 'epoch_zero_trigger_to_dkrs_complete_diagnostic',
    }
    return special.get((system, stage), stage + ':' + basis)


def native_samples(module, data, row):
    records = [r for r in data['records'] if r.get('source') == 'geth']
    progress = [r for r in data['records'] if r.get('source') == 'script']
    stage = row['stage']
    headline, kind, _ = module.STAGE_HEADLINE[stage]
    if kind == 'mean':
        return module.event_samples(records, headline)
    if kind == 'aligned_block':
        return module.aligned_block_samples(records, headline)[0]
    if kind == 'segment_span':
        return module.numeric_samples(module.compute_segment_spans(progress, stage, 'phase_start', 'phase_end'), stage)
    if kind == 'rotation_global':
        return module.compute_rotation_global_samples(records, progress)
    if kind == 'rotation_quorum':
        return module.compute_rotation_samples(records, int(data['run_metadata']['threshold']['required_shares']))
    if kind == 'rotation_round':
        return module.compute_rotation_round_samples(records)
    if kind == 'receiver_update':
        return module.compute_ledger_update_samples(records)
    if kind == 'receiver_update_total':
        return module.compute_ledger_update_total_samples(records)
    if kind in ('ledger_e2e', 'ledger_convergence'):
        pair = module.compute_ledger_spans(progress)
        return module.numeric_samples(pair[0 if kind == 'ledger_e2e' else 1], stage)
    raise ValueError(f'unsupported native metric: {stage}/{kind}')


def export_results(result_dir: Path, output_dir: Path, status=None, in_place=False):
    result_dir, output_dir = result_dir.resolve(), output_dir.resolve()
    manifest = load(result_dir / 'manifest.json')
    state = status if status is not None else load(result_dir / 'status.json')
    mode = manifest['experiment']
    system = 'tgovern' if mode == 'sgx' else mode
    if in_place:
        if output_dir != result_dir:
            raise ValueError('in-place export must target the result root')
        if any((output_dir / name).exists() for name in FILES):
            raise FileExistsError('unified output already exists')
    elif output_dir.exists():
        raise FileExistsError(f'output directory already exists: {output_dir}')
    metrics, samples, warnings, artifacts, verified_runs = [], [], [], [], []
    diagnostic = state.get('state') != 'completed'
    runs = []
    if system == 'nitch':
        runs = sorted((result_dir / 'nitch').glob('run-*/timing/timing-summary.json'))
    elif system == 'tgovern':
        runs = sorted((result_dir / 'sgx-sim/runs').glob('run-*/timing/l5-timing-summary.json'))
    module = collector(system) if runs else None
    for number, path in enumerate(runs, 1):
        relative = path.relative_to(result_dir).as_posix()
        artifacts.append(relative)
        data = load(path)
        run_root = path.parents[1]
        rows = []
        if system == 'nitch':
            verification = load(run_root / 'verification.json')
            verified_runs.append({'sample_run': number, 'verified': verification.get('verified', False),
                                  'edits': verification.get('edit_count', 0), 'rotations': verification.get('rotation_count', 0)})
            warnings.extend(data.get('alerts', []) + data.get('integrity_warnings', []))
            for row in data['stages']:
                rows.append((row, native_samples(module, data, row), False))
        else:
            progress = (run_root / 'e2e.log').read_text(encoding='utf-8')
            edits = len(re.findall(r'redaction [^\n]+ passed$', progress, re.MULTILINE))
            rotations = len(re.findall(r'epoch [0-9]+ DKRS ready:', progress))
            confirmations = len(data['samples'].get('execute_redaction_confirmation', []))
            verified_runs.append({'sample_run': number, 'verified': 'L5 passed:' in progress,
                                  'edits': min(edits, confirmations), 'rotations': rotations})
            for name, entries in data.get('diagnostics', {}).items():
                if entries:
                    warnings.append(f'run {number}: {name}={len(entries)}; inspect {relative}')
            for row in data['stages']:
                if not row.get('headline') and row['stage'] != 'ledger_update_total':
                    continue
                if row['stage'] in ('ledger_update', 'ledger_update_total') and row['stratum'] != 'all':
                    continue
                values = data['samples'].get(row['stage'], [])
                if row['stage'] in ('block_generation', 'block_validation'):
                    values = [v for v in values if int(v.get('txs') or 0) > 0]
                if row['stage'] == 'rotation_flow':
                    for bootstrap in (False, True):
                        selected = [v for v in values if (int(v.get('epoch', -1)) == 0) == bootstrap]
                        if not selected:
                            continue
                        stage = 'rotation_bootstrap' if bootstrap else 'rotation_flow'
                        normalized = module.stage_row(stage, selected, row['basis'], row.get('note', ''), {}, headline=True)
                        rows.append((normalized, selected, bootstrap))
                else:
                    rows.append((row, values, False))
        for row, values, bootstrap in rows:
            stage, basis = row['stage'], row['basis']
            item = dict.fromkeys(METRIC_FIELDS)
            item.update(system=system, run_id=manifest['run_id'], sample_run=number, stage=stage,
                        metric_definition=definition(system, stage, basis), stratum=str(row.get('stratum', 'all')),
                        availability='diagnostic_only' if diagnostic or bootstrap else 'available', unit='ms',
                        basis=basis, source=relative)
            mapping = {'raw_count': 'raw_count', 'used_count': 'used_count', 'excluded_count': 'excluded_count',
                       'raw_mean_ms': 'raw_mean_ms' if system == 'nitch' else 'raw_mean',
                       'filtered_mean_ms': 'filtered_mean_ms' if system == 'nitch' else 'filtered_mean',
                       'p50_ms': 'p50_ms' if system == 'nitch' else 'p50',
                       'p95_ms': 'p95_ms' if system == 'nitch' else 'p95',
                       'p99_ms': 'p99_ms' if system == 'nitch' else 'p99',
                       'max_ms': 'max_ms' if system == 'nitch' else 'max'}
            item.update({field: row.get(key) if row.get(key) != '' else None for field, key in mapping.items()})
            if len(values) != item['raw_count']:
                raise ValueError(f'{system}/{number}/{stage}: sample count {len(values)} != {item["raw_count"]}')
            metrics.append(item)
            for index, value in enumerate(values):
                sample = {field: item.get(field) for field in SAMPLE_FIELDS}
                sample.update(sample_id=str(value.get('sample_id') or f'{stage}-{index+1}'),
                              duration_ms=value['duration_ms'], epoch=value.get('epoch'),
                              request_id=value.get('join_key') or value.get('req_id'),
                              node_id=value.get('node_id') or value.get('source_file'),
                              status='diagnostic_only' if diagnostic or bootstrap else value.get('status', 'ok'))
                samples.append(sample)
        present = {row['stage'] for row in metrics if row['sample_run'] == number}
        for stage in CORE:
            if stage not in present:
                item = dict.fromkeys(METRIC_FIELDS)
                item.update(system=system, run_id=manifest['run_id'], sample_run=number, stage=stage,
                            metric_definition='unavailable', stratum='all', availability='not_collected', unit='ms', source=relative)
                if stage == 'rotation_flow' and not manifest.get('rotations'):
                    item['availability'] = 'not_applicable'
                metrics.append(item)
    if state.get('state') == 'completed' and mode != 'shamir':
        expected_runs = manifest['runs']
        if len(verified_runs) != expected_runs or any(not v['verified'] or
                v['edits'] < (max(1, manifest['rotations']) * manifest['edits'] if system == 'nitch'
                              else manifest['rotations'] * manifest['edits'] or 1) or
                v['rotations'] < manifest['rotations'] for v in verified_runs):
            raise ValueError('completed result is missing required verified runs, edits or rotations')
    if not runs and mode != 'shamir':
        warnings.append('No collected timing input is available; no samples were invented.')
        for stage in CORE:
            item = dict.fromkeys(METRIC_FIELDS)
            item.update(system=system, run_id=manifest['run_id'], sample_run=None, stage=stage,
                        metric_definition='unavailable', stratum='all', availability='not_collected', unit='ms')
            metrics.append(item)
    for number in range(1, len(runs) + 1):
        present = {row['stage'] for row in metrics if row['sample_run'] == number}
        for stage in ('tee_registration', 'execute_redaction_confirmation', 'ledger_update_e2e'):
            if stage not in present:
                item = dict.fromkeys(METRIC_FIELDS)
                item.update(system=system, run_id=manifest['run_id'], sample_run=number, stage=stage,
                            metric_definition='unavailable', stratum='all', unit='ms',
                            availability='not_applicable' if system == 'nitch' and stage != 'ledger_update_e2e' else 'not_collected')
                metrics.append(item)
    order = {stage: index for index, stage in enumerate(CORE)}
    metrics.sort(key=lambda row: (row['sample_run'] or 0, order.get(row['stage'], len(CORE)), row['stage'], row['stratum']))
    warnings.extend([
        'Same stage names do not imply identical measurement boundaries; inspect metric_definition and basis.',
        'Tgovern rotation_bootstrap is diagnostic epoch-zero data, not a requested steady rotation.',
    ])
    summary = {'schema_version': 1, 'system': system, 'run_id': manifest['run_id'],
               'execution_mode': 'sgx_sim' if system == 'tgovern' else 'native',
               'configuration': manifest, 'status': state, 'verification': {'runs': verified_runs,
               'verified_edits': sum(v['edits'] for v in verified_runs),
               'verified_rotations': sum(v['rotations'] for v in verified_runs)},
               'metrics': metrics, 'warnings': warnings,
               'artifacts': {'input_root': os.path.relpath(result_dir, output_dir).replace('\\', '/'),
                             'timing_sources': artifacts}}
    if not in_place:
        output_dir.mkdir(parents=True, exist_ok=False)
    (output_dir / 'summary.json').write_text(json.dumps(summary, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
    visible_metrics = [row for row in metrics if row['stage'] not in HIDDEN_STAGES]
    visible_samples = [dict(row, sample_index=index) for index, row in enumerate(
        (row for row in samples if row['stage'] not in HIDDEN_STAGES), 1)]
    for name, fields, rows in (('summary.csv', CSV_METRIC_FIELDS, visible_metrics),
                               ('samples.csv', CSV_SAMPLE_FIELDS, visible_samples)):
        with (output_dir / name).open('w', encoding='utf-8', newline='') as stream:
            writer = csv.DictWriter(stream, fieldnames=fields, extrasaction='ignore')
            writer.writeheader()
            writer.writerows(rows)
    if in_place:
        clean_detail_csvs(result_dir)
    lines = [f'# {system} experiment result', '', f'- Run: `{manifest["run_id"]}`',
             f'- State: `{state["state"]}`. Exit code: `{state.get("exit_code")}`.',
             f'- Verified edits: {summary["verification"]["verified_edits"]}. Verified rotations: {summary["verification"]["verified_rotations"]}.',
             '', '| Stage | Samples | Mean (ms) | P50 (ms) | P95 (ms) |', '|---|---:|---:|---:|---:|']
    lines.extend(f'| {m["stage"]} | {m["raw_count"]} | {m["raw_mean_ms"]} | {m["p50_ms"]} | {m["p95_ms"]} |'
                 for m in metrics if m['stage'] not in HIDDEN_STAGES and m['availability'] == 'available')
    (output_dir / 'report.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--result-dir', required=True, type=Path)
    parser.add_argument('--output-dir', required=True, type=Path)
    args = parser.parse_args()
    export_results(args.result_dir, args.output_dir)
    print(args.output_dir)


if __name__ == '__main__':
    main()
