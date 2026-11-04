package rawdb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

// 普通用户和验证者。
// ReadValidators 从数据库中读取验证器列表
// todo 删了
func ReadValidators(db ethdb.Database) []common.Address {
	data, _ := db.Get([]byte("eth-validators"))
	if len(data) == 0 {
		return nil
	}

	var validators []common.Address
	if err := rlp.DecodeBytes(data, &validators); err != nil {
		return nil
	}
	return validators
}

// WriteValidators 将验证器列表写入数据库
func WriteValidators(db ethdb.Database, validators []common.Address) {
	data, err := rlp.EncodeToBytes(validators)
	if err != nil {
		return
	}
	db.Put([]byte("eth-validators"), data)
}
