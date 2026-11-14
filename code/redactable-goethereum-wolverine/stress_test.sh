#!/bin/bash
# stress_test.sh - 64节点压力测试：2节点挖矿 + 3000笔交易
# 交易均匀分配到 node3~node64（62个节点），每节点约48笔

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
GETH_BIN="${SCRIPT_DIR}/build/bin/geth"
PASSWORD="1"
BASE_RPC_PORT=8545
TX_COUNT=3000
MINER_COUNT=2
TOTAL_NODES=64

echo "=========================================="
echo "64节点压力测试"
echo "  挖矿节点: node1, node2"
echo "  交易节点: node3 ~ node64 (共62个)"
echo "  交易总数: ${TX_COUNT}"
echo "=========================================="

# 1. 解锁挖矿节点（node1, node2）并启动挖矿
echo ""
echo "=== 启动挖矿 ==="
for i in $(seq 1 $MINER_COUNT); do
    RPC_PORT=$((BASE_RPC_PORT + i - 1))
    ACCOUNT_FILE=$(ls -t "${SCRIPT_DIR}/node${i}/keystore/UTC--"* | head -1)
    ACCOUNT_ADDR=$(jq -r '.address' "$ACCOUNT_FILE")

    # 解锁账户（60分钟）
    echo "  解锁 node${i} 账户 ${ACCOUNT_ADDR} ..."
    curl -s -X POST -H "Content-Type: application/json" \
        --data "{\"jsonrpc\":\"2.0\",\"method\":\"personal_unlockAccount\",\"params\":[\"0x${ACCOUNT_ADDR}\",\"${PASSWORD}\",3600],\"id\":1}" \
        http://localhost:${RPC_PORT} | python3 -c "import sys,json; r=json.load(sys.stdin); print('    结果:', r.get('result','FAIL'))"

    # 启动挖矿
    echo "  启动 node${i} 挖矿 (1线程) ..."
    curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"miner_start","params":[1],"id":1}' \
        http://localhost:${RPC_PORT} | python3 -c "import sys,json; r=json.load(sys.stdin); print('    结果:', r.get('result','FAIL'))"
done

# 等待挖矿产出一些区块
echo ""
echo "  等待挖矿产出初始区块 (15秒)..."
sleep 15

# 检查挖矿状态
for i in $(seq 1 $MINER_COUNT); do
    RPC_PORT=$((BASE_RPC_PORT + i - 1))
    BLOCK=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        http://localhost:${RPC_PORT} | python3 -c "import sys,json; r=json.load(sys.stdin); print(int(r['result'],16))")
    echo "  node${i} 当前区块高度: ${BLOCK}"
done

# 2. 记录挖矿启动后内存
echo ""
echo "=== 挖矿启动后内存 (发送交易前) ==="
free -h | head -2
TOTAL_RSS=$(ps aux | grep -E "geth.*--datadir.*node[0-9]+" | grep -v grep | awk '{sum+=$6} END {printf "%.0f", sum}')
echo "总 RSS: $((TOTAL_RSS/1024)) MB"

# 3. 均匀发送 3000 笔交易
echo ""
echo "=== 发送 ${TX_COUNT} 笔交易 ==="

# 收集所有账户地址和RPC端口
declare -a RPC_PORTS
declare -a ADDRS
for i in $(seq 1 $TOTAL_NODES); do
    RPC_PORTS[$i]=$((BASE_RPC_PORT + i - 1))
    ACCOUNT_FILE=$(ls -t "${SCRIPT_DIR}/node${i}/keystore/UTC--"* 2>/dev/null | head -1 || true)
    if [ -n "$ACCOUNT_FILE" ]; then
        ADDRS[$i]=$(jq -r '.address' "$ACCOUNT_FILE")
    fi
done

# 解锁所有交易发送节点（node3~node64）
echo "  解锁交易发送节点 (node3~node64)..."
UNLOCK_COUNT=0
for i in $(seq 3 $TOTAL_NODES); do
    RPC_PORT=${RPC_PORTS[$i]}
    ADDR=${ADDRS[$i]}
    if [ -z "$ADDR" ]; then continue; fi
    curl -s -X POST -H "Content-Type: application/json" \
        --data "{\"jsonrpc\":\"2.0\",\"method\":\"personal_unlockAccount\",\"params\":[\"0x${ADDR}\",\"${PASSWORD}\",3600],\"id\":1}" \
        http://localhost:${RPC_PORT} > /dev/null 2>&1
    UNLOCK_COUNT=$((UNLOCK_COUNT + 1))
done
echo "  已解锁 ${UNLOCK_COUNT} 个节点"

# 计算每节点发送交易数
SENDER_START=3
SENDER_END=$TOTAL_NODES
SENDER_COUNT=$((SENDER_END - SENDER_START + 1))  # 62
TX_PER_NODE=$((TX_COUNT / SENDER_COUNT))          # 48
REMAINING=$((TX_COUNT - TX_PER_NODE * SENDER_COUNT))

echo "  ${SENDER_COUNT} 个发送节点, 每节点 ${TX_PER_NODE} 笔, 剩余 ${REMAINING} 笔"

# 发送交易
SUCCESS=0
FAIL=0
START_TIME=$(date +%s)

for i in $(seq $SENDER_START $SENDER_END); do
    RPC_PORT=${RPC_PORTS[$i]}
    FROM_ADDR="0x${ADDRS[$i]}"
    # 目标地址：发给 node1
    TO_ADDR="0x${ADDRS[1]}"

    if [ -z "${ADDRS[$i]}" ]; then continue; fi

    # 每节点发 TX_PER_NODE 笔
    NODE_TX=$TX_PER_NODE
    # 前 REMAINING 个节点多发1笔
    if [ $((i - SENDER_START)) -lt $REMAINING ]; then
        NODE_TX=$((NODE_TX + 1))
    fi

    for t in $(seq 1 $NODE_TX); do
        # 随机金额避免重复交易被拒
        AMOUNT=$(printf "0x%x" $((RANDOM % 1000 + 1)))
        RESULT=$(curl -s -X POST -H "Content-Type: application/json" \
            --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_sendTransaction\",\"params\":[{\"from\":\"${FROM_ADDR}\",\"to\":\"${TO_ADDR}\",\"value\":\"${AMOUNT}\",\"gas\":\"0x5208\",\"gasPrice\":\"0x3b9aca00\"}],\"id\":1}" \
            http://localhost:${RPC_PORT} 2>/dev/null || echo "FAIL")

        if echo "$RESULT" | grep -q '"result"'; then
            SUCCESS=$((SUCCESS + 1))
        else
            FAIL=$((FAIL + 1))
        fi

        # 每发100笔打印进度
        TOTAL_SENT=$((SUCCESS + FAIL))
        if [ $((TOTAL_SENT % 100)) -eq 0 ]; then
            ELAPSED=$(($(date +%s) - START_TIME))
            echo "  进度: ${TOTAL_SENT}/${TX_COUNT} (成功:${SUCCESS} 失败:${FAIL}) 耗时:${ELAPSED}s"
        fi
    done
done

END_TIME=$(date +%s)
SEND_ELAPSED=$((END_TIME - START_TIME))
echo ""
echo "  交易发送完成: 成功=${SUCCESS} 失败=${FAIL} 耗时=${SEND_ELAPSED}s"

# 4. 等待交易被打包 + 继续运行到至少10分钟
TOTAL_ELAPSED=$(($(date +%s) - START_TIME))
WAIT_TIME=0
if [ $TOTAL_ELAPSED -lt 600 ]; then
    WAIT_TIME=$((600 - TOTAL_ELAPSED))
fi
echo ""
echo "=== 等待交易被打包 (额外等待 ${WAIT_TIME}s，确保总运行 ≥10min) ==="
sleep $WAIT_TIME

# 5. 最终内存快照
echo ""
echo "=========================================="
echo "最终内存使用情况"
echo "=========================================="
free -h

echo ""
echo "--- 挖矿节点内存 ---"
for i in $(seq 1 $MINER_COUNT); do
    ps aux | grep "geth.*--datadir.*node${i}" | grep -v grep | awk -v name="node${i}" '{
        printf "  %-8s RSS: %d MB (%.1f%%)\n", name, $6/1024, $4
    }'
done

echo ""
echo "--- 所有 geth 进程内存汇总 ---"
ps aux | grep -E "geth.*--datadir.*node[0-9]+" | grep -v grep | awk '
{
    pid=$2; pmem=$4; rss=$6; rss_mb=int(rss/1024)
    for(i=11;i<=NF;i++){
        if(match($i, /node[0-9]+/)){node=$i; break}
    }
    if(node=="") node="geth"
    printf "  %-12s PID:%-8s RSS:%-8d MB  (%.1f%%)\n", node, pid, rss_mb, pmem
}'

TOTAL_RSS_FINAL=$(ps aux | grep -E "geth.*--datadir.*node[0-9]+" | grep -v grep | awk '{sum+=$6} END {printf "%.0f", sum}')
TOTAL_PMEM_FINAL=$(ps aux | grep -E "geth.*--datadir.*node[0-9]+" | grep -v grep | awk '{sum+=$4} END {printf "%.2f", sum}')

echo ""
echo "=========================================="
echo "汇总"
echo "=========================================="
echo "  节点数: 64 (挖矿: 2, 交易: 62)"
echo "  交易数: ${TX_COUNT} (成功:${SUCCESS} 失败:${FAIL})"
echo "  总 RSS: $((TOTAL_RSS_FINAL/1024)) MB"
echo "  总 %MEM: ${TOTAL_PMEM_FINAL}%"
echo "=========================================="

# 6. 查看最终区块高度
echo ""
echo "--- 最终区块高度 ---"
for i in $(seq 1 $TOTAL_NODES); do
    RPC_PORT=$((BASE_RPC_PORT + i - 1))
    BLOCK=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        http://localhost:${RPC_PORT} 2>/dev/null | python3 -c "import sys,json; r=json.load(sys.stdin); print(int(r['result'],16))" 2>/dev/null || echo "?")
    if [ "$i" -le 4 ] || [ "$i" -eq 32 ] || [ "$i" -eq 64 ]; then
        echo "  node${i}: block ${BLOCK}"
    fi
done
echo "  ...(仅显示部分节点)"
