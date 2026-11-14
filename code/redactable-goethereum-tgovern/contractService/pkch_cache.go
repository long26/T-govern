package contractService

import (
	"fmt"
	"math"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
)

type pkCHState uint8

const (
	pkCHUnknown pkCHState = iota
	pkCHNotReady
	pkCHReady
)

type PkCHProvider struct {
	service pkCHReader

	mu              sync.RWMutex
	state           pkCHState
	pkCH            *big.Int
	activationBlock uint64
}

type pkCHReader interface {
	GetPkCH() ([]byte, error)
	GetPkCHHash() ([32]byte, error)
}

func NewPkCHProvider(service pkCHReader) *PkCHProvider {
	return &PkCHProvider{
		service:         service,
		state:           pkCHUnknown,
		activationBlock: math.MaxUint64,
	}
}

func Provider() (*PkCHProvider, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	if serviceInstance.pkCHProvider == nil {
		serviceInstance.pkCHProvider = NewPkCHProvider(serviceInstance)
	}
	return serviceInstance.pkCHProvider, nil
}

func (p *PkCHProvider) CurrentPkCH() (*big.Int, bool) {
	pkCH, ok, _ := p.ensurePkCH()
	return pkCH, ok
}

func (p *PkCHProvider) PolicyPkCHForBlock(number uint64) (*big.Int, bool) {
	p.mu.RLock()
	state := p.state
	pkCH := cloneBigInt(p.pkCH)
	activationBlock := p.activationBlock
	p.mu.RUnlock()
	if state != pkCHReady || pkCH == nil || activationBlock == math.MaxUint64 || number <= activationBlock {
		return nil, false
	}
	return pkCH, true
}

// ActivationBlock reports the first locally observed block containing pkCH.
func (p *PkCHProvider) ActivationBlock() (uint64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activationBlock, p.state == pkCHReady && p.activationBlock != math.MaxUint64
}

// RestoreActivationBlock restores local policy metadata only after checking the chain key.
func (p *PkCHProvider) RestoreActivationBlock(number uint64, expectedKey *big.Int) error {
	key, ready, err := p.ensurePkCH()
	if err != nil {
		return err
	}
	if !ready || expectedKey == nil || key.Cmp(expectedKey) != 0 {
		return fmt.Errorf("persisted pkCH does not match the canonical chain key")
	}
	p.mu.Lock()
	if number < p.activationBlock {
		p.activationBlock = number
	}
	p.mu.Unlock()
	return nil
}

func (p *PkCHProvider) ObservePkCHAtBlock(number uint64) error {
	_, ok, err := p.ensurePkCH()
	if err != nil || !ok {
		return err
	}
	p.mu.Lock()
	if number < p.activationBlock {
		p.activationBlock = number
	}
	p.mu.Unlock()
	return nil
}

func (p *PkCHProvider) ensurePkCH() (*big.Int, bool, error) {
	p.mu.RLock()
	if p.state == pkCHReady {
		pkCH := cloneBigInt(p.pkCH)
		p.mu.RUnlock()
		return pkCH, true, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == pkCHReady {
		return cloneBigInt(p.pkCH), true, nil
	}
	if p.service == nil {
		return nil, false, fmt.Errorf("contract service not initialized")
	}
	raw, err := p.service.GetPkCH()
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 {
		p.state = pkCHNotReady
		return nil, false, nil
	}
	committedHash, err := p.service.GetPkCHHash()
	if err != nil {
		return nil, false, err
	}
	if crypto.Keccak256Hash(raw) != committedHash {
		p.state = pkCHUnknown
		return nil, false, fmt.Errorf("pkCH hash mismatch")
	}
	p.pkCH = new(big.Int).SetBytes(raw)
	p.state = pkCHReady
	return cloneBigInt(p.pkCH), true, nil
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
