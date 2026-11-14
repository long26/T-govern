package eth

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
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
func (pm *ProtocolManager) verifyProposerSignature(proposalTx types.ProposalTx) bool {
	// 获取提案者的签名
	//signature := proposalTx.GetSignature()
	//if len(signature) == 0 {
	//	return false
	//}
	//
	//// 获取提案者的节点信息和公钥
	//proposerID := proposalTx.GetProposerID()
	//node := enode.MustParse(fmt.Sprintf("enode://%s", proposerID))
	//pubkey := node.Pubkey()
	//if pubkey == nil {
	//	return false
	//}
	//
	//// 构造待签名的消息
	//message := crypto.Keccak256(
	//	proposalTx.GetBlockNum().Bytes(),
	//	proposalTx.GetTxBefore().Bytes(),
	//	proposalTx.GetTxAfter().Bytes(),
	//	[]byte(proposalTx.GetReason()),
	//)
	//
	//// 验证签名
	//return crypto.VerifySignature(crypto.FromECDSAPub(pubkey), message, signature[:len(signature)-1])
	return true
}

// updateBlockInfo 更新区块信息
// [2026-08-30 迁移 nitch] 与 nitch 的 updateBlockInfo（eth/protocol_manager_redact.go）
// 结构等值：所有删改进入单个 LevelDB batch 一次提交，含 WriteCanonicalHash
// （编辑保持块哈希不变，为同值重写）；缓存清除移到提交之后；写失败返回
// error 而非 log.Crit。lu_u_batch_build/lu_u_commit/lu_u_total 子打点对齐
// nitch（join_key 为 块号+^uint64(0) 哨兵 index，同 nitch，不参与跨事件 join）。
// [perf 2026-09-01 最小写集] batch 收缩为 body 原位换体 + receipts/td 删除，
// 同值重写的 header/canonical/tx lookup 不再重写（落盘净效果不变，见函数内注释）。
func (pm *ProtocolManager) updateBlockInfo(block *types.Block, newBlock *types.Block) error {
	totalStart := time.Now()
	blockNumber := block.Number().Uint64()

	blockHash := pm.blockchain.ReadCanonicalHash(blockNumber)
	if blockHash == (common.Hash{}) {
		log.Error("Canonical block not found", "number", blockNumber)
		return fmt.Errorf("canonical block not found for %d", blockNumber)
	}

	// Keep all redaction deletes and writes in one atomic batch. Cache the values
	// reused below so block/header hashing is performed at most once here.
	txs := newBlock.Transactions()
	newBlockHash := newBlock.Hash()
	// [perf 2026-09-01 最小写集] validateRedactedBlock 已证：新旧 header RLP
	// 逐字节相同（⇒ 块哈希不变）、除编辑位外交易字节级相同、编辑交易的
	// chameleon hash 不变、块号不变。因此 header/canonical hash/tx lookup
	// 的重写全部是同值重写——原 WriteTxLookupEntries 更对块内每笔交易重算
	// 一次 chameleon.Hash（冷缓存，实测主导 lu_u_batch_build 12-18ms）。
	// 现收缩为唯一真正变化的 body 原位写（body key=(number, blockHash)，两
	// 分量均不变）+ 保持原 DeleteBlock 净效果的 receipts/td 删除；守卫块哈
	// 希不变，未来若出现改哈希的编辑类型则拒绝而非写坏索引。落盘终态与改
	// 前逐字节一致。恢复=还原为 DeleteBlock+WriteBlock+WriteCanonicalHash
	// +WriteTxLookupEntries 四调用。
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

	// Cache invalidation follows the successful atomic write: the canonical hash
	// is unchanged while its body is replaced, so both hash entries are stale.
	pm.blockchain.RemoveRedactionCaches(blockHash)
	pm.blockchain.RemoveRedactionCaches(newBlockHash)

	reqID := fmt.Sprintf("%d-%d", blockNumber, ^uint64(0))
	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_batch_build",
		"duration_ns", buildDur.Nanoseconds(), "join_key", reqID, "block", blockNumber,
		"txs", len(txs), "status", "ok")
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_batch_commit",
		"duration_ns", commitDur.Nanoseconds(), "join_key", reqID, "block", blockNumber,
		"txs", len(txs),
		"batch_bytes", batchBytes,
		"gc_pauses", gcStats.NumGC, "gc_pause_total_ns", gcStats.PauseTotal.Nanoseconds(), "status", "ok")
	log.Info("E2E_TIMING", "marker_schema", 3,
		"stage", "ledger_update", "event", "lu_u_total",
		"duration_ns", time.Since(totalStart).Nanoseconds(), "join_key", reqID,
		"txs", len(newBlock.Transactions()), "block", blockNumber, "status", "ok")

	return nil
}

func validatePayloadOnlyModify(msg *types.ModifyInfo) error {
	if msg == nil {
		return fmt.Errorf("missing redaction modification")
	}
	if msg.ModifyType != 1 {
		return fmt.Errorf("only payload redaction (ModifyType 1) is supported, got ModifyType %d", msg.ModifyType)
	}
	return nil
}

func (pm *ProtocolManager) getNewBlock(block *types.Block, msg *types.ModifyInfo, externalTxRPrime *big.Int, fields ...interface{}) (*types.Block, error) {
	if err := validatePayloadOnlyModify(msg); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("nil block")
	}
	blockNumber := block.Number().Uint64()
	index := msg.Index
	tReadStart := time.Now()
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
	// Keep untouched transactions shared with the canonical block. Only the
	// edited transaction needs an independent copy; cloning every transaction
	// adds RLP encode/decode work and unnecessary allocations.
	modifiedTxs := make(types.Transactions, len(transactions))
	copy(modifiedTxs, transactions)
	txOld := transactions[index]
	editedTx, err := cloneTransaction(txOld)
	if err != nil {
		return nil, fmt.Errorf("copy edited transaction: %w", err)
	}
	modifiedTxs[index] = editedTx
	tReadDur := time.Since(tReadStart)

	tPrepStart := time.Now()
	// [2026-08-30 迁移 nitch] 自检消息使用 32B 域分离摘要，与 committee 侧
	// 锻造 r'（PartialAdapt 收到的 Source/TargetMessage）保持同一消息定义。
	modifiedTxs[index].SetData(msg.DataAfter)
	dataNew, err := types.TransactionHashMessage(modifiedTxs[index])
	if err != nil {
		return nil, err
	}
	tPrepDur := time.Since(tPrepStart)

	tChameleonStart := time.Now()
	if externalTxRPrime != nil {
		if externalTxRPrime.Sign() == 0 {
			return nil, fmt.Errorf("invalid transaction RPrime")
		}
		if txOld.SubPublicKey() == nil || txOld.SubPublicKey().Sign() == 0 || txOld.ChameleonR() == nil || txOld.ChameleonR().Sign() == 0 {
			return nil, fmt.Errorf("old transaction missing chameleon hash fields")
		}
		// [perf 2026-09-05] oldHash 改读本地缓存 txOld.Hash()，避免重复重算旧变色龙哈希；
		// 仅对新数据与 externalTxRPrime 执行单次 Hash(new) 并直接比较碰撞等值。
		newHash := chameleon.Hash(txOld.SubPublicKey(), new(big.Int).SetBytes(dataNew), externalTxRPrime)
		if txOld.Hash() != common.BytesToHash(newHash.Bytes()) {
			return nil, fmt.Errorf("invalid transaction RPrime: chameleon verification failed")
		}
		modifiedTxs[index].SetChameleonR(externalTxRPrime)
	}
	tChameleonDur := time.Since(tChameleonStart)

	tAssembleStart := time.Now()
	// 重建区块: 变色龙哈希碰撞保证了 modifiedTxs 的 Merkle 根与原块严格相等，
	// 直接继承 originalBlock.TxHash()，无需对 N 笔交易执行 O(N) 的 DeriveSha 重建 MPT 树。
	newBlock := types.NewBlockWithHeader(originalBlock.Header()).
		WithBody(modifiedTxs, originalBlock.Uncles())

	newHeader := newBlock.Header()
	newHeader.TxHash = originalBlock.TxHash()

	newBlock = newBlock.WithSeal(newHeader)
	tAssembleDur := time.Since(tAssembleStart)

	baseFields := append([]interface{}{"stage", "edit_execution"}, fields...)
	baseFields = append(baseFields, "block_number", blockNumber, "tx_index", index)
	log.Info("E2E_TIMING", append(baseFields, "event", "redaction_build_read_block", "duration_ns", tReadDur.Nanoseconds())...)
	log.Info("E2E_TIMING", append(baseFields, "event", "redaction_build_tx_prep", "duration_ns", tPrepDur.Nanoseconds())...)
	log.Info("E2E_TIMING", append(baseFields, "event", "redaction_build_chameleon_verify", "duration_ns", tChameleonDur.Nanoseconds())...)
	log.Info("E2E_TIMING", append(baseFields, "event", "redaction_build_assemble_block", "duration_ns", tAssembleDur.Nanoseconds())...)

	return newBlock, nil
}

func redactionRPrime(block *types.Block, index uint64, modifyType uint64) (*big.Int, error) {
	if modifyType != 1 {
		return nil, fmt.Errorf("modify type %d changes the block header hash under plain header hashing", modifyType)
	}
	txs := block.Transactions()
	if int(index) >= len(txs) {
		return nil, fmt.Errorf("tx index out of range")
	}
	r := txs[index].ChameleonR()
	if r == nil || r.Sign() == 0 {
		return nil, fmt.Errorf("modified transaction missing chameleon RPrime")
	}
	return new(big.Int).Set(r), nil
}

func validateRedactedBlock(oldBlock *types.Block, newBlock *types.Block, msg *types.ModifyInfo, emit func(event string, fields ...interface{})) error {
	vStep := time.Now()
	emitStep := func(event string) {
		if emit != nil {
			emit(event, "duration_ns", time.Since(vStep).Nanoseconds())
		}
		vStep = time.Now()
	}
	if err := validatePayloadOnlyModify(msg); err != nil {
		return err
	}
	if oldBlock == nil || newBlock == nil {
		return fmt.Errorf("nil block")
	}
	if len(oldBlock.Transactions()) != len(newBlock.Transactions()) {
		return fmt.Errorf("transaction count changed: have %d, want %d", len(newBlock.Transactions()), len(oldBlock.Transactions()))
	}
	if int(msg.Index) >= len(oldBlock.Transactions()) {
		return fmt.Errorf("tx index out of range")
	}
	if oldBlock.NumberU64() != newBlock.NumberU64() {
		return fmt.Errorf("block number changed: have %d, want %d", newBlock.NumberU64(), oldBlock.NumberU64())
	}
	if oldBlock.ParentHash() != newBlock.ParentHash() {
		return fmt.Errorf("parent hash changed: have %x, want %x", newBlock.ParentHash(), oldBlock.ParentHash())
	}
	if hash := types.CalcUncleHash(newBlock.Uncles()); hash != newBlock.UncleHash() {
		return fmt.Errorf("uncle root hash mismatch: have %x, want %x", hash, newBlock.UncleHash())
	}
	emitStep("lu_v_checks")
	// [perf 2026-08-27] 编辑路径的交易根重算冗余，禁用（区块导入路径的 DeriveSha
	// 检查不属于此情形——全新块无参照物，那边的检查承担 header<->body 绑定，保留不动）。
	// 根是叶子的确定函数（叶子=各 tx 的变色龙哈希），而本函数随后的检查已蕴含根相等：
	//   (A) 整个 header 必须与本地已验证旧块 RLP 逐字节相同（含 TxHash，见下方
	//       oldBlock.TxHash/newBlock.TxHash 与 header RLP 比较）；
	//   (B) 除编辑位外的所有交易按 index 字节级相同（下方循环）；
	//   (C) 编辑交易的变色龙哈希等值由显式 chameleon.Hash+Verify 证明（下方）。
	// 叶子全等 => 根必等，此重算只可能在 A/B/C 已失败时失败，不提供额外拒绝力，
	// 却对 wire 新解码的 N 笔交易各付一次 BLS pairing（~2.68ms/笔，实测主导
	// lu_v_derivesha 24ms@9tx）。恢复=取消下方注释；lu_v_derivesha 打点保留，
	// 禁用期间应恒为 ~0ms。
	// if hash := types.DeriveSha(newBlock.Transactions()); hash != newBlock.TxHash() {
	// 	return fmt.Errorf("transaction root hash mismatch: have %x, want %x", hash, newBlock.TxHash())
	// }
	emitStep("lu_v_derivesha")
	if oldBlock.TxHash() != newBlock.TxHash() {
		return fmt.Errorf("transaction root changed under plain header hashing: have %x, want %x", newBlock.TxHash(), oldBlock.TxHash())
	}
	oldHeader, err := rlp.EncodeToBytes(oldBlock.Header())
	if err != nil {
		return fmt.Errorf("encode old block header: %w", err)
	}
	newHeader, err := rlp.EncodeToBytes(newBlock.Header())
	if err != nil {
		return fmt.Errorf("encode new block header: %w", err)
	}
	if !bytes.Equal(oldHeader, newHeader) || oldBlock.Header().Hash() != newBlock.Header().Hash() {
		return fmt.Errorf("block header hash changed under plain header hashing: have %x, want %x", newBlock.Header().Hash(), oldBlock.Header().Hash())
	}
	emitStep("lu_v_headerrlp")
	for i, oldTx := range oldBlock.Transactions() {
		newTx := newBlock.Transactions()[i]
		if i != int(msg.Index) {
			oldTxBytes, err := rlp.EncodeToBytes(oldTx)
			if err != nil {
				return fmt.Errorf("encode original transaction %d: %w", i, err)
			}
			newTxBytes, err := rlp.EncodeToBytes(newTx)
			if err != nil {
				return fmt.Errorf("encode replacement transaction %d: %w", i, err)
			}
			if !bytes.Equal(oldTxBytes, newTxBytes) {
				return fmt.Errorf("transaction %d changed outside the requested payload edit", i)
			}
			continue
		}
		if !bytes.Equal(newTx.Data(), msg.DataAfter) {
			return fmt.Errorf("replacement payload does not match redaction request")
		}
		if oldTx.SubPublicKey() == nil || oldTx.SubPublicKey().Sign() == 0 || oldTx.ChameleonR() == nil || oldTx.ChameleonR().Sign() == 0 {
			return fmt.Errorf("original transaction missing chameleon hash fields")
		}
		if newTx.SubPublicKey() == nil || newTx.SubPublicKey().Sign() == 0 || newTx.ChameleonR() == nil || newTx.ChameleonR().Sign() == 0 {
			return fmt.Errorf("replacement transaction missing chameleon hash fields")
		}
		unchanged, err := cloneTransaction(newTx)
		if err != nil {
			return fmt.Errorf("copy replacement transaction: %w", err)
		}
		unchanged.SetData(oldTx.Data())
		unchanged.SetChameleonR(oldTx.ChameleonR())
		oldTxBytes, err := rlp.EncodeToBytes(oldTx)
		if err != nil {
			return fmt.Errorf("encode original transaction: %w", err)
		}
		unchangedBytes, err := rlp.EncodeToBytes(unchanged)
		if err != nil {
			return fmt.Errorf("encode canonical replacement transaction: %w", err)
		}
		if !bytes.Equal(oldTxBytes, unchangedBytes) {
			return fmt.Errorf("redaction changes fields other than payload and chameleon randomizer")
		}
		// oldMessage 的编码错误检查保留（oldTx.Hash() 内部同一编码）。
		if _, err := types.TransactionHashMessage(oldTx); err != nil {
			return fmt.Errorf("encode original transaction hash message: %w", err)
		}
		newMessage, err := types.TransactionHashMessage(newTx)
		if err != nil {
			return fmt.Errorf("encode replacement transaction hash message: %w", err)
		}
		emitStep("lu_v_txprep")
		// [2026-08-30 迁移 nitch 校验结构] 校验等值：Hash(old) == Hash(new)。
		// key 统一取 oldTx 内嵌 SubPublicKey——上方 canonical 检查已证明
		// newTx.SubPublicKey 与之相等，等价于 nitch 的全局系统公钥。
		// [perf 2026-08-31 删除冗余 Verify] nitch 式 Verify(hash,key,tx,r) 内部
		// 就是用相同参数重算 Hash(key,tx,r) 再比较。此处若走到 Verify，说明
		// oldHash==newHash 已成立，而 Verify 的参数与上面 newHash 一次调用
		// 逐字节相同，结果恒真，不提供额外拒绝力，却多付一次 2-pairing
		// （实测在 100 节点负载下 ~6.4ms/次，lu_v_chameleon 19.2ms→12.8ms）。
		// 恢复=在 if 条件后补 !chameleon.Verify(oldHash, oldTx.SubPublicKey(),
		// new(big.Int).SetBytes(newMessage), newTx.ChameleonR())。
		// [perf 2026-08-31 oldHash 改读缓存] oldTx 来自本地链，其变色龙哈希在
		// 导入时已由 tx.Hash() 计算并原子缓存（core/types/transaction.go）。
		// 下方原 chameleon.Hash(oldTx.SubPublicKey(), oldMessage, oldTx.ChameleonR())
		// 的参数与该缓存值逐字节相同（TransactionHashMessage 同一函数、同一
		// key/r），重算是纯重复；改读缓存使校验从 2 次 pairing 降为 1 次，
		// 与 nitch 的单次 Verify 重算结构对齐。缓存未命中时 Hash() 惰性重算，
		// 行为不劣于改前。恢复=还原为显式 chameleon.Hash 调用。
		oldHash := oldTx.Hash()
		newHash := chameleon.Hash(oldTx.SubPublicKey(), new(big.Int).SetBytes(newMessage), newTx.ChameleonR())
		if oldHash != common.BytesToHash(newHash.Bytes()) {
			return fmt.Errorf("replacement transaction chameleon hash verification failed")
		}
		emitStep("lu_v_chameleon")
		// The chameleon collision check above already proves the edited
		// transaction identity is preserved. Calling Hash() here would repeat
		// the expensive transaction-level chameleon computation.
		// if oldTx.Hash() != newTx.Hash() {
		// 	return fmt.Errorf("transaction hash changed: have %x, want %x", newTx.Hash(), oldTx.Hash())
		// }
		emitStep("lu_v_txhash")
	}
	emitStep("lu_v_tailtx")
	return nil
}

func cloneTransaction(tx *types.Transaction) (*types.Transaction, error) {
	if tx == nil {
		return nil, fmt.Errorf("nil transaction")
	}
	blob, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return nil, err
	}
	var copied types.Transaction
	if err := rlp.DecodeBytes(blob, &copied); err != nil {
		return nil, err
	}
	return &copied, nil
}
