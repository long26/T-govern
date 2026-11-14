#!/usr/bin/env python3
import secrets
import json
import sys
from eth_account import Account
from Crypto.Hash import keccak

def generate_random_bytes(length=32):
    """生成指定长度的随机字节数组"""
    return secrets.token_bytes(length)

def bytes_to_hex_string(byte_array):
    """将字节数组转换为十六进制字符串（带0x前缀）"""
    return "0x" + byte_array.hex()

def keccak256_hash(data):
    """计算Ethereum Keccak256哈希值"""
    k = keccak.new(digest_bits=256)
    k.update(data)
    return k.digest()

def to_eth_signed_message_hash(message_hash):
    """计算与 Solidity 中 abi.encodePacked("\x19Ethereum Signed Message:\n32", messageHash) 一致的哈希"""
    if len(message_hash) != 32:
        raise ValueError(f"message_hash must be 32 bytes, got {len(message_hash)}")
    prefix = b"\x19Ethereum Signed Message:\n32"
    return keccak.new(data=prefix + message_hash, digest_bits=256).digest()

def load_private_keys():
    """从all_privatekeys.json加载私钥"""
    try:
        with open('all_privatekeys.json', 'r') as f:
            data = json.load(f)
            return data['nodes']
    except FileNotFoundError:
        print("错误：找不到 all_privatekeys.json 文件")
        sys.exit(1)
    except json.JSONDecodeError:
        print("错误：all_privatekeys.json 格式错误")
        sys.exit(1)

def get_private_key(node_number, nodes_data):
    """获取指定节点的私钥"""
    node_key = f"node{node_number}"
    if node_key not in nodes_data:
        print(f"错误：找不到 {node_key}")
        sys.exit(1)
    return nodes_data[node_key]['privateKey']

def sign_message(private_key_hex, digest32):
    """使用私钥对32字节摘要进行签名（不再额外添加前缀）"""
    # 移除0x前缀（如果有）
    if private_key_hex.startswith('0x'):
        private_key_hex = private_key_hex[2:]
    
    # 转换为字节
    private_key_bytes = bytes.fromhex(private_key_hex)
    
    # 创建账户
    account = Account.from_key(private_key_bytes)
    
    # 对32字节摘要进行签名
    signed_message = account.signHash(digest32)
    
    return signed_message.signature

def main():
    # 检查参数：需要提供节点数字
    if len(sys.argv) < 2:
        print("用法：python3 generate_tee_with_signature.py <节点数字> [节点数字...]")
        print("示例：python3 generate_tee_with_signature.py 1")
        print("      python3 generate_tee_with_signature.py 1 2 3")
        sys.exit(1)
    
    # 解析节点数字参数
    node_numbers = []
    for arg in sys.argv[1:]:
        try:
            node_num = int(arg)
            node_numbers.append(node_num)
        except ValueError:
            print(f"错误：'{arg}' 不是有效的数字")
            sys.exit(1)
    
    if not node_numbers:
        print("错误：至少需要提供一个节点数字")
        sys.exit(1)
    
    # 生成3个32字节的随机字节数组
    byte1 = generate_random_bytes(32)
    byte2 = generate_random_bytes(32)
    byte3 = generate_random_bytes(32)
    
    # 将三个数组顺次拼接
    concatenated = byte1 + byte2 + byte3
    
    # 计算合约 registerTEE 验签所需哈希：
    # messageHash = keccak256(abi.encode(MR_ENCLAVE, HardwareId, EnclavePubKey))
    # ethSignedHash = keccak256("\x19Ethereum Signed Message:\n32" ++ messageHash)
    message_hash = keccak256_hash(concatenated)
    eth_signed_hash = to_eth_signed_message_hash(message_hash)
    
    # 加载私钥
    nodes_data = load_private_keys()
    
    # 对 eth_signed_hash 进行签名，和合约 verifyAggregatedSignature 对齐
    signatures = []
    for node_num in node_numbers:
        private_key = get_private_key(node_num, nodes_data)
        signature = sign_message(private_key, eth_signed_hash)
        signatures.append(signature)
    
    # 将所有签名顺序拼接在一起
    concatenated_signatures = b''.join(signatures)
    
    # 输出：registerTEE的输入格式（带签名）
    tee_data = {
        "Value": "50",
        "MR_ENCLAVE": bytes_to_hex_string(byte1),
        "HardwareId": bytes_to_hex_string(byte2),
        "EnclavePubKey": bytes_to_hex_string(byte3),
        "Signature": bytes_to_hex_string(concatenated_signatures)
    }
    
    output = json.dumps(tee_data, indent=4)
    print("'" + output + "'")

if __name__ == "__main__":
    main()