package eth

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func testDistKGMessage(ttl, hop uint64) types.DistKGMessage {
	return types.DistKGMessage{
		Share: &chameleon.DistKGShare{
			FromIndex: big.NewInt(1),
			ToIndex:   big.NewInt(2),
			Share:     big.NewInt(3),
		},
		Relay: types.RelayMetadata{MessageID: "ignored", Origin: "node-a", TTL: ttl, Hop: hop},
	}
}

func TestDistKGFingerprintIgnoresRelayMetadata(t *testing.T) {
	first, err := distKGFingerprint(testDistKGMessage(8, 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := distKGFingerprint(testDistKGMessage(1, 7))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("relay metadata changed fingerprint: %s != %s", first, second)
	}
}

func TestAdvanceRelayIsBounded(t *testing.T) {
	meta := types.RelayMetadata{MessageID: "m", Origin: "o", TTL: 2, Hop: 3}
	next, ok := advanceRelay(meta)
	if !ok || next.TTL != 1 || next.Hop != 4 || next.MessageID != "m" || next.Origin != "o" {
		t.Fatalf("unexpected relay step: ok=%v next=%+v", ok, next)
	}
	next, ok = advanceRelay(types.RelayMetadata{TTL: 0, Hop: 9})
	if ok || next.TTL != 0 || next.Hop != 9 {
		t.Fatalf("TTL exhaustion should not advance: ok=%v next=%+v", ok, next)
	}
}

func TestDistKGSeenCacheDropsDuplicate(t *testing.T) {
	pm := &ProtocolManager{distKGSeen: make(map[common.Hash]time.Time)}
	msg := testDistKGMessage(4, 0)
	_, first := pm.noteDistKGSeen(msg)
	_, second := pm.noteDistKGSeen(msg)
	if !first || second {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

func testRecoveryDeal(dealer, threshold, parties int) *chameleon.DistKGDeal {
	return &chameleon.DistKGDeal{
		DealerIndex: big.NewInt(int64(dealer)),
		Threshold:   big.NewInt(int64(threshold)),
		NumParties:  big.NewInt(int64(parties)),
		A:           []byte{byte(dealer), 1},
		B0:          []byte{byte(dealer), 2},
		G1Bytes:     []byte{3},
		G2Bytes:     []byte{4},
		P:           "05",
	}
}

func TestDistKGInputRequestRLPRoundTrip(t *testing.T) {
	want := types.DistKGMessage{InputRequest: &types.DistKGInputRequest{
		DealerIndex:    big.NewInt(16),
		RequesterIndex: big.NewInt(235),
		NeedDeal:       true,
		Nonce:          7,
		Attempt:        2,
	}}
	encoded, err := rlp.EncodeToBytes(want)
	if err != nil {
		t.Fatal(err)
	}
	var got types.DistKGMessage
	if err := rlp.DecodeBytes(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.InputRequest == nil || got.InputRequest.DealerIndex.Int64() != 16 ||
		got.InputRequest.RequesterIndex.Int64() != 235 || !got.InputRequest.NeedDeal ||
		got.InputRequest.NeedShare || got.InputRequest.Nonce != 7 || got.InputRequest.Attempt != 2 {
		t.Fatalf("unexpected decoded request: %+v", got.InputRequest)
	}
}

func TestDistKGResponseInputsEnforcesBindings(t *testing.T) {
	deal := testRecoveryDeal(3, 1, 4)
	share := &chameleon.DistKGShare{FromIndex: big.NewInt(3), ToIndex: big.NewInt(235), Share: big.NewInt(9)}
	resp := &types.DistKGInputResponse{
		DealerIndex: big.NewInt(3), RequesterIndex: big.NewInt(235), Deal: deal, Share: share,
	}
	gotDeal, gotShare, err := distKGResponseInputs(resp)
	if err != nil || gotDeal != deal || gotShare != share {
		t.Fatalf("valid response rejected: deal=%p share=%p err=%v", gotDeal, gotShare, err)
	}
	resp.Share.ToIndex = big.NewInt(234)
	if _, _, err := distKGResponseInputs(resp); err == nil {
		t.Fatal("expected requester/share binding mismatch")
	}
}

func TestDistKGDealerBuildsReplayFromOriginalCache(t *testing.T) {
	oldManager := globalKeyManager
	globalKeyManager = &KeyManager{nodeIndex: 2, committeeSize: 4}
	defer func() { globalKeyManager = oldManager }()

	deal := testRecoveryDeal(3, 1, 4)
	share := &chameleon.DistKGShare{FromIndex: big.NewInt(3), ToIndex: big.NewInt(2), Share: big.NewInt(11)}
	pm := &ProtocolManager{
		distKGRecoveryDeals:  map[int]*chameleon.DistKGDeal{3: deal},
		distKGOutboundShares: map[int]*chameleon.DistKGShare{2: share},
	}
	msg := pm.buildDistKGInputResponse(&types.DistKGInputRequest{
		DealerIndex: big.NewInt(3), RequesterIndex: big.NewInt(2),
		NeedDeal: true, NeedShare: true, Nonce: 99,
	})
	if msg == nil || msg.InputResponse == nil || msg.InputResponse.Deal == nil || msg.InputResponse.Share == nil {
		t.Fatalf("missing replay response: %+v", msg)
	}
	if msg.InputResponse.Nonce != 99 || msg.InputResponse.Deal == deal || msg.InputResponse.Share == share {
		t.Fatalf("response did not preserve nonce or clone cached inputs: %+v", msg.InputResponse)
	}
	msg.InputResponse.Deal.A[0] = 0xff
	msg.InputResponse.Share.Share.SetInt64(42)
	if deal.A[0] == 0xff || share.Share.Int64() == 42 {
		t.Fatal("response mutation changed replay cache")
	}
}

func TestDistKGMissingRequestsCoverDealAndPrivateShare(t *testing.T) {
	oldManager := globalKeyManager
	globalKeyManager = &KeyManager{
		nodeIndex: 1, committeeSize: 4, isCommitteeMember: true, distKGPending: true,
		distKGDeals: map[int]*chameleon.DistKGDeal{
			1: testRecoveryDeal(1, 1, 4), 2: testRecoveryDeal(2, 1, 4), 4: testRecoveryDeal(4, 1, 4),
		},
		distKGShares: map[int]*big.Int{1: big.NewInt(1), 3: big.NewInt(3), 4: big.NewInt(4)},
	}
	defer func() { globalKeyManager = oldManager }()

	pm := &ProtocolManager{distKGRequested: make(map[string]distKGInputRequestState)}
	requests := pm.distKGMissingInputRequests()
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2: %+v", len(requests), requests)
	}
	byDealer := make(map[int]*types.DistKGInputRequest)
	for i := range requests {
		req := requests[i].InputRequest
		byDealer[int(req.DealerIndex.Int64())] = req
	}
	if req := byDealer[2]; req == nil || req.NeedDeal || !req.NeedShare || req.RequesterIndex.Int64() != 2 {
		t.Fatalf("dealer 2 request mismatch: %+v", req)
	}
	if req := byDealer[3]; req == nil || !req.NeedDeal || req.NeedShare || req.RequesterIndex.Int64() != 2 {
		t.Fatalf("dealer 3 request mismatch: %+v", req)
	}
	if repeated := pm.distKGMissingInputRequests(); len(repeated) != 0 {
		t.Fatalf("retry delay did not suppress immediate requests: %+v", repeated)
	}
}

func TestRecordDistKGDealRejectsSessionConflict(t *testing.T) {
	quit := make(chan struct{})
	close(quit)
	pm := &ProtocolManager{quitSync: quit, distKGRecoveryDeals: make(map[int]*chameleon.DistKGDeal)}
	if err := pm.recordDistKGDeal(testRecoveryDeal(1, 1, 4)); err != nil {
		t.Fatal(err)
	}
	conflict := testRecoveryDeal(2, 2, 4)
	if err := pm.recordDistKGDeal(conflict); err == nil {
		t.Fatal("expected threshold/session conflict")
	}
	pm.wg.Wait()
}
