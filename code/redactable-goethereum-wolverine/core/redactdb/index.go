package redactdb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"sort"
	"sync"
)

// 接口
// ModificationIndex 用于维护修改记录的索引
type ModificationIndex struct {
	mu sync.RWMutex

	// 按区块号索引修改记录
	blockIndex map[string][]*types.RedactTx

	// 按交易哈希索引修改记录
	txIndex map[common.Hash][]*types.RedactTx

	// 按时间戳索引修改记录
	timeIndex []timeIndexEntry
}

type timeIndexEntry struct {
	timestamp uint64
	record    *types.RedactTx
}

// NewModificationIndex 创建新的修改记录索引
func NewModificationIndex() *ModificationIndex {
	return &ModificationIndex{
		blockIndex: make(map[string][]*types.RedactTx),
		txIndex:    make(map[common.Hash][]*types.RedactTx),
		timeIndex:  make([]timeIndexEntry, 0),
	}
}

// AddModification 添加修改记录到索引
func (mi *ModificationIndex) AddModification(redactTx *types.RedactTx) {
	mi.mu.Lock()
	defer mi.mu.Unlock()

	// 更新区块索引
	blockKey := redactTx.BlockNum.String()
	mi.blockIndex[blockKey] = append(mi.blockIndex[blockKey], redactTx)

	// 更新交易索引
	// mi.txIndex[common.BigToHash(redactTx.TxBefore)] = append(mi.txIndex[common.BigToHash(redactTx.TxBefore)], redactTx)

	// 更新时间索引
	mi.timeIndex = append(mi.timeIndex, timeIndexEntry{
		timestamp: uint64(redactTx.CreateTime.Unix()),
		record:    redactTx,
	})

	// 按时间戳排序
	sort.Slice(mi.timeIndex, func(i, j int) bool {
		return mi.timeIndex[i].timestamp < mi.timeIndex[j].timestamp
	})
}

// GetModificationsByBlock 根据区块号查询修改记录
func (mi *ModificationIndex) GetModificationsByBlock(blockNum *big.Int) []*types.RedactTx {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

	return mi.blockIndex[blockNum.String()]
}

// GetModificationsByTx 根据交易哈希查询修改记录
func (mi *ModificationIndex) GetModificationsByTx(txHash common.Hash) []*types.RedactTx {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

	return mi.txIndex[txHash]
}

// GetModificationsByTimeRange 根据时间范围查询修改记录
func (mi *ModificationIndex) GetModificationsByTimeRange(startTime, endTime uint64) []*types.RedactTx {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

	var result []*types.RedactTx

	// 二分查找开始时间
	startIdx := sort.Search(len(mi.timeIndex), func(i int) bool {
		return mi.timeIndex[i].timestamp >= startTime
	})

	// 收集时间范围内的记录
	for i := startIdx; i < len(mi.timeIndex) && mi.timeIndex[i].timestamp <= endTime; i++ {
		result = append(result, mi.timeIndex[i].record)
	}

	return result
}
