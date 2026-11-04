package eth

import (
	"bytes"
	"errors"
	"math/big"
	"math/rand"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestTauGoldenAndDeterminism tests the golden output and determinism of DeriveTau.
func TestTauGoldenAndDeterminism(t *testing.T) {
	// Fixed boundary block hash and epoch
	boundaryHash := common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	epoch := uint64(1)

	tau1 := DeriveTau(boundaryHash, epoch)

	// Expected golden hash for:
	// Keccak256(0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20 || uint64be(1))
	// 40 bytes total.
	goldenHex := "0xdde1ba6395585c1473f59067890f96e8ee6c2a28842cdff9928eb42dc896b275"
	if tau1.Hex() != goldenHex {
		t.Fatalf("DeriveTau golden mismatch: have %s, want %s", tau1.Hex(), goldenHex)
	}

	// Determinism: across 100 runs, must be identical
	for i := 0; i < 100; i++ {
		tauN := DeriveTau(boundaryHash, epoch)
		if tauN != tau1 {
			t.Fatalf("DeriveTau not deterministic on run %d: have %s, want %s", i, tauN.Hex(), tau1.Hex())
		}
	}

	// Distinct inputs must produce distinct taus
	tauEpoch2 := DeriveTau(boundaryHash, 2)
	if tauEpoch2 == tau1 {
		t.Fatalf("DeriveTau with epoch=2 produced identical tau as epoch=1")
	}

	boundaryHash2 := common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f21")
	tauHash2 := DeriveTau(boundaryHash2, 1)
	if tauHash2 == tau1 {
		t.Fatalf("DeriveTau with different boundaryHash produced identical tau")
	}
}

// TestTargetBoundary tests edge cases and comparison semantics of target and PoW verification.
func TestTargetBoundary(t *testing.T) {
	// Base target in the middle of 256-bit space
	targetInt := new(big.Int).Lsh(big.NewInt(1), 240)
	target := common.BigToHash(targetInt)

	// digest == target: must be valid (<=)
	digestEqual := target
	if !VerifyPowDigest(digestEqual, target) {
		t.Errorf("VerifyPowDigest failed when digest == target")
	}

	// digest == target - 1: must be valid (<=)
	digestMinusOne := common.BigToHash(new(big.Int).Sub(targetInt, big.NewInt(1)))
	if !VerifyPowDigest(digestMinusOne, target) {
		t.Errorf("VerifyPowDigest failed when digest == target - 1")
	}

	// digest == target + 1: must be invalid (> target)
	digestPlusOne := common.BigToHash(new(big.Int).Add(targetInt, big.NewInt(1)))
	if VerifyPowDigest(digestPlusOne, target) {
		t.Errorf("VerifyPowDigest succeeded when digest == target + 1")
	}

	// MaxTarget: all possible 256-bit digests must pass
	allOnes := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if !VerifyPowDigest(allOnes, MaxTarget) {
		t.Errorf("VerifyPowDigest failed for allOnes with MaxTarget")
	}
	allZeros := common.Hash{}
	if !VerifyPowDigest(allZeros, MaxTarget) {
		t.Errorf("VerifyPowDigest failed for allZeros with MaxTarget")
	}

	// Zero target: only all-zeros passes
	zeroTarget := common.Hash{}
	if !VerifyPowDigest(allZeros, zeroTarget) {
		t.Errorf("VerifyPowDigest failed for allZeros with zeroTarget")
	}
	oneDigest := common.BigToHash(big.NewInt(1))
	if VerifyPowDigest(oneDigest, zeroTarget) {
		t.Errorf("VerifyPowDigest succeeded for 1 with zeroTarget")
	}

	// Verify that bytes.Compare exactly matches big.Int.Cmp across pseudo-random samples
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 500; i++ {
		var dBytes, tBytes [32]byte
		rng.Read(dBytes[:])
		rng.Read(tBytes[:])
		d := common.Hash(dBytes)
		trg := common.Hash(tBytes)

		byteResult := VerifyPowDigest(d, trg)
		bigD := new(big.Int).SetBytes(d[:])
		bigT := new(big.Int).SetBytes(trg[:])
		bigResult := bigD.Cmp(bigT) <= 0

		if byteResult != bigResult {
			t.Fatalf("Mismatch at iteration %d: byteResult=%v, bigResult=%v (d=%s, target=%s)",
				i, byteResult, bigResult, d.Hex(), trg.Hex())
		}
	}
}

// TestDifficultyToTargetConversion tests bidirectional difficulty and target conversions.
func TestDifficultyToTargetConversion(t *testing.T) {
	// Difficulty = 1 -> target should be MaxTarget
	target1, err := DifficultyToTarget(big.NewInt(1))
	if err != nil {
		t.Fatalf("DifficultyToTarget(1) error: %v", err)
	}
	if target1 != MaxTarget {
		t.Fatalf("DifficultyToTarget(1) = %s, want %s", target1.Hex(), MaxTarget.Hex())
	}

	// Difficulty <= 0 -> error
	if _, err := DifficultyToTarget(big.NewInt(0)); err != ErrZeroDifficulty {
		t.Fatalf("expected ErrZeroDifficulty for diff 0, got %v", err)
	}
	if _, err := DifficultyToTarget(big.NewInt(-1)); err != ErrZeroDifficulty {
		t.Fatalf("expected ErrZeroDifficulty for diff -1, got %v", err)
	}

	// Difficulty = 2 -> target should be 2^255
	target2, err := DifficultyToTarget(big.NewInt(2))
	if err != nil {
		t.Fatalf("DifficultyToTarget(2) error: %v", err)
	}
	expected2 := common.BigToHash(new(big.Int).Lsh(big.NewInt(1), 255))
	if target2 != expected2 {
		t.Fatalf("DifficultyToTarget(2) = %s, want %s", target2.Hex(), expected2.Hex())
	}

	// TargetToDifficulty(expected2) should be 2
	diff, err := TargetToDifficulty(expected2)
	if err != nil {
		t.Fatalf("TargetToDifficulty error: %v", err)
	}
	if diff.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("TargetToDifficulty want 2, got %s", diff.String())
	}
}

// TestCandidateDeduplication verifies that multiple solutions from the same CandidateID
// within an epoch are deduplicated, keeping ONLY the earliest on-chain solution.
func TestCandidateDeduplication(t *testing.T) {
	candA := CandidateID("candidate-A")
	candB := CandidateID("candidate-B")

	// Candidate A submits 3 solutions:
	// 1. block 10, tx 5 (later)
	// 2. block 10, tx 2 (earliest!)
	// 3. block 12, tx 0 (much later)
	solA1 := Solution{Epoch: 1, CandidateID: candA, BlockNumber: 10, TxIndex: 5, Nonce: 101}
	solA2 := Solution{Epoch: 1, CandidateID: candA, BlockNumber: 10, TxIndex: 2, Nonce: 102}
	solA3 := Solution{Epoch: 1, CandidateID: candA, BlockNumber: 12, TxIndex: 0, Nonce: 103}

	// Candidate B submits 2 solutions:
	// 1. block 9, tx 10 (earliest!)
	// 2. block 10, tx 1
	solB1 := Solution{Epoch: 1, CandidateID: candB, BlockNumber: 9, TxIndex: 10, Nonce: 201}
	solB2 := Solution{Epoch: 1, CandidateID: candB, BlockNumber: 10, TxIndex: 1, Nonce: 202}

	// Provide in random order
	input := []Solution{solA3, solB2, solA1, solB1, solA2}
	ranked := DeduplicateAndRankSolutions(input)

	if len(ranked) != 2 {
		t.Fatalf("expected 2 deduplicated solutions, got %d", len(ranked))
	}

	// Earliest solution overall is solB1 (block 9, tx 10)
	if ranked[0].CandidateID != candB || ranked[0].BlockNumber != 9 || ranked[0].TxIndex != 10 || ranked[0].Nonce != 201 {
		t.Fatalf("rank 1 mismatch: expected B (block 9, tx 10, nonce 201), got %+v", ranked[0])
	}

	// Second earliest is solA2 (block 10, tx 2)
	if ranked[1].CandidateID != candA || ranked[1].BlockNumber != 10 || ranked[1].TxIndex != 2 || ranked[1].Nonce != 102 {
		t.Fatalf("rank 2 mismatch: expected A (block 10, tx 2, nonce 102), got %+v", ranked[1])
	}
}

// TestSelectTopNByChainOrder verifies that solutions are strictly selected in
// (blockNumber asc, txIndex asc) order up to n.
func TestSelectTopNByChainOrder(t *testing.T) {
	cands := []CandidateID{"c1", "c2", "c3", "c4", "c5"}
	solutions := []Solution{
		{Epoch: 1, CandidateID: cands[0], BlockNumber: 10, TxIndex: 1},
		{Epoch: 1, CandidateID: cands[1], BlockNumber: 10, TxIndex: 3},
		{Epoch: 1, CandidateID: cands[2], BlockNumber: 11, TxIndex: 0},
		{Epoch: 1, CandidateID: cands[3], BlockNumber: 11, TxIndex: 5},
		{Epoch: 1, CandidateID: cands[4], BlockNumber: 12, TxIndex: 0},
	}

	// Select top 3
	top3, err := SelectTopN(solutions, 3)
	if err != nil {
		t.Fatalf("SelectTopN(3) error: %v", err)
	}
	if len(top3) != 3 {
		t.Fatalf("expected 3, got %d", len(top3))
	}
	expectedTop3 := []CandidateID{"c1", "c2", "c3"}
	for i, sol := range top3 {
		if sol.CandidateID != expectedTop3[i] {
			t.Errorf("top3[%d] = %s, want %s", i, sol.CandidateID, expectedTop3[i])
		}
	}

	// Error on requesting more than available
	if _, err := SelectTopN(solutions, 6); !errors.Is(err, ErrInsufficientCandidates) {
		t.Fatalf("expected ErrInsufficientCandidates for n=6, got %v", err)
	}

	// Error on invalid committee size <= 0
	if _, err := SelectTopN(solutions, 0); !errors.Is(err, ErrInvalidCommitteeSize) {
		t.Fatalf("expected ErrInvalidCommitteeSize for n=0, got %v", err)
	}
	if _, err := SelectTopN(solutions, -1); !errors.Is(err, ErrInvalidCommitteeSize) {
		t.Fatalf("expected ErrInvalidCommitteeSize for n=-1, got %v", err)
	}
}

// TestPermutationInvariance verifies that shuffling input solutions does NOT change
// the election result (winners, members, committeeID, indices).
func TestPermutationInvariance(t *testing.T) {
	// Create multiple candidates with multiple solutions across blocks
	rawSolutions := []Solution{
		{Epoch: 1, CandidateID: "node-gamma", BlockNumber: 15, TxIndex: 1, Nonce: 1},
		{Epoch: 1, CandidateID: "node-gamma", BlockNumber: 12, TxIndex: 0, Nonce: 2}, // earliest for gamma
		{Epoch: 1, CandidateID: "node-alpha", BlockNumber: 10, TxIndex: 3, Nonce: 3}, // earliest for alpha
		{Epoch: 1, CandidateID: "node-alpha", BlockNumber: 11, TxIndex: 0, Nonce: 4},
		{Epoch: 1, CandidateID: "node-beta", BlockNumber: 10, TxIndex: 1, Nonce: 5},    // earliest for beta
		{Epoch: 1, CandidateID: "node-delta", BlockNumber: 14, TxIndex: 2, Nonce: 6},   // earliest for delta
		{Epoch: 1, CandidateID: "node-epsilon", BlockNumber: 16, TxIndex: 0, Nonce: 7}, // earliest for epsilon
	}

	n := 3
	threshold := 1
	epoch := uint64(1)
	activateAt := uint64(20)

	// Baseline run
	baselineResult, err := ElectCommittee(rawSolutions, n, threshold, epoch, activateAt, nil)
	if err != nil {
		t.Fatalf("ElectCommittee baseline error: %v", err)
	}

	// Expected winners by chain order:
	// 1. node-beta (10, 1)
	// 2. node-alpha (10, 3)
	// 3. node-gamma (12, 0)
	expectedWinners := []CandidateID{"node-beta", "node-alpha", "node-gamma"}
	for i, w := range baselineResult.RankedWinners {
		if w.CandidateID != expectedWinners[i] {
			t.Errorf("baseline winner[%d] = %s, want %s", i, w.CandidateID, expectedWinners[i])
		}
	}

	// Expected committee members sorted by NodeID:
	// alpha, beta, gamma
	expectedMembers := []NodeID{"node-alpha", "node-beta", "node-gamma"}
	if !reflect.DeepEqual(baselineResult.Descriptor.Members, expectedMembers) {
		t.Fatalf("baseline members mismatch: have %v, want %v", baselineResult.Descriptor.Members, expectedMembers)
	}

	// Test 50 different random permutations of the input slice
	rng := rand.New(rand.NewSource(12345))
	for round := 0; round < 50; round++ {
		shuffled := make([]Solution, len(rawSolutions))
		copy(shuffled, rawSolutions)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		permResult, err := ElectCommittee(shuffled, n, threshold, epoch, activateAt, nil)
		if err != nil {
			t.Fatalf("ElectCommittee permutation %d error: %v", round, err)
		}

		// Verify CommitteeID is identical
		if permResult.Descriptor.CommitteeID != baselineResult.Descriptor.CommitteeID {
			t.Fatalf("permutation %d: CommitteeID mismatch: have %s, want %s",
				round, permResult.Descriptor.CommitteeID.Hex(), baselineResult.Descriptor.CommitteeID.Hex())
		}

		// Verify Members are identical
		if !reflect.DeepEqual(permResult.Descriptor.Members, baselineResult.Descriptor.Members) {
			t.Fatalf("permutation %d: Members mismatch", round)
		}

		// Verify RankedWinners are identical
		for i := range baselineResult.RankedWinners {
			if permResult.RankedWinners[i].CandidateID != baselineResult.RankedWinners[i].CandidateID ||
				permResult.RankedWinners[i].BlockNumber != baselineResult.RankedWinners[i].BlockNumber ||
				permResult.RankedWinners[i].TxIndex != baselineResult.RankedWinners[i].TxIndex {
				t.Fatalf("permutation %d: RankedWinners[%d] mismatch", round, i)
			}
		}

		// Verify Indices are identical
		if !reflect.DeepEqual(permResult.Indices, baselineResult.Indices) {
			t.Fatalf("permutation %d: Indices mismatch", round)
		}
	}
}

// TestNodeIDIndexSeparationAndStability verifies that election rank is decoupled from
// committee index, and that 1-based index is stable and strictly sorted by NodeID.
func TestNodeIDIndexSeparationAndStability(t *testing.T) {
	// Candidate A has NodeID "zebra" and wins 1st place in election
	// Candidate B has NodeID "alpha" and wins 2nd place in election
	// Candidate C has NodeID "middle" and wins 3rd place in election
	solutions := []Solution{
		{Epoch: 1, CandidateID: "cand-Z", BlockNumber: 10, TxIndex: 0},
		{Epoch: 1, CandidateID: "cand-A", BlockNumber: 10, TxIndex: 5},
		{Epoch: 1, CandidateID: "cand-M", BlockNumber: 11, TxIndex: 0},
	}

	candToNode := map[CandidateID]NodeID{
		"cand-Z": "node-zebra",
		"cand-A": "node-alpha",
		"cand-M": "node-middle",
	}

	mapper := func(cid CandidateID) (NodeID, bool) {
		nid, ok := candToNode[cid]
		return nid, ok
	}

	result, err := ElectCommittee(solutions, 3, 1, 1, 20, mapper)
	if err != nil {
		t.Fatalf("ElectCommittee error: %v", err)
	}

	// 1. Check Election Rank (RankedWinners):
	// Rank 1: cand-Z (block 10, tx 0)
	// Rank 2: cand-A (block 10, tx 5)
	// Rank 3: cand-M (block 11, tx 0)
	if result.RankedWinners[0].CandidateID != "cand-Z" {
		t.Errorf("Rank 1 expected cand-Z, got %s", result.RankedWinners[0].CandidateID)
	}
	if result.RankedWinners[1].CandidateID != "cand-A" {
		t.Errorf("Rank 2 expected cand-A, got %s", result.RankedWinners[1].CandidateID)
	}
	if result.RankedWinners[2].CandidateID != "cand-M" {
		t.Errorf("Rank 3 expected cand-M, got %s", result.RankedWinners[2].CandidateID)
	}

	// 2. Check Committee Index:
	// Sorted NodeIDs:
	// 1: node-alpha (index 1)
	// 2: node-middle (index 2)
	// 3: node-zebra (index 3)
	expectedMembers := []NodeID{"node-alpha", "node-middle", "node-zebra"}
	if !reflect.DeepEqual(result.Descriptor.Members, expectedMembers) {
		t.Fatalf("Committee Members mismatch: have %v, want %v", result.Descriptor.Members, expectedMembers)
	}

	if idx, ok := result.MemberIndex("node-alpha"); !ok || idx != 1 {
		t.Errorf("expected node-alpha index 1, got %d (ok=%v)", idx, ok)
	}
	if idx, ok := result.MemberIndex("node-middle"); !ok || idx != 2 {
		t.Errorf("expected node-middle index 2, got %d (ok=%v)", idx, ok)
	}
	if idx, ok := result.MemberIndex("node-zebra"); !ok || idx != 3 {
		t.Errorf("expected node-zebra index 3, got %d (ok=%v)", idx, ok)
	}

	// Unknown node should return (0, false)
	if idx, ok := result.MemberIndex("node-unknown"); ok || idx != 0 {
		t.Errorf("expected node-unknown not found, got %d (ok=%v)", idx, ok)
	}

	// Descriptor methods: IndexToMember
	if m, ok := result.Descriptor.IndexToMember(1); !ok || m != "node-alpha" {
		t.Errorf("IndexToMember(1) = %s (ok=%v), want node-alpha", m, ok)
	}
	if m, ok := result.Descriptor.IndexToMember(3); !ok || m != "node-zebra" {
		t.Errorf("IndexToMember(3) = %s (ok=%v), want node-zebra", m, ok)
	}
	if _, ok := result.Descriptor.IndexToMember(0); ok {
		t.Errorf("IndexToMember(0) should return false")
	}
	if _, ok := result.Descriptor.IndexToMember(4); ok {
		t.Errorf("IndexToMember(4) should return false")
	}

	// Separation confirmation:
	// cand-Z has election rank 1 (index 0 in RankedWinners), but committee index 3!
	// cand-A has election rank 2 (index 1 in RankedWinners), but committee index 1!
	zebraWinnerRank := 1
	zebraCommIdx, _ := result.MemberIndex("node-zebra")
	if zebraWinnerRank == zebraCommIdx {
		t.Errorf("expected election rank (%d) != committee index (%d) for zebra", zebraWinnerRank, zebraCommIdx)
	}
}

// TestBoundaryCalculation verifies epoch boundary calculation and open interval scan ranges.
func TestBoundaryCalculation(t *testing.T) {
	initialBoundary := uint64(100)
	epochLength := uint64(50)

	// CalcNextBoundary
	if next := CalcNextBoundary(100, 50); next != 150 {
		t.Errorf("CalcNextBoundary(100, 50) = %d, want 150", next)
	}

	// CalcBoundaryBlock
	if b0 := CalcBoundaryBlock(initialBoundary, 0, epochLength); b0 != 100 {
		t.Errorf("CalcBoundaryBlock(0) = %d, want 100", b0)
	}
	if b1 := CalcBoundaryBlock(initialBoundary, 1, epochLength); b1 != 150 {
		t.Errorf("CalcBoundaryBlock(1) = %d, want 150", b1)
	}
	if b2 := CalcBoundaryBlock(initialBoundary, 2, epochLength); b2 != 200 {
		t.Errorf("CalcBoundaryBlock(2) = %d, want 200", b2)
	}

	// IsBoundaryBlock:
	// block 100 (initial boundary): is base, not finalized epoch
	isB, ep := IsBoundaryBlock(100, initialBoundary, epochLength)
	if isB {
		t.Errorf("IsBoundaryBlock(100) should be false (base), got true")
	}

	// block 150: finalizes epoch 1
	isB, ep = IsBoundaryBlock(150, initialBoundary, epochLength)
	if !isB || ep != 1 {
		t.Errorf("IsBoundaryBlock(150) = (%v, %d), want (true, 1)", isB, ep)
	}

	// block 200: finalizes epoch 2
	isB, ep = IsBoundaryBlock(200, initialBoundary, epochLength)
	if !isB || ep != 2 {
		t.Errorf("IsBoundaryBlock(200) = (%v, %d), want (true, 2)", isB, ep)
	}

	// non-boundary blocks
	for _, blk := range []uint64{99, 101, 149, 151, 199, 201} {
		if isB, _ := IsBoundaryBlock(blk, initialBoundary, epochLength); isB {
			t.Errorf("IsBoundaryBlock(%d) should be false", blk)
		}
	}

	// CalcEpoch
	testEpochs := []struct {
		block uint64
		epoch uint64
	}{
		{100, 0},
		{101, 1},
		{125, 1},
		{150, 1}, // block 150 is the boundary completing epoch 1
		{151, 2},
		{175, 2},
		{200, 2}, // block 200 is the boundary completing epoch 2
		{201, 3},
	}
	for _, tc := range testEpochs {
		gotEpoch, err := CalcEpoch(tc.block, initialBoundary, epochLength)
		if err != nil {
			t.Errorf("CalcEpoch(%d) error: %v", tc.block, err)
		}
		if gotEpoch != tc.epoch {
			t.Errorf("CalcEpoch(%d) = %d, want %d", tc.block, gotEpoch, tc.epoch)
		}
	}

	// CalcEpoch before initialBoundary should fail
	if _, err := CalcEpoch(99, initialBoundary, epochLength); err == nil {
		t.Errorf("CalcEpoch(99) should return error for block < initialBoundary")
	}

	// ScanRange for (100, 150) -> [101, 149]
	from, to, err := ScanRange(100, 150)
	if err != nil {
		t.Fatalf("ScanRange(100, 150) error: %v", err)
	}
	if from != 101 || to != 149 {
		t.Errorf("ScanRange(100, 150) = [%d, %d], want [101, 149]", from, to)
	}

	// ScanRange for (100, 101) -> empty interval -> ErrEmptyScanRange
	if _, _, err := ScanRange(100, 101); !errors.Is(err, ErrEmptyScanRange) {
		t.Errorf("ScanRange(100, 101) want ErrEmptyScanRange, got %v", err)
	}

	// ScanRange for invalid inputs (current <= previous)
	if _, _, err := ScanRange(150, 100); !errors.Is(err, ErrInvalidScanRange) {
		t.Errorf("ScanRange(150, 100) want ErrInvalidScanRange, got %v", err)
	}
	if _, _, err := ScanRange(100, 100); !errors.Is(err, ErrInvalidScanRange) {
		t.Errorf("ScanRange(100, 100) want ErrInvalidScanRange, got %v", err)
	}

	// IsInScanRange open interval check
	if IsInScanRange(100, 100, 150) {
		t.Errorf("IsInScanRange(100) should be false (left boundary)")
	}
	if !IsInScanRange(101, 100, 150) {
		t.Errorf("IsInScanRange(101) should be true")
	}
	if !IsInScanRange(149, 100, 150) {
		t.Errorf("IsInScanRange(149) should be true")
	}
	if IsInScanRange(150, 100, 150) {
		t.Errorf("IsInScanRange(150) should be false (right boundary)")
	}
}

// TestPoWDigestAndVerification tests PoW digest computation, verification, and filtering.
func TestPoWDigestAndVerification(t *testing.T) {
	tau := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	epoch := uint64(5)
	candID := CandidateID("node-miner-1")
	pk := []byte("mock-public-key-bytes-32-chars-!")
	nonce := uint64(1234567)

	digest := ComputePowDigest(tau, epoch, candID, pk, nonce)

	// Digest must be deterministic
	digest2 := ComputePowDigest(tau, epoch, candID, pk, nonce)
	if digest != digest2 {
		t.Fatalf("ComputePowDigest not deterministic")
	}

	// Modifying any component changes the digest
	diffTau := ComputePowDigest(common.HexToHash("0x1234"), epoch, candID, pk, nonce)
	diffEpoch := ComputePowDigest(tau, epoch+1, candID, pk, nonce)
	diffCand := ComputePowDigest(tau, epoch, "other-node", pk, nonce)
	diffPK := ComputePowDigest(tau, epoch, candID, []byte("different-pk"), nonce)
	diffNonce := ComputePowDigest(tau, epoch, candID, pk, nonce+1)

	for _, d := range []common.Hash{diffTau, diffEpoch, diffCand, diffPK, diffNonce} {
		if d == digest {
			t.Fatalf("ComputePowDigest produced collision on modified parameter")
		}
	}

	// Valid solution with digest <= target
	target := common.BigToHash(new(big.Int).Add(new(big.Int).SetBytes(digest[:]), big.NewInt(10)))
	solValid := Solution{
		Epoch:       epoch,
		CandidateID: candID,
		ElectionPK:  pk,
		Nonce:       nonce,
		PowDigest:   digest,
		BlockNumber: 100,
		TxIndex:     1,
	}
	if !VerifySolution(&solValid, tau, target) {
		t.Errorf("VerifySolution failed for valid solution")
	}

	// Falsified digest in solution struct
	solTampered := solValid.Clone()
	solTampered.PowDigest = common.HexToHash("0xdeadbeef")
	if VerifySolution(&solTampered, tau, target) {
		t.Errorf("VerifySolution passed for tampered digest")
	}

	// Zero digest in solution struct (calldata requires exact match)
	solZero := solValid.Clone()
	solZero.PowDigest = common.Hash{}
	if VerifySolution(&solZero, tau, target) {
		t.Errorf("VerifySolution passed for zero digest (fixed calldata requires exact match)")
	}

	// Target too strict (digest > target)
	strictTarget := common.BigToHash(new(big.Int).Sub(new(big.Int).SetBytes(digest[:]), big.NewInt(1)))
	if VerifySolution(&solValid, tau, strictTarget) {
		t.Errorf("VerifySolution passed when digest > strictTarget")
	}

	// FilterValidSolutions
	solOther := Solution{
		Epoch:       epoch,
		CandidateID: "miner-other",
		ElectionPK:  pk,
		Nonce:       999999,
		BlockNumber: 101,
		TxIndex:     0,
	}
	solOther.PowDigest = ComputePowDigest(tau, epoch, solOther.CandidateID, pk, solOther.Nonce)

	// Determine which solution has smaller digest
	var smallerSol, largerSol Solution
	if bytes.Compare(solValid.PowDigest[:], solOther.PowDigest[:]) < 0 {
		smallerSol, largerSol = solValid, solOther
	} else {
		smallerSol, largerSol = solOther, solValid
	}

	// Set target exactly equal to smaller digest so larger one fails
	exactTarget := smallerSol.PowDigest
	validList := FilterValidSolutions([]Solution{smallerSol, largerSol}, tau, exactTarget)
	if len(validList) != 1 || validList[0].CandidateID != smallerSol.CandidateID {
		t.Fatalf("FilterValidSolutions expected 1 valid solution, got %d", len(validList))
	}
}

// TestComputeCommitteeIDMultiNodeConsistency verifies that multiple nodes evaluating
// identical inputs produce identical CommitteeID regardless of order.
func TestComputeCommitteeIDMultiNodeConsistency(t *testing.T) {
	epoch := uint64(3)
	threshold := 2
	activateAt := uint64(500)
	members := []NodeID{"enode://node3", "enode://node1", "enode://node2", "enode://node5", "enode://node4"}

	// Node A computes with members in one order
	idA := ComputeCommitteeID(epoch, members, threshold, activateAt)

	// Node B computes with members in reverse order
	revMembers := []NodeID{"enode://node4", "enode://node5", "enode://node2", "enode://node1", "enode://node3"}
	idB := ComputeCommitteeID(epoch, revMembers, threshold, activateAt)

	// Node C computes with members pre-sorted
	sortedMembers := []NodeID{"enode://node1", "enode://node2", "enode://node3", "enode://node4", "enode://node5"}
	idC := ComputeCommitteeID(epoch, sortedMembers, threshold, activateAt)

	if idA != idB || idB != idC {
		t.Fatalf("ComputeCommitteeID inconsistency across nodes: idA=%s, idB=%s, idC=%s",
			idA.Hex(), idB.Hex(), idC.Hex())
	}
}

// TestBuildCommitteeErrors verifies validation errors in BuildCommittee.
func TestBuildCommitteeErrors(t *testing.T) {
	winners := []Solution{
		{Epoch: 1, CandidateID: "c1"},
		{Epoch: 1, CandidateID: "c2"},
	}

	// Negative threshold
	if _, err := BuildCommittee(winners, 1, -1, 100, nil); !errors.Is(err, ErrInvalidThreshold) {
		t.Errorf("expected ErrInvalidThreshold for threshold -1, got %v", err)
	}

	// Threshold >= committee size
	if _, err := BuildCommittee(winners, 1, 2, 100, nil); !errors.Is(err, ErrInvalidThreshold) {
		t.Errorf("expected ErrInvalidThreshold for threshold 2 with size 2, got %v", err)
	}

	// Candidate not mapped
	failMapper := func(cid CandidateID) (NodeID, bool) {
		return "", false
	}
	if _, err := BuildCommittee(winners, 1, 1, 100, failMapper); !errors.Is(err, ErrCandidateNotMapped) {
		t.Errorf("expected ErrCandidateNotMapped, got %v", err)
	}

	// Multiple candidates mapping to duplicate NodeID
	dupMapper := func(cid CandidateID) (NodeID, bool) {
		return "node-same", true
	}
	if _, err := BuildCommittee(winners, 1, 1, 100, dupMapper); !errors.Is(err, ErrDuplicateMappedNodeID) {
		t.Errorf("expected ErrDuplicateMappedNodeID, got %v", err)
	}
}
