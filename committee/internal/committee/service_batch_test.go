package committee

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"

	"committee-mvp/internal/chain"
	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/rpc"
	"committee-mvp/internal/wire"
)

// buildBatchService wires a coordinator whose threshold is met by 3 distinct
// signers, so the batch path in acceptSignResponse is exercised with a real
// aggregation rather than a single-share edge case.
func buildBatchService(t *testing.T, size, threshold int) (*Service, []*crypto.BN254BLS) {
	t.Helper()
	nodes := make([]string, size)
	addrs := map[string]string{}
	for i := 0; i < size; i++ {
		nodes[i] = "node-" + string(rune('1'+i))
		addrs[nodes[i]] = "127.0.0.1:" + string(rune('0'+i))
	}
	cfg := &config.NodeConfig{
		NodeID:           nodes[0],
		ListenAddr:       "127.0.0.1:3401",
		StaticNodes:      nodes,
		StaticNodeAddrs:  addrs,
		CommitteeSize:    size,
		Threshold:        threshold,
		CoordinatorID:    nodes[0],
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
	s := NewService(cfg)
	// Real Shamir shares over a degree-(threshold-1) polynomial so any
	// threshold-sized subset reconstructs f(0); independent keys would satisfy
	// the batch check but fail the aggregate certificate verification.
	coeffs := make([]*big.Int, threshold)
	mod := new(big.Int).Set(bn256.Order)
	for i := range coeffs {
		sum := sha256.Sum256([]byte{byte(i), 0x11})
		coeffs[i] = new(big.Int).SetBytes(sum[:])
		coeffs[i].Mod(coeffs[i], mod)
	}
	shareAt := func(x int64) *big.Int {
		acc := new(big.Int)
		xb := big.NewInt(x)
		for i := len(coeffs) - 1; i >= 0; i-- {
			acc.Mul(acc, xb)
			acc.Add(acc, coeffs[i])
			acc.Mod(acc, mod)
		}
		return acc
	}
	signers := make([]*crypto.BN254BLS, size)
	for i := 0; i < size; i++ {
		b, err := crypto.NewBN254BLSFromBigInt(shareAt(int64(i+1)), cfg.DomainSeparation)
		if err != nil {
			t.Fatalf("signer %d: %v", i, err)
		}
		signers[i] = b
	}
	s.bls = signers[0]
	// committeePubKey is f(0)*G2, the Lagrange-weighted combination of the
	// participating keys, matching what DKG derives.
	committeePub, err := aggregatePubFromShares(signers[:threshold], mod)
	if err != nil {
		t.Fatalf("aggregate pubkey: %v", err)
	}
	s.committeePubKey = committeePub
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
	return s, signers
}

func submitForBatch(t *testing.T, s *Service, tag byte) *rpc.SubmitRedactRequestData {
	t.Helper()
	submit, err := s.SubmitRedactRequest(context.Background(), rpc.SubmitRedactRequestParams{
		ChainID:                "1337",
		ContractAddress:        "0x00000000000000000000000000000000000000aa",
		CommitteePubKeyVersion: "1",
		ExpiresAt:              "2000000000",
		PolicyContext:          []byte(`{"rule":"allow"}`),
		Req: rpc.RedactReqFields{
			Version: 1,
			Epoch:   1,
			ReqID:   "0x" + strings.Repeat(string(tag), 64),
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
		t.Fatalf("submit: %v", err)
	}
	return submit
}

// Three distinct signers reach the threshold and the batch check must accept
// them, producing a valid certificate.
func TestBatchPathAggregatesMultipleSignatures(t *testing.T) {
	s, signers := buildBatchService(t, 4, 3)
	submit := submitForBatch(t, s, '3')
	digest, err := hex.DecodeString(submit.AuthorizationDigest)
	if err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	for i := 0; i < 3; i++ {
		sig, err := signers[i].SignShare(digest)
		if err != nil {
			t.Fatalf("sign %d: %v", i, err)
		}
		pub, err := signers[i].PublicKey()
		if err != nil {
			t.Fatalf("pubkey %d: %v", i, err)
		}
		payload, err := wire.EncodePayload(wire.SignResponsePayload{
			SignerIndex: i, ShareSignature: sig, SignerPubKey: pub,
		})
		if err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		s.onSignResponse(wire.Envelope{SessionID: submit.SessionID, From: "node", Payload: payload})
	}
	waitForAggregate(t, s, submit.SessionID)
	auth, err := s.RedactAuthorization(context.Background(), submit.RequestID)
	if err != nil {
		t.Fatalf("get auth: %v", err)
	}
	if auth.Status != redactStatusAuthorized || auth.CertReqAuth == "" {
		t.Fatalf("unexpected auth: %+v", auth)
	}
}

// A forged share among the threshold set must be rejected and must not produce
// a certificate.
func TestBatchPathRejectsForgedShare(t *testing.T) {
	s, signers := buildBatchService(t, 4, 3)
	submit := submitForBatch(t, s, '4')
	digest, err := hex.DecodeString(submit.AuthorizationDigest)
	if err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	for i := 0; i < 3; i++ {
		sig, err := signers[i].SignShare(digest)
		if err != nil {
			t.Fatalf("sign %d: %v", i, err)
		}
		pub, err := signers[i].PublicKey()
		if err != nil {
			t.Fatalf("pubkey %d: %v", i, err)
		}
		if i == 1 {
			// Sign a different message: consistent with nothing else in the set.
			sig, err = signers[i].SignShare([]byte("tampered-digest"))
			if err != nil {
				t.Fatalf("tampered sign: %v", err)
			}
		}
		payload, err := wire.EncodePayload(wire.SignResponsePayload{
			SignerIndex: i, ShareSignature: sig, SignerPubKey: pub,
		})
		if err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		s.onSignResponse(wire.Envelope{SessionID: submit.SessionID, From: "node", Payload: payload})
	}
	// The batch must fail; give the fallback time to run and assert no cert.
	time.Sleep(200 * time.Millisecond)
	auth, err := s.RedactAuthorization(context.Background(), submit.RequestID)
	if err != nil {
		t.Fatalf("get auth: %v", err)
	}
	if auth.Status == redactStatusAuthorized || auth.CertReqAuth != "" {
		t.Fatalf("forged share produced a certificate: %+v", auth)
	}
}

// aggregatePubFromShares computes sum_i lambda_i * pk_i over x = 1..n, i.e.
// f(0)*G2 for the degree-(n-1) polynomial those shares came from.
func aggregatePubFromShares(signers []*crypto.BN254BLS, mod *big.Int) ([]byte, error) {
	xs := make([]*big.Int, len(signers))
	for i := range xs {
		xs[i] = big.NewInt(int64(i + 1))
	}
	acc := new(bn256.G2)
	first := true
	for i := range signers {
		lam := lagrangeAtZero(xs, i, mod)
		enc, err := signers[i].PublicKey()
		if err != nil {
			return nil, err
		}
		p := new(bn256.G2)
		if _, err := p.Unmarshal(enc); err != nil {
			return nil, err
		}
		p.ScalarMult(p, lam)
		if first {
			acc.Set(p)
			first = false
			continue
		}
		acc.Add(acc, p)
	}
	return acc.Marshal(), nil
}

func lagrangeAtZero(xs []*big.Int, i int, mod *big.Int) *big.Int {
	num := big.NewInt(1)
	den := big.NewInt(1)
	for j := range xs {
		if j == i {
			continue
		}
		num.Mul(num, new(big.Int).Neg(xs[j]))
		num.Mod(num, mod)
		diff := new(big.Int).Sub(xs[i], xs[j])
		den.Mul(den, diff)
		den.Mod(den, mod)
	}
	inv := new(big.Int).ModInverse(den, mod)
	if inv == nil {
		return big.NewInt(0)
	}
	return num.Mul(num, inv).Mod(num, mod)
}
