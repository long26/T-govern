# T-govern（tee-connect）多机编排

`tgovern_multi_host.py` 复用原 L5 业务：TEE 注册 → DistKG/PKCH → baseline 编辑 → 合约自动 epoch 轮换 → DKRS → 轮换后编辑及内容/交易哈希验证。不以 PoW 选举替代原轮换。

- 普通机 A：node1（ON 矿工、合约部署）及 committee-mvp（IPC 连 node1）。
- TEE 机 B：TEE 节点 node2 起。轻节点默认在 A，`--light-remote` 放在 B。
- 远端 RPC 保持 loopback，经**一条 SSH 连接、多个 `-L`**转发到 A 的 `127.0.0.1:28550+i`。日志：`logs/tunnel-rpc.log`。等待 RPC/peer 时检查隧道退出，结束时回收。
- 启动配置一次解析并显式传给两端 geth 及嵌入式 L5。默认 `--epoch-length 128`、steady=120、`--blocks-per-registered-node 1`、`--est-seconds-per-block 15`，按当前 ~10s 出块难度设定（旧的 1–2s 链默认值已不适用）。显式传参或 `TEE_STEADY_EPOCH_LENGTH`/`STEADY_EPOCH_LENGTH` 环境变量仍可覆盖。node1 部署后等待非空 bytecode，注册前校验链上 epoch/active limit/threshold。
- DKRS 进度由编排器按运行向两端发 `dkrs_progress`：远端用 `grep -l` 统计本 epoch 的 start/extract 标记文件（只传文件名，不搬运日志），多轮轮询也不随日志体积增长。timing collector 用一次性过滤后的合并日志（只保留两个消费者读取的行：DKRS 标记 + `E2E_TIMING` 的 `miner_task_queued_for_sealing`/`insert_chain_total`/`stage=edit_execution`/`stage=ledger_update`/`stage=rotation_flow`），其余逐块/逐笔遥测（可达数 GB）不传输也不归档；两端原始日志原样保留。进度独立保存于每次运行的 `l5/e2e-progress.log`。
- 背景交易生成器在多机模式中关闭；目标编辑交易及原业务验证仍执行。

## 32 TEE 拓扑与批量参数

多机编排器和批量 runner 均默认 `--topology sparse --topology-radius 2 --topology-max-degree 9`。`scripts/topology.py` 用环邻居和二次幂捷径生成确定性边集，不再全枚举节点对；`--topology full` 可恢复全连接（该模式忽略稀疏度数上限）。

- `--tee-count 32 --light-nodes 0` 是 **33 个 geth 节点**（32 TEE + node1 普通矿工），默认 132 条无向边、264 次双向 addPeer，最大度 8，全图/TEE 子图直径均为 3。纯 32 节点图则是 144 条边、最大度 9；不要把这两个规模混用。
- addPeer 使用 `ThreadPoolExecutor(max_workers=32)`；只等待实际拓扑邻居，并校验 peer id。每次等待只读一份 `admin_peers`，超时为 `120 + 目标邻居数 × 15` 秒。
- maxpeers 至少覆盖 `3 × 实际最大度`，保留库解析出的更大值，以满足 geth DialRatio=3 的主动拨号预算；full 模式同样扩展预算。
- TEE 子图必须独立连通，不依赖普通节点中继 DKG/DKRS。TTL 下限取 `max(4, max(全图直径, TEE 子图直径) + 1)`，传入两端每个节点的 `--tee.distkg-relay-ttl` / `--tee.dkrs-relay-ttl` / `--txmodify.relay-ttl`，并同步到嵌入式 L5。继承的更大 TTL 保留，过低值提高到下限；这不是 nitch 的 `NITCH_*` 环境变量。TEE 显式启用 `--tee.dkrs-relay`，fanout 覆盖实际最大度。
- `configuration.json` 的 `effective.topology`、批量 `manifest.json` 的 `topology` 保存完整边集、邻居、直径及 TTL 下限。
- 已有成功/失败自动 stop 和单次 `--keep-running` 行为保留；批量失败继续执行按 run-id 的兜底 stop，清理失败即终止批次，不允许把保留节点的调试运行当成验收样本。

先预览 32 TEE 批量命令（不连接远端、不启动节点、不创建结果目录）：

```bash
python3 -B /opt/tgovern/scripts/run-tgovern-multi-samples.py \
  --dry-run --runs 5 --stop-on-failure --tag "sparse32-$(date -u +%Y%m%dT%H%M%SZ)" \
  --remote-ssh root@172.22.53.44 \
  --local-ip 172.22.53.43 --remote-ip 172.22.53.44 \
  --local-root /opt/tgovern --remote-root /opt/tgovern \
  --tee-count 32 --active-limit 32 --active-threshold 17 \
  --committee-count 4 --committee-threshold 3 \
  --topology sparse --topology-radius 2 --topology-max-degree 9 \
  --rotations 1 --baseline-edits 1 --edits-per-epoch 5 \
  --epoch-length 128 --sgx-mode HW --quote-mode mock --attestation noop \
  --gomaxprocs 4 --light-nodes 0 \
  --netem-delay 20 --netem-jitter 0 --netem-loss 0 \
  --netem-limit 10000 --netem-dev eth0
```

上面的 active threshold=17 是半数以上的示例；正式运行需按实验方案确认阈值。注册 bootstrap 的 epoch 安全下限随 TEE 数增加：32 TEE、`--blocks-per-registered-node 1` 时 floor=124，故 `--epoch-length 128` 不会被抬高，解析为 bootstrap=128、steady=120。若继承环境有覆盖，以每轮 `configuration.json` 为准。

**Epoch 预算依据**（慢链四轮实测）：注册+DKG+bootstrap+baseline 最坏用 101 块，轮换后五次编辑最坏用 93 块；加合约 20-block RotationPreparation 窗口和余量得到 128/120。难度的环境变量若改小（更快出块）应同步收紧，不要沿用慢链预算。

**正式跑之前两端需部署同版 `tgovern_multi_host.py` 和新增 `topology.py`**（主控另需更新 `run-tgovern-multi-samples.py`）。只有移除 `--dry-run` 才会启动实验。旧版 stop 入口升级时也需要同目录的 `topology.py`，不要只拷编排器文件。

## 原 16 TEE 五轮实验

```bash
python3 scripts/run-tgovern-multi-samples.py \
  --runs 5 --stop-on-failure --tag original16-<new-tag> \
  --remote-ssh root@172.22.53.44 \
  --local-ip 172.22.53.43 --remote-ip 172.22.53.44 \
  --local-root /opt/tgovern --remote-root /opt/tgovern \
  --tee-count 16 --active-limit 16 --active-threshold 9 \
  --committee-count 4 --committee-threshold 3 \
  --rotations 1 --baseline-edits 1 --edits-per-epoch 5 \
  --epoch-length 150 --sgx-mode HW --quote-mode mock --attestation noop \
  --gomaxprocs 4 --light-nodes 0 \
  --netem-delay 20 --netem-jitter 0 --netem-loss 0 \
  --netem-limit 10000 --netem-dev eth0
```

`baseline-edits` 是 baseline **区间数**，不是交易数。每轮样本期望为：

- rotation_flow = rotations = 1
- edit_execution = edits_per_epoch × (baseline_edits + rotations) = 10
- 五轮：5 次轮换、50 次编辑，并要求成功业务流程、对应确认及完整清理。

**rotation_flow 口径（2026-10 起）**：一次轮换的跨度 = 触发该 epoch 的**链上区块**在 **node1（矿工）的出块时刻** → **本簇 DKRS 完成**（最后一个 active TEE 应用新份额，即 `rotation_dkg_complete` 的最后一笔）。起点区块取**铸出该事件的交易所在区块**的出块时间，**不是**任何 TEE host 的区块导入时间（TEE host 经 P2P 收到后才 import，晚于出块）。两个起点口径都产出：

- `rotation_flow`（headline）= `ActiveSetRotated` 区块（epoch 真正切换、DKRS 从此开始）；
- `rotation_flow_from_prep`（次要）= 更早的 `TEERotationPreparationRequested` 区块（提案侧触发）。

两个 Go 锚点由 teehost 在事件派发处写入 `E2E_TIMING stage=rotation_flow`：`rotation_trigger_seen`（ActiveSetRotated，带 `trigger_block`/`trigger_tx`）与 `rotation_prep_requested`（RotationPreparationRequested，带 `nextEpoch`/`trigger_block`/`trigger_tx`）；终点 `rotation_dkg_complete`（带 `epoch`/`receiverIndex`/`parties`）。采集端用 node1 的 `miner_task_queued_for_sealing block=N` 行取该区块的毫秒级出块时刻。若某 epoch 无链上触发区块（旧日志），起点回退到该节点自身的 `dkrs_start`（`start_source=dkrs_start`）。

### 出块时间与 tx/块（PoW 可调）

链是 ethash PoW，**出块时间由 `genesis.json` 的难度决定**（当前 `0x4000`），tx/块 = tps × 出块时间。两个旋钮：

- `--genesis-difficulty`：难度覆盖（十六进制 `0x..` 或十进制；`0` 保持 genesis 原值）。实测单矿工出块均值（含 ~2s 时间戳节流底噪，故亚线性）：

  | 难度 | 倍数 | 出块均值 |
  |---|---|---|
  | `0x4000` | 1× | ~1.4s（节流受限） |
  | `0x40000` | 16× | 3.7s |
  | `0x80000` | 32× | 6.7s |
  | `0x100000` | 64× | 10.2s |
  | `0x200000` | 128× | 13.7s |

  目标 ~10s/块 → `0x100000`。
- `--on-tx-rate`：背景 ON 交易 tps（0 关闭）。`--tx-per-block`：每块背景交易数（`tx.py` 闭环，实时测量块间隔再调速率；**上限 200**）。tps=20 × 10.2s ≈ 204 → 取 `--tx-per-block 200`。

  `tx.py` 的闭环把块间隔上限默认钳在 `max(2/rate, 1s)`，在慢链（高难度）上会把 ~10s 的块当成 1s，于是永远按最大速率发、`--tx-per-block` 形同虚设。为此编排器自动传 `--block-interval-hint <est-seconds-per-block>`（`tx.py` 的新参数），把钳位上限抬到 `2×hint`，闭环才真正跟踪到目标密度。

`--est-seconds-per-block` 用于缩放 epoch/确认的墙钟超时；改大出块时间时必须同步调大，否则超时会早于链进展触发（默认 4，对应 `0x4000`）。

背景负载在**多机模式下由编排器自己拥有**（库内 `start_background_load` 被 stub）：为 node1 生成并注资一个 ON 负载账户、导入其 keystore 并解锁，在 L5 业务流程期间运行 `tx.py`，流程结束即回收。`--on-tx-rate 0` 时账户与进程都不创建，genesis alloc 与历史一致。

`edit_execution` 口径为 **编辑请求生命周期**：proposer（node1）把编辑请求发给委员会（脚本锚点 `committee_redact_request_submitted`）→ 第一个 TEE 完成编辑（聚合出新的 r' 并广播修改消息，`redaction_broadcast_modify_msg`）。旧日志若无该锚点则回退到 ON 节点的 `material_load_start`。其后的链上授权提交及其 receipt 属于独立的、受出块门控的流程，不计入该口径（提交/receipt 单独由 `execute_redaction_confirmation` 报告；样本仍保留提交 tx_hash 作为溯源）。

`system_init` 口径为**编排器自己留下的初始化锚点之和**：`committee_dkg_begin→committee_pubkey_registered`（委员会 DKG 聚合公钥）与 `tee_pkch_begin→pkch_ready`（PKCH 生成）。多机模式下 chain/datadir 准备、节点启动、合约部署由 `tgovern_multi_host.py` 拥有，sgx_l5 不会把对应锚点（`chain_init_begin`、`node_startup_*`、`contract_deployed`）写进该 run 的 progress 日志，因此这三段列为 `unavailable_segments`，不再让整段 `system_init` 变成空行。若必需段（委员会 DKG / PKCH）缺锚点，仍如实置空并记入 `missing_anchors`。

`tee_registration` 口径为**单个 TEE 的注册时延**：每个候选 TEE 在 `tee_registration_begin` 之后并行注册，各自发一条 `tee_registered duration_ns`；headline 就是这些逐 TEE 样本（16 TEE → 16 样本），不是 16 个 TEE 全部进入 active set 的墙钟跨度。后者作为次级口径 `tee_registration_cluster`（`tee_registration_begin→active_set_bootstrapped`，1 样本/轮）单独报告——它是调度长度而非单 TEE 延迟。子阶段仍按 `tee_registration_substages:<event>` 展平。

批量 runner 明确记录每次 run-id，只读取对应产物；嵌套注册子阶段展平为 `tee_registration_substages:<event>`。失败轮次不合并；仅有 summary 文件不算成功。`--resume` 当前明确拒绝（旧日志不可据此推断成功），新批次使用新 tag，已有目录不会被覆盖。支持 `--dry-run`。

## 产物及恢复

每次运行工作区：`<root>/.tmp/multi-host/<run-id>/`。不自动删除日志或链数据。

A 机产物：`<local-root>/out/<run-id>/`：

- `configuration.json`：请求/实际启动参数
- `run-status.json`：流程与清理结果，只有完整成功才 `success=true`
- `raw/e2e-progress.log`、`raw/logs/geth-node-*.log`
- `timing/l5-timing-summary.json` 等统计与 `collector.log`

批量目录 `results/tgovern-multi/<tag>/` 保存 manifest、逐轮结果/日志、失败清单、aggregate JSON/CSV（包含样本来源）。

SIGINT/SIGTERM 会转入按运行归属的清理。停止时校验 PID 启动时间及工作区，不使用全局 pkill。隧道也有运行记录；netem 使用独占设备记录，只撤销本次持有的 qdisc，拒绝覆盖自定义 root/filter。SIGKILL/掉电无法被捕获，控制程序消失后可显式恢复：

```bash
python3 scripts/tgovern_multi_host.py stop \
  --remote-ssh root@172.22.53.44 \
  --local-root /opt/tgovern --remote-root /opt/tgovern --run-id <id>
```

`--keep-running` 仅用于单次调试保留节点，不用于批量验收；`--no-collect` 跳过统计也不满足批量验收。

## 前提与测试

两端同版脚本/包；主控安装 eth-account；免密 SSH；HW 运行需实际 SGX 设备、AESM 和 HW 运行时。quote mock 不等同于 SGX SIM。CPU isolation 的单机配置不适用于此双机编排，会提前拒绝。

```bash
python3 -B scripts/test_topology.py -v
python3 -B scripts/test_tgovern_multi_host.py -v
python3 -B scripts/test_run_tgovern_multi_samples.py -v
python3 -B scripts/test_two_host_link.py -v
```

单元测试不能替代真实双机运行；最终验收还须检查五轮完整业务证据、原阈值及两端无实验残留。
