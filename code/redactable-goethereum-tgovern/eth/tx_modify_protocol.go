package eth

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	txModifySeenRetention    = 15 * time.Minute
	txModifyRequestRetention = 2 * time.Minute
	txModifyCacheRetention   = 30 * time.Minute
	txModifySeenMax          = 32768
	txModifyRequestMax       = 8192
	txModifyCacheMax         = 4096
	defaultTxModifyRelayTTL  = 1
)

type txModifyCacheEntry struct {
	msg      types.ModifyMsg
	cachedAt time.Time
}

func txModifyMsgKey(msg types.ModifyMsg) [32]byte {
	if msg.ReqID != ([32]byte{}) {
		return msg.ReqID
	}
	if encoded, err := rlp.EncodeToBytes(msg); err == nil {
		return [32]byte(crypto.Keccak256Hash(encoded))
	}
	return [32]byte{}
}

func txModifyRequestKey(req types.TxModifyRequest) [32]byte {
	if req.ReqID != ([32]byte{}) {
		return req.ReqID
	}
	if req.BlockNum == nil {
		return [32]byte{}
	}
	encoded, err := rlp.EncodeToBytes([]interface{}{req.BlockNum, req.Index})
	if err != nil {
		return [32]byte{}
	}
	return [32]byte(crypto.Keccak256Hash(encoded))
}

func txModifyFingerprint(msg types.ModifyMsg) (common.Hash, error) {
	msg.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	payload := append([]byte("txmodify-msg:"), encoded...)
	return crypto.Keccak256Hash(payload), nil
}

func (pm *ProtocolManager) defaultTxModifyRelayTTL() uint64 {
	ttl := pm.txModifyRelayTTL
	if ttl < 0 {
		return 0
	}
	return uint64(ttl)
}

func (pm *ProtocolManager) prepareTxModifyOutbound(msg types.ModifyMsg) types.ModifyMsg {
	if msg.RelayTTL == 0 {
		msg.RelayTTL = pm.defaultTxModifyRelayTTL()
	}
	return msg
}

func (pm *ProtocolManager) prepareTxModifyRequestOutbound(req types.TxModifyRequest) types.TxModifyRequest {
	if req.RelayTTL == 0 {
		req.RelayTTL = pm.defaultTxModifyRelayTTL()
	}
	return req
}

func txModifyRequestFingerprint(req types.TxModifyRequest) (common.Hash, error) {
	canonical := struct {
		ReqID    [32]byte
		BlockNum *big.Int
		Index    uint64
	}{
		ReqID:    req.ReqID,
		BlockNum: req.BlockNum,
		Index:    req.Index,
	}
	encoded, err := rlp.EncodeToBytes(canonical)
	if err != nil {
		return common.Hash{}, err
	}
	payload := append([]byte("txmodify-req:"), encoded...)
	return crypto.Keccak256Hash(payload), nil
}

func (pm *ProtocolManager) markTxModifySeen(msg types.ModifyMsg) bool {
	fp, err := txModifyFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint TxModifyMsg", "reqID", bytes32ToHexString(msg.ReqID), "err", err)
		return true
	}
	now := time.Now()
	pm.txModifyMu.Lock()
	defer pm.txModifyMu.Unlock()
	if pm.txModifySeen == nil {
		pm.txModifySeen = make(map[common.Hash]time.Time)
	}
	if seenAt, ok := pm.txModifySeen[fp]; ok && now.Sub(seenAt) < txModifySeenRetention {
		return false
	}
	pm.txModifySeen[fp] = now
	pruneHashTimesLocked(pm.txModifySeen, now, txModifySeenRetention, txModifySeenMax)
	return true
}

func (pm *ProtocolManager) markTxModifyRequestSeen(req types.TxModifyRequest) bool {
	fp, err := txModifyRequestFingerprint(req)
	if err != nil {
		log.Warn("Failed to fingerprint TxModifyRequest", "reqID", bytes32ToHexString(req.ReqID), "err", err)
		return true
	}
	now := time.Now()
	pm.txModifyMu.Lock()
	defer pm.txModifyMu.Unlock()
	if pm.txModifyRequests == nil {
		pm.txModifyRequests = make(map[common.Hash]time.Time)
	}
	if seenAt, ok := pm.txModifyRequests[fp]; ok && now.Sub(seenAt) < txModifyRequestRetention {
		return false
	}
	pm.txModifyRequests[fp] = now
	pruneHashTimesLocked(pm.txModifyRequests, now, txModifyRequestRetention, txModifyRequestMax)
	return true
}

func pruneHashTimesLocked(entries map[common.Hash]time.Time, now time.Time, retention time.Duration, limit int) {
	for hash, seenAt := range entries {
		if now.Sub(seenAt) >= retention {
			delete(entries, hash)
		}
	}
	for len(entries) > limit {
		var oldest common.Hash
		var oldestAt time.Time
		for hash, seenAt := range entries {
			if oldestAt.IsZero() || seenAt.Before(oldestAt) {
				oldest = hash
				oldestAt = seenAt
			}
		}
		delete(entries, oldest)
	}
}

func (pm *ProtocolManager) cacheTxModifyMsg(msg types.ModifyMsg) {
	key := txModifyMsgKey(msg)
	if key == ([32]byte{}) {
		return
	}
	now := time.Now()
	pm.txModifyMu.Lock()
	defer pm.txModifyMu.Unlock()
	if pm.txModifyCache == nil {
		pm.txModifyCache = make(map[[32]byte]txModifyCacheEntry)
	}
	pm.txModifyCache[key] = txModifyCacheEntry{msg: msg, cachedAt: now}
	pm.pruneTxModifyCacheLocked(now)
}

func (pm *ProtocolManager) pruneTxModifyCacheLocked(now time.Time) {
	for key, entry := range pm.txModifyCache {
		if now.Sub(entry.cachedAt) >= txModifyCacheRetention {
			delete(pm.txModifyCache, key)
		}
	}
	for len(pm.txModifyCache) > txModifyCacheMax {
		var oldest [32]byte
		var oldestAt time.Time
		for key, entry := range pm.txModifyCache {
			if oldestAt.IsZero() || entry.cachedAt.Before(oldestAt) {
				oldest = key
				oldestAt = entry.cachedAt
			}
		}
		delete(pm.txModifyCache, oldest)
	}
}

func (pm *ProtocolManager) cachedTxModifyMsg(req types.TxModifyRequest) (types.ModifyMsg, bool) {
	key := txModifyRequestKey(req)
	now := time.Now()
	pm.txModifyMu.Lock()
	defer pm.txModifyMu.Unlock()
	pm.pruneTxModifyCacheLocked(now)
	if key != ([32]byte{}) {
		if entry, ok := pm.txModifyCache[key]; ok && txModifyMatchesRequest(entry.msg, req) {
			return entry.msg, true
		}
	}
	for _, entry := range pm.txModifyCache {
		if txModifyMatchesRequest(entry.msg, req) {
			return entry.msg, true
		}
	}
	return types.ModifyMsg{}, false
}

func txModifyMatchesRequest(msg types.ModifyMsg, req types.TxModifyRequest) bool {
	if req.ReqID != ([32]byte{}) && msg.ReqID != req.ReqID {
		return false
	}
	if req.BlockNum != nil {
		msgBlock := msg.ProposalTx.Msg.BlockNum
		if msgBlock == nil || msgBlock.Cmp(req.BlockNum) != 0 {
			return false
		}
	}
	return msg.ProposalTx.Msg.Index == req.Index
}

func (pm *ProtocolManager) handleTxModifyGossip(msg types.ModifyMsg, origin *peer) {
	if err := validatePayloadOnlyModify(&msg.ProposalTx.Msg); err != nil {
		log.Warn("Discarding unsupported redaction gossip", "err", err, "reqID", bytes32ToHexString(msg.ReqID))
		pm.emitLedgerTiming("tx_modify_failed", msg, "reason", "unsupported_modify_type", "error", err)
		return
	}
	reqID := bytes32ToHexString(msg.ReqID)
	firstSeen := pm.markTxModifySeen(msg)
	pm.cacheTxModifyMsg(msg)
	if firstSeen {
		if msg.RelayTTL > 0 {
			exclude := ""
			if origin != nil {
				exclude = origin.id
			}
			relayMsg := msg
			relayMsg.RelayTTL--
			success, failed := pm.broadcastTxModifyMsgExcept(relayMsg, exclude)
			log.Info("Relayed TxModifyMsg", "reqID", reqID, "ttl", relayMsg.RelayTTL, "success", success, "failed", failed)
		} else {
			log.Debug("TxModifyMsg relay TTL exhausted", "reqID", reqID, "ttl", msg.RelayTTL)
		}
	} else {
		log.Debug("Duplicate TxModifyMsg received", "reqID", reqID)
		pm.emitLedgerTiming("tx_modify_duplicate", msg, "reason", "duplicate")
	}
	go pm.handleTxModifyMsg(msg)
}

func (pm *ProtocolManager) handleTxModifyRequest(req types.TxModifyRequest, origin *peer) {
	reqID := bytes32ToHexString(req.ReqID)
	if cached, ok := pm.cachedTxModifyMsg(req); ok {
		cached = pm.prepareTxModifyOutbound(cached)
		if origin != nil {
			if err := origin.SendModifyMsg(cached); err != nil {
				log.Warn("Failed to answer TxModifyRequest", "peer", origin.id, "reqID", reqID, "err", err)
			} else {
				log.Info("Answered TxModifyRequest from cache", "peer", origin.id, "reqID", reqID)
			}
		} else {
			pm.BroadcastModifyMsg(cached)
		}
	}
	if !pm.markTxModifyRequestSeen(req) {
		return
	}
	if req.RelayTTL == 0 {
		log.Debug("TxModifyRequest relay TTL exhausted", "reqID", reqID, "ttl", req.RelayTTL)
		return
	}
	exclude := ""
	if origin != nil {
		exclude = origin.id
	}
	relayReq := req
	relayReq.RelayTTL--
	success, failed := pm.broadcastTxModifyRequestExcept(relayReq, exclude)
	log.Info("Relayed TxModifyRequest", "reqID", reqID, "ttl", relayReq.RelayTTL, "success", success, "failed", failed)
}

func (pm *ProtocolManager) broadcastTxModifyRequestExcept(req types.TxModifyRequest, excludePeerID string) (int, int) {
	success, failed := 0, 0
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if err := peer.SendModifyRequest(req); err != nil {
			failed++
			log.Warn("BroadcastTxModifyRequest error", "peer", peer.id, "reqID", bytes32ToHexString(req.ReqID), "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastTxModifyRequest(req types.TxModifyRequest) (int, int) {
	if req.Nonce == 0 {
		req.Nonce = uint64(time.Now().UnixNano())
	}
	req = pm.prepareTxModifyRequestOutbound(req)
	if cached, ok := pm.cachedTxModifyMsg(req); ok {
		go pm.handleTxModifyMsg(cached)
	}
	pm.markTxModifyRequestSeen(req)
	return pm.broadcastTxModifyRequestExcept(req, "")
}

func (pm *ProtocolManager) beginTxModifyApply(key [32]byte) bool {
	if key == ([32]byte{}) {
		return true
	}
	pm.appliedRedLock.Lock()
	defer pm.appliedRedLock.Unlock()
	if pm.appliedRedactions == nil {
		pm.appliedRedactions = make(map[[32]byte]struct{})
	}
	if pm.pendingRedactions == nil {
		pm.pendingRedactions = make(map[[32]byte]struct{})
	}
	if _, ok := pm.appliedRedactions[key]; ok {
		return false
	}
	if _, ok := pm.pendingRedactions[key]; ok {
		return false
	}
	pm.pendingRedactions[key] = struct{}{}
	return true
}

func (pm *ProtocolManager) finishTxModifyApply(key [32]byte, applied bool) {
	if key == ([32]byte{}) {
		return
	}
	pm.appliedRedLock.Lock()
	defer pm.appliedRedLock.Unlock()
	if applied {
		if pm.appliedRedactions == nil {
			pm.appliedRedactions = make(map[[32]byte]struct{})
		}
		pm.appliedRedactions[key] = struct{}{}
	}
	delete(pm.pendingRedactions, key)
}

func (pm *ProtocolManager) markTxModifyApplied(key [32]byte) {
	pm.finishTxModifyApply(key, true)
}

func (pm *ProtocolManager) requestTxModify(reqID [32]byte, blockNum *big.Int, index uint64) (map[string]interface{}, error) {
	if pm == nil {
		return nil, errors.New("protocol manager not initialized")
	}
	req := types.TxModifyRequest{
		ReqID:    reqID,
		BlockNum: blockNum,
		Index:    index,
		Nonce:    uint64(time.Now().UnixNano()),
	}
	success, failed := pm.BroadcastTxModifyRequest(req)
	return map[string]interface{}{
		"Status":     "requested",
		"ReqID":      bytes32ToHexString(reqID),
		"BlockNum":   fmt.Sprintf("%v", blockNum),
		"Index":      index,
		"Recipients": success,
		"Failed":     failed,
	}, nil
}
