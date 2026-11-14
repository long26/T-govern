package eth

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// fakeTxPool records submitted transactions and provides nonces.
type fakeTxPool struct {
	mu      sync.Mutex
	txs     []*types.Transaction
	nonces  map[common.Address]uint64
	failErr error
}

func newFakeTxPool() *fakeTxPool {
	return &fakeTxPool{
		nonces: make(map[common.Address]uint64),
	}
}

func (p *fakeTxPool) SubmitTx(tx *types.Transaction) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failErr != nil {
		return p.failErr
	}
	p.txs = append(p.txs, tx)
	from, _ := types.Sender(types.HomesteadSigner{}, tx)
	p.nonces[from] = tx.Nonce() + 1
	return nil
}

func (p *fakeTxPool) GetNonce(addr common.Address) (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nonces[addr], nil
}

func (p *fakeTxPool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.txs)
}

func (p *fakeTxPool) Transactions() []*types.Transaction {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]*types.Transaction, len(p.txs))
	copy(cp, p.txs)
	return cp
}

// helper to create a candidate with a generated private key
func createTestCandidate(t *testing.T, id CandidateID, pkBytes []byte) (*PowCandidate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)
	signer := NewPrivateKeySigner(key, types.HomesteadSigner{})
	cand := &PowCandidate{
		CandidateID: id,
		ElectionPK:  pkBytes,
		Address:     addr,
		Signer:      signer,
	}
	return cand, key
}

// 1. Test signed tx sender verification
func TestPowWorkerSignedTxSender(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-sender-1", []byte("election-pk-1"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
		GasLimit:      100000,
		GasPrice:      big.NewInt(1000),
	}

	worker := NewPowWorker(cfg, pool, pool)
	if err := worker.AddCandidate(*cand); err != nil {
		t.Fatalf("AddCandidate failed: %v", err)
	}

	ev := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 10,
		NextTau:         common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}

	tx := txs[0]
	// Verify sender recovery
	signer := types.HomesteadSigner{}
	sender, err := types.Sender(signer, tx)
	if err != nil {
		t.Fatalf("failed to recover sender from signed tx: %v", err)
	}
	if sender != cand.Address {
		t.Errorf("sender mismatch: got %s, want %s", sender.Hex(), cand.Address.Hex())
	}

	// Verify not unsigned (V, R, S != 0)
	v, r, s := tx.RawSignatureValues()
	if v == nil || r == nil || s == nil || v.Sign() == 0 || r.Sign() == 0 || s.Sign() == 0 {
		t.Fatal("transaction signature values are zero (unsigned tx detected)")
	}
}

// 2. Test system address
func TestPowWorkerSystemAddress(t *testing.T) {
	customSystemAddr := common.HexToAddress("0x000000000000000000000000000000000000cafe")
	cand, _ := createTestCandidate(t, "cand-sys-1", []byte("election-pk-sys"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: customSystemAddr,
		GasLimit:      80000,
		GasPrice:      big.NewInt(0),
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	ev := &BoundaryEvent{
		FinalizedEpoch:  1,
		TargetEpoch:     2,
		CurrentBoundary: 20,
		NextTau:         common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}

	tx := txs[0]
	if tx.To() == nil {
		t.Fatal("tx.To() is nil (contract creation instead of message call)")
	}
	if *tx.To() != customSystemAddr {
		t.Errorf("tx.To mismatch: got %s, want %s", tx.To().Hex(), customSystemAddr.Hex())
	}
}

// 3. Test calldata roundtrip
func TestPowWorkerCalldataRoundtrip(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-calldata-test", []byte("candidate-pk-bytes-12345"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	targetEpoch := uint64(5)
	nextTau := common.HexToHash("0xabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd")
	ev := &BoundaryEvent{
		FinalizedEpoch:  4,
		TargetEpoch:     targetEpoch,
		CurrentBoundary: 50,
		NextTau:         nextTau,
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}

	decodedSol, err := DecodeSolutionCalldata(txs[0].Data())
	if err != nil {
		t.Fatalf("DecodeSolutionCalldata failed: %v", err)
	}

	if decodedSol.Epoch != targetEpoch {
		t.Errorf("decoded Epoch mismatch: got %d, want %d", decodedSol.Epoch, targetEpoch)
	}
	if decodedSol.CandidateID != cand.CandidateID {
		t.Errorf("decoded CandidateID mismatch: got %s, want %s", decodedSol.CandidateID, cand.CandidateID)
	}
	if string(decodedSol.ElectionPK) != string(cand.ElectionPK) {
		t.Errorf("decoded ElectionPK mismatch: got %x, want %x", decodedSol.ElectionPK, cand.ElectionPK)
	}
	expectedDigest := ComputePowDigest(nextTau, targetEpoch, cand.CandidateID, cand.ElectionPK, decodedSol.Nonce)
	if decodedSol.PowDigest != expectedDigest {
		t.Errorf("decoded PowDigest mismatch: got %s, want %s", decodedSol.PowDigest.Hex(), expectedDigest.Hex())
	}
}

// 4. Test candidate and tau validation with non-trivial target
func TestPowWorkerCandidateAndTau(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-pow-1", []byte("election-pk-pow"))
	pool := newFakeTxPool()

	// Use a moderately restricted target (e.g. leading byte <= 0x0f)
	target := common.HexToHash("0x0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        target,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	targetEpoch := uint64(3)
	nextTau := common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999")
	ev := &BoundaryEvent{
		FinalizedEpoch:  2,
		TargetEpoch:     targetEpoch,
		CurrentBoundary: 30,
		NextTau:         nextTau,
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}

	sol, err := DecodeSolutionCalldata(txs[0].Data())
	if err != nil {
		t.Fatalf("DecodeSolutionCalldata failed: %v", err)
	}

	// Verify solution satisfies VerifySolution
	if !VerifySolution(sol, nextTau, target) {
		t.Fatalf("mined solution failed VerifySolution against nextTau and target")
	}
}

// 5. Test submit 1 time per epoch per candidate
func TestPowWorkerSubmitOncePerEpoch(t *testing.T) {
	cand1, _ := createTestCandidate(t, "cand-submit-1", []byte("pk1"))
	cand2, _ := createTestCandidate(t, "cand-submit-2", []byte("pk2"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand1)
	_ = worker.AddCandidate(*cand2)

	evEpoch1 := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 10,
		NextTau:         common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	// First trigger for epoch 1
	txs1, err := worker.ProcessBoundaryEvent(evEpoch1)
	if err != nil {
		t.Fatalf("first ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs1) != 2 {
		t.Fatalf("expected 2 txs for 2 candidates, got %d", len(txs1))
	}
	if pool.Count() != 2 {
		t.Fatalf("expected 2 txs in pool, got %d", pool.Count())
	}

	// Duplicate trigger for same epoch 1: MUST NOT submit again
	txsDup, err := worker.ProcessBoundaryEvent(evEpoch1)
	if err != nil {
		t.Fatalf("duplicate ProcessBoundaryEvent failed: %v", err)
	}
	if len(txsDup) != 0 {
		t.Fatalf("expected 0 txs on duplicate event, got %d", len(txsDup))
	}
	if pool.Count() != 2 {
		t.Fatalf("pool count changed on duplicate trigger: got %d, want 2", pool.Count())
	}

	// Another duplicate trigger with modified block but same TargetEpoch: MUST NOT submit again
	evEpoch1Alt := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 11,
		NextTau:         common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}
	txsAlt, err := worker.ProcessBoundaryEvent(evEpoch1Alt)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent with alt block failed: %v", err)
	}
	if len(txsAlt) != 0 {
		t.Fatalf("expected 0 txs on same target epoch, got %d", len(txsAlt))
	}
	if pool.Count() != 2 {
		t.Fatalf("pool count changed on alt trigger: got %d, want 2", pool.Count())
	}

	// Next epoch 2: SHOULD submit exactly 2 txs
	evEpoch2 := &BoundaryEvent{
		FinalizedEpoch:  1,
		TargetEpoch:     2,
		CurrentBoundary: 20,
		NextTau:         common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
	}
	txs2, err := worker.ProcessBoundaryEvent(evEpoch2)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent for epoch 2 failed: %v", err)
	}
	if len(txs2) != 2 {
		t.Fatalf("expected 2 txs for epoch 2, got %d", len(txs2))
	}
	if pool.Count() != 4 {
		t.Fatalf("expected 4 txs in pool total, got %d", pool.Count())
	}
}

// 6. Test disabled worker does not submit transactions
func TestPowWorkerDisabledByDefault(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-disabled", []byte("pk-disabled"))
	pool := newFakeTxPool()

	// Default config has Enabled: false
	cfg := DefaultPowWorkerConfig()
	if cfg.Enabled {
		t.Fatal("DefaultPowWorkerConfig must have Enabled: false")
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	ev := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 10,
		NextTau:         common.HexToHash("0x1234567812345678123456781234567812345678123456781234567812345678"),
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}
	if len(txs) != 0 {
		t.Errorf("disabled worker produced %d transactions, want 0", len(txs))
	}
	if pool.Count() != 0 {
		t.Errorf("disabled worker submitted %d transactions to pool, want 0", pool.Count())
	}
}

// 7. Test failure path (logs/returns, no panic, no complex retry)
func TestPowWorkerFailurePath(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-fail", []byte("pk-fail"))
	pool := newFakeTxPool()
	pool.failErr = errors.New("txpool is full")

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	ev := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 10,
		NextTau:         common.HexToHash("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddead"),
	}

	// Should not panic, should return error
	txs, err := worker.ProcessBoundaryEvent(ev)
	if err == nil {
		t.Fatal("expected error when pool fails, got nil")
	}
	if len(txs) != 0 {
		t.Errorf("expected 0 successfully submitted txs on error, got %d", len(txs))
	}

	// Re-submitting after failure is allowed (since it failed to submit)
	pool.failErr = nil
	txsRetry, errRetry := worker.ProcessBoundaryEvent(ev)
	if errRetry != nil {
		t.Fatalf("retry failed after pool recovered: %v", errRetry)
	}
	if len(txsRetry) != 1 {
		t.Errorf("expected 1 tx after pool recovered, got %d", len(txsRetry))
	}
}

// 8. Test fake txpool integration with ScanEpochSolutions
func TestPowWorkerFakeTxPoolIntegration(t *testing.T) {
	cand1, _ := createTestCandidate(t, "cand-int-1", []byte("pk-int-1"))
	cand2, _ := createTestCandidate(t, "cand-int-2", []byte("pk-int-2"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand1)
	_ = worker.AddCandidate(*cand2)

	targetEpoch := uint64(1)
	boundaryBlock := types.NewBlock(&types.Header{
		Number: big.NewInt(10),
		Extra:  []byte("boundary-10"),
	}, nil, nil, nil)
	nextTau := DeriveTau(boundaryBlock.Hash(), targetEpoch)

	ev := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     targetEpoch,
		CurrentBoundary: 10,
		BoundaryBlock:   boundaryBlock,
		NextTau:         nextTau,
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 txs, got %d", len(txs))
	}

	// Populate blocks 10..20 in mock reader
	mockReader := newMockBlockReader()
	for b := uint64(10); b <= 20; b++ {
		mockReader.addBlock(b)
	}
	// Place submitted transactions in block 11 (within open interval 10..20)
	mockReader.addBlock(11, txs...)

	// Now scan epoch solutions across the boundary interval (10, 20)
	solutions, scanErr := ScanEpochSolutions(
		mockReader,
		10,
		20,
		targetEpoch,
		nextTau,
		MaxTarget,
		DefaultSolutionSystemAddress,
		nil,
	)
	if scanErr != nil {
		t.Fatalf("ScanEpochSolutions failed: %v", scanErr)
	}
	if len(solutions) != 2 {
		t.Fatalf("expected 2 solutions scanned, got %d", len(solutions))
	}

	// Verify both candidates are present
	candsFound := make(map[CandidateID]bool)
	for _, s := range solutions {
		candsFound[s.CandidateID] = true
	}
	if !candsFound[cand1.CandidateID] || !candsFound[cand2.CandidateID] {
		t.Errorf("expected both candidates %s and %s in scanned solutions, got %+v",
			cand1.CandidateID, cand2.CandidateID, candsFound)
	}
}

// 9. Test CandidateFromEnv
func TestCandidateFromEnv(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	keyBytes := crypto.FromECDSA(key)
	keyHex := common.Bytes2Hex(keyBytes)

	// 1. Empty env returns ok=false, err=nil
	cand, ok, err := CandidateFromEnv(func(k string) string { return "" })
	if err != nil || ok || cand != nil {
		t.Fatalf("expected empty env to return nil, false, nil; got %v, %v, %v", cand, ok, err)
	}

	// 2. Valid env
	envMap := map[string]string{
		"NITCH_POW_CANDIDATE_ID": "cand-env-test",
		"NITCH_POW_WORKER_KEY":   keyHex,
		"NITCH_POW_ELECTION_PK":  "0xdeadbeef",
	}
	cand, ok, err = CandidateFromEnv(func(k string) string { return envMap[k] })
	if err != nil || !ok || cand == nil {
		t.Fatalf("expected valid candidate from env, got cand=%v, ok=%v, err=%v", cand, ok, err)
	}
	if cand.CandidateID != "cand-env-test" {
		t.Errorf("CandidateID: got %s, want cand-env-test", cand.CandidateID)
	}
	if common.Bytes2Hex(cand.ElectionPK) != "deadbeef" {
		t.Errorf("ElectionPK: got %x, want deadbeef", cand.ElectionPK)
	}
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey)
	if cand.Address != expectedAddr {
		t.Errorf("Address: got %s, want %s", cand.Address.Hex(), expectedAddr.Hex())
	}

	// 3. Fallback to NITCH_NODE_ID
	envMapFallback := map[string]string{
		"NITCH_NODE_ID":        "node-id-fallback",
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	cand, ok, err = CandidateFromEnv(func(k string) string { return envMapFallback[k] })
	if err != nil || !ok || cand == nil {
		t.Fatalf("fallback candidate failed: %v", err)
	}
	if cand.CandidateID != "node-id-fallback" {
		t.Errorf("CandidateID: got %s, want node-id-fallback", cand.CandidateID)
	}
}

// 10. Test ParsePowWorkerConfigFromEnv
func TestParsePowWorkerConfigFromEnv(t *testing.T) {
	// Defaults
	cfg, err := ParsePowWorkerConfigFromEnv(func(k string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error parsing defaults: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("default Enabled must be false, got true")
	}
	if cfg.GasLimit != 100000 {
		t.Errorf("default GasLimit: got %d, want 100000", cfg.GasLimit)
	}
	if cfg.GasPrice == nil || cfg.GasPrice.Cmp(big.NewInt(params.GWei)) != 0 {
		t.Errorf("default GasPrice: got %v, want %v", cfg.GasPrice, params.GWei)
	}

	// Custom overrides
	customEnv := map[string]string{
		"NITCH_POW_WORKER_ENABLED":   "true",
		"NITCH_POW_WORKER_GAS_LIMIT": "150000",
		"NITCH_POW_WORKER_GAS_PRICE": "20000000000",
		"NITCH_POW_DIFFICULTY":       "500",
		"NITCH_POW_SYSTEM_ADDRESS":   "0x0000000000000000000000000000000000009999",
	}
	cfg, err = ParsePowWorkerConfigFromEnv(func(k string) string { return customEnv[k] })
	if err != nil {
		t.Fatalf("unexpected error parsing custom env: %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled: got false, want true")
	}
	if cfg.GasLimit != 150000 {
		t.Errorf("GasLimit: got %d, want 150000", cfg.GasLimit)
	}
	if cfg.GasPrice.Cmp(big.NewInt(20000000000)) != 0 {
		t.Errorf("GasPrice: got %v, want 20000000000", cfg.GasPrice)
	}
	if cfg.SystemAddress != common.HexToAddress("0x0000000000000000000000000000000000009999") {
		t.Errorf("SystemAddress: got %s", cfg.SystemAddress.Hex())
	}
}

// 11. Test end-to-end integration via BoundaryCoordinator.AddHandler
func TestPowWorkerCoordinatorAddHandlerIntegration(t *testing.T) {
	reader := newMockBlockReader()
	for b := uint64(0); b <= 30; b++ {
		reader.addBlock(b)
	}

	electCfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   3,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   DefaultSolutionSystemAddress,
	}

	coordinator := NewBoundaryCoordinator(electCfg, reader, nil)
	pool := newFakeTxPool()

	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	worker := NewPowWorker(workerCfg, pool, pool)
	cand, _ := createTestCandidate(t, "cand-coord-int", []byte("pk-coord-int"))
	_ = worker.AddCandidate(*cand)

	// Add worker handler via AddHandler (simulating eth/backend.go)
	coordinator.AddHandler(worker.BoundaryHandler())

	// Advance chain head to block 10 (epoch 1 boundary)
	head10 := reader.GetBlockByNumber(10)
	events, err := coordinator.ProcessChainHead(head10)
	if err != nil {
		t.Fatalf("ProcessChainHead failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Verify worker submitted transaction
	if pool.Count() != 1 {
		t.Fatalf("expected 1 transaction in pool, got %d", pool.Count())
	}

	tx := pool.Transactions()[0]
	sol, err := DecodeSolutionCalldata(tx.Data())
	if err != nil {
		t.Fatalf("failed to decode calldata: %v", err)
	}
	if sol.Epoch != events[0].TargetEpoch {
		t.Errorf("target epoch mismatch: got %d, want %d", sol.Epoch, events[0].TargetEpoch)
	}
	if sol.CandidateID != cand.CandidateID {
		t.Errorf("candidate mismatch: got %s, want %s", sol.CandidateID, cand.CandidateID)
	}

	// Advance chain head to block 20 (epoch 2 boundary)
	head20 := reader.GetBlockByNumber(20)
	events2, err := coordinator.ProcessChainHead(head20)
	if err != nil {
		t.Fatalf("ProcessChainHead 2 failed: %v", err)
	}
	if len(events2) != 1 {
		t.Fatalf("expected 1 event for boundary 20, got %d", len(events2))
	}

	// Verify worker submitted second transaction for epoch 2
	if pool.Count() != 2 {
		t.Fatalf("expected 2 transactions in pool, got %d", pool.Count())
	}
	tx2 := pool.Transactions()[1]
	sol2, err := DecodeSolutionCalldata(tx2.Data())
	if err != nil {
		t.Fatalf("failed to decode calldata 2: %v", err)
	}
	if sol2.Epoch != events2[0].TargetEpoch {
		t.Errorf("target epoch mismatch: got %d, want %d", sol2.Epoch, events2[0].TargetEpoch)
	}
}

// 12. Test initial event -> solution: verifies that triggering initial epoch produces
// an epoch 1 solution transaction with correct tau, candidate, and valid signature.
func TestPowWorkerInitialEventToSolution(t *testing.T) {
	pool := newFakeTxPool()
	sysAddr := common.HexToAddress("0x0000000000000000000000000000000000001337")
	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: sysAddr,
		GasLimit:      100000,
		GasPrice:      big.NewInt(100),
	}
	worker := NewPowWorker(workerCfg, pool, pool)

	cand1, _ := createTestCandidate(t, "node-1", []byte("pk-node-1"))
	cand2, _ := createTestCandidate(t, "node-2", []byte("pk-node-2"))
	if err := worker.AddCandidate(*cand1); err != nil {
		t.Fatalf("failed to add cand1: %v", err)
	}
	if err := worker.AddCandidate(*cand2); err != nil {
		t.Fatalf("failed to add cand2: %v", err)
	}

	genesisHash := common.HexToHash("0x11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff")
	initialBoundary := uint64(0)
	expectedTau := DeriveTau(genesisHash, 1)

	// Test NewInitialBoundaryEvent and ProcessInitialEvent
	ev := NewInitialBoundaryEvent(initialBoundary, genesisHash)
	if ev.TargetEpoch != 1 {
		t.Fatalf("expected TargetEpoch=1, got %d", ev.TargetEpoch)
	}
	if ev.NextTau != expectedTau {
		t.Fatalf("expected NextTau=%s, got %s", expectedTau.Hex(), ev.NextTau.Hex())
	}
	if ev.CurrentBoundary != initialBoundary {
		t.Fatalf("expected CurrentBoundary=%d, got %d", initialBoundary, ev.CurrentBoundary)
	}

	txs, err := worker.ProcessInitialEvent(ev)
	if err != nil {
		t.Fatalf("ProcessInitialEvent failed: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 submitted txs for 2 candidates, got %d", len(txs))
	}
	if pool.Count() != 2 {
		t.Fatalf("expected 2 txs in pool, got %d", pool.Count())
	}

	for _, tx := range txs {
		if tx.To() == nil || *tx.To() != sysAddr {
			t.Fatalf("tx.To mismatch: got %v, want %s", tx.To(), sysAddr.Hex())
		}
		sol, err := DecodeSolutionCalldata(tx.Data())
		if err != nil {
			t.Fatalf("failed to decode solution calldata: %v", err)
		}
		if sol.Epoch != 1 {
			t.Errorf("solution epoch mismatch: got %d, want 1", sol.Epoch)
		}
		if !VerifySolution(sol, expectedTau, MaxTarget) {
			t.Errorf("VerifySolution failed for candidate %s with tau %s", sol.CandidateID, expectedTau.Hex())
		}
		from, err := types.Sender(types.HomesteadSigner{}, tx)
		if err != nil {
			t.Fatalf("failed to recover tx sender: %v", err)
		}
		switch sol.CandidateID {
		case "node-1":
			if from != cand1.Address {
				t.Errorf("node-1 sender mismatch: got %s, want %s", from.Hex(), cand1.Address.Hex())
			}
		case "node-2":
			if from != cand2.Address {
				t.Errorf("node-2 sender mismatch: got %s, want %s", from.Hex(), cand2.Address.Hex())
			}
		default:
			t.Errorf("unexpected candidate ID: %s", sol.CandidateID)
		}
	}
}

// 13. Test duplicate no-op: verifies that calling TriggerInitialEpoch multiple times
// will not submit duplicate transactions, and that disabled worker is a no-op.
func TestPowWorkerInitialDuplicateNoSubmit(t *testing.T) {
	pool := newFakeTxPool()
	genesisHash := common.HexToHash("0xaabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")

	// Sub-test 1: Disabled worker produces no transactions
	disabledCfg := PowWorkerConfig{
		Enabled:       false,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	disabledWorker := NewPowWorker(disabledCfg, pool, pool)
	cand1, _ := createTestCandidate(t, "node-disabled", []byte("pk-disabled"))
	_ = disabledWorker.AddCandidate(*cand1)

	txs, err := disabledWorker.TriggerInitialEpoch(0, genesisHash)
	if err != nil {
		t.Fatalf("disabled worker returned error: %v", err)
	}
	if len(txs) != 0 || pool.Count() != 0 {
		t.Fatalf("disabled worker submitted %d txs, pool has %d", len(txs), pool.Count())
	}

	// Sub-test 2: Enabled worker submits once and deduplicates subsequent invocations
	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	enabledWorker := NewPowWorker(workerCfg, pool, pool)
	cand2, _ := createTestCandidate(t, "node-dedup-1", []byte("pk-dedup-1"))
	cand3, _ := createTestCandidate(t, "node-dedup-2", []byte("pk-dedup-2"))
	_ = enabledWorker.AddCandidate(*cand2)
	_ = enabledWorker.AddCandidate(*cand3)

	// First trigger -> submits 2 txs (one per candidate)
	firstTxs, err := enabledWorker.TriggerInitialEpoch(0, genesisHash)
	if err != nil {
		t.Fatalf("first TriggerInitialEpoch failed: %v", err)
	}
	if len(firstTxs) != 2 {
		t.Fatalf("expected 2 transactions on first trigger, got %d", len(firstTxs))
	}
	if pool.Count() != 2 {
		t.Fatalf("expected 2 transactions in pool, got %d", pool.Count())
	}

	// Second trigger with same genesis hash -> 0 transactions submitted
	secondTxs, err := enabledWorker.TriggerInitialEpoch(0, genesisHash)
	if err != nil {
		t.Fatalf("second TriggerInitialEpoch failed: %v", err)
	}
	if len(secondTxs) != 0 {
		t.Fatalf("expected 0 transactions on duplicate trigger, got %d", len(secondTxs))
	}
	if pool.Count() != 2 {
		t.Fatalf("pool transaction count changed after duplicate trigger: got %d, want 2", pool.Count())
	}

	// Third trigger via ProcessBoundaryEvent with initial event -> still 0 transactions
	ev := NewInitialBoundaryEvent(0, genesisHash)
	thirdTxs, err := enabledWorker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("third trigger via boundary event failed: %v", err)
	}
	if len(thirdTxs) != 0 {
		t.Fatalf("expected 0 transactions on third trigger, got %d", len(thirdTxs))
	}
	if pool.Count() != 2 {
		t.Fatalf("pool transaction count changed: got %d, want 2", pool.Count())
	}
}

// 14. Test tau formula matches ScanEpochSolutions: verifies that the tau derived from
// (initialBoundaryBlockHash, epoch=1) is identical to what ScanEpochSolutions expects,
// and that solutions submitted by PowWorker for epoch 1 are accepted during chain scanning.
func TestPowWorkerTauMatchesScanEpochSolutions(t *testing.T) {
	reader := newMockBlockReader()
	genesisBlock := reader.addBlock(0)

	sysAddr := DefaultSolutionSystemAddress
	initialBoundary := uint64(0)
	nextBoundary := uint64(10)
	epochLength := uint64(10)

	pool := newFakeTxPool()
	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: sysAddr,
		GasLimit:      100000,
		GasPrice:      big.NewInt(1),
	}
	worker := NewPowWorker(workerCfg, pool, pool)
	cand, _ := createTestCandidate(t, "node-tau-match", []byte("pk-tau-match"))
	if err := worker.AddCandidate(*cand); err != nil {
		t.Fatalf("failed to add candidate: %v", err)
	}

	// Trigger initial epoch using genesis block hash
	txs, err := worker.TriggerInitialEpoch(initialBoundary, genesisBlock.Hash())
	if err != nil {
		t.Fatalf("TriggerInitialEpoch failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 initial solution tx, got %d", len(txs))
	}
	solutionTx := txs[0]

	// Simulate block production: place the solution tx in block 1 (within open interval (0, 10))
	reader.addBlock(1, solutionTx)
	for b := uint64(2); b <= 10; b++ {
		reader.addBlock(b)
	}

	// Formula for finalized epoch 1 tau in BoundaryCoordinator:
	// finalizedEpoch := (nextBoundary - initialBoundary) / epochLength = (10 - 0) / 10 = 1
	// prevBlock := reader.GetBlockByNumber(0)
	// expectedTau := DeriveTau(prevBlock.Hash(), finalizedEpoch)
	expectedEpoch := (nextBoundary - initialBoundary) / epochLength
	if expectedEpoch != 1 {
		t.Fatalf("expected epoch 1, got %d", expectedEpoch)
	}
	expectedTau := DeriveTau(genesisBlock.Hash(), expectedEpoch)

	nodeMapper := func(cid CandidateID) (NodeID, bool) {
		return NodeID(cid), true
	}

	// Positive test: ScanEpochSolutions with expectedTau successfully finds and accepts the solution
	solutions, err := ScanEpochSolutions(
		reader,
		initialBoundary,
		nextBoundary,
		expectedEpoch,
		expectedTau,
		MaxTarget,
		sysAddr,
		nodeMapper,
	)
	if err != nil {
		t.Fatalf("ScanEpochSolutions failed: %v", err)
	}
	if len(solutions) != 1 {
		t.Fatalf("expected 1 valid solution found by ScanEpochSolutions, got %d", len(solutions))
	}
	if solutions[0].CandidateID != cand.CandidateID {
		t.Errorf("candidate ID mismatch: got %s, want %s", solutions[0].CandidateID, cand.CandidateID)
	}
	if solutions[0].Epoch != 1 {
		t.Errorf("epoch mismatch: got %d, want 1", solutions[0].Epoch)
	}

	// Negative test: scanning with an incorrect tau (e.g. epoch 2 tau or wrong block hash) rejects the solution
	wrongTau := DeriveTau(genesisBlock.Hash(), 2)
	wrongSolutions, err := ScanEpochSolutions(
		reader,
		initialBoundary,
		nextBoundary,
		expectedEpoch,
		wrongTau,
		MaxTarget,
		sysAddr,
		nodeMapper,
	)
	if err != nil {
		t.Fatalf("ScanEpochSolutions with wrong tau failed: %v", err)
	}
	if len(wrongSolutions) != 0 {
		t.Fatalf("expected 0 solutions with mismatched tau, got %d", len(wrongSolutions))
	}

	// Verify end-to-end with BoundaryCoordinator.ProcessChainHead at block 10:
	// BoundaryCoordinator processes block 10, executes ScanEpochSolutions for epoch 1,
	// and elects the committee based on this initial epoch solution!
	electCfg := ElectionConfig{
		EpochLength:     epochLength,
		InitialBoundary: initialBoundary,
		CommitteeSize:   1,
		Threshold:       0,
		Target:          MaxTarget,
		SystemAddress:   sysAddr,
	}
	coordinator := NewBoundaryCoordinator(electCfg, reader, nodeMapper)
	head10 := reader.GetBlockByNumber(10)
	events, err := coordinator.ProcessChainHead(head10)
	if err != nil {
		t.Fatalf("ProcessChainHead(10) failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 boundary event at block 10, got %d", len(events))
	}
	ev10 := events[0]
	if ev10.FinalizedEpoch != 1 {
		t.Errorf("FinalizedEpoch mismatch: got %d, want 1", ev10.FinalizedEpoch)
	}
	if len(ev10.ValidSolutions) != 1 {
		t.Fatalf("expected 1 valid solution in boundary event, got %d", len(ev10.ValidSolutions))
	}
	if ev10.ElectionResult == nil || len(ev10.ElectionResult.RankedWinners) != 1 {
		t.Fatalf("expected committee elected for target epoch %d, got %+v", ev10.TargetEpoch, ev10.ElectionResult)
	}
	if ev10.ElectionResult.RankedWinners[0].CandidateID != cand.CandidateID {
		t.Errorf("elected member mismatch: got %s, want %s", ev10.ElectionResult.RankedWinners[0].CandidateID, cand.CandidateID)
	}
	if len(ev10.ElectionResult.Descriptor.Members) != 1 || ev10.ElectionResult.Descriptor.Members[0] != NodeID(cand.CandidateID) {
		t.Errorf("committee member mismatch: got %v, want [%s]", ev10.ElectionResult.Descriptor.Members, cand.CandidateID)
	}
}

// 15. Test Ethereum TriggerInitialEpoch safe behavior when disabled or without candidates
func TestEthereumTriggerInitialEpochDisabled(t *testing.T) {
	eth := &Ethereum{}
	// Nil worker returns nil without error
	if err := eth.TriggerInitialEpoch(); err != nil {
		t.Fatalf("expected nil error for nil worker, got %v", err)
	}

	pool := newFakeTxPool()
	worker := NewPowWorker(DefaultPowWorkerConfig(), pool, pool)
	eth.powWorker = worker
	// Disabled worker returns nil without error
	if err := eth.TriggerInitialEpoch(); err != nil {
		t.Fatalf("expected nil error for disabled worker, got %v", err)
	}
}

// 16. Test Ethereum TriggerInitialEpoch with uninitialized blockchain
func TestEthereumTriggerInitialEpochUninitializedBlockchain(t *testing.T) {
	eth := &Ethereum{}
	pool := newFakeTxPool()
	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	worker := NewPowWorker(workerCfg, pool, pool)
	cand, _ := createTestCandidate(t, "node-test-err", []byte("pk-test-err"))
	_ = worker.AddCandidate(*cand)
	eth.powWorker = worker

	// With nil blockchain -> returns error
	err := eth.TriggerInitialEpoch()
	if err == nil || err.Error() != "blockchain not initialized" {
		t.Fatalf("expected 'blockchain not initialized' error, got: %v", err)
	}
}

// 17. Test key not ready -> zero submissions
func TestPowWorkerKeyNotReadyZeroSubmissions(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-gate-1", []byte("pk-gate-1"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	// Explicitly configure keyReadyFn to return false
	worker.SetKeyReadyFunc(func() bool { return false })

	if worker.IsKeyReady() {
		t.Fatal("expected IsKeyReady to be false")
	}

	genesisHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	// TriggerInitialEpoch MUST return ErrKeyNotReady and 0 submissions
	txs, err := worker.TriggerInitialEpoch(0, genesisHash)
	if !errors.Is(err, ErrKeyNotReady) {
		t.Fatalf("expected ErrKeyNotReady, got: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("expected 0 txs returned when not ready, got %d", len(txs))
	}
	if pool.Count() != 0 {
		t.Fatalf("expected 0 txs in pool when not ready, got %d", pool.Count())
	}

	// ProcessBoundaryEvent MUST also return ErrKeyNotReady and 0 submissions
	ev := NewInitialBoundaryEvent(0, genesisHash)
	txsEv, errEv := worker.ProcessBoundaryEvent(ev)
	if !errors.Is(errEv, ErrKeyNotReady) {
		t.Fatalf("expected ErrKeyNotReady from ProcessBoundaryEvent, got: %v", errEv)
	}
	if len(txsEv) != 0 || pool.Count() != 0 {
		t.Fatalf("expected 0 txs on ProcessBoundaryEvent when not ready, got txs=%d pool=%d", len(txsEv), pool.Count())
	}
}

// 18. Test key ready transition -> automatically submits once
func TestPowWorkerKeyReadyTransitionAutoSubmit(t *testing.T) {
	cand1, _ := createTestCandidate(t, "cand-trans-1", []byte("pk-trans-1"))
	cand2, _ := createTestCandidate(t, "cand-trans-2", []byte("pk-trans-2"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand1)
	_ = worker.AddCandidate(*cand2)

	var ready uint32
	worker.SetKeyReadyFunc(func() bool { return atomic.LoadUint32(&ready) == 1 })

	genesisHash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	// Start background waiter with 10ms tick
	started := worker.StartInitialEpochWaiter(0, genesisHash, 10*time.Millisecond)
	if !started {
		t.Fatal("expected StartInitialEpochWaiter to return true")
	}
	defer worker.Stop()

	// While not ready, ensure no submissions occur even after several ticks
	time.Sleep(30 * time.Millisecond)
	if pool.Count() != 0 {
		t.Fatalf("expected 0 txs in pool while unready, got %d", pool.Count())
	}
	if worker.HasSubmittedInitialEpoch() {
		t.Fatal("expected HasSubmittedInitialEpoch to be false while unready")
	}

	// Transition to ready!
	atomic.StoreUint32(&ready, 1)

	// Wait for automatic single submission
	deadline := time.Now().Add(500 * time.Millisecond)
	for pool.Count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if pool.Count() != 2 {
		t.Fatalf("expected exactly 2 txs in pool after key ready transition, got %d", pool.Count())
	}
	if !worker.HasSubmittedInitialEpoch() {
		t.Fatal("expected HasSubmittedInitialEpoch to be true after submission")
	}

	// Verify both transactions are valid solutions for epoch 1
	txs := pool.Transactions()
	for _, tx := range txs {
		sol, err := DecodeSolutionCalldata(tx.Data())
		if err != nil {
			t.Fatalf("failed to decode solution calldata: %v", err)
		}
		if sol.Epoch != 1 {
			t.Errorf("expected epoch 1, got %d", sol.Epoch)
		}
		from, err := types.Sender(types.HomesteadSigner{}, tx)
		if err != nil {
			t.Fatalf("failed to recover sender: %v", err)
		}
		if from != cand1.Address && from != cand2.Address {
			t.Errorf("unexpected sender: %s", from.Hex())
		}
	}
}

// 19. Test duplicate ticks and calls do not produce duplicate submissions
func TestPowWorkerDuplicateTickNoRepeat(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-dedup-tick", []byte("pk-dedup-tick"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	var ready uint32
	atomic.StoreUint32(&ready, 1)
	worker.SetKeyReadyFunc(func() bool { return atomic.LoadUint32(&ready) == 1 })

	genesisHash := common.HexToHash("0x9988776655443322110099887766554433221100998877665544332211009988")

	// Initial submission via waiter
	worker.StartInitialEpochWaiter(0, genesisHash, 10*time.Millisecond)
	defer worker.Stop()

	// Wait for submission
	deadline := time.Now().Add(500 * time.Millisecond)
	for pool.Count() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pool.Count() != 1 {
		t.Fatalf("expected 1 tx in pool, got %d", pool.Count())
	}

	// Allow multiple ticks to elapse while key is ready
	time.Sleep(50 * time.Millisecond)
	if pool.Count() != 1 {
		t.Fatalf("duplicate ticks produced duplicate txs: pool count = %d, want 1", pool.Count())
	}

	// Direct TriggerInitialEpoch calls must return 0 new txs
	txs, err := worker.TriggerInitialEpoch(0, genesisHash)
	if err != nil {
		t.Fatalf("TriggerInitialEpoch returned error: %v", err)
	}
	if len(txs) != 0 || pool.Count() != 1 {
		t.Fatalf("TriggerInitialEpoch duplicate produced txs: got %d, pool count = %d", len(txs), pool.Count())
	}

	// Direct ProcessBoundaryEvent calls for epoch 1 must return 0 new txs
	ev := NewInitialBoundaryEvent(0, genesisHash)
	txsEv, errEv := worker.ProcessBoundaryEvent(ev)
	if errEv != nil {
		t.Fatalf("ProcessBoundaryEvent returned error: %v", errEv)
	}
	if len(txsEv) != 0 || pool.Count() != 1 {
		t.Fatalf("ProcessBoundaryEvent duplicate produced txs: got %d, pool count = %d", len(txsEv), pool.Count())
	}
}

// 20. Test Stop terminates goroutine cleanly without leaks
func TestPowWorkerStopNoLeak(t *testing.T) {
	cand, _ := createTestCandidate(t, "cand-stop-1", []byte("pk-stop-1"))
	pool := newFakeTxPool()

	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}

	worker := NewPowWorker(cfg, pool, pool)
	_ = worker.AddCandidate(*cand)

	var ready uint32
	worker.SetKeyReadyFunc(func() bool { return atomic.LoadUint32(&ready) == 1 })

	genesisHash := common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555")

	// Start background waiter
	worker.StartInitialEpochWaiter(0, genesisHash, 5*time.Millisecond)

	if !worker.IsWaiterRunning() {
		t.Fatal("expected waiter to be running")
	}

	// Stop worker
	worker.Stop()

	if worker.IsWaiterRunning() {
		t.Fatal("expected waiter to NOT be running after Stop")
	}

	// Calling Stop again must be idempotent and non-blocking
	worker.Stop()
	worker.Stop()

	// Setting ready=true after Stop must NOT trigger any submissions because waiter was stopped
	atomic.StoreUint32(&ready, 1)
	time.Sleep(30 * time.Millisecond)
	if pool.Count() != 0 {
		t.Fatalf("stopped worker unexpectedly submitted transaction: pool count = %d", pool.Count())
	}
}

// 21. Test disabled worker and 0 candidate worker are safe no-ops
func TestPowWorkerDisabledAndNoCandidateSafe(t *testing.T) {
	pool := newFakeTxPool()
	genesisHash := common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")

	// Sub-test 1: Disabled worker with StartInitialEpochWaiter returns false
	disabledWorker := NewPowWorker(DefaultPowWorkerConfig(), pool, pool)
	cand, _ := createTestCandidate(t, "cand-dis", []byte("pk-dis"))
	_ = disabledWorker.AddCandidate(*cand)

	started := disabledWorker.StartInitialEpochWaiter(0, genesisHash, 10*time.Millisecond)
	if started {
		t.Fatal("expected StartInitialEpochWaiter to return false when disabled")
	}
	if disabledWorker.IsWaiterRunning() {
		t.Fatal("expected waiter not running when disabled")
	}

	// Sub-test 2: Enabled worker without candidates is safe
	cfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	noCandWorker := NewPowWorker(cfg, pool, pool)
	if noCandWorker.HasSubmittedInitialEpoch() {
		t.Fatal("expected HasSubmittedInitialEpoch to be false for 0 candidates")
	}
	txs, err := noCandWorker.TriggerInitialEpoch(0, genesisHash)
	if err != nil || len(txs) != 0 {
		t.Fatalf("expected nil error and 0 txs for 0 candidates, got err=%v txs=%d", err, len(txs))
	}
}

// 22. Test Ethereum.TriggerInitialEpoch integrates DKG gate, asynchronous waiting, and idempotent completion
func TestEthereumTriggerInitialEpochDKGGate(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	gspec := &core.Genesis{Config: params.TestChainConfig}
	_ = gspec.MustCommit(db)
	chain, err := core.NewBlockChain(db, nil, gspec.Config, ethash.NewFaker(), vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	pool := newFakeTxPool()
	workerCfg := PowWorkerConfig{
		Enabled:       true,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
	}
	worker := NewPowWorker(workerCfg, pool, pool)
	cand, _ := createTestCandidate(t, "node-eth-dkg", []byte("pk-eth-dkg"))
	_ = worker.AddCandidate(*cand)

	var ready uint32
	worker.SetKeyReadyFunc(func() bool { return atomic.LoadUint32(&ready) == 1 })

	eth := &Ethereum{
		blockchain: chain,
		txPool:     &core.TxPool{},
		powWorker:  worker,
	}

	// 1. Initial trigger when key is NOT ready -> must return nil without blocking!
	if err := eth.TriggerInitialEpoch(); err != nil {
		t.Fatalf("expected nil error (asynchronous waiter started), got: %v", err)
	}

	// Pool must have 0 transactions while unready
	if pool.Count() != 0 {
		t.Fatalf("expected 0 txs in pool while unready, got %d", pool.Count())
	}
	if !worker.IsWaiterRunning() {
		t.Fatal("expected waiter to be running in background")
	}

	// 2. Transition key to ready
	atomic.StoreUint32(&ready, 1)

	// Wait for waiter to submit to pool
	deadline := time.Now().Add(1000 * time.Millisecond)
	for pool.Count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if pool.Count() != 1 {
		t.Fatalf("expected 1 tx in pool after key ready transition, got %d", pool.Count())
	}
	if !worker.HasSubmittedInitialEpoch() {
		t.Fatal("expected HasSubmittedInitialEpoch to be true")
	}

	// 3. Repeated TriggerInitialEpoch call is safe no-op
	if err := eth.TriggerInitialEpoch(); err != nil {
		t.Fatalf("duplicate TriggerInitialEpoch returned error: %v", err)
	}
	if pool.Count() != 1 {
		t.Fatalf("duplicate TriggerInitialEpoch submitted extra txs: pool count = %d, want 1", pool.Count())
	}

	// 4. worker.Stop() terminates cleanly without leaks or panics
	worker.Stop()
}

// 23. Test default gasPrice matches miner default (>= 1 Gwei) and signed tx has identical GasPrice
func TestPowWorkerDefaultGasPriceMatchesMiner(t *testing.T) {
	minerDefault := DefaultConfig.Miner.GasPrice
	if minerDefault == nil || minerDefault.Cmp(big.NewInt(params.GWei)) < 0 {
		t.Fatalf("unexpected miner default gas price: %v", minerDefault)
	}

	// 1. Check DefaultPowWorkerConfig
	cfg := DefaultPowWorkerConfig()
	if cfg.GasPrice == nil {
		t.Fatal("DefaultPowWorkerConfig GasPrice is nil")
	}
	if cfg.GasPrice.Cmp(minerDefault) < 0 {
		t.Errorf("default worker GasPrice (%v) < miner default GasPrice (%v)", cfg.GasPrice, minerDefault)
	}
	if cfg.GasPrice.Cmp(big.NewInt(params.GWei)) < 0 {
		t.Errorf("default worker GasPrice (%v) < 1 GWei (%v)", cfg.GasPrice, params.GWei)
	}

	// 2. Check ParsePowWorkerConfigFromEnv default
	envCfg, err := ParsePowWorkerConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("ParsePowWorkerConfigFromEnv default failed: %v", err)
	}
	if envCfg.GasPrice == nil || envCfg.GasPrice.Cmp(minerDefault) < 0 {
		t.Errorf("env default GasPrice (%v) < miner default GasPrice (%v)", envCfg.GasPrice, minerDefault)
	}

	// 3. Check NewPowWorker fallback on nil GasPrice
	nilPriceCfg := cfg
	nilPriceCfg.GasPrice = nil
	workerNil := NewPowWorker(nilPriceCfg, nil, nil)
	if workerNil.cfg.GasPrice == nil || workerNil.cfg.GasPrice.Cmp(minerDefault) < 0 {
		t.Errorf("NewPowWorker nil GasPrice fallback (%v) < miner default (%v)", workerNil.cfg.GasPrice, minerDefault)
	}

	// 4. Check signed solution transaction has exact GasPrice matching default cfg
	cand, _ := createTestCandidate(t, "cand-gas-default", []byte("pk-gas-default"))
	pool := newFakeTxPool()

	execCfg := DefaultPowWorkerConfig()
	execCfg.Enabled = true

	worker := NewPowWorker(execCfg, pool, pool)
	if err := worker.AddCandidate(*cand); err != nil {
		t.Fatalf("AddCandidate failed: %v", err)
	}

	ev := &BoundaryEvent{
		FinalizedEpoch:  0,
		TargetEpoch:     1,
		CurrentBoundary: 10,
		NextTau:         common.HexToHash("0x1234123412341234123412341234123412341234123412341234123412341234"),
	}

	txs, err := worker.ProcessBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ProcessBoundaryEvent failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}
	tx := txs[0]
	if tx.GasPrice().Cmp(execCfg.GasPrice) != 0 {
		t.Errorf("signed tx GasPrice (%v) != worker configured GasPrice (%v)", tx.GasPrice(), execCfg.GasPrice)
	}
	if tx.GasPrice().Cmp(minerDefault) < 0 {
		t.Errorf("signed tx GasPrice (%v) < miner default GasPrice (%v)", tx.GasPrice(), minerDefault)
	}
	if pool.Count() != 1 {
		t.Fatalf("expected 1 tx submitted to pool, got %d", pool.Count())
	}
	if pool.txs[0].GasPrice().Cmp(execCfg.GasPrice) != 0 {
		t.Errorf("pool tx GasPrice (%v) != worker configured GasPrice (%v)", pool.txs[0].GasPrice(), execCfg.GasPrice)
	}

	// 5. Explicit NITCH_POW_WORKER_GAS_PRICE override remains effective
	overrideEnv := map[string]string{
		"NITCH_POW_WORKER_GAS_PRICE": "2500000000",
	}
	overrideCfg, err := ParsePowWorkerConfigFromEnv(func(k string) string { return overrideEnv[k] })
	if err != nil {
		t.Fatalf("ParsePowWorkerConfigFromEnv with override failed: %v", err)
	}
	expectedOverride := big.NewInt(2500000000)
	if overrideCfg.GasPrice.Cmp(expectedOverride) != 0 {
		t.Errorf("override GasPrice: got %v, want %v", overrideCfg.GasPrice, expectedOverride)
	}
}

func TestPowWorkerInitialReleaseGate(t *testing.T) {
	cfg, err := ParsePowWorkerConfigFromEnv(func(key string) string {
		if key == "NITCH_POW_INITIAL_MANUAL_RELEASE" {
			return "1"
		}
		return ""
	})
	if err != nil || !cfg.ManualInitialRelease {
		t.Fatalf("manual release config: %v %+v", err, cfg)
	}
	cfg.Enabled = true
	pool := newFakeTxPool()
	worker := NewPowWorker(cfg, pool, pool)
	worker.SetKeyReadyFunc(func() bool { return true })
	cand, _ := createTestCandidate(t, "candidate-gated", []byte("candidate-pk"))
	if err := worker.AddCandidate(*cand); err != nil {
		t.Fatal(err)
	}
	blockHash := common.HexToHash("0x1234")
	if !worker.StartInitialEpochWaiter(0, blockHash, 5*time.Millisecond) {
		t.Fatal("initial waiter did not start")
	}
	defer worker.Stop()
	time.Sleep(30 * time.Millisecond)
	if pool.Count() != 0 || !worker.InitialReleasePending() {
		t.Fatal("candidate submitted before orchestration released epoch 1")
	}
	if _, err := worker.ProcessBoundaryEvent(NewInitialBoundaryEvent(0, blockHash)); !errors.Is(err, ErrInitialEpochAwaitingRelease) {
		t.Fatalf("unguarded epoch 1 event: %v", err)
	}
	worker.ReleaseInitialEpoch()
	worker.ReleaseInitialEpoch()
	deadline := time.After(time.Second)
	for !worker.HasSubmittedInitialEpoch() {
		select {
		case <-deadline:
			t.Fatal("initial waiter did not submit after release")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if pool.Count() != 1 {
		t.Fatalf("duplicate initial submission: %d", pool.Count())
	}
	if _, err := worker.TriggerInitialEpoch(0, blockHash); err != nil || pool.Count() != 1 {
		t.Fatalf("repeated trigger: err=%v count=%d", err, pool.Count())
	}
}

func TestPowWorkerInitialReleaseRetryAndLaterEpoch(t *testing.T) {
	cfg := DefaultPowWorkerConfig()
	cfg.Enabled = true
	cfg.ManualInitialRelease = true
	pool := newFakeTxPool()
	pool.failErr = errors.New("temporary submit failure")
	worker := NewPowWorker(cfg, pool, pool)
	worker.SetKeyReadyFunc(func() bool { return true })
	cand, _ := createTestCandidate(t, "candidate-retry", []byte("candidate-pk"))
	if err := worker.AddCandidate(*cand); err != nil {
		t.Fatal(err)
	}
	ev := &BoundaryEvent{TargetEpoch: 2, NextTau: common.HexToHash("0x5678")}
	pool.mu.Lock()
	pool.failErr = nil
	pool.mu.Unlock()
	if _, err := worker.ProcessBoundaryEvent(ev); err != nil || pool.Count() != 1 {
		t.Fatalf("later epoch blocked: err=%v count=%d", err, pool.Count())
	}
	pool.mu.Lock()
	pool.failErr = errors.New("temporary submit failure")
	pool.mu.Unlock()
	worker.ReleaseInitialEpoch()
	blockHash := common.HexToHash("0x1234")
	if _, err := worker.TriggerInitialEpoch(0, blockHash); err == nil || worker.HasSubmittedInitialEpoch() {
		t.Fatalf("failed submission marked complete: %v", err)
	}
	pool.mu.Lock()
	pool.failErr = nil
	pool.mu.Unlock()
	if _, err := worker.TriggerInitialEpoch(0, blockHash); err != nil || !worker.HasSubmittedInitialEpoch() || pool.Count() != 2 {
		t.Fatalf("retry failed: err=%v count=%d", err, pool.Count())
	}
}
