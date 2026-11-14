package chameleon

import (
	"math/big"
	"testing"
)

func TestComputeTransactionRIsDeterministicAndChainBound(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	digest := []byte("signed-transaction-without-chameleon-r")

	r1, err := ComputeTransactionR(keys[0], big.NewInt(1337), digest)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ComputeTransactionR(keys[1], big.NewInt(1337), digest)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Cmp(r2) != 0 {
		t.Fatalf("deterministic transaction r mismatch: %x != %x", r1, r2)
	}
	rOtherChain, err := ComputeTransactionR(keys[0], big.NewInt(1338), digest)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Cmp(rOtherChain) == 0 {
		t.Fatal("transaction r was reused across chains")
	}
}
