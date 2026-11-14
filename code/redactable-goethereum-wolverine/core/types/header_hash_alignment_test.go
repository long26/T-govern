package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
)

// The header identity must never depend on DistKG/key-manager state. This
// guards the protocol boundary shared with tee-connect.
func TestHeaderHashIsAlwaysRLPKeccak(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	chameleon.SetSystemNITCHKeyFromKey(keys[0])

	header := &Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1),
		GasLimit:   8_000_000,
		Extra:      []byte("header-hash-alignment"),
	}
	want := rlpHash(header)
	if got := header.Hash(); got != want {
		t.Fatalf("header hash depends on NITCH context: have %s want %s", got, want)
	}
}
