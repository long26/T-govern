package eth

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func reshareWireRoundTrip(t *testing.T, msg types.ReshareMessage) types.ReshareMessage {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.ReshareMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestReshareWirePreservesB0ForVerification(t *testing.T) {
	keys := testReshareKeys(t)
	deal, share := testDKRSInputForReceiver(t, keys[0], 1, 2, 1, 4)
	messages := []types.ReshareMessage{
		{SessionID: "wire-b0", Deal: deal},
		{SessionID: "wire-b0", InputResponse: &types.ReshareInputResponse{
			ReceiverIndex: big.NewInt(2), DealerIndex: big.NewInt(1),
			HasDeal: true, Deal: deal, HasShare: true, Share: share,
		}},
	}
	for i, msg := range messages {
		decoded := reshareWireRoundTrip(t, msg)
		got := decoded.Deal
		if decoded.InputResponse != nil {
			got = decoded.InputResponse.Deal
		}
		if got == nil || !bytes.Equal(got.B0, deal.B0) || len(got.B0) != 192 {
			t.Fatalf("message %d lost B0 during wire round trip", i)
		}
		valid, err := chameleon.VerifyDKRSShare(keys[0].PP, got, share.Share, 2)
		if err != nil || !valid {
			t.Fatalf("message %d failed verification after wire round trip: valid=%v err=%v", i, valid, err)
		}
	}
}

func TestReshareWireSessionReplaysInputsBeforeStart(t *testing.T) {
	keys := testReshareKeys(t)
	km := testKeyManagerForReshare(keys[1], 1)
	session := "wire-before-local-boundary"
	deal, share := testDKRSInputForReceiver(t, keys[0], 1, 2, 1, 4)
	for _, msg := range []types.ReshareMessage{{SessionID: session, Deal: deal}, {SessionID: session, Share: share}} {
		decoded := reshareWireRoundTrip(t, msg)
		if decoded.SessionID == session {
			t.Fatal("session name was sent instead of a fixed ID")
		}
		if _, err := km.OnReshareMessage(decoded); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := km.StartReshare(session, ReshareSessionConfig{
		OldCommittee: []int{1, 2, 3, 4}, NewCommittee: []int{1, 2, 3, 4}, NewThreshold: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if km.reshareSessionID != session || km.reshareDeals[1] == nil || km.reshareShares[1] == nil {
		t.Fatal("fixed wire ID did not resolve to the original operation and pending inputs")
	}
	if km.resharePendingBuffers[types.ReshareWireSessionName(session)] != nil {
		t.Fatal("left a duplicate pending alias after starting the local session")
	}
	request := &types.ReshareInputRequest{
		DealerIndex: big.NewInt(2), ReceiverIndex: big.NewInt(2), NeedDeal: true, NeedShare: true, Nonce: [32]byte{9},
	}
	wireRequest := reshareWireRoundTrip(t, types.ReshareMessage{SessionID: session, InputRequest: request})
	if !km.ReshareMessageTargetsLocal(wireRequest) {
		t.Fatal("fixed wire ID lost the terminal dealer route")
	}
	response := km.BuildReshareInputResponse(wireRequest.SessionID, request)
	if response == nil || response.SessionID != session || !response.InputResponse.HasDeal || !response.InputResponse.HasShare {
		t.Fatal("recovery could not find the original session's dealer cache")
	}
	wireResponse := reshareWireRoundTrip(t, *response)
	if !km.ReshareMessageTargetsLocal(wireResponse) {
		t.Fatal("fixed wire ID lost the terminal receiver route")
	}
	if _, err := km.OnReshareMessage(wireResponse); err != nil {
		t.Fatal(err)
	}
	before := len(km.reshareDeals)
	other := reshareWireRoundTrip(t, types.ReshareMessage{SessionID: "unrelated-wire-session", Deal: deal})
	if _, err := km.OnReshareMessage(other); err != nil {
		t.Fatal(err)
	}
	if len(km.reshareDeals) != before || km.reshareSessionID != session {
		t.Fatal("another session altered active inputs")
	}
}
