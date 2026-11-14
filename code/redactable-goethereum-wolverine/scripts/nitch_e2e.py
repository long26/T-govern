#!/usr/bin/env python3
"""Run isolated native NITCH nodes through DKG, on-chain election and redaction."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import sys
import time
import urllib.request

from eth_account import Account


ROOT = Path(__file__).resolve().parents[1]
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
COMMITTEE = ('node-1', 'node-2', 'node-3', 'node-4')
CANDIDATES = ('node-2', 'node-3', 'node-4', 'node-5')
QUORUM = 3
EPOCH_BLOCKS = 64


def write_json(path: Path, value) -> None:
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')


class InvariantViolation(RuntimeError):
    pass


def verify_block(before: dict, after: dict, index: int, expected: str) -> None:
    for name in ('hash', 'parentHash', 'transactionsRoot', 'number'):
        if before[name] != after[name]:
            raise InvariantViolation(f'edited block changed {name}')
    old_transactions, new_transactions = before['transactions'], after['transactions']
    if len(old_transactions) != len(new_transactions):
        raise InvariantViolation('edited block changed transaction count')
    for position, (old, new) in enumerate(zip(old_transactions, new_transactions)):
        if old['hash'] != new['hash']:
            raise InvariantViolation(f'transaction hash changed at index {position}')
        if position != index and old['input'] != new['input']:
            raise InvariantViolation('an untargeted payload changed')
    if new_transactions[index]['input'].lower() != expected.lower():
        raise InvariantViolation('edited transaction does not contain the requested payload')


@dataclass
class Node:
    index: int
    directory: Path
    address: str
    key: str
    process: subprocess.Popen | None = None

    @property
    def node_id(self) -> str:
        return f'node-{self.index}'

    def rpc(self, method: str, params=None):
        body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params or []}).encode()
        request = urllib.request.Request(f'http://127.0.0.1:{18540 + self.index}', body,
                                         {'Content-Type': 'application/json'})
        with OPENER.open(request, timeout=30) as response:
            payload = json.load(response)
        if payload.get('error'):
            raise RuntimeError(f'{self.node_id} {method}: {payload["error"]}')
        return payload.get('result')


class Experiment:
    def __init__(self, args):
        self.args = args
        self.nodes: list[Node] = []
        self.start = time.monotonic()
        self.output = args.output
        self.work = args.workdir
        self.evidence = {'verified': False, 'rotation_count': 0, 'edit_count': 0,
                         'node_count': 6, 'rotations': [], 'edits': []}
        self.manifest = {}

    def anchor(self, stage: str, event: str, **fields):
        values = {'stage': stage, 'event': event, 'run_id': self.args.run_id, **fields}
        stamp = datetime.now(timezone.utc).strftime('%m-%d|%H:%M:%S.%f')[:-3]
        message = ' '.join(f'{name}={shlex.quote(str(value))}' for name, value in values.items())
        line = f'[{stamp}] [+{int((time.monotonic() - self.start) * 1000)}ms] PERF_ANCHOR {message}'
        print(line, flush=True)
        with (self.output / 'e2e-progress.log').open('a', encoding='utf-8') as progress:
            progress.write(line + '\n')

    def wait(self, description, probe, timeout=600):
        deadline = time.monotonic() + timeout
        last_error = None
        while time.monotonic() < deadline:
            for node in self.nodes:
                if node.process is not None and node.process.poll() is not None:
                    raise RuntimeError(f'{node.node_id} exited with {node.process.returncode} during {description}')
            try:
                result = probe()
                if result:
                    return result
            except InvariantViolation:
                raise
            except (OSError, ValueError, RuntimeError) as error:
                last_error = str(error)
            time.sleep(0.5)
        raise TimeoutError(f'{description}: {last_error or "condition did not become true"}')

    def prepare(self):
        self.work.mkdir(parents=True, exist_ok=False)
        (self.work / 'chain').mkdir()
        (self.output / 'logs').mkdir()
        password = self.work / 'password.txt'
        password.write_text('capsule-fixture\n')
        for index in range(1, 7):
            account = Account.create()
            self.nodes.append(Node(index, self.work / 'chain' / f'node{index}', account.address,
                                   account.key.hex().removeprefix('0x')))
        registry = {'nodes': [{'node_id': node.node_id, 'index': node.index - 1,
                               'is_committee': node.node_id in COMMITTEE,
                               'timestamp': datetime.now(timezone.utc).isoformat()}
                              for node in self.nodes]}
        write_json(self.work / 'chain' / 'node_registry.json', registry)
        genesis = json.loads((ROOT / 'genesis.json').read_text())
        genesis['config']['chainId'] = 12345
        genesis['alloc'] = {node.address.removeprefix('0x').lower(): {'balance': str(10**24)} for node in self.nodes}
        write_json(self.work / 'genesis.json', genesis)
        for node in self.nodes:
            node.directory.mkdir()
            key_file = self.work / f'key-{node.index}.txt'
            key_file.write_text(node.key + '\n')
            for command in (
                [str(self.args.geth), '--datadir', str(node.directory), 'init', str(self.work / 'genesis.json')],
                [str(self.args.geth), '--datadir', str(node.directory), 'account', 'import',
                 '--password', str(password), str(key_file)],
            ):
                subprocess.run(command, cwd=ROOT, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
        self.manifest = {
            'schema_version': 1, 'timing_schema_version': 3, 'run_id': self.args.run_id,
            'profile': 'offline-functional-no-netem',
            'counts': {'total': 6, 'core': 5, 'committee': 4, 'light': 0, 'ledger_observer': 1},
            'threshold': {'required_shares': QUORUM, 'polynomial_degree': QUORUM - 1},
            'topology': {'mode': 'full_mesh', 'node_count': 6, 'diameter': 1, 'acceptance': {'status': 'pending'}},
            'relay_ttl': {'distkg': 8, 'reshare': 8, 'modify': 8},
            'verification_nodes': [node.node_id for node in self.nodes],
            'ledger_observer': {'index': 6, 'role': 'ledger_observer', 'headline_comparable': True},
            'ledger_update_e2e': {'polling_interval_ms': 500},
            'nodes': [{'node_id': node.node_id, 'index': node.index, 'verification': True,
                       'role': 'ledger_observer' if node.index == 6 else 'core'} for node in self.nodes],
            'election': {'epoch_blocks': EPOCH_BLOCKS, 'initial_epoch': 1, 'initial_committee': list(COMMITTEE),
                         'candidates': list(CANDIDATES), 'difficulty': 1, 'manual_initial_release': True},
            'requested': {'rotations': self.args.rotations, 'edits_per_epoch': self.args.edits},
        }
        write_json(self.output / 'run-manifest.json', self.manifest)

    def start_nodes(self):
        for node in self.nodes:
            env = dict({name: value for name, value in os.environ.items() if not name.startswith('NITCH_')},
                       NITCH_NODE_ID=node.node_id, NITCH_NODE_INDEX=str(node.index),
                       NITCH_NODE_ROLE='ledger_observer' if node.index == 6 else 'core',
                       NITCH_THRESHOLD=str(QUORUM), NITCH_ACTIVE_COMMITTEE_THRESHOLD=str(QUORUM - 1),
                       NITCH_ACTIVE_COMMITTEE_MEMBERS=','.join(COMMITTEE), NITCH_ACTIVE_COMMITTEE_EPOCH='1',
                       NITCH_ACTIVE_COMMITTEE_ACTIVATE_AT='0', NITCH_EPOCH_LENGTH=str(EPOCH_BLOCKS),
                       NITCH_POW_COMMITTEE_SIZE='4', NITCH_POW_THRESHOLD=str(QUORUM - 1),
                       NITCH_POW_DIFFICULTY='1', NITCH_POW_INITIAL_MANUAL_RELEASE='1',
                       NITCH_POW_WORKER_ENABLED='1' if node.node_id in CANDIDATES else '0',
                       NITCH_DISTKG_RELAY_TTL='8', NITCH_RESHARE_RELAY_TTL='8', NITCH_MODIFY_RELAY_TTL='8',
                       GOMAXPROCS='2')
            if node.node_id in CANDIDATES:
                env.update(NITCH_POW_CANDIDATE_ID=node.node_id, NITCH_POW_WORKER_KEY=node.key)
            command = [str(self.args.geth), '--datadir', str(node.directory), '--networkid', '12345',
                       '--port', str(30330 + node.index), '--rpc', '--rpcaddr', '127.0.0.1',
                       '--rpcport', str(18540 + node.index), '--rpcapi',
                       'eth,net,web3,personal,admin,debug,miner,txpool', '--ipcdisable',
                       '--nodiscover', '--nousb', '--maxpeers', '64', '--cache', '128',
                       '--ethash.cachedir', str(self.work / 'ethash-cache'),
                       '--unlock', node.address, '--password', str(self.work / 'password.txt'),
                       '--allow-insecure-unlock', '--miner.etherbase', node.address, '--verbosity', '4']
            with (self.output / 'logs' / f'geth-node-{node.index}.log').open('w') as log:
                node.process = subprocess.Popen(command, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                                stdout=log, stderr=subprocess.STDOUT)
        for node in self.nodes:
            self.wait(f'{node.node_id} RPC startup', lambda node=node: node.rpc('web3_clientVersion'), 120)
            self.wait(f'{node.node_id} account unlock', lambda node=node: node.rpc(
                'personal_unlockAccount', [node.address, 'capsule-fixture', 0]), 120)
        enodes = {}
        for node in self.nodes:
            enode = node.rpc('admin_nodeInfo')['enode']
            enodes[node.node_id] = enode.split('@')[0] + f'@127.0.0.1:{30330 + node.index}'
        for source in self.nodes:
            for target in self.nodes:
                if source is not target:
                    source.rpc('admin_addPeer', [enodes[target.node_id]])
        self.wait('full-mesh peer connections', lambda: all(len(node.rpc('admin_peers')) >= 5 for node in self.nodes), 180)
        self.manifest['topology']['acceptance']['status'] = 'ok'
        self.manifest['process_ids'] = {node.node_id: node.process.pid for node in self.nodes}
        write_json(self.output / 'run-manifest.json', self.manifest)

    def dkg(self):
        for node in self.nodes[:4]:
            if not node.rpc('eth_dkgStatus').get('extracted'):
                try:
                    node.rpc('eth_startDKG')
                except RuntimeError:
                    if not node.rpc('eth_dkgStatus').get('extracted'):
                        raise
        def ready():
            statuses = [node.rpc('eth_dkgStatus') for node in self.nodes]
            return statuses if all(item.get('publicKeyReady') for item in statuses) and all(item.get('extracted') for item in statuses[:4]) else None
        statuses = self.wait('DistKG extraction and public-key dissemination', ready)
        self.evidence['dkg'] = statuses
        self.anchor('system_init', 'phase_end', required_shares=QUORUM, status='ok')

    def transaction(self, tag):
        sender = self.nodes[-1]
        before = 'nitch-before-' + tag
        tx_hash = sender.rpc('eth_sendTransaction', [{'from': sender.address, 'to': self.nodes[0].address,
                                                      'value': '0x0', 'gas': hex(300000), 'gasPrice': hex(10**9),
                                                      'data': '0x' + before.encode().hex()}])
        receipt = self.wait('business transaction receipt', lambda: sender.rpc('eth_getTransactionReceipt', [tx_hash]), 600)
        if receipt.get('status') != '0x1':
            raise RuntimeError(f'business transaction reverted: {receipt}')
        number, index = receipt['blockNumber'], int(receipt['transactionIndex'], 16)
        def propagated():
            blocks = [node.rpc('eth_getBlockByNumber', [number, True]) for node in self.nodes]
            if any(not block or len(block['transactions']) <= index for block in blocks):
                return None
            if not all(block['transactions'][index]['hash'] == tx_hash for block in blocks):
                return None
            if len({block['hash'] for block in blocks}) != 1:
                return None
            return blocks
        snapshots = self.wait('business transaction on every node', propagated)
        return number, index, before, snapshots

    def rotate(self, round_number):
        baseline = self.nodes[0].rpc('eth_electionStatus').get('activeEpoch', 0)
        self.anchor('rotation_flow', 'round_start', round=round_number, join_key=f'rotation-{round_number}')
        if round_number == 1:
            for node in self.nodes:
                if node.node_id in CANDIDATES:
                    node.rpc('eth_releaseInitialEpoch')
        def converged():
            elections = [node.rpc('eth_electionStatus') for node in self.nodes]
            target = elections[0].get('activeEpoch', baseline)
            if target <= baseline or any(item.get('activeEpoch') != target or item.get('pending') for item in elections):
                return None
            members = elections[0].get('activeCommittee', [])
            if set(members) != set(CANDIDATES) or any(item.get('activeCommittee') != members for item in elections):
                return None
            statuses = {node.node_id: node.rpc('eth_reshareStatus') for node in self.nodes}
            if not all(statuses[name].get('extracted') and not statuses[name].get('pending') for name in members):
                return None
            return {'epoch': target, 'members': members, 'elections': elections, 'reshare': statuses}
        result = self.wait('PoW election and completed committee resharing', converged, 1800)
        self.evidence['rotations'].append(result)
        self.evidence['rotation_count'] += 1
        self.anchor('rotation_flow', 'round_end', round=round_number, required_shares=QUORUM, status='ok', join_key=f'rotation-{round_number}')

    def edit(self, tag, transaction):
        number, index, before, snapshots = transaction
        after = 'nitch-after-' + tag
        expected = '0x' + after.encode().hex()
        join_key = f'{int(number, 16)}-{index}'
        members = self.nodes[0].rpc('eth_electionStatus')['activeCommittee']
        proposer = next(node for node in self.nodes if node.node_id == members[0])
        request = {'BlockNum': str(int(number, 16)), 'Index': index, 'ModifyType': 1,
                   'DataBefore': before, 'DataAfter': after, 'RedactReason': 'capsule functional validation'}
        self.anchor('ledger_update_e2e', 'proposal_submitted', join_key=join_key, proposer=proposer.node_id)
        proposer.rpc('eth_proposeModification', [json.dumps(request)])
        self.wait('first payload application', lambda: any(
            node.rpc('eth_getBlockByNumber', [number, True])['transactions'][index]['input'].lower() == expected.lower()
            for node in self.nodes), 600)
        self.anchor('ledger_update_e2e', 'apply_observed', join_key=join_key)
        def verified():
            blocks = [node.rpc('eth_getBlockByNumber', [number, True]) for node in self.nodes]
            if any(not block or len(block['transactions']) <= index or
                   block['transactions'][index]['input'].lower() != expected.lower() for block in blocks):
                return None
            for original, updated in zip(snapshots, blocks):
                verify_block(original, updated, index, expected)
            return blocks
        blocks = self.wait('payload, transaction hash and block invariants on every node', verified, 600)
        result = {'join_key': join_key, 'proposer': proposer.node_id, 'before': before, 'after': after,
                  'block_hash': blocks[0]['hash'], 'transaction_hash': blocks[0]['transactions'][index]['hash'],
                  'verified_nodes': [node.node_id for node in self.nodes]}
        self.evidence['edits'].append(result)
        self.evidence['edit_count'] += 1
        self.anchor('ledger_update_e2e', 'verified_all', join_key=join_key, status='ok', verified_nodes=6)

    def run(self):
        self.anchor('system_init', 'phase_start')
        self.prepare()
        self.start_nodes()
        self.dkg()
        self.anchor('transaction_generation', 'tx_phase_start')
        self.nodes[-1].rpc('miner_start', [1])
        for round_number in range(1, max(1, self.args.rotations) + 1):
            target = self.transaction(f'{round_number}-1')
            if self.args.rotations:
                self.rotate(round_number)
            for edit_number in range(1, self.args.edits + 1):
                if edit_number > 1:
                    target = self.transaction(f'{round_number}-{edit_number}')
                self.edit(f'{round_number}-{edit_number}', target)
        self.anchor('transaction_generation', 'tx_phase_end')
        if any(node.process.poll() is not None or node.process.pid != self.manifest['process_ids'][node.node_id] for node in self.nodes):
            raise RuntimeError('node process changed or exited during the experiment')
        self.evidence['process_ids'] = self.manifest['process_ids']
        self.evidence['verified'] = True

    def cleanup(self):
        for node in self.nodes:
            if node.process is not None and node.process.poll() is None:
                node.process.terminate()
        for node in self.nodes:
            if node.process is not None:
                try:
                    node.process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    node.process.kill()
                    node.process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--geth', required=True, type=Path)
    parser.add_argument('--workdir', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--rotations', type=int, default=1)
    parser.add_argument('--edits', type=int, default=1)
    args = parser.parse_args()
    args.geth = args.geth.resolve()
    args.workdir = args.workdir.resolve()
    args.output = args.output.resolve()
    if args.rotations < 0 or args.edits < 1:
        parser.error('rotations must be nonnegative and edits must be positive')
    args.output.mkdir(parents=True, exist_ok=False)
    experiment = Experiment(args)
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt('experiment interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        experiment.run()
        return 0
    except (Exception, KeyboardInterrupt) as error:
        experiment.evidence['error'] = str(error)
        print(f'nitch E2E failed: {error}', file=sys.stderr, flush=True)
        if (args.output / 'e2e-progress.log').exists():
            experiment.anchor('ledger_update_e2e', 'verification_failed', status='error')
        return 1
    finally:
        experiment.cleanup()
        write_json(args.output / 'verification.json', experiment.evidence)


if __name__ == '__main__':
    sys.exit(main())
