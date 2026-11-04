package eth

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type mockBlockReader struct {
	blocks map[uint64]*types.Block
}

func newMockBlockReader() *mockBlockReader {
	return &mockBlockReader{blocks: make(map[uint64]*types.Block)}
}

func (m *mockBlockReader) GetBlockByNumber(number uint64) *types.Block {
	return m.blocks[number]
}

func (m *mockBlockReader) addBlock(number uint64, txs ...*types.Transaction) *types.Block {
	header := &types.Header{
		Number: big.NewInt(int64(number)),
		Extra:  []byte(fmt.Sprintf("block-%d", number)),
	}
	block := types.NewBlock(header, txs, nil, nil)
	m.blocks[number] = block
	return block
}

func helperCreateValidSolutionTx(
	t *testing.T,
	nonce uint64,
	to common.Address,
	epoch uint64,
	candidateID CandidateID,
	pk []byte,
	tau common.Hash,
) *types.Transaction {
	t.Helper()
	powNonce := uint64(777)
	digest := ComputePowDigest(tau, epoch, candidateID, pk, powNonce)
	sol := &Solution{
		Epoch:       epoch,
		CandidateID: candidateID,
		ElectionPK:  pk,
		Nonce:       powNonce,
		PowDigest:   digest,
	}
	tx, err := NewSolutionTransaction(nonce, to, 100000, big.NewInt(1), sol)
	if err != nil {
		t.Fatalf("failed to create solution transaction: %v", err)
	}
	return tx
}

func TestScanIntervalBoundaries(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	epoch := uint64(1)
	tau := DeriveTau(common.HexToHash("0x1111"), epoch)
	target := MaxTarget

	// Previous boundary = 10, Current boundary = 20
	// Open interval (10, 20) -> blocks 11..19
	prevBoundary := uint64(10)
	currBoundary := uint64(20)

	// Populate blocks 10..20
	for b := prevBoundary; b <= currBoundary; b++ {
		reader.addBlock(b)
	}

	// 1. Place a solution in block 10 (previous boundary) -> MUST NOT be picked up
	tx10 := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand-10", []byte{1}, tau)
	reader.addBlock(prevBoundary, tx10)

	// 2. Place a solution in block 20 (current boundary) -> MUST NOT be picked up
	tx20 := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand-20", []byte{2}, tau)
	reader.addBlock(currBoundary, tx20)

	// 3. Place solutions in blocks 11, 15, 19 (within open interval) -> MUST be picked up
	tx11 := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand-11", []byte{3}, tau)
	reader.addBlock(11, tx11)

	tx15 := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand-15", []byte{4}, tau)
	reader.addBlock(15, tx15)

	tx19 := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand-19", []byte{5}, tau)
	reader.addBlock(19, tx19)

	sols, err := ScanEpochSolutions(reader, prevBoundary, currBoundary, epoch, tau, target, systemAddr, nil)
	if err != nil {
		t.Fatalf("ScanEpochSolutions failed: %v", err)
	}

	if len(sols) != 3 {
		t.Fatalf("expected 3 valid solutions, got %d", len(sols))
	}

	if sols[0].CandidateID != "cand-11" || sols[0].BlockNumber != 11 || sols[0].TxIndex != 0 {
		t.Errorf("sol[0] mismatch: got %+v", sols[0])
	}
	if sols[1].CandidateID != "cand-15" || sols[1].BlockNumber != 15 || sols[1].TxIndex != 0 {
		t.Errorf("sol[1] mismatch: got %+v", sols[1])
	}
	if sols[2].CandidateID != "cand-19" || sols[2].BlockNumber != 19 || sols[2].TxIndex != 0 {
		t.Errorf("sol[2] mismatch: got %+v", sols[2])
	}

	// 4. Test empty scan range (e.g. 10 to 11 has no blocks in open interval)
	_, err = ScanEpochSolutions(reader, 10, 11, epoch, tau, target, systemAddr, nil)
	if err != ErrEmptyScanRange {
		t.Errorf("expected ErrEmptyScanRange, got %v", err)
	}

	// 5. Test invalid scan range (current <= previous)
	_, err = ScanEpochSolutions(reader, 10, 10, epoch, tau, target, systemAddr, nil)
	if err != ErrInvalidScanRange {
		t.Errorf("expected ErrInvalidScanRange for 10->10, got %v", err)
	}
	_, err = ScanEpochSolutions(reader, 10, 9, epoch, tau, target, systemAddr, nil)
	if err != ErrInvalidScanRange {
		t.Errorf("expected ErrInvalidScanRange for 10->9, got %v", err)
	}

	// 6. Test missing block in reader
	delete(reader.blocks, 15)
	_, err = ScanEpochSolutions(reader, prevBoundary, currBoundary, epoch, tau, target, systemAddr, nil)
	if err == nil {
		t.Error("expected error for missing block 15, got nil")
	}
}

func TestScanEpochSolutionsNonSystemTxIgnored(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	randomAddr := common.HexToAddress("0x9999999999999999999999999999999999999999")
	epoch := uint64(1)
	tau := DeriveTau(common.HexToHash("0x2222"), epoch)
	target := MaxTarget

	// Block with multiple transactions:
	// 0: Normal transfer to random address
	txRandom := types.NewTransaction(0, randomAddr, big.NewInt(100), 21000, big.NewInt(1), nil)

	// 1: Contract creation tx (to == nil)
	txContract := types.NewContractCreation(1, big.NewInt(0), 100000, big.NewInt(1), []byte{0x60, 0x60})

	// 2: Tx to system address with malformed/garbage calldata
	txGarbage := types.NewTransaction(2, systemAddr, big.NewInt(0), 100000, big.NewInt(1), []byte("malformed-not-pow"))

	// 3: Valid solution tx to system address
	txValid := helperCreateValidSolutionTx(t, 3, systemAddr, epoch, "valid-node", []byte{0xaa}, tau)

	reader.addBlock(1, txRandom, txContract, txGarbage, txValid)

	sols, err := ScanEpochSolutions(reader, 0, 2, epoch, tau, target, systemAddr, nil)
	if err != nil {
		t.Fatalf("ScanEpochSolutions failed: %v", err)
	}

	if len(sols) != 1 {
		t.Fatalf("expected exactly 1 solution, got %d", len(sols))
	}
	if sols[0].CandidateID != "valid-node" {
		t.Errorf("expected candidate valid-node, got %s", sols[0].CandidateID)
	}
	if sols[0].BlockNumber != 1 || sols[0].TxIndex != 3 {
		t.Errorf("expected BlockNumber 1 and TxIndex 3, got block=%d, tx=%d", sols[0].BlockNumber, sols[0].TxIndex)
	}
}

func TestScanEpochSolutionsValidation(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	epoch := uint64(1)
	tau := DeriveTau(common.HexToHash("0x3333"), epoch)

	// 1. Solution with wrong epoch (epoch 2 instead of epoch 1)
	txWrongEpoch := helperCreateValidSolutionTx(t, 0, systemAddr, 2, "cand-wrong-epoch", []byte{1}, tau)

	// 2. Solution with invalid PoW digest (tampered)
	solBadDigest := &Solution{
		Epoch:       epoch,
		CandidateID: "cand-bad-digest",
		ElectionPK:  []byte{2},
		Nonce:       123,
		PowDigest:   common.HexToHash("0xbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbad0"),
	}
	dataBadDigest, err := EncodeSolutionCalldata(solBadDigest)
	if err != nil {
		t.Fatalf("EncodeSolutionCalldata failed: %v", err)
	}
	txBadDigest := types.NewTransaction(1, systemAddr, big.NewInt(0), 100000, big.NewInt(1), dataBadDigest)

	// 3. Solution where PoW does not meet target
	// We'll compute real digest, but make sure it exceeds a tiny target
	highDigestSol := &Solution{
		Epoch:       epoch,
		CandidateID: "cand-high-target",
		ElectionPK:  []byte{3},
		Nonce:       1,
	}
	highDigestSol.PowDigest = ComputePowDigest(tau, epoch, highDigestSol.CandidateID, highDigestSol.ElectionPK, highDigestSol.Nonce)
	dataHigh, _ := EncodeSolutionCalldata(highDigestSol)
	txHigh := types.NewTransaction(2, systemAddr, big.NewInt(0), 100000, big.NewInt(1), dataHigh)

	// Target that is smaller than highDigestSol.PowDigest
	veryStrictTarget := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")

	// 4. Solution with unknown candidate according to nodeMapper
	txUnknownNode := helperCreateValidSolutionTx(t, 3, systemAddr, epoch, "cand-unknown", []byte{4}, tau)

	// 5. Completely valid solution
	txValid := helperCreateValidSolutionTx(t, 4, systemAddr, epoch, "cand-valid", []byte{5}, tau)

	reader.addBlock(1, txWrongEpoch, txBadDigest, txHigh, txUnknownNode, txValid)

	// Test with MaxTarget and nodeMapper that rejects "cand-unknown"
	nodeMapper := func(c CandidateID) (NodeID, bool) {
		if c == "cand-unknown" {
			return "", false
		}
		return NodeID(c), true
	}

	sols, err := ScanEpochSolutions(reader, 0, 2, epoch, tau, MaxTarget, systemAddr, nodeMapper)
	if err != nil {
		t.Fatalf("ScanEpochSolutions failed: %v", err)
	}

	// Should accept txHigh (as MaxTarget permits it) and txValid, but reject wrong epoch, bad digest, and unknown node
	if len(sols) != 2 {
		t.Fatalf("expected 2 solutions, got %d", len(sols))
	}
	if sols[0].CandidateID != "cand-high-target" || sols[1].CandidateID != "cand-valid" {
		t.Errorf("unexpected solutions: %+v", sols)
	}

	// Now re-run with strict target that rejects txHigh
	strictSols, err := ScanEpochSolutions(reader, 0, 2, epoch, tau, veryStrictTarget, systemAddr, nodeMapper)
	if err != nil {
		t.Fatalf("ScanEpochSolutions with strict target failed: %v", err)
	}
	// Neither txHigh nor txValid meets 0x00...01
	if len(strictSols) != 0 {
		t.Errorf("expected 0 solutions with strict target, got %d", len(strictSols))
	}
}

func TestScanAndElectEndToEnd(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	epoch := uint64(1)
	tau := DeriveTau(common.HexToHash("0x4444"), epoch)

	// 5 candidates submit solutions in blocks 1..5
	// cand1 submits twice: block 1 tx 0, and block 3 tx 0.
	tx1a := helperCreateValidSolutionTx(t, 0, systemAddr, epoch, "cand1", []byte{1}, tau)
	tx2 := helperCreateValidSolutionTx(t, 0, systemAddr, epoch, "cand2", []byte{2}, tau)
	tx1b := helperCreateValidSolutionTx(t, 1, systemAddr, epoch, "cand1", []byte{1}, tau)
	tx3 := helperCreateValidSolutionTx(t, 0, systemAddr, epoch, "cand3", []byte{3}, tau)
	tx4 := helperCreateValidSolutionTx(t, 0, systemAddr, epoch, "cand4", []byte{4}, tau)

	reader.addBlock(1, tx1a)
	reader.addBlock(2, tx2)
	reader.addBlock(3, tx1b)
	reader.addBlock(4, tx3)
	reader.addBlock(5, tx4)

	cfg := ElectionConfig{
		EpochLength:     6,
		InitialBoundary: 0,
		CommitteeSize:   3,
		Threshold:       2,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	res, err := ScanAndElect(reader, 0, 6, epoch, tau, cfg, 6, nil)
	if err != nil {
		t.Fatalf("ScanAndElect failed: %v", err)
	}

	// Ranked winners must be cand1 (block 1), cand2 (block 2), cand3 (block 4)
	if len(res.RankedWinners) != 3 {
		t.Fatalf("expected 3 ranked winners, got %d", len(res.RankedWinners))
	}
	if res.RankedWinners[0].CandidateID != "cand1" || res.RankedWinners[0].BlockNumber != 1 {
		t.Errorf("expected winner 0 to be cand1 from block 1, got %+v", res.RankedWinners[0])
	}
	if res.RankedWinners[1].CandidateID != "cand2" || res.RankedWinners[1].BlockNumber != 2 {
		t.Errorf("expected winner 1 to be cand2 from block 2, got %+v", res.RankedWinners[1])
	}
	if res.RankedWinners[2].CandidateID != "cand3" || res.RankedWinners[2].BlockNumber != 4 {
		t.Errorf("expected winner 2 to be cand3 from block 4, got %+v", res.RankedWinners[2])
	}

	// CommitteeDescriptor members sorted: cand1, cand2, cand3
	if len(res.Descriptor.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(res.Descriptor.Members))
	}
	if res.Descriptor.Members[0] != "cand1" || res.Descriptor.Members[1] != "cand2" || res.Descriptor.Members[2] != "cand3" {
		t.Errorf("descriptor members mismatch: %v", res.Descriptor.Members)
	}
	if res.Descriptor.Threshold != 2 {
		t.Errorf("descriptor threshold mismatch: got %d, want 2", res.Descriptor.Threshold)
	}
	if res.Descriptor.ActivateAt != 6 {
		t.Errorf("descriptor activateAt mismatch: got %d, want 6", res.Descriptor.ActivateAt)
	}
}
