package chain

import (
	"context"
	"math/big"
	"strings"
	"testing"
)

func TestMockGatewayRegistration(t *testing.T) {
	g := NewMockGateway()

	txHash, err := g.SubmitTEERegistration(context.Background(), RegistrationInput{
		RequestID:          "r1",
		OwnerAddress:       "0xabc",
		PendingNonce:       big.NewInt(7),
		MREnclave:          "0x01",
		MRSigner:           "0x02",
		HardwareID:         "0x03",
		SigPubKey:          "0x04",
		VrfPubKey:          "0x05",
		EnclavePK:          "0x" + strings.Repeat("06", 64),
		RegisterCertHash:   "0x06",
		ChallengeHash:      "0x07",
		QuoteHash:          []byte{1, 2, 3},
		MessageHash:        []byte{9, 9, 9},
		AggregateSignature: []byte{4, 5},
		AggregatePublicKey: []byte{6, 7},
	})
	if err != nil {
		t.Fatalf("submit registration: %v", err)
	}
	if txHash == "" {
		t.Fatalf("expected mock tx hash")
	}
	if g.RegistrationCount() != 1 {
		t.Fatalf("expected one registration")
	}
	in, ok := g.LastRegistration()
	if !ok {
		t.Fatalf("expected registration")
	}
	if in.RequestID != "r1" {
		t.Fatalf("unexpected request id: %s", in.RequestID)
	}
	check, ok := g.LastRegistration()
	if !ok {
		t.Fatalf("expected registration")
	}
	if check.PendingNonce == nil || check.PendingNonce.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("expected pending nonce copy, got=%v", check.PendingNonce)
	}
}

func TestMockGatewayMrSignerRegistered(t *testing.T) {
	g := NewMockGateway()
	registered, err := g.IsMrSignerRegistered(context.Background(), "mr-1")
	if err != nil {
		t.Fatalf("is mrsigner registered failed: %v", err)
	}
	if registered {
		t.Fatalf("expected default unregistered state")
	}

	g.SetMrSignerRegistered("mr-1", true)
	registered, err = g.IsMrSignerRegistered(context.Background(), "mr-1")
	if err != nil {
		t.Fatalf("is mrsigner registered failed: %v", err)
	}
	if !registered {
		t.Fatalf("expected registered state")
	}

	g.SetMrSignerRegistered("mr-1", false)
	registered, err = g.IsMrSignerRegistered(context.Background(), "mr-1")
	if err != nil {
		t.Fatalf("is mrsigner registered failed: %v", err)
	}
	if registered {
		t.Fatalf("expected unregistered after clear")
	}
}

func TestMockGatewayPendingAndStatus(t *testing.T) {
	g := NewMockGateway()
	g.SetPendingTEE("0xabc", &PendingTEEInfo{
		Exists:        true,
		Deposit:       big.NewInt(11),
		Nonce:         big.NewInt(12),
		CreatedAt:     big.NewInt(13),
		ChallengeHash: "0x1234",
	})
	g.SetTEEStatus("0xabc", &TEEStatusInfo{Status: 1, HardwareID: "0x1234"})

	pending, err := g.GetPendingTEE(context.Background(), "0xABC")
	if err != nil {
		t.Fatalf("get pending tee: %v", err)
	}
	if !pending.Exists || pending.Nonce.Cmp(big.NewInt(12)) != 0 {
		t.Fatalf("unexpected pending tee: %+v", pending)
	}
	if pending.ChallengeHash != "0x1234" {
		t.Fatalf("unexpected pending evidence: %+v", pending)
	}

	status, err := g.GetTEEStatusByOwner(context.Background(), "0xAbC")
	if err != nil {
		t.Fatalf("get tee status: %v", err)
	}
	if status.Status != 1 || status.HardwareID != "0x1234" {
		t.Fatalf("unexpected tee status: %+v", status)
	}

	g.SetActiveSetInfo(big.NewInt(1), &ActiveSetInfo{
		Epoch:                  big.NewInt(1),
		ActiveTEEs:             []string{"0x" + strings.Repeat("11", 32)},
		ActiveSetHash:          "0x" + strings.Repeat("22", 32),
		PkCHHash:               "0x" + strings.Repeat("33", 32),
		CommitteePubKeyVersion: big.NewInt(4),
		ActiveThreshold:        big.NewInt(2),
		ActiveLimit:            big.NewInt(3),
	})
	activeSet, err := g.GetActiveSetInfo(context.Background(), big.NewInt(1))
	if err != nil {
		t.Fatalf("get active set info: %v", err)
	}
	if activeSet.ActiveThreshold.Cmp(big.NewInt(2)) != 0 || len(activeSet.ActiveTEEs) != 1 {
		t.Fatalf("unexpected active set info: %+v", activeSet)
	}
}
