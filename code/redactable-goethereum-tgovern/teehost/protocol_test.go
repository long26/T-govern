package teehost

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

type fakeProtocolTEEBackend struct {
	fakeTEEBackend
	dealReq          DistKGCreateDealRequest
	verifyCalls      int
	extractReq       DistKGExtractRequest
	dkrsDealReq      DKRSCreateDealRequest
	dkrsVerifyCalls  int
	dkrsExtractReq   DKRSExtractRequest
	extract          TEEExtractResult
	result           TEEDealResult
	dkrsResult       TEEDealResult
	partialReq       PartialAdaptRequest
	partialResult    PartialAdaptResult
	peerEPKs         map[uint64][]byte
	dkrsDealPeerEPKs map[uint64][]byte
}

func (t *fakeProtocolTEEBackend) DistKGCreateDeal(ctx context.Context, req DistKGCreateDealRequest) (TEEDealResult, error) {
	t.dealReq = req
	return t.result, nil
}

func (t *fakeProtocolTEEBackend) DistKGVerifyShare(ctx context.Context, req DistKGVerifyShareRequest) (bool, error) {
	t.verifyCalls++
	return true, nil
}

func (t *fakeProtocolTEEBackend) DistKGExtract(ctx context.Context, req DistKGExtractRequest) (TEEExtractResult, error) {
	t.extractReq = req
	if t.extract.VerificationKey != nil {
		return t.extract, nil
	}
	return TEEExtractResult{VerificationKey: []byte("vk")}, nil
}

func (t *fakeProtocolTEEBackend) DKRSCreateDeal(ctx context.Context, req DKRSCreateDealRequest) (TEEDealResult, error) {
	t.dkrsDealReq = req
	t.dkrsDealPeerEPKs = make(map[uint64][]byte, len(t.peerEPKs))
	for index, epk := range t.peerEPKs {
		t.dkrsDealPeerEPKs[index] = append([]byte(nil), epk...)
	}
	return t.dkrsResult, nil
}

func (t *fakeProtocolTEEBackend) DKRSVerifyShare(ctx context.Context, req DKRSVerifyShareRequest) (bool, error) {
	t.dkrsVerifyCalls++
	return true, nil
}

func (t *fakeProtocolTEEBackend) DKRSExtract(ctx context.Context, req DKRSExtractRequest) (TEEExtractResult, error) {
	t.dkrsExtractReq = req
	if t.extract.VerificationKey != nil {
		return t.extract, nil
	}
	return TEEExtractResult{VerificationKey: []byte("dkrs-vk")}, nil
}

func (t *fakeProtocolTEEBackend) RegisterPeerEPK(ctx context.Context, peerIndex uint64, peerPK []byte) error {
	if t.peerEPKs == nil {
		t.peerEPKs = make(map[uint64][]byte)
	}
	t.peerEPKs[peerIndex] = append([]byte(nil), peerPK...)
	return nil
}

func (t *fakeProtocolTEEBackend) PartialAdapt(ctx context.Context, req PartialAdaptRequest) (PartialAdaptResult, error) {
	t.partialReq = req
	return t.partialResult, nil
}

func (t *fakeProtocolTEEBackend) EraseShare(ctx context.Context, req EraseShareRequest) (EraseShareResult, error) {
	return EraseShareResult{EraseProofHash: hostTestHash("erase-proof"), Proof: make([]byte, 65)}, nil
}

type fakeProtocolNetwork struct {
	deals          []DistKGProtocolMessage
	shares         map[int]DistKGProtocolMessage
	dkrsDeals      []DKRSProtocolMessage
	dkrsSends      map[[32]byte][]DKRSProtocolMessage
	partials       []PartialAdaptProtocolMessage
	materials      []RedactionMaterial
	authorizedMsgs []RedactAuthorizedMessage
}

func (n *fakeProtocolNetwork) BroadcastRedactAuthorized(ctx context.Context, msg RedactAuthorizedMessage) error {
	n.authorizedMsgs = append(n.authorizedMsgs, msg.Clone())
	return nil
}

func (n *fakeProtocolNetwork) BroadcastDistKG(ctx context.Context, msg DistKGProtocolMessage) error {
	n.deals = append(n.deals, msg)
	return nil
}

func (n *fakeProtocolNetwork) SendDistKGShare(ctx context.Context, toIndex int, msg DistKGProtocolMessage) error {
	if n.shares == nil {
		n.shares = make(map[int]DistKGProtocolMessage)
	}
	n.shares[toIndex] = msg
	return nil
}

func (n *fakeProtocolNetwork) BroadcastDKRS(ctx context.Context, msg DKRSProtocolMessage) error {
	n.dkrsDeals = append(n.dkrsDeals, msg)
	return nil
}

func (n *fakeProtocolNetwork) SendDKRS(ctx context.Context, toSigPubKey [32]byte, msg DKRSProtocolMessage) error {
	if n.dkrsSends == nil {
		n.dkrsSends = make(map[[32]byte][]DKRSProtocolMessage)
	}
	n.dkrsSends[toSigPubKey] = append(n.dkrsSends[toSigPubKey], msg)
	return nil
}

func (n *fakeProtocolNetwork) BroadcastPartialAdapt(ctx context.Context, msg PartialAdaptProtocolMessage) error {
	n.partials = append(n.partials, msg)
	return nil
}

func (n *fakeProtocolNetwork) BroadcastRedactMaterial(ctx context.Context, material RedactionMaterial) error {
	n.materials = append(n.materials, material.Clone())
	return nil
}

func (n *fakeProtocolNetwork) dkrsSendCount() int {
	total := 0
	for _, msgs := range n.dkrsSends {
		total += len(msgs)
	}
	return total
}

func activeSetForProtocolTest(prefix string, n int) [][32]byte {
	keys := make([][32]byte, n)
	for i := 1; i <= n; i++ {
		keys[i-1] = hostTestHash(prefix + string(rune('0'+i)))
	}
	return keys
}

func TestProtocolMessageRLPRoundTrip(t *testing.T) {
	distKGMessages := []DistKGProtocolMessage{
		{RelayTTL: 9, Deal: &DistKGDealMessage{DealerIndex: 1, Threshold: 3, NumParties: 4, CommitmentsA: []byte("a"), CommitmentB0: []byte("b0"), G1Bytes: []byte("g1"), G2Bytes: []byte("g2"), PHex: scalarOrderWireHex}},
		{Share: &DistKGShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share")}},
		{Complaint: &DistKGComplaintMessage{ComplainantIndex: 2, DealerIndex: 1}},
		{VerifyKey: &DistKGVerifyKeyMessage{Index: 3, VerificationKey: []byte("vk")}},
	}
	for _, msg := range distKGMessages {
		blob, err := rlp.EncodeToBytes(msg)
		if err != nil {
			t.Fatalf("encode DistKG %#v: %v", msg, err)
		}
		var got DistKGProtocolMessage
		if err := rlp.DecodeBytes(blob, &got); err != nil {
			t.Fatalf("decode DistKG %#v: %v", msg, err)
		}
		if !reflect.DeepEqual(got, msg) {
			t.Fatalf("DistKG round trip = %#v, want %#v", got, msg)
		}
	}

	dkrsMessages := []DKRSProtocolMessage{
		{SessionID: hostTestHash("dkrs-session"), Deal: &DKRSDealMessage{DealerIndex: 1, NewThreshold: 3, NewNumParties: 4, CommitmentsA: []byte("a"), CommitmentB0: []byte("b0")}},
		{SessionID: hostTestHash("dkrs-session"), Share: &DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share")}},
		{SessionID: hostTestHash("dkrs-session"), Complaint: &DKRSComplaintMessage{ComplainantIndex: 2, DealerIndex: 1}},
		{SessionID: hostTestHash("dkrs-session"), VerifyKey: &DKRSVerifyKeyMessage{Index: 3, VerificationKey: []byte("vk")}},
		{SessionID: hostTestHash("dkrs-session"), InputRequest: &DKRSInputRequestMessage{RequesterIndex: 2, DealerIndex: 1, NeedDeal: true, NeedShare: true, Nonce: hostTestHash("nonce"), Attempt: 3}},
		{SessionID: hostTestHash("dkrs-session"), InputResponse: &DKRSInputResponseMessage{RequesterIndex: 2, DealerIndex: 1, Nonce: hostTestHash("nonce"), HasDeal: true, Deal: DKRSDealMessage{DealerIndex: 1, NewThreshold: 3, NewNumParties: 4, CommitmentsA: []byte("a"), CommitmentB0: []byte("b0")}, HasShare: true, Share: DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share")}}},
	}
	for _, msg := range dkrsMessages {
		blob, err := rlp.EncodeToBytes(msg)
		if err != nil {
			t.Fatalf("encode DKRS %#v: %v", msg, err)
		}
		var got DKRSProtocolMessage
		if err := rlp.DecodeBytes(blob, &got); err != nil {
			t.Fatalf("decode DKRS %#v: %v", msg, err)
		}
		if !reflect.DeepEqual(got, msg) {
			t.Fatalf("DKRS round trip = %#v, want %#v", got, msg)
		}
	}

	reqID := hostTestHash("req")
	partialMessages := []PartialAdaptProtocolMessage{
		{RelayTTL: 9, Contribution: &PartialAdaptContributionMessage{ReqID: reqID, Index: 1, Contribution: []byte("c"), ProofChallenge: []byte("pc"), ProofResponse: []byte("pr"), Y: []byte("y"), Sigma: []byte("sig")}},
		{Aggregate: &PartialAdaptAggregateMessage{ReqID: reqID, FromIndex: 2, Contributions: []PartialAdaptContributionMessage{{ReqID: reqID, Index: 1, Contribution: []byte("c"), ProofChallenge: []byte("pc"), ProofResponse: []byte("pr"), Y: []byte("y"), Sigma: []byte("sig")}}, FramedContributions: []byte("framed")}},
	}
	for _, msg := range partialMessages {
		blob, err := rlp.EncodeToBytes(msg)
		if err != nil {
			t.Fatalf("encode partial adapt %#v: %v", msg, err)
		}
		var got PartialAdaptProtocolMessage
		if err := rlp.DecodeBytes(blob, &got); err != nil {
			t.Fatalf("decode partial adapt %#v: %v", msg, err)
		}
		if !reflect.DeepEqual(got, msg) {
			t.Fatalf("partial adapt round trip = %#v, want %#v", got, msg)
		}
	}
}

func TestRedactionMaterialRLPRoundTrip(t *testing.T) {
	material := RedactionMaterial{
		RelayTTL:               9,
		ReqID:                  hostTestHash("req"),
		Epoch:                  big.NewInt(7),
		BlockNum:               big.NewInt(42),
		Index:                  1,
		ModifyType:             1,
		DataAfter:              []byte("after"),
		Meta:                   []byte("meta"),
		SourceMessage:          []byte("source"),
		TargetMessage:          []byte("target"),
		OriginalR:              []byte("r"),
		DReqInput:              []byte("dreq-input"),
		CertReqAuthMsg:         []byte("auth-msg"),
		CertReqAuthSig:         []byte("auth-sig"),
		DReq:                   hostTestHash("dreq"),
		DPolicy:                hostTestHash("dpolicy"),
		CommitteePubKeyVersion: []byte{1},
		ExpiresAt:              []byte{2},
		ActiveSetHash:          hostTestHash("active-set"),
		ActiveTEEList:          []byte("tee-list"),
		ActiveHardwareIDs:      [][32]byte{hostTestHash("hardware-1"), hostTestHash("hardware-2")},
		ActiveSigPubKeys:       [][32]byte{hostTestHash("sig-1"), hostTestHash("sig-2")},
		ActiveSetCertInput:     []byte("cert-input"),
		ActiveTEECount:         2,
		ActiveSetCertLength:    11,
		Threshold:              3,
		RPrimeHash:             hostTestHash("rprime"),
		ResultHash:             hostTestHash("result"),
	}
	material.BodyHashes[1] = hostTestHash("body-1")
	material.BodyHashes[5] = material.ActiveSetHash

	blob, err := rlp.EncodeToBytes(material)
	if err != nil {
		t.Fatalf("encode material: %v", err)
	}
	var got RedactionMaterial
	if err := rlp.DecodeBytes(blob, &got); err != nil {
		t.Fatalf("decode material: %v", err)
	}
	if !reflect.DeepEqual(got, material) {
		t.Fatalf("material round trip = %#v, want %#v", got, material)
	}
}

func TestStartDistKGDistributesTEEFramedShares(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		result: TEEDealResult{
			CommitmentsA:     []byte("commitments"),
			CommitmentB0:     []byte("b0"),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("share-1"), []byte{0x01, 0x02}, []byte("share-3")}),
			G1Bytes:          []byte("g1"),
			G2Bytes:          []byte("g2"),
			PHex:             "11",
		},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	state, err := host.StartDistKG(context.Background(), DistKGProtocolConfig{MyIndex: 2, Threshold: 1, NumParties: 3}, network)
	if err != nil {
		t.Fatalf("start distkg: %v", err)
	}
	if state.MyDeal == nil || state.MyDeal.DealerIndex != 2 {
		t.Fatalf("unexpected state deal: %#v", state.MyDeal)
	}
	if len(network.deals) != 1 || network.deals[0].Deal == nil {
		t.Fatalf("deal broadcasts = %d, want 1", len(network.deals))
	}
	if tee.dealReq.DealerIndex != 2 || tee.dealReq.Threshold != 1 || tee.dealReq.NumParties != 3 {
		t.Fatalf("unexpected tee request: %#v", tee.dealReq)
	}
	wantShares := map[int][]byte{
		1: []byte("share-1"),
		2: []byte{0x01, 0x02},
		3: []byte("share-3"),
	}
	for to, want := range wantShares {
		got := network.shares[to]
		if got.Share == nil {
			t.Fatalf("missing share to %d", to)
		}
		if got.Share.FromIndex != 2 || got.Share.ToIndex != uint64(to) {
			t.Fatalf("unexpected share indexes to %d: %#v", to, got.Share)
		}
		if !reflect.DeepEqual(got.Share.Payload, want) {
			t.Fatalf("share payload to %d = %x, want %x", to, got.Share.Payload, want)
		}
	}
}

func TestHandleDistKGVerifiesExtractsAndBroadcastsVerifyKey(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		result: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("self"), []byte("to-2"), []byte("to-3")}),
			G1Bytes:          []byte("g1"),
			G2Bytes:          []byte("g2"),
			PHex:             "11",
		},
		extract: TEEExtractResult{VerificationKey: []byte("local-vk")},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	if _, err := host.StartDistKG(context.Background(), DistKGProtocolConfig{MyIndex: 1, Threshold: 1, NumParties: 3}, network); err != nil {
		t.Fatalf("start distkg: %v", err)
	}
	for i := 2; i <= 3; i++ {
		msg := DistKGProtocolMessage{
			Deal: &DistKGDealMessage{
				DealerIndex:  uint64(i),
				Threshold:    1,
				NumParties:   3,
				CommitmentsA: bytes.Repeat([]byte{byte(i)}, 96),
				CommitmentB0: bytes.Repeat([]byte{byte(i + 10)}, 192),
			},
		}
		if err := host.HandleDistKG(context.Background(), msg, network); err != nil {
			t.Fatalf("handle deal %d: %v", i, err)
		}
		share := DistKGProtocolMessage{Share: &DistKGShareMessage{FromIndex: uint64(i), ToIndex: 1, Payload: []byte{byte(i), 0xaa}}}
		if err := host.HandleDistKG(context.Background(), share, network); err != nil {
			t.Fatalf("handle share %d: %v", i, err)
		}
	}
	if tee.verifyCalls != 3 {
		t.Fatalf("verify calls = %d, want 3", tee.verifyCalls)
	}
	if got, want := tee.extractReq.QualIndices, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("qual = %v, want %v", got, want)
	}
	if len(tee.extractReq.SharesData) == 0 {
		t.Fatal("extract request missing framed shares")
	}
	var verifyBroadcast *DistKGVerifyKeyMessage
	for _, msg := range network.deals {
		if msg.VerifyKey != nil {
			verifyBroadcast = msg.VerifyKey
		}
	}
	if verifyBroadcast == nil {
		t.Fatal("verify key was not broadcast")
	}
	if verifyBroadcast.Index != 1 || !reflect.DeepEqual(verifyBroadcast.VerificationKey, []byte("local-vk")) {
		t.Fatalf("unexpected verify key broadcast: %#v", verifyBroadcast)
	}
}

func TestStartDKRSDistributesTEEFramedShares(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		dkrsResult: TEEDealResult{
			CommitmentsA:     []byte("dkrs-commitments"),
			CommitmentB0:     []byte("dkrs-b0"),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("new-1"), []byte("new-2"), []byte("new-3")}),
			G1Bytes:          []byte("g1"),
			G2Bytes:          []byte("g2"),
			PHex:             "11",
		},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	sessionID := hostTestHash("dkrs-distribute-session")
	newActiveSet := activeSetForProtocolTest("new", 3)
	state, err := host.StartDKRS(context.Background(), DKRSProtocolConfig{
		DealerIndex:   2,
		ReceiverIndex: 3,
		SessionID:     sessionID,
		NewThreshold:  1,
		NewNumParties: 3,
		OldQual:       []int{1, 2, 3},
		NewActiveSet:  newActiveSet,
	}, network)
	if err != nil {
		t.Fatalf("start dkrs: %v", err)
	}
	if state.MyDeal == nil || state.MyDeal.DealerIndex != 2 {
		t.Fatalf("unexpected state deal: %#v", state.MyDeal)
	}
	if len(network.dkrsDeals) != 1 || network.dkrsDeals[0].Deal == nil {
		t.Fatalf("DKRS deal broadcasts = %d, want 1", len(network.dkrsDeals))
	}
	if network.dkrsDeals[0].SessionID != sessionID {
		t.Fatalf("DKRS deal session = %x, want %x", network.dkrsDeals[0].SessionID, sessionID)
	}
	if tee.dkrsDealReq.DealerIndex != 2 || tee.dkrsDealReq.NewThreshold != 1 || tee.dkrsDealReq.NewNumParties != 3 {
		t.Fatalf("unexpected DKRS tee request: %#v", tee.dkrsDealReq)
	}
	wantShares := map[int][]byte{
		1: []byte("new-1"),
		2: []byte("new-2"),
		3: []byte("new-3"),
	}
	for to, want := range wantShares {
		if to == 3 {
			got := state.Shares[2]
			if got == nil || !reflect.DeepEqual(got.Payload, want) {
				t.Fatalf("self DKRS share payload = %#v, want %x", got, want)
			}
			continue
		}
		sent := network.dkrsSends[newActiveSet[to-1]]
		if len(sent) != 1 {
			t.Fatalf("DKRS sends to %d = %d, want 1", to, len(sent))
		}
		got := sent[0]
		if got.Share == nil {
			t.Fatalf("missing DKRS share to %d", to)
		}
		if got.SessionID != sessionID {
			t.Fatalf("DKRS share session to %d = %x, want %x", to, got.SessionID, sessionID)
		}
		if got.Share.FromIndex != 2 || got.Share.ToIndex != uint64(to) {
			t.Fatalf("unexpected DKRS share indexes to %d: %#v", to, got.Share)
		}
		if !reflect.DeepEqual(got.Share.Payload, want) {
			t.Fatalf("DKRS share payload to %d = %x, want %x", to, got.Share.Payload, want)
		}
	}
}

func TestHandleDKRSVerifiesExtractsAndBroadcastsVerifyKey(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("new-1"), []byte("self"), []byte("new-3")}),
			G1Bytes:          []byte("g1"),
			G2Bytes:          []byte("g2"),
			PHex:             "11",
		},
		extract: TEEExtractResult{VerificationKey: []byte("reshare-vk")},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	cfg := DKRSProtocolConfig{
		DealerIndex:   1,
		ReceiverIndex: 2,
		SessionID:     hostTestHash("dkrs-session"),
		NewThreshold:  1,
		NewNumParties: 3,
		OldQual:       []int{1, 2, 3},
	}
	if _, err := host.StartDKRS(context.Background(), cfg, network); err != nil {
		t.Fatalf("start dkrs: %v", err)
	}
	// The complaint-broadcast marker must not allow extraction to bypass share verification.
	host.dkrsSession.ComplaintsSent = true
	for i := 2; i <= 3; i++ {
		msg := DKRSProtocolMessage{
			SessionID: cfg.SessionID,
			Deal: &DKRSDealMessage{
				DealerIndex:   uint64(i),
				OldNumParties: 3,
				NewThreshold:  1,
				NewNumParties: 3,
				CommitmentsA:  bytes.Repeat([]byte{byte(i)}, 96),
				CommitmentB0:  bytes.Repeat([]byte{byte(i + 10)}, 192),
			},
		}
		if err := host.HandleDKRS(context.Background(), msg, network); err != nil {
			t.Fatalf("handle DKRS deal %d: %v", i, err)
		}
		share := DKRSProtocolMessage{SessionID: cfg.SessionID, Share: &DKRSShareMessage{FromIndex: uint64(i), ToIndex: 2, Payload: []byte{byte(i), 0xbb}}}
		if err := host.HandleDKRS(context.Background(), share, network); err != nil {
			t.Fatalf("handle DKRS share %d: %v", i, err)
		}
	}
	if tee.dkrsVerifyCalls != 3 {
		t.Fatalf("DKRS verify calls = %d, want 3", tee.dkrsVerifyCalls)
	}
	if !host.dkrsSession.SharesVerified {
		t.Fatal("DKRS extraction completed without marking shares verified")
	}
	if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want %v", got, want)
	}
	if tee.dkrsExtractReq.MyIndex != 2 {
		t.Fatalf("DKRS extract MyIndex = %d, want 2", tee.dkrsExtractReq.MyIndex)
	}
	if len(tee.dkrsExtractReq.SharesData) == 0 {
		t.Fatal("DKRS extract request missing framed shares")
	}
	var verifyBroadcast *DKRSVerifyKeyMessage
	for _, msg := range network.dkrsDeals {
		if msg.VerifyKey != nil {
			verifyBroadcast = msg.VerifyKey
		}
	}
	if verifyBroadcast == nil {
		t.Fatal("DKRS verify key was not broadcast")
	}
	if verifyBroadcast.Index != 2 || !reflect.DeepEqual(verifyBroadcast.VerificationKey, []byte("reshare-vk")) {
		t.Fatalf("unexpected DKRS verify key broadcast: %#v", verifyBroadcast)
	}
}

func TestDKRSInputFetchRequestsOnlyMissingDealer(t *testing.T) {
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
	}, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	sessionID := hostTestHash("dkrs-fetch-session")
	oldSet := activeSetForProtocolTest("old", 3)
	newSet := activeSetForProtocolTest("new", 3)
	host.dkrsSession = &DKRSSessionState{
		Config: DKRSProtocolConfig{
			ReceiverIndex: 2,
			SessionID:     sessionID,
			NewThreshold:  1,
			NewNumParties: 3,
			OldQual:       []int{1, 2, 3},
			OldActiveSet:  oldSet,
			NewActiveSet:  newSet,
		},
		Deals: map[int]*DKRSDealMessage{
			1: &DKRSDealMessage{DealerIndex: 1, NewThreshold: 1, NewNumParties: 3},
			3: &DKRSDealMessage{DealerIndex: 3, NewThreshold: 1, NewNumParties: 3},
		},
		Shares: map[int]*DKRSShareMessage{
			1: &DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share-1")},
			3: &DKRSShareMessage{FromIndex: 3, ToIndex: 2, Payload: []byte("share-3")},
		},
		InvalidShares: make(map[int]struct{}),
	}
	network := &fakeProtocolNetwork{}
	done, err := host.requestMissingDKRSInputs(context.Background(), sessionID, 1, network)
	if err != nil {
		t.Fatalf("request missing DKRS inputs: %v", err)
	}
	if done {
		t.Fatal("fetch unexpectedly considered the incomplete session done")
	}
	sent := network.dkrsSends[oldSet[1]]
	if len(sent) != 1 || sent[0].InputRequest == nil {
		t.Fatalf("request to dealer 2 = %#v, want one input request", sent)
	}
	req := sent[0].InputRequest
	if req.RequesterIndex != 2 || req.DealerIndex != 2 || !req.NeedDeal || !req.NeedShare {
		t.Fatalf("unexpected input request: %#v", req)
	}
	if network.dkrsSendCount() != 1 {
		t.Fatalf("DKRS direct sends = %d, want 1", network.dkrsSendCount())
	}
}

func TestHandleDKRSInputRequestRespondsFromDealerCache(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("dealer"), VRFPubKey: hostTestHash("vrf")}},
		dkrsResult: TEEDealResult{
			CommitmentsA:     []byte("dkrs-commitments"),
			CommitmentB0:     []byte("dkrs-b0"),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	sessionID := hostTestHash("dkrs-cache-session")
	oldSet := activeSetForProtocolTest("old", 3)
	newSet := activeSetForProtocolTest("new", 3)
	network := &fakeProtocolNetwork{}
	if _, err := host.StartDKRS(context.Background(), DKRSProtocolConfig{
		DealerIndex:   1,
		ReceiverIndex: 0,
		SessionID:     sessionID,
		NewThreshold:  1,
		NewNumParties: 3,
		OldQual:       []int{1, 2, 3},
		OldActiveSet:  oldSet,
		NewActiveSet:  newSet,
	}, network); err != nil {
		t.Fatalf("start dealer-only DKRS: %v", err)
	}
	req := DKRSProtocolMessage{
		SessionID: sessionID,
		InputRequest: &DKRSInputRequestMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			NeedDeal:       true,
			NeedShare:      true,
			Nonce:          hostTestHash("nonce"),
			Attempt:        1,
		},
	}
	if err := host.HandleDKRS(context.Background(), req, network); err != nil {
		t.Fatalf("handle input request: %v", err)
	}
	sent := network.dkrsSends[newSet[1]]
	if len(sent) == 0 {
		t.Fatal("missing input response to requester 2")
	}
	resp := sent[len(sent)-1].InputResponse
	if resp == nil || !resp.HasDeal || !resp.HasShare {
		t.Fatalf("unexpected input response: %#v", resp)
	}
	if !reflect.DeepEqual(resp.Share.Payload, []byte("to-new-2")) {
		t.Fatalf("response share payload = %q, want to-new-2", resp.Share.Payload)
	}
}

func TestHandleDKRSInputResponseFeedsExtraction(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("receiver"), VRFPubKey: hostTestHash("vrf")}},
		extract:        TEEExtractResult{VerificationKey: []byte("response-vk")},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	sessionID := hostTestHash("dkrs-response-session")
	oldSet := activeSetForProtocolTest("old", 2)
	newSet := activeSetForProtocolTest("new", 2)
	network := &fakeProtocolNetwork{}
	cfg := DKRSProtocolConfig{
		DealerIndex:   0,
		ReceiverIndex: 2,
		SessionID:     sessionID,
		NewThreshold:  1,
		NewNumParties: 2,
		OldQual:       []int{1, 2},
		OldActiveSet:  oldSet,
		NewActiveSet:  newSet,
	}
	state, err := host.StartDKRS(context.Background(), cfg, network)
	if err != nil {
		t.Fatalf("start receiver-only DKRS: %v", err)
	}
	deal1 := &DKRSDealMessage{DealerIndex: 1, OldNumParties: 2, NewThreshold: 1, NewNumParties: 2, CommitmentsA: bytes.Repeat([]byte{0x11}, 96), CommitmentB0: bytes.Repeat([]byte{0x21}, 192)}
	share1 := &DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share-1")}
	if err := host.HandleDKRS(context.Background(), DKRSProtocolMessage{SessionID: sessionID, Deal: deal1}, network); err != nil {
		t.Fatalf("handle deal 1: %v", err)
	}
	if err := host.HandleDKRS(context.Background(), DKRSProtocolMessage{SessionID: sessionID, Share: share1}, network); err != nil {
		t.Fatalf("handle share 1: %v", err)
	}
	if state.Extracted != nil {
		t.Fatal("DKRS extracted before input response completed the missing dealer")
	}
	deal2 := DKRSDealMessage{DealerIndex: 2, OldNumParties: 2, NewThreshold: 1, NewNumParties: 2, CommitmentsA: bytes.Repeat([]byte{0x12}, 96), CommitmentB0: bytes.Repeat([]byte{0x22}, 192)}
	share2 := DKRSShareMessage{FromIndex: 2, ToIndex: 2, Payload: []byte("share-2")}
	resp := &DKRSInputResponseMessage{
		RequesterIndex: 2,
		DealerIndex:    2,
		Nonce:          hostTestHash("nonce"),
		HasDeal:        true,
		Deal:           deal2,
		HasShare:       true,
		Share:          share2,
	}
	if err := host.HandleDKRS(context.Background(), DKRSProtocolMessage{SessionID: sessionID, InputResponse: resp}, network); err != nil {
		t.Fatalf("handle input response: %v", err)
	}
	if state.Extracted == nil {
		t.Fatal("DKRS did not extract after input response completed the inputs")
	}
	if tee.dkrsVerifyCalls != 2 {
		t.Fatalf("DKRS verify calls = %d, want 2", tee.dkrsVerifyCalls)
	}
	if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want %v", got, want)
	}
}

func TestHandleDKRSPendingMessagesAreSessionScoped(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		extract:        TEEExtractResult{VerificationKey: []byte("reshare-vk")},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	oldSession := hostTestHash("old-dkrs-session")
	newSession := hostTestHash("new-dkrs-session")
	for _, sessionID := range [][32]byte{oldSession, newSession} {
		deal := DKRSProtocolMessage{SessionID: sessionID, Deal: &DKRSDealMessage{
			DealerIndex:   1,
			OldNumParties: 1,
			NewThreshold:  0,
			NewNumParties: 1,
			CommitmentsA:  bytes.Repeat([]byte{sessionID[0]}, 96),
			CommitmentB0:  bytes.Repeat([]byte{sessionID[1]}, 192),
		}}
		if err := host.HandleDKRS(context.Background(), deal, network); err != nil {
			t.Fatalf("queue deal for session %x: %v", sessionID, err)
		}
		share := DKRSProtocolMessage{SessionID: sessionID, Share: &DKRSShareMessage{FromIndex: 1, ToIndex: 1, Payload: []byte{sessionID[2], 0xcc}}}
		if err := host.HandleDKRS(context.Background(), share, network); err != nil {
			t.Fatalf("queue share for session %x: %v", sessionID, err)
		}
	}
	if _, err := host.StartDKRS(context.Background(), DKRSProtocolConfig{
		ReceiverIndex: 1,
		SessionID:     newSession,
		NewThreshold:  0,
		NewNumParties: 1,
		OldQual:       []int{1},
	}, network); err != nil {
		t.Fatalf("start dkrs: %v", err)
	}
	if err := host.handlePendingDKRS(context.Background(), network); err != nil {
		t.Fatalf("handle pending DKRS: %v", err)
	}
	if tee.dkrsExtractReq.MyIndex != 1 {
		t.Fatalf("DKRS did not extract from matching pending session: %#v", tee.dkrsExtractReq)
	}
	if len(host.pendingDKRS[oldSession]) != 2 {
		t.Fatalf("old session pending messages were consumed: %d", len(host.pendingDKRS[oldSession]))
	}
	if len(host.pendingDKRS[newSession]) != 0 {
		t.Fatalf("new session pending messages remain: %d", len(host.pendingDKRS[newSession]))
	}
}

func TestHandleDKRSQueuesFutureSessionAfterCurrentExtracted(t *testing.T) {
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		extract:        TEEExtractResult{VerificationKey: []byte("reshare-vk")},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	oldSession := hostTestHash("old-extracted-dkrs-session")
	newSession := hostTestHash("future-dkrs-session")
	host.dkrsSession = &DKRSSessionState{
		Config:    DKRSProtocolConfig{ReceiverIndex: 1, SessionID: oldSession, NewThreshold: 0, NewNumParties: 1, OldQual: []int{1}},
		Deals:     make(map[int]*DKRSDealMessage),
		Shares:    make(map[int]*DKRSShareMessage),
		Extracted: &TEEExtractResult{VerificationKey: []byte("old-vk")},
	}
	network := &fakeProtocolNetwork{}
	deal := DKRSProtocolMessage{SessionID: newSession, Deal: &DKRSDealMessage{
		DealerIndex:   1,
		OldNumParties: 1,
		NewThreshold:  0,
		NewNumParties: 1,
		CommitmentsA:  bytes.Repeat([]byte{0x11}, 96),
		CommitmentB0:  bytes.Repeat([]byte{0x22}, 192),
	}}
	if err := host.HandleDKRS(context.Background(), deal, network); err != nil {
		t.Fatalf("queue future deal: %v", err)
	}
	share := DKRSProtocolMessage{SessionID: newSession, Share: &DKRSShareMessage{FromIndex: 1, ToIndex: 1, Payload: []byte{0x33, 0xcc}}}
	if err := host.HandleDKRS(context.Background(), share, network); err != nil {
		t.Fatalf("queue future share: %v", err)
	}
	if len(host.pendingDKRS[newSession]) != 2 {
		t.Fatalf("future session pending messages = %d, want 2", len(host.pendingDKRS[newSession]))
	}
	if _, err := host.StartDKRS(context.Background(), DKRSProtocolConfig{
		ReceiverIndex: 1,
		SessionID:     newSession,
		NewThreshold:  0,
		NewNumParties: 1,
		OldQual:       []int{1},
	}, network); err != nil {
		t.Fatalf("start future dkrs: %v", err)
	}
	if err := host.handlePendingDKRS(context.Background(), network); err != nil {
		t.Fatalf("handle future pending DKRS: %v", err)
	}
	if tee.dkrsExtractReq.MyIndex != 1 {
		t.Fatalf("future DKRS did not extract after pending replay: %#v", tee.dkrsExtractReq)
	}
}

func TestDKRSQualUsesLocalInvalidSharesAndComplaintThreshold(t *testing.T) {
	state := &DKRSSessionState{
		Config: DKRSProtocolConfig{
			NewThreshold: 2,
			OldQual:      []int{1, 2, 3, 4},
		},
		InvalidShares: map[int]struct{}{4: {}},
		Complaints: []DKRSComplaintMessage{
			{ComplainantIndex: 1, DealerIndex: 2},
			{ComplainantIndex: 1, DealerIndex: 3},
			{ComplainantIndex: 2, DealerIndex: 3},
			{ComplainantIndex: 3, DealerIndex: 3},
		},
	}
	if got, want := dkrsQual(state), []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want %v", got, want)
	}
}

func TestDKRSQualTruncatesToFixedPrefix(t *testing.T) {
	state := &DKRSSessionState{
		Config: DKRSProtocolConfig{
			NewThreshold: 2, // required prefix = NewThreshold+2 = 4 dealers
			OldQual:      []int{1, 2, 3, 4, 5, 6},
		},
		InvalidShares: make(map[int]struct{}),
	}
	// Honest case: the fixed prefix {1,2,3,4}; later OldQual members are cut.
	if got, want := dkrsQual(state), []int{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want fixed prefix %v", got, want)
	}
	// A locally invalid share is skipped while scanning, so the prefix window
	// slides forward: {1,3,4,5}.
	state.InvalidShares[2] = struct{}{}
	if got, want := dkrsQual(state), []int{1, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want %v", got, want)
	}
}

func TestPartialAdaptBroadcastsContributionAndCombinesLocally(t *testing.T) {
	var reqID [32]byte
	copy(reqID[:], []byte("partial-adapt-request"))
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}},
		partialResult: PartialAdaptResult{
			Contribution:   []byte("local-contribution"),
			ProofChallenge: []byte("challenge"),
			ProofResponse:  []byte("response"),
			Y:              []byte("y"),
			Sigma:          []byte("sigma"),
		},
	}
	host, err := NewHost(&fakeContractBackend{chainID: oneForProtocolTest()}, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	network := &fakeProtocolNetwork{}
	cfg := PartialAdaptProtocolConfig{
		ReqID:         reqID,
		MyIndex:       1,
		Threshold:     1,
		NumParties:    3,
		SourceMessage: []byte("old-tx"),
		TargetMessage: []byte("new-tx"),
		PartialAdaptRequest: PartialAdaptRequest{
			DReq: []byte("dreq"),
		},
	}
	state, err := host.StartPartialAdapt(context.Background(), cfg, network)
	if err != nil {
		t.Fatalf("start partial adapt: %v", err)
	}
	if state.LocalResult == nil || !reflect.DeepEqual(state.LocalResult.Contribution, []byte("local-contribution")) {
		t.Fatalf("unexpected local result: %#v", state.LocalResult)
	}
	if !reflect.DeepEqual(tee.partialReq.SourceMessage, []byte("old-tx")) || !reflect.DeepEqual(tee.partialReq.TargetMessage, []byte("new-tx")) {
		t.Fatalf("unexpected partial adapt request messages: %#v", tee.partialReq)
	}
	if len(network.partials) != 1 || network.partials[0].Contribution == nil {
		t.Fatalf("partial contribution broadcasts = %d, want 1", len(network.partials))
	}
	remote := PartialAdaptProtocolMessage{Contribution: &PartialAdaptContributionMessage{
		ReqID:          reqID,
		Index:          2,
		Contribution:   []byte("remote-contribution"),
		ProofChallenge: []byte("remote-challenge"),
		ProofResponse:  []byte("remote-response"),
		Y:              []byte("remote-y"),
		Sigma:          []byte("remote-sigma"),
	}}
	if err := host.HandlePartialAdapt(context.Background(), remote, network); err != nil {
		t.Fatalf("handle partial adapt: %v", err)
	}
	if len(network.partials) != 1 || network.partials[0].Aggregate != nil {
		t.Fatalf("unexpected aggregate broadcast: %#v", network.partials)
	}
	aggregate, ok := host.PartialAdaptAggregate(reqID)
	if !ok {
		t.Fatal("local execution snapshot not found")
	}
	if aggregate.FromIndex != 1 || len(aggregate.Contributions) != 2 {
		t.Fatalf("unexpected aggregate: %#v", aggregate)
	}
	if aggregate.Contributions[0].Index != 1 || aggregate.Contributions[1].Index != 2 {
		t.Fatalf("aggregate contributions are not deterministic: %#v", aggregate.Contributions)
	}
	if len(aggregate.FramedContributions) != 0 {
		t.Fatal("local execution snapshot should not duplicate framed contributions")
	}
	if err := host.HandlePartialAdapt(context.Background(), remote, network); err != nil {
		t.Fatalf("handle duplicate contribution: %v", err)
	}
	if len(network.partials) != 1 {
		t.Fatalf("duplicate contribution triggered a broadcast: %#v", network.partials)
	}
}

func framedSharesForTest(values [][]byte) []byte {
	var out []byte
	for _, value := range values {
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(value)))
		out = append(out, size...)
		out = append(out, value...)
	}
	return out
}

func oneForProtocolTest() *big.Int {
	return big.NewInt(1)
}
