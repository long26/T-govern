#!/bin/bash
# init.sh - 主控脚本：批量创建账户并初始化节点

# ===== 配置参数 =====
if [ $# -ne 1 ]; then
    echo "错误：请指定节点个数"
    exit 1
fi


NODE_COUNT=$1  # 要创建的节点数量
GENESIS_FILE="genesis.json"
GENESIS_TEMP="genesis_temp.json"
SHARED_KEY_FILE="all_privatekeys.json"  # 共享密钥文件

echo "=== 清空共享密钥文件 ==="
echo '{"nodes": {}}' > "$SHARED_KEY_FILE"
echo "✅ 已清空共享密钥文件: ${SHARED_KEY_FILE}"

echo "=== 删除节点注册和密钥文件 ==="
if [ -f "node_registry.json" ]; then
    rm -f "node_registry.json"
    echo "✅ 已删除 node_registry.json"
else
    echo "ℹ️  node_registry.json 不存在，跳过"
fi

if [ -f "nitch_keys.json" ]; then
    rm -f "nitch_keys.json"
    echo "✅ 已删除 nitch_keys.json"
else
    echo "ℹ️  nitch_keys.json 不存在，跳过"
fi

addresses=()
jq '.address = ""' storage.json > tmp.json && mv tmp.json storage.json

echo "=== 创建所有节点账户 ==="
for i in $(seq 1 $NODE_COUNT); do
    echo "创建节点 $i 账户..."
    addr=$(./create_account.sh $i)
    addresses+=($addr)
    echo "✅ 账户地址: $addr"
done

# ===== 更新创世文件 =====
echo -e "\n=== 更新创世文件分配"
cp "${GENESIS_FILE}" "${GENESIS_TEMP}"  # 备份原始文件
echo "清空alloc字段..."
jq '.alloc = {}' "$GENESIS_TEMP" > tmp.json && mv tmp.json "$GENESIS_TEMP"
# 添加所有地址到创世文件
for addr in "${addresses[@]}"; do
    echo "添加地址: $addr"
    jq --arg addr "$addr" \
        '.alloc[$addr] = {
            "balance": "1000000000000000000000"
        }' "$GENESIS_TEMP" > tmp.json && mv tmp.json "$GENESIS_TEMP"
done

# 应用更新
mv "$GENESIS_TEMP" "$GENESIS_FILE"
echo "✅ 创世文件更新完成"

# ===== 第二阶段：初始化所有节点 =====
echo -e "\n=== 初始化节点 ==="
for i in $(seq 1 $NODE_COUNT); do
    ./init_node.sh $i
done

echo -e "\n======================================="
echo "网络搭建完成！共创建 ${NODE_COUNT} 个节点"
echo "账户地址列表:"
printf '%s\n' "${addresses[@]}"
echo "======================================="