package keyStore

import (
	"math/big"
)

// 注释
// 每个节点都会持有一个pkStore，用来存储公钥和私钥

type PublicKeyStore struct {
	//key: round(string)
	systemPublicKey  map[string]*big.Int
	systemPrivateKey map[string]*big.Int
	publicKeyMap     map[string]*big.Int
	privateKeyMap    map[string]*big.Int
}

func NewPublicKeyStore() *PublicKeyStore {
	return &PublicKeyStore{
		publicKeyMap:  make(map[string]*big.Int),
		privateKeyMap: make(map[string]*big.Int),
	}
}

func (pks *PublicKeyStore) SavePublicKey(publicKey *big.Int, round string) {
	pks.publicKeyMap[round] = publicKey
}

func (pks *PublicKeyStore) GetPublicKey(round string) (*big.Int, bool) {
	publicKey, exists := pks.publicKeyMap[round]
	return publicKey, exists
}

func (pks *PublicKeyStore) SavePrivateKey(privateKey *big.Int, round string) {
	pks.privateKeyMap[round] = privateKey
}
func (pks *PublicKeyStore) GetPrivateKey(round string) (*big.Int, bool) {
	privateKey, exists := pks.privateKeyMap[round]
	return privateKey, exists
}
