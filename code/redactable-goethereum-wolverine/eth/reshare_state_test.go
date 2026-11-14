package eth

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func testKeyManagerForReshare(key *chameleon.NITCHKey, nodeIndex0 int) *KeyManager {
	return &KeyManager{
		localKey:          key,
		publicParams:      key.PP,
		threshold:         key.Threshold,
		numParties:        key.NumParties,
		committeeSize:     key.NumParties,
		nodeIndex:         nodeIndex0,
		isCommitteeMember: true,
		peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
		pendingProposals:  make(map[string]*ProposalShares),
	}
}

func testDKRSInputForReceiver(t *testing.T, key *chameleon.NITCHKey, dealer, receiver, newThreshold, newN int) (*chameleon.DKRSDeal, *chameleon.DKRSShare) {
	t.Helper()

	deal, priv, err := chameleon.CreateDKRSDeal(key.PP, dealer, key.SecretShare, key.Threshold, newThreshold, newN)
	if err != nil {
		t.Fatalf("CreateDKRSDeal(%d): %v", dealer, err)
	}
	deal.OldNumParties = big.NewInt(int64(key.NumParties))
	share := priv.Shares[receiver]
	if share == nil {
		t.Fatalf("missing generated share dealer=%d receiver=%d", dealer, receiver)
	}
	return deal, &chameleon.DKRSShare{
		FromIndex: big.NewInt(int64(dealer)),
		ToIndex:   big.NewInt(int64(receiver)),
		Share:     new(big.Int).Set(share),
	}
}

func testReshareKeys(t *testing.T) []*chameleon.NITCHKey {
	t.Helper()

	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 4)
	if err != nil {
		t.Fatalf("SimulatedDistKG: %v", err)
	}
	return keys
}

func testExtractReshareKeyManager(t *testing.T, keys []*chameleon.NITCHKey, receiver int, sessionID string, newThreshold int) *KeyManager {
	t.Helper()

	committee := []int{1, 2, 3, 4}
	km := testKeyManagerForReshare(keys[receiver-1], receiver-1)
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: newThreshold}); err != nil {
		t.Fatalf("StartReshare receiver=%d: %v", receiver, err)
	}
	for dealer := 1; dealer <= len(committee); dealer++ {
		if dealer == receiver {
			continue
		}
		deal, share := testDKRSInputForReceiver(t, keys[dealer-1], dealer, receiver, newThreshold, len(committee))
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err != nil {
			t.Fatalf("apply deal receiver=%d dealer=%d: %v", receiver, dealer, err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
			t.Fatalf("apply share receiver=%d dealer=%d: %v", receiver, dealer, err)
		}
	}
	km.ReshareVerifySharesOnly()
	if !km.ReshareCanExtract() {
		t.Fatalf("expected receiver=%d to be extractable", receiver)
	}
	if err := km.ReshareExtractAndApply(); err != nil {
		t.Fatalf("ReshareExtractAndApply receiver=%d: %v", receiver, err)
	}
	return km
}

func TestReshareStartStoresSelfShareAndOutboundCache(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[0], 0)
	committee := []int{1, 2, 3, 4}

	deal, shares, err := km.StartReshare("self-share", ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1})
	if err != nil {
		t.Fatalf("StartReshare: %v", err)
	}
	if deal == nil {
		t.Fatal("expected self deal")
	}
	selfShareMsg := shares[1]
	if selfShareMsg == nil || selfShareMsg.Share == nil {
		t.Fatal("expected share to self in outbound share set")
	}
	stored := km.reshareShares[1]
	if stored == nil {
		t.Fatal("expected self share stored in reshareShares")
	}
	if stored.Cmp(selfShareMsg.Share) != 0 {
		t.Fatalf("stored self share mismatch: have %s want %s", stored, selfShareMsg.Share)
	}
	if stored == selfShareMsg.Share {
		t.Fatal("self share should be deep-copied, not aliased")
	}
	cache := km.reshareOutboundCache["self-share"]
	if cache == nil || cache.deal == nil || cache.shares[1] == nil {
		t.Fatal("expected outbound cache to contain self session deal and shares")
	}
}

func TestReshareStatusExposesThresholdProgress(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare("status-progress", ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	status := km.ReshareStatus()
	checks := map[string]interface{}{
		"pending":           true,
		"extracted":         false,
		"sessionID":         "status-progress",
		"thresholdT":        1,
		"requiredShares":    2,
		"oldCommitteeSize":  4,
		"newCommitteeSize":  4,
		"myNewIndex":        2,
		"dealsRecv":         1,
		"sharesRecv":        1,
		"missingDealCount":  3,
		"missingShareCount": 3,
		"requestedInputs":   0,
	}
	for key, want := range checks {
		if status[key] != want {
			t.Fatalf("status[%s] mismatch: have %v want %v (full status=%v)", key, status[key], want, status)
		}
	}
}

func TestResharePendingReplayAllowsExtractWithoutNewMessage(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[3], 3)
	sessionID := "pending-replay"
	committee := []int{1, 2, 3, 4}

	for dealer := 1; dealer <= 3; dealer++ {
		deal, share := testDKRSInputForReceiver(t, keys[dealer-1], dealer, 4, 1, len(committee))
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err != nil {
			t.Fatalf("buffer deal %d: %v", dealer, err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
			t.Fatalf("buffer share %d: %v", dealer, err)
		}
	}
	if len(km.resharePendingBuffers[sessionID].order) != 6 {
		t.Fatalf("pending buffer size mismatch: have %d want 6", len(km.resharePendingBuffers[sessionID].order))
	}

	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}
	for dealer := 1; dealer <= 4; dealer++ {
		if km.reshareDeals[dealer] == nil {
			t.Fatalf("missing deal after replay for dealer %d", dealer)
		}
		if km.reshareShares[dealer] == nil {
			t.Fatalf("missing share after replay for dealer %d", dealer)
		}
	}
	km.ReshareVerifySharesOnly()
	if !km.ReshareCanExtract() {
		t.Fatal("expected pending replay to make reshare extractable")
	}
	if err := km.ReshareExtractAndApply(); err != nil {
		t.Fatalf("ReshareExtractAndApply: %v", err)
	}
	if !km.reshareExtracted || km.reshareLastSession != sessionID {
		t.Fatalf("extract status mismatch: extracted=%v last=%q", km.reshareExtracted, km.reshareLastSession)
	}
}

func TestReshareWaitsForCommonQualifierAndSupportsAdaptation(t *testing.T) {
	keys := testReshareKeys(t)
	const sessionID = "common-qualifier-adaptation"
	committee := []int{1, 2, 3, 4}
	config := ReshareSessionConfig{
		OldCommittee: committee,
		NewCommittee: committee,
		NewThreshold: 1,
	}

	managers := make([]*KeyManager, len(committee))
	deals := make(map[int]*chameleon.DKRSDeal, len(committee))
	shares := make(map[int]map[int]*chameleon.DKRSShare, len(committee))
	for dealer := 1; dealer <= len(committee); dealer++ {
		km := testKeyManagerForReshare(keys[dealer-1], dealer-1)
		deal, outbound, err := km.StartReshare(sessionID, config)
		if err != nil {
			t.Fatalf("StartReshare dealer=%d: %v", dealer, err)
		}
		managers[dealer-1] = km
		deals[dealer] = deal
		shares[dealer] = outbound
	}

	deliver := func(receiver, dealer int) {
		t.Helper()
		km := managers[receiver-1]
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deals[dealer]}); err != nil {
			t.Fatalf("deliver deal receiver=%d dealer=%d: %v", receiver, dealer, err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: shares[dealer][receiver]}); err != nil {
			t.Fatalf("deliver share receiver=%d dealer=%d: %v", receiver, dealer, err)
		}
	}

	// Receivers 3 and 4 already have their own dealer input. Even after another
	// high-index dealer arrives they must not extract from {3,4}; all receivers
	// must wait for the deterministic old-committee qualifier {1,2}.
	deliver(3, 4)
	deliver(4, 3)
	if managers[2].ReshareCanExtract() || managers[3].ReshareCanExtract() {
		t.Fatal("receiver became extractable from a non-common fallback qualifier")
	}

	for receiver := 1; receiver <= len(committee); receiver++ {
		deliver(receiver, 1)
		deliver(receiver, 2)
		managers[receiver-1].ReshareVerifySharesOnly()
		if !managers[receiver-1].ReshareCanExtract() {
			t.Fatalf("receiver=%d did not become extractable with common qualifier", receiver)
		}
		if err := managers[receiver-1].ReshareExtractAndApply(); err != nil {
			t.Fatalf("ReshareExtractAndApply receiver=%d: %v", receiver, err)
		}
	}

	oldMsg := chameleon.Message("common-qualifier-old")
	newMsg := chameleon.Message("common-qualifier-new")
	r := chameleon.RandomPointG1(managers[0].publicParams)
	hashOutput, err := chameleon.Hash(managers[0].localKey, oldMsg, r)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	adaptationShares := make([]*chameleon.AdaptationShare, 0, 2)
	for i := 0; i < 2; i++ {
		share, err := chameleon.PartialAdapt(managers[i].localKey, oldMsg, newMsg)
		if err != nil {
			t.Fatalf("PartialAdapt receiver=%d: %v", i+1, err)
		}
		adaptationShares = append(adaptationShares, share)
	}
	newR, err := chameleon.Combine(managers[0].publicParams, adaptationShares, 1, len(committee), hashOutput, oldMsg, newMsg)
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if !chameleon.Verify(managers[0].localKey, hashOutput.Hash, newMsg, newR) {
		t.Fatal("adaptation failed after deterministic-qualifier reshare")
	}
}

func TestReshareVerifyKeyUpdatesPeerKeyForAdaptationShares(t *testing.T) {
	keys := testReshareKeys(t)
	sessionID := "verify-key-updates-peer"
	receiver := testExtractReshareKeyManager(t, keys, 1, sessionID, 1)
	peer := testExtractReshareKeyManager(t, keys, 2, sessionID, 1)

	oldPeerKey := &chameleon.NITCHKey{
		PP:              keys[1].PP,
		VerificationKey: keys[1].VerificationKey,
		Index:           keys[1].Index,
	}
	receiver.RegisterPeerKey(peerKeyAddressForIndex(2), oldPeerKey)

	oldMsg := chameleon.Message([]byte("old message"))
	newMsg := chameleon.Message([]byte("new message"))
	r := chameleon.RandomPointG1(receiver.publicParams)
	if r == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	hashOutput, err := chameleon.Hash(receiver.localKey, oldMsg, r)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	share, err := chameleon.PartialAdapt(peer.localKey, oldMsg, newMsg)
	if err != nil {
		t.Fatalf("PartialAdapt: %v", err)
	}
	if err := receiver.AddShare("before-dkrs-vk", share, oldMsg, newMsg, hashOutput); err == nil {
		t.Fatal("expected new peer share to fail against stale DKG peer key")
	}

	vk := chameleon.NewDKRSVerifyKey(peer.localKey)
	if applied, err := receiver.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, VerifyKey: vk}); err != nil {
		t.Fatalf("OnReshareMessage verify key: %v", err)
	} else if !applied {
		t.Fatal("expected post-extract DKRS verify key to be applied")
	}
	if err := receiver.AddShare("after-dkrs-vk", share, oldMsg, newMsg, hashOutput); err != nil {
		t.Fatalf("expected new peer share to verify after DKRS verify key registration: %v", err)
	}
}

func TestReshareBuffersFutureShareBeforeSessionStarts(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[3], 3)
	sessionID := "future-session-new-index"

	_, share := testDKRSInputForReceiver(t, keys[0], 1, 2, 1, 4)
	if applied, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
		t.Fatalf("OnReshareMessage: %v", err)
	} else if applied {
		t.Fatal("future share should not be applied before session starts")
	}
	if buf := km.resharePendingBuffers[sessionID]; buf == nil || len(buf.order) != 1 {
		t.Fatalf("future share must be buffered until its receiver index is known: %+v", buf)
	}
}

func TestReshareDropsNonLocalShareDuringSession(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[3], 3)
	sessionID := "drop-live-nonlocal"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	_, share := testDKRSInputForReceiver(t, keys[0], 1, 2, 1, len(committee))
	if applied, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
		t.Fatalf("OnReshareMessage: %v", err)
	} else if applied {
		t.Fatal("non-local live share should not be applied")
	}
	if km.reshareShares[1] != nil {
		t.Fatal("non-local share should not be stored")
	}
}

func TestReshareMessageTargetsLocalStopsAtDealerAndReceiver(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	sessionID := "target-terminal"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{
		OldCommittee: committee, NewCommittee: committee, NewThreshold: 1,
	}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	request := types.ReshareMessage{SessionID: sessionID, InputRequest: &types.ReshareInputRequest{
		DealerIndex: big.NewInt(2), ReceiverIndex: big.NewInt(4), NeedShare: true,
	}}
	if !km.ReshareMessageTargetsLocal(request) {
		t.Fatal("dealer-targeted request should terminate at the local dealer")
	}
	share := types.ReshareMessage{SessionID: sessionID, Share: &chameleon.DKRSShare{
		FromIndex: big.NewInt(1), ToIndex: big.NewInt(2), Share: big.NewInt(9),
	}}
	if !km.ReshareMessageTargetsLocal(share) {
		t.Fatal("receiver-targeted share should terminate at the local receiver")
	}
	response := types.ReshareMessage{SessionID: sessionID, InputResponse: &types.ReshareInputResponse{
		DealerIndex: big.NewInt(1), ReceiverIndex: big.NewInt(2), Nonce: [32]byte{1}, HasDeal: true,
		Deal: &chameleon.DKRSDeal{DealerIndex: big.NewInt(1)},
	}}
	if !km.ReshareMessageTargetsLocal(response) {
		t.Fatal("receiver-targeted response should terminate at the local receiver")
	}
	share.Share.ToIndex = big.NewInt(3)
	if km.ReshareMessageTargetsLocal(share) {
		t.Fatal("message for another receiver should continue relaying")
	}
	if km.ReshareMessageTargetsLocal(types.ReshareMessage{SessionID: sessionID, Deal: &chameleon.DKRSDeal{}}) {
		t.Fatal("broadcast deal must not be treated as a terminal targeted message")
	}
}

func TestReshareRejectsDealWithWrongSessionParameters(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	sessionID := "reject-bad-deal"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	deal, _ := testDKRSInputForReceiver(t, keys[0], 1, 2, 1, len(committee))
	deal.NewThreshold = big.NewInt(2)
	if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err == nil {
		t.Fatal("expected wrong-threshold deal to be rejected")
	}
	if km.reshareDeals[1] != nil {
		t.Fatal("invalid deal should not be stored")
	}
}

func TestReshareRejectsShareFromOutsideOldCommittee(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	sessionID := "reject-bad-share"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	share := &chameleon.DKRSShare{
		FromIndex: big.NewInt(99),
		ToIndex:   big.NewInt(2),
		Share:     big.NewInt(1),
	}
	if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err == nil {
		t.Fatal("expected outside-committee share to be rejected")
	}
	if km.reshareShares[99] != nil {
		t.Fatal("invalid share should not be stored")
	}
}

func TestReshareVerifyAndComplainFlagsBadShare(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	sessionID := "verify-complain"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}

	for _, dealer := range []int{1, 3, 4} {
		deal, share := testDKRSInputForReceiver(t, keys[dealer-1], dealer, 2, 1, len(committee))
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err != nil {
			t.Fatalf("apply deal %d: %v", dealer, err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
			t.Fatalf("apply share %d: %v", dealer, err)
		}
	}

	km.mu.Lock()
	km.reshareShares[1] = new(big.Int).Add(km.reshareShares[1], big.NewInt(1))
	km.mu.Unlock()

	complaints, err := km.ReshareVerifyAndComplain()
	if err != nil {
		t.Fatalf("ReshareVerifyAndComplain: %v", err)
	}
	for _, complaint := range complaints {
		if complaint.DealerIndex.Cmp(big.NewInt(1)) == 0 && complaint.ComplainantIndex.Cmp(big.NewInt(2)) == 0 {
			return
		}
	}
	t.Fatalf("expected complaint from receiver 2 against dealer 1, got %+v", complaints)
}

func TestReshareMissingInputRequestAndResponse(t *testing.T) {
	keys := testReshareKeys(t)
	sessionID := "missing-input"
	committee := []int{1, 2, 3, 4}

	receiver := testKeyManagerForReshare(keys[2], 2)
	if _, _, err := receiver.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("receiver StartReshare: %v", err)
	}
	for _, dealer := range []int{1, 3} {
		deal, share := testDKRSInputForReceiver(t, keys[dealer-1], dealer, 3, 1, len(committee))
		if _, err := receiver.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err != nil {
			t.Fatalf("apply deal %d: %v", dealer, err)
		}
		if _, err := receiver.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
			t.Fatalf("apply share %d: %v", dealer, err)
		}
	}

	requests := receiver.ReshareMissingInputRequests(sessionID)
	if len(requests) != 1 {
		t.Fatalf("missing request count mismatch: have %d want 1", len(requests))
	}
	req := requests[0].InputRequest
	if req == nil {
		t.Fatal("expected input request")
	}
	if req.DealerIndex.Cmp(big.NewInt(2)) != 0 || req.ReceiverIndex.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("unexpected request indexes: dealer=%v receiver=%v", req.DealerIndex, req.ReceiverIndex)
	}
	if !req.NeedDeal || !req.NeedShare {
		t.Fatalf("expected both deal and share to be requested: needDeal=%v needShare=%v", req.NeedDeal, req.NeedShare)
	}
	if req.Attempt != 1 {
		t.Fatalf("first request attempt mismatch: have %d want 1", req.Attempt)
	}

	dealer := testKeyManagerForReshare(keys[1], 1)
	if _, _, err := dealer.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("dealer StartReshare: %v", err)
	}
	resp := dealer.BuildReshareInputResponse(sessionID, req)
	if resp == nil || resp.InputResponse == nil || resp.InputResponse.Deal == nil || resp.InputResponse.Share == nil {
		t.Fatal("expected cached input response with deal and share")
	}
	if _, err := receiver.OnReshareMessage(*resp); err != nil {
		t.Fatalf("apply input response: %v", err)
	}
	if receiver.reshareDeals[2] == nil || receiver.reshareShares[2] == nil {
		t.Fatal("expected input response to fill missing dealer 2 inputs")
	}
	if requests := receiver.ReshareMissingInputRequests(sessionID); len(requests) != 0 {
		t.Fatalf("expected no further missing-input requests, got %d", len(requests))
	}
}

func TestReshareMissingInputRequestRetriesAfterBackoff(t *testing.T) {
	keys := testReshareKeys(t)
	sessionID := "missing-input-retry"
	committee := []int{1, 2, 3, 4}

	receiver := testKeyManagerForReshare(keys[1], 1)
	if _, _, err := receiver.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("receiver StartReshare: %v", err)
	}
	first := receiver.ReshareMissingInputRequests(sessionID)
	if len(first) == 0 || first[0].InputRequest == nil {
		t.Fatal("expected first missing-input request")
	}
	if retry := receiver.ReshareMissingInputRequests(sessionID); len(retry) != 0 {
		t.Fatalf("immediate retry should be suppressed, got %d requests", len(retry))
	}

	receiver.mu.Lock()
	for key, state := range receiver.reshareRequestedInputs {
		state.lastRequest = time.Now().Add(-reshareInputRetryInitial - time.Second)
		receiver.reshareRequestedInputs[key] = state
		break
	}
	receiver.mu.Unlock()

	second := receiver.ReshareMissingInputRequests(sessionID)
	if len(second) == 0 || second[0].InputRequest == nil {
		t.Fatal("expected retry request after backoff")
	}
	if second[0].InputRequest.Attempt != 2 {
		t.Fatalf("retry attempt mismatch: have %d want 2", second[0].InputRequest.Attempt)
	}
	if second[0].InputRequest.Nonce == first[0].InputRequest.Nonce {
		t.Fatal("retry should use a fresh nonce")
	}
}

func TestReshareSeenCacheDedupesFingerprint(t *testing.T) {
	pm := &ProtocolManager{}
	msg := types.ReshareMessage{
		SessionID: "seen-cache",
		InputRequest: &types.ReshareInputRequest{
			DealerIndex:   big.NewInt(1),
			ReceiverIndex: big.NewInt(2),
			NeedDeal:      true,
			NeedShare:     true,
			Nonce:         [32]byte{7},
		},
	}

	if _, first := pm.noteReshareSeen(msg); !first {
		t.Fatal("first message should be unseen")
	}
	if _, second := pm.noteReshareSeen(msg); second {
		t.Fatal("duplicate message should be seen")
	}
	if len(pm.reshareSeen) != 1 {
		t.Fatalf("seen cache size mismatch: have %d want 1", len(pm.reshareSeen))
	}
}

// TestReshareFingerprintIgnoresRelayTTL 验证 B.3：同一逻辑消息在不同 RelayTTL
// 下指纹必须相等，否则逐跳 TTL 递减会导致去重失效、中继风暴。
func TestReshareFingerprintIgnoresRelayTTL(t *testing.T) {
	base := types.ReshareMessage{
		SessionID: "ttl-fp",
		Deal: &chameleon.DKRSDeal{
			DealerIndex:   big.NewInt(1),
			NewThreshold:  big.NewInt(1),
			NewNumParties: big.NewInt(4),
		},
	}
	fp0, err := reshareFingerprint(base)
	if err != nil {
		t.Fatalf("fingerprint TTL=0: %v", err)
	}
	for _, ttl := range []uint64{1, 2, 64} {
		msg := base
		msg.RelayTTL = ttl
		msg.Relay = types.RelayMetadata{MessageID: "ignored", Origin: "node", TTL: ttl, Hop: ttl}
		fp, err := reshareFingerprint(msg)
		if err != nil {
			t.Fatalf("fingerprint TTL=%d: %v", ttl, err)
		}
		if fp != fp0 {
			t.Fatalf("fingerprint changed with RelayTTL=%d: have %s want %s", ttl, fp, fp0)
		}
	}
}

func TestReshareVerifySharesOnlyNoComplaintAndIdempotency(t *testing.T) {
	keys := testReshareKeys(t)
	const sessionID = "verify-shares-only"
	committee := []int{1, 2, 3, 4}
	config := ReshareSessionConfig{
		OldCommittee: committee,
		NewCommittee: committee,
		NewThreshold: 1,
	}

	receiver := testKeyManagerForReshare(keys[0], 0)
	if _, _, err := receiver.StartReshare(sessionID, config); err != nil {
		t.Fatalf("receiver StartReshare: %v", err)
	}

	for dealer := 2; dealer <= 4; dealer++ {
		dkm := testKeyManagerForReshare(keys[dealer-1], dealer-1)
		deal, shares, err := dkm.StartReshare(sessionID, config)
		if err != nil {
			t.Fatalf("dealer %d StartReshare: %v", dealer, err)
		}
		if _, err := receiver.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: deal}); err != nil {
			t.Fatalf("receiver receive deal %d: %v", dealer, err)
		}
		if _, err := receiver.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: shares[1]}); err != nil {
			t.Fatalf("receiver receive share %d: %v", dealer, err)
		}
	}

	if !receiver.ReshareHasEnoughData() {
		t.Fatal("expected receiver to have enough data")
	}

	// 验证 MarkReshareInputsReady 幂等性
	firstInputs, qualCount, req := receiver.MarkReshareInputsReady(sessionID)
	if !firstInputs || qualCount < 2 || req != 2 {
		t.Fatalf("expected first inputs ready to be true, got %v (qual=%d, req=%d)", firstInputs, qualCount, req)
	}
	secondInputs, _, _ := receiver.MarkReshareInputsReady(sessionID)
	if secondInputs {
		t.Fatal("expected second inputs ready call to be suppressed")
	}

	if receiver.ReshareCanExtract() {
		t.Fatal("receiver became extractable before verification")
	}
	if err := receiver.ReshareExtractAndApply(); err == nil {
		t.Fatal("extraction accepted unverified shares")
	}

	// 执行 ReshareVerifySharesOnly，确认无投诉且记录正确数量
	verified, invalid, parallelism, started := receiver.ReshareVerifySharesOnly()
	if !started {
		t.Fatal("expected verification to start")
	}
	if verified != 2 || invalid != 0 || parallelism < 1 {
		t.Fatalf("unexpected verification results: verified=%d invalid=%d parallelism=%d", verified, invalid, parallelism)
	}
	if len(receiver.reshareComplaints) != 0 {
		t.Fatalf("expected zero complaints, got %d", len(receiver.reshareComplaints))
	}

	// 验证第二次调用不会重复验证
	_, _, _, secondStarted := receiver.ReshareVerifySharesOnly()
	if secondStarted {
		t.Fatal("expected second verify call to be skipped")
	}

	// 验证 MarkReshareQualReady 幂等性
	firstQual, qCount := receiver.MarkReshareQualReady(sessionID)
	if !firstQual || qCount < 2 {
		t.Fatalf("expected first qual ready to be true, got %v (qual=%d)", firstQual, qCount)
	}
	secondQual, _ := receiver.MarkReshareQualReady(sessionID)
	if secondQual {
		t.Fatal("expected second qual ready call to be suppressed")
	}

	// 验证提取能成功完成
	if err := receiver.ReshareExtractAndApply(); err != nil {
		t.Fatalf("ReshareExtractAndApply failed: %v", err)
	}
}

func TestReshareFutureSessionReplaysAtNewReceiverIndex(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[3], 3)
	km.localNodeID = "node4"
	km.activeCommittee = &CommitteeDescriptor{Epoch: 1, Members: []NodeID{"node1", "node2", "node3", "node4"}}
	const sessionID = "new-committee-session"
	deal, share := testDKRSInputForReceiver(t, keys[0], 1, 1, 1, 4)
	_, otherShare := testDKRSInputForReceiver(t, keys[1], 2, 2, 1, 4)
	for _, msg := range []types.ReshareMessage{
		{SessionID: sessionID, Deal: deal},
		{SessionID: sessionID, Share: share},
		{SessionID: sessionID, Share: otherShare},
	} {
		if applied, err := km.OnReshareMessage(msg); err != nil || applied {
			t.Fatalf("early message should be buffered: applied=%v err=%v", applied, err)
		}
	}
	if buf := km.resharePendingBuffers[sessionID]; buf == nil || len(buf.order) != 3 {
		t.Fatalf("early new-index shares were lost: %+v", buf)
	}
	km.SetPendingCommittee(&CommitteeDescriptor{Epoch: 2, Members: []NodeID{"node4", "node1", "node2", "node3"}})
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}
	if km.reshareRole.ReceiverIndex != 1 || km.reshareShares[1] == nil || km.reshareShares[2] != nil {
		t.Fatalf("replay used wrong receiver index: role=%+v shares=%v", km.reshareRole, km.reshareShares)
	}
}

func TestReshareFinishedDealerAnswersSessionInputRequest(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	const sessionID = "completed-dealer-session"
	committee := []int{1, 2, 3, 4}
	if _, _, err := km.StartReshare(sessionID, ReshareSessionConfig{OldCommittee: committee, NewCommittee: committee, NewThreshold: 1}); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}
	km.mu.Lock()
	km.resharePending = false
	km.reshareRole = ReshareRole{}
	km.reshareExtracted = true
	km.reshareLastSession = sessionID
	km.mu.Unlock()
	req := &types.ReshareInputRequest{DealerIndex: big.NewInt(2), ReceiverIndex: big.NewInt(3), NeedDeal: true, NeedShare: true, Nonce: [32]byte{42}}
	msg := types.ReshareMessage{SessionID: sessionID, InputRequest: req}
	if !km.ReshareMessageTargetsLocal(msg) {
		t.Fatal("cached old dealer should remain the terminal recipient")
	}
	resp := km.BuildReshareInputResponse(sessionID, req)
	if resp == nil || resp.InputResponse == nil || resp.InputResponse.Share == nil || resp.InputResponse.Deal == nil {
		t.Fatal("completed dealer did not answer from its session cache")
	}
	wrong := *req
	wrong.DealerIndex = big.NewInt(1)
	if km.BuildReshareInputResponse(sessionID, &wrong) != nil || km.BuildReshareInputResponse("other-session", req) != nil {
		t.Fatal("response must require a cached deal matching both session and dealer")
	}
}
