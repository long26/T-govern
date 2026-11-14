#!/bin/bash
# 重置 DKG 状态脚本
# 用法: ./reset_dkg.sh [节点编号]
# 如果不指定节点编号，重置所有节点

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ $# -eq 0 ]; then
    echo "🔄 重置所有节点的 DKG 状态..."
    echo "警告：这将清除节点注册信息"
    read -p "确认继续？(y/n): " confirm
    if [ "$confirm" != "y" ]; then
        echo "已取消"
        exit 0
    fi
    
    # 删除节点注册文件
    if [ -f "${SCRIPT_DIR}/node_registry.json" ]; then
        rm "${SCRIPT_DIR}/node_registry.json"
        echo "✅ 已删除 node_registry.json"
    fi
    
    echo "✅ 所有节点的 DKG 状态已重置"
    echo "💡 请重启所有节点以重新初始化 DKG"
else
    NODE_NUM=$1
    NODE_NAME="node${NODE_NUM}"
    
    echo "🔄 重置节点 ${NODE_NAME} 的 DKG 状态..."
    
    # 注意：单个节点重置比较复杂，需要重启该节点
    # 最简单的方法是停止并重启节点
    
    if [ -f "${SCRIPT_DIR}/${NODE_NAME}.pid" ]; then
        echo "⚠️  节点 ${NODE_NAME} 正在运行"
        echo "建议操作:"
        echo "  1. 停止节点: ./stop_background.sh ${NODE_NUM}"
        echo "  2. 重置所有节点: ./reset_dkg.sh"
        echo "  3. 重新启动节点"
    else
        echo "⚠️  节点 ${NODE_NAME} 未运行"
        echo "如果需要重置所有节点，请运行: ./reset_dkg.sh"
    fi
fi

echo ""
echo "ℹ️  DKG 委员会信息:"
echo "   - 默认委员会规模: 5 个节点"
echo "   - 委员会成员: 节点 1-5 (index 0-4)"
echo "   - 非委员会成员: 节点 6-10 (index 5-9)"
echo "   - 只有委员会成员可以参与 DKG"