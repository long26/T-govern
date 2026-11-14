package chameleon

import (
	"fmt"
	"testing"
)

// committeeScale is deliberately used only to prepare a public key fixture.
// TCH.Setup, TCH.Hash and TCH.Verify do not receive t or n: they operate on
// fixed BLS12-381 public parameters and one global public key. Consequently,
// DistKG and random-check-value generation are outside every timed region.
type committeeScale struct {
	threshold  int
	numParties int
}

var publicAlgorithmCommitteeScales = []committeeScale{
	{threshold: 0, numParties: 1},
	{threshold: 3, numParties: 8},
	{threshold: 15, numParties: 32},
}

const publicAlgorithmWarmupIterations = 20

var (
	benchmarkPublicPP      *PublicParameters
	benchmarkPublicHashOut *HashOutput
	benchmarkPublicVerify  bool
)

// publicAlgorithmFixture creates a key for a particular threshold committee,
// then prepares fixed public Hash/Verify inputs. It must never be included in
// the public-algorithm timing boundary.
func publicAlgorithmFixture(scale committeeScale) (*PublicParameters, *NITCHKey, Message, *HashOutput, error) {
	pp, err := Setup()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("setup: %w", err)
	}
	keys, err := SimulatedDistKG(pp, scale.threshold, scale.numParties)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("distkg t=%d n=%d: %w", scale.threshold, scale.numParties, err)
	}
	r := RandomPointG1(pp)
	if r == nil {
		return nil, nil, nil, nil, fmt.Errorf("random check value")
	}
	message := Message("tch-public-algorithm-scale-benchmark")
	hashOutput, err := Hash(keys[0], message, r)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("prepare hash: %w", err)
	}
	if !Verify(keys[0], hashOutput.Hash, message, hashOutput.CheckValue) {
		return nil, nil, nil, nil, fmt.Errorf("prepared hash does not verify")
	}
	return pp, keys[0], message, hashOutput, nil
}

// TestTCHPublicAlgorithmsAcrossCommitteeScales is a semantic guard for the
// benchmark fixtures. It shows that public Hash and Verify work unchanged for
// every supported committee scale. It intentionally does not make a timing
// assertion; performance comparisons belong to Go benchmarks.
func TestTCHPublicAlgorithmsAcrossCommitteeScales(t *testing.T) {
	for _, scale := range publicAlgorithmCommitteeScales {
		scale := scale
		t.Run(fmt.Sprintf("t=%d/n=%d", scale.threshold, scale.numParties), func(t *testing.T) {
			pp, key, message, hashOutput, err := publicAlgorithmFixture(scale)
			if err != nil {
				t.Fatal(err)
			}
			if pp.g1 == nil || pp.g2 == nil || pp.p == nil || key.GlobalPK == nil {
				t.Fatal("Setup/DistKG returned incomplete public material")
			}
			if !Verify(key, hashOutput.Hash, message, hashOutput.CheckValue) {
				t.Fatal("public Verify rejected a Hash output")
			}
		})
	}
}

// BenchmarkTCHSetup measures the fixed BLS12-381 public-parameter setup.
// Setup has no t/n input, so its cost is structurally independent of the
// committee size; DistKG is the separate scale-dependent operation.
func BenchmarkTCHSetup(b *testing.B) {
	for i := 0; i < publicAlgorithmWarmupIterations; i++ {
		if _, err := Setup(); err != nil {
			b.Fatalf("Setup warmup %d: %v", i+1, err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pp, err := Setup()
		if err != nil {
			b.Fatalf("Setup: %v", err)
		}
		benchmarkPublicPP = pp
	}
}

// BenchmarkTCHHashAcrossCommitteeScales measures only public Hash. The
// prepared key differs by committee scale, while r selection and DistKG are
// deliberately excluded.
func BenchmarkTCHHashAcrossCommitteeScales(b *testing.B) {
	for _, scale := range publicAlgorithmCommitteeScales {
		scale := scale
		b.Run(fmt.Sprintf("t=%d/n=%d", scale.threshold, scale.numParties), func(b *testing.B) {
			_, key, message, hashOutput, err := publicAlgorithmFixture(scale)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < publicAlgorithmWarmupIterations; i++ {
				if _, err := Hash(key, message, hashOutput.CheckValue); err != nil {
					b.Fatalf("Hash warmup %d: %v", i+1, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				output, err := Hash(key, message, hashOutput.CheckValue)
				if err != nil {
					b.Fatalf("Hash: %v", err)
				}
				benchmarkPublicHashOut = output
			}
		})
	}
}

// BenchmarkTCHVerifyAcrossCommitteeScales measures only public Verify. Hash,
// r selection and committee formation are fixture preparation, not work done
// by Verify.
func BenchmarkTCHVerifyAcrossCommitteeScales(b *testing.B) {
	for _, scale := range publicAlgorithmCommitteeScales {
		scale := scale
		b.Run(fmt.Sprintf("t=%d/n=%d", scale.threshold, scale.numParties), func(b *testing.B) {
			_, key, message, hashOutput, err := publicAlgorithmFixture(scale)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < publicAlgorithmWarmupIterations; i++ {
				if !Verify(key, hashOutput.Hash, message, hashOutput.CheckValue) {
					b.Fatalf("Verify warmup %d rejected a prepared Hash output", i+1)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkPublicVerify = Verify(key, hashOutput.Hash, message, hashOutput.CheckValue)
				if !benchmarkPublicVerify {
					b.Fatal("Verify rejected a prepared Hash output")
				}
			}
		})
	}
}
