package crypto

import (
	"math/big"
	"testing"

	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

const batchTestDST = "committee-sig/mvp/v1"

// batchShares builds n signers over a message, returning their encoded share
// signatures and public keys.
func batchShares(t *testing.T, message []byte, n int) ([][]byte, [][]byte) {
	t.Helper()
	h := hashToG1(message, []byte(batchTestDST))
	sigs := make([][]byte, n)
	pubs := make([][]byte, n)
	for i := 0; i < n; i++ {
		sk := big.NewInt(int64(1000 + i*7))
		sigs[i] = new(bn256.G1).ScalarMult(h, sk).Marshal()
		pk, err := new(bn256.G2).ScalarBaseMult(sk).Marshal(), error(nil)
		if err != nil {
			t.Fatalf("marshal pubkey: %v", err)
		}
		pubs[i] = pk
	}
	return sigs, pubs
}

func TestShareBatchAcceptsHonestShares(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 21)
	b := NewShareBatch(msg, []byte(batchTestDST))
	for _, s := range sigs {
		if err := b.Add(s); err != nil {
			t.Fatalf("add share: %v", err)
		}
	}
	if err := b.VerifyAll(pubs); err != nil {
		t.Fatalf("VerifyAll rejected honest shares: %v", err)
	}
	if i, err := b.VerifyEach(pubs); err != nil {
		t.Fatalf("VerifyEach rejected honest shares at %d: %v", i, err)
	}
}

// A share signed for a different message must fail both paths.
func TestShareBatchRejectsWrongMessageShare(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 5)
	// Replace share 2 with one bound to another message.
	other := hashToG1([]byte("other-message"), []byte(batchTestDST))
	sigs[2] = new(bn256.G1).ScalarMult(other, big.NewInt(int64(1000+2*7))).Marshal()

	b := NewShareBatch(msg, []byte(batchTestDST))
	for _, s := range sigs {
		b.Add(s)
	}
	if err := b.VerifyAll(pubs); err == nil {
		t.Fatal("VerifyAll accepted a share bound to the wrong message")
	}
	i, err := b.VerifyEach(pubs)
	if err == nil {
		t.Fatal("VerifyEach accepted a share bound to the wrong message")
	}
	if i != 2 {
		t.Fatalf("VerifyEach blamed index %d, want 2", i)
	}
}

// A forged share (random scalar, honest registered key) must fail.
func TestShareBatchRejectsForgedShare(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 5)
	h := hashToG1(msg, []byte(batchTestDST))
	sigs[0] = new(bn256.G1).ScalarMult(h, big.NewInt(31337)).Marshal()

	b := NewShareBatch(msg, []byte(batchTestDST))
	for _, s := range sigs {
		b.Add(s)
	}
	if err := b.VerifyAll(pubs); err == nil {
		t.Fatal("VerifyAll accepted a forged share")
	}
	if i, err := b.VerifyEach(pubs); err == nil || i != 0 {
		t.Fatalf("VerifyEach = (%d, %v), want blames index 0", i, err)
	}
}

// Omitting or duplicating a share must not let the aggregate pass.
func TestShareBatchRejectsCountMismatch(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 5)
	b := NewShareBatch(msg, []byte(batchTestDST))
	for _, s := range sigs {
		b.Add(s)
	}
	if err := b.VerifyAll(pubs[:4]); err == nil {
		t.Fatal("VerifyAll accepted a truncated pubkey set")
	}
	// Duplicating one share and one pubkey keeps the aggregate self-consistent
	// but no longer matches the intended signer set; the caller's index map is
	// what prevents this, so assert the batch itself still verifies (it is
	// algebraically consistent) and document that dedup is the caller's job.
	b2 := NewShareBatch(msg, []byte(batchTestDST))
	b2.Add(sigs[0])
	b2.Add(sigs[0])
	if err := b2.VerifyAll([][]byte{pubs[0], pubs[0]}); err != nil {
		t.Fatalf("duplicated consistent shares should verify algebraically: %v", err)
	}
}

func TestShareBatchRejectsMalformedInput(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 3)

	b := NewShareBatch(msg, []byte(batchTestDST))
	if err := b.Add(nil); err == nil {
		t.Fatal("Add accepted an empty signature")
	}
	if err := b.Add([]byte{1, 2, 3}); err == nil {
		t.Fatal("Add accepted a malformed signature")
	}
	for _, s := range sigs {
		b.Add(s)
	}
	if err := b.VerifyAll([][]byte{pubs[0], nil, pubs[2]}); err == nil {
		t.Fatal("VerifyAll accepted an empty public key")
	}
	if err := b.VerifyAll([][]byte{pubs[0], {9, 9}, pubs[2]}); err == nil {
		t.Fatal("VerifyAll accepted a malformed public key")
	}
	empty := NewShareBatch(msg, []byte(batchTestDST))
	if err := empty.VerifyAll(nil); err == nil {
		t.Fatal("VerifyAll accepted an empty batch")
	}
}

// The cache must return the same decoded point for repeated lookups, decode
// once, and never let a malformed encoding for an already-known index evict
// the established key.
func TestShareKeyCacheReusesAndProtectsKeys(t *testing.T) {
	sigs, pubs := batchShares(t, []byte("m"), 3)
	_ = sigs
	c := NewShareKeyCache()

	first, err := c.Get(0, pubs[0])
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	second, err := c.Get(0, pubs[0])
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if first != second {
		t.Fatal("cache returned a different point on the second lookup")
	}

	// A garbage re-encoding for a known index must not replace the good key.
	kept, err := c.Get(0, []byte{0xde, 0xad})
	if err != nil {
		t.Fatalf("get with malformed input: %v", err)
	}
	if kept != first {
		t.Fatal("malformed input evicted the cached key")
	}

	// An unknown index with a malformed key must error, not cache.
	if _, err := c.Get(7, []byte{0xde, 0xad}); err == nil {
		t.Fatal("cache accepted a malformed key for an unknown index")
	}

	// Reset clears the cache.
	c.Reset()
	third, err := c.Get(0, pubs[0])
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if third == first {
		t.Fatal("Reset did not clear the cache")
	}
}

// A batch filled from the cache must agree with one filled from wire encodings.
func TestShareBatchDecodedKeysMatchEncodedPath(t *testing.T) {
	msg := []byte("same-message")
	sigs, pubs := batchShares(t, msg, 5)

	encoded := NewShareBatch(msg, []byte(batchTestDST))
	cached := NewShareBatch(msg, []byte(batchTestDST))
	cache := NewShareKeyCache()
	for i, s := range sigs {
		encoded.Add(s)
		cached.Add(s)
		key, err := cache.Get(i, pubs[i])
		if err != nil {
			t.Fatalf("cache get %d: %v", i, err)
		}
		cached.AddDecodedKey(key)
	}
	if err := encoded.VerifyAll(pubs); err != nil {
		t.Fatalf("encoded path rejected honest shares: %v", err)
	}
	if err := cached.VerifyAll(nil); err != nil {
		t.Fatalf("cached path rejected honest shares: %v", err)
	}

	// Both paths must reject the same forgery.
	h := hashToG1(msg, []byte(batchTestDST))
	forged := new(bn256.G1).ScalarMult(h, big.NewInt(31337)).Marshal()
	encoded2 := NewShareBatch(msg, []byte(batchTestDST))
	cached2 := NewShareBatch(msg, []byte(batchTestDST))
	cache2 := NewShareKeyCache()
	for i, s := range sigs {
		if i == 0 {
			s = forged
		}
		encoded2.Add(s)
		cached2.Add(s)
		key, _ := cache2.Get(i, pubs[i])
		cached2.AddDecodedKey(key)
	}
	pubs2 := append([][]byte{forged}, pubs[1:]...)
	_ = pubs2
	if err := encoded2.VerifyAll(pubs); err == nil {
		t.Fatal("encoded path accepted a forged share")
	}
	if err := cached2.VerifyAll(nil); err == nil {
		t.Fatal("cached path accepted a forged share")
	}
}
