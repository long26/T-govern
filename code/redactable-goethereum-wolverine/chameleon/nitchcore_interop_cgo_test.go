//go:build nitchcgo
// +build nitchcgo

// nitchcore_interop_cgo_test.go — cgo-vs-pure differential, cross-interop and
// E2E layer of the nitchcore interop test suite.
//
// Everything here touches the cgoXxx twins in hashseam_cgo.go, so the whole
// file is gated behind the nitchcgo build tag. The pure twins stay untagged
// and always compiled (hashseam_pure.go), which is what makes the A/B
// differential possible: for every protocol step we check that the C++/MIRACL
// implementation and the pure-Go implementation agree on the same wire data,
// and that each side accepts and reproduces the other's artifacts.
//
// Coverage, per docs/nitchcore-c-api-contract.md §6:
//   - DistKG: cgo deal verified by both verifiers, extracted by both
//     extractors to identical key material;
//   - DKRS: full reshare with cross-created deals, cross-verified shares and
//     cross-extracted keys preserving the global public key;
//   - Adaptation: NIZK transcript cross-validation (the H2-transcript interop
//     test), Go Combine interpolating C++ contributions, tamper rejection;
//   - Concurrency: mixed cgo calls under -race.
package chameleon

import (
	"bytes"
	"fmt"
	"math/big"
	"sync"
	"testing"

	bls12381 "github.com/kilic/bls12-381"
)

// ====================== 4. DistKG cross-interop ======================

// TestCgoPureDistKGCrossInterop drives the DistKG round with C++-created deals
// for seeds 0..2 and checks, per share and per extraction, that the pure and
// cgo implementations are interchangeable.
func TestCgoPureDistKGCrossInterop(t *testing.T) {
	const tThreshold = 2
	const nParties = 4
	for seed := 0; seed < 3; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			pp, err := Setup()
			if err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// One cgo deal per dealer, plus the shares each dealer sends out.
			deals := make(map[int]*DistKGDeal, nParties)
			sharesByDealer := make(map[int]map[int]*big.Int, nParties)
			for i := 1; i <= nParties; i++ {
				deal, priv, err := cgoCreateDistKGDeal(pp, i, tThreshold, nParties)
				if err != nil {
					t.Fatalf("cgoCreateDistKGDeal(dealer %d) failed: %v", i, err)
				}
				deals[i] = deal
				sharesByDealer[i] = priv.Shares
			}

			// Verify ALL n*n shares with BOTH verifiers; they must agree per
			// share (and all be true).
			qual := make([]int, 0, nParties)
			for i := 1; i <= nParties; i++ {
				qual = append(qual, i)
			}
			for i := 1; i <= nParties; i++ {
				for j := 1; j <= nParties; j++ {
					share := sharesByDealer[i][j]
					pureOK, err := pureVerifyDistKGShare(pp, deals[i], share, j, tThreshold, nParties)
					if err != nil {
						t.Fatalf("pureVerifyDistKGShare(dealer=%d, receiver=%d) error: %v", i, j, err)
					}
					cgoOK, err := cgoVerifyDistKGShare(pp, deals[i], share, j, tThreshold, nParties)
					if err != nil {
						t.Fatalf("cgoVerifyDistKGShare(dealer=%d, receiver=%d) error: %v", i, j, err)
					}
					if pureOK != cgoOK {
						t.Fatalf("verifier disagreement for dealer=%d receiver=%d: pure=%v cgo=%v",
							i, j, pureOK, cgoOK)
					}
					if !pureOK {
						t.Fatalf("share from dealer=%d to receiver=%d failed verification in both verifiers", i, j)
					}
				}
			}

			// Both extractors, same inputs, must produce identical key material.
			for j := 1; j <= nParties; j++ {
				sharesToJ := make(map[int]*big.Int, nParties)
				for i := 1; i <= nParties; i++ {
					sharesToJ[i] = sharesByDealer[i][j]
				}
				pureKey, err := pureDistKGExtract(pp, j, qual, deals, sharesToJ, tThreshold, nParties)
				if err != nil {
					t.Fatalf("pureDistKGExtract(j=%d) failed: %v", j, err)
				}
				cgoKey, err := cgoDistKGExtract(pp, j, qual, deals, sharesToJ, tThreshold, nParties)
				if err != nil {
					t.Fatalf("cgoDistKGExtract(j=%d) failed: %v", j, err)
				}
				if pureKey.SecretShare.Cmp(cgoKey.SecretShare) != 0 {
					t.Fatalf("party %d: SecretShare mismatch: pure=%s cgo=%s",
						j, pureKey.SecretShare.String(), cgoKey.SecretShare.String())
				}
				pairingMu.Lock()
				purePK := pairing.G2.ToBytes(pureKey.GlobalPK)
				cgoPK := pairing.G2.ToBytes(cgoKey.GlobalPK)
				purePKG1 := pairing.G1.ToBytes(pureKey.GlobalPKG1)
				cgoPKG1 := pairing.G1.ToBytes(cgoKey.GlobalPKG1)
				pureVK := pairing.G1.ToBytes(pureKey.VerificationKey)
				cgoVK := pairing.G1.ToBytes(cgoKey.VerificationKey)
				pairingMu.Unlock()
				if !bytes.Equal(purePK, cgoPK) {
					t.Fatalf("party %d: GlobalPK bytes mismatch", j)
				}
				if !bytes.Equal(purePKG1, cgoPKG1) {
					t.Fatalf("party %d: GlobalPKG1 bytes mismatch", j)
				}
				if !bytes.Equal(pureVK, cgoVK) {
					t.Fatalf("party %d: VerificationKey bytes mismatch", j)
				}
			}

			// Tamper: corrupt a commitment in deal.A. A commitment-block swap
			// keeps every 96-byte block a well-formed curve point (so both
			// verifiers reach their cryptographic checks instead of failing at
			// parse) but breaks the share equation for any receiver whose
			// weight on the moved coefficient is non-zero. Both verifiers must
			// return false.
			//
			// Note: a raw single-byte flip in deal.A generally makes the block
			// unparseable, on which the two twins legitimately diverge —
			// cgoVerifyDistKGShare maps it to (false, NITCH_ERR_CRYPTO) while
			// pureVerifyDistKGShare ignores the FromBytes error and can panic
			// on the nil point (hashseam_pure.go:206). That divergence is a
			// pre-existing pure-side robustness gap, out of scope here.
			tampered := *deals[1]
			tampered.A = append([]byte(nil), deals[1].A...)
			copy(tampered.A[0:g1PointByteSize], deals[1].A[g1PointByteSize:2*g1PointByteSize])
			copy(tampered.A[g1PointByteSize:2*g1PointByteSize], deals[1].A[0:g1PointByteSize])
			for j := 1; j <= nParties; j++ {
				share := sharesByDealer[1][j]
				pureOK, err := pureVerifyDistKGShare(pp, &tampered, share, j, tThreshold, nParties)
				if err != nil {
					t.Fatalf("pureVerifyDistKGShare(tampered, receiver=%d) error: %v", j, err)
				}
				cgoOK, err := cgoVerifyDistKGShare(pp, &tampered, share, j, tThreshold, nParties)
				if err != nil {
					t.Fatalf("cgoVerifyDistKGShare(tampered, receiver=%d) error: %v", j, err)
				}
				if pureOK || cgoOK {
					t.Fatalf("tampered deal.A accepted: receiver=%d pure=%v cgo=%v", j, pureOK, cgoOK)
				}
				if pureOK != cgoOK {
					t.Fatalf("verifier disagreement on tampered deal for receiver=%d: pure=%v cgo=%v", j, pureOK, cgoOK)
				}
			}
		})
	}
}

// ====================== 5. DKRS reshare cross-interop ======================

// interopResharedCommittee is the fixture shared by tests 5-7: a (t=1, n=2)
// pure DistKG committee reshared to (t=2, n=4) via cgo deals, with both pure
// and cgo deals/keys retained for cross-checks.
type interopResharedCommittee struct {
	pp      *PublicParameters
	oldKeys []*NITCHKey
	newKeys []*NITCHKey // extracted with cgoDKRSExtract from the cgo deals

	oldT, oldN     int
	newT, newN     int
	qual           []int
	cgoDeals       map[int]*DKRSDeal
	cgoDealPriv    map[int]*DKRSDealPrivate
	pureDeals      map[int]*DKRSDeal
	pureDealShares map[int]map[int]*big.Int
}

// interopBuildResharedCommittee builds the fixture. Old committee (1,2) is
// generated with pure SimulatedDistKG; dealer 1 additionally creates a pure
// DKRS deal from the SAME oldSecretShare so the pure/cgo deal paths can be
// cross-verified against identical secrets.
func interopBuildResharedCommittee(t *testing.T) *interopResharedCommittee {
	t.Helper()
	const oldT, oldN = 1, 2
	const newT, newN = 2, 4

	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	oldKeys, err := SimulatedDistKG(pp, oldT, oldN)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	f := &interopResharedCommittee{
		pp: pp, oldKeys: oldKeys,
		oldT: oldT, oldN: oldN, newT: newT, newN: newN,
		qual:           []int{1, 2},
		cgoDeals:       make(map[int]*DKRSDeal, oldN),
		cgoDealPriv:    make(map[int]*DKRSDealPrivate, oldN),
		pureDeals:      make(map[int]*DKRSDeal, oldN),
		pureDealShares: make(map[int]map[int]*big.Int, oldN),
	}

	// cgo deals from every old member's sk.
	for i := 1; i <= oldN; i++ {
		deal, priv, err := cgoCreateDKRSDeal(pp, i, oldKeys[i-1].SecretShare, oldT, newT, newN)
		if err != nil {
			t.Fatalf("cgoCreateDKRSDeal(dealer %d) failed: %v", i, err)
		}
		deal.OldNumParties = big.NewInt(oldN)
		f.cgoDeals[i] = deal
		f.cgoDealPriv[i] = priv
	}
	// pure deal from dealer 1's SAME oldSecretShare.
	pureDeal, purePriv, err := pureCreateDKRSDeal(pp, 1, oldKeys[0].SecretShare, oldT, newT, newN)
	if err != nil {
		t.Fatalf("pureCreateDKRSDeal failed: %v", err)
	}
	pureDeal.OldNumParties = big.NewInt(oldN)
	f.pureDeals[1] = pureDeal
	f.pureDealShares[1] = purePriv.Shares

	// Random higher coefficients differ between the two implementations, so
	// the commitments must NOT be byte-equal.
	if bytes.Equal(pureDeal.A, f.cgoDeals[1].A) {
		t.Fatal("pure and cgo DKRS deals produced identical commitments; polynomials should be independently random")
	}

	// Cross-verify shares: cgo deals checked by the pure verifier, and the
	// pure deal checked by the cgo verifier.
	for i := 1; i <= oldN; i++ {
		for j := 1; j <= newN; j++ {
			ok, err := pureVerifyDKRSShare(pp, f.cgoDeals[i], f.cgoDealPriv[i].Shares[j], j, newT, newN)
			if err != nil {
				t.Fatalf("pureVerifyDKRSShare(cgo deal %d, receiver %d) error: %v", i, j, err)
			}
			if !ok {
				t.Fatalf("pureVerifyDKRSShare rejected cgo deal %d share to receiver %d", i, j)
			}
		}
	}
	for j := 1; j <= newN; j++ {
		ok, err := cgoVerifyDKRSShare(pp, pureDeal, purePriv.Shares[j], j, newT, newN)
		if err != nil {
			t.Fatalf("cgoVerifyDKRSShare(pure deal, receiver %d) error: %v", j, err)
		}
		if !ok {
			t.Fatalf("cgoVerifyDKRSShare rejected pure deal share to receiver %d", j)
		}
	}

	// Extract new keys from the cgo deals with BOTH extractors; results must
	// agree, and the global public key must survive the reshare.
	f.newKeys = make([]*NITCHKey, newN)
	for j := 1; j <= newN; j++ {
		sharesToMe := make(map[int]*big.Int, oldN)
		for i := 1; i <= oldN; i++ {
			sharesToMe[i] = f.cgoDealPriv[i].Shares[j]
		}
		pureKey, err := pureDKRSExtract(pp, oldKeys[0], j, f.qual, f.cgoDeals, sharesToMe, newT, newN, oldN)
		if err != nil {
			t.Fatalf("pureDKRSExtract(j=%d) failed: %v", j, err)
		}
		cgoKey, err := cgoDKRSExtract(pp, oldKeys[0], j, f.qual, f.cgoDeals, sharesToMe, newT, newN, oldN)
		if err != nil {
			t.Fatalf("cgoDKRSExtract(j=%d) failed: %v", j, err)
		}
		if pureKey.SecretShare.Cmp(cgoKey.SecretShare) != 0 {
			t.Fatalf("new member %d: sk mismatch between extractors: pure=%s cgo=%s",
				j, pureKey.SecretShare.String(), cgoKey.SecretShare.String())
		}
		f.newKeys[j-1] = cgoKey
	}
	for j, key := range f.newKeys {
		if !SameGlobalPublicKey(oldKeys[0], key) {
			t.Fatalf("new member %d: SameGlobalPublicKey failed after reshare", j+1)
		}
	}

	// Threshold algebra on the reshared committee: g1^sk == vk must hold for
	// the cgo-extracted keys too.
	pairingMu.Lock()
	for j, key := range f.newKeys {
		g1sk := pairing.G1.New()
		pairing.G1.MulScalar(g1sk, pp.g1, bigIntToFr(key.SecretShare))
		if !bytes.Equal(pairing.G1.ToBytes(g1sk), pairing.G1.ToBytes(key.VerificationKey)) {
			pairingMu.Unlock()
			t.Fatalf("new member %d: g1^sk != VerificationKey for cgo-extracted key", j+1)
		}
	}
	pairingMu.Unlock()

	return f
}

// TestCgoPureDKRSCrossInterop runs the full reshare and the NIZK
// cross-validation matrix. The share-level interop above proves the deal
// algebra agrees; the critical remaining piece is the H2 transcript: a proof
// generated by one implementation must verify under the other, in both
// directions.
func TestCgoPureDKRSCrossInterop(t *testing.T) {
	f := interopBuildResharedCommittee(t)
	m := Message("dkrs-interop-source-message")
	mPrime := Message("dkrs-interop-target-message")

	for j := 1; j <= f.newN; j++ {
		key := f.newKeys[j-1]
		j := j

		// pure adapt with the old key (message pair from the old committee's
		// perspective) must PartialVerify under the pure verifier.
		t.Run(fmt.Sprintf("member%d", j), func(t *testing.T) {
			pureShare, err := purePartialAdapt(key, m, mPrime)
			if err != nil {
				t.Fatalf("purePartialAdapt(member %d) failed: %v", j, err)
			}
			if !purePartialVerify(key, pureShare, m, mPrime) {
				t.Fatalf("purePartialVerify rejected a purePartialAdapt share for member %d", j)
			}
			// cgo verifier must accept the pure proof (H2 transcript interop).
			if !cgoPartialVerify(key, pureShare, m, mPrime) {
				t.Fatalf("cgoPartialVerify rejected a purePartialAdapt share for member %d", j)
			}

			// cgo adapt, verified by both.
			cgoShare, err := cgoPartialAdapt(key, m, mPrime)
			if err != nil {
				t.Fatalf("cgoPartialAdapt(member %d) failed: %v", j, err)
			}
			if !cgoPartialVerify(key, cgoShare, m, mPrime) {
				t.Fatalf("cgoPartialVerify rejected a cgoPartialAdapt share for member %d", j)
			}
			if !purePartialVerify(key, cgoShare, m, mPrime) {
				t.Fatalf("purePartialVerify rejected a cgoPartialAdapt share for member %d", j)
			}

			// The contribution itself must be implementation-independent:
			// c_i = y^{sk_i} is fully determined by (sk_i, m, mPrime), so the
			// pure and cgo contributions must be the same group point.
			pairingMu.Lock()
			pureBytes := pairing.G1.ToBytes(pureShare.Share)
			cgoBytes := pairing.G1.ToBytes(cgoShare.Share)
			pairingMu.Unlock()
			if !bytes.Equal(pureBytes, cgoBytes) {
				t.Fatalf("member %d: contribution mismatch between pure and cgo adapt", j)
			}
		})
	}
}

// ====================== 6. Combine interop ======================

// TestCgoPureCombineInterop proves the Go Combine interpolates C++-produced
// contributions correctly: Hash under the reshared committee, adapt with
// t+1 cgo (respectively pure) shares, Combine in Go, and Verify(m', r') must
// hold in both cases.
func TestCgoPureCombineInterop(t *testing.T) {
	f := interopBuildResharedCommittee(t)
	m := Message("combine-interop-source-message")
	mPrime := Message("combine-interop-target-message")

	r0 := RandomPointG1(f.pp)
	if r0 == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	hashOutput, err := Hash(f.newKeys[0], m, r0)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if !Verify(f.newKeys[0], hashOutput.Hash, m, hashOutput.CheckValue) {
		t.Fatal("pre-adaptation Verify failed for the reshared committee")
	}

	buildShares := func(adapt func(*NITCHKey, Message, Message) (*AdaptationShare, error), label string) {
		shares := make([]*AdaptationShare, 0, f.newT+1)
		for i := 0; i < f.newT+1; i++ {
			share, err := adapt(f.newKeys[i], m, mPrime)
			if err != nil {
				t.Fatalf("%s PartialAdapt(member %d) failed: %v", label, i+1, err)
			}
			// Cross-check each share before combining.
			if !purePartialVerify(f.newKeys[i], share, m, mPrime) {
				t.Fatalf("%s share %d failed purePartialVerify", label, i+1)
			}
			if !cgoPartialVerify(f.newKeys[i], share, m, mPrime) {
				t.Fatalf("%s share %d failed cgoPartialVerify", label, i+1)
			}
			shares = append(shares, share)
		}
		rPrime, err := Combine(f.pp, shares, f.newT, f.newN, hashOutput, m, mPrime)
		if err != nil {
			t.Fatalf("%s Combine failed: %v", label, err)
		}
		if rPrime == nil || len(rPrime.Bytes()) != g1CompressedPointByteSize {
			t.Fatalf("%s Combine returned malformed r'", label)
		}
		if !Verify(f.newKeys[0], hashOutput.Hash, mPrime, rPrime) {
			t.Fatalf("%s: Verify(key, h, m', r') is false after Combine", label)
		}
	}
	buildShares(cgoPartialAdapt, "cgo")
	buildShares(purePartialAdapt, "pure")
}

// ====================== 7. tamper rejection ======================

// TestCgoPartialVerifyRejectsTampering corrupts each component of a valid
// cgo-produced adaptation share and requires BOTH verifiers to reject it.
func TestCgoPartialVerifyRejectsTampering(t *testing.T) {
	f := interopBuildResharedCommittee(t)
	m := Message("tamper-source-message")
	mPrime := Message("tamper-target-message")
	key := f.newKeys[0]

	valid, err := cgoPartialAdapt(key, m, mPrime)
	if err != nil {
		t.Fatalf("cgoPartialAdapt failed: %v", err)
	}
	if !cgoPartialVerify(key, valid, m, mPrime) {
		t.Fatal("baseline cgoPartialVerify failed on a fresh valid share")
	}
	if !purePartialVerify(key, valid, m, mPrime) {
		t.Fatal("baseline purePartialVerify failed on a fresh valid share")
	}

	// mustReject asserts both verifiers refuse the corrupted share.
	mustReject := func(name string, share *AdaptationShare) {
		t.Helper()
		if purePartialVerify(key, share, m, mPrime) {
			t.Fatalf("%s: purePartialVerify accepted a tampered share", name)
		}
		if cgoPartialVerify(key, share, m, mPrime) {
			t.Fatalf("%s: cgoPartialVerify accepted a tampered share", name)
		}
	}

	// Challenge tampering.
	tamperedOmega := *valid
	tamperedOmega.Proof = &NIZKProof{
		Omega: new(big.Int).Add(valid.Proof.Omega, big.NewInt(1)),
		Z:     valid.Proof.Z,
	}
	mustReject("omega+1", &tamperedOmega)

	// Response tampering.
	tamperedZ := *valid
	tamperedZ.Proof = &NIZKProof{
		Omega: valid.Proof.Omega,
		Z:     new(big.Int).Add(valid.Proof.Z, big.NewInt(1)),
	}
	mustReject("z+1", &tamperedZ)

	// Contribution tampering: flipping raw serialization bytes does not yield
	// a parseable curve point (both verifiers treat it as invalid), so also
	// include a well-formed-but-wrong point — the negation of the true
	// contribution — which isolates the transcript check from parse checks.
	flipped := *valid
	pairingMu.Lock()
	flippedBytes := pairing.G1.ToBytes(valid.Share)
	flippedBytes[48] ^= 0xFF
	negPoint := pairing.G1.New()
	pairing.G1.Neg(negPoint, valid.Share)
	negBytes := pairing.G1.ToBytes(negPoint)
	pairingMu.Unlock()
	if bytes.Equal(negBytes, flippedBytes) {
		t.Fatal("internal: negated contribution equals the byte-flipped contribution")
	}
	flipped.Share, _ = bytesToG1PointOrNil(flippedBytes)
	if flipped.Share != nil {
		mustReject("flipped-contribution-bytes", &flipped)
	}
	negated := *valid
	negated.Share = negPoint
	mustReject("negated-contribution", &negated)
}

// bytesToG1PointOrNil parses a 96-byte uncompressed G1 point, returning nil
// when the bytes are not a valid point (used by the tamper test to branch on
// parseability without panicking).
func bytesToG1PointOrNil(b []byte) (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	p, err := pairing.G1.FromBytes(b)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ====================== 8. concurrency ======================

// TestCgoConcurrentMixed hammers the three stateful cgo entry points from 4
// goroutines x 20 iterations, each goroutine with its own deal data. The C
// library keeps a thread_local context; this test is the -race check that the
// Go-side locking contract (pairingMu never held across a C call) holds.
func TestCgoConcurrentMixed(t *testing.T) {
	const goroutines = 4
	const iterations = 20
	const tThreshold = 1
	const nParties = 2
	const newT = 1
	const newN = 2

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*iterations*4)
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			pp, err := Setup()
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: Setup: %w", g, err)
				return
			}
			oldKeys, err := SimulatedDistKG(pp, tThreshold, nParties)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: SimulatedDistKG: %w", g, err)
				return
			}
			src := Message(fmt.Sprintf("concurrent-src-%d", g))
			dst := Message(fmt.Sprintf("concurrent-dst-%d", g))
			for it := 0; it < iterations; it++ {
				// 1. DistKG deal creation.
				deal, priv, err := cgoCreateDistKGDeal(pp, 1, tThreshold, nParties)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: cgoCreateDistKGDeal: %w", g, it, err)
					return
				}
				for j := 1; j <= nParties; j++ {
					ok, err := cgoVerifyDistKGShare(pp, deal, priv.Shares[j], j, tThreshold, nParties)
					if err != nil || !ok {
						errs <- fmt.Errorf("goroutine %d iter %d: cgoVerifyDistKGShare(j=%d): ok=%v err=%v", g, it, j, ok, err)
						return
					}
				}

				// 2. DKRS deal creation + share verification (cgo verifier
				//    over a cgo deal; the pure verifier over the same data is
				//    covered by test 5).
				dkrsDeal, dkrsPriv, err := cgoCreateDKRSDeal(pp, 1, oldKeys[0].SecretShare, tThreshold, newT, newN)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: cgoCreateDKRSDeal: %w", g, it, err)
					return
				}
				for j := 1; j <= newN; j++ {
					ok, err := cgoVerifyDKRSShare(pp, dkrsDeal, dkrsPriv.Shares[j], j, newT, newN)
					if err != nil || !ok {
						errs <- fmt.Errorf("goroutine %d iter %d: cgoVerifyDKRSShare(j=%d): ok=%v err=%v", g, it, j, ok, err)
						return
					}
				}

				// 3. Adaptation: cgo adapt, then cross-verify with BOTH
				//    verifiers on this goroutine's own key/messages.
				share, err := cgoPartialAdapt(oldKeys[0], src, dst)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: cgoPartialAdapt: %w", g, it, err)
					return
				}
				if !cgoPartialVerify(oldKeys[0], share, src, dst) {
					errs <- fmt.Errorf("goroutine %d iter %d: cgoPartialVerify rejected its own share", g, it)
					return
				}
				if !purePartialVerify(oldKeys[0], share, src, dst) {
					errs <- fmt.Errorf("goroutine %d iter %d: purePartialVerify rejected a cgo share", g, it)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
