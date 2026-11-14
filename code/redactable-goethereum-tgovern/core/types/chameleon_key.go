package types

import (
	"errors"
	"math/big"
	"sync"
)

var ErrChameleonKeyNotReady = errors.New("canonical chameleon key is not ready")

type chameleonKeySource struct {
	resolve  func() (*big.Int, bool)
	previous *chameleonKeySource
	released bool
}

var chameleonKeySources = struct {
	sync.RWMutex
	byChain map[string]*chameleonKeySource
}{byChain: make(map[string]*chameleonKeySource)}

// RegisterChameleonKeySource connects decoded transactions to a trusted chain key.
// The returned release function removes this registration without disturbing a newer one.
func RegisterChameleonKeySource(chainID *big.Int, resolve func() (*big.Int, bool)) func() {
	if chainID == nil || resolve == nil {
		return func() {}
	}
	id := chainID.String()
	source := &chameleonKeySource{resolve: resolve}
	chameleonKeySources.Lock()
	source.previous = chameleonKeySources.byChain[id]
	chameleonKeySources.byChain[id] = source
	chameleonKeySources.Unlock()
	return func() {
		chameleonKeySources.Lock()
		defer chameleonKeySources.Unlock()
		source.released = true
		if chameleonKeySources.byChain[id] == source {
			previous := source.previous
			for previous != nil && previous.released {
				previous = previous.previous
			}
			if previous == nil {
				delete(chameleonKeySources.byChain, id)
			} else {
				chameleonKeySources.byChain[id] = previous
			}
		}
	}
}

func (tx *Transaction) chameleonPublicKey() (*big.Int, bool) {
	// Explicit keys are local state only; wire and JSON decoders never populate them.
	if key := tx.data.SubPublicKey; key != nil && key.Sign() > 0 {
		return new(big.Int).Set(key), true
	}
	if tx.data.ChameleonR == nil || tx.data.ChameleonR.Sign() == 0 {
		return nil, false
	}
	chameleonKeySources.RLock()
	id := tx.ChainId()
	source := chameleonKeySources.byChain[id.String()]
	if source == nil && id.Sign() == 0 && len(chameleonKeySources.byChain) == 1 {
		// An unprotected transaction has no chain ID; resolve only an unambiguous host.
		for _, only := range chameleonKeySources.byChain {
			source = only
		}
	}
	chameleonKeySources.RUnlock()
	if source == nil {
		return nil, false
	}
	// Never call the provider under the registry lock: it can read chain state.
	key, ready := source.resolve()
	if !ready || key == nil || key.Sign() <= 0 {
		return nil, false
	}
	return new(big.Int).Set(key), true
}
