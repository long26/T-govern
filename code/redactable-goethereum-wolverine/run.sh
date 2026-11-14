#!/bin/bash
# 节点启动
set -e  # 任何错误立即退出

# ===== 配置参数 =====
NETWORK_ID=12345
BASE_PORT=30301
BASE_RPC_PORT=8545
PASSWORD="1"  # 节点账户密码

# ===== 参数检查 =====
if [ $# -ne 1 ]; then
    echo "错误：请指定节点编号 (1, 2, 3...)"
    echo "用法: $0 <节点编号>"
    exit 1
fi

NODE_NUM=$1
NODE_NAME="node${NODE_NUM}"
PORT=$((BASE_PORT + NODE_NUM - 1))
RPC_PORT=$((BASE_RPC_PORT + NODE_NUM - 1))

# ===== 路径配置 =====
SCRIPT_DIR=$(pwd)
GETH_BIN="${SCRIPT_DIR}/build/bin/geth"
ETHKEY_BIN="${SCRIPT_DIR}/cmd/ethkey/ethkey"
NODE_DIR="${SCRIPT_DIR}/${NODE_NAME}"
KEY_DIR="${NODE_DIR}/keystore"

PRIVATE_KEY_FILE="${SCRIPT_DIR}/privatekey.txt"

ACCOUNT_FILE=$(ls -t "${KEY_DIR}/"UTC--* | head -1)

SHARED_KEY_FILE="${SCRIPT_DIR}/all_privatekeys.json"

echo "🔒 提取私钥和地址..."
OUTPUT=$(echo -n "${PASSWORD}" | "${ETHKEY_BIN}" inspect --private "${ACCOUNT_FILE}" \
    --passwordfile /dev/stdin 2>&1)

# 提取地址和私钥
ADDRESS=$(echo "$OUTPUT" | grep "Address" | awk '{print $2}')
PRIVATE_KEY=$(echo "$OUTPUT" | grep "Private key" | awk '{print $NF}')

if command -v jq &> /dev/null; then
    # 使用 jq 更新共享文件
    jq --arg node "$NODE_NAME" \
       --arg addr "$ADDRESS" \
       --arg pk "$PRIVATE_KEY" \
       '.nodes[$node] = {"address": $addr, "privateKey": $pk}' \
       "$SHARED_KEY_FILE" > tmp.json && mv tmp.json "$SHARED_KEY_FILE"

    echo "✅ 共享密钥文件已更新"
else
    # 如果没有 jq，使用文本处理（简单但不够健壮）
    echo "⚠️ 警告：未找到 jq 命令，使用文本方式更新（可能不准确）"

    # 删除旧条目（如果存在）
    sed -i "/\"$NODE_NAME\":/d" "$SHARED_KEY_FILE"

    # 添加新条目
    sed -i "s/\"nodes\": {/\"nodes\": {\n    \"$NODE_NAME\": {\n      \"address\": \"$ADDRESS\",\n      \"privateKey\": \"$PRIVATE_KEY\"\n    },/" "$SHARED_KEY_FILE"

    # 修复可能的格式问题
    sed -i 's/},}/}}/' "$SHARED_KEY_FILE"
    sed -i 's/},}/}}/' "$SHARED_KEY_FILE"

    echo "✅ 共享密钥文件已更新（文本方式）"
fi

echo "🔒 提取私钥..."
if echo -n "${PASSWORD}" | "${ETHKEY_BIN}" inspect --private "${ACCOUNT_FILE}" \
    --passwordfile /dev/stdin 2>&1 | grep "Private key" | awk '{print $NF}' > "${PRIVATE_KEY_FILE}"; then
    PRIVATE_KEY=$(cat "${PRIVATE_KEY_FILE}")
    echo "私钥: ${PRIVATE_KEY}"
    echo "✅ 私钥已保存至: ${PRIVATE_KEY_FILE}"
else
    echo "❌ 提取私钥失败!"
    # 这里可以添加错误处理逻辑
    exit 1
fi

# ===== 启动节点 =====
echo "🚀 启动节点 ${NODE_NAME} (端口:${PORT}, RPC端口:${RPC_PORT})..."
echo "📌 控制台命令: geth attach http://localhost:${RPC_PORT}"

"${GETH_BIN}" \
    --datadir "${NODE_DIR}" \
    --networkid "${NETWORK_ID}" \
    --port "${PORT}" \
    --rpc \
    --rpcport "${RPC_PORT}" \
    --rpcaddr "localhost" \
    --rpccorsdomain "*" \
    --rpcapi "eth,net,web3,personal,admin,debug,miner" \
    --allow-insecure-unlock \
    --maxpeers 128 \
    --nodiscover \
    console