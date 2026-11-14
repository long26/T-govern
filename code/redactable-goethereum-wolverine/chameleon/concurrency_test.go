package chameleon

import (
	"fmt"
	"math/big"
	"sync"
	"testing"
)

// These tests reproduce the production data race that caused the block-edit
// verification path to intermittently reject a perfectly valid G1 nonce r with a
// spurious "point is not on curve" (the "falling back to rlpHash" /
// "Old block chameleon hash verification failed" errors in R-min4).
//
// Root cause: the shared kilic pairing engine (package-global `pairing`) uses
// non-thread-safe internal scratch buffers (e.g. G1.t). Several hot paths
// (bigIntToG1Point / g1PointToBigInt / Combine / PartialAdapt / verifyNIZKProof)
// accessed the engine WITHOUT holding pairingMu, so a concurrent engine op
// corrupted the scratch while IsOnCurve (inside G1.FromBytes) was reading it,
// making IsOnCurve return false for points that are genuinely on the curve.
//
// Run with `go test -race ./chameleon/`. Against the pre-fix code this reports a
// data race AND/OR intermittent failures; against the patched code it is clean
// and deterministic.

// TestConcurrentHashVerify hammers Hash+Verify on a shared, valid r from many
// goroutines. Hash internally calls bigIntToG1Point(r) — the exact call that
// intermittently failed. It must never error and Verify must never return false.
func TestConcurrentHashVerify(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	keys, err := SimulatedDistKG(pp, 2, 8)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	m := Message("concurrent-hash-verify-message")
	r := RandomPointG1(pp) // fixed, valid G1 point; shared by all goroutines

	// Baseline (serial): must succeed and the r round-trip must be valid.
	base, err := Hash(keys[0], m, r)
	if err != nil {
		t.Fatalf("baseline Hash failed: %v", err)
	}
	if !Verify(keys[0], base.Hash, m, base.CheckValue) {
		t.Fatal("baseline Verify returned false")
	}

	const goroutines = 24
	const iterations = 20

	var wg sync.WaitGroup
	errc := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// bigIntToG1Point(r) lives inside Hash.
				ho, herr := Hash(keys[0], m, r)
				if herr != nil {
					errc <- fmt.Errorf("Hash failed under concurrency: %v", herr)
					return
				}
				if !Verify(keys[0], ho.Hash, m, ho.CheckValue) {
					errc <- fmt.Errorf("Verify returned false under concurrency")
					return
				}
				// Exercise the r→point→r round-trip used by Combine: feed the
				// freshly serialized CheckValue back into Hash.
				if _, herr := Hash(keys[0], Message(fmt.Sprintf("alt-%d", j)), ho.CheckValue); herr != nil {
					errc <- fmt.Errorf("Hash(CheckValue) failed under concurrency: %v", herr)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errc)
	for e := range errc {
		t.Error(e)
	}
}

// TestConcurrentAdaptCombine runs the full threshold-adapt path
// (PartialAdapt → PartialVerify → Combine → Verify) concurrently. Pre-fix,
// verifyNIZKProof and Combine touched the engine without the lock, which also
// produced the "Share verification failed" / share-quorum errors seen in R-min4.
func TestConcurrentAdaptCombine(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	m := Message("adapt-concurrent")
	mPrime := Message("adapt-concurrent-prime")
	r := RandomPointG1(pp)
	ho, err := Hash(keys[0], m, r)
	if err != nil {
		t.Fatalf("baseline Hash failed: %v", err)
	}

	const goroutines = 12
	var wg sync.WaitGroup
	errc := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shares := make([]*AdaptationShare, 0, tThreshold+1)
			for k := 0; k <= tThreshold; k++ {
				s, serr := PartialAdapt(keys[k], m, mPrime)
				if serr != nil {
					errc <- fmt.Errorf("PartialAdapt failed: %v", serr)
					return
				}
				if !PartialVerify(keys[k], s, m, mPrime) {
					errc <- fmt.Errorf("PartialVerify returned false under concurrency")
					return
				}
				shares = append(shares, s)
			}
			rp, cerr := Combine(pp, shares, tThreshold, nParties, ho, m, mPrime)
			if cerr != nil {
				errc <- fmt.Errorf("Combine failed under concurrency: %v", cerr)
				return
			}
			// Chameleon-hash property: CH(m, r) == CH(m', r').
			if !Verify(keys[0], ho.Hash, mPrime, rp) {
				errc <- fmt.Errorf("Verify(m', r') returned false under concurrency")
				return
			}
		}()
	}
	wg.Wait()
	close(errc)
	for e := range errc {
		t.Error(e)
	}
}

// TestConcurrentMixedAll mixes every public engine-touching entry point across
// goroutines sharing the same keys, maximizing contention on the shared engine.
func TestConcurrentMixedAll(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	const goroutines = 16
	var wg sync.WaitGroup
	errc := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				m := Message(fmt.Sprintf("mixed-%d-%d", id, j))
				r := RandomPointG1(pp)
				ho, herr := Hash(keys[0], m, r)
				if herr != nil {
					errc <- fmt.Errorf("Hash failed: %v", herr)
					return
				}
				if !Verify(keys[0], ho.Hash, m, ho.CheckValue) {
					errc <- fmt.Errorf("Verify failed")
					return
				}
				// ComputeTransactionR also touches the engine under its own lock.
				if _, rerr := ComputeTransactionR(keys[0], big.NewInt(1337), ho.Hash.Bytes()); rerr != nil {
					errc <- fmt.Errorf("ComputeTransactionR failed: %v", rerr)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for e := range errc {
		t.Error(e)
	}
}
