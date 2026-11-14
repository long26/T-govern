package eth

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	// ErrMaxAttemptsExceeded is returned when max nonce mining attempts is reached without a valid solution.
	ErrMaxAttemptsExceeded = errors.New("maximum nonce attempts exceeded without finding valid solution")

	// ErrWorkerDisabled is returned when attempting to process an event while the worker is disabled.
	ErrWorkerDisabled = errors.New("pow worker is disabled")

	// ErrNilSigner is returned when a candidate has no signer configured.
	ErrNilSigner = errors.New("candidate has nil signer")

	// ErrUnsignedTransaction is returned when a generated transaction has zero signature components.
	ErrUnsignedTransaction = errors.New("transaction signature values are zero (unsigned transaction)")

	// ErrKeyNotReady is returned when attempting to submit solutions before the system public key / DKG is ready.
	ErrKeyNotReady = errors.New("NITCH system public key is not ready")

	ErrInitialEpochAwaitingRelease = errors.New("initial PoW epoch is awaiting orchestration release")
)

// TxSigner is an interface for signing candidate solution transactions.
type TxSigner interface {
	Sender() common.Address
	Sign(tx *types.Transaction) (*types.Transaction, error)
}

// PrivateKeySigner implements TxSigner using an ECDSA private key.
type PrivateKeySigner struct {
	key    *ecdsa.PrivateKey
	signer types.Signer
	addr   common.Address
}

// NewPrivateKeySigner creates a new PrivateKeySigner with the given private key and Ethereum signer.
func NewPrivateKeySigner(key *ecdsa.PrivateKey, signer types.Signer) *PrivateKeySigner {
	if signer == nil {
		signer = types.HomesteadSigner{}
	}
	var addr common.Address
	if key != nil {
		addr = crypto.PubkeyToAddress(key.PublicKey)
	}
	return &PrivateKeySigner{
		key:    key,
		signer: signer,
		addr:   addr,
	}
}

// Sender returns the public account address corresponding to the private key.
func (s *PrivateKeySigner) Sender() common.Address {
	return s.addr
}

// Sign signs the transaction using the configured signer and private key.
func (s *PrivateKeySigner) Sign(tx *types.Transaction) (*types.Transaction, error) {
	if s.key == nil {
		return nil, errors.New("nil private key in signer")
	}
	return types.SignTx(tx, s.signer, s.key)
}

// TxSignerFunc is an adapter to allow the use of ordinary functions as TxSigner.
type TxSignerFunc struct {
	addr   common.Address
	signFn func(tx *types.Transaction) (*types.Transaction, error)
}

// NewTxSignerFunc creates a TxSigner from a sender address and signing function.
func NewTxSignerFunc(addr common.Address, fn func(tx *types.Transaction) (*types.Transaction, error)) TxSigner {
	return &TxSignerFunc{addr: addr, signFn: fn}
}

// Sender returns the configured sender address.
func (s *TxSignerFunc) Sender() common.Address {
	return s.addr
}

// Sign delegates to the wrapped signing function.
func (s *TxSignerFunc) Sign(tx *types.Transaction) (*types.Transaction, error) {
	if s.signFn == nil {
		return nil, errors.New("nil sign function")
	}
	return s.signFn(tx)
}

// TxSubmitter submits signed transactions into the local transaction pool.
type TxSubmitter interface {
	SubmitTx(tx *types.Transaction) error
}

// TxSubmitterFunc is a function adapter for TxSubmitter.
type TxSubmitterFunc func(tx *types.Transaction) error

// SubmitTx calls the underlying function.
func (f TxSubmitterFunc) SubmitTx(tx *types.Transaction) error {
	return f(tx)
}

// NonceProvider resolves the next transaction nonce for an account address.
type NonceProvider interface {
	GetNonce(addr common.Address) (uint64, error)
}

// NonceProviderFunc is a function adapter for NonceProvider.
type NonceProviderFunc func(addr common.Address) (uint64, error)

// GetNonce calls the underlying function.
func (f NonceProviderFunc) GetNonce(addr common.Address) (uint64, error) {
	return f(addr)
}

// PowCandidate represents a candidate configured to mine and submit solutions.
type PowCandidate struct {
	CandidateID CandidateID    `json:"candidate_id"`
	ElectionPK  []byte         `json:"election_pk"`
	Address     common.Address `json:"address"`
	Signer      TxSigner       `json:"-"`
}

// PowWorkerConfig contains configuration parameters for the PoW solution worker.
type PowWorkerConfig struct {
	Enabled              bool           `json:"enabled"`
	ManualInitialRelease bool           `json:"manual_initial_release"`
	Target               common.Hash    `json:"target"`
	SystemAddress        common.Address `json:"system_address"`
	GasLimit             uint64         `json:"gas_limit"`
	GasPrice             *big.Int       `json:"gas_price"`
	MaxAttempts          uint64         `json:"max_attempts"` // 0 = unlimited / until found
}

// DefaultPowWorkerConfig returns the default configuration.
// By default, the worker is disabled (Enabled: false) to preserve static node behavior.
// GasPrice defaults to 1 Gwei (params.GWei) to match standard miner txpool minimum price
// ensuring remote miners do not reject solution transactions as underpriced.
func DefaultPowWorkerConfig() PowWorkerConfig {
	return PowWorkerConfig{
		Enabled:       false,
		Target:        MaxTarget,
		SystemAddress: DefaultSolutionSystemAddress,
		GasLimit:      100000,
		GasPrice:      big.NewInt(params.GWei),
		MaxAttempts:   0,
	}
}

// ParsePowWorkerConfigFromEnv parses PoW worker configuration from environment variables.
//
// Environment variables:
//   NITCH_POW_WORKER_ENABLED      - "1", "true", "yes" to enable (default: false)
//   NITCH_POW_INITIAL_MANUAL_RELEASE - wait for eth_releaseInitialEpoch before submitting epoch 1
//   NITCH_POW_WORKER_GAS_LIMIT    - gas limit for solution tx (default: 100000)
//   NITCH_POW_WORKER_GAS_PRICE    - gas price in wei (default: 1 Gwei = 1000000000)
//   NITCH_POW_WORKER_MAX_ATTEMPTS - max mining nonce attempts (default: 0 = unlimited)
//   NITCH_POW_TARGET              - hex target (inherits from election config if set)
//   NITCH_POW_DIFFICULTY          - difficulty value (converted to target if NITCH_POW_TARGET not set)
//   NITCH_POW_SYSTEM_ADDRESS      - hex destination address for solution transactions
func ParsePowWorkerConfigFromEnv(getenv func(string) string) (PowWorkerConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := DefaultPowWorkerConfig()

	// NITCH_POW_WORKER_ENABLED
	if raw := strings.TrimSpace(getenv("NITCH_POW_WORKER_ENABLED")); raw != "" {
		lower := strings.ToLower(raw)
		cfg.Enabled = lower == "1" || lower == "true" || lower == "yes"
	}

	if raw := strings.ToLower(strings.TrimSpace(getenv("NITCH_POW_INITIAL_MANUAL_RELEASE"))); raw != "" {
		cfg.ManualInitialRelease = raw == "1" || raw == "true" || raw == "yes"
	}

	// NITCH_POW_WORKER_GAS_LIMIT
	if raw := strings.TrimSpace(getenv("NITCH_POW_WORKER_GAS_LIMIT")); raw != "" {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || val == 0 {
			return PowWorkerConfig{}, fmt.Errorf("invalid NITCH_POW_WORKER_GAS_LIMIT %q", raw)
		}
		cfg.GasLimit = val
	}

	// NITCH_POW_WORKER_GAS_PRICE
	if raw := strings.TrimSpace(getenv("NITCH_POW_WORKER_GAS_PRICE")); raw != "" {
		gp, ok := new(big.Int).SetString(raw, 0)
		if !ok || gp.Sign() < 0 {
			return PowWorkerConfig{}, fmt.Errorf("invalid NITCH_POW_WORKER_GAS_PRICE %q", raw)
		}
		cfg.GasPrice = gp
	}

	// NITCH_POW_WORKER_MAX_ATTEMPTS
	if raw := strings.TrimSpace(getenv("NITCH_POW_WORKER_MAX_ATTEMPTS")); raw != "" {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return PowWorkerConfig{}, fmt.Errorf("invalid NITCH_POW_WORKER_MAX_ATTEMPTS %q", raw)
		}
		cfg.MaxAttempts = val
	}

	// NITCH_POW_DIFFICULTY -> target
	if raw := strings.TrimSpace(getenv("NITCH_POW_DIFFICULTY")); raw != "" {
		diff, ok := new(big.Int).SetString(raw, 0)
		if !ok || diff.Sign() <= 0 {
			return PowWorkerConfig{}, fmt.Errorf("invalid NITCH_POW_DIFFICULTY %q", raw)
		}
		target, err := DifficultyToTarget(diff)
		if err != nil {
			return PowWorkerConfig{}, err
		}
		cfg.Target = target
	}

	// NITCH_POW_TARGET overrides difficulty if provided
	if raw := strings.TrimSpace(getenv("NITCH_POW_TARGET")); raw != "" {
		target := common.HexToHash(raw)
		if target == (common.Hash{}) {
			return PowWorkerConfig{}, fmt.Errorf("invalid NITCH_POW_TARGET %q", raw)
		}
		cfg.Target = target
	}

	// NITCH_POW_SYSTEM_ADDRESS
	if raw := strings.TrimSpace(getenv("NITCH_POW_SYSTEM_ADDRESS")); raw != "" {
		cfg.SystemAddress = common.HexToAddress(raw)
	}

	return cfg, nil
}

// CandidateFromEnv attempts to construct a PowCandidate from environment variables:
//   NITCH_POW_CANDIDATE_ID (falls back to NITCH_NODE_ID, NITCH_DATA_DIR, NITCH_DATADIR, or KeyManager localNodeID)
//   NITCH_POW_ELECTION_PK  (hex string with 0x prefix or raw string bytes)
//   NITCH_POW_WORKER_KEY   (hex secp256k1 private key)
//
// Returns (cand, ok, err). If no candidate env vars are set, ok is false and err is nil.
func CandidateFromEnv(getenv func(string) string) (*PowCandidate, bool, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	candID := strings.TrimSpace(getenv("NITCH_POW_CANDIDATE_ID"))
	keyHex := strings.TrimSpace(getenv("NITCH_POW_WORKER_KEY"))
	pkRaw := strings.TrimSpace(getenv("NITCH_POW_ELECTION_PK"))

	// If no candidate-specific configuration is present, this node is not configuring a PoW candidate.
	if candID == "" && keyHex == "" && pkRaw == "" {
		return nil, false, nil
	}

	// Fallback chain for candidate ID: NITCH_NODE_ID -> NITCH_DATA_DIR -> NITCH_DATADIR -> KeyManager localNodeID
	if candID == "" {
		candID = strings.TrimSpace(getenv("NITCH_NODE_ID"))
	}
	if candID == "" {
		candID = strings.TrimSpace(getenv("NITCH_DATA_DIR"))
	}
	if candID == "" {
		candID = strings.TrimSpace(getenv("NITCH_DATADIR"))
	}
	if candID == "" {
		if km := GetKeyManager(); km != nil {
			candID = string(km.GetLocalNodeID())
		}
	}

	if candID == "" {
		return nil, false, errors.New("candidate ID missing (set NITCH_POW_CANDIDATE_ID or NITCH_NODE_ID)")
	}
	if keyHex == "" {
		return nil, false, errors.New("worker private key missing (set NITCH_POW_WORKER_KEY)")
	}

	keyHex = strings.TrimPrefix(keyHex, "0x")
	prvKey, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, false, fmt.Errorf("invalid NITCH_POW_WORKER_KEY: %w", err)
	}

	var electionPK []byte
	if pkRaw != "" {
		if strings.HasPrefix(pkRaw, "0x") {
			electionPK = common.Hex2Bytes(strings.TrimPrefix(pkRaw, "0x"))
		} else {
			electionPK = []byte(pkRaw)
		}
	} else {
		// Default election PK to uncompressed public key bytes if not explicitly provided
		electionPK = crypto.FromECDSAPub(&prvKey.PublicKey)
	}

	addr := crypto.PubkeyToAddress(prvKey.PublicKey)
	signer := NewPrivateKeySigner(prvKey, types.HomesteadSigner{})

	return &PowCandidate{
		CandidateID: CandidateID(candID),
		ElectionPK:  electionPK,
		Address:     addr,
		Signer:      signer,
	}, true, nil
}

// MineSolution iterates nonces starting from 0 until a nonce satisfying target is found.
// Exactly one valid nonce is returned ("每个 CandidateID 只求一个满足 target 的 nonce").
func MineSolution(tau common.Hash, epoch uint64, candidateID CandidateID, electionPK []byte, target common.Hash, maxAttempts uint64) (*Solution, error) {
	if target == (common.Hash{}) {
		target = MaxTarget
	}

	var nonce uint64
	for {
		digest := ComputePowDigest(tau, epoch, candidateID, electionPK, nonce)
		if VerifyPowDigest(digest, target) {
			return &Solution{
				Epoch:       epoch,
				CandidateID: candidateID,
				ElectionPK:  electionPK,
				Nonce:       nonce,
				PowDigest:   digest,
			}, nil
		}
		if maxAttempts > 0 && nonce+1 >= maxAttempts {
			return nil, fmt.Errorf("%w: tried %d nonces", ErrMaxAttemptsExceeded, nonce+1)
		}
		if nonce == ^uint64(0) {
			return nil, errors.New("nonce space exhausted (uint64 overflow)")
		}
		nonce++
	}
}

// PowWorker listens to boundary events, mines a valid solution for each registered candidate
// using NextTau for TargetEpoch, packages into an Ethereum transaction, signs it, and submits to txpool.
type PowWorker struct {
	mu sync.RWMutex

	cfg           PowWorkerConfig
	submitter     TxSubmitter
	nonceProvider NonceProvider

	candidates map[CandidateID]PowCandidate
	candOrder  []CandidateID

	// submitted tracks (epoch, candidateID) pairs already submitted to guarantee at most 1 submission per epoch.
	submitted        map[string]bool
	initialReleased  bool
	initialTriggerMu sync.Mutex

	// keyReadyFn optionally gates solution submission on DKG / system key readiness.
	// If nil, IsKeyReady returns true.
	keyReadyFn func() bool

	// Background initial epoch waiter state
	waiterMu      sync.Mutex
	waiterRunning bool
	waiterStopCh  chan struct{}
	waiterDoneCh  chan struct{}
}

// NewPowWorker creates a new PoW solution worker.
func NewPowWorker(cfg PowWorkerConfig, submitter TxSubmitter, nonceProvider NonceProvider) *PowWorker {
	if cfg.Target == (common.Hash{}) {
		cfg.Target = MaxTarget
	}
	if cfg.SystemAddress == (common.Address{}) {
		cfg.SystemAddress = DefaultSolutionSystemAddress
	}
	if cfg.GasLimit == 0 {
		cfg.GasLimit = 100000
	}
	if cfg.GasPrice == nil {
		cfg.GasPrice = big.NewInt(params.GWei)
	}

	return &PowWorker{
		cfg:             cfg,
		submitter:       submitter,
		nonceProvider:   nonceProvider,
		candidates:      make(map[CandidateID]PowCandidate),
		submitted:       make(map[string]bool),
		initialReleased: !cfg.ManualInitialRelease,
	}
}

// Enabled returns whether the worker is enabled.
func (w *PowWorker) Enabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg.Enabled
}

// InitialReleasePending reports whether epoch 1 is waiting for orchestration.
func (w *PowWorker) InitialReleasePending() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg.ManualInitialRelease && !w.initialReleased
}

// ReleaseInitialEpoch opens the epoch 1 gate; the caller then triggers submission.
func (w *PowWorker) ReleaseInitialEpoch() {
	w.mu.Lock()
	w.initialReleased = true
	w.mu.Unlock()
}

func (w *PowWorker) InitialEpochStatus() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	submitted := len(w.candOrder) > 0
	for _, id := range w.candOrder {
		submitted = submitted && w.submitted[epochCandidateKey(1, id)]
	}
	return map[string]interface{}{
		"enabled": w.cfg.Enabled, "manualRelease": w.cfg.ManualInitialRelease,
		"released": w.initialReleased, "submitted": submitted, "candidateCount": len(w.candOrder),
	}
}

// SetEnabled toggles worker activation.
func (w *PowWorker) SetEnabled(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cfg.Enabled = enabled
}

// SetSubmitter updates the transaction submitter.
func (w *PowWorker) SetSubmitter(submitter TxSubmitter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.submitter = submitter
}

// SetNonceProvider updates the nonce provider.
func (w *PowWorker) SetNonceProvider(np NonceProvider) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nonceProvider = np
}

// SetKeyReadyFunc sets an optional callback to verify whether the system key/DKG is ready
// before submitting transactions. If nil, no key readiness gate is enforced (IsKeyReady returns true).
func (w *PowWorker) SetKeyReadyFunc(fn func() bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.keyReadyFn = fn
}

// KeyReadyFunc returns the configured key readiness callback, or nil.
func (w *PowWorker) KeyReadyFunc() func() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.keyReadyFn
}

// IsKeyReady reports whether the system key/DKG is ready.
// If a keyReadyFn was configured, it delegates to it; otherwise returns true.
func (w *PowWorker) IsKeyReady() bool {
	w.mu.RLock()
	fn := w.keyReadyFn
	w.mu.RUnlock()
	if fn != nil {
		return fn()
	}
	return true
}

// HasSubmittedInitialEpoch reports whether all currently registered candidates have submitted
// solutions for epoch 1. If no candidates are registered, it returns false.
func (w *PowWorker) HasSubmittedInitialEpoch() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.candidates) == 0 {
		return false
	}
	for _, id := range w.candOrder {
		key := epochCandidateKey(1, id)
		if !w.submitted[key] {
			return false
		}
	}
	return true
}

// AddCandidate registers a candidate for PoW solution mining and submission.
func (w *PowWorker) AddCandidate(cand PowCandidate) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(cand.CandidateID) == 0 {
		return ErrInvalidCandidateID
	}
	if len(cand.ElectionPK) == 0 {
		return ErrInvalidElectionPK
	}
	if cand.Signer == nil {
		return ErrNilSigner
	}
	if cand.Address == (common.Address{}) {
		cand.Address = cand.Signer.Sender()
	}

	if _, exists := w.candidates[cand.CandidateID]; !exists {
		w.candOrder = append(w.candOrder, cand.CandidateID)
	}
	w.candidates[cand.CandidateID] = cand
	return nil
}

// CandidateCount returns the number of registered candidates.
func (w *PowWorker) CandidateCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.candidates)
}

func epochCandidateKey(epoch uint64, id CandidateID) string {
	return fmt.Sprintf("%d:%s", epoch, id)
}

// BuildAndSignTx creates, encodes, and signs a solution transaction for the given candidate and solution.
func (w *PowWorker) BuildAndSignTx(cand *PowCandidate, sol *Solution) (*types.Transaction, error) {
	if cand == nil {
		return nil, errors.New("nil candidate")
	}
	if cand.Signer == nil {
		return nil, ErrNilSigner
	}
	if sol == nil {
		return nil, ErrNilSolution
	}
	if w.nonceProvider == nil {
		return nil, errors.New("nil nonce provider")
	}

	nonce, err := w.nonceProvider.GetNonce(cand.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce for account %s: %w", cand.Address.Hex(), err)
	}

	unsignedTx, err := NewSolutionTransaction(nonce, w.cfg.SystemAddress, w.cfg.GasLimit, w.cfg.GasPrice, sol)
	if err != nil {
		return nil, fmt.Errorf("failed to construct solution transaction: %w", err)
	}

	signedTx, err := cand.Signer.Sign(unsignedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign solution transaction: %w", err)
	}

	// Verify the transaction was actually signed (no unsigned transactions allowed)
	v, r, s := signedTx.RawSignatureValues()
	if v == nil || r == nil || s == nil || v.Sign() == 0 || r.Sign() == 0 || s.Sign() == 0 {
		return nil, ErrUnsignedTransaction
	}

	return signedTx, nil
}

// ProcessBoundaryEvent handles an epoch boundary event:
// 1. Checks if worker is enabled.
// 2. Extracts NextTau and TargetEpoch.
// 3. For each registered CandidateID, checks if already submitted for TargetEpoch.
// 4. If not submitted, mines exactly 1 nonce satisfying Target.
// 5. Constructs, signs, and submits the transaction to the txpool.
// 6. Marks (TargetEpoch, CandidateID) as submitted.
func (w *PowWorker) ProcessBoundaryEvent(ev *BoundaryEvent) ([]*types.Transaction, error) {
	if ev != nil && ev.TargetEpoch == 1 {
		w.initialTriggerMu.Lock()
		defer w.initialTriggerMu.Unlock()
	}
	w.mu.Lock()
	if ev != nil && ev.TargetEpoch == 1 && w.cfg.ManualInitialRelease && !w.initialReleased {
		w.mu.Unlock()
		return nil, ErrInitialEpochAwaitingRelease
	}
	if !w.cfg.Enabled {
		w.mu.Unlock()
		return nil, nil
	}
	if w.keyReadyFn != nil && !w.keyReadyFn() {
		w.mu.Unlock()
		return nil, ErrKeyNotReady
	}
	if ev == nil {
		w.mu.Unlock()
		return nil, errors.New("nil boundary event")
	}
	if ev.NextTau == (common.Hash{}) {
		w.mu.Unlock()
		return nil, errors.New("empty NextTau in boundary event")
	}
	if w.submitter == nil {
		w.mu.Unlock()
		return nil, errors.New("nil transaction submitter")
	}

	targetEpoch := ev.TargetEpoch
	nextTau := ev.NextTau
	target := w.cfg.Target
	maxAttempts := w.cfg.MaxAttempts

	// Snapshot candidate list in deterministic order
	candidatesToProcess := make([]PowCandidate, 0, len(w.candOrder))
	for _, id := range w.candOrder {
		key := epochCandidateKey(targetEpoch, id)
		if w.submitted[key] {
			continue // Already submitted for this epoch
		}
		if cand, exists := w.candidates[id]; exists {
			candidatesToProcess = append(candidatesToProcess, cand)
		}
	}
	w.mu.Unlock()

	if len(candidatesToProcess) == 0 {
		return nil, nil
	}

	var submittedTxs []*types.Transaction
	for _, cand := range candidatesToProcess {
		key := epochCandidateKey(targetEpoch, cand.CandidateID)

		// Mine exactly 1 nonce satisfying target
		sol, err := MineSolution(nextTau, targetEpoch, cand.CandidateID, cand.ElectionPK, target, maxAttempts)
		if err != nil {
			log.Warn("PoW worker failed to mine solution",
				"candidate", cand.CandidateID, "epoch", targetEpoch, "err", err)
			return submittedTxs, err
		}

		// Build and sign transaction
		signedTx, err := w.BuildAndSignTx(&cand, sol)
		if err != nil {
			log.Warn("PoW worker failed to build/sign solution transaction",
				"candidate", cand.CandidateID, "epoch", targetEpoch, "err", err)
			return submittedTxs, err
		}

		// Submit transaction to txpool
		if err := w.submitter.SubmitTx(signedTx); err != nil {
			log.Warn("PoW worker failed to submit solution transaction",
				"candidate", cand.CandidateID, "epoch", targetEpoch, "txHash", signedTx.Hash().Hex(), "err", err)
			return submittedTxs, err
		}

		// Mark as submitted
		w.mu.Lock()
		w.submitted[key] = true
		w.mu.Unlock()

		submittedTxs = append(submittedTxs, signedTx)
		log.Info("PoW solution submitted to txpool",
			"candidate", cand.CandidateID, "epoch", targetEpoch,
			"txHash", signedTx.Hash().Hex(), "nonce", sol.Nonce,
			"powDigest", sol.PowDigest.Hex())
	}

	return submittedTxs, nil
}

// BoundaryHandler returns a BoundaryHandler callback suitable for registration with BoundaryCoordinator.
func (w *PowWorker) BoundaryHandler() BoundaryHandler {
	return func(ev *BoundaryEvent) error {
		_, err := w.ProcessBoundaryEvent(ev)
		if err != nil {
			log.Warn("PoW worker boundary processing failed",
				"boundary", ev.CurrentBoundary, "targetEpoch", ev.TargetEpoch, "err", err)
			// Return error to coordinator logger without crashing or triggering retries
			return err
		}
		return nil
	}
}

// NewInitialBoundaryEvent constructs a BoundaryEvent representing the initial epoch boundary (epoch 1)
// using the canonical block hash at the initial boundary height (typically genesis block at height 0).
// TargetEpoch is set to 1 and NextTau is derived via DeriveTau(initialBlockHash, 1).
func NewInitialBoundaryEvent(initialBoundary uint64, initialBlockHash common.Hash) *BoundaryEvent {
	epoch := uint64(1)
	tau := DeriveTau(initialBlockHash, epoch)
	return &BoundaryEvent{
		Epoch:            0,
		FinalizedEpoch:   0,
		TargetEpoch:      epoch,
		PreviousBoundary: initialBoundary,
		CurrentBoundary:  initialBoundary,
		NextTau:          tau,
	}
}

// ProcessInitialEvent processes an initial epoch boundary event to submit epoch 1 solutions.
// It delegates to ProcessBoundaryEvent.
func (w *PowWorker) ProcessInitialEvent(ev *BoundaryEvent) ([]*types.Transaction, error) {
	return w.ProcessBoundaryEvent(ev)
}

// TriggerInitialEpoch triggers solution mining and submission for the initial epoch (epoch 1).
// It derives tau from the initial boundary block hash and epoch 1, and processes the initial boundary event.
// If the worker is disabled, it returns nil immediately without doing any work.
// Submissions are strictly deduplicated by (epoch, candidateID); repeated invocations will not
// produce duplicate transactions.
func (w *PowWorker) TriggerInitialEpoch(initialBoundary uint64, initialBlockHash common.Hash) ([]*types.Transaction, error) {
	if !w.Enabled() {
		return nil, nil
	}
	if !w.IsKeyReady() {
		return nil, ErrKeyNotReady
	}
	if initialBlockHash == (common.Hash{}) {
		return nil, errors.New("empty initial block hash")
	}
	ev := NewInitialBoundaryEvent(initialBoundary, initialBlockHash)
	return w.ProcessBoundaryEvent(ev)
}

// StartInitialEpochWaiter starts an asynchronous loop that polls for system key readiness
// (via IsKeyReady) and triggers initial epoch submission once ready.
// If the worker is disabled, initialBlockHash is empty, or epoch 1 is already submitted
// for all registered candidates, it does nothing and returns false.
// If the key is already ready, it triggers initial epoch immediately.
// Otherwise, it starts the background goroutine (if not already running) and returns true.
func (w *PowWorker) StartInitialEpochWaiter(initialBoundary uint64, initialBlockHash common.Hash, checkInterval time.Duration) bool {
	if !w.Enabled() {
		return false
	}
	if initialBlockHash == (common.Hash{}) {
		return false
	}
	if w.HasSubmittedInitialEpoch() {
		return false
	}

	// If already ready, attempt immediate submission
	if w.IsKeyReady() && !w.InitialReleasePending() {
		txs, err := w.TriggerInitialEpoch(initialBoundary, initialBlockHash)
		if err == nil && len(txs) > 0 {
			return true
		}
		if w.HasSubmittedInitialEpoch() {
			return true
		}
	}

	w.waiterMu.Lock()
	defer w.waiterMu.Unlock()

	if w.waiterRunning {
		return true // already running
	}

	w.waiterRunning = true
	w.waiterStopCh = make(chan struct{})
	w.waiterDoneCh = make(chan struct{})

	go w.runInitialEpochWaiter(initialBoundary, initialBlockHash, checkInterval)
	return true
}

func (w *PowWorker) runInitialEpochWaiter(initialBoundary uint64, initialBlockHash common.Hash, interval time.Duration) {
	defer func() {
		w.waiterMu.Lock()
		w.waiterRunning = false
		w.waiterMu.Unlock()
		close(w.waiterDoneCh)
	}()

	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.waiterStopCh:
			return
		case <-ticker.C:
			if !w.Enabled() {
				return
			}
			// Attempt candidate discovery from env if candidate list is empty
			if w.CandidateCount() == 0 {
				if cand, ok, err := CandidateFromEnv(nil); err == nil && ok && cand != nil {
					_ = w.AddCandidate(*cand)
				}
			}
			if w.CandidateCount() == 0 {
				continue
			}
			if w.HasSubmittedInitialEpoch() {
				return
			}
			if w.IsKeyReady() && !w.InitialReleasePending() {
				txs, err := w.TriggerInitialEpoch(initialBoundary, initialBlockHash)
				if err != nil {
					if errors.Is(err, ErrKeyNotReady) {
						continue
					}
					log.Warn("PoW worker initial epoch retry failed", "err", err)
					continue
				}
				if len(txs) > 0 {
					log.Info("Initial epoch solution transaction(s) submitted to txpool after DKG key ready",
						"epoch", 1, "count", len(txs), "boundary", initialBoundary, "blockHash", initialBlockHash.Hex())
				}
				if w.HasSubmittedInitialEpoch() {
					return
				}
			}
		}
	}
}

// Stop terminates any background goroutines (such as the initial epoch waiter) cleanly.
func (w *PowWorker) Stop() {
	w.waiterMu.Lock()
	if !w.waiterRunning {
		w.waiterMu.Unlock()
		return
	}
	w.waiterRunning = false
	close(w.waiterStopCh)
	doneCh := w.waiterDoneCh
	w.waiterMu.Unlock()

	if doneCh != nil {
		<-doneCh
	}
}

// IsWaiterRunning reports whether the initial epoch background waiter is currently running.
func (w *PowWorker) IsWaiterRunning() bool {
	w.waiterMu.Lock()
	defer w.waiterMu.Unlock()
	return w.waiterRunning
}
