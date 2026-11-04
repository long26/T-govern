#!/bin/bash
# create_account.sh - 仅创建节点账户并输出地址

if [ $# -ne 1 ]; then
    echo "用法: $0 <节点编号>"
    exit 1
fi

# ===== 配置参数 =====
PASSWORD="1"  # 节点账户密码
NODE_NUM=$1
NODE_NAME="node${NODE_NUM}"
SCRIPT_DIR=$(pwd)
NODE_DIR="${SCRIPT_DIR}/${NODE_NAME}"
KEY_DIR="${NODE_DIR}/keystore"
GETH_BIN="${SCRIPT_DIR}/build/bin/geth"

if [ -d "${NODE_DIR}" ]; then
    rm -rf "${NODE_DIR}" #删除已有节点
fi
# ===== 创建账户 =====
mkdir -p "${NODE_DIR}"
echo -e "${PASSWORD}\n${PASSWORD}\n" | "${GETH_BIN}" --datadir "${NODE_DIR}" account new >/dev/null

# ===== 输出账户地址 =====
ACCOUNT_FILE=$(ls -t "${KEY_DIR}/"UTC--* | head -1)
KEY_ADDR=$(jq -r '.address' "${ACCOUNT_FILE}")
echo "${KEY_ADDR}"  # 标准输出账户地址