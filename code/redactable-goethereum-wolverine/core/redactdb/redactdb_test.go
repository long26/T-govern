package redactdb

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/stretchr/testify/assert"
)

// 创建测试用的RedactTx
func createTestRedactTx(blockNum int64, modifyTime uint64) *types.RedactTx {
	return &types.RedactTx{
		BlockNum:   big.NewInt(blockNum),
		ModifyTime: modifyTime,
		TxHash:     common.HexToHash("0x123"),
		Reason:     "test modification",
		Status:     types.StatusActive,
	}
}

// TestRedactDBBasicOperations 测试RedactDB的基本操作
func TestRedactDBBasicOperations(t *testing.T) {
	// 创建内存数据库和RedactDB实例
	memDB := memorydb.New()
	rdb := NewRedactDB(memDB)

	// 测试存储修改记录
	redactTx := createTestRedactTx(1, uint64(time.Now().Unix()))
	err := rdb.StoreModification(redactTx)
	assert.NoError(t, err, "存储修改记录应该成功")

	// 测试按区块号查询
	modifications, err := rdb.GetModificationsByBlock(big.NewInt(1))
	assert.NoError(t, err, "查询修改记录应该成功")
	assert.Equal(t, 1, len(modifications), "应该找到一条修改记录")
	assert.Equal(t, redactTx.TxHash, modifications[0].TxHash, "交易哈希应该匹配")

	// 测试按时间范围查询
	startTime := redactTx.ModifyTime - 3600
	endTime := redactTx.ModifyTime + 3600
	timeRangeModifications, err := rdb.GetModificationsByTimeRange(startTime, endTime)
	assert.NoError(t, err, "按时间范围查询应该成功")
	assert.Equal(t, 1, len(timeRangeModifications), "应该找到一条修改记录")
}

// TestModificationHistory 测试修改历史记录功能
func TestModificationHistory(t *testing.T) {
	memDB := memorydb.New()
	rdb := NewRedactDB(memDB)

	// 创建一个交易的多次修改记录
	txHash := common.HexToHash("0x123")
	modifyTime1 := uint64(time.Now().Unix())
	modifyTime2 := modifyTime1 + 3600

	// 添加第一次修改记录
	err := rdb.AddModificationHistory(txHash, modifyTime1, "first modification", nil, nil)
	assert.NoError(t, err, "添加第一次修改记录应该成功")

	// 添加第二次修改记录
	err = rdb.AddModificationHistory(txHash, modifyTime2, "second modification", nil, nil)
	assert.NoError(t, err, "添加第二次修改记录应该成功")

	// 获取修改历史
	history, err := rdb.GetModificationHistory(txHash)
	assert.NoError(t, err, "获取修改历史应该成功")
	assert.Equal(t, 2, len(history), "应该有两条修改记录")

	// 验证最新的修改记录
	latestMod, err := rdb.GetLatestModification(txHash)
	assert.NoError(t, err, "获取最新修改记录应该成功")
	assert.Equal(t, modifyTime2, latestMod.ModifyTime, "最新修改时间应该匹配")
}

// TestErrorHandling 测试错误处理
func TestErrorHandling(t *testing.T) {
	memDB := memorydb.New()
	rdb := NewRedactDB(memDB)

	// 测试查询不存在的交易
	nonExistentHash := common.HexToHash("0x456")
	history, err := rdb.GetModificationHistory(nonExistentHash)
	assert.NoError(t, err, "查询不存在的交易不应该返回错误")
	assert.Equal(t, 0, len(history), "不存在的交易应该返回空历史记录")

	// 测试查询不存在的区块
	modifications, err := rdb.GetModificationsByBlock(big.NewInt(999))
	assert.NoError(t, err, "查询不存在的区块不应该返回错误")
	assert.Equal(t, 0, len(modifications), "不存在的区块应该返回空修改记录")
}