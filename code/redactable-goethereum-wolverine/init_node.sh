#!/bin/bash
# init_node.sh - 使用预更新的创世文件初始化节点

if [ $# -ne 1 ]; then
    echo "用法: $0 <节点编号>"
    exit 1
fi

# ===== 配置参数 =====
NODE_NUM=$1
NODE_NAME="node${NODE_NUM}"
SCRIPT_DIR=$(pwd)
NODE_DIR="${SCRIPT_DIR}/${NODE_NAME}"
GENESIS_FILE="${SCRIPT_DIR}/genesis.json"
GETH_BIN="${SCRIPT_DIR}/build/bin/geth"


# ===== 初始化节点 =====
echo "⚙️  初始化节点 ${NODE_NAME}..."
"${GETH_BIN}" --datadir "${NODE_DIR}" init "${GENESIS_FILE}" >/dev/null
echo "✅ 节点初始化完成"


