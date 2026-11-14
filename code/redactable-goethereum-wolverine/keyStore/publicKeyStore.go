package keyStore

import (
	"math/big"
)

// 注释
// 每个节点都会持有一个pkStore

type PublicKeyStore struct {
	//key: round(string)
	inverseShareMap map[string]*big.Int // 非交互式阈值锻造用 inv_i = sk^{-1}/n
}

func NewPublicKeyStore() *PublicKeyStore {
	return &PublicKeyStore{
		inverseShareMap: make(map[string]*big.Int),
	}
}

// SaveInverseShare 保存当前轮次的逆元份额 inv_i（用于非交互式阈值锻造）
func (pks *PublicKeyStore) SaveInverseShare(invShare *big.Int, round string) {
	if pks.inverseShareMap == nil {
		pks.inverseShareMap = make(map[string]*big.Int)
	}
	pks.inverseShareMap[round] = invShare
}

// GetInverseShare 获取当前轮次的逆元份额
func (pks *PublicKeyStore) GetInverseShare(round string) (*big.Int, bool) {
	if pks.inverseShareMap == nil {
		return nil, false
	}
	inv, exists := pks.inverseShareMap[round]
	return inv, exists
}
