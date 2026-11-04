package eth

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	distKGRelayDefaultTTL  = uint64(1)
	distKGSeenTTL          = 15 * time.Minute
	distKGSeenLimit        = 32768
	distKGRetryTick        = 2 * time.Second
	distKGRetryTTL         = 10 * time.Minute
	distKGInputRetryMin    = 2 * time.Second
	distKGInputRetryMax    = 5 * time.Second
	distKGInputMaxAttempts = 180
)

type distKGInputRequestState struct {
	attempts    int
	lastRequest time.Time
}

var (
	distKGRelayMeter     = metrics.NewRegisteredCounter("nitch/distkg/relay/sent", nil)
	distKGDuplicateMeter = metrics.NewRegisteredCounter("nitch/distkg/relay/duplicate", nil)
	distKGTTLDropMeter   = metrics.NewRegisteredCounter("nitch/distkg/relay/ttl_exhausted", nil)
)

func configuredRelayTTL(name string, fallback uint64) uint64 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		log.Warn("Invalid NITCH relay TTL; using default", "env", name, "value", raw, "default", fallback)
		return fallback
	}
	return value
}

func distKGFingerprint(msg types.DistKGMessage) (common.Hash, error) {
	msg.Relay = types.RelayMetadata{}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(append([]byte("nitch-distkg:"), encoded...)), nil
}

func distKGMessageKind(msg types.DistKGMessage) string {
	switch {
	case msg.Deal != nil:
		return "deal"
	case msg.Share != nil:
		return "share"
	case msg.Complaint != nil:
		return "complaint"
	case msg.Response != nil:
		return "response"
	case msg.VerifyKey != nil:
		return "verify_key"
	case msg.InputRequest != nil:
		return "input_request"
	case msg.InputResponse != nil:
		return "input_response"
	default:
		return "unknown"
	}
}

func cloneDistKGDeal(deal *chameleon.DistKGDeal) *chameleon.DistKGDeal {
	if deal == nil {
		return nil
	}
	return &chameleon.DistKGDeal{
		DealerIndex: copyBig(deal.DealerIndex),
		Threshold:   copyBig(deal.Threshold),
		NumParties:  copyBig(deal.NumParties),
		A:           copyBytes(deal.A),
		B0:          copyBytes(deal.B0),
		G1Bytes:     copyBytes(deal.G1Bytes),
		G2Bytes:     copyBytes(deal.G2Bytes),
		P:           deal.P,
	}
}

func cloneDistKGShare(share *chameleon.DistKGShare) *chameleon.DistKGShare {
	if share == nil {
		return nil
	}
	return &chameleon.DistKGShare{
		FromIndex: copyBig(share.FromIndex),
		ToIndex:   copyBig(share.ToIndex),
		Share:     copyBig(share.Share),
	}
}

// recordDistKGDeal keeps a transport-level replay cache. The cache never marks
// a Deal accepted by the DKG state machine; callers must still invoke the
// ordinary ObserveDistKGDeal/OnDistKGDeal path.
func (pm *ProtocolManager) recordDistKGDeal(deal *chameleon.DistKGDeal) error {
	if pm == nil || deal == nil {
		return fmt.Errorf("nil DistKG deal")
	}
	dealer, err := chameleonIndexToInt(deal.DealerIndex)
	if err != nil || dealer < 1 {
		return fmt.Errorf("invalid DistKG dealer index")
	}
	threshold, err := chameleonIndexToInt(deal.Threshold)
	if err != nil || threshold < 0 {
		return fmt.Errorf("invalid DistKG threshold")
	}
	parties, err := chameleonIndexToInt(deal.NumParties)
	if err != nil || parties < 1 || threshold >= parties || dealer > parties {
		return fmt.Errorf("invalid DistKG committee parameters")
	}
	if len(deal.A) == 0 || len(deal.B0) == 0 || len(deal.G1Bytes) == 0 || len(deal.G2Bytes) == 0 || deal.P == "" {
		return fmt.Errorf("incomplete DistKG deal")
	}

	pm.distKGRecoveryMu.Lock()
	if pm.distKGRecoveryDeals == nil {
		pm.distKGRecoveryDeals = make(map[int]*chameleon.DistKGDeal)
	}
	for _, cached := range pm.distKGRecoveryDeals {
		if cached == nil {
			continue
		}
		if cached.Threshold.Cmp(deal.Threshold) != 0 || cached.NumParties.Cmp(deal.NumParties) != 0 ||
			cached.P != deal.P || !bytes.Equal(cached.G1Bytes, deal.G1Bytes) || !bytes.Equal(cached.G2Bytes, deal.G2Bytes) {
			pm.distKGRecoveryMu.Unlock()
			return fmt.Errorf("DistKG deal parameters conflict with cached session")
		}
		break
	}
	if cached := pm.distKGRecoveryDeals[dealer]; cached != nil {
		oldBytes, oldErr := rlp.EncodeToBytes(cached)
		newBytes, newErr := rlp.EncodeToBytes(deal)
		pm.distKGRecoveryMu.Unlock()
		if oldErr != nil || newErr != nil || !bytes.Equal(oldBytes, newBytes) {
			return fmt.Errorf("conflicting DistKG deal for dealer %d", dealer)
		}
		return nil
	}
	pm.distKGRecoveryDeals[dealer] = cloneDistKGDeal(deal)
	pm.distKGRecoveryMu.Unlock()
	pm.startDistKGRetryLoop()
	return nil
}

func (pm *ProtocolManager) recordDistKGOutboundShare(share *chameleon.DistKGShare) {
	if pm == nil || share == nil || share.Share == nil {
		return
	}
	km := GetKeyManager()
	if km == nil {
		return
	}
	from, err := chameleonIndexToInt(share.FromIndex)
	if err != nil || from != km.GetNodeIndex()+1 {
		return
	}
	to, err := chameleonIndexToInt(share.ToIndex)
	if err != nil || to < 1 || to > km.GetCommitteeSize() {
		return
	}
	pm.distKGRecoveryMu.Lock()
	if pm.distKGOutboundShares == nil {
		pm.distKGOutboundShares = make(map[int]*chameleon.DistKGShare)
	}
	pm.distKGOutboundShares[to] = cloneDistKGShare(share)
	pm.distKGRecoveryMu.Unlock()
}

func (pm *ProtocolManager) cacheDistKGOutbound(msg types.DistKGMessage) {
	if msg.Deal != nil {
		if err := pm.recordDistKGDeal(msg.Deal); err != nil {
			log.Warn("Refusing to cache outbound DistKG deal", "err", err)
		}
	}
	if msg.Share != nil {
		pm.recordDistKGOutboundShare(msg.Share)
	}
}

func distKGResponseInputs(resp *types.DistKGInputResponse) (*chameleon.DistKGDeal, *chameleon.DistKGShare, error) {
	if resp == nil {
		return nil, nil, nil
	}
	dealer, err := chameleonIndexToInt(resp.DealerIndex)
	if err != nil || dealer < 1 {
		return nil, nil, fmt.Errorf("invalid DistKG response dealer")
	}
	requester, err := chameleonIndexToInt(resp.RequesterIndex)
	if err != nil || requester < 1 {
		return nil, nil, fmt.Errorf("invalid DistKG response requester")
	}
	if resp.Deal == nil && resp.Share == nil {
		return nil, nil, fmt.Errorf("empty DistKG input response")
	}
	if resp.Deal != nil {
		idx, err := chameleonIndexToInt(resp.Deal.DealerIndex)
		if err != nil || idx != dealer {
			return nil, nil, fmt.Errorf("DistKG response Deal dealer mismatch")
		}
	}
	if resp.Share != nil {
		from, fromErr := chameleonIndexToInt(resp.Share.FromIndex)
		to, toErr := chameleonIndexToInt(resp.Share.ToIndex)
		if fromErr != nil || toErr != nil || from != dealer || to != requester || resp.Share.Share == nil {
			return nil, nil, fmt.Errorf("DistKG response Share binding mismatch")
		}
	}
	return resp.Deal, resp.Share, nil
}

func (pm *ProtocolManager) buildDistKGInputResponse(req *types.DistKGInputRequest) *types.DistKGMessage {
	if pm == nil || req == nil || (!req.NeedDeal && !req.NeedShare) {
		return nil
	}
	km := GetKeyManager()
	if km == nil {
		return nil
	}
	dealer, err := chameleonIndexToInt(req.DealerIndex)
	if err != nil || dealer != km.GetNodeIndex()+1 {
		return nil
	}
	requester, err := chameleonIndexToInt(req.RequesterIndex)
	if err != nil || requester < 1 {
		return nil
	}
	pm.distKGRecoveryMu.Lock()
	defer pm.distKGRecoveryMu.Unlock()
	resp := &types.DistKGInputResponse{
		DealerIndex:    big.NewInt(int64(dealer)),
		RequesterIndex: big.NewInt(int64(requester)),
		Nonce:          req.Nonce,
	}
	if req.NeedDeal {
		resp.Deal = cloneDistKGDeal(pm.distKGRecoveryDeals[dealer])
	}
	if req.NeedShare {
		resp.Share = cloneDistKGShare(pm.distKGOutboundShares[requester])
	}
	if resp.Deal == nil && resp.Share == nil {
		return nil
	}
	return &types.DistKGMessage{InputResponse: resp}
}

func intSet(values []int) map[int]bool {
	out := make(map[int]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func (pm *ProtocolManager) distKGMissingInputRequests() []types.DistKGMessage {
	if pm == nil {
		return nil
	}
	km := GetKeyManager()
	if km == nil {
		return nil
	}
	requester := km.GetNodeIndex() + 1
	committeeSize := km.GetCommitteeSize()
	needDeals := make(map[int]bool)
	needShares := make(map[int]bool)
	if km.IsCommitteeMember() && km.IsDistKGPending() {
		missingDeals, missingShares := km.DistKGMissingInputs()
		needDeals = intSet(missingDeals)
		needShares = intSet(missingShares)
	} else {
		if _, err := chameleon.GetSystemNITCHKey(); err == nil {
			return nil
		}
		pm.distKGRecoveryMu.Lock()
		parties := 0
		for _, deal := range pm.distKGRecoveryDeals {
			if deal != nil && deal.NumParties != nil && deal.NumParties.IsInt64() {
				parties = int(deal.NumParties.Int64())
				break
			}
		}
		for dealer := 1; dealer <= parties; dealer++ {
			if pm.distKGRecoveryDeals[dealer] == nil {
				needDeals[dealer] = true
			}
		}
		pm.distKGRecoveryMu.Unlock()
	}

	pm.distKGRecoveryMu.Lock()
	defer pm.distKGRecoveryMu.Unlock()
	if pm.distKGRequested == nil {
		pm.distKGRequested = make(map[string]distKGInputRequestState)
	}
	now := time.Now()
	var out []types.DistKGMessage
	for dealer := 1; dealer <= committeeSize; dealer++ {
		needDeal, needShare := needDeals[dealer], needShares[dealer]
		if !needDeal && !needShare {
			continue
		}
		key := fmt.Sprintf("%d:%d:%t:%t", dealer, requester, needDeal, needShare)
		state := pm.distKGRequested[key]
		if state.attempts >= distKGInputMaxAttempts {
			continue
		}
		if state.attempts > 0 && now.Sub(state.lastRequest) < distKGInputRetryDelay(state.attempts) {
			continue
		}
		pm.distKGRequestNonce++
		state.attempts++
		state.lastRequest = now
		pm.distKGRequested[key] = state
		out = append(out, types.DistKGMessage{InputRequest: &types.DistKGInputRequest{
			DealerIndex:    big.NewInt(int64(dealer)),
			RequesterIndex: big.NewInt(int64(requester)),
			NeedDeal:       needDeal,
			NeedShare:      needShare,
			Nonce:          pm.distKGRequestNonce,
			Attempt:        uint64(state.attempts),
		}})
	}
	return out
}

func distKGInputRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	delay := distKGInputRetryMin
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= distKGInputRetryMax {
			return distKGInputRetryMax
		}
	}
	return delay
}

func (pm *ProtocolManager) distKGRecoveryFinished() bool {
	km := GetKeyManager()
	if km == nil {
		return false
	}
	if km.IsCommitteeMember() && km.IsDistKGPending() {
		return false
	}
	_, err := chameleon.GetSystemNITCHKey()
	return err == nil
}

func (pm *ProtocolManager) advanceDistKGRecovery() {
	for _, req := range pm.distKGMissingInputRequests() {
		dealer := bigIntIndex(req.InputRequest.DealerIndex)
		log.Info("DistKG requesting missing input", "dealer", dealer,
			"requester", req.InputRequest.RequesterIndex, "need_deal", req.InputRequest.NeedDeal,
			"need_share", req.InputRequest.NeedShare, "attempt", req.InputRequest.Attempt)
		_, _ = pm.SendDistKGToIndex(dealer, req)
	}
}

func (pm *ProtocolManager) startDistKGRetryLoop() {
	if pm == nil {
		return
	}
	pm.distKGRecoveryMu.Lock()
	if pm.distKGRecoveryLive {
		pm.distKGRecoveryMu.Unlock()
		return
	}
	pm.distKGRecoveryLive = true
	pm.distKGRecoveryMu.Unlock()

	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()
		defer func() {
			pm.distKGRecoveryMu.Lock()
			pm.distKGRecoveryLive = false
			pm.distKGRecoveryMu.Unlock()
		}()
		ticker := time.NewTicker(distKGRetryTick)
		defer ticker.Stop()
		deadline := time.NewTimer(distKGRetryTTL)
		defer deadline.Stop()
		for {
			select {
			case <-ticker.C:
				if pm.distKGRecoveryFinished() {
					return
				}
				pm.advanceDistKGRecovery()
			case <-deadline.C:
				log.Warn("DistKG missing-input retry loop timed out")
				return
			case <-pm.quitSync:
				return
			}
		}
	}()
}

func (pm *ProtocolManager) relayOrigin() string {
	if origin := pm.localPeerID(); origin != "" {
		return origin
	}
	return "rpc:" + pm.port
}

func (pm *ProtocolManager) prepareDistKGOutbound(msg types.DistKGMessage) types.DistKGMessage {
	fingerprint, err := distKGFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint outbound DistKG message", "err", err)
		return msg
	}
	msg.Relay = types.RelayMetadata{
		MessageID: fingerprint.Hex(),
		Origin:    pm.relayOrigin(),
		TTL:       pm.distKGRelayTTL,
	}
	return msg
}

func (pm *ProtocolManager) noteDistKGSeen(msg types.DistKGMessage) (string, bool) {
	fingerprint, err := distKGFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint DistKG message", "err", err)
		return msg.Relay.MessageID, true
	}
	messageID := fingerprint.Hex()
	if msg.Relay.MessageID != "" && msg.Relay.MessageID != messageID {
		log.Warn("DistKG relay message_id mismatch", "claimed", msg.Relay.MessageID, "computed", messageID)
	}
	now := time.Now()
	pm.distKGSeenMu.Lock()
	defer pm.distKGSeenMu.Unlock()
	if pm.distKGSeen == nil {
		pm.distKGSeen = make(map[common.Hash]time.Time)
	}
	pm.expireDistKGSeenLocked(now)
	if _, exists := pm.distKGSeen[fingerprint]; exists {
		return messageID, false
	}
	pm.distKGSeen[fingerprint] = now
	pm.distKGSeenOrder = append(pm.distKGSeenOrder, fingerprint)
	for len(pm.distKGSeen) > distKGSeenLimit && len(pm.distKGSeenOrder) > 0 {
		oldest := pm.distKGSeenOrder[0]
		pm.distKGSeenOrder = pm.distKGSeenOrder[1:]
		delete(pm.distKGSeen, oldest)
	}
	return messageID, true
}

func (pm *ProtocolManager) expireDistKGSeenLocked(now time.Time) {
	for len(pm.distKGSeenOrder) > 0 {
		oldest := pm.distKGSeenOrder[0]
		seenAt, exists := pm.distKGSeen[oldest]
		if !exists {
			pm.distKGSeenOrder = pm.distKGSeenOrder[1:]
			continue
		}
		if now.Sub(seenAt) <= distKGSeenTTL {
			return
		}
		pm.distKGSeenOrder = pm.distKGSeenOrder[1:]
		delete(pm.distKGSeen, oldest)
	}
}

func advanceRelay(meta types.RelayMetadata) (types.RelayMetadata, bool) {
	if meta.TTL == 0 {
		return meta, false
	}
	meta.TTL--
	meta.Hop++
	return meta, true
}

func (pm *ProtocolManager) relayDistKGMessage(msg types.DistKGMessage, receivedFrom *peer, messageID string) {
	relay, ok := advanceRelay(msg.Relay)
	from := ""
	if receivedFrom != nil {
		from = receivedFrom.id
	}
	if !ok {
		distKGTTLDropMeter.Inc(1)
		log.Debug("DistKG relay TTL exhausted", "message_id", messageID, "type", distKGMessageKind(msg),
			"origin", msg.Relay.Origin, "received_from", from, "hop", msg.Relay.Hop, "ttl", msg.Relay.TTL)
		return
	}
	before := msg.Relay.TTL
	msg.Relay = relay
	success, failed := pm.broadcastDistKGExcept(msg, from)
	distKGRelayMeter.Inc(int64(success))
	log.Debug("DistKG message relayed", "message_id", messageID, "type", distKGMessageKind(msg),
		"origin", relay.Origin, "received_from", from, "hop", relay.Hop,
		"ttl_before", before, "ttl_after", relay.TTL, "success", success, "failed", failed)
}

func (pm *ProtocolManager) broadcastDistKGExcept(msg types.DistKGMessage, excludeID string) (successCount, failCount int) {
	if pm == nil || pm.peers == nil {
		return 0, 0
	}
	for _, target := range pm.peers.Peers() {
		if target.id == excludeID {
			continue
		}
		if err := target.SendDistKG(msg); err != nil {
			log.Warn("Failed to send DistKG relay", "peer", target.id,
				"message_id", msg.Relay.MessageID, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	return successCount, failCount
}
