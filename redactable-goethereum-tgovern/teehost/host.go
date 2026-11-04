package teehost

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contractService"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

const (
	selectionVRFSubmitAttempts = 3
	selectionVRFRetryDelay     = time.Second
)

type ContractBackend interface {
	SubscribeTEEEvents(ch chan<- contractService.TEEEvent) event.Subscription
	SubmitTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (string, error)
	GetActiveTEEs() ([][32]byte, error)
	GetActiveSetInfo(epoch *big.Int) (*contractService.ActiveSetInfo, error)
	GetTEEInfo(sigPubKey [32]byte) (*contractService.TEEInfo, error)
	GetTEEEnclavePK(sigPubKey [32]byte) ([]byte, error)
	GetSystemParams() (*contractService.SystemParams, error)
	GetPkCH() ([]byte, error)
	SetPkCH(pkCH []byte, signature []byte) (string, error)
	ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error)
	ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (string, error)
	WaitTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	SubmitEraseProof(sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (string, error)
	ContractAddress() common.Address
	ChainID(ctx context.Context) (*big.Int, error)
}

type TEEBackend interface {
	Identity(ctx context.Context) (TEEIdentity, error)
	EvalVRF(ctx context.Context, req VRFRequest) (VRFResponse, error)
	Close()
}

type TEEProtocolBackend interface {
	DistKGCreateDeal(ctx context.Context, req DistKGCreateDealRequest) (TEEDealResult, error)
	DistKGVerifyShare(ctx context.Context, req DistKGVerifyShareRequest) (bool, error)
	DistKGExtract(ctx context.Context, req DistKGExtractRequest) (TEEExtractResult, error)
	DKRSCreateDeal(ctx context.Context, req DKRSCreateDealRequest) (TEEDealResult, error)
	DKRSVerifyShare(ctx context.Context, req DKRSVerifyShareRequest) (bool, error)
	DKRSExtract(ctx context.Context, req DKRSExtractRequest) (TEEExtractResult, error)
	PartialAdapt(ctx context.Context, req PartialAdaptRequest) (PartialAdaptResult, error)
	EraseShare(ctx context.Context, req EraseShareRequest) (EraseShareResult, error)
}

type TEEPeerEPKRegistrar interface {
	RegisterPeerEPK(ctx context.Context, peerIndex uint64, peerPK []byte) error
}

type RedactionExecutor interface {
	ExecuteTEEClusterRedactionFromMaterial(ctx context.Context, req RedactionExecutionRequest) (map[string]interface{}, error)
}

type RedactionExecutionRequest struct {
	ReqID      [32]byte
	BlockNum   *big.Int
	Index      uint64
	ModifyType uint64
	DataAfter  []byte
	PiReqA     []byte
	RPrime     []byte
}

type TEEIdentity struct {
	HardwareID [32]byte
	SigPubKey  [32]byte
	VRFPubKey  [32]byte
}

func (id TEEIdentity) primaryKey() [32]byte {
	if id.SigPubKey != ([32]byte{}) {
		return id.SigPubKey
	}
	return id.HardwareID
}

type VRFRequest struct {
	ChainID         []byte
	ContractAddress []byte
	Epoch           uint64
	HardwareID      [32]byte
	VRFPubKey       [32]byte
	Seed            [32]byte
}

type VRFResponse struct {
	VRFResult [32]byte
	VRFProof  []byte
}

type TEEAttestationInputs struct {
	CertEAMsg             []byte
	CertEASig             []byte
	ActiveTEEList         []byte
	ActiveSetCertInput    []byte
	ActiveTEECount        uint32
	ActiveSetCertInputLen uint32
}

type TEEDealResult struct {
	CommitmentsA     []byte
	CommitmentB0     []byte
	SharesSerialized []byte
	G1Bytes          []byte
	G2Bytes          []byte
	PHex             string
}

type TEEExtractResult struct {
	GlobalPK        []byte
	GlobalPKG1      []byte
	VerificationKey []byte
	PkCHSignature   []byte
}

type DistKGCreateDealRequest struct {
	DealerIndex int
	Threshold   int
	NumParties  int
	TEEAttestationInputs
}

type DistKGVerifyShareRequest struct {
	ReceiverIndex int
	DealerIndex   int
	Threshold     int
	NumParties    int
	CommitmentsA  []byte
	CommitmentB0  []byte
	ShareBytes    []byte
	TEEAttestationInputs
}

type DistKGExtractRequest struct {
	MyIndex       int
	Threshold     int
	NumParties    int
	QualIndices   []int32
	B0Data        []byte
	B0DealerMap   []int32
	A0Data        []byte
	A0DealerMap   []int32
	SharesData    []byte
	SharesFromMap []int32
	ChainID       []byte
	Contract      []byte
	TEEOwner      []byte
	TEEAttestationInputs
}

type DKRSCreateDealRequest struct {
	DealerIndex   int
	NewThreshold  int
	NewNumParties int
	TEEAttestationInputs
}

type DKRSVerifyShareRequest struct {
	ReceiverIndex int
	DealerIndex   int
	NewThreshold  int
	NewNumParties int
	CommitmentsA  []byte
	CommitmentB0  []byte
	ShareBytes    []byte
	TEEAttestationInputs
}

type DKRSExtractRequest struct {
	MyIndex       int
	NewThreshold  int
	NewNumParties int
	OldNumParties int
	QualOld       []int32
	B0Data        []byte
	B0DealerMap   []int32
	A0Data        []byte
	A0DealerMap   []int32
	SharesData    []byte
	SharesFromMap []int32
	TEEAttestationInputs
}

type PartialAdaptRequest struct {
	SourceMessage          []byte
	TargetMessage          []byte
	CertReqAuthMsg         []byte
	CertReqAuthSig         []byte
	DReq                   []byte
	DPolicy                []byte
	CommitteePubKeyVersion []byte
	ExpiresAt              []byte
	DReqInput              []byte
	TEEAttestationInputs
}

type PartialAdaptResult struct {
	Contribution   []byte
	ProofChallenge []byte
	ProofResponse  []byte
	Y              []byte
	Sigma          []byte
}

type EraseShareRequest struct {
	Epoch           uint64
	NodeIndex       int
	ChainID         []byte
	ContractAddress []byte
	TEEOwner        []byte
}

type EraseShareResult struct {
	EraseProofHash [32]byte
	Proof          []byte
}

type pendingEraseRequest struct {
	Epoch *big.Int
}

type HostConfig struct {
	EventBuffer       int
	MaterialStore     RedactionMaterialStore
	RedactionExecutor RedactionExecutor
	TimingSink        func(stage string, event string, fields ...interface{})
}

type Host struct {
	cfg      HostConfig
	contract ContractBackend
	tee      TEEBackend

	identity TEEIdentity
	events   chan contractService.TEEEvent
	sub      event.Subscription

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu                 sync.Mutex
	submitted          map[string]struct{}
	registered         bool
	active             bool
	revoked            bool
	currentEpoch       *big.Int
	activeSetHash      [32]byte
	activeSet          [][32]byte
	activeSetByEpoch   map[string][][32]byte
	startedDistKG      map[string]struct{}
	startedDKRS        map[string]struct{}
	freshShareEpochs   map[string]struct{}
	submittedErase     map[string]struct{}
	pendingErase       map[string]*pendingEraseRequest
	dkrsDealerDone     map[string]struct{}
	rotationByOldEpoch map[string]string
	pendingAuth        map[[32]byte]*RedactAuthorization
	authorizedTiming   map[[32]byte]struct{}
	executingRedact    map[[32]byte]struct{}
	executedRedact     map[[32]byte]struct{}

	network   ProtocolNetwork
	materials RedactionMaterialStore
	executor  RedactionExecutor

	protocolMu    sync.Mutex
	dkrsEPKMu     sync.Mutex
	distKGSession *DistKGSessionState
	dkrsSession   *DKRSSessionState
	partialAdapt  map[[32]byte]*PartialAdaptSessionState

	pendingDistKG       []DistKGProtocolMessage
	pendingDKRS         map[[32]byte][]DKRSProtocolMessage
	pendingPartialAdapt map[[32]byte][]PartialAdaptProtocolMessage
	dkrsOutbound        map[[32]byte]*dkrsOutboundCache
	dkrsOutboundOrder   [][32]byte
}

type dkrsOutboundCache struct {
	DealerIndex  int
	OldActiveSet [][32]byte
	NewActiveSet [][32]byte
	Deal         *DKRSDealMessage
	Shares       map[int]*DKRSShareMessage
}

func NewHost(contract ContractBackend, tee TEEBackend, cfg HostConfig) (*Host, error) {
	if contract == nil {
		return nil, fmt.Errorf("contract backend is nil")
	}
	if tee == nil {
		return nil, fmt.Errorf("tee backend is nil")
	}
	if cfg.EventBuffer == 0 {
		cfg.EventBuffer = 64
	}
	if cfg.MaterialStore == nil {
		cfg.MaterialStore = NewMemoryRedactionMaterialStore()
	}
	return &Host{
		cfg:                 cfg,
		contract:            contract,
		tee:                 tee,
		submitted:           make(map[string]struct{}),
		activeSetByEpoch:    make(map[string][][32]byte),
		startedDistKG:       make(map[string]struct{}),
		startedDKRS:         make(map[string]struct{}),
		freshShareEpochs:    make(map[string]struct{}),
		submittedErase:      make(map[string]struct{}),
		pendingErase:        make(map[string]*pendingEraseRequest),
		dkrsDealerDone:      make(map[string]struct{}),
		rotationByOldEpoch:  make(map[string]string),
		pendingAuth:         make(map[[32]byte]*RedactAuthorization),
		authorizedTiming:    make(map[[32]byte]struct{}),
		executingRedact:     make(map[[32]byte]struct{}),
		executedRedact:      make(map[[32]byte]struct{}),
		materials:           cfg.MaterialStore,
		executor:            cfg.RedactionExecutor,
		pendingDKRS:         make(map[[32]byte][]DKRSProtocolMessage),
		pendingPartialAdapt: make(map[[32]byte][]PartialAdaptProtocolMessage),
		dkrsOutbound:        make(map[[32]byte]*dkrsOutboundCache),
	}, nil
}

func (h *Host) SetProtocolNetwork(network ProtocolNetwork) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.network = network
}

func (h *Host) SetRedactionExecutor(executor RedactionExecutor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.executor = executor
}

func (h *Host) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	identity, err := h.tee.Identity(parent)
	if err != nil {
		return err
	}
	if identity.HardwareID == ([32]byte{}) {
		return fmt.Errorf("tee hardware id is empty")
	}
	if identity.SigPubKey == ([32]byte{}) {
		log.Warn("TEE identity missing sigPubKey; falling back to legacy hardwareID identity key")
		identity.SigPubKey = identity.HardwareID
	}
	if identity.VRFPubKey == ([32]byte{}) {
		return fmt.Errorf("tee vrf public key is empty")
	}
	h.identity = identity
	h.events = make(chan contractService.TEEEvent, h.cfg.EventBuffer)
	h.sub = h.contract.SubscribeTEEEvents(h.events)
	h.ctx, h.cancel = context.WithCancel(parent)
	h.wg.Add(1)
	go h.loop()
	return nil
}

// HardwareID returns the 32-byte TEE identity key used by the networking layer.
// Deprecated name: the returned value is sigPubKey. The method name is kept for
// existing protocol interfaces and wire structs that still carry HardwareID.
func (h *Host) HardwareID() [32]byte {
	return h.identity.primaryKey()
}

// DistKGRecipient resolves an index using the membership order of this session.
func (h *Host) DistKGRecipient(index int) ([32]byte, bool) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	if h.distKGSession == nil {
		return [32]byte{}, false
	}
	return activeSetKeyAt(h.distKGSession.Config.ActiveSet, index)
}

func (h *Host) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.sub != nil {
		h.sub.Unsubscribe()
	}
	h.wg.Wait()
	if h.tee != nil {
		h.tee.Close()
	}
}

func (h *Host) loop() {
	defer h.wg.Done()
	for {
		select {
		case ev := <-h.events:
			if err := h.handleEvent(h.ctx, ev); err != nil {
				log.Warn("TEE host event handling failed", "kind", ev.Kind, "err", err)
			}
		case err := <-h.sub.Err():
			if err != nil {
				log.Warn("TEE host subscription ended", "err", err)
			}
			return
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *Host) handleEvent(ctx context.Context, ev contractService.TEEEvent) error {
	switch ev.Kind {
	case contractService.TEEEventRotationPreparationRequested:
		if ev.RotationRequested == nil {
			return fmt.Errorf("rotation event payload is nil")
		}
		return h.handleRotationRequested(ctx, ev.RotationRequested.NextEpoch, ev.RotationRequested.Seed, rotationTrigger{block: ev.BlockNumber, tx: ev.TxHash})
	case contractService.TEEEventVRFSubmitted:
		if ev.VRFSubmitted != nil && ev.VRFSubmitted.HardwareId == h.identity.primaryKey() {
			h.markSubmitted(ev.VRFSubmitted.Epoch)
		}
	case contractService.TEEEventRegistered:
		if ev.Registered == nil {
			return nil
		}
		legacyHardwareIDMatch := ev.Registered.SigPubKey == ([32]byte{}) && ev.Registered.HardwareId == h.identity.HardwareID
		if ev.Registered.SigPubKey == h.identity.primaryKey() || legacyHardwareIDMatch {
			h.mu.Lock()
			h.registered = true
			h.mu.Unlock()
			sigPubKey := h.identity.primaryKey()
			log.Info("TEE host observed local registration", "hardwareID", hexKey(h.identity.HardwareID), "sigPubKey", hexKey(sigPubKey), "eventHardwareID", hexKey(ev.Registered.HardwareId), "eventSigPubKey", hexKey(ev.Registered.SigPubKey))
			go h.syncCurrentActiveSetUntilActive(h.ctx)
		}
	case contractService.TEEEventActiveSetCheckpointed:
		if ev.ActiveSetCheckpointed == nil {
			return fmt.Errorf("active set checkpoint payload is nil")
		}
		return h.handleActiveSet(ctx, ev.ActiveSetCheckpointed.Epoch, ev.ActiveSetCheckpointed.ActiveSetHash, ev.ActiveSetCheckpointed.HardwareIds, false, rotationTrigger{block: ev.BlockNumber, tx: ev.TxHash})
	case contractService.TEEEventActiveSetRotated:
		if ev.ActiveSetRotated == nil {
			return fmt.Errorf("active set rotated payload is nil")
		}
		return h.handleActiveSet(ctx, ev.ActiveSetRotated.Epoch, ev.ActiveSetRotated.ActiveSetHash, ev.ActiveSetRotated.HardwareIds, true, rotationTrigger{block: ev.BlockNumber, tx: ev.TxHash})
	case contractService.TEEEventRedactAuthorized:
		if ev.RedactAuthorized == nil {
			return fmt.Errorf("redact authorized payload is nil")
		}
		return h.handleRedactAuthorized(ctx, ev.RedactAuthorized)
	case contractService.TEEEventRedactExecuted:
		if ev.RedactExecuted != nil {
			h.markRedactionClosed(ev.RedactExecuted.ReqID)
		}
	case contractService.TEEEventRedactExpired:
		if ev.RedactExpired != nil {
			h.markRedactionClosed(ev.RedactExpired.ReqID)
		}
	case contractService.TEEEventRedactRejected:
		if ev.RedactRejected != nil {
			h.markRedactionClosed(ev.RedactRejected.ReqID)
		}
	case contractService.TEEEventExitPending:
		if ev.ExitPending != nil && ev.ExitPending.HardwareId == h.identity.primaryKey() {
			h.rememberExitRotation(ev.ExitPending.FromEpoch, ev.ExitPending.NextEpoch)
			return h.handleEraseRequested(ctx, ev.ExitPending.FromEpoch)
		}
	case contractService.TEEEventEraseProofDeadlineSet:
		if ev.EraseProofDeadlineSet != nil && ev.EraseProofDeadlineSet.HardwareId == h.identity.primaryKey() {
			return h.handleEraseRequested(ctx, ev.EraseProofDeadlineSet.ExitEpoch)
		}
	case contractService.TEEEventExited:
		if ev.Exited != nil && ev.Exited.HardwareId == h.identity.primaryKey() {
			h.markLocalInactive()
		}
	case contractService.TEEEventRevoked:
		if ev.Revoked != nil && ev.Revoked.HardwareId == h.identity.primaryKey() {
			h.markLocalRevoked()
		}
	case contractService.TEEEventPkCHSet:
		h.protocolMu.Lock()
		// A peer publishing the global key does not mean this host has
		// extracted its own share. Keep collecting late DistKG inputs.
		if h.distKGSession != nil && h.distKGSession.Extracted != nil {
			h.distKGSession = nil
		}
		h.protocolMu.Unlock()
	}
	return nil
}

func (h *Host) handleRotationRequested(ctx context.Context, epoch *big.Int, seed [32]byte, trigger rotationTrigger) error {
	if epoch == nil || epoch.Sign() < 0 {
		return fmt.Errorf("invalid rotation epoch")
	}
	fields := append([]interface{}{"duration_ns", int64(0),
		"epoch", epochString(epoch), "status", "ok"}, trigger.fields()...)
	h.emitTiming("rotation_flow", "rotation_prep_requested", fields...)
	if h.isSubmitted(epoch) {
		return nil
	}
	h.markSubmitted(epoch)

	chainID, err := h.contract.ChainID(ctx)
	if err != nil {
		h.unmarkSubmitted(epoch)
		return err
	}
	if chainID == nil || chainID.Sign() <= 0 {
		h.unmarkSubmitted(epoch)
		return fmt.Errorf("invalid chain id")
	}
	sigPubKey := h.identity.primaryKey()
	req := VRFRequest{
		ChainID:         chainID.FillBytes(make([]byte, 32)),
		ContractAddress: common.LeftPadBytes(h.contract.ContractAddress().Bytes(), 32),
		Epoch:           epoch.Uint64(),
		HardwareID:      sigPubKey,
		VRFPubKey:       h.identity.VRFPubKey,
		Seed:            seed,
	}
	resp, err := h.tee.EvalVRF(ctx, req)
	if err != nil {
		h.unmarkSubmitted(epoch)
		return err
	}
	if len(resp.VRFProof) != 65 {
		h.unmarkSubmitted(epoch)
		return fmt.Errorf("invalid vrf proof length: got %d, want 65", len(resp.VRFProof))
	}
	if err := h.submitSelectionVRFWithRetry(ctx, epoch, sigPubKey, resp); err != nil {
		h.unmarkSubmitted(epoch)
		return err
	}
	return nil
}

func (h *Host) submitSelectionVRFWithRetry(ctx context.Context, epoch *big.Int, sigPubKey [32]byte, resp VRFResponse) error {
	var lastErr error
	for attempt := 1; attempt <= selectionVRFSubmitAttempts; attempt++ {
		txHash, err := h.contract.SubmitTEESelectionVRF(epoch, sigPubKey, resp.VRFResult, resp.VRFProof)
		if err == nil {
			log.Info("TEE host submitted selection VRF", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "tx", txHash, "attempt", attempt)
			return nil
		}
		lastErr = err
		log.Warn("TEE host selection VRF submit failed", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "attempt", attempt, "maxAttempts", selectionVRFSubmitAttempts, "err", err)
		if attempt == selectionVRFSubmitAttempts {
			break
		}
		if err := sleepContext(ctx, time.Duration(attempt)*selectionVRFRetryDelay); err != nil {
			return err
		}
	}
	return fmt.Errorf("submit selection VRF failed after %d attempts: %w", selectionVRFSubmitAttempts, lastErr)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (h *Host) markSubmitted(epoch *big.Int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.submitted[h.submissionKey(epoch)] = struct{}{}
}

func (h *Host) unmarkSubmitted(epoch *big.Int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.submitted, h.submissionKey(epoch))
}

func (h *Host) isSubmitted(epoch *big.Int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.submitted[h.submissionKey(epoch)]
	return ok
}

func (h *Host) submissionKey(epoch *big.Int) string {
	sigPubKey := h.identity.primaryKey()
	return epoch.String() + ":" + common.Bytes2Hex(sigPubKey[:])
}
