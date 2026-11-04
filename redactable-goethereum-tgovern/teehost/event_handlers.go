package teehost

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/contractService"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

func (h *Host) RegisterRedactionMaterial(ctx context.Context, material RedactionMaterial, broadcast bool) error {
	if err := h.materials.Put(ctx, material); err != nil {
		return err
	}
	if broadcast {
		if network := h.protocolNetwork(); network != nil {
			if err := network.BroadcastRedactMaterial(ctx, material); err != nil {
				return err
			}
		}
	}
	return h.tryStartPartialAdapt(ctx, material.ReqID)
}

func (h *Host) syncCurrentActiveSet(ctx context.Context) error {
	params, err := h.contract.GetSystemParams()
	if err != nil || params == nil || params.Epoch == nil {
		return err
	}
	info, err := h.contract.GetActiveSetInfo(params.Epoch)
	if err != nil {
		return err
	}
	localKey := h.identity.primaryKey()
	if indexOfSigPubKey(info.HardwareIDs, localKey) == 0 {
		activeTEEs, err := h.contract.GetActiveTEEs()
		if err != nil {
			return err
		}
		if indexOfSigPubKey(activeTEEs, localKey) == 0 {
			return nil
		}
		info = &contractService.ActiveSetInfo{
			Epoch:                  new(big.Int).Set(params.Epoch),
			ActiveSetHash:          sigPubKeysHash(activeTEEs),
			HardwareIDs:            append([][32]byte(nil), activeTEEs...),
			PkCHHash:               params.PkCHHash,
			CommitteePubKeyVersion: params.CommitteeVersion,
			ActiveThreshold:        params.ActiveThreshold,
			ActiveLimit:            params.ActiveLimit,
		}
	}
	return h.handleActiveSet(ctx, info.Epoch, info.ActiveSetHash, info.HardwareIDs, false, rotationTrigger{})
}

func (h *Host) syncCurrentActiveSetUntilActive(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()

	for {
		if err := h.syncCurrentActiveSet(ctx); err != nil {
			log.Debug("TEE host active set sync deferred", "err", err)
		}
		if h.isActive() && h.hasDistKGSession() {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			sigPubKey := h.identity.primaryKey()
			log.Warn("TEE host active set sync timed out", "hardwareID", hexKey(h.identity.HardwareID), "sigPubKey", hexKey(sigPubKey))
			return
		case <-ctx.Done():
			return
		}
	}
}

func (h *Host) HandleRedactMaterial(ctx context.Context, material RedactionMaterial, network ProtocolNetwork) error {
	if network != nil {
		h.SetProtocolNetwork(network)
	}
	return h.RegisterRedactionMaterial(ctx, material, false)
}

// rotationTrigger carries the on-chain location of the log event that drove an
// active-set transition, so the rotation timing anchors can point back at the
// exact block/tx that triggered the epoch. The zero value means "unknown"
// (e.g. the syncCurrentActiveSet polling path, which has no event log).
type rotationTrigger struct {
	block uint64
	tx    common.Hash
}

// fields renders the trigger as emitTiming key/value pairs. trigger_block=0 is
// skipped so the anchor stays honest when the trigger is unknown.
func (t rotationTrigger) fields() []interface{} {
	out := []interface{}{}
	if t.block != 0 {
		out = append(out, "trigger_block", t.block)
	}
	if t.tx != (common.Hash{}) {
		out = append(out, "trigger_tx", t.tx.Hex())
	}
	return out
}

func (h *Host) handleActiveSet(ctx context.Context, epoch *big.Int, activeSetHash [32]byte, sigPubKeys [][32]byte, rotated bool, trigger rotationTrigger) error {
	if epoch == nil {
		return fmt.Errorf("active set epoch is nil")
	}
	localKey := h.identity.primaryKey()
	newIndex := indexOfSigPubKey(sigPubKeys, localKey)
	isActive := newIndex > 0
	h.mu.Lock()
	previousEpoch := cloneBigInt(h.currentEpoch)
	previousActiveSet := append([][32]byte(nil), h.activeSet...)
	dkrsOldEpoch := cloneBigInt(previousEpoch)
	dkrsOldActiveSet := append([][32]byte(nil), previousActiveSet...)
	if rotated && epoch.Sign() > 0 {
		candidateOldEpoch := new(big.Int).Sub(epoch, big.NewInt(1))
		if cached := h.activeSetByEpoch[candidateOldEpoch.String()]; len(cached) > 0 {
			dkrsOldEpoch = candidateOldEpoch
			dkrsOldActiveSet = append([][32]byte(nil), cached...)
		}
	}
	dkrsOldIndex := indexOfSigPubKey(dkrsOldActiveSet, localKey)
	if previousEpoch != nil && len(previousActiveSet) > 0 {
		h.activeSetByEpoch[previousEpoch.String()] = append([][32]byte(nil), previousActiveSet...)
	}
	h.currentEpoch = new(big.Int).Set(epoch)
	h.activeSetHash = activeSetHash
	h.activeSet = append([][32]byte(nil), sigPubKeys...)
	h.activeSetByEpoch[epoch.String()] = append([][32]byte(nil), sigPubKeys...)
	h.active = isActive
	h.mu.Unlock()

	log.Info("TEE host active set update", "epoch", epoch, "rotated", rotated, "isActive", isActive, "localIndex", newIndex, "oldIndex", dkrsOldIndex, "hardwareID", hexKey(h.identity.HardwareID), "sigPubKey", hexKey(localKey), "activeSetHash", hexKey(activeSetHash), "activeSigPubKeys", hexKeys(sigPubKeys, 8))
	if rotated {
		fields := append([]interface{}{"duration_ns", int64(0),
			"epoch", epochString(epoch), "status", "ok"}, trigger.fields()...)
		h.emitTiming("rotation_flow", "rotation_trigger_seen", fields...)
	}

	if !isActive && !(rotated && dkrsOldIndex > 0) {
		log.Debug("TEE host ignored active set event; local TEE is not active", "epoch", epoch, "hardwareID", hexKey(h.identity.HardwareID), "sigPubKey", hexKey(localKey))
		return nil
	}
	activeSetRPCStart := time.Now()
	info, err := h.contract.GetActiveSetInfo(epoch)
	status := "ok"
	if err != nil {
		status = "error"
	}
	if rotated {
		h.emitTiming("rotation_flow", "rot_contract_active_set", "duration_ns", time.Since(activeSetRPCStart).Nanoseconds(),
			"epoch", epochString(epoch), "rpc", "GetActiveSetInfo", "status", status)
	}
	if err != nil {
		return err
	}
	if indexOfSigPubKey(info.HardwareIDs, localKey) == 0 && newIndex > 0 {
		info = &contractService.ActiveSetInfo{
			Epoch:                  new(big.Int).Set(epoch),
			ActiveSetHash:          activeSetHash,
			HardwareIDs:            append([][32]byte(nil), sigPubKeys...),
			PkCHHash:               info.PkCHHash,
			CommitteePubKeyVersion: info.CommitteePubKeyVersion,
			ActiveThreshold:        info.ActiveThreshold,
			ActiveLimit:            info.ActiveLimit,
		}
	}
	if err := h.startDistKGIfNeeded(ctx, info); err != nil {
		return err
	}
	if rotated && (dkrsOldIndex > 0 || isActive) {
		pkCHRPCStart := time.Now()
		pkCH, err := h.contract.GetPkCH()
		status := "ok"
		if err != nil {
			status = "error"
		}
		h.emitTiming("rotation_flow", "rot_contract_pkch", "duration_ns", time.Since(pkCHRPCStart).Nanoseconds(),
			"epoch", epochString(epoch), "rpc", "GetPkCH", "status", status)
		if err != nil {
			return err
		}
		if len(pkCH) == 0 {
			return nil
		}
		return h.startDKRSForRotation(ctx, dkrsOldEpoch, dkrsOldActiveSet, info)
	}
	return nil
}

func (h *Host) startDistKGIfNeeded(ctx context.Context, info *contractService.ActiveSetInfo) error {
	if info == nil || info.Epoch == nil {
		return nil
	}
	pkCH, err := h.contract.GetPkCH()
	if err != nil {
		return err
	}
	if len(pkCH) != 0 {
		return nil
	}
	myIndex := indexOfSigPubKey(info.HardwareIDs, h.identity.primaryKey())
	if myIndex == 0 {
		return nil
	}
	threshold := bigIntToInt(info.ActiveThreshold, 0)
	numParties := len(info.HardwareIDs)
	activeLimit := bigIntToInt(info.ActiveLimit, 0)
	if activeLimit > 0 && numParties < activeLimit {
		log.Debug("TEE host deferred DistKG until active set reaches limit", "epoch", info.Epoch, "active", numParties, "limit", activeLimit)
		return nil
	}
	if threshold <= 0 || numParties < threshold {
		log.Debug("TEE host deferred DistKG until active threshold is met", "epoch", info.Epoch, "active", numParties, "threshold", threshold)
		return nil
	}
	network := h.protocolNetwork()
	if network == nil {
		log.Warn("TEE host cannot start DistKG without protocol network", "epoch", info.Epoch)
		return nil
	}
	key := fmt.Sprintf("%s:%x", info.Epoch.String(), info.ActiveSetHash)
	h.mu.Lock()
	if _, ok := h.startedDistKG[key]; ok {
		h.mu.Unlock()
		return nil
	}
	h.startedDistKG[key] = struct{}{}
	h.mu.Unlock()

	log.Info("TEE host starting DistKG", "epoch", info.Epoch, "index", myIndex, "threshold", threshold, "parties", numParties, "sigPubKey", hexKey(h.identity.primaryKey()), "activeSigPubKeys", hexKeys(info.HardwareIDs, 8))
	_, err = h.StartDistKG(ctx, DistKGProtocolConfig{
		MyIndex:     myIndex,
		Epoch:       cloneBigInt(info.Epoch),
		Threshold:   threshold - 1,
		NumParties:  numParties,
		ActiveSet:   append([][32]byte(nil), info.HardwareIDs...),
		Attestation: attestationFromActiveSetInfo(info),
	}, network)
	if err != nil {
		h.mu.Lock()
		delete(h.startedDistKG, key)
		h.mu.Unlock()
		return err
	}
	return h.HandleDistKG(ctx, DistKGProtocolMessage{}, network)
}

func (h *Host) startDKRSForRotation(ctx context.Context, oldEpoch *big.Int, oldActiveSet [][32]byte, info *contractService.ActiveSetInfo) error {
	if info == nil || info.Epoch == nil {
		return nil
	}
	if len(oldActiveSet) == 0 {
		log.Warn("TEE host cannot start DKRS without previous active set", "epoch", info.Epoch)
		return nil
	}
	localKey := h.identity.primaryKey()
	dealerIndex := indexOfSigPubKey(oldActiveSet, localKey)
	receiverIndex := indexOfSigPubKey(info.HardwareIDs, localKey)
	if dealerIndex == 0 && receiverIndex == 0 {
		return nil
	}
	return h.runDKRSForRotation(ctx, oldEpoch, oldActiveSet, info)
}

// runDKRSForRotation runs the DKRS resharing for a rotation assuming peer EPKs
// are ready. It is the no-gate entry point shared by startDKRSForRotation and
// the loop ticker retrying a previously-deferred (pending) rotation; the
// startedDKRS guard makes it idempotent across both callers.
func (h *Host) runDKRSForRotation(ctx context.Context, oldEpoch *big.Int, oldActiveSet [][32]byte, info *contractService.ActiveSetInfo) error {
	key := fmt.Sprintf("%s:%x", info.Epoch.String(), info.ActiveSetHash)
	localKey := h.identity.primaryKey()
	dealerIndex := indexOfSigPubKey(oldActiveSet, localKey)
	receiverIndex := indexOfSigPubKey(info.HardwareIDs, localKey)
	network := h.protocolNetwork()
	if network == nil {
		log.Warn("TEE host cannot start DKRS without protocol network", "epoch", info.Epoch)
		return nil
	}
	threshold := bigIntToInt(info.ActiveThreshold, 0)
	if threshold <= 0 {
		return fmt.Errorf("invalid active threshold for DKRS: %d", threshold)
	}
	oldQual := make([]int, 0, len(oldActiveSet))
	for i := range oldActiveSet {
		oldQual = append(oldQual, i+1)
	}
	sessionID := dkrsSessionID(oldEpoch, info.Epoch, sigPubKeysHash(oldActiveSet), info.ActiveSetHash, len(oldActiveSet), len(info.HardwareIDs), threshold)
	if dealerIndex > 0 && h.hasKnownMissingFreshShareForEpoch(oldEpoch) {
		log.Warn("TEE host skipped DKRS dealer role: missing fresh share for old epoch", "oldEpoch", oldEpoch, "newEpoch", info.Epoch, "session", shortSessionID(sessionID), "dealerIndex", dealerIndex, "receiverIndex", receiverIndex)
		dealerIndex = 0
		if receiverIndex == 0 {
			return nil
		}
	}
	h.mu.Lock()
	if _, ok := h.startedDKRS[key]; ok {
		h.mu.Unlock()
		return nil
	}
	h.startedDKRS[key] = struct{}{}
	h.mu.Unlock()
	h.emitTiming("rotation_flow", "rotation_session_init",
		"duration_ns", int64(0),
		"epoch", epochString(info.Epoch), "session", shortSessionID(sessionID),
		"dealerIndex", dealerIndex, "receiverIndex", receiverIndex, "status", "ok")
	log.Info("TEE host starting DKRS", "oldEpoch", oldEpoch, "newEpoch", info.Epoch, "session", shortSessionID(sessionID), "dealerIndex", dealerIndex, "receiverIndex", receiverIndex, "oldParties", len(oldActiveSet), "newParties", len(info.HardwareIDs), "threshold", threshold, "sigPubKey", hexKey(localKey), "oldSigPubKeys", hexKeys(oldActiveSet, 8), "newSigPubKeys", hexKeys(info.HardwareIDs, 8))
	rotStart := time.Now()
	_, err := h.StartDKRS(ctx, DKRSProtocolConfig{
		DealerIndex:   dealerIndex,
		ReceiverIndex: receiverIndex,
		SessionID:     sessionID,
		OldEpoch:      cloneBigInt(oldEpoch),
		NewEpoch:      cloneBigInt(info.Epoch),
		// The contract fixes tT at construction for every epoch.
		OldThreshold:  threshold - 1,
		NewThreshold:  threshold - 1,
		NewNumParties: len(info.HardwareIDs),
		OldNumParties: len(oldActiveSet),
		OldQual:       oldQual,
		OldActiveSet:  append([][32]byte(nil), oldActiveSet...),
		NewActiveSet:  append([][32]byte(nil), info.HardwareIDs...),
		Attestation:   attestationFromActiveSetInfo(info),
	}, network)
	if err != nil {
		h.mu.Lock()
		delete(h.startedDKRS, key)
		h.mu.Unlock()
		return err
	}
	h.emitTiming("rotation_flow", "rot_start_dkrs",
		"duration_ns", time.Since(rotStart).Nanoseconds(),
		"epoch", epochString(info.Epoch), "session", shortSessionID(sessionID),
		"dealerIndex", dealerIndex, "receiverIndex", receiverIndex)
	if dealerIndex > 0 {
		h.markDKRSDealerDone(oldEpoch)
	}
	if receiverIndex == 0 {
		return h.flushPendingErase(ctx, oldEpoch)
	}
	rotStep := time.Now()
	if err := h.withDKRSPeerEPKs(ctx, oldEpoch, oldActiveSet, "old", func() error { return nil }); err != nil {
		h.mu.Lock()
		delete(h.startedDKRS, key)
		h.mu.Unlock()
		return err
	}
	h.emitTiming("rotation_flow", "rot_old_epks",
		"duration_ns", time.Since(rotStep).Nanoseconds(),
		"epoch", epochString(info.Epoch), "session", shortSessionID(sessionID))
	rotStep = time.Now()
	if err := h.handlePendingDKRS(ctx, network); err != nil {
		return err
	}
	if err := h.HandleDKRS(ctx, DKRSProtocolMessage{}, network); err != nil {
		return err
	}
	h.emitTiming("rotation_flow", "rot_first_advance",
		"duration_ns", time.Since(rotStep).Nanoseconds(),
		"epoch", epochString(info.Epoch), "session", shortSessionID(sessionID))
	return h.flushPendingErase(ctx, oldEpoch)
}

func (h *Host) registerDKRSPeerEPKs(ctx context.Context, epoch *big.Int, activeSet [][32]byte, role string) error {
	registrar, ok := h.tee.(TEEPeerEPKRegistrar)
	if !ok {
		return nil
	}
	epkStart := time.Now()
	var totalRpcDur time.Duration
	var totalRegDur time.Duration
	for i, sigPubKey := range activeSet {
		index := uint64(i + 1)
		rpcStart := time.Now()
		epk, err := h.contract.GetTEEEnclavePK(sigPubKey)
		totalRpcDur += time.Since(rpcStart)
		if err != nil {
			log.Error("TEE host failed to load DKRS peer EPK", "epoch", epoch, "role", role, "index", index, "sigPubKey", hexKey(sigPubKey), "reason", err)
			return fmt.Errorf("load %s DKRS peer EPK epoch %s index %d sigPubKey %s: %w", role, epochString(epoch), index, hexKey(sigPubKey), err)
		}
		if err := validatePeerEPK(epk); err != nil {
			log.Error("TEE host invalid DKRS peer EPK", "epoch", epoch, "role", role, "index", index, "sigPubKey", hexKey(sigPubKey), "reason", err)
			return fmt.Errorf("invalid %s DKRS peer EPK epoch %s index %d sigPubKey %s: %w", role, epochString(epoch), index, hexKey(sigPubKey), err)
		}
		regStart := time.Now()
		if err := registrar.RegisterPeerEPK(ctx, index, epk); err != nil {
			return fmt.Errorf("register %s DKRS peer EPK index %d sigPubKey %s: %w", role, index, hexKey(sigPubKey), err)
		}
		totalRegDur += time.Since(regStart)
		log.Debug("TEE host registered DKRS peer EPK", "role", role, "index", index, "mySigPubKey", hexKey(h.identity.primaryKey()), "peerSigPubKey", hexKey(sigPubKey), "epkHash", shortPayloadHash(epk))
	}
	h.emitTiming("rotation_flow", "rot_contract_epk",
		"duration_ns", totalRpcDur.Nanoseconds(),
		"epoch", epochString(epoch), "role", role, "peer_count", len(activeSet), "status", "ok")
	h.emitTiming("rotation_flow", "rot_epk_sync",
		"duration_ns", time.Since(epkStart).Nanoseconds(),
		"epoch", epochString(epoch), "role", role, "peer_count", len(activeSet),
		"rpc_duration_ns", totalRpcDur.Nanoseconds(),
		"register_duration_ns", totalRegDur.Nanoseconds(),
		"status", "ok")
	return nil
}

func (h *Host) withDKRSPeerEPKs(ctx context.Context, epoch *big.Int, activeSet [][32]byte, role string, fn func() error) error {
	if len(activeSet) == 0 {
		return fn()
	}
	h.dkrsEPKMu.Lock()
	defer h.dkrsEPKMu.Unlock()
	if err := h.registerDKRSPeerEPKs(ctx, epoch, activeSet, role); err != nil {
		return err
	}
	return fn()
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func (h *Host) markFreshShareEpoch(epoch *big.Int) {
	if epoch == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.freshShareEpochs == nil {
		h.freshShareEpochs = make(map[string]struct{})
	}
	h.freshShareEpochs[epoch.String()] = struct{}{}
}

func (h *Host) hasKnownMissingFreshShareForEpoch(epoch *big.Int) bool {
	if epoch == nil {
		return false
	}
	h.mu.Lock()
	if _, ok := h.freshShareEpochs[epoch.String()]; ok {
		h.mu.Unlock()
		return false
	}
	h.mu.Unlock()

	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	if h.dkrsSession == nil ||
		h.dkrsSession.Config.NewEpoch == nil ||
		h.dkrsSession.Config.NewEpoch.Cmp(epoch) != 0 ||
		h.dkrsSession.Config.ReceiverIndex == 0 {
		return false
	}
	return h.dkrsSession.Extracted == nil
}

func (h *Host) handleRedactAuthorized(ctx context.Context, ev *contracts.ContractsRedactAuthorized) error {
	if ev == nil {
		return fmt.Errorf("redact authorized event is nil")
	}
	h.mu.Lock()
	if h.redactionExecutionActiveLocked(ev.ReqID) {
		h.mu.Unlock()
		log.Debug("TEE host ignored duplicate authorization for active redaction", "reqID", common.Bytes2Hex(ev.ReqID[:]))
		return nil
	}
	_, timingRecorded := h.authorizedTiming[ev.ReqID]
	if !timingRecorded {
		h.authorizedTiming[ev.ReqID] = struct{}{}
	}
	h.pendingAuth[ev.ReqID] = &RedactAuthorization{
		ReqID:     ev.ReqID,
		Epoch:     cloneBigInt(ev.Epoch),
		DReq:      ev.DReq,
		DPolicy:   ev.DPolicy,
		ExpiresAt: cloneBigInt(ev.ExpiresAt),
	}
	h.mu.Unlock()
	if timingRecorded {
		log.Debug("TEE host received duplicate redaction authorization", "reqID", common.Bytes2Hex(ev.ReqID[:]))
	} else {
		h.emitTiming("edit_execution", "tee_authorized_received",
			"join_key", common.Bytes2Hex(ev.ReqID[:]),
			"tee_host", hexKey(h.identity.primaryKey()),
			"epoch", ev.Epoch,
			"source", "contract",
			"status", "accepted",
		)
	}
	return h.tryStartPartialAdapt(ctx, ev.ReqID)
}

func (h *Host) HandleRedactAuthorizedMessage(ctx context.Context, msg RedactAuthorizedMessage, network ProtocolNetwork) error {
	if network != nil {
		h.SetProtocolNetwork(network)
	}
	return h.RegisterRedactAuthorization(ctx, msg, false)
}

func (h *Host) RegisterRedactAuthorization(ctx context.Context, msg RedactAuthorizedMessage, broadcast bool) error {
	reqID := msg.ReqID
	h.mu.Lock()
	closed := h.redactionExecutionActiveLocked(reqID)
	h.mu.Unlock()
	if closed {
		return nil
	}
	verifier, ok := h.contract.(interface {
		VerifyRedactAuthorization(context.Context, contractService.RedactReqFields, [32]byte, [32]byte, *big.Int, *big.Int, [32]byte, []byte) error
	})
	if !ok {
		return fmt.Errorf("contract backend cannot verify P2P redaction authorization")
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := verifier.VerifyRedactAuthorization(verifyCtx, contractService.RedactReqFields{
		Version: msg.Version, Epoch: msg.Epoch, ReqID: msg.ReqID, BodyHashes: msg.BodyHashes,
	}, msg.DReq, msg.DPolicy, msg.CommitteePubKeyVersion, msg.ExpiresAt, msg.AuthDigest, msg.AggregateSig)
	cancel()
	if err != nil {
		return err
	}
	if broadcast {
		if network := h.protocolNetwork(); network != nil {
			if err := network.BroadcastRedactAuthorized(ctx, msg); err != nil {
				return err
			}
		}
	}
	h.mu.Lock()
	if h.redactionExecutionActiveLocked(reqID) {
		h.mu.Unlock()
		log.Debug("TEE host ignored duplicate authorization for active redaction", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	_, timingRecorded := h.authorizedTiming[reqID]
	if !timingRecorded {
		h.authorizedTiming[reqID] = struct{}{}
	}
	// CertReqAuthMsg/CertReqAuthSig stay empty on purpose. The enclave only runs
	// verify_cert_req_auth when both are present, and it verifies them as a
	// BLS12-381 signature against g_vk_gc, whereas the committee signs with its
	// BN254 scheme (committee-mvp/internal/crypto). Handing the committee
	// signature to the enclave therefore fails with SGX_ERROR_INVALID_SIGNATURE
	// (0x2003) and no host can start. The contract-event path leaves these empty
	// too; the request binding travels through d_req/d_policy/expires_at instead.
	// Propagating AuthDigest/AggregateSig here would require the committee to
	// sign with the same BLS12-381 curve the enclave verifies against.
	h.pendingAuth[reqID] = &RedactAuthorization{
		ReqID:                  reqID,
		Epoch:                  new(big.Int).SetUint64(msg.Epoch),
		DReq:                   msg.DReq,
		DPolicy:                msg.DPolicy,
		ExpiresAt:              cloneBigInt(msg.ExpiresAt),
		CommitteePubKeyVersion: cloneBigInt(msg.CommitteePubKeyVersion),
		AuthDigest:             msg.AuthDigest,
		AggregatePubKey:        append([]byte(nil), msg.AggregatePubKey...),
		Bitmap:                 append([]byte(nil), msg.Bitmap...),
	}
	h.mu.Unlock()
	if timingRecorded {
		log.Debug("TEE host received duplicate redaction authorization", "reqID", common.Bytes2Hex(reqID[:]))
	} else {
		h.emitTiming("edit_execution", "tee_authorized_received",
			"join_key", common.Bytes2Hex(reqID[:]),
			"tee_host", hexKey(h.identity.primaryKey()),
			"epoch", msg.Epoch,
			"source", "p2p",
			"status", "accepted",
		)
	}
	return h.tryStartPartialAdapt(ctx, reqID)
}

func (h *Host) tryStartPartialAdapt(ctx context.Context, reqID [32]byte) (err error) {
	joinKey := common.Bytes2Hex(reqID[:])
	span := h.beginEditTiming("partial_adapt_validation", "join_key", joinKey, "tee", hexKey(h.identity.primaryKey()))
	defer func() { span(err) }()
	if !h.isActive() {
		return nil
	}
	if h.waitingForDKRSExtract() {
		log.Debug("TEE host deferred PartialAdapt until DKRS extraction completes", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	h.mu.Lock()
	auth := h.pendingAuth[reqID]
	if auth == nil {
		h.mu.Unlock()
		return nil
	}
	if h.redactionExecutionActiveLocked(reqID) {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	material, ok, err := h.materials.Get(ctx, reqID)
	if err != nil || !ok {
		return err
	}
	if material.DReq != ([32]byte{}) && material.DReq != auth.DReq {
		return fmt.Errorf("redaction material dReq mismatch")
	}
	if material.DPolicy != ([32]byte{}) && material.DPolicy != auth.DPolicy {
		return fmt.Errorf("redaction material dPolicy mismatch")
	}
	activeSet := h.activeSetSnapshot()
	myIndex := indexOfSigPubKey(activeSet, h.identity.primaryKey())
	if myIndex == 0 {
		return nil
	}
	h.mu.Lock()
	currentActiveHash := h.activeSetHash
	currentEpoch := cloneBigInt(h.currentEpoch)
	h.mu.Unlock()
	network := h.protocolNetwork()
	if network == nil {
		log.Warn("TEE host cannot start PartialAdapt without protocol network", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	threshold := material.Threshold
	if threshold == 0 {
		threshold = len(activeSet)
	}
	// The authorization certificate is never sourced from the P2P message: the
	// enclave verifies it as BLS12-381 while the committee signs with BN254, so
	// supplying it would make the enclave reject the call outright. The enclave
	// reads all three of these fields together and only binds the digest when
	// each is the expected width, so they come from the material/contract path
	// exactly as in the event-driven flow.
	certReqAuthMsg := material.CertReqAuthMsg
	certReqAuthSig := material.CertReqAuthSig
	cpkv := material.CommitteePubKeyVersion
	expiresAt := material.ExpiresAt
	log.Info("TEE host preparing PartialAdapt", "reqID", common.Bytes2Hex(reqID[:]), "epoch", currentEpoch, "myIndex", myIndex, "threshold", threshold, "activeParties", len(activeSet), "materialActiveHash", common.Bytes2Hex(material.ActiveSetHash[:]), "currentActiveHash", common.Bytes2Hex(currentActiveHash[:]), "originalRHash", shortPayloadHash(material.OriginalR), "sourceHash", shortPayloadHash(material.SourceMessage), "targetHash", shortPayloadHash(material.TargetMessage))
	_, err = h.StartPartialAdapt(ctx, PartialAdaptProtocolConfig{
		ReqID:         reqID,
		MyIndex:       myIndex,
		Threshold:     threshold - 1,
		NumParties:    len(activeSet),
		SourceMessage: material.SourceMessage,
		TargetMessage: material.TargetMessage,
		PartialAdaptRequest: PartialAdaptRequest{
			CertReqAuthMsg:         certReqAuthMsg,
			CertReqAuthSig:         certReqAuthSig,
			DReq:                   auth.DReq[:],
			DPolicy:                auth.DPolicy[:],
			CommitteePubKeyVersion: cpkv,
			ExpiresAt:              expiresAt,
			DReqInput:              material.DReqInput,
			TEEAttestationInputs: TEEAttestationInputs{
				CertEAMsg:             nil,
				CertEASig:             nil,
				ActiveTEEList:         material.ActiveTEEList,
				ActiveSetCertInput:    material.ActiveSetCertInput,
				ActiveTEECount:        material.ActiveTEECount,
				ActiveSetCertInputLen: material.ActiveSetCertLength,
			},
		},
	}, network)
	if err != nil {
		return err
	}
	return h.maybeExecutePartialAdapt(ctx, reqID)
}

func (h *Host) waitingForDKRSExtract() bool {
	h.protocolMu.Lock()
	defer h.protocolMu.Unlock()
	return h.dkrsSession != nil &&
		h.dkrsSession.Config.ReceiverIndex > 0 &&
		h.dkrsSession.Extracted == nil
}

func (h *Host) retryPendingRedactions(ctx context.Context) error {
	h.mu.Lock()
	reqIDs := make([][32]byte, 0, len(h.pendingAuth))
	for reqID := range h.pendingAuth {
		if !h.redactionExecutionActiveLocked(reqID) {
			reqIDs = append(reqIDs, reqID)
		}
	}
	h.mu.Unlock()
	for _, reqID := range reqIDs {
		if err := h.tryStartPartialAdapt(ctx, reqID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) handleEraseRequested(ctx context.Context, epoch *big.Int) error {
	if epoch == nil {
		return nil
	}
	if h.shouldDeferErase(epoch) {
		h.deferErase(epoch)
		sigPubKey := h.identity.primaryKey()
		log.Info("TEE host deferred erase until DKRS dealer broadcast completes", "epoch", epoch, "hardwareID", hexKey(h.identity.HardwareID), "sigPubKey", hexKey(sigPubKey))
		return nil
	}
	return h.executeEraseRequested(ctx, epoch)
}

func (h *Host) executeEraseRequested(ctx context.Context, epoch *big.Int) error {
	sigPubKey := h.identity.primaryKey()
	key := epoch.String() + ":" + common.Bytes2Hex(sigPubKey[:])
	h.mu.Lock()
	if _, ok := h.submittedErase[key]; ok {
		h.mu.Unlock()
		return nil
	}
	activeSet := append([][32]byte(nil), h.activeSetByEpoch[epoch.String()]...)
	if len(activeSet) == 0 && h.currentEpoch != nil && h.currentEpoch.Cmp(epoch) == 0 {
		activeSet = append([][32]byte(nil), h.activeSet...)
	}
	myIndex := indexOfSigPubKey(activeSet, sigPubKey)
	if myIndex > 0 {
		h.submittedErase[key] = struct{}{}
	}
	h.mu.Unlock()
	if myIndex == 0 {
		return fmt.Errorf("local TEE sigPubKey %s is missing from active set for erase epoch %s", hexKey(sigPubKey), epoch.String())
	}
	backend, ok := h.tee.(TEEProtocolBackend)
	if !ok {
		return errors.New("tee backend does not implement protocol operations")
	}
	teeInfo, err := h.contract.GetTEEInfo(sigPubKey)
	if err != nil {
		return err
	}
	chainID, err := h.contract.ChainID(ctx)
	if err != nil {
		return err
	}
	result, err := backend.EraseShare(ctx, EraseShareRequest{
		Epoch:           epoch.Uint64(),
		NodeIndex:       myIndex,
		ChainID:         chainID.FillBytes(make([]byte, 32)),
		ContractAddress: common.LeftPadBytes(h.contract.ContractAddress().Bytes(), 32),
		TEEOwner:        common.LeftPadBytes(teeInfo.Owner.Bytes(), 32),
	})
	if err != nil {
		return err
	}
	tx, err := h.contract.SubmitEraseProof(sigPubKey, epoch, result.EraseProofHash, result.Proof)
	if err != nil {
		return err
	}
	log.Info("TEE host submitted erase proof", "epoch", epoch, "tx", tx)
	return nil
}

func (h *Host) rememberExitRotation(oldEpoch *big.Int, newEpoch *big.Int) {
	if oldEpoch == nil || newEpoch == nil {
		return
	}
	h.mu.Lock()
	h.rotationByOldEpoch[oldEpoch.String()] = newEpoch.String()
	h.mu.Unlock()
}

func (h *Host) shouldDeferErase(epoch *big.Int) bool {
	if epoch == nil {
		return false
	}
	key := epoch.String()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rotationByOldEpoch[key]; !ok {
		return false
	}
	if _, ok := h.dkrsDealerDone[key]; ok {
		return false
	}
	activeSet := append([][32]byte(nil), h.activeSetByEpoch[key]...)
	if len(activeSet) == 0 && h.currentEpoch != nil && h.currentEpoch.Cmp(epoch) == 0 {
		activeSet = append([][32]byte(nil), h.activeSet...)
	}
	if len(activeSet) == 0 {
		return true
	}
	return indexOfSigPubKey(activeSet, h.identity.primaryKey()) > 0
}

func (h *Host) deferErase(epoch *big.Int) {
	if epoch == nil {
		return
	}
	h.mu.Lock()
	h.pendingErase[epoch.String()] = &pendingEraseRequest{Epoch: new(big.Int).Set(epoch)}
	h.mu.Unlock()
}

func (h *Host) markDKRSDealerDone(epoch *big.Int) {
	if epoch == nil {
		return
	}
	h.mu.Lock()
	h.dkrsDealerDone[epoch.String()] = struct{}{}
	h.mu.Unlock()
}

func (h *Host) flushPendingErase(ctx context.Context, epoch *big.Int) error {
	if epoch == nil {
		return nil
	}
	h.mu.Lock()
	pending := h.pendingErase[epoch.String()]
	h.mu.Unlock()
	if pending == nil {
		return nil
	}
	if err := h.handleEraseRequested(ctx, pending.Epoch); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.pendingErase, epoch.String())
	h.mu.Unlock()
	return nil
}

func (h *Host) maybeSubmitPkCH(ctx context.Context, result *TEEExtractResult) error {
	if result == nil {
		return nil
	}
	log.Info("TEE host extracted pkCH", "pkCHBytes", len(result.GlobalPK), "sigBytes", len(result.PkCHSignature))
	if len(result.GlobalPK) == 0 {
		log.Warn("TEE host skipping empty pkCH")
		return nil
	}
	pkCH, err := h.contract.GetPkCH()
	if err != nil {
		return err
	}
	if len(pkCH) != 0 {
		return nil
	}
	if len(result.PkCHSignature) == 0 {
		return errors.New("tee extract did not return pkCH signature")
	}
	tx, err := h.contract.SetPkCH(result.GlobalPK, result.PkCHSignature)
	if err != nil {
		return err
	}
	log.Info("TEE host submitted pkCH", "tx", tx)
	return nil
}

func (h *Host) maybeExecutePartialAdapt(ctx context.Context, reqID [32]byte) (err error) {
	joinKey := common.Bytes2Hex(reqID[:])
	span := h.beginEditTiming("aggregation_pi_req_a_rprime", "join_key", joinKey, "tee", hexKey(h.identity.primaryKey()))
	defer func() { span(err) }()
	h.protocolMu.Lock()
	if h.partialAdapt == nil || h.partialAdapt[reqID] == nil || h.partialAdapt[reqID].Aggregate == nil {
		h.protocolMu.Unlock()
		return nil
	}
	cfg := clonePartialAdaptProtocolConfig(h.partialAdapt[reqID].Config)
	aggregate := clonePartialAdaptAggregateMessage(h.partialAdapt[reqID].Aggregate)
	h.protocolMu.Unlock()
	h.mu.Lock()
	auth := h.pendingAuth[reqID]
	if auth == nil {
		h.mu.Unlock()
		return nil
	}
	if h.redactionExecutionActiveLocked(reqID) {
		h.mu.Unlock()
		return nil
	}
	if h.executingRedact == nil {
		h.executingRedact = make(map[[32]byte]struct{})
	}
	h.executingRedact[reqID] = struct{}{}
	h.mu.Unlock()
	executed := false
	defer func() {
		if !executed {
			h.releaseRedactionExecution(reqID)
		}
	}()
	material, ok, err := h.materials.Get(ctx, reqID)
	if err != nil || !ok {
		return err
	}
	log.Info("TEE host evaluating PartialAdapt aggregate", "reqID", common.Bytes2Hex(reqID[:]), "fromIndex", hostIndex(aggregate.FromIndex), "contributions", partialContributionIndices(aggregate.Contributions), "threshold", cfg.Threshold, "numParties", cfg.NumParties, "materialActiveHash", common.Bytes2Hex(material.ActiveSetHash[:]), "originalRHash", shortPayloadHash(material.OriginalR), "sourceHash", shortPayloadHash(material.SourceMessage), "targetHash", shortPayloadHash(material.TargetMessage))
	piReqA, err := func() ([]byte, error) {
		encodeSpan := h.beginEditTiming("encode_pi_req_a", "join_key", joinKey, "tee", hexKey(h.identity.primaryKey()))
		val, err := h.encodePiReqA(ctx, material, aggregate.Contributions)
		encodeSpan(err)
		return val, err
	}()
	if err != nil {
		return err
	}
	rPrime, err := func() ([]byte, error) {
		combineSpan := h.beginEditTiming("redaction_combine_shares_pure", "join_key", joinKey, "tee", hexKey(h.identity.primaryKey()))
		val, err := combinePartialAdaptRPrime(material, cfg, aggregate.Contributions)
		combineSpan(err)
		return val, err
	}()
	if err != nil {
		log.Warn("TEE host failed to combine PartialAdapt RPrime", "reqID", common.Bytes2Hex(reqID[:]), "contributions", partialContributionIndices(aggregate.Contributions), "threshold", cfg.Threshold, "numParties", cfg.NumParties, "err", err)
		return err
	}
	log.Info("TEE host combined PartialAdapt RPrime", "reqID", common.Bytes2Hex(reqID[:]), "rPrimeHash", shortPayloadHash(rPrime), "piReqAHash", shortPayloadHash(piReqA))
	if executor := h.redactionExecutor(); executor != nil {
		if material.BlockNum == nil {
			return fmt.Errorf("redaction material block number is required")
		}
		if len(rPrime) == 0 {
			return fmt.Errorf("redaction material original R is required for automatic execution")
		}
		_, err := executor.ExecuteTEEClusterRedactionFromMaterial(ctx, RedactionExecutionRequest{
			ReqID:      reqID,
			BlockNum:   material.BlockNum,
			Index:      material.Index,
			ModifyType: material.ModifyType,
			DataAfter:  material.DataAfter,
			PiReqA:     piReqA,
			RPrime:     rPrime,
		})
		if err != nil {
			return err
		}
		executed = true
		h.markRedactionClosed(reqID)
		log.Info("TEE host executed redaction through eth executor", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	rPrimeHash := material.RPrimeHash
	if len(rPrime) > 0 {
		rPrimeHash = crypto.Keccak256Hash(rPrime)
	}
	if rPrimeHash == ([32]byte{}) || material.ResultHash == ([32]byte{}) {
		log.Warn("TEE host cannot directly execute redaction without executor or result hash", "reqID", common.Bytes2Hex(reqID[:]))
		return nil
	}
	activeHash := auth.ActiveSetHash
	if activeHash == ([32]byte{}) {
		activeHash = material.ActiveSetHash
	}
	proofHash, err := h.contract.ComputeRedactionProofHash(reqID, auth.DReq, activeHash, piReqA)
	if err != nil {
		return err
	}
	tx, err := h.contract.ExecuteRedaction(reqID, rPrimeHash, proofHash, material.ResultHash, piReqA)
	if err != nil {
		h.emitTiming("edit_execution", "execute_redaction_submit_failed",
			"join_key", common.Bytes2Hex(reqID[:]),
			"tee_host", hexKey(h.identity.primaryKey()),
			"reason", "submit_error",
			"error", err,
		)
		return err
	}
	txBytes, err := hexutil.Decode(tx)
	if err != nil || len(txBytes) != common.HashLength || common.BytesToHash(txBytes) == (common.Hash{}) {
		h.emitTiming("edit_execution", "execute_redaction_submit_failed",
			"join_key", common.Bytes2Hex(reqID[:]),
			"tee_host", hexKey(h.identity.primaryKey()),
			"tx_hash", tx,
			"reason", "invalid_tx_hash",
		)
		return fmt.Errorf("execute redaction returned invalid transaction hash %q", tx)
	}
	txHash := common.BytesToHash(txBytes)
	tx = txHash.Hex()
	h.emitTiming("edit_execution", "execute_redaction_tx_submitted",
		"join_key", common.Bytes2Hex(reqID[:]),
		"tx_hash", tx,
		"tee_host", hexKey(h.identity.primaryKey()),
	)
	receiptCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	receipt, err := h.contract.WaitTransactionReceipt(receiptCtx, txHash)
	cancel()
	if err != nil {
		h.emitTiming("edit_execution", "execute_redaction_receipt_failed",
			"join_key", common.Bytes2Hex(reqID[:]),
			"tx_hash", tx,
			"tee_host", hexKey(h.identity.primaryKey()),
			"reason", "receipt_wait_error",
			"error", err,
		)
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		h.emitTiming("edit_execution", "execute_redaction_receipt_failed",
			"join_key", common.Bytes2Hex(reqID[:]),
			"tx_hash", tx,
			"tee_host", hexKey(h.identity.primaryKey()),
			"receipt_block_number", receiptBlockNumber(receipt),
			"status", receipt.Status,
			"reason", "status_0",
		)
		return fmt.Errorf("execute redaction transaction %s failed in block %d", tx, receiptBlockNumber(receipt))
	}
	h.emitTiming("edit_execution", "execute_redaction_receipt_success",
		"join_key", common.Bytes2Hex(reqID[:]),
		"tx_hash", tx,
		"tee_host", hexKey(h.identity.primaryKey()),
		"receipt_block_number", receiptBlockNumber(receipt),
		"status", receipt.Status,
	)
	executed = true
	h.markRedactionClosed(reqID)
	log.Info("TEE host executed redaction", "reqID", common.Bytes2Hex(reqID[:]), "tx", tx)
	return nil
}

func (h *Host) beginEditTiming(event string, fields ...interface{}) func(error) {
	started := time.Now()
	base := append([]interface{}{"tee", hexKey(h.identity.primaryKey())}, fields...)
	h.emitTiming("edit_execution", event+"_start", base...)
	return func(err error) {
		status, reason := "ok", "completed"
		if err != nil {
			status, reason = "error", "error"
		}
		endFields := append([]interface{}{}, base...)
		endFields = append(endFields, "status", status, "reason", reason, "duration_ns", time.Since(started).Nanoseconds())
		h.emitTiming("edit_execution", event+"_end", endFields...)
	}
}

func (h *Host) emitTiming(stage string, event string, fields ...interface{}) {
	if h.cfg.TimingSink != nil {
		h.cfg.TimingSink(stage, event, fields...)
		return
	}
	contextFields := []interface{}{"stage", stage, "event", event}
	log.Info("E2E_TIMING", append(contextFields, fields...)...)
}

func receiptBlockNumber(receipt *types.Receipt) uint64 {
	if receipt == nil || receipt.BlockNumber == nil {
		return 0
	}
	return receipt.BlockNumber.Uint64()
}

func (h *Host) redactionExecutionActiveLocked(reqID [32]byte) bool {
	if _, done := h.executedRedact[reqID]; done {
		return true
	}
	if _, running := h.executingRedact[reqID]; running {
		return true
	}
	return false
}

func (h *Host) releaseRedactionExecution(reqID [32]byte) {
	h.mu.Lock()
	delete(h.executingRedact, reqID)
	h.mu.Unlock()
}

func (h *Host) redactionExecutor() RedactionExecutor {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.executor
}

func combinePartialAdaptRPrime(material RedactionMaterial, cfg PartialAdaptProtocolConfig, contributions []PartialAdaptContributionMessage) ([]byte, error) {
	if len(material.OriginalR) == 0 {
		return nil, nil
	}
	threshold := cfg.Threshold
	if threshold == 0 && material.Threshold > 0 {
		threshold = material.Threshold - 1
	}
	numParties := cfg.NumParties
	if numParties == 0 {
		numParties = len(material.ActiveHardwareIDs)
	}
	if threshold < 0 || numParties <= 0 {
		return nil, fmt.Errorf("invalid partial adapt combine config: threshold=%d numParties=%d", threshold, numParties)
	}
	shares := make([]*chameleon.NITCHBLSAdaptationShare, 0, len(contributions))
	for _, contribution := range contributions {
		shares = append(shares, &chameleon.NITCHBLSAdaptationShare{
			Index: hostIndex(contribution.Index),
			Share: append([]byte(nil), contribution.Contribution...),
		})
	}
	pp, err := chameleon.NITCHBLSSetup()
	if err != nil {
		return nil, err
	}
	return chameleon.NITCHBLSCombineEnclaveIndexed(pp, shares, threshold, numParties, &chameleon.NITCHBLSHashOutput{
		CheckValue: append([]byte(nil), material.OriginalR...),
	})
}

func (h *Host) encodePiReqA(ctx context.Context, material RedactionMaterial, contributions []PartialAdaptContributionMessage) ([]byte, error) {
	type shareProof struct {
		SigPubKey [32]byte
		SI        []byte
		Sigma     []byte
	}
	shareProofType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "sigPubKey", Type: "bytes32"},
		{Name: "sI", Type: "bytes"},
		{Name: "sigma", Type: "bytes"},
	})
	if err != nil {
		return nil, err
	}
	values := make([]shareProof, 0, len(contributions))
	for _, contribution := range contributions {
		sigPubKey, err := h.sigPubKeyForContribution(ctx, material, hostIndex(contribution.Index))
		if err != nil {
			return nil, err
		}
		values = append(values, shareProof{
			SigPubKey: sigPubKey,
			SI:        append([]byte(nil), contribution.Contribution...),
			Sigma:     append([]byte(nil), contribution.Sigma...),
		})
	}
	args := abi.Arguments{{Type: shareProofType}}
	return args.Pack(values)
}

func (h *Host) sigPubKeyForContribution(ctx context.Context, material RedactionMaterial, index int) ([32]byte, error) {
	if index >= 1 && index <= len(material.ActiveSigPubKeys) {
		return material.ActiveSigPubKeys[index-1], nil
	}
	if index >= 1 && index <= len(material.ActiveHardwareIDs) {
		info, err := h.contract.GetTEEInfo(material.ActiveHardwareIDs[index-1])
		if err != nil {
			return [32]byte{}, err
		}
		return info.SigPubKey, nil
	}
	activeSet := h.activeSetSnapshot()
	if index >= 1 && index <= len(activeSet) {
		info, err := h.contract.GetTEEInfo(activeSet[index-1])
		if err != nil {
			return [32]byte{}, err
		}
		return info.SigPubKey, nil
	}
	return [32]byte{}, fmt.Errorf("missing sig pubkey for contribution index %d", index)
}

func (h *Host) markRedactionClosed(reqID [32]byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.executedRedact[reqID] = struct{}{}
	delete(h.executingRedact, reqID)
	delete(h.pendingAuth, reqID)
	h.protocolMu.Lock()
	if h.partialAdapt != nil {
		delete(h.partialAdapt, reqID)
	}
	h.protocolMu.Unlock()
	_ = h.materials.Delete(context.Background(), reqID)
}

func (h *Host) markLocalInactive() {
	h.mu.Lock()
	h.active = false
	h.mu.Unlock()
	h.protocolMu.Lock()
	h.dkrsSession = nil
	h.partialAdapt = nil
	h.protocolMu.Unlock()
}

func (h *Host) markLocalRevoked() {
	h.mu.Lock()
	h.revoked = true
	h.active = false
	h.mu.Unlock()
	h.protocolMu.Lock()
	h.distKGSession = nil
	h.dkrsSession = nil
	h.partialAdapt = nil
	h.protocolMu.Unlock()
}

func (h *Host) protocolNetwork() ProtocolNetwork {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.network
}

func (h *Host) isActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active && !h.revoked
}

func (h *Host) activeSetSnapshot() [][32]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][32]byte(nil), h.activeSet...)
}

func attestationFromActiveSetInfo(info *contractService.ActiveSetInfo) TEEAttestationInputs {
	count := uint32(len(info.HardwareIDs))
	return TEEAttestationInputs{
		ActiveTEECount: count,
	}
}

func indexOfSigPubKey(sigPubKeys [][32]byte, sigPubKey [32]byte) int {
	for i, candidate := range sigPubKeys {
		if candidate == sigPubKey {
			return i + 1
		}
	}
	return 0
}

func indexOfHardwareID(hardwareIDs [][32]byte, hardwareID [32]byte) int {
	return indexOfSigPubKey(hardwareIDs, hardwareID)
}

func sigPubKeysHash(sigPubKeys [][32]byte) [32]byte {
	flat := make([]byte, 0, len(sigPubKeys)*32)
	for _, sigPubKey := range sigPubKeys {
		flat = append(flat, sigPubKey[:]...)
	}
	return crypto.Keccak256Hash(flat)
}

func hardwareIDsHash(hardwareIDs [][32]byte) [32]byte {
	return sigPubKeysHash(hardwareIDs)
}

func hexKey(key [32]byte) string {
	return common.Bytes2Hex(key[:])
}

func hexKeys(keys [][32]byte, limit int) []string {
	if limit <= 0 || limit > len(keys) {
		limit = len(keys)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, hexKey(keys[i]))
	}
	if limit < len(keys) {
		out = append(out, fmt.Sprintf("...(+%d)", len(keys)-limit))
	}
	return out
}

func epochString(epoch *big.Int) string {
	if epoch == nil {
		return "<nil>"
	}
	return epoch.String()
}

func validatePeerEPK(epk []byte) error {
	if len(epk) != 64 {
		return fmt.Errorf("invalid length: got %d, want 64", len(epk))
	}
	for _, b := range epk {
		if b != 0 {
			return nil
		}
	}
	return errors.New("all zero")
}

func dkrsSessionID(oldEpoch, newEpoch *big.Int, oldActiveSetHash, newActiveSetHash [32]byte, oldParties, newParties, threshold int) [32]byte {
	oldEpochBytes := []byte{}
	if oldEpoch != nil {
		oldEpochBytes = oldEpoch.Bytes()
	}
	newEpochBytes := []byte{}
	if newEpoch != nil {
		newEpochBytes = newEpoch.Bytes()
	}
	return crypto.Keccak256Hash(
		[]byte("teehost-dkrs-session-v1"),
		common.LeftPadBytes(oldEpochBytes, 32),
		common.LeftPadBytes(newEpochBytes, 32),
		oldActiveSetHash[:],
		newActiveSetHash[:],
		common.LeftPadBytes(big.NewInt(int64(oldParties)).Bytes(), 8),
		common.LeftPadBytes(big.NewInt(int64(newParties)).Bytes(), 8),
		common.LeftPadBytes(big.NewInt(int64(threshold)).Bytes(), 8),
	)
}

func bigIntToInt(v *big.Int, fallback int) int {
	if v == nil || !v.IsInt64() {
		return fallback
	}
	return int(v.Int64())
}
