package eth

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

var chameleonKeyBindingKey = []byte("tgovern-canonical-pkch-v1")

type chameleonKeyBinding struct {
	Genesis         common.Hash
	ChainID         *big.Int
	Key             []byte
	ActivationBlock uint64
}

func loadChameleonKeyBinding(db ethdb.KeyValueReader, genesis common.Hash, chainID *big.Int) (*chameleonKeyBinding, error) {
	present, err := db.Has(chameleonKeyBindingKey)
	if err != nil || !present {
		return nil, err
	}
	encoded, err := db.Get(chameleonKeyBindingKey)
	if err != nil {
		return nil, err
	}
	var binding chameleonKeyBinding
	if err := rlp.DecodeBytes(encoded, &binding); err != nil {
		return nil, fmt.Errorf("decode canonical pkCH binding: %w", err)
	}
	if binding.Genesis != genesis || chainID == nil || binding.ChainID == nil || binding.ChainID.Cmp(chainID) != 0 ||
		len(binding.Key) == 0 || len(binding.Key) > 192 || new(big.Int).SetBytes(binding.Key).Sign() == 0 {
		return nil, fmt.Errorf("invalid canonical pkCH binding for chain")
	}
	return &binding, nil
}

func (binding *chameleonKeyBinding) CurrentPkCH() (*big.Int, bool) {
	if binding == nil || len(binding.Key) == 0 {
		return nil, false
	}
	return new(big.Int).SetBytes(binding.Key), true
}

func (binding *chameleonKeyBinding) PolicyPkCHForBlock(number uint64) (*big.Int, bool) {
	if binding == nil || number <= binding.ActivationBlock {
		return nil, false
	}
	return binding.CurrentPkCH()
}

func registerPersistedChameleonKey(binding *chameleonKeyBinding) func() {
	key := new(big.Int).SetBytes(binding.Key)
	return types.RegisterChameleonKeySource(binding.ChainID, func() (*big.Int, bool) {
		return new(big.Int).Set(key), true
	})
}

func (s *Ethereum) persistChameleonKeyBinding() error {
	key, ready := s.chameleonProvider.CurrentPkCH()
	activation, observed := s.chameleonProvider.ActivationBlock()
	if !ready || !observed {
		return nil
	}
	binding := chameleonKeyBinding{
		Genesis: s.blockchain.Genesis().Hash(), ChainID: s.blockchain.Config().ChainID,
		Key: key.Bytes(), ActivationBlock: activation,
	}
	encoded, err := rlp.EncodeToBytes(binding)
	if err != nil {
		return err
	}
	if previous, err := s.chainDb.Get(chameleonKeyBindingKey); err == nil && bytes.Equal(previous, encoded) {
		return nil
	}
	if err := s.chainDb.Put(chameleonKeyBindingKey, encoded); err != nil {
		return err
	}
	s.chameleonBinding = &binding
	return nil
}
