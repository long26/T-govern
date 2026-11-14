package chameleon

import (
	"math/big"
	"testing"
)

// BenchmarkLegacyHashBLSPath times the legacy Hash entry point (cached-context
// path) using committee-shaped inputs, i.e. what validateRedactedBlock and
// DeriveSha pay per transaction.
func BenchmarkLegacyHashBLSPath(b *testing.B) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	keys, err := NITCHBLSSimulatedDistKG(pp, 1, 2)
	if err != nil {
		b.Fatalf("simulated dkg: %v", err)
	}
	message := []byte("bench-compatibility-message")
	r, err := NITCHBLSComputeR(keys[0], []byte("bench-r"))
	if err != nil {
		b.Fatalf("compute r: %v", err)
	}
	pkInt, err := NITCHBLSPublicKeyToBigInt(keys[0])
	if err != nil {
		b.Fatalf("pk to int: %v", err)
	}
	rInt, err := NITCHBLSRToBigInt(r)
	if err != nil {
		b.Fatalf("r to int: %v", err)
	}
	msgInt := new(big.Int).SetBytes(message)
	if _, ok := nitchBLSHashBigInt(pkInt, msgInt, rInt); !ok {
		b.Fatal("BLS path not taken")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Hash(pkInt, msgInt, rInt)
	}
}
