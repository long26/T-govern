package chameleon

// Evidence test for the 2026-08-30 investigation of the in-situ per-op cost
// gap (tgovern 7.5ms/op vs nitch 2.0ms/op under experiment load).
// RESULTS (idle, this machine): message length is FLAT (32B..1024B all
// ~2.5-2.8ms), ctx alternation/rebuild is ~free (steady 2.5 / alternating
// 2.8 / forced-rebuild 2.5ms) — i.e. neither explains the gap when idle.
// The gap only appears under experiment load with the FULL-serialization
// message; after migrating to the 32B NITCH_TX_HASH_V1 digest
// (types.TransactionHashMessage) in-situ per-op dropped to 2.66ms and the
// segment profile matched nitch exactly (pairing ~2.0 / h2c ~0.3 / lock ~0).
// Kept as regression/baseline tooling for future per-op questions.
//
// Run: go test ./chameleon/ -run TestH2CMessageLengthEvidence -v -count=1

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestH2CMessageLengthEvidence(t *testing.T) {
	os.Setenv("CHAMELEON_TIMING", "0") // keep per-call breakdown logs off; sweep is the signal

	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	keys, err := NITCHBLSSimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	key := keys[0]
	pkInt, err := NITCHBLSPublicKeyToBigInt(key)
	if err != nil {
		t.Fatalf("pk to int: %v", err)
	}
	r, err := NITCHBLSComputeR(key, []byte("evidence-r"))
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	rInt, err := NITCHBLSRToBigInt(r)
	if err != nil {
		t.Fatalf("r to int: %v", err)
	}

	// Warm the context cache so the sweep measures steady-state calls.
	Hash(pkInt, big.NewInt(1), rInt)

	const iters = 30
	lengths := []int{32, 64, 128, 192, 256, 320, 384, 448, 512, 1024}
	t.Logf("%6s %12s %12s %14s", "len", "full(ms)", "digest32(ms)", "full/digest32")

	for _, length := range lengths {
		msg := make([]byte, length)

		var fullTotal time.Duration
		for i := 0; i < iters; i++ {
			binary.BigEndian.PutUint32(msg[:4], uint32(i)) // fresh content per call
			m := new(big.Int).SetBytes(msg)
			start := time.Now()
			if Hash(pkInt, m, rInt) == nil {
				t.Fatalf("len=%d: Hash returned nil", length)
			}
			fullTotal += time.Since(start)
		}

		var digestTotal time.Duration
		for i := 0; i < iters; i++ {
			binary.BigEndian.PutUint32(msg[:4], uint32(i))
			digest := sha256.Sum256(msg) // stand-in for keccak; any 32B digest has the same h2c cost
			m := new(big.Int).SetBytes(digest[:])
			start := time.Now()
			if Hash(pkInt, m, rInt) == nil {
				t.Fatalf("len=%d digest: Hash returned nil", length)
			}
			digestTotal += time.Since(start)
		}

		fullMs := float64(fullTotal.Nanoseconds()) / float64(iters) / 1e6
		digestMs := float64(digestTotal.Nanoseconds()) / float64(iters) / 1e6
		ratio := fullMs / digestMs
		t.Logf("%6d %12.3f %12.3f %14.2f", length, fullMs, digestMs, ratio)
	}

	// Phase 2: single-slot context thrash. After a committee rotation the ON
	// node interleaves two pks: redaction validation hashes OLD-tx pkCH while
	// sealing hashes NEW-pkCH pool txs — the single-slot nitchBLSCtx cache
	// rebuilds on every alternation. Measure: steady same-pk, alternating
	// two-pk, and a fresh-rebuild (first call with an unseen pk).
	keysB, err := NITCHBLSSimulatedDistKG(pp, 2, 4)
	if err != nil {
		t.Fatalf("simulated dkg B: %v", err)
	}
	pkIntB, err := NITCHBLSPublicKeyToBigInt(keysB[0])
	if err != nil {
		t.Fatalf("pk B to int: %v", err)
	}
	msgA := new(big.Int).SetBytes(bytes.Repeat([]byte{0xa1}, 256))
	msgB := new(big.Int).SetBytes(bytes.Repeat([]byte{0xb2}, 256))

	// Steady state: one pk, warm ctx.
	var steadyTotal time.Duration
	for i := 0; i < iters; i++ {
		start := time.Now()
		Hash(pkInt, msgA, rInt)
		steadyTotal += time.Since(start)
	}

	// Alternating: two pks every call — every call flips the slot.
	var altTotal time.Duration
	for i := 0; i < iters; i++ {
		pk, m := pkInt, msgA
		if i%2 == 1 {
			pk, m = pkIntB, msgB
		}
		start := time.Now()
		Hash(pk, m, rInt)
		altTotal += time.Since(start)
	}

	// Fresh rebuild: call with a third never-seen pk, measured individually.
	keysC, err := NITCHBLSSimulatedDistKG(pp, 3, 8)
	if err != nil {
		t.Fatalf("simulated dkg C: %v", err)
	}
	pkIntC, err := NITCHBLSPublicKeyToBigInt(keysC[0])
	if err != nil {
		t.Fatalf("pk C to int: %v", err)
	}
	rebuildSamples := make([]time.Duration, 0, 8)
	pks := []*big.Int{pkInt, pkIntB, pkInt, pkIntB, pkInt, pkIntB, pkInt, pkIntB}
	for i := 0; i < 8; i++ {
		pk, m := pks[i], msgA
		if pks[i] == pkIntB {
			m = msgB
		}
		// interleave one call with pkC to force a rebuild of the next pk's ctx
		Hash(pkIntC, msgA, rInt)
		start := time.Now()
		Hash(pk, m, rInt)
		rebuildSamples = append(rebuildSamples, time.Since(start))
	}
	var rebuildTotal time.Duration
	for _, s := range rebuildSamples {
		rebuildTotal += s
	}

	steadyMs := float64(steadyTotal.Nanoseconds()) / float64(iters) / 1e6
	altMs := float64(altTotal.Nanoseconds()) / float64(iters) / 1e6
	rebuildMs := float64(rebuildTotal.Nanoseconds()) / float64(len(rebuildSamples)) / 1e6
	t.Logf("steady_same_pk=%.3fms alternating_two_pk=%.3fms forced_rebuild=%.3fms", steadyMs, altMs, rebuildMs)
}
