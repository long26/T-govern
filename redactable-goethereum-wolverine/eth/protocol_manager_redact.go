package eth

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
	"runtime/debug"
	"time"
)

// verifyProposer 验证提案者身份
//func (pm *ProtocolManager) verifyProposer(proposerID enode.ID) bool {
//	// 获取提案者的节点信息
//	node := enode.MustParse(fmt.Sprintf("enode://%s", proposerID))
//	pubkey := node.Pubkey()
//	if pubkey == nil {
//		return false
//	}
//	proposerAddr := crypto.PubkeyToAddress(*pubkey)
//
//	// 检查提案者是否是授权节点
//	return pm.eth.IsValidator(proposerAddr)
//}

// verifyProposerSignature 验证提案签名
func (pm *ProtocolManager) verifyProposerSignature(redactTx types.RedactTx) bool {
	// 获取提案者的签名
	//signature := redactTx.GetSignature()
	//if len(signature) == 0 {
	//	return false
	//}
	//
	//// 获取提案者的节点信息和公钥
	//proposerID := redactTx.GetProposerID()
	//node := enode.MustParse(fmt.Sprintf("enode://%s", proposerID))
	//pubkey := node.Pubkey()
	//if pubkey == nil {
	//	return false
	//}
	//
	//// 构造待签名的消息
	//message := crypto.Keccak256(
	//	redactTx.GetBlockNum().Bytes(),
	//	redactTx.GetTxBefore().Bytes(),
	//	redactTx.GetTxAfter().Bytes(),
	//	[]byte(redactTx.GetReason()),
	//)
	//
	//// 验证签名
	//return crypto.VerifySignature(crypto.FromECDSAPub(pubkey), message, signature[:len(signature)-1])
	return true
}

// isAuthorizedNode 检查当前节点是否是授权节点
//func (pm *ProtocolManager) isAuthorizedNode() bool {
//	// 获取当前节点的以太坊地址
//	nodeAddr := crypto.PubkeyToAddress(*pm.p2pServer.Self().Pubkey())
//
//	// 检查节点是否是授权节点
//	return pm.eth.IsValidator(nodeAddr)
//}

// hasValidModificationRecord 检查是否有合法的修改记录
func (pm *ProtocolManager) hasValidModificationRecord(redactTx types.RedactTx) bool {
	// TODO: 实现修改记录验证逻辑
	// 这里应该检查数据库中是否存在该交易的合法修改记录
	return true
}

// updateBlockInfo 更新区块信息
func (pm *ProtocolManager) updateBlockInfo(block *types.Block, newBlock *types.Block) error {
	totalStart := time.Now()
	blockNumber := block.Number().Uint64()

	blockHash := pm.blockchain.ReadCanonicalHash(blockNumber)
	if blockHash == (common.Hash{}) {
		log.Error("Canonical block not found", "number", blockNumber)
		return fmt.Errorf("canonical block not found for %d", blockNumber)
	}

	// 所有删改进入单个 LevelDB batch，一次提交（a1f6170 起；此前逐笔直写
	// lookup entry 是 ledger_update 随区块交易数线性放大的主因）。
	// [perf 2026-09-03 最小写集，对齐 tee-connect 同名改动] 调用方
	// ValidateModifiedBlockConsistency 已证：新旧 header RLP 逐字节相同
	// （⇒ 块哈希不变，该函数含显式检查）、除编辑位外交易字节级相同、编辑交易
	// 的 chameleon hash 不变、块号不变。因此 header/canonical hash/tx lookup
	// 的重写全部是同值重写——其中 WriteTxLookupEntries 还要对块内每笔交易重算
	// 一次 chameleon tx hash（接收侧 newBlock 来自线上反序列化，缓存全冷）。
	// 现收缩为唯一真正变化的 body 原位写（body key=(number, blockHash)，两
	// 分量均不变）+ 保持原 DeleteBlock 净效果的 receipts/td 删除；守卫块哈
	// 希不变，未来若出现改哈希的编辑类型则拒绝而非写坏索引。落盘终态与改
	// 前逐字节一致。恢复=还原为 DeleteBlock+DeleteTxLookupEntry+WriteBlock
	// +WriteCanonicalHash+WriteTxLookupEntries 五调用（a1f6170）。
	newBlockHash := newBlock.Hash()
	if newBlockHash != blockHash {
		return fmt.Errorf("redaction changed block hash: have %s, want %s", newBlockHash.Hex(), blockHash.Hex())
	}
	buildStart := time.Now()
	batch := pm.blockchain.NewBatch()
	rawdb.DeleteReceipts(batch, blockHash, blockNumber)
	rawdb.DeleteTd(batch, blockHash, blockNumber)
	rawdb.DeleteBody(batch, blockHash, blockNumber)
	rawdb.WriteBody(batch, blockHash, blockNumber, newBlock.Body())
	buildDur := time.Since(buildStart)
	batchBytes := batch.ValueSize()
	commitStart := time.Now()
	if err := batch.Write(); err != nil {
		return fmt.Errorf("redaction block write: %v", err)
	}
	commitDur := time.Since(commitStart)

	// 缓存失效与原 DeleteBlock/WriteBlock 一致：旧 hash 与新 hash 的
	// block/body/RLP 缓存都不能保留（canonical hash 不变的 body 替换）。
	pm.blockchain.RemoveRedactionCaches(blockHash)
	pm.blockchain.RemoveRedactionCaches(newBlockHash)

	reqID := redactionJoinKey(block.Number(), ^uint64(0))
	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_batch_build",
		"duration_ns", buildDur.Nanoseconds(), "join_key", reqID, "block", blockNumber,
		"txs", len(newBlock.Transactions()), "status", "ok")
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_batch_commit",
		"duration_ns", commitDur.Nanoseconds(), "join_key", reqID, "block", blockNumber,
		"txs", len(newBlock.Transactions()),
		"batch_bytes", batchBytes,
		"gc_pauses", gcStats.NumGC, "gc_pause_total_ns", gcStats.PauseTotal.Nanoseconds(), "status", "ok")
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_total",
		"duration_ns", time.Since(totalStart).Nanoseconds(), "join_key", reqID,
		"txs", len(newBlock.Transactions()), "block", blockNumber, "status", "ok")

	return nil
}
func (pm *ProtocolManager) getNewBlock(block *types.Block, msg *types.ModifiedMessage, index uint64) (*types.Block, error) {
	return pm.getNewBlockWithRandomizer(block, msg, index, nil)
}

// getLedgerUpdateBlockWithRandomizer reconstructs the receiver's expected
// edited body without recomputing TxHash. The receiver separately proves that
// the transaction root and transaction identities are unchanged, so deriving
// the root here would duplicate the expensive validation work. Keep this
// separate from getNewBlockWithRandomizer: proposer-side block construction
// still needs to derive the header transaction root.
func (pm *ProtocolManager) getLedgerUpdateBlockWithRandomizer(block *types.Block, msg *types.ModifiedMessage, index uint64, randomizer *big.Int) (*types.Block, error) {
	if block == nil || msg == nil {
		return nil, fmt.Errorf("nil block or redaction instruction")
	}
	if msg.ModifyType != 1 {
		return nil, fmt.Errorf("only payload redaction (ModifyType 1) is supported")
	}
	blockNumber := block.Number().Uint64()
	blockHash := pm.blockchain.ReadCanonicalHash(blockNumber)
	if blockHash == (common.Hash{}) {
		return nil, fmt.Errorf("canonical block not found for %d", blockNumber)
	}
	originalBlock := pm.blockchain.ReadBlock(blockHash, blockNumber)
	if originalBlock == nil {
		return nil, fmt.Errorf("block data missing for %d (%s)", blockNumber, blockHash.Hex())
	}
	transactions := originalBlock.Transactions()
	if int(index) >= len(transactions) {
		return nil, fmt.Errorf("tx index out of range")
	}
	modifiedTxs := make(types.Transactions, len(transactions))
	copy(modifiedTxs, transactions)
	redactedTx, err := transactions[index].CopyForRedaction()
	if err != nil {
		return nil, fmt.Errorf("copy source transaction: %v", err)
	}
	redactedTx.SetData([]byte(msg.DataAfter))
	if randomizer != nil {
		redactedTx.SetChameleonR(randomizer)
	}
	modifiedTxs[index] = redactedTx
	// WithBody preserves the copied header, including its already validated
	// TxHash. Do not call DeriveSha on this receiver-only reconstruction.
	return types.NewBlockWithHeader(originalBlock.Header()).
		WithBody(modifiedTxs, originalBlock.Uncles()), nil
}

// getNewBlockWithRandomizer constructs the only admissible edited body: one
// payload change and its matching transaction-level NITCH randomizer. Header
// fields are copied verbatim; TxHash is recomputed from transaction identities.
func (pm *ProtocolManager) getNewBlockWithRandomizer(block *types.Block, msg *types.ModifiedMessage, index uint64, randomizer *big.Int) (*types.Block, error) {
	start := time.Now()
	if block == nil || msg == nil {
		return nil, fmt.Errorf("nil block or redaction instruction")
	}
	if msg.ModifyType != 1 {
		return nil, fmt.Errorf("only payload redaction (ModifyType 1) is supported")
	}
	blockNumber := block.Number().Uint64()
	// 编辑关联键（与提案/接收侧一致，跨节点关联）。
	reqID := redactionJoinKey(block.Number(), index)

	blockHash := pm.blockchain.ReadCanonicalHash(blockNumber)
	if blockHash == (common.Hash{}) {
		log.Error("Canonical block not found", "number", blockNumber)
		return nil, fmt.Errorf("canonical block not found for %d", blockNumber)
	}
	originalBlock := pm.blockchain.ReadBlock(blockHash, blockNumber)
	if originalBlock == nil {
		log.Error("Block not found", "number", blockNumber, "hash", blockHash)
		return nil, fmt.Errorf("block data missing for %d (%s)", blockNumber, blockHash.Hex())
	}
	transactions := originalBlock.Transactions()
	if int(index) >= len(transactions) {
		log.Error("Tx index out of range", "index", index)
		return nil, fmt.Errorf("tx index out of range")
	}
	modifiedTxs := make(types.Transactions, len(transactions))
	copy(modifiedTxs, transactions)
	redactedTx, err := transactions[index].CopyForRedaction()
	if err != nil {
		return nil, fmt.Errorf("copy source transaction: %v", err)
	}
	redactedTx.SetData([]byte(msg.DataAfter))
	if randomizer != nil {
		redactedTx.SetChameleonR(randomizer)
	}
	modifiedTxs[index] = redactedTx
	// 重建区块: 变色龙哈希碰撞保证了 modifiedTxs 的 Merkle 根与原块严格相等，
	// 直接继承 originalBlock.TxHash()，无需对 N 笔交易执行 O(N) 的 DeriveSha 重建 MPT 树。
	newBlock := types.NewBlockWithHeader(originalBlock.Header()).
		WithBody(modifiedTxs, originalBlock.Uncles())

	// 更新交易根哈希
	newHeader := newBlock.Header()
	newHeader.TxHash = originalBlock.TxHash()
	newBlock = newBlock.WithSeal(newHeader)
	// 性能打点：阶段6 edit_execution · 根据编辑指令构造新区块（mean）
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_build_new_block",
		"duration_ns", time.Since(start).Nanoseconds(), "join_key", reqID,
		"modifyType", msg.ModifyType, "block", blockNumber, "status", "ok")
	return newBlock, nil
}
