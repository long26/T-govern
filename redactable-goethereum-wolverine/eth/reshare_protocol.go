package eth

import (
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
	reshareSeenTTL   = 15 * time.Minute
	reshareSeenLimit = 32768
	reshareRetryTick = 2 * time.Second
	reshareRetryTTL  = 10 * time.Minute

	// 流行病中继参数（对齐 tee-connect defaultDKRSRelayTTL / defaultDKRSRelayMaxPeers）。
	reshareRelayDefaultTTL = uint64(1) // 出站默认剩余跳数；0 表示不再中继
	reshareRelayMaxPeers   = 64        // 单跳中继扇出上限（<=0 视为无限）
)

var (
	reshareRelayMeter     = metrics.NewRegisteredCounter("nitch/reshare/relay/sent", nil)
	reshareDuplicateMeter = metrics.NewRegisteredCounter("nitch/reshare/relay/duplicate", nil)
	reshareTTLDropMeter   = metrics.NewRegisteredCounter("nitch/reshare/relay/ttl_exhausted", nil)
)

func (pm *ProtocolManager) advanceReshare(sessionID string) {
	if sessionID == "" {
		return
	}
	km := GetKeyManager()
	if km == nil {
		return
	}
	sessionID = km.ResolveReshareSessionID(sessionID)

	for _, resp := range km.BuildPendingReshareInputResponses(sessionID) {
		_, _ = pm.SendReshareToIndex(reshareTargetIndex(resp), resp)
	}
	for _, req := range km.ReshareMissingInputRequests(sessionID) {
		_, _ = pm.SendReshareToIndex(reshareTargetIndex(req), req)
	}
	// 显式角色门控：dealer-only 节点只负责造 Deal/答 InputRequest，不验证/投诉/提取。
	// （此前由 ReshareHasEnoughData 隐式返回 false 实现等价 no-op，此处显式化以提升可读性。）
	if !km.ReshareIsReceiver() {
		if km.ReshareDealerCanFinish() {
			if err := km.FinishDealerSession(sessionID); err == nil {
				pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
			}
		}
		return
	}
	if !km.ReshareHasEnoughData() {
		return
	}
	if logged, qualCount, required := km.MarkReshareInputsReady(sessionID); logged {
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_inputs_ready",
			"duration_ns", 0, "join_key", sessionID, "node", pm.port,
			"qual_count", qualCount, "required", required, "status", "ok")
	}

	verifyStart := time.Now()
	verified, invalid, parallelism, started := km.ReshareVerifySharesOnly()
	if started {
		status := "ok_no_complaint"
		if invalid > 0 {
			status = "invalid_no_complaint"
		}
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_verify_shares",
			"duration_ns", time.Since(verifyStart).Nanoseconds(), "join_key", sessionID,
			"node", pm.port, "verified_count", verified, "invalid_count", invalid,
			"parallelism", parallelism, "status", status)
		// Keep the legacy event for old collectors, but make clear that this is
		// verification-only and never a complaint broadcast.
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_verify_complaints",
			"duration_ns", time.Since(verifyStart).Nanoseconds(), "join_key", sessionID,
			"node", pm.port, "status", status)
	}
	if !km.ReshareCanExtract() {
		return
	}
	if logged, qualCount := km.MarkReshareQualReady(sessionID); logged {
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_qual_ready",
			"duration_ns", 0, "join_key", sessionID, "node", pm.port,
			"qual_count", qualCount, "status", "ok")
	}
	rotExtractStart := time.Now()
	if err := km.ReshareExtractAndApply(); err != nil {
		log.Warn("Reshare extract failed", "session", sessionID, "err", err)
		return
	}
	pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_extract",
		"duration_ns", time.Since(rotExtractStart).Nanoseconds(), "join_key", sessionID,
		"node", pm.port, "status", "ok")
	if localKey := km.GetLocalKey(); localKey != nil {
		verifyKey := chameleon.NewDKRSVerifyKey(localKey)
		_, _ = pm.BroadcastReshare(types.ReshareMessage{SessionID: sessionID, VerifyKey: verifyKey})
	}
}

func (pm *ProtocolManager) startReshareRetryLoop(sessionID string) {
	if pm == nil || sessionID == "" {
		return
	}
	pm.reshareRetryMu.Lock()
	if pm.reshareRetryLive == nil {
		pm.reshareRetryLive = make(map[string]struct{})
	}
	if _, ok := pm.reshareRetryLive[sessionID]; ok {
		pm.reshareRetryMu.Unlock()
		return
	}
	pm.reshareRetryLive[sessionID] = struct{}{}
	pm.reshareRetryMu.Unlock()

	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()
		defer func() {
			pm.reshareRetryMu.Lock()
			delete(pm.reshareRetryLive, sessionID)
			pm.reshareRetryMu.Unlock()
		}()

		ticker := time.NewTicker(reshareRetryTick)
		defer ticker.Stop()
		deadline := time.NewTimer(reshareRetryTTL)
		defer deadline.Stop()

		for {
			select {
			case <-ticker.C:
				pm.advanceReshare(sessionID)
				if km := GetKeyManager(); km != nil && km.ReshareSessionFinished(sessionID) {
					return
				}
			case <-deadline.C:
				log.Warn("Reshare retry loop timed out", "session", sessionID)
				return
			case <-pm.quitSync:
				return
			}
		}
	}()
}

func (pm *ProtocolManager) noteReshareSeen(msg types.ReshareMessage) (string, bool) {
	if pm == nil {
		return msg.Relay.MessageID, true
	}
	fp, err := reshareFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint Reshare message", "session", msg.SessionID, "err", err)
		return msg.Relay.MessageID, true
	}
	messageID := fp.Hex()
	if msg.Relay.MessageID != "" && msg.Relay.MessageID != messageID {
		log.Warn("Reshare relay message_id mismatch", "claimed", msg.Relay.MessageID, "computed", messageID)
	}
	now := time.Now()
	pm.reshareSeenMu.Lock()
	defer pm.reshareSeenMu.Unlock()

	if pm.reshareSeen == nil {
		pm.reshareSeen = make(map[common.Hash]time.Time)
	}
	pm.expireReshareSeenLocked(now)
	if _, ok := pm.reshareSeen[fp]; ok {
		return messageID, false
	}
	pm.reshareSeen[fp] = now
	pm.reshareSeenOrder = append(pm.reshareSeenOrder, fp)
	for len(pm.reshareSeen) > reshareSeenLimit && len(pm.reshareSeenOrder) > 0 {
		oldest := pm.reshareSeenOrder[0]
		pm.reshareSeenOrder = pm.reshareSeenOrder[1:]
		delete(pm.reshareSeen, oldest)
	}
	return messageID, true
}

func (pm *ProtocolManager) expireReshareSeenLocked(now time.Time) {
	for len(pm.reshareSeenOrder) > 0 {
		oldest := pm.reshareSeenOrder[0]
		seenAt, ok := pm.reshareSeen[oldest]
		if !ok {
			pm.reshareSeenOrder = pm.reshareSeenOrder[1:]
			continue
		}
		if now.Sub(seenAt) <= reshareSeenTTL {
			return
		}
		pm.reshareSeenOrder = pm.reshareSeenOrder[1:]
		delete(pm.reshareSeen, oldest)
	}
}

func reshareFingerprint(msg types.ReshareMessage) (common.Hash, error) {
	// RelayTTL 每跳递减，置零后再哈希，保证同一逻辑消息各跳指纹一致、去重生效。
	msg.RelayTTL = 0
	msg.Relay = types.RelayMetadata{}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(encoded), nil
}

// prepareReshareOutbound 在消息出站前统一盖 TTL，使各构造点无需逐个设值。
// KeyManager 进程内适配器（OnReshareDeal 等直接 OnReshareMessage 的路径）不上线、保持 0。
func (pm *ProtocolManager) prepareReshareOutbound(msg types.ReshareMessage) types.ReshareMessage {
	fingerprint, err := reshareFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint outbound Reshare message", "session", msg.SessionID, "err", err)
		return msg
	}
	msg.Relay = types.RelayMetadata{
		MessageID: fingerprint.Hex(),
		Origin:    pm.relayOrigin(),
		TTL:       pm.reshareRelayTTL,
	}
	msg.RelayTTL = msg.Relay.TTL
	return msg
}

func reshareMessageKind(msg types.ReshareMessage) string {
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

func (pm *ProtocolManager) relayReshareMessage(msg types.ReshareMessage, receivedFrom *peer, messageID string) {
	from := ""
	if receivedFrom != nil {
		from = receivedFrom.id
	}
	next, ok := advanceRelay(msg.Relay)
	if !ok {
		reshareTTLDropMeter.Inc(1)
		log.Debug("Reshare relay TTL exhausted", "message_id", messageID, "type", reshareMessageKind(msg),
			"session", msg.SessionID, "origin", msg.Relay.Origin, "received_from", from,
			"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL)
		return
	}
	before := msg.Relay.TTL
	msg.Relay = next
	msg.RelayTTL = next.TTL
	target := reshareTargetIndex(msg)
	if target > 0 {
		route := pm.targetPeerForReshare(target, msg)
		if route == nil && msg.InputRequest == nil && msg.Complaint == nil {
			route = pm.directCommitteePeer(target)
		}
		if route != nil && route.id != from {
			if err := route.SendReshare(msg); err == nil {
				reshareRelayMeter.Inc(1)
				log.Debug("Reshare message relayed toward target", "message_id", messageID,
					"type", reshareMessageKind(msg), "target", target, "peer", route.id,
					"session", msg.SessionID, "origin", next.Origin, "received_from", from,
					"hop", next.Hop, "ttl_before", before, "ttl_after", next.TTL)
				return
			} else {
				log.Warn("Failed to relay Reshare message toward target", "target", target,
					"peer", route.id, "err", err)
			}
		}
	}
	success, failed := pm.broadcastReshareExcept(msg, from, reshareRelayMaxPeers)
	reshareRelayMeter.Inc(int64(success))
	log.Debug("Reshare message relayed", "message_id", messageID, "type", reshareMessageKind(msg),
		"session", msg.SessionID, "origin", next.Origin, "received_from", from,
		"hop", next.Hop, "ttl_before", before, "ttl_after", next.TTL,
		"success", success, "failed", failed)
}

// broadcastReshareExcept 以有界扇出中继一条 Reshare 消息给除 excludeID 外的 peer。
// maxPeers<=0 视为无上限。对齐 tee-connect broadcastTEEDKRSExcept。
func (pm *ProtocolManager) broadcastReshareExcept(msg types.ReshareMessage, excludeID string, maxPeers int) (successCount, failCount int) {
	for _, peer := range pm.resharePeersSnapshot(excludeID) {
		if maxPeers > 0 && successCount+failCount >= maxPeers {
			break
		}
		if err := peer.SendReshare(msg); err != nil {
			log.Warn("Failed to relay Reshare message", "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	return successCount, failCount
}

func reshareTargetIndex(msg types.ReshareMessage) int {
	if msg.Share != nil {
		return bigIntIndex(msg.Share.ToIndex)
	}
	if msg.InputRequest != nil {
		return bigIntIndex(msg.InputRequest.DealerIndex)
	}
	if msg.InputResponse != nil {
		return bigIntIndex(msg.InputResponse.ReceiverIndex)
	}
	return 0
}

// reshareOriginIndex returns the committee member that created a message.
// It is used only to validate a direct peer route against Relay.Origin.
func reshareOriginIndex(msg types.ReshareMessage) int {
	if msg.Deal != nil {
		return bigIntIndex(msg.Deal.DealerIndex)
	}
	if msg.Share != nil {
		return bigIntIndex(msg.Share.FromIndex)
	}
	if msg.Response != nil {
		return bigIntIndex(msg.Response.DealerIndex)
	}
	if msg.VerifyKey != nil {
		return bigIntIndex(msg.VerifyKey.Index)
	}
	if msg.InputRequest != nil {
		return bigIntIndex(msg.InputRequest.ReceiverIndex)
	}
	if msg.InputResponse != nil {
		return bigIntIndex(msg.InputResponse.DealerIndex)
	}
	return 0
}

func (pm *ProtocolManager) SendReshareToIndex(index int, msg types.ReshareMessage) (successCount, failCount int) {
	msg = pm.prepareReshareOutbound(msg)
	_, _ = pm.noteReshareSeen(msg)
	peer := pm.targetPeerForReshare(index, msg)
	if peer == nil && msg.InputRequest == nil && msg.Complaint == nil {
		peer = pm.directCommitteePeer(index)
	}
	if peer != nil {
		if err := peer.SendReshare(msg); err != nil {
			log.Warn("Failed to send targeted Reshare message", "target", index, "peer", peer.id, "err", err)
			failCount++
		} else {
			log.Debug("Reshare targeted outbound", "target", index, "peer", peer.id,
				"type", reshareMessageKind(msg), "session", msg.SessionID, "ttl", msg.Relay.TTL)
			return 1, 0
		}
	}
	for _, peer := range pm.resharePeersSnapshot("") {
		if err := peer.SendReshare(msg); err != nil {
			log.Warn("Failed to send fallback Reshare message", "target", index, "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	return successCount, failCount
}

func (pm *ProtocolManager) resharePeersSnapshot(skipID string) []*peer {
	if pm == nil || pm.peers == nil {
		return nil
	}
	pm.peers.lock.RLock()
	defer pm.peers.lock.RUnlock()

	peers := make([]*peer, 0, len(pm.peers.peers))
	for id, peer := range pm.peers.peers {
		if skipID != "" && id == skipID {
			continue
		}
		peers = append(peers, peer)
	}
	return peers
}
