package eth

import (
	"fmt"
	"math/big"
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
	modifySeenTTL         = 15 * time.Minute
	modifySeenLimit       = 32768
	modifyRelayDefaultTTL = uint64(1)
)

var (
	modifyRelayMeter     = metrics.NewRegisteredCounter("nitch/modify/relay/sent", nil)
	modifyDuplicateMeter = metrics.NewRegisteredCounter("nitch/modify/relay/duplicate", nil)
	modifyTTLDropMeter   = metrics.NewRegisteredCounter("nitch/modify/relay/ttl_exhausted", nil)
)

func (pm *ProtocolManager) noteModifySeen(msg types.ModifyMsg) (string, bool) {
	fp, err := modifyFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint Modify message", "err", err)
		return msg.Relay.MessageID, true
	}
	return noteRelayFingerprint(pm, fp, msg.Relay.MessageID, "modify")
}

func (pm *ProtocolManager) noteModifyProposalSeen(msg types.RedactTx) (string, bool) {
	fp, err := modifyProposalFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint Modify proposal", "err", err)
		return msg.Relay.MessageID, true
	}
	return noteRelayFingerprint(pm, fp, msg.Relay.MessageID, "modify_proposal")
}

func (pm *ProtocolManager) noteKeyShareSeen(msg types.KeyShareMessage) (string, bool) {
	fp, err := keyShareFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint key share", "proposalID", msg.ProposalID, "err", err)
		return msg.Relay.MessageID, true
	}
	return noteRelayFingerprint(pm, fp, msg.Relay.MessageID, "key_share")
}

func noteRelayFingerprint(pm *ProtocolManager, fp common.Hash, claimed, kind string) (string, bool) {
	messageID := fp.Hex()
	if claimed != "" && claimed != messageID {
		log.Warn("NITCH relay message_id mismatch", "type", kind, "claimed", claimed, "computed", messageID)
	}
	return messageID, pm.noteModifyFingerprintSeen(fp)
}

func (pm *ProtocolManager) noteModifyFingerprintSeen(fp common.Hash) bool {
	if pm == nil {
		return true
	}
	now := time.Now()
	pm.modifySeenMu.Lock()
	defer pm.modifySeenMu.Unlock()

	if pm.modifySeen == nil {
		pm.modifySeen = make(map[common.Hash]time.Time)
	}
	pm.expireModifySeenLocked(now)
	if _, ok := pm.modifySeen[fp]; ok {
		return false
	}
	pm.modifySeen[fp] = now
	pm.modifySeenOrder = append(pm.modifySeenOrder, fp)
	for len(pm.modifySeen) > modifySeenLimit && len(pm.modifySeenOrder) > 0 {
		oldest := pm.modifySeenOrder[0]
		pm.modifySeenOrder = pm.modifySeenOrder[1:]
		delete(pm.modifySeen, oldest)
	}
	return true
}

func (pm *ProtocolManager) expireModifySeenLocked(now time.Time) {
	for len(pm.modifySeenOrder) > 0 {
		oldest := pm.modifySeenOrder[0]
		seenAt, ok := pm.modifySeen[oldest]
		if !ok {
			pm.modifySeenOrder = pm.modifySeenOrder[1:]
			continue
		}
		if now.Sub(seenAt) <= modifySeenTTL {
			return
		}
		pm.modifySeenOrder = pm.modifySeenOrder[1:]
		delete(pm.modifySeen, oldest)
	}
}

func modifyFingerprint(msg types.ModifyMsg) (common.Hash, error) {
	msg.Relay = types.RelayMetadata{}
	proposalHash, err := modifyProposalFingerprint(msg.RedactTx)
	if err != nil {
		return common.Hash{}, err
	}
	return prefixedRLPHash("modify-msg", struct {
		ProposalHash common.Hash
		NewBlock     *types.Block
	}{proposalHash, msg.NewBlock})
}

func modifyProposalFingerprint(msg types.RedactTx) (common.Hash, error) {
	createdAt := uint64(0)
	if !msg.CreateTime.IsZero() && msg.CreateTime.UnixNano() > 0 {
		createdAt = uint64(msg.CreateTime.UnixNano())
	}
	return prefixedRLPHash("modify-proposal", struct {
		BlockNum   *big.Int
		CreatedAt  uint64
		ProposalID string
		Collector  string
		OldMessage []byte
		NewMessage []byte
		HashOutput *chameleon.HashOutput
		Msg        types.ModifiedMessage
	}{msg.BlockNum, createdAt, msg.ProposalID, msg.CollectorID,
		msg.OldMessage, msg.NewMessage, msg.HashOutput, msg.Msg})
}

func keyShareFingerprint(msg types.KeyShareMessage) (common.Hash, error) {
	msg.Relay = types.RelayMetadata{}
	return prefixedRLPHash("key-share", msg)
}

func (pm *ProtocolManager) prepareModifyProposalOutbound(msg types.RedactTx) types.RedactTx {
	fingerprint, err := modifyProposalFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint outbound Modify proposal", "err", err)
		return msg
	}
	msg.Relay = types.RelayMetadata{MessageID: fingerprint.Hex(), Origin: pm.relayOrigin(), TTL: pm.modifyRelayTTL}
	return msg
}

func (pm *ProtocolManager) prepareKeyShareOutbound(msg types.KeyShareMessage) types.KeyShareMessage {
	fingerprint, err := keyShareFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint outbound key share", "proposalID", msg.ProposalID, "err", err)
		return msg
	}
	msg.Relay = types.RelayMetadata{MessageID: fingerprint.Hex(), Origin: pm.relayOrigin(), TTL: pm.modifyRelayTTL}
	return msg
}

func (pm *ProtocolManager) prepareModifyOutbound(msg types.ModifyMsg) types.ModifyMsg {
	fingerprint, err := modifyFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint outbound Modify message", "err", err)
		return msg
	}
	msg.Relay = types.RelayMetadata{MessageID: fingerprint.Hex(), Origin: pm.relayOrigin(), TTL: pm.modifyRelayTTL}
	return msg
}

func prefixedRLPHash(prefix string, value interface{}) (common.Hash, error) {
	encoded, err := rlp.EncodeToBytes(value)
	if err != nil {
		return common.Hash{}, err
	}
	payload := append([]byte(fmt.Sprintf("%s:", prefix)), encoded...)
	return crypto.Keccak256Hash(payload), nil
}

func (pm *ProtocolManager) relayModifyProposal(msg types.RedactTx, origin *peer) (successCount, failCount int) {
	skipID := ""
	if origin != nil {
		skipID = origin.id
	}
	reqID := redactionJoinKey(msg.BlockNum, msg.Msg.Index)
	next, ok := advanceRelay(msg.Relay)
	if !ok {
		modifyTTLDropMeter.Inc(1)
		log.Debug("Modify proposal relay TTL exhausted", "message_id", msg.Relay.MessageID,
			"join_key", reqID, "origin", msg.Relay.Origin, "received_from", skipID,
			"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL)
		return 0, 0
	}
	before := msg.Relay.TTL
	msg.Relay = next
	for _, peer := range pm.modifyPeersSnapshot(skipID) {
		if err := peer.SendModifyProposal(msg); err != nil {
			log.Warn("Failed to relay Modify proposal", "join_key", reqID, "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	modifyRelayMeter.Inc(int64(successCount))
	log.Debug("Modify proposal relayed", "message_id", next.MessageID, "join_key", reqID,
		"origin", next.Origin, "received_from", skipID, "hop", next.Hop,
		"ttl_before", before, "ttl_after", next.TTL, "success", successCount, "failed", failCount)
	return successCount, failCount
}

func (pm *ProtocolManager) relayKeyShare(msg types.KeyShareMessage, origin *peer) (successCount, failCount int) {
	skipID := ""
	if origin != nil {
		skipID = origin.id
	}
	next, ok := advanceRelay(msg.Relay)
	if !ok {
		modifyTTLDropMeter.Inc(1)
		log.Debug("Key share relay TTL exhausted", "message_id", msg.Relay.MessageID,
			"proposalID", msg.ProposalID, "origin", msg.Relay.Origin, "received_from", skipID,
			"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL)
		return 0, 0
	}
	before := msg.Relay.TTL
	msg.Relay = next
	for _, peer := range pm.modifyPeersSnapshot(skipID) {
		if err := peer.SendKeyShare(msg); err != nil {
			log.Warn("Failed to relay key share", "proposalID", msg.ProposalID, "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	modifyRelayMeter.Inc(int64(successCount))
	log.Debug("Key share relayed", "message_id", next.MessageID, "proposalID", msg.ProposalID,
		"origin", next.Origin, "received_from", skipID, "hop", next.Hop,
		"ttl_before", before, "ttl_after", next.TTL, "success", successCount, "failed", failCount)
	return successCount, failCount
}

func (pm *ProtocolManager) localPeerID() string {
	if pm == nil || pm.p2pServer == nil || pm.p2pServer.Self() == nil {
		return ""
	}
	return pm.p2pServer.Self().ID().TerminalString()
}

func (pm *ProtocolManager) ensureModifyCollector(redactTx *types.RedactTx) {
	if redactTx == nil || redactTx.CollectorID != "" {
		return
	}
	redactTx.CollectorID = pm.localPeerID()
}

func (pm *ProtocolManager) relayModifyMsg(msg types.ModifyMsg, origin *peer) (successCount, failCount int) {
	skipID := ""
	if origin != nil {
		skipID = origin.id
	}
	reqID := redactionJoinKey(msg.RedactTx.BlockNum, msg.RedactTx.Msg.Index)
	next, ok := advanceRelay(msg.Relay)
	if !ok {
		modifyTTLDropMeter.Inc(1)
		log.Debug("TxModify relay TTL exhausted", "message_id", msg.Relay.MessageID,
			"join_key", reqID, "origin", msg.Relay.Origin, "received_from", skipID,
			"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL)
		return 0, 0
	}
	before := msg.Relay.TTL
	msg.Relay = next
	for _, peer := range pm.modifyPeersSnapshot(skipID) {
		if err := peer.SendModifyMsg(msg); err != nil {
			log.Warn("Failed to relay Modify message", "join_key", reqID, "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	modifyRelayMeter.Inc(int64(successCount))
	log.Debug("TxModify message relayed", "message_id", next.MessageID, "join_key", reqID,
		"origin", next.Origin, "received_from", skipID, "hop", next.Hop,
		"ttl_before", before, "ttl_after", next.TTL, "success", successCount, "failed", failCount)
	return successCount, failCount
}

func (pm *ProtocolManager) modifyPeersSnapshot(skipID string) []*peer {
	if pm == nil || pm.peers == nil {
		return nil
	}
	peers := pm.peers.Peers()
	if skipID == "" {
		return peers
	}
	filtered := peers[:0]
	for _, peer := range peers {
		if peer.id != skipID {
			filtered = append(filtered, peer)
		}
	}
	return filtered
}
