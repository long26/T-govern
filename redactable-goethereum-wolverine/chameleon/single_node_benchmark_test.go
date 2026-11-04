package chameleon

import (
	"fmt"
	"math/big"
	"testing"
)

const (
	singleNodeThreshold = 0
	singleNodeParties   = 1
)

var (
	benchmarkDealSink        *DistKGDeal
	benchmarkPrivateDealSink *DistKGDealPrivate
	benchmarkShareSink       *AdaptationShare
)

// singleNodeKeyGen runs only Protocol 1's local Dealing step for n=1, t=0.
// The sole polynomial coefficient/self-share is this node's sk contribution,
// and deal.B0 is the corresponding single-node global pk. Setup, verification,
// complaints and extraction are deliberately outside this paper-aligned metric.
func singleNodeKeyGen(pp *PublicParameters) (*DistKGDeal, *DistKGDealPrivate, error) {
	deal, privateDeal, err := CreateDistKGDeal(pp, 1, singleNodeThreshold, singleNodeParties)
	if err != nil {
		return nil, nil, fmt.Errorf("create deal: %w", err)
	}
	selfShare := privateDeal.Shares[1]
	if selfShare == nil {
		return nil, nil, fmt.Errorf("create deal: missing self share")
	}
	if len(privateDeal.Coeffs) != 1 || privateDeal.Coeffs[0].Cmp(selfShare) != 0 {
		return nil, nil, fmt.Errorf("create deal: t=0 coefficient does not equal self share")
	}
	if len(deal.B0) == 0 {
		return nil, nil, fmt.Errorf("create deal: missing pk commitment B0")
	}
	return deal, privateDeal, nil
}

// extractSingleNodeKey completes the remaining DKG steps for correctness tests
// and adapt fixture preparation. None of this work is included in keyGen timing.
func extractSingleNodeKey(pp *PublicParameters, deal *DistKGDeal, privateDeal *DistKGDealPrivate) (*NITCHKey, error) {
	selfShare := privateDeal.Shares[1]
	valid, err := VerifyDistKGShare(pp, deal, selfShare, 1)
	if err != nil {
		return nil, fmt.Errorf("verify self share: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("verify self share: invalid share")
	}
	qual := BuildQUAL(nil, singleNodeThreshold, singleNodeParties)
	key, err := DistKGExtract(
		pp,
		1,
		qual,
		map[int]*DistKGDeal{1: deal},
		map[int]*big.Int{1: selfShare},
	)
	if err != nil {
		return nil, fmt.Errorf("extract key: %w", err)
	}
	if key.SecretShare == nil || key.GlobalPK == nil {
		return nil, fmt.Errorf("extract key: missing sk or pk")
	}
	return key, nil
}

// singleNodeAdapt is exactly the paper's PartialAdapt algorithm: consume
// (sk_i, vk_i, m, m') and produce the adaptation share (i, c_i, pi_i).
// PartialVerify and Combine are separate paper algorithms and are not timed.
func singleNodeAdapt(key *NITCHKey, oldMessage, newMessage Message) (*AdaptationShare, error) {
	share, err := PartialAdapt(key, oldMessage, newMessage)
	if err != nil {
		return nil, fmt.Errorf("partial adapt: %w", err)
	}
	if share == nil || share.Share == nil || share.Proof == nil {
		return nil, fmt.Errorf("partial adapt: incomplete adaptation share")
	}
	return share, nil
}

// TestSingleNodeKeyGenAndAdapt validates the exact flow used by the benchmarks.
// It is a correctness test rather than a performance measurement.
func TestSingleNodeKeyGenAndAdapt(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	deal, privateDeal, err := singleNodeKeyGen(pp)
	if err != nil {
		t.Fatalf("keyGen dealing: %v", err)
	}
	key, err := extractSingleNodeKey(pp, deal, privateDeal)
	if err != nil {
		t.Fatalf("extract fixture key: %v", err)
	}
	if key.Threshold != singleNodeThreshold || key.NumParties != singleNodeParties || key.Index != 1 {
		t.Fatalf("unexpected key metadata: t=%d n=%d index=%d", key.Threshold, key.NumParties, key.Index)
	}

	oldMessage := Message("single-node original message")
	newMessage := Message("single-node edited message")
	r := RandomPointG1(key.PP)
	if r == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	hashOutput, err := Hash(key, oldMessage, r)
	if err != nil {
		t.Fatalf("prepare original hash: %v", err)
	}

	share, err := singleNodeAdapt(key, oldMessage, newMessage)
	if err != nil {
		t.Fatalf("adapt: %v", err)
	}
	if !PartialVerify(key, share, oldMessage, newMessage) {
		t.Fatal("adaptation share verification failed")
	}
	rPrime, err := Combine(
		pp,
		[]*AdaptationShare{share},
		singleNodeThreshold,
		singleNodeParties,
		hashOutput,
		oldMessage,
		newMessage,
	)
	if err != nil {
		t.Fatalf("combine correctness check: %v", err)
	}
	if !Verify(key, hashOutput.Hash, newMessage, rPrime) {
		t.Fatal("adapted parameters do not preserve the chameleon hash")
	}
}

// BenchmarkSingleNodeKeyGen measures the paper-aligned local key generation
// contribution: Protocol 1 Dealing for n=1, t=0. Public Setup is precomputed.
func BenchmarkSingleNodeKeyGen(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	if _, _, err := singleNodeKeyGen(pp); err != nil {
		b.Fatalf("keyGen warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deal, privateDeal, err := singleNodeKeyGen(pp)
		if err != nil {
			b.Fatalf("keyGen: %v", err)
		}
		benchmarkDealSink = deal
		benchmarkPrivateDealSink = privateDeal
	}
}

// BenchmarkSingleNodeAdapt measures exactly PartialAdapt: from receipt of
// (sk_i, vk_i, m, m') through production of (i, c_i, pi_i).
func BenchmarkSingleNodeAdapt(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	deal, privateDeal, err := singleNodeKeyGen(pp)
	if err != nil {
		b.Fatalf("prepare key contribution: %v", err)
	}
	key, err := extractSingleNodeKey(pp, deal, privateDeal)
	if err != nil {
		b.Fatalf("prepare extracted key: %v", err)
	}
	oldMessage := Message("single-node original message")
	newMessage := Message("single-node edited message")

	// Validate the prepared inputs and warm the crypto path before measurement.
	share, err := singleNodeAdapt(key, oldMessage, newMessage)
	if err != nil {
		b.Fatalf("adapt warmup: %v", err)
	}
	if !PartialVerify(key, share, oldMessage, newMessage) {
		b.Fatal("adapt warmup produced an invalid share")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		share, err := singleNodeAdapt(key, oldMessage, newMessage)
		if err != nil {
			b.Fatalf("adapt: %v", err)
		}
		benchmarkShareSink = share
	}
}
