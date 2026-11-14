package txstamp

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

type testProvider struct {
	pkCH  *big.Int
	ready bool
}

func (p testProvider) CurrentPkCH() (*big.Int, bool) {
	if !p.ready || p.pkCH == nil {
		return nil, false
	}
	return new(big.Int).Set(p.pkCH), true
}

func TestStampIfReady(t *testing.T) {
	pkCH := big.NewInt(29)
	tx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	if err := StampIfReady(tx, testProvider{pkCH: pkCH, ready: true}); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if tx.SubPublicKey() == nil || tx.SubPublicKey().Cmp(pkCH) != 0 {
		t.Fatalf("SubPublicKey = %v, want %v", tx.SubPublicKey(), pkCH)
	}
	if tx.ChameleonR() == nil || tx.ChameleonR().Sign() == 0 {
		t.Fatalf("ChameleonR was not generated")
	}
	encodedR, err := chameleon.NITCHBLSBigIntToR(tx.ChameleonR())
	if err != nil {
		t.Fatalf("ChameleonR is not a valid compressed G1 point: %v", err)
	}
	if len(encodedR) != 48 {
		t.Fatalf("compressed ChameleonR length = %d, want 48", len(encodedR))
	}
}

func TestValidateForPolicy(t *testing.T) {
	pkCH := big.NewInt(31)
	tx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	if err := ValidateForPolicy(tx, false, nil); err != nil {
		t.Fatalf("legacy tx before pkCH ready rejected: %v", err)
	}
	if err := ValidateForPolicy(tx, true, pkCH); err != nil {
		t.Fatalf("immutable transaction rejected after pkCH activation: %v", err)
	}

	tx.SetChameleonHashFields(pkCH, big.NewInt(37))
	if err := ValidateForPolicy(tx, true, pkCH); err != nil {
		t.Fatalf("canonical chameleon tx rejected: %v", err)
	}
	if err := ValidateForPolicy(tx, false, nil); !errors.Is(err, ErrInvalidChameleonPublicKey) {
		t.Fatalf("pre-pkCH chameleon tx error = %v, want ErrInvalidChameleonPublicKey", err)
	}
}
