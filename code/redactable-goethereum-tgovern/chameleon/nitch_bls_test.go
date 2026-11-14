package chameleon

import (
	"bytes"
	"math/big"
	"testing"
)

func TestNITCHBLSAdaptationRoundTrip(t *testing.T) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	keys, err := NITCHBLSSimulatedDistKG(pp, 2, 4)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}

	oldMsg := []byte("original transaction payload")
	newMsg := []byte("redacted transaction payload")
	r, err := NITCHBLSComputeR(keys[0], []byte("block-7"), oldMsg)
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	if len(r) != nitchG1CompressedSize {
		t.Fatalf("compressed r length = %d, want %d", len(r), nitchG1CompressedSize)
	}
	oldHash, err := NITCHBLSHash(keys[0], oldMsg, r)
	if err != nil {
		t.Fatalf("hash old message: %v", err)
	}
	if !NITCHBLSVerify(keys[0], oldHash.Hash, oldMsg, r) {
		t.Fatal("old hash verification failed")
	}

	shares := make([]*NITCHBLSAdaptationShare, 0, 3)
	for _, key := range keys[:3] {
		share, err := NITCHBLSPartialAdapt(key, oldMsg, newMsg)
		if err != nil {
			t.Fatalf("partial adapt %d: %v", key.Index, err)
		}
		if !NITCHBLSPartialVerify(key.ClonePublic(), share, oldMsg, newMsg) {
			t.Fatalf("partial verify failed for %d", key.Index)
		}
		shares = append(shares, share)
	}
	rPrime, err := NITCHBLSCombine(pp, shares, 2, 4, oldHash)
	if err != nil {
		t.Fatalf("combine: %v", err)
	}
	if len(rPrime) != nitchG1CompressedSize {
		t.Fatalf("compressed rPrime length = %d, want %d", len(rPrime), nitchG1CompressedSize)
	}
	enclaveIndexedRPrime, err := NITCHBLSCombineEnclaveIndexed(pp, shares, 2, 4, oldHash)
	if err != nil {
		t.Fatalf("enclave indexed combine: %v", err)
	}
	if !bytes.Equal(enclaveIndexedRPrime, rPrime) {
		t.Fatal("enclave indexed combine must use the same unit-root domain as regular combine")
	}
	if bytes.Equal(rPrime, r) {
		t.Fatal("rPrime should differ from r for a changed message")
	}
	if !NITCHBLSVerify(keys[0], oldHash.Hash, newMsg, rPrime) {
		t.Fatal("new message did not verify against original chameleon hash")
	}
}

func TestNITCHBLSRCompressedBigIntRoundTrip(t *testing.T) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	keys, err := NITCHBLSSimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	r, err := NITCHBLSComputeR(keys[0], []byte("compressed-r-round-trip"))
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	rInt, err := NITCHBLSRToBigInt(r)
	if err != nil {
		t.Fatalf("r to big.Int: %v", err)
	}
	roundTrip, err := NITCHBLSBigIntToR(rInt)
	if err != nil {
		t.Fatalf("big.Int to r: %v", err)
	}
	if !bytes.Equal(roundTrip, r) {
		t.Fatalf("compressed r round trip mismatch: got %x want %x", roundTrip, r)
	}
	if _, err := NITCHBLSRToBigInt(make([]byte, nitchG1PointSize)); err == nil {
		t.Fatal("96-byte uncompressed r was accepted")
	}
	invalid := make([]byte, nitchG1CompressedSize)
	invalid[0] = 0x80
	if _, err := NITCHBLSRToBigInt(invalid); err == nil {
		t.Fatal("invalid compressed G1 point was accepted")
	}
}

func TestNITCHBLSDistKGDealExtract(t *testing.T) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	const threshold = 1
	const numParties = 4
	deals := make(map[int]*NITCHBLSDistKGDeal)
	privates := make(map[int]*NITCHBLSDistKGPrivate)
	for i := 1; i <= numParties; i++ {
		deal, priv, err := NITCHBLSCreateDistKGDeal(pp, i, threshold, numParties)
		if err != nil {
			t.Fatalf("create deal %d: %v", i, err)
		}
		deals[i] = deal
		privates[i] = priv
	}
	for receiver := 1; receiver <= numParties; receiver++ {
		for dealer := 1; dealer <= numParties; dealer++ {
			ok, err := NITCHBLSVerifyDistKGShare(pp, deals[dealer], privates[dealer].Shares[receiver], receiver)
			if err != nil {
				t.Fatalf("verify share receiver=%d dealer=%d: %v", receiver, dealer, err)
			}
			if !ok {
				t.Fatalf("share receiver=%d dealer=%d rejected", receiver, dealer)
			}
		}
	}

	qual := []int{1, 2, 3, 4}
	var referencePK []byte
	for receiver := 1; receiver <= numParties; receiver++ {
		sharesToMe := make(map[int]*big.Int)
		for dealer := 1; dealer <= numParties; dealer++ {
			sharesToMe[dealer] = privates[dealer].Shares[receiver]
		}
		key, err := NITCHBLSDistKGExtract(pp, receiver, qual, deals, sharesToMe)
		if err != nil {
			t.Fatalf("extract receiver=%d: %v", receiver, err)
		}
		if len(key.GlobalPK) != nitchG2PointSize {
			t.Fatalf("global pk length = %d, want %d", len(key.GlobalPK), nitchG2PointSize)
		}
		if referencePK == nil {
			referencePK = key.GlobalPK
		} else if !bytes.Equal(referencePK, key.GlobalPK) {
			t.Fatal("extracted global public keys differ")
		}
	}
}

func TestLegacyHashDispatchesToNITCHBLS(t *testing.T) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	keys, err := NITCHBLSSimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	message := []byte("compatibility message")
	r, err := NITCHBLSComputeR(keys[0], []byte("compatibility-r"))
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	nitchHash, err := NITCHBLSHash(keys[0], message, r)
	if err != nil {
		t.Fatalf("nitch hash: %v", err)
	}
	pkInt, err := NITCHBLSPublicKeyToBigInt(keys[0])
	if err != nil {
		t.Fatalf("pk to int: %v", err)
	}
	rInt, err := NITCHBLSRToBigInt(r)
	if err != nil {
		t.Fatalf("r to int: %v", err)
	}

	legacyHash := Hash(pkInt, new(big.Int).SetBytes(message), rInt)
	if legacyHash.Cmp(new(big.Int).SetBytes(nitchHash.Hash)) != 0 {
		t.Fatal("legacy Hash did not dispatch to NITCH BLS")
	}
	if !Verify(legacyHash, pkInt, new(big.Int).SetBytes(message), rInt) {
		t.Fatal("legacy Verify rejected NITCH BLS encoded inputs")
	}
}

// TestLegacyHashContextReuseEquivalence guards the cached-context fast path
// in nitchBLSHashBigInt: interleaving multiple committee keys forces repeated
// cache rebuilds, repeated calls with the same key hit the cache, and every
// result must stay bit-identical to a freshly-parsed NITCHBLSHash call, with
// legacy Verify still accepting it.
func TestLegacyHashContextReuseEquivalence(t *testing.T) {
	pp, err := NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	keysA, err := NITCHBLSSimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatalf("distkg A: %v", err)
	}
	keysB, err := NITCHBLSSimulatedDistKG(pp, 2, 4)
	if err != nil {
		t.Fatalf("distkg B: %v", err)
	}
	for round, key := range []*NITCHBLSKey{keysA[0], keysB[0], keysA[0], keysB[0]} {
		pkInt, err := NITCHBLSPublicKeyToBigInt(key)
		if err != nil {
			t.Fatalf("round %d pk: %v", round, err)
		}
		message := []byte("legacy-context-equivalence-message")
		r, err := NITCHBLSComputeR(key, []byte("legacy-context-equivalence-r"))
		if err != nil {
			t.Fatalf("round %d r: %v", round, err)
		}
		rInt, err := NITCHBLSRToBigInt(r)
		if err != nil {
			t.Fatalf("round %d rInt: %v", round, err)
		}
		msgInt := new(big.Int).SetBytes(message)
		for call := 0; call < 3; call++ {
			legacy := Hash(pkInt, msgInt, rInt)
			if legacy == nil {
				t.Fatalf("round %d call %d: legacy Hash returned nil", round, call)
			}
			fresh, err := NITCHBLSHash(&NITCHBLSKey{PP: pp, GlobalPK: key.GlobalPK}, message, r)
			if err != nil {
				t.Fatalf("round %d call %d fresh: %v", round, call, err)
			}
			if legacy.Cmp(new(big.Int).SetBytes(fresh.Hash)) != 0 {
				t.Fatalf("round %d call %d: cached-context hash diverged from fresh NITCHBLSHash", round, call)
			}
			if !Verify(legacy, pkInt, msgInt, rInt) {
				t.Fatalf("round %d call %d: legacy Verify rejected", round, call)
			}
		}
	}
}
