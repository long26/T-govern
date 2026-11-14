#!/bin/bash
# 停止后台运行的节点
# 用法: ./stop_background.sh <节点编号>

if [ $# -ne 1 ]; then
    echo "错误：请指定节点编号 (1, 2, 3...)"
    echo "用法: $0 <节点编号>"
    exit 1
fi

NODE_NUM=$1
NODE_NAME="node${NODE_NUM}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PID_FILE="${SCRIPT_DIR}/${NODE_NAME}.pid"

# 检查PID文件是否存在
if [ ! -f "$PID_FILE" ]; then
    echo "❌ 节点 ${NODE_NAME} 未在后台运行（未找到PID文件）"
    echo "   PID文件: ${PID_FILE}"
    exit 1
fi

# 读取PID
PID=$(cat "$PID_FILE")

# 验证PID是否有效
if [ -z "$PID" ]; then
    echo "❌ PID文件为空，正在清理..."
    rm -f "$PID_FILE"
    exit 1
fi

# 检查进程是否存在
if ! ps -p "$PID" > /dev/null 2>&1; then
    echo "⚠️  节点 ${NODE_NAME} 已停止（进程不存在），正在清理PID文件..."
    rm -f "$PID_FILE"
    exit 0
fi

echo "⏹️  正在停止节点 ${NODE_NAME}..."
echo "   PID: ${PID}"

# 尝试优雅地停止进程
kill "$PID" 2>/dev/null

# 等待进程结束，最多等待5秒
TIMEOUT=5
COUNT=0
while [ $COUNT -lt $TIMEOUT ]; do
    if ! ps -p "$PID" > /dev/null 2>&1; then
        echo "✅ 节点 ${NODE_NAME} 已成功停止"
        rm -f "$PID_FILE"
        exit 0
    fi
    sleep 1
    COUNT=$((COUNT + 1))
done

# 如果进程仍在运行，强制终止
if ps -p "$PID" > /dev/null 2>&1; then
    echo "⚠️  节点 ${NODE_NAME} 未响应，正在强制终止..."
    kill -9 "$PID" 2>/dev/null
    sleep 1
    
    # 再次检查
    if ! ps -p "$PID" > /dev/null 2>&1; then
        echo "✅ 节点 ${NODE_NAME} 已被强制终止"
        rm -f "$PID_FILE"
        exit 0
    else
        echo "❌ 无法停止节点 ${NODE_NAME} (PID: ${PID})"
        echo "   请手动检查并终止进程"
        exit 1
    fi
fi

exit 0