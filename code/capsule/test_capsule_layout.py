import os
from pathlib import Path
import subprocess
import sys
import unittest

CODE = Path(__file__).resolve().parents[1]
REPOSITORY = CODE.parent


class CapsuleLayoutTests(unittest.TestCase):
    @unittest.skipUnless((REPOSITORY / 'environment/Dockerfile').exists(), 'requires repository build context')
    def test_dependency_snapshots_match_sources(self):
        result = subprocess.run([sys.executable, str(CODE / 'capsule/sync-build-inputs.py'), '--check'],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    @unittest.skipUnless((REPOSITORY / 'environment/Dockerfile').exists(), 'requires repository build context')
    def test_platform_copy_sources_exist_in_environment_context(self):
        environment = REPOSITORY / 'environment'
        dockerfile = (environment / 'Dockerfile').read_text()
        self.assertTrue(dockerfile.startswith('FROM registry.codeocean.com/codeocean/ubuntu:22.04.1\n'))
        self.assertNotIn('ARG BASE_IMAGE', dockerfile)
        self.assertNotIn('COPY environment/', dockerfile)
        for line in dockerfile.splitlines():
            if line.startswith('COPY '):
                for source in line.split()[1:-1]:
                    self.assertTrue((environment / source).exists(), source)

    def test_entry_calls_runtime_not_itself(self):
        entry = (CODE / 'run').read_text()
        self.assertIn('$ROOT/capsule/run_capsule.py', entry)
        self.assertNotIn('bash run', entry)
        self.assertNotIn('$ROOT/environment/', entry)

    def test_default_cache_is_built_into_image(self):
        env = {name: value for name, value in os.environ.items() if not name.startswith('CO_')}
        for mode, cache in [('sgx', 'go-mod-cache.tar.gz'), ('nitch', 'nitch-go-mod-cache.tar.gz')]:
            result = subprocess.run([sys.executable, str(CODE / 'capsule/run_capsule.py'), '--check'],
                                    env={**env, 'CO_EXPERIMENT': mode}, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('/opt/tgovern/cache/' + cache, result.stdout.replace('\\\\', '/'))


if __name__ == '__main__':
    unittest.main()
