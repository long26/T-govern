# Run the capsule on Code Ocean

## Import the native capsule layout

Import the repository without flattening its directories:

```text
environment/
    Dockerfile
    postInstall
    prepare-go-cache.sh
    experiment_config.py
    manifests/
code/
    run
    capsule/
    committee/
    redactable-goethereum-tgovern/
    redactable-goethereum-wolverine/
```

Code Ocean manages the root `environment` directory as Core Files. Do not move it into `code`. All experiment sources and runtime Python files are already under `code` and must stay there. `code/capsule` contains runtime helpers, not environment installation files.

## Build the environment

Use the supplied `environment/Dockerfile`. Its base image is the supported `registry.codeocean.com/codeocean/ubuntu:22.04.1`. Docker `COPY` paths are relative to the `environment` directory, so the correct source is `postInstall`, not `environment/postInstall`.

Build the environment before running experiments. Installation downloads the fixed Go 1.20.14 toolchain, SGX SDK 2.25.100.3, Python dependencies, and both experiments' Go module caches. The caches and their SHA256 files are installed inside the image:

```text
/opt/tgovern/cache/go-mod-cache.tar.gz
/opt/tgovern/cache/go-mod-cache.tar.gz.sha256
/opt/tgovern/cache/nitch-go-mod-cache.tar.gz
/opt/tgovern/cache/nitch-go-mod-cache.tar.gz.sha256
```

**No dependency files need to be uploaded to `/data`.** These are synthetic experiments and need no external input dataset. Installation requires network access, but the actual experiments do not install packages or download modules.

The environment build cannot read `/code`. Its `manifests` directory therefore contains synchronized copies of the three projects' `go.mod` and `go.sum`. After changing Go dependencies, run from the repository root:

```bash
python code/capsule/sync-build-inputs.py
python code/capsule/sync-build-inputs.py --check
```

Commit the updated manifests and rebuild the capsule environment. Tests reject stale build inputs. Runtime cache validation also checks the manifests against the actual source files and refuses incompatible caches rather than silently using the network.

The environment copies of `prepare-go-cache.sh` and `experiment_config.py` are generated build inputs. Edit their canonical versions under `code/capsule`, then run the same synchronization command.

`postInstall` supports `system`, `python`, and `cache` stages. The supplied Dockerfile runs each once. If the platform appends its own postInstall command to a manually edited Dockerfile, remove the duplicate invocation; do not run full installation twice.

## Set the real run entry

The master entry is **`code/run`**, whose runtime path is `/code/run`. Its command invokes `/code/capsule/run_capsule.py` directly. It does not call itself.

If Code Ocean's Set as File to Run operation generates a wrapper containing `bash run.sh`, make sure the wrapper is not itself named `run.sh`. Avoid that operation on the master entry if it overwrites it. The supplied master entry must contain:

```bash
#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export PATH="/opt/tgovern/venv/bin:/opt/tgovern/go/bin:$PATH"
exec "${PYTHON_BIN:-python3}" "$ROOT/capsule/run_capsule.py" "$@"
```

Choose one experiment through the runtime environment:

```text
CO_EXPERIMENT=sgx
```

or:

```text
CO_EXPERIMENT=nitch
```

Unset `CO_EXPERIMENT` defaults to `sgx`. `shamir` runs only the small Shamir test. The retired `legacy` and `all` modes remain unavailable.

For local CLI use from the repository root:

```bash
cd code
bash run --check
CO_EXPERIMENT=nitch bash run --check
```

The profiles default to one fresh run, one rotation, and five edits. Tgovern has four TEEs with a three-share threshold and a four-member signing committee with a three-share threshold. Its steady epoch is 200 blocks. The initial registration epoch is unchanged. NITCH has four initial committee members, a three-share threshold, a spare candidate, and a separate ledger observer across six geth processes.

Set `CO_RUNS`, `CO_ROTATIONS`, and `CO_EDITS` to override repetition. `TEE_STEADY_EPOCH_LENGTH` overrides Tgovern's steady epoch; `STEADY_EPOCH_LENGTH` is a lower-priority alias. Those settings do not change native NITCH epochs. Keep enough blocks for all transaction confirmations and edits before the next epoch boundary. Increasing only the wall-clock timeout does not prevent an epoch-mismatched authorization.

`CO_TIMEOUT_SECONDS` bounds each build or experiment command. Set it to `7200` for the initial full run, and configure a sufficient platform computation limit too. Real PoW and chain confirmations can take tens of minutes.

`CO_GO_CACHE` can explicitly select a different archive for debugging. It is not required for normal deployment. Existing manually uploaded archives still work when this override points to them and their manifests match the current source.

## Retrieve unified results

Each invocation creates a new `/results/<run-id>/` and refuses to overwrite an existing run. Both systems automatically write:

```text
manifest.json
status.json
run.log
summary.json
summary.csv
samples.csv
report.md
```

The final `status.json` must say `completed`, `exit_code` must be zero, and `error` must be null. Summary verification counts must match the requested edits and rotations. A failed run keeps diagnostic logs and must not be treated as a successful benchmark.

`summary.json` uses schema version 1. The same top-level fields describe both systems: `system`, `run_id`, `execution_mode`, `configuration`, `status`, `verification`, `metrics`, `warnings`, and `artifacts`. `summary.csv` uses the same columns, with explicit millisecond statistics. `samples.csv` contains actual observations, not artificial samples reconstructed from averages.

The same stage name does not guarantee an identical measurement boundary. Read `metric_definition`, `basis`, and `stratum`. Tgovern epoch-zero rotation data is labeled diagnostic `rotation_bootstrap`. Node-level ledger strata are not counted twice. Missing and inapplicable metrics remain distinct rather than becoming zero values. Broadcast completion alone does not prove a verified edit.

Detailed source results remain under `nitch/` or `sgx-sim/`. Sources in the unified export are relative to the original result root, found through `artifacts.input_root`. Temporary chain databases and test account keys are not archived by default. Inspect logs before publication.

For retained results, the converter does not change original files:

```bash
python code/capsule/result_export.py \
    --result-dir capsule-results/nitch-4-3-rot1-edit5-r1 \
    --output-dir capsule-results/unified-nitch-five-edits
```

## Local verification with the platform build context

Build from the repository root using only `environment` as the Docker context:

```bash
docker build --platform linux/amd64 \
    -f environment/Dockerfile -t tgovern-codeocean-native environment
```

If local downloads require an HTTP proxy on port 7890, add these build arguments:

```text
--build-arg HTTP_PROXY=http://host.docker.internal:7890
--build-arg HTTPS_PROXY=http://host.docker.internal:7890
```

Do not set that local Docker Desktop address in a Code Ocean cloud environment. Proxy settings are not baked into image `ENV` settings.

`tools/docker/Dockerfile.local` is a separate fallback for local validation if the platform registry is inaccessible. It uses the same environment context and installation/cache scripts but is not the platform Dockerfile:

```bash
docker build --platform linux/amd64 \
    -f tools/docker/Dockerfile.local -t tgovern-codeocean-local environment
```

Run with no data mount and no network:

```bash
mkdir -p capsule-results
docker run --rm --network none \
    -e CO_EXPERIMENT=nitch -e CO_TIMEOUT_SECONDS=7200 \
    -v "$PWD/code:/code:ro" -v "$PWD/capsule-results:/results" \
    tgovern-codeocean-native bash run
```

Change only `CO_EXPERIMENT=sgx` for Tgovern. No SGX hardware, AESM, privileged container, or netem is required. Tgovern uses the SDK's actual enclave simulation with mock quotes and noop attestation. NITCH builds native crypto without linking an SGX runtime. Neither result should be presented as hardware SGX performance or the separate loaded-block experiment's 20ms network configuration.

Run regressions from the repository root:

```bash
python -m unittest discover -s code/capsule -p 'test_*.py' -v
python -m unittest discover -s code/redactable-goethereum-wolverine/scripts -p test_nitch_e2e.py -v
```

Some existing sampler/collector tests predate the current statistic schemas. Their separate known fixture mismatches are not suppressed by changing the production collector. Successful local container runs validate paths and execution, not the Code Ocean web interface itself.

## Official documentation

- [Run scripts](https://docs.codeocean.com/osl-guide/user-manual/reproducible-code-execution/how-to-write-and-set-a-run-script).
- [Dockerfiles and base environments](https://docs.codeocean.com/user-guide/v2.13.0/how-to/dockerfiles-in-code-ocean).
- [Environment-only build inputs](https://docs.codeocean.com/osl-guide/tutorials/tutorial-writing-a-custom-dockerfile-to-build-a-capsule-from-a-manifest-file).
