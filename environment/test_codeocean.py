import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / 'environment' / 'run_capsule.py'


class CapsuleEntryTests(unittest.TestCase):
    def invoke(self, *args, **values):
        env = {key: value for key, value in os.environ.items()
               if not key.startswith('CO_') and key not in ('SGX_MODE', 'TEE_QUOTE_MODE', 'TEE_ATTESTATION_PROVIDER',
                                                          'TEE_STEADY_EPOCH_LENGTH', 'STEADY_EPOCH_LENGTH')}
        env.update(values)
        return subprocess.run([sys.executable, str(RUNNER), *args], env=env,
                              capture_output=True, text=True)

    def test_default_is_small_sim_experiment(self):
        result = self.invoke('--check')
        self.assertEqual(result.returncode, 0, result.stderr)
        config = json.loads(result.stdout)
        self.assertEqual(config['experiment'], 'sgx')
        self.assertEqual(config['runs'], 1)
        self.assertEqual(config['rotations'], 1)
        self.assertEqual(config['edits'], 5)
        self.assertEqual(config['sgx_mode'], 'SIM')
        self.assertEqual(config['quote_mode'], 'mock')

    def test_sgx_default_steady_epoch_is_200_blocks(self):
        result = self.invoke('--check')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout).get('steady_epoch_length'), 200)

    def test_explicit_steady_epoch_override_and_alias_priority(self):
        for values, expected in (({'STEADY_EPOCH_LENGTH': '240'}, 240),
                                 ({'TEE_STEADY_EPOCH_LENGTH': '300', 'STEADY_EPOCH_LENGTH': '240'}, 300)):
            with self.subTest(values=values):
                result = self.invoke('--check', **values)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout).get('steady_epoch_length'), expected)
        result = self.invoke('--check', TEE_STEADY_EPOCH_LENGTH='0')
        self.assertNotEqual(result.returncode, 0)

    def test_nitch_has_its_own_cache_and_no_tee_claim(self):
        result = self.invoke('--check', CO_EXPERIMENT='nitch')
        self.assertEqual(result.returncode, 0, result.stderr)
        config = json.loads(result.stdout)
        self.assertEqual(config['experiment'], 'nitch')
        self.assertEqual(config['rotations'], 1)
        self.assertEqual(config['edits'], 5)
        self.assertEqual(Path(config['cache']).name, 'nitch-go-mod-cache.tar.gz')
        self.assertIsNone(config['sgx_mode'])
        self.assertIsNone(config['quote_mode'])
        self.assertIsNone(config['attestation_provider'])
        self.assertIsNone(config['steady_epoch_length'])

    def test_explicit_edit_and_rotation_counts_override_defaults(self):
        for mode in ('sgx', 'nitch'):
            with self.subTest(mode=mode):
                result = self.invoke('--check', CO_EXPERIMENT=mode, CO_ROTATIONS='2', CO_EDITS='3')
                self.assertEqual(result.returncode, 0, result.stderr)
                config = json.loads(result.stdout)
                self.assertEqual((config['rotations'], config['edits']), (2, 3))

    def test_nitch_does_not_depend_on_sgx_environment(self):
        result = self.invoke('--check', CO_EXPERIMENT='nitch', SGX_MODE='HW', TEE_QUOTE_MODE='real')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(json.loads(result.stdout)['sgx_mode'])

    def test_rejects_hardware_and_invalid_counts(self):
        for values in ({'SGX_MODE': 'HW'}, {'CO_RUNS': '0'}, {'CO_RUNS': '1.5'},
                       {'CO_ROTATIONS': '-1'}, {'CO_EDITS': '0'},
                       {'CO_EXPERIMENT': 'unknown'}, {'TEE_QUOTE_MODE': 'real'},
                       {'TEE_ATTESTATION_PROVIDER': 'azure'}, {'CO_RUN_ID': '../escape'}):
            with self.subTest(values=values):
                result = self.invoke('--check', **values)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('error:', result.stderr)

    def test_retired_modes_fail_without_creating_results(self):
        with tempfile.TemporaryDirectory() as directory:
            for mode in ('legacy', 'all'):
                with self.subTest(mode=mode):
                    result = self.invoke('--check', CO_EXPERIMENT=mode, CO_RESULTS_DIR=directory)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('CO_EXPERIMENT must be sgx or shamir', result.stderr)
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_paths_with_spaces_are_preserved(self):
        with tempfile.TemporaryDirectory(prefix='capsule space ') as directory:
            result = self.invoke('--check', CO_RESULTS_DIR=directory, CO_RUNS='2')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)['results_dir'], str(Path(directory).resolve()))
            self.assertEqual(json.loads(result.stdout)['runs'], 2)
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_existing_results_are_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / 'existing'
            target.mkdir()
            sentinel = target / 'sentinel.txt'
            sentinel.write_text('keep')
            result = self.invoke(CO_RESULTS_DIR=directory, CO_RUN_ID='existing')
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('already exists', result.stderr)
            self.assertEqual(sentinel.read_text(), 'keep')
            self.assertEqual(list(target.iterdir()), [sentinel])

    def test_failure_preserves_status_and_log(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.invoke(CO_RESULTS_DIR=directory, CO_RUN_ID='failure',
                                 CO_GO_CACHE=str(Path(directory) / 'missing.tar.gz'))
            self.assertNotEqual(result.returncode, 0)
            target = Path(directory) / 'failure'
            status = json.loads((target / 'status.json').read_text())
            self.assertEqual(status['state'], 'failed')
            self.assertNotEqual(status['exit_code'], 0)
            self.assertTrue(status['error'])
            self.assertIn(status['error'], (target / 'run.log').read_text())


class CapsuleArtifactTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('capsule_runner', RUNNER)
        cls.runner = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = cls.runner
        spec.loader.exec_module(cls.runner)

    def make_archive(self, path, members):
        with tarfile.open(path, 'w:gz') as archive:
            for name, content in members.items():
                info = tarfile.TarInfo(name)
                info.size = len(content)
                archive.addfile(info, io.BytesIO(content))
        path.with_name(path.name + '.sha256').write_text(hashlib.sha256(path.read_bytes()).hexdigest())

    def manifest(self):
        return json.dumps({'go_version': 'go1.20.14', 'target': 'linux/amd64',
                           'inputs': {f'{module}/{name}': self.runner.sha256(ROOT / module / name)
                                      for module in self.runner.MODULES for name in ('go.mod', 'go.sum')}}).encode()

    def test_matching_cache_restores_module_downloads(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            archive = directory / 'cache.tar.gz'
            self.make_archive(archive, {'manifest.json': self.manifest(), 'cache/download/module.zip': b'data'})
            self.runner.restore_cache(archive, directory / 'restored')
            self.assertEqual((directory / 'restored/cache/download/module.zip').read_bytes(), b'data')

    def test_nitch_cache_is_separate_from_sgx_cache(self):
        modules = self.runner.EXPERIMENTS['nitch'].modules
        manifest = json.dumps({'go_version': 'go1.20.14', 'target': 'linux/amd64',
                               'inputs': {f'{module}/{name}': self.runner.sha256(ROOT / module / name)
                                          for module in modules for name in ('go.mod', 'go.sum')}}).encode()
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            archive = directory / 'nitch.tar.gz'
            self.make_archive(archive, {'manifest.json': manifest, 'cache/download/nitch.zip': b'native'})
            self.runner.restore_cache(archive, directory / 'nitch', modules)
            self.assertEqual((directory / 'nitch/cache/download/nitch.zip').read_bytes(), b'native')
            with self.assertRaisesRegex(ValueError, 'incompatible'):
                self.runner.restore_cache(archive, directory / 'sgx')

    def test_nitch_staging_does_not_require_sgx_sources(self):
        modules = self.runner.EXPERIMENTS['nitch'].modules
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            project = directory / 'source/redactable-goethereum-wolverine/cmd/geth'
            project.mkdir(parents=True)
            (project / 'main.go').write_text('package main')
            native = project.parents[1]
            (native / 'node1').mkdir()
            (native / 'node1/keystore').write_text('existing private chain')
            (native / 'node').mkdir()
            (native / 'node/service.go').write_text('package node')
            with patch.object(self.runner, 'ROOT', directory / 'source'):
                self.runner.stage_sources(directory / 'staged', modules)
            self.assertEqual((directory / 'staged/redactable-goethereum-wolverine/cmd/geth/main.go').read_text(), 'package main')
            self.assertFalse((directory / 'staged/redactable-goethereum-wolverine/node1').exists())
            self.assertEqual((directory / 'staged/redactable-goethereum-wolverine/node/service.go').read_text(), 'package node')
            self.assertEqual((native / 'node1/keystore').read_text(), 'existing private chain')
            self.assertFalse((directory / 'staged/committee').exists())
            self.assertFalse((directory / 'staged/redactable-goethereum-tgovern').exists())

    def test_stale_cache_is_rejected_without_extracting(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            archive = directory / 'cache.tar.gz'
            self.make_archive(archive, {'manifest.json': b'{}'})
            with self.assertRaisesRegex(ValueError, 'stale'):
                self.runner.restore_cache(archive, directory / 'restored')
            self.assertFalse((directory / 'restored').exists())

    def test_cache_checksum_failure_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'cache.tar.gz'
            self.make_archive(archive, {'manifest.json': self.manifest()})
            archive.write_bytes(archive.read_bytes() + b'corrupt')
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                self.runner.restore_cache(archive, Path(directory) / 'restored')

    def test_cache_cannot_escape_destination(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            archive = directory / 'cache.tar.gz'
            self.make_archive(archive, {'manifest.json': self.manifest(), '../escape': b'bad'})
            with self.assertRaisesRegex(ValueError, 'unsafe'):
                self.runner.restore_cache(archive, directory / 'restored')
            self.assertFalse((directory / 'escape').exists())

    def test_empty_timing_output_cannot_pass_as_a_successful_experiment(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'aggregate.json').write_text(json.dumps({'samples': {}}))
            with patch.dict(os.environ, {'CO_ROTATIONS': '1', 'CO_RUNS': '1', 'CO_EDITS': '1'}):
                config = self.runner.Config.from_environment()
            with self.assertRaisesRegex(RuntimeError, 'incomplete system_init samples'):
                self.runner.validate_samples(output, config)

    def test_rotation_run_requires_rotation_samples(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'aggregate.json').write_text(json.dumps({'samples': {
                'system_init': [{'duration_ms': 1}], 'edit_execution': [{'duration_ms': 1}],
            }}))
            with patch.dict(os.environ, {'CO_ROTATIONS': '1', 'CO_RUNS': '1', 'CO_EDITS': '1'}):
                config = self.runner.Config.from_environment()
            with self.assertRaisesRegex(RuntimeError, 'incomplete rotation_flow samples'):
                self.runner.validate_samples(output, config)

    def test_baseline_only_run_accepts_init_and_edit_samples(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'aggregate.json').write_text(json.dumps({'samples': {
                'system_init': [{'duration_ms': 1}], 'edit_execution': [{'duration_ms': 1}],
            }}))
            with patch.dict(os.environ, {'CO_ROTATIONS': '0', 'CO_RUNS': '1', 'CO_EDITS': '1'}):
                config = self.runner.Config.from_environment()
            self.runner.validate_samples(output, config)

    def test_nitch_empty_timing_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            (run / 'timing').mkdir()
            (run / 'timing/timing-summary.json').write_text(json.dumps({'stages': [], 'alerts': []}))
            with self.assertRaisesRegex(RuntimeError, 'incomplete nitch timing stage'):
                self.runner.validate_nitch_timing(run, 1)

    def test_nitch_verification_alert_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            (run / 'timing').mkdir()
            stages = [{'stage': stage, 'count': 1, 'duration_ms': 1} for stage in (
                'system_init', 'transaction_generation', 'transaction_validation', 'block_generation',
                'block_validation', 'edit_execution', 'ledger_update', 'ledger_update_e2e', 'rotation_flow')]
            (run / 'timing/timing-summary.json').write_text(json.dumps({
                'stages': stages, 'alerts': ['[ledger verification failed] target mismatch']}))
            with self.assertRaisesRegex(RuntimeError, 'ledger verification failed'):
                self.runner.validate_nitch_timing(run, 1)

    def test_staging_keeps_geth_command_sources_and_skips_binary_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            source = directory / 'source'
            for module in self.runner.MODULES:
                (source / module / 'cmd/geth').mkdir(parents=True)
                (source / module / 'cmd/geth/main.go').write_text('package main')
                (source / module / 'internal/chain').mkdir(parents=True)
                (source / module / 'internal/chain/reader.go').write_text('package chain')
                (source / module / 'chain').mkdir()
                (source / module / 'chain/runtime.key').write_text('disposable runtime data')
                (source / module / 'artifact.so').write_bytes(b'prebuilt')
            with patch.object(self.runner, 'ROOT', source):
                self.runner.stage_sources(directory / 'staged')
            for module in self.runner.MODULES:
                self.assertEqual((directory / 'staged' / module / 'cmd/geth/main.go').read_text(), 'package main')
                chain_source = directory / 'staged' / module / 'internal/chain/reader.go'
                self.assertTrue(chain_source.exists(), 'internal/chain source package was excluded')
                self.assertEqual(chain_source.read_text(), 'package chain')
                self.assertFalse((directory / 'staged' / module / 'chain').exists())
                self.assertFalse((directory / 'staged' / module / 'artifact.so').exists())
                self.assertTrue((source / module / 'artifact.so').exists())


if __name__ == '__main__':
    unittest.main()
