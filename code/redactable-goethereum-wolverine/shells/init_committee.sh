#!/bin/bash
# 初始化委员会配置脚本
# 用法: ./init_committee.sh <委员会规模> <总节点数>
# 示例: ./init_committee.sh 5 10        # 5个委员会成员，总共10个节点
# 示例: ./init_committee.sh 3 10        # 3个委员会成员，总共10个节点

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ $# -ne 2 ]; then
    echo "错误：请提供委员会规模和总节点数"
    echo "用法: $0 <委员会规模> <总节点数>"
    echo ""
    echo "示例:"
    echo "  $0 5 10   # 5个委员会成员，总共10个节点"
    echo "  $0 3 10   # 3个委员会成员，总共10个节点"
    exit 1
fi

COMMITTEE_SIZE=$1
TOTAL_NODES=$2

if [ $COMMITTEE_SIZE -gt $TOTAL_NODES ]; then
    echo "❌ 错误：委员会规模不能超过总节点数"
    exit 1
fi

echo "📝 初始化委员会配置..."
echo "   委员会规模: $COMMITTEE_SIZE"
echo "   总节点数: $TOTAL_NODES"
echo ""

# 创建 node_registry.json
REGISTRY_FILE="${SCRIPT_DIR}/node_registry.json"

# 生成注册表
NODES_JSON=""
for ((i=0; i<TOTAL_NODES; i++)); do
    IS_COMMITTEE=$([ $i -lt $COMMITTEE_SIZE ] && echo "true" || echo "false")
    NODE_ID="${SCRIPT_DIR}/node$((i+1))"
    
    if [ -n "$NODES_JSON" ]; then
        NODES_JSON="$NODES_JSON,"
    fi
    
    NODES_JSON="$NODES_JSON
    {
      \"node_id\": \"$NODE_ID\",
      \"index\": $i,
      \"is_committee\": $IS_COMMITTEE,
      \"timestamp\": \"$(date -u +"%Y-%m-%dT%H:%M:%S.%N+00:00")\"
    }"
done

cat > "$REGISTRY_FILE" <<EOF
{
  "nodes": [$NODES_JSON
  ]
}
EOF

if [ $? -eq 0 ]; then
    echo "✅ 委员会配置已保存到 $REGISTRY_FILE"
    echo ""
    echo "委员会成员（节点 1-$COMMITTEE_SIZE）:"
    for ((i=1; i<=COMMITTEE_SIZE; i++)); do
        echo "  节点 $i (index $((i-1)))"
    done
    echo ""
    echo "非委员会成员（节点 $((COMMITTEE_SIZE+1))-$TOTAL_NODES）:"
    for ((i=$((COMMITTEE_SIZE+1)); i<=TOTAL_NODES; i++)); do
        echo "  节点 $i (index $((i-1)))"
    done
    echo ""
    echo "⚠️  重要提示："
    echo "   1. 现在可以启动节点，委员会配置将自动应用"
    echo "   2. 如果已有 node_registry.json，此操作会覆盖它"
    echo "   3. 如果已启动过节点，请先运行 ./reset_dkg.sh 清理"
    echo ""
    echo "下一步操作："
    echo "  1. 启动节点: ./run_all_background.sh $TOTAL_NODES"
    echo "  2. 连接节点: ../addPeer.sh $TOTAL_NODES"
    echo "  3. 启动 DKG: 在委员会节点上调用 eth.startDKG()"
    echo "  4. 开放业务交易前: ./wait_dkg_ready.sh $COMMITTEE_SIZE $TOTAL_NODES"
else
    echo "❌ 错误：无法创建 $REGISTRY_FILE"
    exit 1
fi
