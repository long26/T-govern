package teehost

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

type ProtocolNetwork interface {
	BroadcastDistKG(ctx context.Context, msg DistKGProtocolMessage) error
	SendDistKGShare(ctx context.Context, toIndex int, msg DistKGProtocolMessage) error
	BroadcastDKRS(ctx context.Context, msg DKRSProtocolMessage) error
	SendDKRS(ctx context.Context, toSigPubKey [32]byte, msg DKRSProtocolMessage) error
	BroadcastPartialAdapt(ctx context.Context, msg PartialAdaptProtocolMessage) error
	BroadcastRedactMaterial(ctx context.Context, material RedactionMaterial) error
	BroadcastRedactAuthorized(ctx context.Context, msg RedactAuthorizedMessage) error
}

type DistKGProtocolConfig struct {
	MyIndex     int
	Epoch       *big.Int
	Threshold   int
	NumParties  int
	ActiveSet   [][32]byte
	Attestation TEEAttestationInputs
}

type DistKGProtocolMessage struct {
	RelayTTL  uint64
	Deal      *DistKGDealMessage
	Share     *DistKGShareMessage
	Complaint *DistKGComplaintMessage
	VerifyKey *DistKGVerifyKeyMessage
}

const (
	protocolMessageKindNone uint64 = iota
	protocolMessageKindDeal
	protocolMessageKindShare
	protocolMessageKindComplaint
	protocolMessageKindVerifyKey
	protocolMessageKindInputRequest
	protocolMessageKindInputResponse
)

const (
	maxDKRSOutboundCacheSessions = 5
	dkrsInputFetchInitialDelay   = 3 * time.Second
	dkrsInputFetchBaseDelay      = 2 * time.Second
	dkrsInputFetchMaxDelay       = 16 * time.Second
	dkrsInputFetchMaxAttempts    = 6
)

type distKGProtocolMessageRLP struct {
	Deal          *DistKGDealMessage      `rlp:"nil"`
	Share         *DistKGShareMessage     `rlp:"nil"`
	Complaint     *DistKGComplaintMessage `rlp:"nil"`
	Response      rlp.RawValue
	VerifyKey     *DistKGVerifyKeyMessage `rlp:"nil"`
	InputRequest  rlp.RawValue
	InputResponse rlp.RawValue
	RelayTTL      uint64
}

func (m DistKGProtocolMessage) payloadCount() int {
	count := 0
	for _, present := range []bool{m.Deal != nil, m.Share != nil, m.Complaint != nil, m.VerifyKey != nil} {
		if present {
			count++
		}
	}
	return count
}

func (m DistKGProtocolMessage) EncodeRLP(w io.Writer) error {
	if m.payloadCount() > 1 {
		return errors.New("multiple DistKG payloads")
	}
	wire := distKGProtocolMessageRLP{
		Deal: m.Deal, Share: m.Share, Complaint: m.Complaint, VerifyKey: m.VerifyKey, RelayTTL: m.RelayTTL,
		Response: rlp.RawValue{0xc0}, InputRequest: rlp.RawValue{0xc0}, InputResponse: rlp.RawValue{0xc0},
	}
	if m.Deal != nil {
		deal := *m.Deal
		var err error
		deal.PHex, err = canonicalModulusHex(deal.PHex)
		if err != nil {
			return err
		}
		wire.Deal = &deal
	}
	return rlp.Encode(w, wire)
}

func (m *DistKGProtocolMessage) DecodeRLP(s *rlp.Stream) error {
	var wire distKGProtocolMessageRLP
	if err := s.Decode(&wire); err != nil {
		return err
	}
	for _, reserved := range []rlp.RawValue{wire.Response, wire.InputRequest, wire.InputResponse} {
		if len(reserved) != 1 || reserved[0] != 0xc0 {
			return errors.New("unsupported DistKG payload slot")
		}
	}
	out := DistKGProtocolMessage{RelayTTL: wire.RelayTTL, Deal: wire.Deal, Share: wire.Share, Complaint: wire.Complaint, VerifyKey: wire.VerifyKey}
	if out.payloadCount() > 1 {
		return errors.New("multiple DistKG payloads")
	}
	if out.Deal != nil {
		canonical, err := canonicalModulusHex(out.Deal.PHex)
		if err != nil || canonical != out.Deal.PHex {
			return errors.New("noncanonical DistKG modulus")
		}
	}
	*m = out
	return nil
}

type DistKGDealMessage struct {
	DealerIndex  uint64
	Threshold    uint64
	NumParties   uint64
	CommitmentsA []byte
	CommitmentB0 []byte
	G1Bytes      []byte
	G2Bytes      []byte
	PHex         string
}

type DistKGShareMessage struct {
	FromIndex uint64
	ToIndex   uint64
	Payload   []byte
}

type DistKGComplaintMessage struct {
	ComplainantIndex uint64
	DealerIndex      uint64
}

type DistKGVerifyKeyMessage struct {
	Index           uint64
	VerificationKey []byte
}

type DKRSProtocolConfig struct {
	DealerIndex   int
	ReceiverIndex int
	SessionID     [32]byte
	OldEpoch      *big.Int
	NewEpoch      *big.Int
	OldThreshold  int
	NewThreshold  int
	NewNumParties int
	OldNumParties int
	OldQual       []int
	OldActiveSet  [][32]byte
	NewActiveSet  [][32]byte
	Attestation   TEEAttestationInputs
}

type DKRSProtocolMessage struct {
	SessionID     [32]byte
	RelayTTL      uint64
	Deal          *DKRSDealMessage
	Share         *DKRSShareMessage
	Complaint     *DKRSComplaintMessage
	VerifyKey     *DKRSVerifyKeyMessage
	InputRequest  *DKRSInputRequestMessage
	InputResponse *DKRSInputResponseMessage
}

type dkrsProtocolMessageRLP struct {
	SessionID     [32]byte
	RelayTTL      uint64
	Kind          uint64
	Deal          DKRSDealMessage
	Share         DKRSShareMessage
	Complaint     DKRSComplaintMessage
	VerifyKey     DKRSVerifyKeyMessage
	InputRequest  DKRSInputRequestMessage
	InputResponse DKRSInputResponseMessage
}

func (m DKRSProtocolMessage) EncodeRLP(w io.Writer) error {
	wire := dkrsProtocolMessageRLP{SessionID: m.SessionID, RelayTTL: m.RelayTTL}
	switch {
	case m.Deal != nil:
		wire.Kind = protocolMessageKindDeal
		wire.Deal = *m.Deal
	case m.Share != nil:
		wire.Kind = protocolMessageKindShare
		wire.Share = *m.Share
	case m.Complaint != nil:
		wire.Kind = protocolMessageKindComplaint
		wire.Complaint = *m.Complaint
	case m.VerifyKey != nil:
		wire.Kind = protocolMessageKindVerifyKey
		wire.VerifyKey = *m.VerifyKey
	case m.InputRequest != nil:
		wire.Kind = protocolMessageKindInputRequest
		wire.InputRequest = *m.InputRequest
	case m.InputResponse != nil:
		wire.Kind = protocolMessageKindInputResponse
		wire.InputResponse = *m.InputResponse
	default:
		wire.Kind = protocolMessageKindNone
	}
	return rlp.Encode(w, wire)
}

func (m *DKRSProtocolMessage) DecodeRLP(s *rlp.Stream) error {
	var wire dkrsProtocolMessageRLP
	if err := s.Decode(&wire); err != nil {
		return err
	}
	*m = DKRSProtocolMessage{SessionID: wire.SessionID, RelayTTL: wire.RelayTTL}
	switch wire.Kind {
	case protocolMessageKindNone:
	case protocolMessageKindDeal:
		m.Deal = &wire.Deal
	case protocolMessageKindShare:
		m.Share = &wire.Share
	case protocolMessageKindComplaint:
		m.Complaint = &wire.Complaint
	case protocolMessageKindVerifyKey:
		m.VerifyKey = &wire.VerifyKey
	case protocolMessageKindInputRequest:
		m.InputRequest = &wire.InputRequest
	case protocolMessageKindInputResponse:
		m.InputResponse = &wire.InputResponse
	default:
		return fmt.Errorf("unknown DKRS protocol message kind %d", wire.Kind)
	}
	return nil
}

type DKRSDealMessage struct {
	DealerIndex   uint64
	OldThreshold  uint64
	OldNumParties uint64
	NewThreshold  uint64
	NewNumParties uint64
	CommitmentsA  []byte
	CommitmentB0  []byte // TEE-specific G2 commitment required by the unchanged enclave.
}

type DKRSShareMessage struct {
	FromIndex uint64
	ToIndex   uint64
	Payload   []byte
}

type DKRSComplaintMessage struct {
	ComplainantIndex uint64
	DealerIndex      uint64
}

type DKRSVerifyKeyMessage struct {
	Index           uint64
	VerificationKey []byte
}

type DKRSInputRequestMessage struct {
	RequesterIndex uint64
	DealerIndex    uint64
	NeedDeal       bool
	NeedShare      bool
	Nonce          [32]byte
	Attempt        uint64
}

type DKRSInputResponseMessage struct {
	RequesterIndex uint64
	DealerIndex    uint64
	Nonce          [32]byte
	HasDeal        bool
	Deal           DKRSDealMessage
	HasShare       bool
	Share          DKRSShareMessage
}

type PartialAdaptProtocolConfig struct {
	ReqID         [32]byte
	MyIndex       int
	Threshold     int
	NumParties    int
	SourceMessage []byte
	TargetMessage []byte
	PartialAdaptRequest
}

type PartialAdaptProtocolMessage struct {
	RelayTTL     uint64
	Contribution *PartialAdaptContributionMessage
	Aggregate    *PartialAdaptAggregateMessage
}

type partialAdaptProtocolMessageRLP struct {
	Kind         uint64
	RelayTTL     uint64
	Contribution PartialAdaptContributionMessage
	Aggregate    PartialAdaptAggregateMessage
}

func (m PartialAdaptProtocolMessage) EncodeRLP(w io.Writer) error {
	wire := partialAdaptProtocolMessageRLP{RelayTTL: m.RelayTTL}
	switch {
	case m.Contribution != nil:
		wire.Kind = protocolMessageKindDeal
		wire.Contribution = *m.Contribution
	case m.Aggregate != nil:
		wire.Kind = protocolMessageKindShare
		wire.Aggregate = *m.Aggregate
	default:
		wire.Kind = protocolMessageKindNone
	}
	return rlp.Encode(w, wire)
}

func (m *PartialAdaptProtocolMessage) DecodeRLP(s *rlp.Stream) error {
	var wire partialAdaptProtocolMessageRLP
	if err := s.Decode(&wire); err != nil {
		return err
	}
	*m = PartialAdaptProtocolMessage{RelayTTL: wire.RelayTTL}
	switch wire.Kind {
	case protocolMessageKindNone:
	case protocolMessageKindDeal:
		m.Contribution = &wire.Contribution
	case protocolMessageKindShare:
		m.Aggregate = &wire.Aggregate
	default:
		return fmt.Errorf("unknown partial adapt protocol message kind %d", wire.Kind)
	}
	return nil
}

type PartialAdaptContributionMessage struct {
	ReqID          [32]byte
	Index          uint64
	Contribution   []byte
	ProofChallenge []byte
	ProofResponse  []byte
	Y              []byte
	Sigma          []byte
}

type PartialAdaptAggregateMessage struct {
	ReqID               [32]byte
	FromIndex           uint64
	Contributions       []PartialAdaptContributionMessage
	FramedContributions []byte
}

type DistKGSessionState struct {
	Config             DistKGProtocolConfig
	MyDeal             *DistKGDealMessage
	Deals              map[int]*DistKGDealMessage
	Shares             map[int]*DistKGShareMessage
	Complaints         []DistKGComplaintMessage
	ComplaintsSent     bool
	Extracted          *TEEExtractResult
	VerifyKeyBroadcast bool
}

type DKRSSessionState struct {
	Config             DKRSProtocolConfig
	MyDeal             *DKRSDealMessage
	Deals              map[int]*DKRSDealMessage
	Shares             map[int]*DKRSShareMessage
	Complaints         []DKRSComplaintMessage
	InvalidShares      map[int]struct{}
	ComplaintsSent     bool
	SharesVerified     bool
	Extracted          *TEEExtractResult
	VerifyKeyBroadcast bool
	InputsReadyLogged  bool
	QualReadyLogged    bool
}

type PartialAdaptSessionState struct {
	Config      PartialAdaptProtocolConfig
	Shares      map[int]*PartialAdaptContributionMessage
	Aggregate   *PartialAdaptAggregateMessage
	Aggregated  bool
	LocalResult *PartialAdaptResult
}

func wireIndex(index int) uint64 {
	if index < 0 {
		return 0
	}
	return uint64(index)
}

func hostIndex(index uint64) int {
	return int(index)
}

func activeSetKeyAt(activeSet [][32]byte, index int) ([32]byte, bool) {
	if index < 1 || index > len(activeSet) {
		return [32]byte{}, false
	}
	key := activeSet[index-1]
	if key == ([32]byte{}) {
		return [32]byte{}, false
	}
	return key, true
}

func (h *Host) StartDistKG(ctx context.Context, cfg DistKGProtocolConfig, network ProtocolNetwork) (*DistKGSessionState, error) {
	if network == nil {
		return nil, errors.New("protocol network is nil")
	}
	if cfg.MyIndex < 1 || cfg.MyIndex > cfg.NumParties {
		return nil, fmt.Errorf("my index must be in [1,%d]", cfg.NumParties)
	}
	if cfg.Threshold < 0 || cfg.Threshold >= cfg.NumParties {
		return nil, fmt.Errorf("invalid threshold/numParties: %d/%d", cfg.Threshold, cfg.NumParties)
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return nil, errors.New("tee backend does not implement protocol operations")
	}
	result, err := backend.DistKGCreateDeal(ctx, DistKGCreateDealRequest{
		DealerIndex:          cfg.MyIndex,
		Threshold:            cfg.Threshold,
		NumParties:           cfg.NumParties,
		TEEAttestationInputs: cfg.Attestation,
	})
	if err != nil {
		return nil, err
	}
	deal := &DistKGDealMessage{
		DealerIndex:  wireIndex(cfg.MyIndex),
		Threshold:    wireIndex(cfg.Threshold),
		NumParties:   wireIndex(cfg.NumParties),
		CommitmentsA: append([]byte(nil), result.CommitmentsA...),
		CommitmentB0: append([]byte(nil), result.CommitmentB0...),
		G1Bytes:      append([]byte(nil), result.G1Bytes...),
		G2Bytes:      append([]byte(nil), result.G2Bytes...),
		PHex:         result.PHex,
	}
	shareFrames, err := splitTEEFramedShares(result.SharesSerialized, cfg.NumParties)
	if err != nil {
		return nil, err
	}
	state := &DistKGSessionState{
		Config: cfg,
		MyDeal: deal,
		Deals:  map[int]*DistKGDealMessage{cfg.MyIndex: deal},
		Shares: map[int]*DistKGShareMessage{
			cfg.MyIndex: {
				FromIndex: wireIndex(cfg.MyIndex),
				ToIndex:   wireIndex(cfg.MyIndex),
				Payload:   append([]byte(nil), shareFrames[cfg.MyIndex]...),
			},
		},
	}
	h.setDistKGSession(state)
	if err := network.BroadcastDistKG(ctx, DistKGProtocolMessage{Deal: deal}); err != nil {
		return nil, err
	}
	for receiverIndex, payload := range shareFrames {
		if len(payload) == 0 {
			continue
		}
		msg := DistKGProtocolMessage{
			Share: &DistKGShareMessage{
				FromIndex: wireIndex(cfg.MyIndex),
				ToIndex:   wireIndex(receiverIndex),
				Payload:   payload,
			},
		}
		if err := network.SendDistKGShare(ctx, receiverIndex, msg); err != nil {
			return nil, err
		}
	}
	for _, pending := range h.takePendingDistKG() {
		if err := h.HandleDistKG(ctx, pending, network); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (h *Host) HandleDistKG(ctx context.Context, msg DistKGProtocolMessage, network ProtocolNetwork) error {
	if network == nil {
		return errors.New("protocol network is nil")
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return errors.New("tee backend does not implement protocol operations")
	}
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	if msg.Share != nil {
		log.Debug("TEE host received DistKG share", "fromIndex", msg.Share.FromIndex, "toIndex", msg.Share.ToIndex, "sessionStarted", h.distKGSession != nil)
	}
	if h.distKGSession == nil {
		h.pendingDistKG = append(h.pendingDistKG, cloneDistKGProtocolMessage(msg))
		log.Debug("TEE host queued DistKG message until session starts")
		return nil
	}
	state := h.distKGSession
	if state.Deals == nil {
		state.Deals = make(map[int]*DistKGDealMessage)
	}
	if state.Shares == nil {
		state.Shares = make(map[int]*DistKGShareMessage)
	}
	if msg.Deal != nil {
		state.Deals[hostIndex(msg.Deal.DealerIndex)] = cloneDistKGDealMessage(msg.Deal)
	}
	if msg.Share != nil && hostIndex(msg.Share.ToIndex) == state.Config.MyIndex {
		state.Shares[hostIndex(msg.Share.FromIndex)] = cloneDistKGShareMessage(msg.Share)
	}
	if msg.Complaint != nil {
		state.Complaints = append(state.Complaints, *msg.Complaint)
	}
	if !distKGReadyForExtract(state) {
		log.Debug("TEE host waiting for DistKG inputs", "index", state.Config.MyIndex, "deals", len(state.Deals), "shares", len(state.Shares), "needed", state.Config.NumParties)
		return nil
	}
	if !state.ComplaintsSent {
		for dealerIndex := 1; dealerIndex <= state.Config.NumParties; dealerIndex++ {
			deal := state.Deals[dealerIndex]
			share := state.Shares[dealerIndex]
			if deal == nil || share == nil {
				continue
			}
			ok, err := backend.DistKGVerifyShare(ctx, DistKGVerifyShareRequest{
				ReceiverIndex:        state.Config.MyIndex,
				DealerIndex:          dealerIndex,
				Threshold:            state.Config.Threshold,
				NumParties:           state.Config.NumParties,
				CommitmentsA:         deal.CommitmentsA,
				CommitmentB0:         deal.CommitmentB0,
				ShareBytes:           share.Payload,
				TEEAttestationInputs: state.Config.Attestation,
			})
			if err != nil || !ok {
				complaint := DistKGComplaintMessage{ComplainantIndex: wireIndex(state.Config.MyIndex), DealerIndex: wireIndex(dealerIndex)}
				state.Complaints = append(state.Complaints, complaint)
				if err := network.BroadcastDistKG(ctx, DistKGProtocolMessage{Complaint: &complaint}); err != nil {
					return err
				}
			}
		}
		state.ComplaintsSent = true
	}
	if state.Extracted != nil {
		return nil
	}
	qual := distKGQual(state)
	if len(qual) < state.Config.Threshold+1 {
		log.Debug("TEE host waiting for DistKG qualified inputs", "index", state.Config.MyIndex, "qual", len(qual), "needed", state.Config.Threshold+1)
		return nil
	}
	extractReq, err := buildDistKGExtractRequest(state, qual)
	if err != nil {
		return err
	}
	teeInfo, err := h.contract.GetTEEInfo(h.identity.primaryKey())
	if err != nil {
		return err
	}
	chainID, err := h.contract.ChainID(ctx)
	if err != nil {
		return err
	}
	log.Info("TEE host extracting DistKG", "index", state.Config.MyIndex, "qual", len(qual), "threshold", state.Config.Threshold)
	extractReq.ChainID = chainID.FillBytes(make([]byte, 32))
	extractReq.Contract = common.LeftPadBytes(h.contract.ContractAddress().Bytes(), 32)
	extractReq.TEEOwner = common.LeftPadBytes(teeInfo.Owner.Bytes(), 32)
	result, err := backend.DistKGExtract(ctx, extractReq)
	if err != nil {
		return err
	}
	state.Extracted = &result
	h.markFreshShareEpoch(state.Config.Epoch)
	if err := h.maybeSubmitPkCH(ctx, &result); err != nil {
		return err
	}
	if !state.VerifyKeyBroadcast {
		verifyKey := &DistKGVerifyKeyMessage{Index: wireIndex(state.Config.MyIndex), VerificationKey: append([]byte(nil), result.VerificationKey...)}
		if err := network.BroadcastDistKG(ctx, DistKGProtocolMessage{VerifyKey: verifyKey}); err != nil {
			return err
		}
		state.VerifyKeyBroadcast = true
	}
	return nil
}

func (h *Host) setDistKGSession(state *DistKGSessionState) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	h.distKGSession = state
}

func (h *Host) takePendingDistKG() []DistKGProtocolMessage {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	pending := append([]DistKGProtocolMessage(nil), h.pendingDistKG...)
	h.pendingDistKG = nil
	return pending
}

func (h *Host) hasDistKGSession() bool {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	return h.distKGSession != nil
}

func (h *Host) StartDKRS(ctx context.Context, cfg DKRSProtocolConfig, network ProtocolNetwork) (*DKRSSessionState, error) {
	logDKRSCommunicationSession(cfg)
	if network == nil {
		return nil, errors.New("protocol network is nil")
	}
	if cfg.DealerIndex == 0 && cfg.ReceiverIndex == 0 {
		return nil, errors.New("dkrs requires a dealer or receiver role")
	}
	if cfg.DealerIndex < 0 {
		return nil, errors.New("dealer index cannot be negative")
	}
	if cfg.ReceiverIndex < 0 || cfg.ReceiverIndex > cfg.NewNumParties {
		return nil, fmt.Errorf("receiver index must be in [0,%d]", cfg.NewNumParties)
	}
	if cfg.NewThreshold < 0 || cfg.NewThreshold >= cfg.NewNumParties {
		return nil, fmt.Errorf("invalid new threshold/numParties: %d/%d", cfg.NewThreshold, cfg.NewNumParties)
	}
	if len(cfg.OldQual) == 0 {
		return nil, errors.New("old qualified committee is empty")
	}
	if cfg.OldNumParties <= 0 {
		cfg.OldNumParties = len(cfg.OldQual)
	}
	if cfg.OldThreshold < 0 || cfg.OldThreshold >= cfg.OldNumParties {
		return nil, errors.New("invalid old threshold/numParties")
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return nil, errors.New("tee backend does not implement protocol operations")
	}
	state := &DKRSSessionState{
		Config:        cfg,
		Deals:         make(map[int]*DKRSDealMessage),
		Shares:        make(map[int]*DKRSShareMessage),
		InvalidShares: make(map[int]struct{}),
	}
	if cfg.ReceiverIndex > 0 {
		h.setDKRSSession(state)
	}
	if cfg.DealerIndex == 0 {
		if cfg.ReceiverIndex > 0 && len(cfg.OldActiveSet) > 0 && len(cfg.NewActiveSet) > 0 {
			h.startDKRSInputFetchLoop(ctx, cfg.SessionID, network)
		}
		return state, nil
	}
	var result TEEDealResult
	rotDealStart := time.Now()
	err := h.withDKRSPeerEPKs(ctx, cfg.NewEpoch, cfg.NewActiveSet, "new", func() error {
		var createErr error
		createStart := time.Now()
		result, createErr = backend.DKRSCreateDeal(ctx, DKRSCreateDealRequest{
			DealerIndex:          cfg.DealerIndex,
			NewThreshold:         cfg.NewThreshold,
			NewNumParties:        cfg.NewNumParties,
			TEEAttestationInputs: cfg.Attestation,
		})
		h.emitTiming("rotation_flow", "rot_deal_create",
			"duration_ns", time.Since(createStart).Nanoseconds(),
			"session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex)
		return createErr
	})
	if err != nil {
		return nil, err
	}
	h.emitTiming("rotation_flow", "rot_new_epks_and_deal",
		"duration_ns", time.Since(rotDealStart).Nanoseconds(),
		"session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex)
	deal := &DKRSDealMessage{
		DealerIndex:   wireIndex(cfg.DealerIndex),
		OldThreshold:  wireIndex(cfg.OldThreshold),
		OldNumParties: wireIndex(cfg.OldNumParties),
		NewThreshold:  wireIndex(cfg.NewThreshold),
		NewNumParties: wireIndex(cfg.NewNumParties),
		CommitmentsA:  append([]byte(nil), result.CommitmentsA...),
		CommitmentB0:  append([]byte(nil), result.CommitmentB0...),
	}
	h.cacheDKRSOutboundDeal(cfg.SessionID, cfg, deal)
	rotBcastStart := time.Now()
	if err := network.BroadcastDKRS(ctx, DKRSProtocolMessage{SessionID: cfg.SessionID, Deal: deal}); err != nil {
		return nil, err
	}
	h.emitTiming("rotation_flow", "rot_deal_broadcast",
		"duration_ns", time.Since(rotBcastStart).Nanoseconds(),
		"session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex)
	shareBuildStart := time.Now()
	shareFrames, err := splitTEEFramedShares(result.SharesSerialized, cfg.NewNumParties)
	if err != nil {
		return nil, err
	}
	for receiverIndex, payload := range shareFrames {
		if len(payload) == 0 {
			continue
		}
		msg := DKRSProtocolMessage{
			SessionID: cfg.SessionID,
			Share: &DKRSShareMessage{
				FromIndex: wireIndex(cfg.DealerIndex),
				ToIndex:   wireIndex(receiverIndex),
				Payload:   payload,
			},
		}
		h.cacheDKRSOutboundShare(cfg.SessionID, cfg, receiverIndex, msg.Share)
	}
	h.emitTiming("rotation_flow", "rot_share_build", "duration_ns", time.Since(shareBuildStart).Nanoseconds(),
		"session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex,
		"share_count", len(shareFrames), "status", "ok")

	shareSendStart := time.Now()
	shareSent, shareFailed := 0, 0
	for receiverIndex, payload := range shareFrames {
		if len(payload) == 0 || receiverIndex == cfg.ReceiverIndex {
			continue
		}
		msg := DKRSProtocolMessage{
			SessionID: cfg.SessionID,
			Share: &DKRSShareMessage{
				FromIndex: wireIndex(cfg.DealerIndex),
				ToIndex:   wireIndex(receiverIndex),
				Payload:   payload,
			},
		}
		toSigPubKey, ok := activeSetKeyAt(cfg.NewActiveSet, receiverIndex)
		if !ok {
			log.Warn("TEE host cannot send DKRS share without receiver key", "session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex, "receiverIndex", receiverIndex)
			shareFailed++
			continue
		}
		if err := network.SendDKRS(ctx, toSigPubKey, msg); err != nil {
			shareFailed++
			log.Warn("TEE host failed to send DKRS share", "session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex, "receiverIndex", receiverIndex, "err", err)
			continue
		}
		shareSent++
	}
	h.emitTiming("rotation_flow", "rot_shares_send", "duration_ns", time.Since(shareSendStart).Nanoseconds(),
		"session", shortSessionID(cfg.SessionID), "dealerIndex", cfg.DealerIndex,
		"share_count", len(shareFrames), "sent", shareSent, "failed", shareFailed, "status", "ok")
	h.protocolMu.Lock()
	state.MyDeal = deal
	state.Deals[cfg.DealerIndex] = deal
	if cfg.ReceiverIndex > 0 {
		state.Shares[cfg.DealerIndex] = &DKRSShareMessage{
			FromIndex: wireIndex(cfg.DealerIndex),
			ToIndex:   wireIndex(cfg.ReceiverIndex),
			Payload:   append([]byte(nil), shareFrames[cfg.ReceiverIndex]...),
		}
	}
	h.protocolMu.Unlock()
	if cfg.ReceiverIndex > 0 && len(cfg.OldActiveSet) > 0 && len(cfg.NewActiveSet) > 0 {
		h.startDKRSInputFetchLoop(ctx, cfg.SessionID, network)
	}
	return state, nil
}

func (h *Host) HandleDKRS(ctx context.Context, msg DKRSProtocolMessage, network ProtocolNetwork) (err error) {
	if network == nil {
		return errors.New("protocol network is nil")
	}
	if msg.InputRequest != nil {
		return h.handleDKRSInputRequest(ctx, msg, network)
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return errors.New("tee backend does not implement protocol operations")
	}
	h.protocolMu.Lock()
	extractedNow := false
	defer func() {
		h.protocolMu.Unlock()
		if extractedNow {
			if retryErr := h.retryPendingRedactions(ctx); retryErr != nil && err == nil {
				err = retryErr
			}
		}
	}()
	if h.dkrsSession == nil {
		if h.pendingDKRS == nil {
			h.pendingDKRS = make(map[[32]byte][]DKRSProtocolMessage)
		}
		h.pendingDKRS[msg.SessionID] = append(h.pendingDKRS[msg.SessionID], cloneDKRSProtocolMessage(msg))
		log.Debug("TEE host queued DKRS message until session starts", "session", shortSessionID(msg.SessionID))
		return nil
	}
	state := h.dkrsSession
	if !isDKRSEmptyMessage(msg) && msg.SessionID != state.Config.SessionID {
		if h.pendingDKRS == nil {
			h.pendingDKRS = make(map[[32]byte][]DKRSProtocolMessage)
		}
		h.pendingDKRS[msg.SessionID] = append(h.pendingDKRS[msg.SessionID], cloneDKRSProtocolMessage(msg))
		log.Debug("TEE host queued DKRS message for different session", "have", shortSessionID(state.Config.SessionID), "got", shortSessionID(msg.SessionID))
		return nil
	}
	if state.Config.ReceiverIndex == 0 {
		return nil
	}
	if state.Deals == nil {
		state.Deals = make(map[int]*DKRSDealMessage)
	}
	if state.Shares == nil {
		state.Shares = make(map[int]*DKRSShareMessage)
	}
	if state.InvalidShares == nil {
		state.InvalidShares = make(map[int]struct{})
	}
	if msg.InputResponse != nil {
		applyDKRSInputResponse(state, msg.InputResponse)
	}
	if msg.Deal != nil && validDKRSDealForSession(state, msg.Deal) {
		state.Deals[hostIndex(msg.Deal.DealerIndex)] = cloneDKRSDealMessage(msg.Deal)
	}
	if msg.Share != nil && validDKRSShareForSession(state, msg.Share) {
		state.Shares[hostIndex(msg.Share.FromIndex)] = cloneDKRSShareMessage(msg.Share)
	}
	if msg.Complaint != nil && validDKRSComplaintForSession(state, msg.Complaint) {
		state.Complaints = append(state.Complaints, *msg.Complaint)
	}
	if !dkrsHasAllInputs(state) {
		return nil
	}
	if !state.InputsReadyLogged {
		state.InputsReadyLogged = true
		h.emitTiming("rotation_flow", "rot_inputs_ready",
			"duration_ns", int64(0),
			"session", shortSessionID(state.Config.SessionID),
			"receiverIndex", state.Config.ReceiverIndex,
			"qual_count", len(state.Config.OldQual),
			"required", len(state.Config.OldQual),
			"status", "ok")
	}
	if !state.SharesVerified {
		verifyStart := time.Now()
		verifiedCount := 0
		invalidCount := 0
		complaints := make([]DKRSComplaintMessage, 0)
		if err := h.withDKRSPeerEPKs(ctx, state.Config.OldEpoch, state.Config.OldActiveSet, "old", func() error {
			for _, dealerIndex := range state.Config.OldQual {
				deal := state.Deals[dealerIndex]
				share := state.Shares[dealerIndex]
				if deal == nil || share == nil {
					continue
				}
				shareStart := time.Now()
				ok, err := backend.DKRSVerifyShare(ctx, DKRSVerifyShareRequest{
					ReceiverIndex:        state.Config.ReceiverIndex,
					DealerIndex:          dealerIndex,
					NewThreshold:         state.Config.NewThreshold,
					NewNumParties:        state.Config.NewNumParties,
					CommitmentsA:         deal.CommitmentsA,
					CommitmentB0:         deal.CommitmentB0,
					ShareBytes:           share.Payload,
					TEEAttestationInputs: state.Config.Attestation,
				})
				shareDuration := time.Since(shareStart)
				shareStatus := "ok"
				if err != nil || !ok {
					shareStatus = "invalid"
					invalidCount++
					state.InvalidShares[dealerIndex] = struct{}{}
					complaint := DKRSComplaintMessage{ComplainantIndex: wireIndex(state.Config.ReceiverIndex), DealerIndex: wireIndex(dealerIndex)}
					state.Complaints = append(state.Complaints, complaint)
					complaints = append(complaints, complaint)
				} else {
					verifiedCount++
				}
				h.emitTiming("rotation_flow", "rot_verify_share",
					"duration_ns", shareDuration.Nanoseconds(),
					"session", shortSessionID(state.Config.SessionID),
					"receiverIndex", state.Config.ReceiverIndex,
					"dealerIndex", dealerIndex,
					"status", shareStatus)
			}
			return nil
		}); err != nil {
			return err
		}
		verifyDuration := time.Since(verifyStart)
		verifyStatus := "ok"
		if invalidCount > 0 {
			verifyStatus = "has_complaints"
		}
		h.emitTiming("rotation_flow", "rot_verify_shares",
			"duration_ns", verifyDuration.Nanoseconds(),
			"session", shortSessionID(state.Config.SessionID),
			"receiverIndex", state.Config.ReceiverIndex,
			"verified_count", verifiedCount,
			"invalid_count", invalidCount,
			"status", verifyStatus)
		for _, complaint := range complaints {
			complaint := complaint
			if err := network.BroadcastDKRS(ctx, DKRSProtocolMessage{SessionID: state.Config.SessionID, Complaint: &complaint}); err != nil {
				return err
			}
		}
		state.ComplaintsSent = true
		state.SharesVerified = true
	}
	if !state.SharesVerified {
		return nil
	}
	if state.Extracted != nil {
		return nil
	}
	qual := dkrsQual(state)
	if len(qual) < state.Config.NewThreshold+1 {
		return nil
	}
	if !state.QualReadyLogged {
		state.QualReadyLogged = true
		h.emitTiming("rotation_flow", "rot_qual_ready",
			"duration_ns", int64(0),
			"session", shortSessionID(state.Config.SessionID),
			"receiverIndex", state.Config.ReceiverIndex,
			"qual_count", len(qual),
			"status", "ok")
	}
	log.Info("TEE host extracting DKRS", "receiverIndex", state.Config.ReceiverIndex, "threshold", state.Config.NewThreshold, "newParties", state.Config.NewNumParties, "qual", qual, "deals", len(state.Deals), "shares", len(state.Shares))
	extractReq, err := buildDKRSExtractRequest(state, qual)
	if err != nil {
		return err
	}
	var result TEEExtractResult
	rotExtractStart := time.Now()
	err = h.withDKRSPeerEPKs(ctx, state.Config.OldEpoch, state.Config.OldActiveSet, "old", func() error {
		var extractErr error
		extractCallStart := time.Now()
		result, extractErr = backend.DKRSExtract(ctx, extractReq)
		cryptoDuration := time.Since(extractCallStart)
		h.emitTiming("rotation_flow", "rot_extract_crypto",
			"duration_ns", cryptoDuration.Nanoseconds(),
			"session", shortSessionID(state.Config.SessionID),
			"receiverIndex", state.Config.ReceiverIndex,
			"status", "ok")
		h.emitTiming("rotation_flow", "rot_extract",
			"duration_ns", cryptoDuration.Nanoseconds(),
			"session", shortSessionID(state.Config.SessionID), "receiverIndex", state.Config.ReceiverIndex)
		return extractErr
	})
	if err != nil {
		log.Warn("TEE host DKRS extract failed", "receiverIndex", state.Config.ReceiverIndex, "qual", qual, "err", err)
		return err
	}
	h.emitTiming("rotation_flow", "rot_old_epks_and_extract",
		"duration_ns", time.Since(rotExtractStart).Nanoseconds(),
		"session", shortSessionID(state.Config.SessionID), "receiverIndex", state.Config.ReceiverIndex)
	applyStart := time.Now()
	state.Extracted = &result
	extractedNow = true
	h.markFreshShareEpoch(state.Config.NewEpoch)
	h.emitTiming("rotation_flow", "rot_apply",
		"duration_ns", time.Since(applyStart).Nanoseconds(),
		"session", shortSessionID(state.Config.SessionID),
		"receiverIndex", state.Config.ReceiverIndex,
		"status", "ok")
	h.emitTiming("rotation_flow", "rot_extracted",
		"duration_ns", int64(0),
		"session", shortSessionID(state.Config.SessionID),
		"receiverIndex", state.Config.ReceiverIndex,
		"status", "ok")
	// DKG-complete marker for this node: it has extracted the new share and
	// applied it. Emitted per active TEE with an explicit epoch so the rotation
	// timing collector can key the end of the span on (epoch, receiverIndex)
	// without scraping the human-readable "TEE host extracted DKRS" line. The
	// cluster-wide "all TEEs done" instant is the latest of these per epoch.
	h.emitTiming("rotation_flow", "rotation_dkg_complete",
		"duration_ns", int64(0),
		"epoch", epochString(state.Config.NewEpoch),
		"session", shortSessionID(state.Config.SessionID),
		"receiverIndex", state.Config.ReceiverIndex,
		"parties", state.Config.NewNumParties,
		"status", "ok")
	log.Info("TEE host extracted DKRS", "receiverIndex", state.Config.ReceiverIndex, "verificationKeyHash", shortPayloadHash(result.VerificationKey), "globalPKHash", shortPayloadHash(result.GlobalPK), "globalPKG1Hash", shortPayloadHash(result.GlobalPKG1))
	if !state.VerifyKeyBroadcast {
		verifyKey := &DKRSVerifyKeyMessage{Index: wireIndex(state.Config.ReceiverIndex), VerificationKey: append([]byte(nil), result.VerificationKey...)}
		if err := network.BroadcastDKRS(ctx, DKRSProtocolMessage{SessionID: state.Config.SessionID, VerifyKey: verifyKey}); err != nil {
			return err
		}
		state.VerifyKeyBroadcast = true
	}
	return nil
}

type dkrsInputFetchMessage struct {
	toSigPubKey [32]byte
	msg         DKRSProtocolMessage
}

func (h *Host) startDKRSInputFetchLoop(ctx context.Context, sessionID [32]byte, network ProtocolNetwork) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		delay := dkrsInputFetchInitialDelay
		for attempt := uint64(1); attempt <= dkrsInputFetchMaxAttempts; attempt++ {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			done, err := h.requestMissingDKRSInputs(ctx, sessionID, attempt, network)
			if err != nil {
				log.Warn("TEE host DKRS input fetch failed", "session", shortSessionID(sessionID), "attempt", attempt, "err", err)
			}
			if done {
				return
			}
			delay *= 2
			if delay > dkrsInputFetchMaxDelay {
				delay = dkrsInputFetchMaxDelay
			}
		}
	}()
}

func (h *Host) requestMissingDKRSInputs(ctx context.Context, sessionID [32]byte, attempt uint64, network ProtocolNetwork) (bool, error) {
	if network == nil {
		return false, errors.New("protocol network is nil")
	}
	h.protocolMu.Lock()
	done, requests := h.buildDKRSInputFetchMessagesLocked(sessionID, attempt)
	h.protocolMu.Unlock()
	for _, request := range requests {
		if err := network.SendDKRS(ctx, request.toSigPubKey, request.msg); err != nil {
			log.Warn("TEE host failed to request DKRS input", "session", shortSessionID(sessionID), "attempt", attempt, "err", err)
		}
	}
	if len(requests) > 0 {
		log.Info("TEE host requested missing DKRS inputs", "session", shortSessionID(sessionID), "attempt", attempt, "requests", len(requests))
	}
	return done, nil
}

func (h *Host) buildDKRSInputFetchMessagesLocked(sessionID [32]byte, attempt uint64) (bool, []dkrsInputFetchMessage) {
	if h.dkrsSession == nil || h.dkrsSession.Config.SessionID != sessionID {
		return true, nil
	}
	state := h.dkrsSession
	if state.Config.ReceiverIndex == 0 || state.Extracted != nil || dkrsHasAllInputs(state) {
		return true, nil
	}
	requests := make([]dkrsInputFetchMessage, 0)
	for _, dealerIndex := range state.Config.OldQual {
		needDeal := state.Deals == nil || state.Deals[dealerIndex] == nil
		needShare := state.Shares == nil || state.Shares[dealerIndex] == nil
		if !needDeal && !needShare {
			continue
		}
		toSigPubKey, ok := activeSetKeyAt(state.Config.OldActiveSet, dealerIndex)
		if !ok {
			log.Warn("TEE host cannot request DKRS input without dealer key", "session", shortSessionID(sessionID), "dealerIndex", dealerIndex)
			continue
		}
		nonceHash := crypto.Keccak256Hash(sessionID[:], []byte(fmt.Sprintf(":%d:%d:%d", state.Config.ReceiverIndex, dealerIndex, attempt)))
		requests = append(requests, dkrsInputFetchMessage{
			toSigPubKey: toSigPubKey,
			msg: DKRSProtocolMessage{
				SessionID: sessionID,
				InputRequest: &DKRSInputRequestMessage{
					RequesterIndex: wireIndex(state.Config.ReceiverIndex),
					DealerIndex:    wireIndex(dealerIndex),
					NeedDeal:       needDeal,
					NeedShare:      needShare,
					Nonce:          [32]byte(nonceHash),
					Attempt:        attempt,
				},
			},
		})
	}
	return false, requests
}

func (h *Host) handleDKRSInputRequest(ctx context.Context, msg DKRSProtocolMessage, network ProtocolNetwork) error {
	req := msg.InputRequest
	if req == nil {
		return nil
	}
	h.protocolMu.Lock()
	response, toSigPubKey, ok := h.buildDKRSInputResponseLocked(msg.SessionID, req)
	h.protocolMu.Unlock()
	if !ok {
		return nil
	}
	if err := network.SendDKRS(ctx, toSigPubKey, DKRSProtocolMessage{SessionID: msg.SessionID, InputResponse: response}); err != nil {
		log.Warn("TEE host failed to respond with DKRS input", "session", shortSessionID(msg.SessionID), "requesterIndex", hostIndex(req.RequesterIndex), "dealerIndex", hostIndex(req.DealerIndex), "err", err)
	}
	return nil
}

func (h *Host) buildDKRSInputResponseLocked(sessionID [32]byte, req *DKRSInputRequestMessage) (*DKRSInputResponseMessage, [32]byte, bool) {
	var emptyKey [32]byte
	if req == nil || (!req.NeedDeal && !req.NeedShare) {
		return nil, emptyKey, false
	}
	cache := h.dkrsOutbound[sessionID]
	if cache == nil {
		return nil, emptyKey, false
	}
	requesterIndex := hostIndex(req.RequesterIndex)
	dealerIndex := hostIndex(req.DealerIndex)
	if requesterIndex <= 0 || dealerIndex <= 0 || dealerIndex != cache.DealerIndex {
		return nil, emptyKey, false
	}
	toSigPubKey, ok := activeSetKeyAt(cache.NewActiveSet, requesterIndex)
	if !ok {
		log.Warn("TEE host cannot respond to DKRS input request without requester key", "session", shortSessionID(sessionID), "requesterIndex", requesterIndex, "dealerIndex", dealerIndex)
		return nil, emptyKey, false
	}
	response := &DKRSInputResponseMessage{
		RequesterIndex: req.RequesterIndex,
		DealerIndex:    req.DealerIndex,
		Nonce:          req.Nonce,
	}
	if req.NeedDeal && cache.Deal != nil {
		response.HasDeal = true
		response.Deal = *cloneDKRSDealMessage(cache.Deal)
	}
	if req.NeedShare && cache.Shares != nil {
		if share := cache.Shares[requesterIndex]; share != nil {
			response.HasShare = true
			response.Share = *cloneDKRSShareMessage(share)
		}
	}
	if !response.HasDeal && !response.HasShare {
		return nil, emptyKey, false
	}
	log.Debug("TEE host responding with DKRS input", "session", shortSessionID(sessionID), "requesterIndex", requesterIndex, "dealerIndex", dealerIndex, "deal", response.HasDeal, "share", response.HasShare, "attempt", req.Attempt)
	return response, toSigPubKey, true
}

func applyDKRSInputResponse(state *DKRSSessionState, resp *DKRSInputResponseMessage) {
	if state == nil || resp == nil {
		return
	}
	requesterIndex := hostIndex(resp.RequesterIndex)
	dealerIndex := hostIndex(resp.DealerIndex)
	if requesterIndex != state.Config.ReceiverIndex {
		return
	}
	if _, ok := dkrsOldQualSet(state)[dealerIndex]; !ok {
		return
	}
	if resp.HasDeal && state.Deals[dealerIndex] == nil && validDKRSDealForSession(state, &resp.Deal) {
		state.Deals[dealerIndex] = cloneDKRSDealMessage(&resp.Deal)
	}
	if resp.HasShare && state.Shares[dealerIndex] == nil && validDKRSShareForSession(state, &resp.Share) {
		state.Shares[dealerIndex] = cloneDKRSShareMessage(&resp.Share)
	}
}

func (h *Host) setDKRSSession(state *DKRSSessionState) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	h.dkrsSession = state
}

func (h *Host) cacheDKRSOutboundSessionLocked(sessionID [32]byte, cfg DKRSProtocolConfig) *dkrsOutboundCache {
	if h.dkrsOutbound == nil {
		h.dkrsOutbound = make(map[[32]byte]*dkrsOutboundCache)
	}
	cache := h.dkrsOutbound[sessionID]
	if cache == nil {
		cache = &dkrsOutboundCache{
			DealerIndex:  cfg.DealerIndex,
			OldActiveSet: append([][32]byte(nil), cfg.OldActiveSet...),
			NewActiveSet: append([][32]byte(nil), cfg.NewActiveSet...),
			Shares:       make(map[int]*DKRSShareMessage),
		}
		h.dkrsOutbound[sessionID] = cache
		h.dkrsOutboundOrder = append(h.dkrsOutboundOrder, sessionID)
		for len(h.dkrsOutboundOrder) > maxDKRSOutboundCacheSessions {
			evict := h.dkrsOutboundOrder[0]
			h.dkrsOutboundOrder = h.dkrsOutboundOrder[1:]
			delete(h.dkrsOutbound, evict)
		}
		return cache
	}
	if cfg.DealerIndex > 0 {
		cache.DealerIndex = cfg.DealerIndex
	}
	if len(cfg.OldActiveSet) > 0 {
		cache.OldActiveSet = append([][32]byte(nil), cfg.OldActiveSet...)
	}
	if len(cfg.NewActiveSet) > 0 {
		cache.NewActiveSet = append([][32]byte(nil), cfg.NewActiveSet...)
	}
	if cache.Shares == nil {
		cache.Shares = make(map[int]*DKRSShareMessage)
	}
	return cache
}

func (h *Host) cacheDKRSOutboundDeal(sessionID [32]byte, cfg DKRSProtocolConfig, deal *DKRSDealMessage) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	cache := h.cacheDKRSOutboundSessionLocked(sessionID, cfg)
	cache.Deal = cloneDKRSDealMessage(deal)
}

func (h *Host) cacheDKRSOutboundShare(sessionID [32]byte, cfg DKRSProtocolConfig, receiverIndex int, share *DKRSShareMessage) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	cache := h.cacheDKRSOutboundSessionLocked(sessionID, cfg)
	cache.Shares[receiverIndex] = cloneDKRSShareMessage(share)
}

func (h *Host) takePendingDKRS(sessionID [32]byte) []DKRSProtocolMessage {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	pending := append([]DKRSProtocolMessage(nil), h.pendingDKRS[sessionID]...)
	delete(h.pendingDKRS, sessionID)
	return pending
}

func (h *Host) handlePendingDKRS(ctx context.Context, network ProtocolNetwork) error {
	h.protocolMu.Lock()
	if h.dkrsSession == nil {
		h.protocolMu.Unlock()
		return nil
	}
	sessionID := h.dkrsSession.Config.SessionID
	h.protocolMu.Unlock()
	for _, pending := range h.takePendingDKRS(sessionID) {
		if err := h.HandleDKRS(ctx, pending, network); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) StartPartialAdapt(ctx context.Context, cfg PartialAdaptProtocolConfig, network ProtocolNetwork) (*PartialAdaptSessionState, error) {
	if network == nil {
		return nil, errors.New("protocol network is nil")
	}
	if cfg.ReqID == ([32]byte{}) {
		return nil, errors.New("partial adapt request id is empty")
	}
	if cfg.MyIndex < 1 || cfg.MyIndex > cfg.NumParties {
		return nil, fmt.Errorf("my index must be in [1,%d]", cfg.NumParties)
	}
	if cfg.Threshold < 0 || cfg.Threshold >= cfg.NumParties {
		return nil, fmt.Errorf("invalid threshold/numParties: %d/%d", cfg.Threshold, cfg.NumParties)
	}
	if len(cfg.SourceMessage) == 0 || len(cfg.TargetMessage) == 0 {
		return nil, errors.New("partial adapt source and target messages are required")
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return nil, errors.New("tee backend does not implement protocol operations")
	}
	req := cfg.PartialAdaptRequest
	req.SourceMessage = append([]byte(nil), cfg.SourceMessage...)
	req.TargetMessage = append([]byte(nil), cfg.TargetMessage...)
	log.Info("TEE host starting PartialAdapt", "reqID", common.Bytes2Hex(cfg.ReqID[:]), "myIndex", cfg.MyIndex, "threshold", cfg.Threshold, "numParties", cfg.NumParties, "sourceHash", shortPayloadHash(cfg.SourceMessage), "targetHash", shortPayloadHash(cfg.TargetMessage))
	result, err := backend.PartialAdapt(ctx, req)
	if err != nil {
		log.Warn("TEE host PartialAdapt failed", "reqID", common.Bytes2Hex(cfg.ReqID[:]), "myIndex", cfg.MyIndex, "err", err)
		return nil, err
	}
	contribution := partialAdaptContributionFromResult(cfg.ReqID, cfg.MyIndex, result)
	log.Info("TEE host produced PartialAdapt contribution", "reqID", common.Bytes2Hex(cfg.ReqID[:]), "index", cfg.MyIndex, "contributionHash", shortPayloadHash(result.Contribution), "sigmaHash", shortPayloadHash(result.Sigma))
	state := &PartialAdaptSessionState{
		Config:      clonePartialAdaptProtocolConfig(cfg),
		Shares:      map[int]*PartialAdaptContributionMessage{cfg.MyIndex: contribution},
		LocalResult: &result,
	}
	h.protocolMu.Lock()
	if h.partialAdapt == nil {
		h.partialAdapt = make(map[[32]byte]*PartialAdaptSessionState)
	}
	h.partialAdapt[cfg.ReqID] = state
	h.protocolMu.Unlock()
	if err := network.BroadcastPartialAdapt(ctx, PartialAdaptProtocolMessage{Contribution: contribution}); err != nil {
		return nil, err
	}
	for _, pending := range h.takePendingPartialAdapt(cfg.ReqID) {
		if err := h.HandlePartialAdapt(ctx, pending, network); err != nil {
			return nil, err
		}
	}
	if err := h.maybeFinalizePartialAdapt(ctx, cfg.ReqID); err != nil {
		return nil, err
	}
	return state, nil
}

func (h *Host) HandlePartialAdapt(ctx context.Context, msg PartialAdaptProtocolMessage, network ProtocolNetwork) error {
	if network == nil {
		return errors.New("protocol network is nil")
	}
	if msg.Contribution == nil || msg.Aggregate != nil {
		return nil
	}
	reqID := msg.Contribution.ReqID
	h.protocolMu.Lock()
	if h.partialAdapt == nil || h.partialAdapt[reqID] == nil {
		if h.pendingPartialAdapt == nil {
			h.pendingPartialAdapt = make(map[[32]byte][]PartialAdaptProtocolMessage)
		}
		h.pendingPartialAdapt[reqID] = append(h.pendingPartialAdapt[reqID], clonePartialAdaptProtocolMessage(msg))
		h.protocolMu.Unlock()
		log.Debug("TEE host queued partial adapt message until session starts", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	state := h.partialAdapt[reqID]
	if state.Shares == nil {
		state.Shares = make(map[int]*PartialAdaptContributionMessage)
	}
	if msg.Contribution != nil && msg.Contribution.Index >= 1 && hostIndex(msg.Contribution.Index) <= state.Config.NumParties {
		state.Shares[hostIndex(msg.Contribution.Index)] = clonePartialAdaptContributionMessage(msg.Contribution)
	}
	h.protocolMu.Unlock()
	if err := h.maybeFinalizePartialAdapt(ctx, reqID); err != nil {
		return err
	}
	return h.maybeExecutePartialAdapt(ctx, reqID)
}

func (h *Host) PartialAdaptAggregate(reqID [32]byte) (*PartialAdaptAggregateMessage, bool) {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	if h.partialAdapt == nil || h.partialAdapt[reqID] == nil || h.partialAdapt[reqID].Aggregate == nil {
		return nil, false
	}
	return clonePartialAdaptAggregateMessage(h.partialAdapt[reqID].Aggregate), true
}

func (h *Host) takePendingPartialAdapt(reqID [32]byte) []PartialAdaptProtocolMessage {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	pending := append([]PartialAdaptProtocolMessage(nil), h.pendingPartialAdapt[reqID]...)
	delete(h.pendingPartialAdapt, reqID)
	return pending
}

func (h *Host) maybeFinalizePartialAdapt(ctx context.Context, reqID [32]byte) error {
	h.protocolMu.Lock()
	if h.partialAdapt == nil || h.partialAdapt[reqID] == nil {
		h.protocolMu.Unlock()
		return errors.New("partial adapt session is not initialized")
	}
	state := h.partialAdapt[reqID]
	if state.Aggregated {
		h.protocolMu.Unlock()
		return nil
	}
	needed := state.Config.Threshold + 1
	if len(state.Shares) < needed {
		h.protocolMu.Unlock()
		return nil
	}
	contributions := selectPartialAdaptContributions(state.Shares, needed)
	log.Info("TEE host collected PartialAdapt contributions locally", "reqID", common.Bytes2Hex(reqID[:]), "fromIndex", state.Config.MyIndex, "needed", needed, "available", len(state.Shares), "indices", partialContributionIndices(contributions))
	// Keep the execution snapshot local; peers already propagate the individual contributions.
	state.Aggregate = &PartialAdaptAggregateMessage{
		ReqID:         reqID,
		FromIndex:     wireIndex(state.Config.MyIndex),
		Contributions: contributions,
	}
	state.Aggregated = true
	h.protocolMu.Unlock()
	return h.maybeExecutePartialAdapt(ctx, reqID)
}

func splitTEEFramedShares(data []byte, numParties int) (map[int][]byte, error) {
	if numParties <= 0 {
		return nil, errors.New("numParties must be positive")
	}
	out := make(map[int][]byte, numParties)
	offset := 0
	for i := 1; i <= numParties; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("missing share frame for party %d", i)
		}
		size := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if size < 0 || offset+size > len(data) {
			return nil, fmt.Errorf("invalid share frame for party %d", i)
		}
		out[i] = append([]byte(nil), data[offset:offset+size]...)
		offset += size
	}
	if offset != len(data) {
		return nil, fmt.Errorf("trailing share frame bytes: %d", len(data)-offset)
	}
	return out, nil
}

func distKGReadyForExtract(state *DistKGSessionState) bool {
	if state == nil {
		return false
	}
	ready := 0
	for i := 1; i <= state.Config.NumParties; i++ {
		if state.Deals[i] != nil && state.Shares[i] != nil {
			ready++
		}
	}
	return ready >= state.Config.NumParties
}

func distKGQual(state *DistKGSessionState) []int {
	complained := make(map[int]struct{}, len(state.Complaints))
	for _, complaint := range state.Complaints {
		complained[hostIndex(complaint.DealerIndex)] = struct{}{}
	}
	qual := make([]int, 0, state.Config.NumParties)
	for i := 1; i <= state.Config.NumParties; i++ {
		if _, bad := complained[i]; !bad && state.Deals[i] != nil && state.Shares[i] != nil {
			qual = append(qual, i)
		}
	}
	return qual
}

func buildDistKGExtractRequest(state *DistKGSessionState, qual []int) (DistKGExtractRequest, error) {
	req := DistKGExtractRequest{
		MyIndex:              state.Config.MyIndex,
		Threshold:            state.Config.Threshold,
		NumParties:           state.Config.NumParties,
		QualIndices:          make([]int32, 0, len(qual)),
		B0DealerMap:          make([]int32, 0, len(qual)),
		A0DealerMap:          make([]int32, 0, len(qual)),
		SharesFromMap:        make([]int32, 0, len(qual)),
		TEEAttestationInputs: state.Config.Attestation,
	}
	shareFrames := make(map[int][]byte, len(qual))
	for _, idx := range qual {
		deal := state.Deals[idx]
		share := state.Shares[idx]
		if deal == nil || share == nil {
			return DistKGExtractRequest{}, fmt.Errorf("missing qualified input from %d", idx)
		}
		if len(deal.CommitmentsA) < nitchG1PointSizeForTEEHost() {
			return DistKGExtractRequest{}, fmt.Errorf("deal %d has invalid A length", idx)
		}
		req.QualIndices = append(req.QualIndices, int32(idx))
		req.B0DealerMap = append(req.B0DealerMap, int32(idx))
		req.A0DealerMap = append(req.A0DealerMap, int32(idx))
		req.SharesFromMap = append(req.SharesFromMap, int32(idx))
		req.B0Data = append(req.B0Data, deal.CommitmentB0...)
		req.A0Data = append(req.A0Data, deal.CommitmentsA[:nitchG1PointSizeForTEEHost()]...)
		shareFrames[idx] = share.Payload
	}
	req.SharesData = frameTEEIndexedPayloads(qual, shareFrames)
	return req, nil
}

func dkrsHasAllInputs(state *DKRSSessionState) bool {
	if state == nil {
		return false
	}
	if state.Config.ReceiverIndex == 0 {
		return false
	}
	qual := dkrsQual(state)
	if len(qual) < state.Config.NewThreshold+1 {
		return false
	}
	for _, idx := range qual {
		if state.Deals[idx] == nil || state.Shares[idx] == nil {
			return false
		}
	}
	return true
}

func isDKRSEmptyMessage(msg DKRSProtocolMessage) bool {
	return msg.Deal == nil && msg.Share == nil && msg.Complaint == nil && msg.VerifyKey == nil && msg.InputRequest == nil && msg.InputResponse == nil
}

func dkrsOldQualSet(state *DKRSSessionState) map[int]struct{} {
	out := make(map[int]struct{}, len(state.Config.OldQual))
	for _, idx := range state.Config.OldQual {
		out[idx] = struct{}{}
	}
	return out
}

func validDKRSDealForSession(state *DKRSSessionState, deal *DKRSDealMessage) bool {
	if state == nil || deal == nil {
		return false
	}
	dealerIndex := hostIndex(deal.DealerIndex)
	if _, ok := dkrsOldQualSet(state)[dealerIndex]; !ok {
		return false
	}
	return deal.OldThreshold == wireIndex(state.Config.OldThreshold) &&
		deal.OldNumParties == wireIndex(state.Config.OldNumParties) &&
		deal.NewThreshold == wireIndex(state.Config.NewThreshold) &&
		deal.NewNumParties == wireIndex(state.Config.NewNumParties)
}

func validDKRSShareForSession(state *DKRSSessionState, share *DKRSShareMessage) bool {
	if state == nil || share == nil {
		return false
	}
	if hostIndex(share.ToIndex) != state.Config.ReceiverIndex {
		return false
	}
	_, ok := dkrsOldQualSet(state)[hostIndex(share.FromIndex)]
	return ok
}

func validDKRSComplaintForSession(state *DKRSSessionState, complaint *DKRSComplaintMessage) bool {
	if state == nil || complaint == nil {
		return false
	}
	complainant := hostIndex(complaint.ComplainantIndex)
	if complainant < 1 || complainant > state.Config.NewNumParties {
		return false
	}
	_, ok := dkrsOldQualSet(state)[hostIndex(complaint.DealerIndex)]
	return ok
}

// dkrsQual returns the deterministic fixed dealer set for DKRS extraction: the
// first ActiveThreshold+1 entries of Config.OldQual in listed order (skipping
// complaint-disqualified dealers and locally invalid shares). Config.OldQual is
// derived from the on-chain active set (event_handlers.go) and is identical on
// every node, so every receiver extracts from this same prefix. Later OldQual
// members' inputs never advance the extraction gate. NewThreshold is
// ActiveThreshold-1, hence the +2. Mirrors nitch's reshareRequiredQualLocked
// (required = Threshold+1 fixed prefix of the old committee).
func dkrsQual(state *DKRSSessionState) []int {
	disqualified := dkrsDisqualifiedDealers(state)
	required := state.Config.NewThreshold + 2
	qual := make([]int, 0, required)
	for _, idx := range state.Config.OldQual {
		if _, bad := disqualified[idx]; bad {
			continue
		}
		if _, bad := state.InvalidShares[idx]; !bad {
			qual = append(qual, idx)
		}
		if len(qual) >= required {
			break
		}
	}
	return qual
}

func dkrsDisqualifiedDealers(state *DKRSSessionState) map[int]struct{} {
	disqualified := make(map[int]struct{})
	if state == nil {
		return disqualified
	}
	complainantsByDealer := make(map[int]map[int]struct{})
	for _, complaint := range state.Complaints {
		dealer := hostIndex(complaint.DealerIndex)
		complainant := hostIndex(complaint.ComplainantIndex)
		if complainantsByDealer[dealer] == nil {
			complainantsByDealer[dealer] = make(map[int]struct{})
		}
		complainantsByDealer[dealer][complainant] = struct{}{}
	}
	for dealer, complainants := range complainantsByDealer {
		if len(complainants) > state.Config.NewThreshold {
			disqualified[dealer] = struct{}{}
		}
	}
	return disqualified
}

func buildDKRSExtractRequest(state *DKRSSessionState, qual []int) (DKRSExtractRequest, error) {
	req := DKRSExtractRequest{
		MyIndex:              state.Config.ReceiverIndex,
		NewThreshold:         state.Config.NewThreshold,
		NewNumParties:        state.Config.NewNumParties,
		OldNumParties:        state.Config.OldNumParties,
		QualOld:              make([]int32, 0, len(qual)),
		B0DealerMap:          make([]int32, 0, len(qual)),
		A0DealerMap:          make([]int32, 0, len(qual)),
		SharesFromMap:        make([]int32, 0, len(qual)),
		TEEAttestationInputs: state.Config.Attestation,
	}
	shareFrames := make(map[int][]byte, len(qual))
	for _, idx := range qual {
		deal := state.Deals[idx]
		share := state.Shares[idx]
		if deal == nil || share == nil {
			return DKRSExtractRequest{}, fmt.Errorf("missing qualified DKRS input from %d", idx)
		}
		if len(deal.CommitmentsA) < nitchG1PointSizeForTEEHost() {
			return DKRSExtractRequest{}, fmt.Errorf("DKRS deal %d has invalid A length", idx)
		}
		req.QualOld = append(req.QualOld, int32(idx))
		req.B0DealerMap = append(req.B0DealerMap, int32(idx))
		req.A0DealerMap = append(req.A0DealerMap, int32(idx))
		req.SharesFromMap = append(req.SharesFromMap, int32(idx))
		req.B0Data = append(req.B0Data, deal.CommitmentB0...)
		req.A0Data = append(req.A0Data, deal.CommitmentsA[:nitchG1PointSizeForTEEHost()]...)
		shareFrames[idx] = share.Payload
	}
	req.SharesData = frameTEEIndexedPayloads(qual, shareFrames)
	return req, nil
}

func frameTEEIndexedPayloads(indices []int, payloads map[int][]byte) []byte {
	var out []byte
	for _, idx := range indices {
		payload := payloads[idx]
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(payload)))
		out = append(out, size...)
		out = append(out, payload...)
	}
	return out
}

func cloneDistKGDealMessage(in *DistKGDealMessage) *DistKGDealMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.CommitmentsA = append([]byte(nil), in.CommitmentsA...)
	out.CommitmentB0 = append([]byte(nil), in.CommitmentB0...)
	out.G1Bytes = append([]byte(nil), in.G1Bytes...)
	out.G2Bytes = append([]byte(nil), in.G2Bytes...)
	return &out
}

func cloneDistKGShareMessage(in *DistKGShareMessage) *DistKGShareMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.Payload = append([]byte(nil), in.Payload...)
	return &out
}

func cloneDistKGProtocolMessage(in DistKGProtocolMessage) DistKGProtocolMessage {
	return DistKGProtocolMessage{
		RelayTTL:  in.RelayTTL,
		Deal:      cloneDistKGDealMessage(in.Deal),
		Share:     cloneDistKGShareMessage(in.Share),
		Complaint: cloneDistKGComplaintMessage(in.Complaint),
		VerifyKey: cloneDistKGVerifyKeyMessage(in.VerifyKey),
	}
}

func cloneDistKGComplaintMessage(in *DistKGComplaintMessage) *DistKGComplaintMessage {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneDistKGVerifyKeyMessage(in *DistKGVerifyKeyMessage) *DistKGVerifyKeyMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.VerificationKey = append([]byte(nil), in.VerificationKey...)
	return &out
}

func cloneDKRSDealMessage(in *DKRSDealMessage) *DKRSDealMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.CommitmentsA = append([]byte(nil), in.CommitmentsA...)
	out.CommitmentB0 = append([]byte(nil), in.CommitmentB0...)
	return &out
}

func cloneDKRSShareMessage(in *DKRSShareMessage) *DKRSShareMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.Payload = append([]byte(nil), in.Payload...)
	return &out
}

func cloneDKRSProtocolMessage(in DKRSProtocolMessage) DKRSProtocolMessage {
	return DKRSProtocolMessage{
		SessionID:     in.SessionID,
		RelayTTL:      in.RelayTTL,
		Deal:          cloneDKRSDealMessage(in.Deal),
		Share:         cloneDKRSShareMessage(in.Share),
		Complaint:     cloneDKRSComplaintMessage(in.Complaint),
		VerifyKey:     cloneDKRSVerifyKeyMessage(in.VerifyKey),
		InputRequest:  cloneDKRSInputRequestMessage(in.InputRequest),
		InputResponse: cloneDKRSInputResponseMessage(in.InputResponse),
	}
}

func cloneDKRSComplaintMessage(in *DKRSComplaintMessage) *DKRSComplaintMessage {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneDKRSVerifyKeyMessage(in *DKRSVerifyKeyMessage) *DKRSVerifyKeyMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.VerificationKey = append([]byte(nil), in.VerificationKey...)
	return &out
}

func cloneDKRSInputRequestMessage(in *DKRSInputRequestMessage) *DKRSInputRequestMessage {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneDKRSInputResponseMessage(in *DKRSInputResponseMessage) *DKRSInputResponseMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.Deal = *cloneDKRSDealMessage(&in.Deal)
	out.Share = *cloneDKRSShareMessage(&in.Share)
	return &out
}

func partialAdaptContributionFromResult(reqID [32]byte, index int, result PartialAdaptResult) *PartialAdaptContributionMessage {
	return &PartialAdaptContributionMessage{
		ReqID:          reqID,
		Index:          wireIndex(index),
		Contribution:   append([]byte(nil), result.Contribution...),
		ProofChallenge: append([]byte(nil), result.ProofChallenge...),
		ProofResponse:  append([]byte(nil), result.ProofResponse...),
		Y:              append([]byte(nil), result.Y...),
		Sigma:          append([]byte(nil), result.Sigma...),
	}
}

func clonePartialAdaptProtocolConfig(in PartialAdaptProtocolConfig) PartialAdaptProtocolConfig {
	out := in
	out.SourceMessage = append([]byte(nil), in.SourceMessage...)
	out.TargetMessage = append([]byte(nil), in.TargetMessage...)
	out.PartialAdaptRequest = clonePartialAdaptRequest(in.PartialAdaptRequest)
	return out
}

func clonePartialAdaptRequest(in PartialAdaptRequest) PartialAdaptRequest {
	out := in
	out.SourceMessage = append([]byte(nil), in.SourceMessage...)
	out.TargetMessage = append([]byte(nil), in.TargetMessage...)
	out.CertReqAuthMsg = append([]byte(nil), in.CertReqAuthMsg...)
	out.CertReqAuthSig = append([]byte(nil), in.CertReqAuthSig...)
	out.DReq = append([]byte(nil), in.DReq...)
	out.DPolicy = append([]byte(nil), in.DPolicy...)
	out.CommitteePubKeyVersion = append([]byte(nil), in.CommitteePubKeyVersion...)
	out.ExpiresAt = append([]byte(nil), in.ExpiresAt...)
	out.DReqInput = append([]byte(nil), in.DReqInput...)
	out.TEEAttestationInputs = cloneTEEAttestationInputs(in.TEEAttestationInputs)
	return out
}

func cloneTEEAttestationInputs(in TEEAttestationInputs) TEEAttestationInputs {
	out := in
	out.CertEAMsg = append([]byte(nil), in.CertEAMsg...)
	out.CertEASig = append([]byte(nil), in.CertEASig...)
	out.ActiveTEEList = append([]byte(nil), in.ActiveTEEList...)
	out.ActiveSetCertInput = append([]byte(nil), in.ActiveSetCertInput...)
	return out
}

func clonePartialAdaptContributionMessage(in *PartialAdaptContributionMessage) *PartialAdaptContributionMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.Contribution = append([]byte(nil), in.Contribution...)
	out.ProofChallenge = append([]byte(nil), in.ProofChallenge...)
	out.ProofResponse = append([]byte(nil), in.ProofResponse...)
	out.Y = append([]byte(nil), in.Y...)
	out.Sigma = append([]byte(nil), in.Sigma...)
	return &out
}

func clonePartialAdaptAggregateMessage(in *PartialAdaptAggregateMessage) *PartialAdaptAggregateMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.FramedContributions = append([]byte(nil), in.FramedContributions...)
	out.Contributions = make([]PartialAdaptContributionMessage, len(in.Contributions))
	for i := range in.Contributions {
		cloned := clonePartialAdaptContributionMessage(&in.Contributions[i])
		if cloned != nil {
			out.Contributions[i] = *cloned
		}
	}
	return &out
}

func clonePartialAdaptProtocolMessage(in PartialAdaptProtocolMessage) PartialAdaptProtocolMessage {
	return PartialAdaptProtocolMessage{
		RelayTTL:     in.RelayTTL,
		Contribution: clonePartialAdaptContributionMessage(in.Contribution),
		Aggregate:    clonePartialAdaptAggregateMessage(in.Aggregate),
	}
}

func selectPartialAdaptContributions(shares map[int]*PartialAdaptContributionMessage, needed int) []PartialAdaptContributionMessage {
	indices := make([]int, 0, len(shares))
	for idx := range shares {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	out := make([]PartialAdaptContributionMessage, 0, needed)
	for _, idx := range indices {
		if len(out) == needed {
			break
		}
		cloned := clonePartialAdaptContributionMessage(shares[idx])
		if cloned != nil {
			out = append(out, *cloned)
		}
	}
	return out
}

func framePartialAdaptContributions(contributions []PartialAdaptContributionMessage) []byte {
	var out []byte
	for _, contribution := range contributions {
		index := make([]byte, 4)
		binary.LittleEndian.PutUint32(index, uint32(contribution.Index))
		out = append(out, index...)
		out = appendFramedBytes(out, contribution.Contribution)
		out = appendFramedBytes(out, contribution.ProofChallenge)
		out = appendFramedBytes(out, contribution.ProofResponse)
		out = appendFramedBytes(out, contribution.Y)
		out = appendFramedBytes(out, contribution.Sigma)
	}
	return out
}

func shortPayloadHash(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	hash := crypto.Keccak256Hash(payload)
	return common.Bytes2Hex(hash[:6])
}

func shortSessionID(sessionID [32]byte) string {
	if sessionID == ([32]byte{}) {
		return ""
	}
	return common.Bytes2Hex(sessionID[:6])
}

func partialContributionIndices(contributions []PartialAdaptContributionMessage) []int {
	indices := make([]int, 0, len(contributions))
	for _, contribution := range contributions {
		indices = append(indices, hostIndex(contribution.Index))
	}
	return indices
}

func appendFramedBytes(out []byte, payload []byte) []byte {
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(payload)))
	out = append(out, size...)
	return append(out, payload...)
}

func nitchG1PointSizeForTEEHost() int {
	return 96
}
