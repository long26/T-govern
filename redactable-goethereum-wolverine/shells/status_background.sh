#!/bin/bash
# 查看后台运行的节点状态
# 用法: ./status_background.sh [节点编号]
# 如果不指定节点编号，显示所有节点状态

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_RPC_PORT=8545
LOG_DIR="${SCRIPT_DIR}/nodelogs"

if [ $# -eq 0 ]; then
    # 显示所有节点状态
    echo "📊 所有后台节点状态:"
    echo "======================================"
    
    found=false
    cd "$SCRIPT_DIR"
    for pid_file in node*.pid; do
        if [ -f "$pid_file" ]; then
            node_name="${pid_file%.pid}"
            NODE_NUM=${node_name#node}
            RPC_PORT=$((BASE_RPC_PORT + NODE_NUM - 1))
            LOG_FILE="${LOG_DIR}/${node_name}.log"
            
            if [ -f "$pid_file" ]; then
                PID=$(cat "$pid_file")
                if ps -p "$PID" > /dev/null 2>&1; then
                    echo "✅ ${node_name}: 运行中 (PID: ${PID}, RPC: localhost:${RPC_PORT})"
                    echo "   日志: ${LOG_FILE}"
                else
                    echo "❌ ${node_name}: 已停止 (PID 文件残留)"
                    echo "   建议: 运行 ./stop_background.sh ${NODE_NUM} 清理"
                fi
                found=true
            fi
        fi
    done
    
    if [ "$found" = false ]; then
        echo "没有找到后台运行的节点"
    fi
    echo ""
    echo "使用方式:"
    echo "  查看单个节点: ./status_background.sh <节点编号>"
    echo "  启动节点: ./run_background.sh <节点编号>"
    echo "  停止节点: ./stop_background.sh <节点编号>"
else
    # 显示指定节点状态
    NODE_NUM=$1
    NODE_NAME="node${NODE_NUM}"
    PID_FILE="${SCRIPT_DIR}/${NODE_NAME}.pid"
    LOG_FILE="${LOG_DIR}/${NODE_NAME}.log"
    RPC_PORT=$((BASE_RPC_PORT + NODE_NUM - 1))
    
    if [ ! -f "$PID_FILE" ]; then
        echo "❌ 节点 ${NODE_NAME} 未在后台运行"
        echo ""
        echo "使用 ./run_background.sh ${NODE_NUM} 启动节点"
        exit 1
    fi
    
    PID=$(cat "$PID_FILE")
    
    echo "📊 节点 ${NODE_NAME} 状态:"
    echo "======================================"
    echo "PID: ${PID}"
    echo "RPC 端口: ${RPC_PORT}"
    echo "日志文件: ${LOG_FILE}"
    
    if ps -p "$PID" > /dev/null 2>&1; then
        echo "状态: ✅ 运行中"
        
        # 检查 RPC 是否可用
        if command -v nc &> /dev/null; then
            if nc -z localhost "$RPC_PORT" 2>/dev/null; then
                echo "RPC: ✅ 可用 (http://localhost:${RPC_PORT})"
            else
                echo "RPC: ⚠️  不可用 (节点可能还在启动中)"
            fi
        fi
        
        echo ""
        echo "连接控制台: geth attach http://localhost:${RPC_PORT}"
        echo "查看日志: tail -f ${LOG_FILE}"
        echo "停止节点: ./stop_background.sh ${NODE_NUM}"
    else
        echo "状态: ❌ 已停止 (PID 文件残留)"
        echo ""
        echo "运行 ./stop_background.sh ${NODE_NUM} 清理 PID 文件"
    fi
fi