package txstamp

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	ErrMissingChameleonFields    = errors.New("missing chameleon hash fields")
	ErrInvalidChameleonFields    = errors.New("invalid chameleon hash fields")
	ErrInvalidChameleonPublicKey = errors.New("invalid chameleon public key")
)

type PkCHProvider interface {
	CurrentPkCH() (*big.Int, bool)
}

func StampIfReady(tx *types.Transaction, provider PkCHProvider) error {
	if tx == nil || provider == nil {
		return nil
	}
	if hasAnyChameleonField(tx) {
		return nil
	}
	pkCH, ready := provider.CurrentPkCH()
	if !ready || pkCH == nil || pkCH.Sign() == 0 {
		return nil
	}
	r, err := chameleon.GenerateR()
	if err != nil {
		return err
	}
	tx.SetChameleonHashFields(pkCH, r)
	return nil
}

func ValidateForPolicy(tx *types.Transaction, pkCHReady bool, pkCH *big.Int) error {
	hasPubKey, hasR := chameleonFieldPresence(tx)
	if hasPubKey && !hasR {
		return fmt.Errorf("%w: local public key without ChameleonR", ErrInvalidChameleonFields)
	}
	if !pkCHReady {
		if hasPubKey || hasR {
			return fmt.Errorf("%w: pkCH not ready", ErrInvalidChameleonPublicKey)
		}
		return nil
	}
	// pkCH is ready: chameleon fields are OPTIONAL, not mandatory. A tx without
	// them is simply non-editable (plain RLP hash) and is valid in any block.
	// This lets pre-activation-created system/registration txs (RLP by design,
	// since pkCH did not exist when they were signed) be mined into post-activation
	// blocks instead of poisoning sync. Only when chameleon fields ARE present
	// must they bind to the active pkCH (so redactions stay sound and pkCH cannot
	// be forged). A mixed block (editable chameleon + immutable RLP txs) preserves
	// its hash: redaction collides on the chameleon tx's hash, RLP txs are untouched.
	if !hasPubKey && !hasR {
		return nil
	}
	if pkCH == nil || pkCH.Sign() == 0 {
		return fmt.Errorf("%w: pkCH not ready", ErrInvalidChameleonPublicKey)
	}
	if localKey := tx.SubPublicKey(); localKey != nil {
		if localKey.Cmp(pkCH) != 0 {
			return ErrInvalidChameleonPublicKey
		}
	} else {
		// Bind an unresolved key only; rebinding the same key discards the cached BLS hash.
		tx.SetChameleonHashFields(pkCH, tx.ChameleonR())
	}
	return nil
}

func hasAnyChameleonField(tx *types.Transaction) bool {
	hasPubKey, hasR := chameleonFieldPresence(tx)
	return hasPubKey || hasR
}

func chameleonFieldPresence(tx *types.Transaction) (bool, bool) {
	if tx == nil {
		return false, false
	}
	subPubKey := tx.SubPublicKey()
	chameleonR := tx.ChameleonR()
	return subPubKey != nil && subPubKey.Sign() != 0, chameleonR != nil && chameleonR.Sign() != 0
}
