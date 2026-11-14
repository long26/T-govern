package redactdb

import (
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
)

// 接口
// RedactDB 管理交易修改记录的数据库操作
type RedactDB struct {
	db    ethdb.Database
	index *ModificationIndex
}

// NewRedactDB 创建一个新的RedactDB实例
func NewRedactDB(db ethdb.Database) *RedactDB {
	return &RedactDB{
		db:    db,
		index: NewModificationIndex(),
	}
}

// StoreModification 存储一条修改记录
func (rdb *RedactDB) StoreModification(redactTx *types.RedactTx) error {
	// 将RedactTx编码为JSON
	data, err := json.Marshal(redactTx)
	if err != nil {
		return err
	}

	// 使用区块号和时间戳作为键
	key := append(redactTx.BlockNum.Bytes(), big.NewInt(redactTx.CreateTime.Unix()).Bytes()...)

	// 存储到数据库
	err = rdb.db.Put(key, data)
	if err != nil {
		log.Error("Failed to store modification record", "err", err)
		return err
	}

	// 更新索引
	rdb.index.AddModification(redactTx)

	return nil
}

// RecordModification 记录一条修改记录（StoreModification的别名）
func (rdb *RedactDB) RecordModification(redactTx *types.RedactTx) error {
	return rdb.StoreModification(redactTx)
}

// GetModificationsByBlock 根据区块号查询修改记录
func (rdb *RedactDB) GetModificationsByBlock(blockNum *big.Int) ([]*types.RedactTx, error) {
	return rdb.index.GetModificationsByBlock(blockNum), nil
}

// GetModificationsByTimeRange 根据时间范围查询修改记录
func (rdb *RedactDB) GetModificationsByTimeRange(startTime, endTime uint64) ([]*types.RedactTx, error) {
	return rdb.index.GetModificationsByTimeRange(startTime, endTime), nil
}

// GetModificationHistory 获取指定交易的修改历史记录
func (rdb *RedactDB) GetModificationHistory(txHash common.Hash) ([]*types.RedactTx, error) {
	return rdb.index.GetModificationsByTx(txHash), nil
}

// AddModificationHistory 添加一条修改历史记录
func (rdb *RedactDB) AddModificationHistory(txHash common.Hash, modifyTime uint64, reason string, oldTx, newTx *big.Int) error {
	// 创建新的修改记录
	redactTx := &types.RedactTx{}
	//redactTx.SetModifyTime(modifyTime)
	//redactTx.TxBefore = oldTx
	//redactTx.TxAfter = newTx

	// 存储修改记录
	return rdb.StoreModification(redactTx)
}

// GetLatestModification 获取指定交易的最新修改记录
func (rdb *RedactDB) GetLatestModification(txHash common.Hash) (*types.RedactTx, error) {
	history, err := rdb.GetModificationHistory(txHash)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return nil, nil
	}

	// 返回最新的修改记录（假设按时间戳排序）
	return history[len(history)-1], nil
}
