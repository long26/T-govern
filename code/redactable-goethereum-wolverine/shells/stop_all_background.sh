#!/bin/bash
# 批量停止后台运行的节点
# 用法: ./stop_all_background.sh [节点列表]
# 示例: ./stop_all_background.sh 1 2 3
# 或者: ./stop_all_background.sh 1 2 3 4 5

# 获取脚本所在目录的父目录（项目根目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 解析参数
NODES=()

if [ $# -eq 0 ]; then
    # 不传参数：停止节点 1-5
    NODES=(1 2 3 4 5)
elif [ $# -eq 1 ] && [[ "$1" =~ ^[0-9]+$ ]]; then
    # 传一个数字：停止节点 1 到这个数字
    END_NODE=$1
    for ((i=1; i<=END_NODE; i++)); do
        NODES+=("$i")
    done
else
    # 传多个参数：停止指定的节点
    NODES=("$@")
fi

echo "⏹️  批量停止节点..."
echo "节点列表: ${NODES[@]}"
echo "======================================"

SUCCESS_COUNT=0
FAILED_COUNT=0
FAILED_NODES=()

for NODE_NUM in "${NODES[@]}"; do
    echo ""
    echo "正在停止节点 ${NODE_NUM}..."
    
    if ./stop_background.sh "$NODE_NUM"; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    else
        FAILED_COUNT=$((FAILED_COUNT + 1))
        FAILED_NODES+=("$NODE_NUM")
    fi
done

echo ""
echo "======================================"
echo "批量停止完成！"
echo "✅ 成功: ${SUCCESS_COUNT} 个节点"
if [ ${FAILED_COUNT} -gt 0 ]; then
    echo "❌ 失败: ${FAILED_COUNT} 个节点 (节点: ${FAILED_NODES[*]})"
fi
echo ""
echo "查看所有节点状态: ./status_background.sh"