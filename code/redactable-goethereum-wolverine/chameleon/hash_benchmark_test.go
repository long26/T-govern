package chameleon

import "testing"

// BenchmarkHash measures the public NITCH hash path used by this repository:
// hash-to-G1, G1 deserialization, two-pair multi-pairing, final
// exponentiation, and serialization of the GT hash and G1 check value.
// Random check-value generation and DistKG are intentionally outside the
// timed region.
func BenchmarkHash(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("Setup: %v", err)
	}
	keys, err := SimulatedDistKG(pp, 3, 8)
	if err != nil {
		b.Fatalf("SimulatedDistKG: %v", err)
	}
	r := RandomPointG1(pp)
	if r == nil {
		b.Fatal("RandomPointG1 returned nil")
	}
	message := Message("nitch-hash-benchmark")

	// Warm the shared pairing engine and validate the benchmark inputs before
	// starting the timer.
	if _, err := Hash(keys[0], message, r); err != nil {
		b.Fatalf("Hash warmup: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Hash(keys[0], message, r); err != nil {
			b.Fatalf("Hash: %v", err)
		}
	}
}
