import csv
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from result_export import export_results, clean_detail_csvs, FILES, CSV_METRIC_FIELDS, CSV_SAMPLE_FIELDS, HIDDEN_STAGES

ROOT = Path(__file__).resolve().parents[1]


class ResultExportTests(unittest.TestCase):
    def fixture(self, root, system='nitch', state='failed'):
        root.mkdir()
        (root / 'manifest.json').write_text(json.dumps({'experiment': system, 'run_id': 'fixture',
                                                      'runs': 1, 'rotations': 1, 'edits': 5}))
        (root / 'status.json').write_text(json.dumps({'state': state, 'exit_code': 1 if state == 'failed' else 0, 'error': 'failure' if state == 'failed' else None}))

    def test_failed_run_without_timing_has_no_invented_samples(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root / 'input')
            summary = export_results(root / 'input', root / 'output')
            self.assertEqual(summary['status']['state'], 'failed')
            self.assertEqual(summary['verification']['verified_edits'], 0)
            self.assertEqual(list(csv.DictReader((root / 'output/samples.csv').open())), [])
            self.assertEqual(set(FILES), {p.name for p in (root / 'output').iterdir()})

    def test_completed_run_without_evidence_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root / 'input', state='completed')
            with self.assertRaisesRegex(ValueError, 'missing required verified'):
                export_results(root / 'input', root / 'output')
            self.assertFalse((root / 'output').exists())

    def test_existing_output_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root / 'input')
            (root / 'output').mkdir()
            (root / 'output/sentinel').write_text('keep')
            with self.assertRaises(FileExistsError):
                export_results(root / 'input', root / 'output')
            self.assertEqual((root / 'output/sentinel').read_text(), 'keep')

    def test_detail_csvs_remove_excluded_stages_and_descriptions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'nitch/run-001/timing/timing-stages.csv'
            path.parent.mkdir(parents=True)
            path.write_text('stage,count,mean_ms,basis,note,event\n'
                            'edit_execution,5,12,entry -> broadcast,details,anchor\n'
                            'transaction_generation,5,2,details,details,anchor\n', encoding='utf-8')
            clean_detail_csvs(root)
            text = path.read_text(encoding='utf-8')
            self.assertEqual(text, 'stage,count,mean_ms\nedit_execution,5,12\n')

    def test_in_place_export_replaces_legacy_detail_reports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'run'
            self.fixture(root)
            detail = root / 'nitch/run-001/timing/timing-report.md'
            detail.parent.mkdir(parents=True)
            detail.write_text('Definition native_prepare_to_dkg_ready transaction_generation', encoding='utf-8')
            export_results(root, root, in_place=True)
            self.assertEqual(detail.read_text(encoding='utf-8'), (root / 'report.md').read_text(encoding='utf-8'))
            self.assertNotIn('Definition', detail.read_text(encoding='utf-8'))

    def test_shamir_does_not_invent_block_metrics(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root / 'input', system='shamir', state='completed')
            summary = export_results(root / 'input', root / 'output')
            self.assertEqual(summary['metrics'], [])
            self.assertEqual(summary['system'], 'shamir')

    def test_entry_export_failure_is_not_completed(self):
        import run_capsule
        with tempfile.TemporaryDirectory() as directory:
            env = {'CO_EXPERIMENT': 'shamir', 'CO_RESULTS_DIR': directory, 'CO_RUN_ID': 'export-failure'}
            with patch.dict('os.environ', env), patch('sys.argv', ['run_capsule.py']), \
                 patch.object(run_capsule, 'run_experiments'), \
                 patch.object(run_capsule, 'export_results', side_effect=ValueError('bad export')):
                self.assertEqual(run_capsule.main(), 1)
            status = json.loads((Path(directory) / 'export-failure/status.json').read_text())
            self.assertEqual(status['state'], 'failed')
            self.assertIn('bad export', status['error'])


class RealResultExportTests(unittest.TestCase):
    @unittest.skipUnless((ROOT.parent / 'capsule-results/tgovern-epoch200-rot1-edit5-r1/status.json').exists(), 'requires retained real results')
    def test_real_systems_share_schema_without_merging_epoch_zero(self):
        inputs = ['nitch-4-3-rot1-edit5-r1', 'tgovern-epoch200-rot1-edit5-r1']
        with tempfile.TemporaryDirectory() as directory:
            summaries = []
            for index, name in enumerate(inputs):
                output = Path(directory) / str(index)
                summary = export_results(ROOT.parent / 'capsule-results' / name, output)
                self.assertEqual(summary['verification']['verified_edits'], 5)
                self.assertEqual(summary['verification']['verified_rotations'], 1)
                with (output / 'summary.csv').open(encoding='utf-8') as stream:
                    self.assertEqual(tuple(csv.DictReader(stream).fieldnames), CSV_METRIC_FIELDS)
                with (output / 'samples.csv').open(encoding='utf-8') as stream:
                    reader = csv.DictReader(stream)
                    self.assertEqual(tuple(reader.fieldnames), CSV_SAMPLE_FIELDS)
                    samples = list(reader)
                for metric in summary['metrics']:
                    if metric['raw_count'] is not None and metric['stage'] not in HIDDEN_STAGES:
                        actual = [s for s in samples if s['stage'] == metric['stage']]
                        self.assertEqual(len(actual), metric['raw_count'])
                report = (output / 'report.md').read_text(encoding='utf-8')
                self.assertIn('| Stage | Samples | Mean (ms) | P50 (ms) | P95 (ms) |', report)
                for excluded in ('Definition', 'Measurement notes', 'metric_definition', 'basis',
                                 'transaction_generation', 'transaction_validation',
                                 'execute_redaction_confirmation', 'ledger_update_e2e',
                                 'ledger_update_total', 'rotation_bootstrap', '->', 'not_collected'):
                    self.assertNotIn(excluded, report)
                self.assertTrue(any(m['stage'] == 'transaction_generation' for m in summary['metrics']))
                for filename in ('summary.csv', 'samples.csv'):
                    text = (output / filename).read_text(encoding='utf-8')
                    for token in (*HIDDEN_STAGES, 'metric_definition', 'stratum', 'basis', '->'):
                        self.assertNotIn(token, text)
                summaries.append(summary)
            self.assertEqual(set(summaries[0]), set(summaries[1]))
            tgovern = summaries[1]
            rotations = [m for m in tgovern['metrics'] if m['stage'] == 'rotation_flow']
            self.assertEqual(rotations[0]['raw_count'], 1)
            self.assertEqual(rotations[0]['raw_mean_ms'], 2208.0)
            bootstrap = [m for m in tgovern['metrics'] if m['stage'] == 'rotation_bootstrap']
            self.assertEqual(bootstrap[0]['availability'], 'diagnostic_only')
            ledger = [m for m in tgovern['metrics'] if m['stage'] == 'ledger_update']
            self.assertEqual(len(ledger), 1)
            self.assertEqual(ledger[0]['raw_count'], 10)


if __name__ == '__main__':
    unittest.main()
