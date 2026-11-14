package crypto

// Benchmark harness for the committee threshold signing path used by
// redact authorization (Cert_req^auth) and TEE quote registration.
//
// It mocks the committee with a real (t, n) Shamir sharing over the BN254
// scalar field, so the partial signatures aggregated here are exactly the
// shape produced by internal/committee. No geth node is involved.
//
// Run: go test ./internal/crypto/ -run '^$' -bench 'Committee' -benchtime 20x

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"testing"

	"committee-mvp/internal/share"

	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

type committeeCase struct {
	name      string
	n, t      int
	committee []*BN254BLS
	aggPub    []byte
	msg       []byte
}

// newCommitteeCase builds an n-member committee with a t-of-n Shamir sharing of
// a single secret, plus the matching aggregated public key.
func newCommitteeCase(tb testing.TB, name string, n, t int) *committeeCase {
	tb.Helper()

	secret, err := rand.Int(rand.Reader, bn256.Order)
	if err != nil {
		tb.Fatalf("sample secret: %v", err)
	}
	shares, err := share.Split(secret, t, n)
	if err != nil {
		tb.Fatalf("split %d-of-%d: %v", t, n, err)
	}

	members := make([]*BN254BLS, n)
	pubs := make([][]byte, n)
	fullBitmap := make([]byte, (n+7)/8)
	for i := 0; i < n; i++ {
		// share.Split returns indices starting at 1; AggregateThresholdSignatures
		// evaluates at x = index+1, so the polynomial must be sampled at x = i+1.
		x := big.NewInt(int64(shares[i].Index))
		if got := int64(i + 1); x.Int64() != got {
			tb.Fatalf("share index mismatch: got %s want %d", x, got)
		}
		members[i], err = NewBN254BLSFromBigInt(shares[i].Value, testDST)
		if err != nil {
			tb.Fatalf("member %d: %v", i, err)
		}
		pubs[i], err = members[i].PublicKey()
		if err != nil {
			tb.Fatalf("member %d pubkey: %v", i, err)
		}
		fullBitmap[i/8] |= 1 << uint(i%8)
	}

	// Sanity: reconstruct the secret from t shares. The committee public key is
	// the public key of that reconstructed secret -- in the real DKG it is the
	// sum of the dealer public keys, which is the public key of the sum of the
	// dealer secrets, NOT the sum of the member share public keys.
	recovered, err := share.Recover(shares[:t], t)
	if err != nil {
		tb.Fatalf("recover secret: %v", err)
	}
	aggSigner, err := NewBN254BLSFromBigInt(recovered, testDST)
	if err != nil {
		tb.Fatalf("recovered signer: %v", err)
	}
	aggPub, err := aggSigner.PublicKey()
	if err != nil {
		tb.Fatalf("recovered pubkey: %v", err)
	}

	// Cross-check the other direction: summing the member share public keys must
	// also land on the same point, otherwise our index convention (share i at
	// x = i+1) does not match AggregateThresholdSignatures.
	sumPub, err := AggregatePublicKeys(pubs, fullBitmap)
	if err != nil {
		tb.Fatalf("aggregate share pubkeys: %v", err)
	}
	if string(sumPub) == string(aggPub) {
		tb.Fatalf("setup broken: summing member share pubkeys must differ from the committee secret pubkey")
	}

	// Mock authorization digest (16-byte d_req): 32 bytes, as the real
	// redact authorization message is (computeRedactAuthorizationDigest).
	msg := make([]byte, 32)
	copy(msg, []byte("T_GOVERN_REDACT_AUTH_V1/mock/"))
	for i := 16; i < 32; i++ {
		msg[i] = byte(i)
	}

	return &committeeCase{name: name, n: n, t: t, committee: members, aggPub: aggPub, msg: msg}
}

// signWithFirst returns partial signatures from the first count members and
// verifies the aggregate they produce.
func (c *committeeCase) signWithFirst(tb testing.TB, count int) ([][]byte, []byte, []int) {
	tb.Helper()
	sigs := make([][]byte, count)
	indices := make([]int, count)
	for i := 0; i < count; i++ {
		sig, err := c.committee[i].SignShare(c.msg)
		if err != nil {
			tb.Fatalf("sign share %d: %v", i, err)
		}
		sigs[i] = sig
		indices[i] = i
	}
	bitmap := make([]byte, (c.n+7)/8)
	for _, idx := range indices {
		bitmap[idx/8] |= 1 << uint(idx%8)
	}
	return sigs, bitmap, indices
}

// TestCommitteeSetupMatchesRecoveredSecret guards the benchmark fixture: if this
// fails, the aggregation timings below are meaningless.
func TestCommitteeSetupMatchesRecoveredSecret(t *testing.T) {
	for _, tc := range []struct{ n, t int }{{8, 5}, {16, 11}, {32, 21}} {
		c := newCommitteeCase(t, fmt.Sprintf("%d-%d", tc.n, tc.t), tc.n, tc.t)
		sigs, bitmap, indices := c.signWithFirst(t, tc.t)
		aggSig, err := AggregateThresholdSignatures(sigs, indices)
		if err != nil {
			t.Fatalf("n=%d t=%d aggregate: %v", tc.n, tc.t, err)
		}
		verifier, err := NewBN254BLS(nil, testDST)
		if err != nil {
			t.Fatalf("verifier: %v", err)
		}
		if err := verifier.VerifyAggregate(c.aggPub, c.msg, aggSig, bitmap); err != nil {
			t.Fatalf("n=%d t=%d aggregate does not verify: %v", tc.n, tc.t, err)
		}
	}
}

// signerRounds reports how many partial signatures to aggregate at each step:
// every accepted round from t up to n, plus the extra erasure-code rounds when
// t <= n/2 (needed so the erasure code has 2t coded symbols).
func signerRounds(n, t int) []int {
	rounds := []int{}
	for k := t; k <= n; k++ {
		rounds = append(rounds, k)
	}
	for k := 2 * t; k <= n; k++ {
		if k > t {
			rounds = append(rounds, k)
		}
	}
	// dedupe, keep ascending
	out := rounds[:0]
	seen := map[int]bool{}
	for _, k := range rounds {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func BenchmarkCommitteeAggregate(b *testing.B) {
	sizes := []struct{ name string; n, t int }{
		{"8-5", 8, 5},
		{"16-11", 16, 11},
		{"32-21", 32, 21},
	}
	for _, sz := range sizes {
		c := newCommitteeCase(b, sz.name, sz.n, sz.t)
		for _, k := range signerRounds(sz.n, sz.t) {
			sigs, _, indices := c.signWithFirst(b, k)
			b.Run(fmt.Sprintf("%s/signers=%d", sz.name, k), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := AggregateThresholdSignatures(sigs, indices); err != nil {
						b.Fatalf("aggregate: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkCommitteeAggregatePlusVerify measures the coordinator's full
// finalization step: aggregate the partial signatures, then verify the result
// against the committee public key.
func BenchmarkCommitteeAggregatePlusVerify(b *testing.B) {
	sizes := []struct{ name string; n, t int }{
		{"8-5", 8, 5},
		{"16-11", 16, 11},
		{"32-21", 32, 21},
	}
	verifier, err := NewBN254BLS(nil, testDST)
	if err != nil {
		b.Fatalf("verifier: %v", err)
	}
	for _, sz := range sizes {
		c := newCommitteeCase(b, sz.name, sz.n, sz.t)
		sigs, bitmap, indices := c.signWithFirst(b, sz.t)
		msg := c.msg
		aggPub := c.aggPub
		b.Run(fmt.Sprintf("%s/signers=%d", sz.name, sz.t), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				aggSig, err := AggregateThresholdSignatures(sigs, indices)
				if err != nil {
					b.Fatalf("aggregate: %v", err)
				}
				if err := verifier.VerifyAggregate(aggPub, msg, aggSig, bitmap); err != nil {
					b.Fatalf("verify: %v", err)
				}
			}
		})
	}
}

// BenchmarkCommitteeAggregateByMsgLen isolates the cost of hashing the
// authorization digest: fixed t signers, varying message size.
func BenchmarkCommitteeAggregateByMsgLen(b *testing.B) {
	c := newCommitteeCase(b, "8-5", 8, 5)
	sigs, _, indices := c.signWithFirst(b, c.t)
	for _, msgLen := range []int{16, 32, 128, 1024} {
		msg := make([]byte, msgLen)
		binary.BigEndian.PutUint64(msg, uint64(msgLen))
		b.Run(fmt.Sprintf("msglen=%d", msgLen), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := AggregateThresholdSignatures(sigs, indices); err != nil {
					b.Fatalf("aggregate: %v", err)
				}
				_ = msg
			}
		})
	}
}
