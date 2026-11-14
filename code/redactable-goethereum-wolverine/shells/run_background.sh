#!/bin/bash
# 后台运行节点脚本
# 用法: ./run_background.sh <节点编号>

if [ $# -ne 1 ]; then
    echo "错误：请指定节点编号 (1, 2, 3...)"
    echo "用法: $0 <节点编号>"
    exit 1
fi

NODE_NUM=$1
NODE_NAME="node${NODE_NUM}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${SCRIPT_DIR}/nodelogs"
LOG_FILE="${LOG_DIR}/${NODE_NAME}.log"
PID_FILE="${SCRIPT_DIR}/${NODE_NAME}.pid"

# 创建日志目录
mkdir -p "${LOG_DIR}"

# 检查是否已经在运行
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if ps -p "$OLD_PID" > /dev/null 2>&1; then
        echo "⚠️  节点 ${NODE_NAME} 已在运行 (PID: $OLD_PID)"
        echo "如需重启，请先运行: ./stop_background.sh ${NODE_NUM}"
        exit 1
    else
        # PID 文件存在但进程不存在，清理旧文件
        rm -f "$PID_FILE"
    fi
fi

echo "🚀 在后台启动节点 ${NODE_NAME}..."
echo "📋 日志文件: ${LOG_FILE}"
echo "📋 PID 文件: ${PID_FILE}"

# ===== 配置参数 =====
NETWORK_ID=12345
BASE_PORT=30301
BASE_RPC_PORT=8545
PASSWORD="1"

PORT=$((BASE_PORT + NODE_NUM - 1))
RPC_PORT=$((BASE_RPC_PORT + NODE_NUM - 1))

# ===== 路径配置 =====
GETH_BIN="${SCRIPT_DIR}/build/bin/geth"
ETHKEY_BIN="${SCRIPT_DIR}/cmd/ethkey/ethkey"
NODE_DIR="${SCRIPT_DIR}/${NODE_NAME}"
KEY_DIR="${NODE_DIR}/keystore"

PRIVATE_KEY_FILE="${SCRIPT_DIR}/privatekey.txt"
SHARED_KEY_FILE="${SCRIPT_DIR}/all_privatekeys.json"

ACCOUNT_FILE=$(ls -t "${KEY_DIR}/"UTC--* 2>/dev/null | head -1)

if [ ! -f "$ACCOUNT_FILE" ]; then
    echo "❌ 错误：未找到账户文件在 ${KEY_DIR}"
    exit 1
fi

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
    echo "⚠️  警告：未找到 jq 命令，使用文本方式更新"
    sed -i "/\"$NODE_NAME\":/d" "$SHARED_KEY_FILE"
    sed -i "s/\"nodes\": {/\"nodes\": {\n    \"$NODE_NAME\": {\n      \"address\": \"$ADDRESS\",\n      \"privateKey\": \"$PRIVATE_KEY\"\n    },/" "$SHARED_KEY_FILE"
    sed -i 's/},}/}}/' "$SHARED_KEY_FILE"
    echo "✅ 共享密钥文件已更新"
fi

echo "🔒 提取私钥到文件..."
echo "${PRIVATE_KEY}" > "${PRIVATE_KEY_FILE}"
echo "✅ 私钥已保存至: ${PRIVATE_KEY_FILE}"

# ===== 后台启动节点 =====
echo "🚀 启动节点 ${NODE_NAME} (端口:${PORT}, RPC端口:${RPC_PORT})..."
echo "📌 控制台命令: geth attach http://localhost:${RPC_PORT}"

# 使用 nohup 在后台运行，不使用 console（直接作为服务运行）
nohup "${GETH_BIN}" \
    --datadir "${NODE_DIR}" \
    --networkid "${NETWORK_ID}" \
    --port "${PORT}" \
    --maxpeers 128 \
    --rpc \
    --rpcport "${RPC_PORT}" \
    --rpcaddr "localhost" \
    --rpccorsdomain "*" \
    --rpcapi "eth,net,web3,personal,admin,debug,miner" \
    --allow-insecure-unlock \
    --nodiscover \
    > "${LOG_FILE}" 2>&1 &

PID=$!
echo $PID > "$PID_FILE"

echo "✅ 节点 ${NODE_NAME} 已在后台启动"
echo "   PID: ${PID}"
echo "   日志: tail -f ${LOG_FILE}"
echo ""
echo "监控日志: tail -f ${LOG_FILE}"
echo "停止节点: ./stop_background.sh ${NODE_NUM}"