# Tgovern

Built on a fork of go-ethereum v1.9.11.

```
Tgovern/
├── redactable-goethereum-tgovern/     # Tgovern
├── redactable-goethereum-wolverine/   # Wolverine on Ethereum
└── committee/                         # Threshold-signature committee process
```

## Directory layout

### redactable-goethereum-tgovern (Tgovern)

TEE-based redaction: DKG, DKRS reshares and edit computation execute inside an SGX enclave; an external `committee-mvp` process contributes threshold signatures.

| Path | Role |
| --- | --- |
| `sgx/` | Enclave: `enclave/Enclave.edl` (ECALL boundary), `enclave/*.cpp` (trusted code), `app/tee.cpp` (untrusted host wrapper), `lib/miracl` (BLS12-381), `sgx_module.go` (Go/CGO bridge) |
| `teehost/` | Host-side event watcher and protocol message handling |
| `contractService/` | `PermissionManagerV3` interaction: TEE registration, redaction authorization, PK-CH cache |
| `contracts/`, `solCode/` | Contract bindings and Solidity source |
| `eth/` | geth protocol layer: `validator.go` (block/tx edit validation), `protocol_manager_redact.go`, `tx_modify_protocol.go`, `api.go` (RPC) |
| `chameleon/` | Chameleon hash |
| `committee-mvp/` | Separate module: committee threshold-signature service |

### redactable-goethereum-wolverine (Wolverine)

Pure-software Wolverine: Distributed Key Generation, DKRS resharing, PartialAdapt/Combine edits, and PoW committee rotation — no SGX.

| Path | Role |
| --- | --- |
| `eth/key_manager.go` | Key material, DistKG state, node registry |
| `eth/distkg_protocol.go`, `reshare_protocol.go`, `reshare_state.go` | DKG and reshare state machines |
| `eth/election*.go` | PoW committee election and phase coordination |
| `eth/validator.go`, `protocol_manager_redact.go`, `modify_protocol.go`, `redaction_policy.go` | Edit validation and propagation |
| `eth/rotation_adapt.go` | Key adaptation at committee rotation |
| `chameleon/` | Chameleon hash; `chameleon/nitchcore` holds the core algorithm |
| `cmd/geth/main.go` | Node bootstrap, `NITCH_*` environment handling |

## Dependencies

- Go 1.18+ (upstream `go.mod` declares 1.13)
- SGX: Intel SGX SDK ; HW mode additionally needs DCAP 
- `jq`, `curl` for the scripts

## Build

```bash
cd redactable-goethereum-tgovern && make geth-sgx SGX_MODE=HW   # SGX_MODE=SIM without hardware
cd ../committee && go build -o bin/committee-mvp ./cmd/committee-mvp

cd ../redactable-goethereum-wolverine && make geth
```

## Run a single node

### Tgovern

```bash
$TGV_ROOT/bin/geth-sgx --datadir $TGV_ROOT/chain/node1 \
  --networkid 12345 --port 30301 \
  --rpc --rpcport 8545 --rpcaddr 127.0.0.1 \
  --rpcapi "eth,net,web3,personal,admin,debug,miner,txpool" \
  --unlock <address> --password password.txt --allow-insecure-unlock \
  --nodiscover --teehost --mine --minerthreads 1
```

Then start the committee process:

```bash
$TGV_ROOT/bin/committee-mvp node --config $TGV_ROOT/work/committee/node-1.json
# control RPC: http://127.0.0.1:4401/rpc
```

Minimal `node-1.json` (single seat):

```json
{
  "node_id": "node-1",
  "listen_addr": "127.0.0.1:3401",
  "static_nodes": ["node-1"],
  "static_node_addrs": {"node-1": "127.0.0.1:3401"},
  "control_addr": "127.0.0.1:4401",
  "chain_rpc_mode": "ipc",
  "geth_ipc_path": "/path/to/chain/node1/geth.ipc",
  "vrf_contract_address": "<PermissionManagerV3 address>",
  "vrf_submitter_address": "<node1 address>",
  "committee_size": 1,
  "threshold": 1,
  "coordinator_id": "node-1",
  "attestation_provider": "noop"
}
```

Check:

```bash
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"committee_getPublicKey","params":{}}' \
  http://127.0.0.1:4401/rpc
```

### Wolverine

```bash
GETH=./build/bin/geth
$GETH --datadir chain/node1 account new
$GETH --datadir chain/node1 init genesis.json
# write chain/node_registry.json marking node1 as is_committee

NITCH_NODE_INDEX=1 NITCH_NODE_ROLE=core NITCH_THRESHOLD=1 \
NITCH_DISTKG_RELAY_TTL=8 NITCH_RESHARE_RELAY_TTL=8 NITCH_MODIFY_RELAY_TTL=8 \
$GETH --datadir chain/node1 --networkid 12345 --port 30301 --maxpeers 128 \
  --syncmode full --ethash.cachedir cache/ethash --cache 128 \
  --rpc --rpcport 8545 --rpcaddr 127.0.0.1 \
  --rpcapi "eth,net,web3,personal,admin,debug,miner" \
  --allow-insecure-unlock --nodiscover --verbosity 4
```

## Connect nodes

```bash
./addPeer.sh 3
```

## Experiments

| System | Entry point |
| --- | --- |
| Tgovern | `tgovern/scripts/sgx_l5_sim_e2e.py` (end-to-end), `run-sgx-l5-samples.py` (multi-round sampling), `collect-l5-timing.py` (timing aggregation) |
| Wolverine | `nitch/scripts/run_perf.sh` (orchestrator: DKG → rotation/edit loop → archive), `collect_perf.py` (results) |

```bash
# Tgovern
python3 scripts/sgx_l5_sim_e2e.py --nodes 9 --committee 8

# Wolverine
scripts/run_perf.sh --committee 4 --nodes 10 --rate 50 --edits 5 --rotations 3 --tag R1
```
