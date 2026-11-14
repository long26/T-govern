#!/bin/bash
# 设置委员会节点脚本
# 用法: ./set_committee.sh <委员会规模> [节点列表]
# 示例1: ./set_committee.sh 5              # 前5个节点为委员会
# 示例2: ./set_committee.sh 3 1 3 5        # 节点1、3、5为委员会

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGISTRY_FILE="${SCRIPT_DIR}/node_registry.json"

if [ ! -f "$REGISTRY_FILE" ]; then
    echo "❌ 错误：未找到 $REGISTRY_FILE"
    echo "请先启动节点以生成注册文件"
    exit 1
fi

# 读取现有注册表
REGISTRY=$(cat "$REGISTRY_FILE")

if [ $# -eq 0 ]; then
    echo "错误：请指定委员会规模"
    echo "用法: $0 <委员会规模> [节点列表]"
    echo ""
    echo "示例:"
    echo "  $0 5              # 前5个节点为委员会"
    echo "  $0 3 1 3 5        # 节点1、3、5为委员会"
    exit 1
fi

COMMITTEE_SIZE=$1

if [ $# -eq 1 ]; then
    # 模式1：前N个节点为委员会
    echo "📝 设置前 $COMMITTEE_SIZE 个节点为委员会..."
    
    # 使用 jq 修改注册表，标记前N个节点为委员会成员
    NEW_REGISTRY=$(echo "$REGISTRY" | jq --argjson size "$COMMITTEE_SIZE" '
        .nodes |= map(.is_committee = (.index < $size))
    ')
    
    if [ $? -eq 0 ]; then
        echo "$NEW_REGISTRY" > "$REGISTRY_FILE"
        echo "✅ 委员会设置完成"
        echo ""
        echo "委员会成员（前 $COMMITTEE_SIZE 个节点）:"
        echo "$NEW_REGISTRY" | jq -r '.nodes[] | select(.is_committee == true) | "  节点 \(.index + 1) (index \(.index)): \(.node_id)"'
    else
        echo "❌ 错误：处理失败，请确保已安装 jq"
        exit 1
    fi
else
    # 模式2：指定特定节点为委员会
    shift
    COMMITTEE_NODES=("$@")
    
    echo "📝 设置指定节点为委员会: ${COMMITTEE_NODES[@]}"
    
    # 创建临时文件存储委员会节点索引（0-based）
    COMMITTEE_INDICES=()
    for node_num in "${COMMITTEE_NODES[@]}"; do
        # 转换为 0-based 索引
        index=$((node_num - 1))
        COMMITTEE_INDICES+=("$index")
    done
    
    # 构建 jq 查询
    INDEX_FILTER=$(printf ",%s" "${COMMITTEE_INDICES[@]}")
    INDEX_FILTER="[${INDEX_FILTER:1}]"
    
    # 使用 jq 修改注册表
    NEW_REGISTRY=$(echo "$REGISTRY" | jq --argjson indices "$INDEX_FILTER" '
        .nodes |= map(.is_committee = (.index as $i | $indices | contains([$i])))
    ')
    
    if [ $? -eq 0 ]; then
        echo "$NEW_REGISTRY" > "$REGISTRY_FILE"
        echo "✅ 委员会设置完成"
        echo ""
        echo "委员会成员（节点 ${COMMITTEE_NODES[@]}）:"
        echo "$NEW_REGISTRY" | jq -r '.nodes[] | select(.is_committee == true) | "  节点 \(.index + 1) (index \(.index)): \(.node_id)"'
    else
        echo "❌ 错误：处理失败，请确保已安装 jq"
        exit 1
    fi
fi

echo ""
echo "⚠️  重要提示："
echo "   1. 修改委员会后需要重启所有节点"
echo "   2. 修改委员会会重置 DKG 状态"
echo ""
echo "建议操作流程："
echo "  1. 停止所有节点: ./stop_all_background.sh 10"
echo "  2. 设置委员会: ./set_committee.sh $COMMITTEE_SIZE"
echo "  3. 重置 DKG: ./reset_dkg.sh"
echo "  4. 重新启动节点: ./run_all_background.sh 10"
echo "  5. 连接节点: ./addPeer.sh 10"