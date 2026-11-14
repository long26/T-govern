import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

import run_capsule


ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(sys.platform.startswith('linux'), 'requires Linux process groups')
class SamplingLifecycleTests(unittest.TestCase):
    def test_command_failure_keeps_original_exit_code_and_output(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            with (directory / 'command.log').open('w') as log:
                with self.assertRaises(subprocess.CalledProcessError) as error:
                    run_capsule.execute([sys.executable, '-c', 'print("failure detail"); raise SystemExit(27)'],
                                        directory, dict(os.environ), log, 10)
            self.assertEqual(error.exception.returncode, 27)
            self.assertIn('failure detail', (directory / 'command.log').read_text())

    def test_command_timeout_reaps_child(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            pid_file = directory / 'pid'
            code = f'import os, pathlib, time; pathlib.Path({str(pid_file)!r}).write_text(str(os.getpid())); time.sleep(60)'
            with (directory / 'command.log').open('w') as log:
                with self.assertRaises(subprocess.TimeoutExpired):
                    run_capsule.execute([sys.executable, '-c', code], directory, dict(os.environ), log, 1)
            self.assertTrue(pid_file.exists())
            with self.assertRaises(ProcessLookupError):
                os.kill(int(pid_file.read_text()), 0)

    def test_sigterm_archives_failure_and_reaps_e2e(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            workspace = directory / 'workspace'
            child = directory / 'e2e.py'
            child.write_text(
                'import os, pathlib, time\n'
                f'work = pathlib.Path({str(workspace)!r})\n'
                '(work / "logs").mkdir(parents=True)\n'
                '(work / "logs/probe.log").write_text("diagnostic retained")\n'
                '(work / "pid").write_text(str(os.getpid()))\n'
                'time.sleep(60)\n')
            scripts = ROOT / 'redactable-goethereum-tgovern/scripts'
            wrapper = directory / 'sample.py'
            wrapper.write_text(
                'import importlib.util, pathlib, sys\n'
                f'sys.path.insert(0, {str(scripts)!r})\n'
                f'spec = importlib.util.spec_from_file_location("samples", {str(scripts / "run-sgx-l5-samples.py")!r})\n'
                'module = importlib.util.module_from_spec(spec)\n'
                'spec.loader.exec_module(module)\n'
                f'module.WORK = pathlib.Path({str(workspace)!r})\n'
                f'module.E2E = pathlib.Path({str(child)!r})\n'
                'sys.exit(module.main())\n')
            command = [sys.executable, str(wrapper), '--runs', '1', '--stop-on-failure',
                       '--output-root', str(directory / 'results'), '--tag', 'signal']
            env = {key: value for key, value in os.environ.items()
                   if key not in ('TEE_ROTATIONS', 'TEE_EDITS_PER_EPOCH', 'TEE_BASELINE_EDITS')}
            with (directory / 'parent.log').open('w') as log:
                process = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + 10
                    while not (workspace / 'pid').exists() and time.monotonic() < deadline:
                        self.assertIsNone(process.poll(), (directory / 'parent.log').read_text())
                        time.sleep(0.05)
                    self.assertTrue((workspace / 'pid').exists())
                    child_pid = int((workspace / 'pid').read_text())
                    process.send_signal(signal.SIGTERM)
                    self.assertEqual(process.wait(timeout=20), 1)
                    output = directory / 'results/signal'
                    self.assertEqual((output / 'runs/run-001/raw/logs/probe.log').read_text(), 'diagnostic retained')
                    failures = json.loads((output / 'failures.json').read_text())
                    self.assertIn('interrupted by signal', failures[0]['runner_error'])
                    with self.assertRaises(ProcessLookupError):
                        os.kill(child_pid, 0)
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.wait()


if __name__ == '__main__':
    unittest.main()
