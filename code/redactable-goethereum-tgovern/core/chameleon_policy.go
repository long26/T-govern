package core

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/chameleon/txstamp"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	ErrMissingChameleonFields    = txstamp.ErrMissingChameleonFields
	ErrInvalidChameleonFields    = txstamp.ErrInvalidChameleonFields
	ErrInvalidChameleonPublicKey = txstamp.ErrInvalidChameleonPublicKey
)

// ChameleonKeyProvider exposes the chain's canonical pkCH to core without
// making core depend on contract bindings or RPC services.
type ChameleonKeyProvider interface {
	CurrentPkCH() (*big.Int, bool)
	PolicyPkCHForBlock(number uint64) (*big.Int, bool)
}

type chameleonPolicy struct {
	mu       sync.RWMutex
	provider ChameleonKeyProvider
}

func (p *chameleonPolicy) SetChameleonKeyProvider(provider ChameleonKeyProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provider = provider
}

func (p *chameleonPolicy) currentPkCH() (*big.Int, bool) {
	p.mu.RLock()
	provider := p.provider
	p.mu.RUnlock()
	if provider == nil {
		return nil, false
	}
	return provider.CurrentPkCH()
}

func (p *chameleonPolicy) pkCHForBlock(number uint64) (*big.Int, bool) {
	p.mu.RLock()
	provider := p.provider
	p.mu.RUnlock()
	if provider == nil {
		return nil, false
	}
	return provider.PolicyPkCHForBlock(number)
}

func validateChameleonFields(tx *types.Transaction, pkCH *big.Int, enforce bool) error {
	return txstamp.ValidateForPolicy(tx, enforce, pkCH)
}

func validateBlockChameleonFields(block *types.Block, pkCH *big.Int, enforce bool) error {
	for i, tx := range block.Transactions() {
		if err := validateChameleonFields(tx, pkCH, enforce); err != nil {
			return fmt.Errorf("transaction %d: %w", i, err)
		}
	}
	return nil
}
