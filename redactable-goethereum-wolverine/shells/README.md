# 节点管理脚本使用说明

本目录包含用于管理 Ethereum 节点的脚本集合。

## 📁 脚本列表

### 节点管理脚本
- `run_background.sh` - 在后台启动单个节点
- `stop_background.sh` - 停止单个后台节点
- `status_background.sh` - 查看节点状态

### 批量操作脚本
- `run_all_background.sh` - 批量启动多个节点
- `stop_all_background.sh` - 批量停止多个节点

### DKG 管理脚本
- `init_committee.sh` - 初始化委员会配置（首次使用）
- `set_committee.sh` - 修改委员会成员
- `reset_dkg.sh` - 重置 DKG 状态
- `wait_dkg_ready.sh` - 轮询委员会 extracted 和全体节点 publicKeyReady

## 🚀 快速开始

### 1. 初始化节点（首次使用）

**重要**: 首次使用必须先运行项目根目录的 `init.sh` 来创建节点账户和数据目录。

```bash
# 回到项目根目录
cd ..

# 初始化10个节点
./init.sh 10
```

### 2. 初始化委员会配置

```bash
# 进入脚本目录
cd shells

# 5个委员会成员，总共10个节点
./init_committee.sh 5 10

# 或者 3个委员会成员，总共10个节点
./init_committee.sh 3 10
```

### 3. 启动所有节点

```bash
# 启动10个节点
./run_all_background.sh 10
```

### 3. 连接节点

```bash
# 回到项目根目录
cd ..

# 连接10个节点
./addPeer.sh 10
```

### 4. 启动 DKG

```bash
# 连接到任意委员会节点（节点1-5）
geth attach http://localhost:8545

# 在控制台中启动 DKG
> eth.startDKG()
```

等待所有节点可安全接收业务交易后再启动负载：

```bash
./wait_dkg_ready.sh 5 10
```

## 📝 详细使用说明

### 节点管理

#### 启动单个节点

```bash
./run_background.sh 1    # 启动节点1
./run_background.sh 5    # 启动节点5
```

#### 停止单个节点

```bash
./stop_background.sh 1   # 停止节点1
./stop_background.sh 5   # 停止节点5
```

#### 查看节点状态

```bash
./status_background.sh 1    # 查看节点1状态
./status_background.sh       # 查看所有节点状态
```

### 批量操作

#### 批量启动节点（三种模式）

**模式1：不传参数（启动1-5）**
```bash
./run_all_background.sh
```

**模式2：传一个数字（启动1到N）**
```bash
./run_all_background.sh 5   # 启动节点1-5
./run_all_background.sh 10  # 启动节点1-10
```

**模式3：传多个数字（启动指定节点）**
```bash
./run_all_background.sh 1 3 5      # 启动节点1、3、5
./run_all_background.sh 1 2 3 4 5   # 启动节点1-5
```

#### 批量停止节点

```bash
./stop_all_background.sh 10  # 停止节点1-10
./stop_all_background.sh 1 3 5  # 停止节点1、3、5
```

### 委员会管理

#### 初始化委员会配置（首次使用）

```bash
# 5个委员会成员，总共10个节点
./init_committee.sh 5 10
```

#### 修改委员会成员

**方法1：前N个节点为委员会**

```bash
# 前5个节点为委员会
./set_committee.sh 5

# 前3个节点为委员会
./set_committee.sh 3
```

**方法2：指定特定节点为委员会**

```bash
# 节点1、3、5为委员会
./set_committee.sh 3 1 3 5

# 节点1、2、7、9为委员会
./set_committee.sh 4 1 2 7 9
```

#### 查看委员会配置

```bash
# 回到项目根目录
cd ..

# 查看节点注册信息
cat node_registry.json | jq '.nodes[] | select(.is_committee == true)'
```

### DKG 管理

#### 重置 DKG 状态

```bash
# 重置所有节点的 DKG 状态
./reset_dkg.sh
```

## 🔄 完整操作流程

### 场景1：首次启动（5个委员会成员，10个节点）

```bash
# 1. 初始化节点账户和数据目录（项目根目录）
cd ..
./init.sh 10

# 2. 初始化委员会配置
cd shells
./init_committee.sh 5 10

# 3. 启动所有节点
./run_all_background.sh 10

# 3. 等待节点启动（约10秒）
sleep 10

# 4. 查看节点状态
./status_background.sh

# 5. 连接节点
cd ..
./addPeer.sh 10

# 6. 启动 DKG
geth attach http://localhost:8545
> eth.startDKG()
```

### 场景2：修改委员会配置

```bash
# 1. 停止所有节点
./stop_all_background.sh 10

# 2. 修改委员会（例如改为3个）
./set_committee.sh 3

# 3. 重置 DKG
./reset_dkg.sh

# 4. 重新启动节点
./run_all_background.sh 10

# 5. 连接节点
cd ..
./addPeer.sh 10

# 6. 启动 DKG
geth attach http://localhost:8545
> eth.startDKG()
```

### 场景3：重置 DKG（解决 DKG 错误）

```bash
# 1. 停止所有节点
./stop_all_background.sh 10

# 2. 重置 DKG
./reset_dkg.sh

# 3. 重新启动节点
./run_all_background.sh 10

# 4. 连接节点
cd ..
./addPeer.sh 10

# 5. 启动 DKG
geth attach http://localhost:8545
> eth.startDKG()
```

## 📊 日志查看

### 查看节点日志

```bash
# 回到项目根目录
cd ..

# 查看节点1的日志
tail -f nodelogs/node1.log

# 查看节点5的日志
tail -f nodelogs/node5.log
```

## ⚠️ 常见问题

### Q1: DKG 启动失败，提示 "no need to start DistKG"

**原因**：节点已经生成过 Deal，状态不一致。

**解决方案**：
```bash
./stop_all_background.sh 10
./reset_dkg.sh
./run_all_background.sh 10
cd ..
./addPeer.sh 10
```

### Q2: 提示 "not a committee member"

**原因**：该节点不在委员会中（index >= 5）。

**解决方案**：只在委员会节点（1-5）上调用 `eth.startDKG()`。

### Q3: 提示 "no peers connected"

**原因**：节点未连接到其他节点。

**解决方案**：
```bash
cd ..
./addPeer.sh 10
```

## 📁 目录结构

```
redactable-goethereum/
├── shells/              # 脚本目录
│   ├── run_background.sh
│   ├── stop_background.sh
│   ├── status_background.sh
│   ├── run_all_background.sh
│   ├── stop_all_background.sh
│   ├── init_committee.sh
│   ├── set_committee.sh
│   └── reset_dkg.sh
├── nodelogs/            # 日志目录
│   ├── node1.log
│   ├── node2.log
│   └── ...
├── node1/              # 节点数据目录
├── node2/
├── ...
└── node_registry.json   # 节点注册文件
```

## 🎯 推荐配置

| 场景 | 委员会规模 | 总节点数 | 命令 |
|------|-----------|---------|------|
| 测试环境 | 3 | 10 | `./init_committee.sh 3 10` |
| 小型网络 | 5 | 10 | `./init_committee.sh 5 10` |
| 中型网络 | 7 | 20 | `./init_committee.sh 7 20` |
| 大型网络 | 10 | 50 | `./init_committee.sh 10 50` |

## 💡 提示

1. **首次使用**: 必须先运行项目根目录的 `./init.sh <节点数>` 来初始化节点
2. 所有脚本都需要在 `shells` 目录下运行
3. 修改委员会配置后必须重启节点
4. DKG 只能在委员会节点上启动
5. 日志文件存储在 `nodelogs/` 目录
6. PID 文件存储在项目根目录
