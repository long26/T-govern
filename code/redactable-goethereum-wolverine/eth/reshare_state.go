package eth

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	ctypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const (
	resharePendingSessionLimit  = 16
	resharePendingMessageLimit  = 4096
	reshareOutboundSessionLimit = 5
	reshareInputRetryInitial    = 2 * time.Second
	reshareInputRetryMax        = 5 * time.Second
	reshareInputMaxAttempts     = 180
)

type resharePendingBuffer struct {
	messages map[string]ctypes.ReshareMessage
	order    []string
}

type reshareOutboundSession struct {
	deal   *chameleon.DKRSDeal
	shares map[int]*chameleon.DKRSShare
}

type reshareInputRequestState struct {
	nonce       [32]byte
	attempts    int
	lastRequest time.Time
}

func (km *KeyManager) ensureResharePendingBuffersLocked() {
	if km.resharePendingBuffers == nil {
		km.resharePendingBuffers = make(map[string]*resharePendingBuffer)
	}
	if km.reshareOutboundCache == nil {
		km.reshareOutboundCache = make(map[string]*reshareOutboundSession)
	}
}

func (km *KeyManager) pendingReshareBufferLocked(sessionID string) *resharePendingBuffer {
	km.ensureResharePendingBuffersLocked()
	buf := km.resharePendingBuffers[sessionID]
	if buf != nil {
		return buf
	}
	buf = &resharePendingBuffer{
		messages: make(map[string]ctypes.ReshareMessage),
	}
	km.resharePendingBuffers[sessionID] = buf
	km.resharePendingOrder = append(km.resharePendingOrder, sessionID)
	for len(km.resharePendingOrder) > resharePendingSessionLimit {
		oldest := km.resharePendingOrder[0]
		km.resharePendingOrder = km.resharePendingOrder[1:]
		delete(km.resharePendingBuffers, oldest)
	}
	return buf
}

func (km *KeyManager) bufferReshareMessageLocked(msg ctypes.ReshareMessage) bool {
	if msg.SessionID == "" {
		return false
	}
	if km.shouldDropNonLocalReshareShareLocked(msg) {
		return false
	}
	key := reshareMessageCacheKey(msg)
	if key == "" {
		return false
	}
	buf := km.pendingReshareBufferLocked(msg.SessionID)
	if _, ok := buf.messages[key]; ok {
		return false
	}
	if len(buf.order) >= resharePendingMessageLimit {
		oldest := buf.order[0]
		buf.order = buf.order[1:]
		delete(buf.messages, oldest)
	}
	buf.messages[key] = cloneReshareMessage(msg)
	buf.order = append(buf.order, key)
	return true
}

func (km *KeyManager) replayResharePendingLocked(sessionID string) {
	if km.resharePendingBuffers == nil {
		return
	}
	km.restoreResharePendingWireSessionLocked(sessionID)
	buf := km.resharePendingBuffers[sessionID]
	if buf == nil {
		return
	}
	for _, key := range append([]string(nil), buf.order...) {
		msg, ok := buf.messages[key]
		if !ok {
			continue
		}
		if msg.SessionID != sessionID || km.shouldDropNonLocalReshareShareLocked(msg) {
			continue
		}
		if _, err := km.applyReshareMessageLocked(msg); err != nil {
			log.Warn("Reshare pending replay failed", "session", sessionID, "key", key, "err", err)
		}
	}
}

// OnReshareMessage processes a current-session message or keeps it in a
// per-session pending buffer until StartReshare activates that session.
func (km *KeyManager) OnReshareMessage(msg ctypes.ReshareMessage) (bool, error) {
	if msg.SessionID == "" {
		return false, nil
	}
	km.mu.Lock()
	defer km.mu.Unlock()

	km.ensureResharePendingBuffersLocked()
	msg.SessionID = km.resolveReshareSessionIDLocked(msg.SessionID)
	if km.shouldDropNonLocalReshareShareLocked(msg) {
		return false, nil
	}
	if !km.resharePending || km.reshareSessionID != msg.SessionID {
		if msg.SessionID == km.reshareLastSession {
			if msg.VerifyKey != nil {
				return km.applyReshareVerifyKeyLocked(msg.VerifyKey), nil
			}
			// The session is finished; input requests are answered separately
			// from the outbound cache by the protocol handler.
			return false, nil
		}
		km.bufferReshareMessageLocked(msg)
		return false, nil
	}
	return km.applyReshareMessageLocked(msg)
}

func (km *KeyManager) shouldDropNonLocalReshareShareLocked(msg ctypes.ReshareMessage) bool {
	// A message for a future session may arrive before the boundary block.
	// The active committee index belongs to the previous session and cannot
	// identify this session's receiver; buffer first and filter on replay.
	if !km.resharePending || km.reshareSessionID != msg.SessionID {
		return false
	}
	if !km.reshareRole.IsReceiver && (msg.Share != nil || msg.InputResponse != nil) {
		return true
	}
	localIndex := km.localReshareReceiverIndexLocked()
	if localIndex == 0 {
		return false
	}
	if msg.Share != nil && !reshareShareTargetsIndex(msg.Share, localIndex) {
		return true
	}
	if msg.InputResponse != nil {
		if msg.InputResponse.ReceiverIndex != nil {
			receiver, err := chameleonIndexToInt(msg.InputResponse.ReceiverIndex)
			if err == nil && receiver != localIndex {
				return true
			}
		}
		if msg.InputResponse.Share != nil && !reshareShareTargetsIndex(msg.InputResponse.Share, localIndex) {
			return true
		}
	}
	return false
}

func (km *KeyManager) localReshareReceiverIndexLocked() int {
	if km.resharePending && km.reshareRole.IsReceiver {
		return km.reshareRole.ReceiverIndex
	}
	// Phase 3A：描述符模式下按 pending（其次 active）描述符位置解析本地 receiver 索引，
	// 使 election 选中、会话尚未 StartReshare 的新成员也能正确过滤非本地 Share。
	if km.descriptorModeLocked() {
		localID := km.localNodeIDLocked()
		if km.pendingCommittee != nil {
			if idx, ok := km.pendingCommittee.MemberIndex(localID); ok {
				return idx
			}
		}
		if idx, ok := km.activeCommittee.MemberIndex(localID); ok {
			return idx
		}
		return 0
	}
	if km.isCommitteeMember {
		return km.nodeIndex + 1
	}
	return 0
}

// ReshareMessageTargetsLocal reports whether this node is the terminal
// recipient of a point-to-point reshare message. Dealer indexes identify old
// committee nodes directly; receiver indexes are positions in NewCommittee.
// A message that arrives before the local reshare session is configured keeps
// relaying unless its dealer target can already be identified.
func (km *KeyManager) ReshareMessageTargetsLocal(msg ctypes.ReshareMessage) bool {
	if km == nil {
		return false
	}
	target := reshareTargetIndex(msg)
	if target <= 0 {
		return false
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	msg.SessionID = km.resolveReshareSessionIDLocked(msg.SessionID)

	if msg.InputRequest != nil {
		if cache := km.reshareOutboundCache[msg.SessionID]; cache != nil && cache.deal != nil &&
			cache.deal.DealerIndex != nil && msg.InputRequest.DealerIndex != nil &&
			cache.deal.DealerIndex.Cmp(msg.InputRequest.DealerIndex) == 0 {
			return true
		}
		if km.resharePending && km.reshareSessionID == msg.SessionID {
			return km.reshareRole.IsDealer && km.reshareRole.DealerIndex == target
		}
		// A request for a finished session uses the old dealer indices, not
		// this node's current active-committee index.
		if km.reshareLastSession == msg.SessionID {
			return false
		}
		return km.localDealerIndexLocked() == target
	}
	if msg.Share != nil || msg.InputResponse != nil {
		return km.resharePending && km.reshareSessionID == msg.SessionID &&
			km.reshareRole.IsReceiver && km.reshareRole.ReceiverIndex == target
	}
	return false
}

func reshareShareTargetsIndex(share *chameleon.DKRSShare, index int) bool {
	if share == nil || share.ToIndex == nil || index <= 0 {
		return true
	}
	toIdx, err := chameleonIndexToInt(share.ToIndex)
	return err != nil || toIdx == index
}

func (km *KeyManager) applyReshareMessageLocked(msg ctypes.ReshareMessage) (bool, error) {
	if err := km.validateReshareMessageLocked(msg); err != nil {
		return false, err
	}
	key := reshareMessageCacheKey(msg)
	if key != "" {
		if km.reshareAppliedMessages == nil {
			km.reshareAppliedMessages = make(map[string]struct{})
		}
		if _, ok := km.reshareAppliedMessages[key]; ok {
			return false, nil
		}
	}

	applied := false
	if msg.Deal != nil {
		if err := km.applyReshareDealLocked(msg.SessionID, msg.Deal); err != nil {
			return applied, err
		}
		applied = true
	}
	if msg.Share != nil {
		if ok, err := km.applyReshareShareLocked(msg.SessionID, msg.Share); err != nil {
			return applied, err
		} else if ok {
			applied = true
		}
	}
	if msg.Complaint != nil {
		if ok := km.applyReshareComplaintLocked(msg.Complaint); ok {
			applied = true
		}
	}
	if msg.Response != nil {
		if ok, err := km.applyReshareResponseLocked(msg.Response); err != nil {
			return applied, err
		} else if ok {
			applied = true
		}
	}
	if msg.VerifyKey != nil {
		if ok := km.applyReshareVerifyKeyLocked(msg.VerifyKey); ok {
			applied = true
		}
	}
	if msg.InputResponse != nil {
		if msg.InputResponse.Deal != nil {
			if err := km.applyReshareDealLocked(msg.SessionID, msg.InputResponse.Deal); err != nil {
				return applied, err
			}
			applied = true
		}
		if msg.InputResponse.Share != nil {
			if ok, err := km.applyReshareShareLocked(msg.SessionID, msg.InputResponse.Share); err != nil {
				return applied, err
			} else if ok {
				applied = true
			}
		}
	}
	if key != "" {
		km.reshareAppliedMessages[key] = struct{}{}
	}
	return applied, nil
}

func (km *KeyManager) validateReshareMessageLocked(msg ctypes.ReshareMessage) error {
	if !km.resharePending || km.reshareSessionID != msg.SessionID {
		return nil
	}
	if msg.Deal != nil {
		if err := km.validateDKRSDealLocked(msg.Deal); err != nil {
			return err
		}
	}
	if msg.Share != nil {
		if err := km.validateDKRSShareLocked(msg.Share); err != nil {
			return err
		}
	}
	if msg.Complaint != nil {
		dealer, err := chameleonIndexToInt(msg.Complaint.DealerIndex)
		if err != nil || !containsInt(km.reshareConfig.OldCommittee, dealer) {
			return fmt.Errorf("invalid reshare complaint dealer index")
		}
		complainant, err := chameleonIndexToInt(msg.Complaint.ComplainantIndex)
		if err != nil || complainant < 1 || complainant > len(km.reshareConfig.NewCommittee) {
			return fmt.Errorf("invalid reshare complaint complainant index")
		}
	}
	if msg.Response != nil {
		dealer, err := chameleonIndexToInt(msg.Response.DealerIndex)
		if err != nil || !containsInt(km.reshareConfig.OldCommittee, dealer) {
			return fmt.Errorf("invalid reshare response dealer index")
		}
		toIdx, err := chameleonIndexToInt(msg.Response.ToIndex)
		if err != nil || toIdx < 1 || toIdx > len(km.reshareConfig.NewCommittee) {
			return fmt.Errorf("invalid reshare response receiver index")
		}
	}
	if msg.VerifyKey != nil {
		idx, err := chameleonIndexToInt(msg.VerifyKey.Index)
		if err != nil || idx < 1 || idx > len(km.reshareConfig.NewCommittee) {
			return fmt.Errorf("invalid reshare verify key index")
		}
	}
	if msg.InputRequest != nil {
		dealer, err := chameleonIndexToInt(msg.InputRequest.DealerIndex)
		if err != nil || !containsInt(km.reshareConfig.OldCommittee, dealer) {
			return fmt.Errorf("invalid reshare input request dealer index")
		}
		receiver, err := chameleonIndexToInt(msg.InputRequest.ReceiverIndex)
		if err != nil || receiver < 1 || receiver > len(km.reshareConfig.NewCommittee) {
			return fmt.Errorf("invalid reshare input request receiver index")
		}
	}
	if msg.InputResponse != nil {
		resp := msg.InputResponse
		if resp.HasDeal != (resp.Deal != nil) || resp.HasShare != (resp.Share != nil) {
			return fmt.Errorf("inconsistent reshare input response presence flags")
		}
		dealer, err := chameleonIndexToInt(resp.DealerIndex)
		if err != nil || !containsInt(km.reshareConfig.OldCommittee, dealer) {
			return fmt.Errorf("invalid reshare input response dealer index")
		}
		receiver, err := chameleonIndexToInt(resp.ReceiverIndex)
		if err != nil || receiver < 1 || receiver > len(km.reshareConfig.NewCommittee) {
			return fmt.Errorf("invalid reshare input response receiver index")
		}
		if resp.Deal != nil {
			if err := km.validateDKRSDealLocked(resp.Deal); err != nil {
				return err
			}
		}
		if resp.Share != nil {
			if err := km.validateDKRSShareLocked(resp.Share); err != nil {
				return err
			}
		}
	}
	return nil
}

func (km *KeyManager) validateDKRSDealLocked(deal *chameleon.DKRSDeal) error {
	if deal == nil {
		return nil
	}
	dealer, err := chameleonIndexToInt(deal.DealerIndex)
	if err != nil || !containsInt(km.reshareConfig.OldCommittee, dealer) {
		return fmt.Errorf("invalid reshare deal dealer index")
	}
	if err := expectBigInt(deal.NewThreshold, km.reshareConfig.NewThreshold, "reshare deal new threshold"); err != nil {
		return err
	}
	if err := expectBigInt(deal.NewNumParties, len(km.reshareConfig.NewCommittee), "reshare deal new committee size"); err != nil {
		return err
	}
	if deal.OldNumParties != nil && deal.OldNumParties.Sign() > 0 {
		if err := expectBigInt(deal.OldNumParties, len(km.reshareConfig.OldCommittee), "reshare deal old committee size"); err != nil {
			return err
		}
	}
	if deal.OldThreshold != nil && km.localKey != nil {
		if err := expectBigInt(deal.OldThreshold, km.localKey.Threshold, "reshare deal old threshold"); err != nil {
			return err
		}
	}
	return nil
}

func (km *KeyManager) validateDKRSShareLocked(share *chameleon.DKRSShare) error {
	if share == nil {
		return nil
	}
	fromIdx, err := chameleonIndexToInt(share.FromIndex)
	if err != nil || !containsInt(km.reshareConfig.OldCommittee, fromIdx) {
		return fmt.Errorf("invalid reshare share sender index")
	}
	toIdx, err := chameleonIndexToInt(share.ToIndex)
	if err != nil || toIdx < 1 || toIdx > len(km.reshareConfig.NewCommittee) {
		return fmt.Errorf("invalid reshare share receiver index")
	}
	return nil
}

func expectBigInt(value *big.Int, want int, name string) error {
	if value == nil || !value.IsInt64() || value.Int64() != int64(want) {
		return fmt.Errorf("%s mismatch", name)
	}
	return nil
}

func (km *KeyManager) applyReshareDealLocked(sessionID string, deal *chameleon.DKRSDeal) error {
	recvStart := time.Now()
	if deal == nil {
		return nil
	}
	idx, err := chameleonIndexToInt(deal.DealerIndex)
	if err != nil {
		return err
	}
	if km.reshareDeals == nil {
		km.reshareDeals = make(map[int]*chameleon.DKRSDeal)
	}
	if km.reshareDeals[idx] != nil {
		return nil
	}
	km.reshareDeals[idx] = cloneDKRSDeal(deal)
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "reshare_deal_received",
		"duration_ns", time.Since(recvStart).Nanoseconds(), "join_key", sessionID,
		"node", km.nodeIndex, "dealer", idx, "status", "ok")
	return nil
}

func (km *KeyManager) applyReshareShareLocked(sessionID string, share *chameleon.DKRSShare) (bool, error) {
	recvStart := time.Now()
	if share == nil || share.Share == nil || !km.reshareRole.IsReceiver {
		return false, nil
	}
	toIdx, err := chameleonIndexToInt(share.ToIndex)
	if err != nil {
		return false, err
	}
	if toIdx != km.reshareRole.ReceiverIndex {
		return false, nil
	}
	fromIdx, err := chameleonIndexToInt(share.FromIndex)
	if err != nil {
		return false, err
	}
	if km.reshareShares == nil {
		km.reshareShares = make(map[int]*big.Int)
	}
	if km.reshareShares[fromIdx] != nil {
		return false, nil
	}
	km.reshareShares[fromIdx] = new(big.Int).Set(share.Share)
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "reshare_share_received",
		"duration_ns", time.Since(recvStart).Nanoseconds(), "join_key", sessionID,
		"node", km.nodeIndex, "from", fromIdx, "status", "ok")
	return true, nil
}

func (km *KeyManager) applyReshareComplaintLocked(c *chameleon.DKRSComplaint) bool {
	if c == nil {
		return false
	}
	km.reshareComplaints = append(km.reshareComplaints, *cloneDKRSComplaint(c))
	return true
}

func (km *KeyManager) applyReshareResponseLocked(resp *chameleon.DKRSResponse) (bool, error) {
	if resp == nil || resp.Share == nil || !km.reshareRole.IsReceiver {
		return false, nil
	}
	toIdx, err := chameleonIndexToInt(resp.ToIndex)
	if err != nil {
		return false, err
	}
	if toIdx != km.reshareRole.ReceiverIndex {
		return false, nil
	}
	fromIdx, err := chameleonIndexToInt(resp.DealerIndex)
	if err != nil {
		return false, err
	}
	if km.reshareShares == nil {
		km.reshareShares = make(map[int]*big.Int)
	}
	if previous := km.reshareShares[fromIdx]; previous == nil || previous.Cmp(resp.Share) != 0 {
		km.reshareInputRevision++
		km.reshareVerifyStarted = false
		km.reshareVerified = false
	}
	km.reshareShares[fromIdx] = new(big.Int).Set(resp.Share)
	return true, nil
}

func (km *KeyManager) applyReshareVerifyKeyLocked(vk *chameleon.DKRSVerifyKey) bool {
	if vk == nil || vk.Index == nil {
		return false
	}
	idx, err := chameleonIndexToInt(vk.Index)
	if err != nil {
		return false
	}
	if km.reshareVerifyKeys == nil {
		km.reshareVerifyKeys = make(map[int]*chameleon.DKRSVerifyKey)
	}
	km.reshareVerifyKeys[idx] = cloneDKRSVerifyKey(vk)
	if err := km.registerDKRSVerifyKeyLocked(vk); err != nil {
		log.Warn("Reshare: failed to register verify key", "index", idx, "err", err)
	}
	if km.reshareRole.IsDealer && !km.reshareRole.IsReceiver && km.reshareDealerCanFinishLocked() {
		km.finishDealerSessionLocked(km.reshareSessionID)
	}
	return true
}

func (km *KeyManager) cacheReshareOutboundLocked(sessionID string, deal *chameleon.DKRSDeal, shares map[int]*chameleon.DKRSShare) {
	if sessionID == "" || deal == nil {
		return
	}
	km.ensureResharePendingBuffersLocked()
	cachedShares := make(map[int]*chameleon.DKRSShare, len(shares))
	for idx, share := range shares {
		cachedShares[idx] = cloneDKRSShare(share)
	}
	_, exists := km.reshareOutboundCache[sessionID]
	km.reshareOutboundCache[sessionID] = &reshareOutboundSession{
		deal:   cloneDKRSDeal(deal),
		shares: cachedShares,
	}
	if !exists {
		km.reshareOutboundOrder = append(km.reshareOutboundOrder, sessionID)
	}
	for len(km.reshareOutboundOrder) > reshareOutboundSessionLimit {
		oldest := km.reshareOutboundOrder[0]
		km.reshareOutboundOrder = km.reshareOutboundOrder[1:]
		delete(km.reshareOutboundCache, oldest)
	}
}

func (km *KeyManager) ReshareMissingInputRequests(sessionID string) []ctypes.ReshareMessage {
	km.mu.Lock()
	defer km.mu.Unlock()
	sessionID = km.resolveReshareSessionIDLocked(sessionID)

	if !km.resharePending || km.reshareSessionID != sessionID || !km.reshareRole.IsReceiver {
		return nil
	}
	if km.reshareRequestedInputs == nil {
		km.reshareRequestedInputs = make(map[string]reshareInputRequestState)
	}
	now := time.Now()
	var out []ctypes.ReshareMessage
	for _, dealer := range km.reshareRequiredQualLocked() {
		needDeal := km.reshareDeals == nil || km.reshareDeals[dealer] == nil
		needShare := km.reshareShares == nil || km.reshareShares[dealer] == nil
		if !needDeal && !needShare {
			continue
		}
		reqKey := fmt.Sprintf("%d:%d:%t:%t", dealer, km.reshareRole.ReceiverIndex, needDeal, needShare)
		state := km.reshareRequestedInputs[reqKey]
		if state.attempts >= reshareInputMaxAttempts {
			continue
		}
		if state.attempts > 0 && now.Sub(state.lastRequest) < reshareInputRetryDelay(state.attempts) {
			continue
		}
		km.reshareRequestNonce++
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], km.reshareRequestNonce)
		nonce := crypto.Keccak256Hash([]byte("NITCH_DKRS_INPUT_V1"), crypto.Keccak256([]byte(sessionID)), crypto.Keccak256([]byte(reqKey)), counter[:])
		state.nonce = nonce
		state.attempts++
		state.lastRequest = now
		km.reshareRequestedInputs[reqKey] = state
		out = append(out, ctypes.ReshareMessage{
			SessionID: sessionID,
			InputRequest: &ctypes.ReshareInputRequest{
				DealerIndex:   big.NewInt(int64(dealer)),
				ReceiverIndex: big.NewInt(int64(km.reshareRole.ReceiverIndex)),
				NeedDeal:      needDeal,
				NeedShare:     needShare,
				Nonce:         nonce,
				Attempt:       uint64(state.attempts),
			},
		})
	}
	return out
}

func reshareInputRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	delay := reshareInputRetryInitial
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= reshareInputRetryMax {
			return reshareInputRetryMax
		}
	}
	return delay
}

func (km *KeyManager) BuildReshareInputResponse(sessionID string, req *ctypes.ReshareInputRequest) *ctypes.ReshareMessage {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.buildReshareInputResponseLocked(sessionID, req)
}

func (km *KeyManager) BuildPendingReshareInputResponses(sessionID string) []ctypes.ReshareMessage {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.resharePendingBuffers == nil {
		return nil
	}
	buf := km.resharePendingBuffers[sessionID]
	if buf == nil {
		return nil
	}
	var out []ctypes.ReshareMessage
	for _, key := range buf.order {
		msg, ok := buf.messages[key]
		if !ok || msg.InputRequest == nil {
			continue
		}
		if resp := km.buildReshareInputResponseLocked(sessionID, msg.InputRequest); resp != nil {
			out = append(out, *resp)
		}
	}
	return out
}

func (km *KeyManager) buildReshareInputResponseLocked(sessionID string, req *ctypes.ReshareInputRequest) *ctypes.ReshareMessage {
	sessionID = km.resolveReshareSessionIDLocked(sessionID)
	if req == nil || sessionID == "" {
		return nil
	}
	dealer, err := chameleonIndexToInt(req.DealerIndex)
	if err != nil {
		return nil
	}
	receiver, err := chameleonIndexToInt(req.ReceiverIndex)
	if err != nil {
		return nil
	}
	// The cached deal proves this node was the dealer for this specific session.
	// After extraction reshareRole is cleared, but late receivers still need
	// the same authenticated share through the bounded outbound cache.
	cache := km.reshareOutboundCache[sessionID]
	if cache == nil || cache.deal == nil || cache.deal.DealerIndex == nil ||
		cache.deal.DealerIndex.Cmp(req.DealerIndex) != 0 || dealer <= 0 {
		return nil
	}
	resp := &ctypes.ReshareInputResponse{
		DealerIndex:   big.NewInt(int64(dealer)),
		ReceiverIndex: big.NewInt(int64(receiver)),
		Nonce:         req.Nonce,
	}
	if req.NeedDeal {
		resp.Deal = cloneDKRSDeal(cache.deal)
	}
	if req.NeedShare && cache.shares != nil {
		resp.Share = cloneDKRSShare(cache.shares[receiver])
	}
	resp.HasDeal, resp.HasShare = resp.Deal != nil, resp.Share != nil
	if !resp.HasDeal && !resp.HasShare {
		return nil
	}
	return &ctypes.ReshareMessage{
		SessionID:     sessionID,
		InputResponse: resp,
	}
}

func reshareMessageCacheKey(msg ctypes.ReshareMessage) string {
	sessionID := msg.SessionID
	switch {
	case msg.Deal != nil:
		return fmt.Sprintf("%s|deal|%s", sessionID, bigKey(msg.Deal.DealerIndex))
	case msg.Share != nil:
		return fmt.Sprintf("%s|share|%s|%s", sessionID, bigKey(msg.Share.FromIndex), bigKey(msg.Share.ToIndex))
	case msg.Complaint != nil:
		return fmt.Sprintf("%s|complaint|%s|%s", sessionID, bigKey(msg.Complaint.DealerIndex), bigKey(msg.Complaint.ComplainantIndex))
	case msg.Response != nil:
		return fmt.Sprintf("%s|response|%s|%s", sessionID, bigKey(msg.Response.DealerIndex), bigKey(msg.Response.ToIndex))
	case msg.VerifyKey != nil:
		return fmt.Sprintf("%s|verify-key|%s", sessionID, bigKey(msg.VerifyKey.Index))
	case msg.InputRequest != nil:
		req := msg.InputRequest
		return fmt.Sprintf("%s|input-request|%s|%s|%t|%t|%x", sessionID, bigKey(req.DealerIndex), bigKey(req.ReceiverIndex), req.NeedDeal, req.NeedShare, req.Nonce)
	case msg.InputResponse != nil:
		resp := msg.InputResponse
		return fmt.Sprintf("%s|input-response|%s|%s|%x", sessionID, bigKey(resp.DealerIndex), bigKey(resp.ReceiverIndex), resp.Nonce)
	default:
		return ""
	}
}

func bigKey(v *big.Int) string {
	if v == nil {
		return "nil"
	}
	return v.String()
}

func cloneReshareMessage(msg ctypes.ReshareMessage) ctypes.ReshareMessage {
	return ctypes.ReshareMessage{
		SessionID:     msg.SessionID,
		Deal:          cloneDKRSDeal(msg.Deal),
		Share:         cloneDKRSShare(msg.Share),
		Complaint:     cloneDKRSComplaint(msg.Complaint),
		Response:      cloneDKRSResponse(msg.Response),
		VerifyKey:     cloneDKRSVerifyKey(msg.VerifyKey),
		InputRequest:  cloneReshareInputRequest(msg.InputRequest),
		InputResponse: cloneReshareInputResponse(msg.InputResponse),
		RelayTTL:      msg.RelayTTL,
		Relay:         msg.Relay,
	}
}

func cloneReshareInputRequest(req *ctypes.ReshareInputRequest) *ctypes.ReshareInputRequest {
	if req == nil {
		return nil
	}
	return &ctypes.ReshareInputRequest{
		DealerIndex:   copyBig(req.DealerIndex),
		ReceiverIndex: copyBig(req.ReceiverIndex),
		NeedDeal:      req.NeedDeal,
		NeedShare:     req.NeedShare,
		Nonce:         req.Nonce,
		Attempt:       req.Attempt,
	}
}

func cloneReshareInputResponse(resp *ctypes.ReshareInputResponse) *ctypes.ReshareInputResponse {
	if resp == nil {
		return nil
	}
	return &ctypes.ReshareInputResponse{
		DealerIndex:   copyBig(resp.DealerIndex),
		ReceiverIndex: copyBig(resp.ReceiverIndex),
		HasDeal:       resp.HasDeal,
		Deal:          cloneDKRSDeal(resp.Deal),
		HasShare:      resp.HasShare,
		Share:         cloneDKRSShare(resp.Share),
		Nonce:         resp.Nonce,
	}
}

func cloneDKRSDeal(deal *chameleon.DKRSDeal) *chameleon.DKRSDeal {
	if deal == nil {
		return nil
	}
	return &chameleon.DKRSDeal{
		DealerIndex:   copyBig(deal.DealerIndex),
		OldThreshold:  copyBig(deal.OldThreshold),
		OldNumParties: copyBig(deal.OldNumParties),
		NewThreshold:  copyBig(deal.NewThreshold),
		NewNumParties: copyBig(deal.NewNumParties),
		A:             copyBytes(deal.A),
		B0:            copyBytes(deal.B0),
	}
}

func cloneDKRSShare(share *chameleon.DKRSShare) *chameleon.DKRSShare {
	if share == nil {
		return nil
	}
	return &chameleon.DKRSShare{
		FromIndex: copyBig(share.FromIndex),
		ToIndex:   copyBig(share.ToIndex),
		Share:     copyBig(share.Share),
	}
}

func cloneDKRSComplaint(c *chameleon.DKRSComplaint) *chameleon.DKRSComplaint {
	if c == nil {
		return nil
	}
	return &chameleon.DKRSComplaint{
		ComplainantIndex: copyBig(c.ComplainantIndex),
		DealerIndex:      copyBig(c.DealerIndex),
	}
}

func cloneDKRSResponse(resp *chameleon.DKRSResponse) *chameleon.DKRSResponse {
	if resp == nil {
		return nil
	}
	return &chameleon.DKRSResponse{
		DealerIndex: copyBig(resp.DealerIndex),
		ToIndex:     copyBig(resp.ToIndex),
		Share:       copyBig(resp.Share),
	}
}

func cloneDKRSVerifyKey(vk *chameleon.DKRSVerifyKey) *chameleon.DKRSVerifyKey {
	if vk == nil {
		return nil
	}
	return &chameleon.DKRSVerifyKey{
		Index:           copyBig(vk.Index),
		VerificationKey: copyBytes(vk.VerificationKey),
	}
}

func copyBig(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

func copyBytes(v []byte) []byte {
	if v == nil {
		return nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out
}
