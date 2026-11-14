#!/usr/bin/env python3
import json
import sys
from eth_account import Account
from Crypto.Hash import keccak
from eth_abi import encode

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
    """使用私钥对32字节摘要进行签名"""
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

def parse_hardware_ids(hardware_id_strs):
    """将 HardwareId 字符串数组转换为 bytes32 数组"""
    hardware_ids = []
    for hw_str in hardware_id_strs:
        # 移除0x前缀
        if hw_str.startswith('0x'):
            hw_str = hw_str[2:]
        # 检查长度
        if len(hw_str) != 64:
            print(f"错误：HardwareId '{hw_str}' 不是有效的32字节（64个十六进制字符）")
            sys.exit(1)
        # 转换为字节
        hw_bytes = bytes.fromhex(hw_str)
        hardware_ids.append(hw_bytes)
    return hardware_ids

def main():
    # 检查参数
    if len(sys.argv) < 4:
        print("用法：python3 hardwareId_signature.py <节点数字1> <节点数字2> ... -- <HardwareId1> <HardwareId2> ...")
        print("示例：python3 hardwareId_signature.py 1 2 3 -- 0x1234... 0xabcd...")
        print("\n说明：")
        print("  - 在 -- 前面的数字代表参与签名的节点编号（如 1 2 3）")
        print("  - 在 -- 后面的是 HardwareId 的 hex 字符串数组（必须是32字节）")
        sys.exit(1)
    
    try:
        separator_index = sys.argv.index('--')
    except ValueError:
        print("错误：必须使用 '--' 分隔节点编号和 HardwareId")
        sys.exit(1)
    
    # 解析节点数字参数
    node_numbers = []
    for arg in sys.argv[1:separator_index]:
        try:
            node_num = int(arg)
            node_numbers.append(node_num)
        except ValueError:
            print(f"错误：'{arg}' 不是有效的数字")
            sys.exit(1)
    
    if not node_numbers:
        print("错误：至少需要提供一个节点数字")
        sys.exit(1)
    
    # 解析 HardwareId 参数
    hardware_id_strs = sys.argv[separator_index + 1:]
    if not hardware_id_strs:
        print("错误：至少需要提供一个 HardwareId")
        sys.exit(1)
    
    # 转换 HardwareId 为字节数组
    hardware_ids = parse_hardware_ids(hardware_id_strs)
    
    # 将所有 HardwareId 顺序拼接
    concatenated = b''.join(hardware_ids)
    
    # 计算 rotateActiveTEEs 验签所需哈希：
    # messageHash = keccak256(abi.encode(_HardwareId))
    # ethSignedHash = keccak256("\x19Ethereum Signed Message:\n32" ++ messageHash)
    # 将 hardware_ids 作为 bytes32[] 进行 ABI 编码
    encoded = encode(["bytes32[]"], [hardware_ids])
    message_hash = keccak256_hash(encoded)
    eth_signed_hash = to_eth_signed_message_hash(message_hash)
    
    # 加载私钥
    nodes_data = load_private_keys()
    
    # 对 eth_signed_hash 进行签名
    signatures = []
    for node_num in node_numbers:
        private_key = get_private_key(node_num, nodes_data)
        signature = sign_message(private_key, eth_signed_hash)
        signatures.append(signature)
    
    # 将所有签名顺序拼接在一起
    concatenated_signatures = b''.join(signatures)
    
    # 输出：rotateActiveTEEs的输入格式（可直接用于函数调用）
    hardware_ids_hex = [bytes_to_hex_string(hw_id) for hw_id in hardware_ids]
    hardware_ids_str = ', '.join([f'"{hw_id}"' for hw_id in hardware_ids_hex])
    signature_hex = bytes_to_hex_string(concatenated_signatures)
    
    print(f'HardwareId: [{hardware_ids_str}]')
    print(f'Signature: "{signature_hex}"')
    print(f'\n函数调用示例:')
    print(f'[{hardware_ids_str}], "{signature_hex}"')

if __name__ == "__main__":
    main()