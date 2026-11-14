// Package eth - Redaction Policy（编辑策略）实现
//
// 基于论文 "Li 等 - 2023 - Wolverine: A Scalable and Transaction-Consistent
// Redactable Permissionless Blockchain" 中的 Definition 3.6（Redaction policy）。
//
// RP(C, RI) = 1 当且仅当执行编辑指令 RI 后，链 C* 的交易满足验证谓词 V。
// 即：编辑不得破坏交易有效性，不得导致双重支付等不一致。
package eth

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// ValidateRedactionInstruction 验证编辑指令 RI 是否满足 Redaction Policy。
//
// 论文要求：RP(C, RI) = 1 当且仅当 V(xC*) = 1，其中 C* = Redact(C, RI)。
// 本实现进行以下检查：
//  1. 区块存在且可访问
//  2. 交易索引有效
//  3. 修改类型合法（1=payload, 2=delete, 3=extra, 4=amount）
//  4. 修改后区块结构合法（不破坏链一致性）
//
// 参数：
//   - chain: 区块链引用
//   - msg: 修改指令（ModifiedMessage，含 BlockNum、Index、ModifyType 等）
//
// 返回：nil 表示通过验证，否则返回错误原因。
func ValidateRedactionInstruction(chain *core.BlockChain, msg *types.ModifiedMessage) error {
	if chain == nil || msg == nil {
		return fmt.Errorf("redaction policy: nil parameter")
	}
	if msg.ModifyType != 1 {
		return fmt.Errorf("redaction policy: only payload modification (ModifyType 1) is allowed")
	}

	blockNum, err := strconv.ParseUint(msg.BlockNum, 10, 64)
	if err != nil {
		return fmt.Errorf("redaction policy: invalid BlockNum %s", msg.BlockNum)
	}

	block := chain.GetBlockByNumber(blockNum)
	if block == nil {
		return fmt.Errorf("redaction policy: block %d not found", blockNum)
	}

	txs := block.Transactions()
	if int(msg.Index) >= len(txs) {
		return fmt.Errorf("redaction policy: tx index %d out of range (block has %d txs)", msg.Index, len(txs))
	}
	if msg.DataBefore != "" && !bytes.Equal(txs[msg.Index].Data(), []byte(msg.DataBefore)) {
		return fmt.Errorf("redaction policy: payload before-image does not match transaction")
	}

	return nil
}

// ValidateModifiedBlockConsistency 验证修改后的区块与链的一致性。
//
// 论文要求：红action 后链 C* 须满足交易验证谓词 V。
// 本实现检查：
//  1. 新区块哈希输入（含新 r'）与变色龙哈希验证一致（由 handler 完成）
//  2. 区块号、父哈希等结构字段保持不变
//
// 注：完整的交易一致性需在 updateBlockInfo 后重新验证链，
// 此处仅做修改指令层面的预检查。
func ValidateModifiedBlockConsistency(chain *core.BlockChain, oldBlock, newBlock *types.Block, editedIndex uint64) error {
	start := time.Now()
	if chain == nil || oldBlock == nil || newBlock == nil {
		return fmt.Errorf("consistency check: nil parameter")
	}
	if oldBlock.NumberU64() != newBlock.NumberU64() {
		return fmt.Errorf("consistency check: block number mismatch")
	}
	if oldBlock.ParentHash() != newBlock.ParentHash() {
		return fmt.Errorf("consistency check: parent hash changed (forbidden)")
	}
	if successor := chain.GetBlockByNumber(oldBlock.NumberU64() + 1); successor != nil && successor.ParentHash() != oldBlock.Hash() {
		return fmt.Errorf("consistency check: successor parent hash does not reference source block")
	}
	oldHeader, err := rlp.EncodeToBytes(oldBlock.Header())
	if err != nil {
		return fmt.Errorf("consistency check: encode old header: %w", err)
	}
	newHeader, err := rlp.EncodeToBytes(newBlock.Header())
	if err != nil {
		return fmt.Errorf("consistency check: encode new header: %w", err)
	}
	if !bytes.Equal(oldHeader, newHeader) {
		return fmt.Errorf("consistency check: header RLP changed")
	}
	if oldBlock.TxHash() != newBlock.TxHash() {
		return fmt.Errorf("consistency check: transaction root changed")
	}
	if oldBlock.Hash() != newBlock.Hash() {
		return fmt.Errorf("consistency check: block hash changed")
	}
	if len(oldBlock.Transactions()) != len(newBlock.Transactions()) {
		return fmt.Errorf("consistency check: transaction count changed")
	}
	for index, oldTx := range oldBlock.Transactions() {
		// The edited transaction is validated by the payload-only and
		// chameleon checks in the receiver path. Hashing it here would repeat
		// the expensive cryptographic work. Every other transaction must be
		// byte-identical, so RLP comparison is the stronger and cheaper check.
		if uint64(index) == editedIndex {
			continue
		}
		oldBytes, err := rlp.EncodeToBytes(oldTx)
		if err != nil {
			return fmt.Errorf("consistency check: encode source tx %d: %w", index, err)
		}
		newBytes, err := rlp.EncodeToBytes(newBlock.Transactions()[index])
		if err != nil {
			return fmt.Errorf("consistency check: encode edited tx %d: %w", index, err)
		}
		if !bytes.Equal(oldBytes, newBytes) {
			return fmt.Errorf("consistency check: transaction %d changed", index)
		}
	}
	// 性能打点：阶段6 edit_execution · 外部一致性校验（mean）
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_verify_external",
		"duration_ns", time.Since(start).Nanoseconds(), "block", newBlock.NumberU64(), "status", "ok")
	return nil
}

// ValidatePayloadOnlyBlockEdit validates the transaction-body part that is not
// visible in a header: one payload replacement, an adapted r', and no other
// transaction mutation.
func ValidatePayloadOnlyBlockEdit(oldBlock, newBlock *types.Block, index uint64, payloadAfter []byte) error {
	if oldBlock == nil || newBlock == nil || int(index) >= len(oldBlock.Transactions()) || int(index) >= len(newBlock.Transactions()) {
		return fmt.Errorf("payload-only check: invalid block or transaction index")
	}
	for i, oldTx := range oldBlock.Transactions() {
		newTx := newBlock.Transactions()[i]
		if i == int(index) {
			if !oldTx.PayloadOnlyEquivalent(newTx) {
				return fmt.Errorf("payload-only check: transaction %d changed outside payload/randomizer", i)
			}
			if !bytes.Equal(newTx.Data(), payloadAfter) {
				return fmt.Errorf("payload-only check: transaction %d payload mismatch", i)
			}
			continue
		}
		oldEncoded, err := rlp.EncodeToBytes(oldTx)
		if err != nil {
			return fmt.Errorf("payload-only check: encode source transaction %d: %w", i, err)
		}
		newEncoded, err := rlp.EncodeToBytes(newTx)
		if err != nil {
			return fmt.Errorf("payload-only check: encode edited transaction %d: %w", i, err)
		}
		if !bytes.Equal(oldEncoded, newEncoded) {
			return fmt.Errorf("payload-only check: non-target transaction %d changed", i)
		}
	}
	return nil
}
