import copy
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest

import collect_perf
from nitch_e2e import Experiment, InvariantViolation, verify_block


class BlockVerificationTests(unittest.TestCase):
    def setUp(self):
        self.before = {'hash': '0xblock', 'parentHash': '0xparent', 'transactionsRoot': '0xroot',
                       'number': '0x1', 'transactions': [
                           {'hash': '0xtarget', 'input': '0x6265666f7265'},
                           {'hash': '0xother', 'input': '0x6f74686572'},
                       ]}
        self.after = copy.deepcopy(self.before)
        self.after['transactions'][0]['input'] = '0x6166746572'

    def test_payload_edit_with_preserved_identifiers_is_accepted(self):
        verify_block(self.before, self.after, 0, '0x6166746572')

    def test_header_changes_cannot_be_treated_as_convergence_delay(self):
        for field in ('hash', 'parentHash', 'transactionsRoot', 'number'):
            with self.subTest(field=field):
                modified = copy.deepcopy(self.after)
                modified[field] = 'changed'
                with self.assertRaisesRegex(InvariantViolation, field):
                    verify_block(self.before, modified, 0, '0x6166746572')

    def test_changed_transaction_hash_is_rejected(self):
        self.after['transactions'][0]['hash'] = 'changed'
        with self.assertRaisesRegex(InvariantViolation, 'transaction hash'):
            verify_block(self.before, self.after, 0, '0x6166746572')

    def test_untargeted_payload_changes_are_rejected(self):
        self.after['transactions'][1]['input'] = 'changed'
        with self.assertRaisesRegex(InvariantViolation, 'untargeted'):
            verify_block(self.before, self.after, 0, '0x6166746572')

    def test_noop_is_not_a_successful_edit(self):
        with self.assertRaisesRegex(InvariantViolation, 'requested payload'):
            verify_block(self.before, self.before, 0, '0x6166746572')

    def test_missing_transaction_is_rejected(self):
        self.after['transactions'].pop()
        with self.assertRaisesRegex(InvariantViolation, 'transaction count'):
            verify_block(self.before, self.after, 0, '0x6166746572')

    def test_wait_propagates_invariant_failure_immediately(self):
        with tempfile.TemporaryDirectory() as directory:
            experiment = Experiment(SimpleNamespace(output=Path(directory), workdir=Path(directory) / 'work'))
            def failure():
                raise InvariantViolation('invalid ledger')
            with self.assertRaisesRegex(InvariantViolation, 'invalid ledger'):
                experiment.wait('verification', failure, timeout=1)


class TimingContractTests(unittest.TestCase):
    def test_ledger_headline_sums_local_steps_not_total_handling_span(self):
        base = {'source': 'geth', 'source_file': 'geth-node-6.log', 'node_id': 'node-6',
                'node_role': 'ledger_observer', 'node': 18546, 'join_key': '11-0',
                'status': 'ok', 'measurement_window': True, 'marker_schema': 3,
                'block': 11, 'tx_index': 0, 'timestamp_s': 100.0}
        records = [{**base, 'stage': 'ledger_update', 'event': event, 'duration_ms': value}
                   for event, value in [('lu_getblock', 1.0), ('lu_validate', 2.0), ('lu_update', 3.0)]]
        records.append({**base, 'stage': 'edit_execution', 'event': 'redaction_edit_applied', 'duration_ms': 8.0})
        headline = collect_perf.compute_ledger_update_samples(records)
        total = collect_perf.compute_ledger_update_total_samples(records)
        self.assertEqual([row['duration_ms'] for row in headline], [6.0])
        self.assertEqual([row['duration_ms'] for row in total], [8.0])
        self.assertEqual(collect_perf.compute_ledger_update_samples(records[1:]), [])


if __name__ == '__main__':
    unittest.main()
