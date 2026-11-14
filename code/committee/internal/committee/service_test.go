package committee

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"committee-mvp/internal/chain"
	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/rpc"
	"committee-mvp/internal/share"
	"committee-mvp/internal/wire"
)

func TestFinalizeDKGBuildsStableCommitteePubKey(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7", "node-8"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
			"node-2": "127.0.0.1:3402",
			"node-3": "127.0.0.1:3403",
			"node-4": "127.0.0.1:3404",
			"node-5": "127.0.0.1:3405",
			"node-6": "127.0.0.1:3406",
			"node-7": "127.0.0.1:3407",
			"node-8": "127.0.0.1:3408",
		},
		CommitteeSize:    8,
		Threshold:        5,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}

	s := NewService(cfg)
	var expectedPubs [][]byte
	for dealerIdx := 0; dealerIdx < cfg.CommitteeSize; dealerIdx++ {
		secret := bigIntFromInt64(int64(100 + dealerIdx))
		shs, err := share.Split(secret, cfg.Threshold, cfg.CommitteeSize)
		if err != nil {
			t.Fatalf("split dealer %d: %v", dealerIdx, err)
		}
		s.dkgShares[dealerIdx] = shs[0].Value // node-1 gets index=1 share

		d, err := crypto.NewBN254BLSFromBigInt(secret, cfg.DomainSeparation)
		if err != nil {
			t.Fatalf("new dealer signer %d: %v", dealerIdx, err)
		}
		pub, err := d.PublicKey()
		if err != nil {
			t.Fatalf("dealer pub %d: %v", dealerIdx, err)
		}
		s.dealerPubKeys[dealerIdx] = pub
		expectedPubs = append(expectedPubs, pub)
	}

	fullBitmap := make([]byte, (cfg.CommitteeSize+7)/8)
	for i := 0; i < cfg.CommitteeSize; i++ {
		fullBitmap[i/8] |= 1 << uint(i%8)
	}
	expectedCommitteePub, err := crypto.AggregatePublicKeys(expectedPubs, fullBitmap)
	if err != nil {
		t.Fatalf("aggregate expected committee pub: %v", err)
	}

	s.finalizeDKG()
	if !s.isDKGReady() {
		t.Fatalf("dkg not marked ready")
	}
	if !bytes.Equal(s.committeePubKey, expectedCommitteePub) {
		t.Fatalf("committee public key mismatch")
	}
}

func TestNewSessionIDUsesRequestAndRandomness(t *testing.T) {
	id1, err := newSessionID("sign", []byte("hello"))
	if err != nil {
		t.Fatalf("new session id 1: %v", err)
	}
	id2, err := newSessionID("sign", []byte("hello"))
	if err != nil {
		t.Fatalf("new session id 2: %v", err)
	}
	if !strings.HasPrefix(id1, "sign-") || !strings.HasPrefix(id2, "sign-") {
		t.Fatalf("unexpected session id prefixes: %q %q", id1, id2)
	}
	if id1 == id2 {
		t.Fatalf("session ids should differ for the same request because nonce is random")
	}
}

func bigIntFromInt64(v int64) *big.Int {
	return new(big.Int).SetInt64(v)
}

func TestRegisterTEENoopFlowUsesPendingTEEAndReportData(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}

	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(12345), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)

	mockChain := chain.NewMockGateway()
	owner := "0x0000000000000000000000000000000000000001"
	mockChain.SetPendingTEE(owner, &chain.PendingTEEInfo{
		Exists:        true,
		Deposit:       big.NewInt(11),
		Nonce:         big.NewInt(7),
		CreatedAt:     big.NewInt(99),
		ChallengeHash: "0x" + strings.Repeat("33", 32),
	})
	s.chainGateway = mockChain

	reportData := rpc.TEEReportData{
		HardwareID:      "0x" + strings.Repeat("11", 32),
		CommitteePubKey: "0x" + hex.EncodeToString(pub),
		SigPubKey:       "0x" + strings.Repeat("00", 12) + strings.Repeat("22", 20),
		VrfPubKey:       "0x" + strings.Repeat("00", 12) + strings.Repeat("44", 20),
		EnclavePK:       "0x" + strings.Repeat("66", 64),
	}
	reportDataHash, _, _, _, _, _, err := computeTEEReportDataHash(reportData.HardwareID, reportData.CommitteePubKey, reportData.SigPubKey, reportData.VrfPubKey, reportData.EnclavePK)
	if err != nil {
		t.Fatalf("compute report data hash: %v", err)
	}
	quote := testSGXQuoteWithReportDataHash(0xaa, 0xbb, 9, reportDataHash)
	result, err := s.RegisterTEE(context.Background(), "", owner, quote, reportData)
	if err != nil {
		t.Fatalf("register tee: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted result, got: %+v", result)
	}
	if result.PendingNonce != "7" {
		t.Fatalf("unexpected pending nonce: %s", result.PendingNonce)
	}
	if result.HardwareID != "0x"+strings.Repeat("11", 32) {
		t.Fatalf("unexpected hardware id: %s", result.HardwareID)
	}
	if result.SigPubKey != reportData.SigPubKey {
		t.Fatalf("unexpected signature public key: %s", result.SigPubKey)
	}
	if result.ReportDataHash != "0x"+hex.EncodeToString(reportDataHash) {
		t.Fatalf("unexpected report data hash: %s", result.ReportDataHash)
	}

	registration, ok := mockChain.LastRegistration()
	if !ok {
		t.Fatalf("expected one registration submission")
	}
	if registration.OwnerAddress != strings.ToLower(owner) {
		t.Fatalf("unexpected owner: %s", registration.OwnerAddress)
	}
	if registration.PendingNonce == nil || registration.PendingNonce.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("unexpected pending nonce: %v", registration.PendingNonce)
	}
	if registration.HardwareID != result.HardwareID {
		t.Fatalf("unexpected registration hardware id: %s", registration.HardwareID)
	}
	if registration.SigPubKey != result.SigPubKey || registration.VrfPubKey != reportData.VrfPubKey {
		t.Fatalf("unexpected registration evidence: %+v", registration)
	}
	if registration.EnclavePK != reportData.EnclavePK {
		t.Fatalf("unexpected registration enclave pk: %s", registration.EnclavePK)
	}
	if registration.RegisterCertHash == "" || registration.ChallengeHash != "0x"+strings.Repeat("33", 32) {
		t.Fatalf("unexpected registration cert fields: %+v", registration)
	}
	if len(registration.AggregateSignature) == 0 || len(registration.MessageHash) != 32 {
		t.Fatalf("unexpected registration payload: %+v", registration)
	}
}

func TestRegisterTEERejectsReportDataHashMismatch(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}

	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(12345), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)

	owner := "0x0000000000000000000000000000000000000001"
	mockChain := chain.NewMockGateway()
	mockChain.SetPendingTEE(owner, &chain.PendingTEEInfo{
		Exists:        true,
		Deposit:       big.NewInt(11),
		Nonce:         big.NewInt(7),
		CreatedAt:     big.NewInt(99),
		ChallengeHash: "0x" + strings.Repeat("33", 32),
	})
	s.chainGateway = mockChain

	reportData := rpc.TEEReportData{
		HardwareID:      "0x" + strings.Repeat("11", 32),
		CommitteePubKey: "0x" + hex.EncodeToString(pub),
		SigPubKey:       "0x" + strings.Repeat("00", 12) + strings.Repeat("22", 20),
		VrfPubKey:       "0x" + strings.Repeat("00", 12) + strings.Repeat("44", 20),
		EnclavePK:       "0x" + strings.Repeat("66", 64),
	}
	wrongHash := bytes.Repeat([]byte{0xee}, 32)
	quote := testSGXQuoteWithReportDataHash(0xaa, 0xbb, 9, wrongHash)
	result, err := s.RegisterTEE(context.Background(), "", owner, quote, reportData)
	if err != nil {
		t.Fatalf("register tee: %v", err)
	}
	if result.Accepted {
		t.Fatalf("expected report_data hash mismatch rejection, got accepted result: %+v", result)
	}
	if result.Reason != "report_data hash mismatch" {
		t.Fatalf("unexpected rejection reason: %q", result.Reason)
	}
	if _, ok := mockChain.LastRegistration(); ok {
		t.Fatalf("registration should not be submitted on report_data hash mismatch")
	}
}

func TestRedactAuthorizationFlowAggregatesAndSubmits(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(23456), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)
	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("07", 32),
		PkCHHash:               "0x" + strings.Repeat("08", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	params := rpc.SubmitRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		ExpiresAt:              "2000000000",
		PolicyContext:          []byte(`{"rule":"allow"}`),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat("01", 32),
			BodyHashes: rpc.RedactBodyHashes{
				TxID:          "0x" + strings.Repeat("02", 32),
				MHash:         "0x" + strings.Repeat("03", 32),
				MPrimeHash:    "0x" + strings.Repeat("04", 32),
				PosHash:       "0x" + strings.Repeat("05", 32),
				MetaHash:      "0x" + strings.Repeat("06", 32),
				ActiveSetHash: "0x" + strings.Repeat("07", 32),
				PkCHHash:      "0x" + strings.Repeat("08", 32),
			},
		},
	}
	submit, err := s.SubmitRedactRequest(context.Background(), params)
	if err != nil {
		t.Fatalf("submit redact request: %v", err)
	}
	if submit.Status != redactStatusPending || len(submit.AuthorizationDigest) != 64 {
		t.Fatalf("unexpected submit result: %+v", submit)
	}

	shareSig, err := s.bls.SignShare(mustDecodeHex(t, submit.AuthorizationDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}
	s.onSignResponse(wire.Envelope{SessionID: submit.SessionID, From: "node-1", Payload: payload})
	waitForAggregate(t, s, submit.SessionID)

	auth, err := s.RedactAuthorization(context.Background(), submit.RequestID)
	if err != nil {
		t.Fatalf("get redact authorization: %v", err)
	}
	if auth.Status != redactStatusAuthorized || auth.CertReqAuth == "" || auth.AggregatePubKey == "" {
		t.Fatalf("unexpected auth result: %+v", auth)
	}
	chainResult, err := s.SubmitRedactAuthorizationOnChain(context.Background(), submit.RequestID)
	if err != nil {
		t.Fatalf("submit auth on chain: %v", err)
	}
	if chainResult.TxHash == "" || chainResult.Status != redactStatusSubmitted {
		t.Fatalf("unexpected chain result: %+v", chainResult)
	}
	chained, ok := mockChain.LastRedactAuthorization()
	if !ok {
		t.Fatalf("expected redact authorization submission")
	}
	if chained.RequestID != submit.RequestID || len(chained.AggregateSignature) == 0 || len(chained.DPolicy) != 32 {
		t.Fatalf("unexpected chain payload: %+v", chained)
	}
}

func TestRedactRejectionFlowAggregatesAndSubmits(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(33445), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)
	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("07", 32),
		PkCHHash:               "0x" + strings.Repeat("08", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	params := rpc.RejectRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		PolicyContext:          []byte(`{"rule":"deny"}`),
		ReasonHash:             "0x" + strings.Repeat("09", 32),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat("11", 32),
			BodyHashes: rpc.RedactBodyHashes{
				TxID:          "0x" + strings.Repeat("02", 32),
				MHash:         "0x" + strings.Repeat("03", 32),
				MPrimeHash:    "0x" + strings.Repeat("04", 32),
				PosHash:       "0x" + strings.Repeat("05", 32),
				MetaHash:      "0x" + strings.Repeat("06", 32),
				ActiveSetHash: "0x" + strings.Repeat("07", 32),
				PkCHHash:      "0x" + strings.Repeat("08", 32),
			},
		},
	}
	reject, err := s.RejectRedactRequest(context.Background(), params)
	if err != nil {
		t.Fatalf("reject redact request: %v", err)
	}
	if reject.Status != redactStatusPending || len(reject.RejectionDigest) != 64 {
		t.Fatalf("unexpected reject result: %+v", reject)
	}

	shareSig, err := s.bls.SignShare(mustDecodeHex(t, reject.RejectionDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}
	s.onSignResponse(wire.Envelope{SessionID: reject.SessionID, From: "node-1", Payload: payload})
	waitForAggregate(t, s, reject.SessionID)

	cert, err := s.RedactRejection(context.Background(), reject.RequestID)
	if err != nil {
		t.Fatalf("get redact rejection: %v", err)
	}
	if cert.Status != redactStatusRejected || cert.CertReqReject == "" || cert.AggregatePubKey == "" {
		t.Fatalf("unexpected rejection cert: %+v", cert)
	}
	chainResult, err := s.SubmitRedactRejectionOnChain(context.Background(), reject.RequestID)
	if err != nil {
		t.Fatalf("submit rejection on chain: %v", err)
	}
	if chainResult.TxHash == "" || chainResult.Status != redactStatusSubmitted {
		t.Fatalf("unexpected chain result: %+v", chainResult)
	}
	chained, ok := mockChain.LastRedactRejection()
	if !ok {
		t.Fatalf("expected redact rejection submission")
	}
	if chained.RequestID != reject.RequestID || len(chained.AggregateSignature) == 0 || len(chained.ReasonHash) != 32 {
		t.Fatalf("unexpected chain payload: %+v", chained)
	}
}

func TestTEERevocationFlowAggregatesAndSubmits(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(44556), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)
	mockChain := chain.NewMockGateway()
	s.chainGateway = mockChain

	params := rpc.RevokeTEEParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		HardwareID:             "0x" + strings.Repeat("21", 32),
		ReasonHash:             "0x" + strings.Repeat("22", 32),
		SlashAmount:            "2",
	}
	revoke, err := s.RevokeTEE(context.Background(), params)
	if err != nil {
		t.Fatalf("revoke tee: %v", err)
	}
	if revoke.Status != redactStatusPending || len(revoke.RevocationDigest) != 64 {
		t.Fatalf("unexpected revoke result: %+v", revoke)
	}

	shareSig, err := s.bls.SignShare(mustDecodeHex(t, revoke.RevocationDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}
	s.onSignResponse(wire.Envelope{SessionID: revoke.SessionID, From: "node-1", Payload: payload})
	waitForAggregate(t, s, revoke.SessionID)

	cert, err := s.TEERevocation(context.Background(), revoke.RequestID)
	if err != nil {
		t.Fatalf("get tee revocation: %v", err)
	}
	if cert.Status != teeRevocationStatusSigned || cert.AggregateSig == "" || cert.AggregatePubKey == "" {
		t.Fatalf("unexpected revocation cert: %+v", cert)
	}
	chainResult, err := s.SubmitTEERevocationOnChain(context.Background(), revoke.RequestID)
	if err != nil {
		t.Fatalf("submit revocation on chain: %v", err)
	}
	if chainResult.TxHash == "" || chainResult.Status != redactStatusSubmitted {
		t.Fatalf("unexpected chain result: %+v", chainResult)
	}
	chained, ok := mockChain.LastTEERevocation()
	if !ok {
		t.Fatalf("expected tee revocation submission")
	}
	if chained.RequestID != revoke.RequestID || len(chained.AggregateSignature) == 0 || len(chained.HardwareID) != 32 || chained.SlashAmount.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("unexpected chain payload: %+v", chained)
	}
}

func TestRedactRequestRejectsMismatchedActiveSetBinding(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(45678), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)

	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("aa", 32),
		PkCHHash:               "0x" + strings.Repeat("08", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	params := rpc.SubmitRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		ExpiresAt:              "2000000000",
		PolicyContext:          []byte(`{"rule":"allow"}`),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat("01", 32),
			BodyHashes: rpc.RedactBodyHashes{
				TxID:          "0x" + strings.Repeat("02", 32),
				MHash:         "0x" + strings.Repeat("03", 32),
				MPrimeHash:    "0x" + strings.Repeat("04", 32),
				PosHash:       "0x" + strings.Repeat("05", 32),
				MetaHash:      "0x" + strings.Repeat("06", 32),
				ActiveSetHash: "0x" + strings.Repeat("07", 32),
				PkCHHash:      "0x" + strings.Repeat("08", 32),
			},
		},
	}
	_, err = s.SubmitRedactRequest(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "active_set_hash does not match chain active set") {
		t.Fatalf("expected active set hash mismatch, got: %v", err)
	}

	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("07", 32),
		PkCHHash:               "0x" + strings.Repeat("bb", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	_, err = s.SubmitRedactRequest(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "pk_ch_hash does not match chain active set") {
		t.Fatalf("expected pk ch hash mismatch, got: %v", err)
	}
}

func TestActiveSetCertFlowAggregatesFromChainState(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
		},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(34567), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)

	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveTEEs:             []string{"0x" + strings.Repeat("11", 32)},
		ActiveSetHash:          "0x" + strings.Repeat("22", 32),
		PkCHHash:               "0x" + strings.Repeat("33", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	cert, err := s.ActiveSetCert(context.Background(), rpc.GetActiveSetCertParams{
		ChainID:         "1337",
		ContractAddress: "0x00000000000000000000000000000000000000aa",
		Epoch:           "1",
		ActiveTEEList:   []string{"0x" + strings.Repeat("11", 32)},
	})
	if err != nil {
		t.Fatalf("generate active set cert: %v", err)
	}
	if cert.Status != redactStatusPending || len(cert.ActiveSetCertDigest) != 64 {
		t.Fatalf("unexpected active set cert submit result: %+v", cert)
	}

	shareSig, err := s.bls.SignShare(mustDecodeHex(t, cert.ActiveSetCertDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}
	s.onSignResponse(wire.Envelope{SessionID: cert.SessionID, From: "node-1", Payload: payload})
	waitForAggregate(t, s, cert.SessionID)

	got, err := s.ActiveSetCert(context.Background(), rpc.GetActiveSetCertParams{Epoch: "1"})
	if err != nil {
		t.Fatalf("get active set cert: %v", err)
	}
	if got.Status != activeSetCertStatusCertified || got.CertEA == "" || got.AggregatePubKey == "" {
		t.Fatalf("unexpected active set cert result: %+v", got)
	}
}

func TestRedactRequestRejectsIncompleteReq(t *testing.T) {
	_, err := normalizeRedactReqFields(rpc.RedactReqFields{
		Version: 1,
		Epoch:   1,
		ReqID:   "0x" + strings.Repeat("01", 32),
		BodyHashes: rpc.RedactBodyHashes{
			TxID: "0x" + strings.Repeat("02", 32),
		},
	})
	if err == nil {
		t.Fatalf("expected missing body hash error")
	}
}

func TestRedactRequestAllowsGenesisEpoch(t *testing.T) {
	req, err := normalizeRedactReqFields(rpc.RedactReqFields{
		Version: 1,
		Epoch:   0,
		ReqID:   "0x" + strings.Repeat("01", 32),
		BodyHashes: rpc.RedactBodyHashes{
			TxID:          "0x" + strings.Repeat("02", 32),
			MHash:         "0x" + strings.Repeat("03", 32),
			MPrimeHash:    "0x" + strings.Repeat("04", 32),
			PosHash:       "0x" + strings.Repeat("05", 32),
			MetaHash:      "0x" + strings.Repeat("06", 32),
			ActiveSetHash: "0x" + strings.Repeat("07", 32),
			PkCHHash:      "0x" + strings.Repeat("08", 32),
		},
	})
	if err != nil {
		t.Fatalf("normalize genesis epoch redact req: %v", err)
	}
	if req.Epoch != 0 {
		t.Fatalf("epoch = %d, want 0", req.Epoch)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	value = strings.TrimPrefix(value, "0x")
	out, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return out
}

func testSGXQuoteWithReportDataHash(mrEnclaveByte, mrSignerByte byte, isvSVN uint16, reportDataHash []byte) []byte {
	quote := make([]byte, 48+384)
	for i := 0; i < 32; i++ {
		quote[48+64+i] = mrEnclaveByte
		quote[48+128+i] = mrSignerByte
	}
	copy(quote[48+320:48+320+32], reportDataHash)
	quote[48+258] = byte(isvSVN)
	quote[48+259] = byte(isvSVN >> 8)
	return quote
}

// waitForAggregate waits until the coordinator has verified the threshold
// shares and stored the aggregate certificate. Share verification now runs
// concurrently with envelope dispatch (VerifyShare is a ~3.4ms BN254 pairing
// and used to be serialised under s.mu), so the aggregate is no longer stored
// by the time onSignResponse returns. Callers poll for it, exactly as the
// coordinator's RPC surface does.
func waitForAggregate(t *testing.T, s *Service, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := s.lookupAggregateResult(sessionID); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for aggregate result session_id=%s", sessionID)
}

// TestSignResponseConcurrentDuplicatesDoNotCorruptState exercises the
// lock/claim logic that replaced the old inline verification. Shares from the
// same signer arriving concurrently must leave exactly one recorded response
// and a stored aggregate. The `verifying` claim additionally keeps a duplicate
// from paying for a second BN254 pairing; correctness is guaranteed by
// acceptSignResponse overwriting by signer index under s.mu.
func TestSignResponseConcurrentDuplicatesDoNotCorruptState(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:           "node-1",
		ListenAddr:       "127.0.0.1:3401",
		StaticNodes:      []string{"node-1"},
		StaticNodeAddrs:  map[string]string{"node-1": "127.0.0.1:3401"},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(34567), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)
	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("07", 32),
		PkCHHash:               "0x" + strings.Repeat("08", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	submit, err := s.SubmitRedactRequest(context.Background(), rpc.SubmitRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		ExpiresAt:              "2000000000",
		PolicyContext:          []byte(`{"rule":"allow"}`),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat("11", 32),
			BodyHashes: rpc.RedactBodyHashes{
				TxID:          "0x" + strings.Repeat("12", 32),
				MHash:         "0x" + strings.Repeat("13", 32),
				MPrimeHash:    "0x" + strings.Repeat("14", 32),
				PosHash:       "0x" + strings.Repeat("15", 32),
				MetaHash:      "0x" + strings.Repeat("16", 32),
				ActiveSetHash: "0x" + strings.Repeat("07", 32),
				PkCHHash:      "0x" + strings.Repeat("08", 32),
			},
		},
	})
	if err != nil {
		t.Fatalf("submit redact request: %v", err)
	}

	shareSig, err := s.bls.SignShare(mustDecodeHex(t, submit.AuthorizationDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}

	const racers = 32
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			s.onSignResponse(wire.Envelope{SessionID: submit.SessionID, From: "node-1", Payload: payload})
		}()
	}
	wg.Wait()
	waitForAggregate(t, s, submit.SessionID)

	s.mu.Lock()
	state := s.sessions[submit.SessionID]
	recorded := len(state.responses)
	aggregated := state.aggregated
	s.mu.Unlock()
	if recorded != 1 {
		t.Fatalf("recorded responses = %d, want 1 for concurrent duplicate shares", recorded)
	}
	if !aggregated {
		t.Fatal("session not marked aggregated")
	}
	if _, ok := s.lookupAggregateResult(submit.SessionID); !ok {
		t.Fatal("aggregate certificate was not stored")
	}
}

// TestRedactAuthorizationWaitBlocksUntilThreshold proves the completion-channel
// path: a waiter must return as soon as the certificate is stored, and the
// elapsed time must track the actual completion rather than a poll cadence.
// It also covers the already-complete fast path, which must not block at all.
func TestRedactAuthorizationWaitBlocksUntilThreshold(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:           "node-1",
		ListenAddr:       "127.0.0.1:3401",
		StaticNodes:      []string{"node-1"},
		StaticNodeAddrs:  map[string]string{"node-1": "127.0.0.1:3401"},
		CommitteeSize:    1,
		Threshold:        1,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	bls, err := crypto.NewBN254BLSFromBigInt(big.NewInt(45678), cfg.DomainSeparation)
	if err != nil {
		t.Fatalf("new bls: %v", err)
	}
	s.bls = bls
	pub, err := s.bls.PublicKey()
	if err != nil {
		t.Fatalf("committee public key: %v", err)
	}
	s.committeePubKey = pub
	close(s.dkgReady)
	mockChain := chain.NewMockGateway()
	mockChain.SetActiveSetInfo(big.NewInt(1), &chain.ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveSetHash:          "0x" + strings.Repeat("07", 32),
		PkCHHash:               "0x" + strings.Repeat("08", 32),
		CommitteePubKeyVersion: big.NewInt(1),
		ActiveThreshold:        big.NewInt(1),
		ActiveLimit:            big.NewInt(1),
	})
	s.chainGateway = mockChain

	submit, err := s.SubmitRedactRequest(context.Background(), rpc.SubmitRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		ExpiresAt:              "2000000000",
		PolicyContext:          []byte(`{"rule":"allow"}`),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat("21", 32),
			BodyHashes: rpc.RedactBodyHashes{
				TxID:          "0x" + strings.Repeat("22", 32),
				MHash:         "0x" + strings.Repeat("23", 32),
				MPrimeHash:    "0x" + strings.Repeat("24", 32),
				PosHash:       "0x" + strings.Repeat("25", 32),
				MetaHash:      "0x" + strings.Repeat("26", 32),
				ActiveSetHash: "0x" + strings.Repeat("07", 32),
				PkCHHash:      "0x" + strings.Repeat("08", 32),
			},
		},
	})
	if err != nil {
		t.Fatalf("submit redact request: %v", err)
	}

	// A waiter started before the threshold is met must not return early.
	type waitResult struct {
		out *rpc.RedactAuthorizationData
		err error
	}
	started := make(chan struct{})
	waited := make(chan waitResult, 1)
	go func() {
		close(started)
		out, err := s.RedactAuthorizationWait(context.Background(), submit.RequestID, 5*time.Second)
		waited <- waitResult{out, err}
	}()
	<-started
	select {
	case res := <-waited:
		t.Fatalf("wait returned before the threshold was met: %+v", res.out)
	case <-time.After(50 * time.Millisecond):
	}

	// Provide the share and release the waiter.
	shareSig, err := s.bls.SignShare(mustDecodeHex(t, submit.AuthorizationDigest))
	if err != nil {
		t.Fatalf("sign share: %v", err)
	}
	payload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    0,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		t.Fatalf("encode sign response: %v", err)
	}
	s.onSignResponse(wire.Envelope{SessionID: submit.SessionID, From: "node-1", Payload: payload})

	select {
	case res := <-waited:
		if res.err != nil {
			t.Fatalf("wait failed: %v", res.err)
		}
		if res.out.Status != redactStatusAuthorized || res.out.CertReqAuth == "" {
			t.Fatalf("unexpected wait result: %+v", res.out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter was not released by the completion channel")
	}

	// The fast path must not block once the result is already stored.
	began := time.Now()
	out, err := s.RedactAuthorizationWait(context.Background(), submit.RequestID, 5*time.Second)
	if err != nil {
		t.Fatalf("fast-path wait failed: %v", err)
	}
	if out.Status != redactStatusAuthorized {
		t.Fatalf("unexpected fast-path result: %+v", out)
	}
	if elapsed := time.Since(began); elapsed > 100*time.Millisecond {
		t.Fatalf("fast path took %v, want an immediate return", elapsed)
	}

	// A timeout with no certificate stays pending rather than erroring.
	timeoutOut, err := s.RedactAuthorizationWait(context.Background(), submit.RequestID, time.Nanosecond)
	if err != nil {
		t.Fatalf("timed-out wait errored: %v", err)
	}
	if timeoutOut.Status != redactStatusAuthorized {
		t.Fatalf("unexpected timed-out result: %+v", timeoutOut)
	}
}
