#!/bin/bash

NODES=${1:-3}
RPC_PORT_BASE=8545
P2P_PORT_BASE=30301

if ! [[ "$NODES" =~ ^[0-9]+$ ]] || [[ "$NODES" -lt 2 ]]; then
  echo "Usage: $0 [node_count]"
  echo "node_count must be an integer >= 2"
  exit 1
fi

declare -a enodes
for ((i=1; i<=$NODES; i++)); do
  rpc_port=$((RPC_PORT_BASE + i - 1))
  p2p_port=$((P2P_PORT_BASE + i - 1))

  # 获取原始enode
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}' \
    http://localhost:$rpc_port)

  # 提取并处理enode
  enode_raw=$(echo $response | jq -r '.result.enode')

  # 正确替换端口（保留IP，只替换端口号）
  enode_fixed=$(echo $enode_raw | sed "s/:[0-9]\+'/:$p2p_port'/" | sed "s/:\/[0-9]\+/:\/$p2p_port/")

  # 处理特殊IP格式
  enode_final=$(echo $enode_fixed | sed "s/@\[::\]/@127.0.0.1/" | sed "s/@0.0.0.0/@127.0.0.1/")

  enodes[$i]=$enode_final
  echo "Node $i enode: ${enodes[$i]}"
done

# 互相添加对等节点（添加错误处理）
id_counter=1
for ((i=1; i<=$NODES; i++)); do
  for ((j=1; j<=$NODES; j++)); do
    if [ $i -ne $j ]; then
      rpc_port=$((RPC_PORT_BASE + i - 1))

      # 执行添加请求并捕获响应
      response=$(curl -s -X POST -H "Content-Type: application/json" \
        --data "{
          \"jsonrpc\":\"2.0\",
          \"method\":\"admin_addPeer\",
          \"params\":[\"${enodes[$j]}\"],
          \"id\":${id_counter}
        }" \
        http://localhost:$rpc_port)

      # 打印详细结果
      echo "Node $i added peer: Node $j => $response"
      ((id_counter++))

      # 添加短暂延迟
      sleep 0.1
    fi
  done
done
