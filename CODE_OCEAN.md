# Run Tgovern on Code Ocean with SGX SIM

The capsule entry point is `run`. By default it runs the Shamir test and one SGX SIM experiment with four TEE candidates and a three-share threshold, a four-member signing committee and a three-share threshold, one rotation, and five edits after rotation. The experiment uses `geth-sgx --teehost` and the existing L5 runner. It uses mock quotes and noop attestation, not hardware remote attestation. Do not report SIM timings as SGX hardware measurements.

The installation recipe targets Ubuntu 22.04 amd64, Go 1.20.14, Python 3.10, and Intel SGX SDK 2.25.100.3. The old geth fork requires an older Go toolchain. Use this environment only for isolated research with disposable accounts, not a public blockchain node.

The pinned runtime image passes all 17 capsule regression tests. The cleaned default `bash run` entry also completed a local offline run with exit code zero. That run verified TEE registration, DKG, rotation, DKRS, and redaction with the transaction hash preserved. Its checked status is in `capsule-results/sgx-sim-clean-entry-r1/status.json`. This is local container evidence, not a claim that the capsule has already been run on the Code Ocean service.

## Install the capsule environment

1. Select an Ubuntu 22.04 amd64 base environment in Code Ocean.
2. Paste `environment/postInstall` into the Environment editor's Post-Install script.
3. Build the environment.

Post-Install runs as root and cannot access `/code` or `/data`. The script downloads only toolchains and Python dependencies. It stores the Go toolchain and the Python virtual environment under `/opt/tgovern`, and the SDK under `/opt/intel/sgxsdk`. It verifies the Go and Intel archive checksums before installation. Python installation uses pip 25.3 with resumed-download retries and keeps package hash verification enabled. The Dockerfile caches system installation separately from Python installation, so a Python download failure does not invalidate a completed system layer.

Do not install a kernel SGX driver or start AESM. The SDK supplies `libsgx_dcap_tvl.a`. The pinned Intel package archive supplies the DCAP development headers that this source still includes in SIM builds.

For local Linux Docker validation, build the same installation recipe from the repository root:

```bash
docker build --platform linux/amd64 -f environment/Dockerfile -t tgovern-codeocean .
```

If Docker Hub is unreachable, use Canonical's Ubuntu image from Amazon ECR:

```bash
docker build --platform linux/amd64 \
    --build-arg BASE_IMAGE=public.ecr.aws/ubuntu/ubuntu:22.04 \
    -f environment/Dockerfile -t tgovern-codeocean .
```

For a local HTTP proxy on port 7890, add build arguments. On Docker Desktop the build container reaches the host through `host.docker.internal`, not `127.0.0.1`:

```bash
docker build --platform linux/amd64 \
    --build-arg BASE_IMAGE=public.ecr.aws/ubuntu/ubuntu:22.04 \
    --build-arg HTTP_PROXY=http://host.docker.internal:7890 \
    --build-arg HTTPS_PROXY=http://host.docker.internal:7890 \
    -f environment/Dockerfile -t tgovern-codeocean .
```

These arguments apply to installation only. Do not add the proxy to the runtime image's `ENV` settings. The experiment command below keeps `--network none`.

## Prepare offline Go dependencies

Upload the repository contents into `/code`, including the `environment` folder. Keep these paths:

```text
/code/run
/code/environment/run_capsule.py
/code/committee/
/code/redactable-goethereum-tgovern/
```

In a Code Ocean Cloud Workstation using the built environment, prepare the dependency archive while network access is available:

```bash
cd /code
bash environment/prepare-go-cache.sh /data/go-mod-cache.tar.gz
```

Persist these two files as capsule data before starting the Reproducible Run:

```text
/data/go-mod-cache.tar.gz
/data/go-mod-cache.tar.gz.sha256
```

If `/data` is read-only in your workstation, generate the files in a writable directory, then upload or attach them through Code Ocean's data interface:

```bash
bash environment/prepare-go-cache.sh /tmp/go-mod-cache.tar.gz
```

Alternatively, prepare the files locally in the Docker image. Run these commands in a POSIX shell with Docker Linux containers available:

```bash
mkdir -p capsule-data capsule-results
docker run --rm --platform linux/amd64 \
    -v "$PWD:/code:ro" -v "$PWD/capsule-data:/data" \
    tgovern-codeocean bash environment/prepare-go-cache.sh
```

Prepare the archive from the exact source tree uploaded to the capsule. The archive records hashes of both Go modules' `go.mod` and `go.sum` files. Regenerate it after those files change. The preparation script refuses to replace an existing archive.

If the default Go proxy is unreachable, pass a reachable proxy only to the preparation container, for example `--env GOPROXY=https://goproxy.cn`. Keep Go checksum verification enabled during preparation. The reproducible run still sets `GOPROXY=off` and does not use the preparation proxy.

## Start a reproducible run

Set `/code/run` as the run file. If the upload drops executable permissions, set them in a workstation:

```bash
chmod +x /code/run
```

Inspect the selected configuration without installing, building, or starting nodes:

```bash
bash run --check
```

Click **Run**. The entry point restores the offline cache, stages source files in a fresh temporary directory, rebuilds MIRACL and the SIM enclave, checks the SIM dynamic-library linkage, and runs the cross-library test before the experiment. It then calls `run-sgx-l5-samples.py` with explicit sample counts. The original source tree and its build artifacts are not overwritten.

The runtime sets `GOPROXY=off` and `GOTOOLCHAIN=local`. It performs no package installation. The capsule must already contain all dependencies.

Before starting nodes, the entry point generates the epoch-0 Ethash DAG in an isolated temporary home directory. Repeated SGX samples reuse that dataset. This keeps first-time DAG generation outside the scripts' contract-deployment deadlines without replacing real PoW mining. The preparation command is logged in `run.log` and is outside the sampler's initialization timing.

For a local test with no network and no SGX device:

```bash
docker run --rm --platform linux/amd64 --network none \
    -v "$PWD:/code:ro" -v "$PWD/capsule-data:/data:ro" \
    -v "$PWD/capsule-results:/results" \
    tgovern-codeocean bash run
```

Increase the sample count through environment variables, without editing the entry point:

```bash
CO_RUNS=3 CO_ROTATIONS=1 CO_EDITS=2 bash run
```

The Code Ocean SGX profile uses a 200-block steady epoch after its first rotation, leaving more room for the default five confirmed edits. The bootstrap epoch is unchanged. Override the steady epoch with `TEE_STEADY_EPOCH_LENGTH`; the lower-priority alias `STEADY_EPOCH_LENGTH` is also accepted. The chosen value is recorded in `manifest.json` and passed to the SGX runner. These settings do not change native NITCH epochs.

Use `CO_ROTATIONS=0` for an initialization and baseline-edit run. The entry point requests one baseline edit in this case. `CO_TIMEOUT_SECONDS` sets a timeout for each build or experiment command. Its default is 3600 seconds, so larger sample batches need a larger value.

The entry point accepts `sgx`, `shamir`, and `nitch`. Its default remains `sgx`, which runs the Shamir test followed by the enclave-backed SGX SIM workflow. Native Wolverine runs only when explicitly selected with `CO_EXPERIMENT=nitch`. The retired software-only TEE demonstrations are not available.

To run only the Shamir test:

```bash
CO_EXPERIMENT=shamir bash run
```

The obsolete software-only E2E scripts and their duplicate copies have been removed. For a manual payload check on a running test chain, `committee/scripts/verify-edit.sh` remains available with explicit block and transaction arguments.

## Prepare and run native NITCH/Wolverine

The native adapter has completed a real six-node offline container run with exit code zero. It verified DKG, a signed on-chain election from epoch 1 to 2, resharing from node-1..node-4 to node-2..node-5, and payload redaction on all six nodes while preserving transaction and block identifiers. Collector completeness alerts were empty. Evidence is in `capsule-results/nitch-e2e-offline-r2/`, including `status.json` and `nitch/run-001/verification.json`. This validates the local Code Ocean filesystem layout, not execution on the Code Ocean service itself.

Use the same fixed image for local verification. NITCH builds `chameleon/nitchcore` with the host C++ toolchain and builds geth without SGX tags. The adapter rejects unresolved libraries or an SGX runtime in the native binary's linkage. It does not start the separate SGX committee service.

Prepare native dependencies through the host proxy:

```bash
docker run --rm --platform linux/amd64 \
    -e HTTP_PROXY=http://host.docker.internal:7890 \
    -e HTTPS_PROXY=http://host.docker.internal:7890 \
    -v "$PWD:/code:ro" -v "$PWD/capsule-data:/data" \
    tgovern-codeocean bash environment/prepare-go-cache.sh --experiment nitch
```

Upload these files as nitch capsule data:

```text
/data/nitch-go-mod-cache.tar.gz
/data/nitch-go-mod-cache.tar.gz.sha256
```

The original SGX archive is unchanged. Cache manifests contain only the selected experiment's Go module inputs. The adapter rejects using a native archive for SGX or an SGX archive for native execution.

Set `CO_EXPERIMENT=nitch` in the nitch capsule's runtime environment. Inspect the configuration before execution:

```bash
CO_EXPERIMENT=nitch bash run --check
CO_EXPERIMENT=nitch bash run
```

For local verification, keep the actual experiment offline:

```bash
docker run --rm --platform linux/amd64 --network none \
    -e CO_EXPERIMENT=nitch \
    -v "$PWD:/code:ro" -v "$PWD/capsule-data:/data:ro" \
    -v "$PWD/capsule-results:/results" \
    tgovern-codeocean bash run
```

The default native profile has six geth processes, four initial committee members, a three-share quorum, one rotation, and five payload edits after rotation. A spare candidate joins the next committee, and one observer stays outside both committees. Nodes are not restarted between DKG, election, resharing, and edits. `CO_RUNS`, `CO_ROTATIONS`, and `CO_EDITS` control repetition. With zero rotations, each run performs the requested baseline edits.

Native execution uses production DKG and election RPCs, signed on-chain PoW solutions, and the existing redaction protocol. It checks the actual payload on every node and rejects changes to transaction hashes, block hashes, parent hashes, transaction roots, or untargeted payloads. It does not replace the chain with the in-process test fixture.

Each successful native sample writes `/results/<run-id>/nitch/run-001/` with `logs/`, `e2e-progress.log`, `run-manifest.json`, `verification.json`, and `timing/`. The existing `collect_perf.py` produces `timing-summary.json`, `timing-summary.csv`, `timing-stages.csv`, and `timing-report.md`. The adapter rejects incomplete required stages and collector verification alerts. Repeated samples have separate directories and an `nitch/index.json` index.

This is a functional profile on loopback without netem. It needs no privileged container or `CAP_NET_ADMIN`. It does not reproduce the separate loaded-block experiment's 20ms network condition and must not be presented as that paper-performance configuration. The existing collector regression fixtures also have a pre-existing mismatch between total handling spans and the newer local-step headline. The adapter does not change the collector's statistical definitions to suppress that mismatch.

## Retrieve results and diagnose failures

Each invocation creates `/results/<run-id>`. It includes `manifest.json`, `status.json`, and `run.log`. SGX results include the existing sampler's `sgx-sim/aggregate.csv`, `aggregate.json`, per-run logs, and timing summaries. `sgx-linkage.txt` records the linked SGX runtime. `sgx-last-run` retains the latest node logs even when the sampler fails or is interrupted.

Both systems automatically write the same final result interface at the run root:

```text
summary.json
summary.csv
samples.csv
report.md
```

`summary.json` uses schema version 1 and includes `system`, `run_id`, `execution_mode`, `configuration`, `status`, `verification`, `metrics`, `warnings`, and `artifacts`. `summary.csv` has one row per sample run, stage, definition, and stratum. Statistics use explicit millisecond fields such as `raw_mean_ms`, `filtered_mean_ms`, `p50_ms`, and `p95_ms`. `samples.csv` contains actual underlying observations, not values invented from aggregate means. Counts and percentiles are not averaged across repeated runs.

Read `metric_definition` and `basis` before comparing systems. Initialization and editing have different start boundaries, and native resharing excludes the on-chain trigger portion included by the Tgovern rotation headline. Tgovern epoch-zero data appears separately as diagnostic `rotation_bootstrap`. Its `ledger_update` output uses the all-node stratum once, without repeating node subgroups. Missing metrics use `not_collected`; inapplicable metrics use `not_applicable`. Failed-run metrics are diagnostic only. A broadcast timing sample does not count as a verified edit.

Original files under `nitch/` and `sgx-sim/` remain available. Every metric's `source` is relative to the original result root, located by `artifacts.input_root` relative to the unified output directory.

Convert retained results without modifying the originals:

```bash
python environment/result_export.py \
    --result-dir capsule-results/nitch-4-3-rot1-edit5-r1 \
    --output-dir capsule-results/unified-nitch-five-edits
python environment/result_export.py \
    --result-dir capsule-results/tgovern-epoch200-rot1-edit5-r1 \
    --output-dir capsule-results/unified-tgovern-five-edits
```

The conversion refuses to overwrite an existing output directory. Automatic export failure also makes the capsule fail rather than report completed with missing final results.

Set `CO_RESULTS_DIR` to use another results root. Set `CO_RUN_ID` to choose the child directory name. The entry point refuses to overwrite an existing run directory. Default names include a timestamp and random suffix.

A successful run has `"state": "completed"` and exit code zero in `status.json`. A failed command returns a nonzero exit code and retains its log. Inspect `sgx-sim/runs/run-001/e2e.log` and the node logs when an E2E step fails. Check for enclave initialization, DistKG, verified edits, and rotation completion before treating the experiment as reproduced.

Temporary chain databases and account files are not archived by default. Logs come from disposable test chains and still need inspection before publication.

Run the portable regression suite with:

```bash
python3 -m unittest discover -s environment -p 'test_*.py' -v
```

The process-group integration test requires Linux. On Windows it is skipped. The existing `test_run_sgx_l5_samples.py` statistics tests currently expect `used_count`, which the sampler's existing `stage_stats` does not return. That pre-existing mismatch is separate from the capsule entry-point tests.

## Official documentation

- [Write and set a run script](https://docs.codeocean.com/osl-guide/user-manual/reproducible-code-execution/how-to-write-and-set-a-run-script).
- [Capsule code paths and result persistence](https://docs.codeocean.com/user-guide/v4.2.0/compute-capsule-basics/structure-of-a-compute-capsule/code).
- [Post-Install restrictions](https://docs.codeocean.com/user-guide/setting-up-the-environment/the-post-install-script).
- [Install dependencies before runtime](https://docs.codeocean.com/osl-guide/tutorials/tutorial-publishing-a-library-package-on-code-ocean).
