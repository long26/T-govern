import time
import random
import threading
from queue import Queue
from web3 import Web3
from web3.middleware.geth_poa import geth_poa_middleware
import argparse

# ===== 配置参数 =====
# 节点数量
NODE_COUNT = 3
BASE_PORT = 8545
NODE_RPC_URLS = [f"http://localhost:{BASE_PORT + i}" for i in range(NODE_COUNT)]
SENDER_NODE_INDEX = 0

# 私钥管理
PRIVATE_KEY_FILE = "privatekey.txt"

# 交易参数
AMOUNT_ETHER = 0.001
GAS_PRICE = Web3.to_wei(1, 'gwei')
GAS_LIMIT = 21000  # 标准转账gas
# 可选：自定义 input(即 data) 的关键字，留空则不携带 input
INPUT_KEYWORD = ""  # 例如: "hello-redactable"
# 单笔交易模式：当通过 --input 传参时开启
SINGLE_TX_MODE = False

# 区块参数
TARGET_TX_PER_BLOCK = 50  # 每个区块的目标交易数
BLOCK_TIME = 1  # 目标区块时间（秒）
TX_PER_SECOND = TARGET_TX_PER_BLOCK / BLOCK_TIME  # 每秒交易数

# 运行时间（秒）
DURATION_SECONDS = 40

# ===== 初始化 Web3 =====
w3 = Web3(Web3.HTTPProvider(NODE_RPC_URLS[SENDER_NODE_INDEX]))
if not w3.is_connected():
    print(f"错误：无法连接到发送方节点 {NODE_RPC_URLS[SENDER_NODE_INDEX]}")
    exit(1)

w3.middleware_onion.inject(geth_poa_middleware, layer=0)

def get_private_key():
    """获取私钥"""
    try:
        with open(PRIVATE_KEY_FILE, 'r') as file:
            return file.readline().strip()
    except:
        print("无法读取私钥文件")
        exit(1)

private_key = get_private_key()
sender_account = w3.eth.account.from_key(private_key)
sender_address = sender_account.address
print(f"已加载发送方地址: {sender_address}")

# ===== 自动获取接收方地址 =====
def get_receivers():
    """从所有节点获取账户地址作为接收方"""
    receivers = []

    for rpc_url in NODE_RPC_URLS:
        try:
            node_w3 = Web3(Web3.HTTPProvider(rpc_url))
            if node_w3.is_connected():
                accounts = node_w3.eth.accounts
                for account in accounts:
                    if account.lower() != sender_address.lower():
                        receivers.append(account)
        except:
            continue

    if not receivers:
        print("错误：未找到任何接收方地址")
        exit(1)

    return list(set(receivers))

# ===== 交易队列和挖矿控制 =====
class TransactionManager:
    def __init__(self):
        self.tx_queue = Queue()
        self.mining = False
        self.tx_count = 0
        self.block_count = 0
        self.lock = threading.Lock()
        self.running = True
        self.generation_complete = threading.Event()

        # 获取初始区块号
        self.start_block = w3.eth.block_number

        # 获取接收方地址
        self.receivers = get_receivers()
        print(f"找到 {len(self.receivers)} 个接收方地址")

    def generate_transactions(self):
        """生成交易并放入队列"""
        interval = 1.0 / TX_PER_SECOND
        start_time = time.time()
        end_time = start_time + DURATION_SECONDS
        count = 0

        # 单笔交易模式：只生成一次并返回
        if SINGLE_TX_MODE:
            receiver = random.choice(self.receivers)
            data_hex = INPUT_KEYWORD if INPUT_KEYWORD and INPUT_KEYWORD.startswith("0x") else (Web3.to_hex(text=INPUT_KEYWORD) if INPUT_KEYWORD else None)
            tx_data = {
                'receiver': receiver,
                'value': w3.to_wei(AMOUNT_ETHER, 'ether'),
                'data': data_hex
            }
            self.tx_queue.put(tx_data)
            count += 1
            self.generation_complete.set()
            print(f"交易生成完成，共生成 {count} 笔交易")
            return

        while time.time() < end_time and self.running:
            # 随机选择接收方
            receiver = random.choice(self.receivers)

            # 编码 input(data)：支持 0x... 原样传入或文本自动编码为 hex
            if INPUT_KEYWORD:
                data_hex = INPUT_KEYWORD if INPUT_KEYWORD.startswith("0x") else Web3.to_hex(text=INPUT_KEYWORD)
            else:
                data_hex = None

            # 创建交易对象（传入 data）
            tx_data = {
                'receiver': receiver,
                'value': w3.to_wei(AMOUNT_ETHER, 'ether'),
                'data': data_hex
            }

            # 放入队列
            self.tx_queue.put(tx_data)
            count += 1

            # 计算并等待下一次交易
            next_time = start_time + (count * interval)
            sleep_time = max(0, next_time - time.time())
            time.sleep(sleep_time)

        self.generation_complete.set()
        print(f"交易生成完成，共生成 {count} 笔交易")

    def process_transactions(self):
        """处理交易队列中的交易"""
        while not self.tx_queue.empty() or self.running:
            if self.tx_queue.empty():
                continue
            try:
                # 从队列获取交易
                tx_data = self.tx_queue.get(timeout=1)

                # 获取当前nonce
                nonce = w3.eth.get_transaction_count(sender_address, 'pending')

                # 构建交易
                tx = {
                    'nonce': nonce,
                    'to': tx_data['receiver'],
                    'value': tx_data['value'],
                    'gas': GAS_LIMIT+1000,
                    'gasPrice': GAS_PRICE,
                    'chainId': w3.eth.chain_id
                }
                # 在这里传入 input -> data
                if tx_data.get('data'):
                    tx['data'] = tx_data['data']
                    # 含 data 时估算 gas，避免 21000 不足

                # 签名并发送交易
                signed_tx = w3.eth.account.sign_transaction(tx, private_key)
                tx_hash = w3.eth.send_raw_transaction(signed_tx.rawTransaction)

                with self.lock:
                    self.tx_count += 1

                # 每发送50笔交易检查一次区块
                if self.tx_count % TARGET_TX_PER_BLOCK == 0:
                    print("miner_block")
                    self.mine_block()
                # 显示状态
                if self.tx_count % 10 == 0:
                    balance = w3.from_wei(w3.eth.get_balance(sender_address), 'ether')
                    print(f"[{time.strftime('%H:%M:%S')}] 已发送交易: {self.tx_count}, 余额: {balance:.4f} ETH")
            except Exception as e:
                print(f"处理交易时出错: {str(e)}")

    def mine_block(self):
        """挖一个包含目标数量交易的区块"""
        with self.lock:
            if self.mining:
                return

            self.mining = True
            print(f"开始挖矿，目标交易数: {TARGET_TX_PER_BLOCK}")

            # 启动挖矿
            start_block = w3.eth.block_number
            w3.geth.miner.start(1)
            # 等待区块生成
            while True:
                current_block = w3.eth.block_number
                if current_block > start_block:
                    block = w3.eth.get_block(current_block)
                    tx_count = len(block.transactions)
                    print(f"新区块 #{current_block} 包含 {tx_count} 笔交易")
                    break
                    # 检查交易数量
#                     if tx_count >= TARGET_TX_PER_BLOCK:
#                         break

                time.sleep(0.1)

            w3.geth.miner.stop()
            self.mining = False
            self.block_count += 1
            print(f"停止挖矿，已挖区块: {self.block_count}")

    def run(self):
        """运行交易管理和挖矿"""
        # 启动交易生成线程
        generator_thread = threading.Thread(target=self.generate_transactions)
        generator_thread.start()

        # 启动交易处理线程
        processor_thread = threading.Thread(target=self.process_transactions)
        processor_thread.start()
        # 等待完成
        generator_thread.join()
        # 等待队列清空
        while not self.tx_queue.empty():
            time.sleep(1)

        self.running = False
        # 如果还有未打包的交易，挖最后一个区块
        if self.tx_count % TARGET_TX_PER_BLOCK != 0:
            self.mine_block()

        # 等待交易处理完成
        processor_thread.join(timeout=30)

        # 最终统计
        final_balance = w3.from_wei(w3.eth.get_balance(sender_address), 'ether')
        print("\n===== 自动转账完成 =====")
        print(f"总交易数: {self.tx_count}")
        print(f"总区块数: {self.block_count}")
        print(f"平均每区块交易数: {self.tx_count / self.block_count if self.block_count > 0 else 0:.2f}")
        print(f"最终余额: {final_balance:.4f} ETH")
        print("=========================")

# ===== 主程序 =====
if __name__ == "__main__":
    # 解析命令行参数：--input 支持文本或 0x十六进制
    parser = argparse.ArgumentParser()
    parser.add_argument('--input', dest='input_keyword', help='交易 input，支持文本或0x十六进制')
    args = parser.parse_args()
    if args.input_keyword is not None:
        INPUT_KEYWORD = args.input_keyword
        SINGLE_TX_MODE = True  # 开启单笔模式

    print(f"节点数量: {NODE_COUNT}")
    print(f"发送方地址: {sender_address}")
    print(f"目标每区块交易数: {TARGET_TX_PER_BLOCK}")
    print(f"目标区块时间: {BLOCK_TIME} 秒")
    print(f"运行时间: {DURATION_SECONDS} 秒")
    print("=======================================")

    # 检查余额
    balance = w3.from_wei(w3.eth.get_balance(sender_address), 'ether')
    print(f"当前余额: {balance:.4f} ETH")

    # 计算预计转账总额
    total_amount = AMOUNT_ETHER * TX_PER_SECOND * DURATION_SECONDS
    if balance < total_amount:
        print(f"警告：余额可能不足！预计需要 {total_amount:.4f} ETH")

    # 启动交易管理
    manager = TransactionManager()
    manager.run()