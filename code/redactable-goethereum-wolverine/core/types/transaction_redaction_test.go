package types

import (
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// transaction_signing_test.go historically shared this helper with a deleted
// transaction_test.go. Keep the package tests self-contained.
func defaultTestKey() (*ecdsa.PrivateKey, error) {
	return crypto.HexToECDSA("0123456789012345678901234567890123456789012345678901234567890123")
}

func TestRedactedTransactionPreservesHashAcrossRLP(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	chameleon.SetSystemNITCHKeyFromKey(keys[0])

	tx := NewTransaction(7, common.HexToAddress("0x1234"), big.NewInt(11), 21000, big.NewInt(2), []byte("before"))
	if err := tx.EnsureChameleonR(big.NewInt(1337)); err != nil {
		t.Fatal(err)
	}
	originalHash, err := tx.HashWithError()
	if err != nil {
		t.Fatal(err)
	}
	originalEncoding, err := rlp.EncodeToBytes(tx)
	if err != nil {
		t.Fatal(err)
	}

	redacted, err := tx.CopyForRedaction()
	if err != nil {
		t.Fatal(err)
	}
	redacted.SetData([]byte("after"))
	if got := string(tx.Data()); got != "before" {
		t.Fatalf("source transaction mutated: %q", got)
	}
	oldMessage, err := TransactionHashMessage(tx)
	if err != nil {
		t.Fatal(err)
	}
	newMessage, err := TransactionHashMessage(redacted)
	if err != nil {
		t.Fatal(err)
	}
	oldOutput, err := chameleon.Hash(keys[0], oldMessage, tx.ChameleonR())
	if err != nil {
		t.Fatal(err)
	}
	share0, err := chameleon.PartialAdapt(keys[0], oldMessage, newMessage)
	if err != nil {
		t.Fatal(err)
	}
	share1, err := chameleon.PartialAdapt(keys[1], oldMessage, newMessage)
	if err != nil {
		t.Fatal(err)
	}
	newR, err := chameleon.Combine(pp, []*chameleon.AdaptationShare{share0, share1}, 1, 2, oldOutput, oldMessage, newMessage)
	if err != nil {
		t.Fatal(err)
	}
	redacted.SetChameleonR(newR)
	gotHash, err := redacted.HashWithError()
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != originalHash {
		got := gotHash
		t.Fatalf("redacted hash changed in memory: have %s want %s", got, originalHash)
	}
	if got, want := DeriveSha(Transactions{redacted}), DeriveSha(Transactions{tx}); got != want {
		t.Fatalf("redacted transaction root changed: have %s want %s", got, want)
	}

	encoded, err := rlp.EncodeToBytes(redacted)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encoded, originalEncoding) {
		t.Fatal("redacted transaction encoding did not persist the changed payload")
	}
	var decoded Transaction
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := string(decoded.Data()); got != "after" {
		t.Fatalf("decoded payload mismatch: %q", got)
	}
	gotHash, err = decoded.HashWithError()
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != originalHash {
		got := gotHash
		t.Fatalf("redacted hash changed after RLP round trip: have %s want %s", got, originalHash)
	}
}

func TestSignerHashDoesNotIncludeChameleonR(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	chameleon.SetSystemNITCHKeyFromKey(keys[0])

	chainID := big.NewInt(1337)
	signer := NewEIP155Signer(chainID)
	tx := NewTransaction(1, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil)
	before := signer.Hash(tx)
	if err := tx.EnsureChameleonR(chainID); err != nil {
		t.Fatal(err)
	}
	if got := signer.Hash(tx); got != before {
		t.Fatalf("signer hash includes ChameleonR: have %s want %s", got, before)
	}
}

func TestNITCHTransactionHashValidationCachesAndReusesResult(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	chameleon.SetSystemNITCHKeyFromKey(keys[0])

	tx := NewTransaction(9, common.HexToAddress("0x1234"), big.NewInt(17), 21000, big.NewInt(3), []byte("cache"))
	if err := tx.EnsureChameleonR(big.NewInt(1337)); err != nil {
		t.Fatal(err)
	}

	cached, ok := tx.hash.Load().(*common.Hash)
	if !ok || cached == nil {
		t.Fatal("ValidateChameleonHashContext did not cache the NITCH transaction hash")
	}
	fromValidation := *cached

	fromErrorPath, err := tx.HashWithError()
	if err != nil {
		t.Fatal(err)
	}
	if fromErrorPath != fromValidation {
		t.Fatalf("HashWithError mismatch: have %s want %s", fromErrorPath, fromValidation)
	}
	if got := tx.hash.Load(); got != cached {
		t.Fatal("HashWithError recomputed an already cached NITCH transaction hash")
	}

	if fromHash := tx.Hash(); fromHash != fromValidation {
		t.Fatalf("Hash mismatch: have %s want %s", fromHash, fromValidation)
	}
	if got := tx.hash.Load(); got != cached {
		t.Fatal("Hash recomputed an already cached NITCH transaction hash")
	}
}

func TestLegacyTransactionHashCachesPointer(t *testing.T) {
	tx := NewTransaction(10, common.HexToAddress("0x1234"), big.NewInt(19), 21000, big.NewInt(4), []byte("legacy-cache"))
	want := tx.Hash()

	cached, ok := tx.hash.Load().(*common.Hash)
	if !ok || cached == nil {
		t.Fatal("legacy transaction hash was not stored as a valid cache pointer")
	}
	if *cached != want {
		t.Fatalf("cached legacy hash mismatch: have %s want %s", *cached, want)
	}
	if got := tx.Hash(); got != want {
		t.Fatalf("legacy Hash mismatch: have %s want %s", got, want)
	}
	if got := tx.hash.Load(); got != cached {
		t.Fatal("legacy Hash recomputed an already cached transaction hash")
	}
}

func TestNITCHTransactionHashFailureDoesNotCache(t *testing.T) {
	tx := NewTransaction(11, common.HexToAddress("0x1234"), big.NewInt(23), 21000, big.NewInt(5), []byte("invalid"))
	tx.SetChameleonR(big.NewInt(1))

	if _, err := tx.HashWithError(); err == nil {
		t.Fatal("expected invalid NITCH hash context to fail")
	}
	if got := tx.hash.Load(); got != nil {
		t.Fatalf("failed NITCH hash calculation populated cache: %T", got)
	}
	if got := tx.Hash(); got != (common.Hash{}) {
		t.Fatalf("Hash returned %s for invalid NITCH context, want zero hash", got)
	}
	if got := tx.hash.Load(); got != nil {
		t.Fatalf("failed Hash populated cache: %T", got)
	}
}

func requireInvalidTransactionHashCache(t *testing.T, tx *Transaction) {
	t.Helper()
	if got := tx.hash.Load(); got != hashCacheInvalid {
		t.Fatalf("transaction hash cache was not invalidated: %T", got)
	}
}

func requireValidTransactionHashCache(t *testing.T, tx *Transaction) *common.Hash {
	t.Helper()
	cached, ok := tx.hash.Load().(*common.Hash)
	if !ok || cached == nil || cached == hashCacheInvalid {
		t.Fatalf("transaction hash cache is not valid: %T", tx.hash.Load())
	}
	return cached
}

func TestNITCHTransactionHashSettersInvalidateCache(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	chameleon.SetSystemNITCHKeyFromKey(keys[0])

	tx := NewTransaction(12, common.HexToAddress("0x1234"), big.NewInt(29), 21000, big.NewInt(6), []byte("before"))
	if err := tx.EnsureChameleonR(big.NewInt(1337)); err != nil {
		t.Fatal(err)
	}
	original := tx.Hash()

	tx.SetData([]byte("after"))
	requireInvalidTransactionHashCache(t, tx)
	if got := tx.Hash(); got == original {
		t.Fatalf("SetData did not change NITCH transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)

	previous := tx.Hash()
	tx.SetAmount(big.NewInt(31))
	requireInvalidTransactionHashCache(t, tx)
	if got := tx.Hash(); got == previous {
		t.Fatalf("SetAmount did not change NITCH transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)

	message, err := TransactionHashMessage(tx)
	if err != nil {
		t.Fatal(err)
	}
	replacementR, err := chameleon.ComputeTransactionR(keys[0], big.NewInt(1338), message)
	if err != nil {
		t.Fatal(err)
	}
	previous = tx.Hash()
	tx.SetChameleonR(replacementR)
	requireInvalidTransactionHashCache(t, tx)
	if got := tx.Hash(); got == previous {
		t.Fatalf("SetChameleonR did not change NITCH transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)

	tx.SetChameleonR(nil)
	requireInvalidTransactionHashCache(t, tx)
	legacyHash := tx.Hash()
	if legacyHash == (common.Hash{}) {
		t.Fatal("clearing ChameleonR did not restore the legacy RLP hash")
	}
	requireValidTransactionHashCache(t, tx)

	tx.SetChameleonR(replacementR)
	requireInvalidTransactionHashCache(t, tx)
	if got := tx.Hash(); got == legacyHash {
		t.Fatalf("setting ChameleonR did not restore the NITCH transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)
}

func TestLegacyTransactionHashSettersInvalidateCacheAndCopyAmount(t *testing.T) {
	tx := NewTransaction(13, common.HexToAddress("0x1234"), big.NewInt(37), 21000, big.NewInt(7), []byte("before"))
	original := tx.Hash()

	tx.SetData([]byte("after"))
	requireInvalidTransactionHashCache(t, tx)
	if got := tx.Hash(); got == original {
		t.Fatalf("SetData did not change legacy transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)

	amount := big.NewInt(41)
	previous := tx.Hash()
	tx.SetAmount(amount)
	requireInvalidTransactionHashCache(t, tx)
	amount.SetInt64(43)
	if got := tx.Value(); got.Cmp(big.NewInt(41)) != 0 {
		t.Fatalf("SetAmount retained caller-owned big.Int: have %s want 41", got)
	}
	if got := tx.Hash(); got == previous {
		t.Fatalf("SetAmount did not change legacy transaction hash: %s", got)
	}
	requireValidTransactionHashCache(t, tx)
}

func TestDecodeRLPInvalidatesExistingTransactionHashCache(t *testing.T) {
	source := NewTransaction(14, common.HexToAddress("0x1234"), big.NewInt(47), 21000, big.NewInt(8), []byte("source"))
	encoded, err := rlp.EncodeToBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	want := source.Hash()

	target := NewTransaction(15, common.HexToAddress("0x5678"), big.NewInt(53), 21000, big.NewInt(9), []byte("target"))
	stale := target.Hash()
	if err := rlp.DecodeBytes(encoded, target); err != nil {
		t.Fatal(err)
	}
	requireInvalidTransactionHashCache(t, target)
	if got := target.Hash(); got != want {
		t.Fatalf("decoded transaction reused stale hash: have %s want %s", got, want)
	}
	if stale == want {
		t.Fatal("test setup did not create a distinct stale transaction hash")
	}
}

func TestTransactionCopiesDoNotInheritHashCache(t *testing.T) {
	tx := NewTransaction(16, common.HexToAddress("0x1234"), big.NewInt(59), 21000, big.NewInt(10), []byte("copy"))
	tx.Hash()
	requireValidTransactionHashCache(t, tx)

	redacted, err := tx.CopyForRedaction()
	if err != nil {
		t.Fatal(err)
	}
	if got := redacted.hash.Load(); got != nil {
		t.Fatalf("CopyForRedaction inherited hash cache: %T", got)
	}

	key, err := defaultTestKey()
	if err != nil {
		t.Fatal(err)
	}
	signer := NewEIP155Signer(big.NewInt(1337))
	sig, err := crypto.Sign(signer.Hash(tx).Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := tx.WithSignature(signer, sig)
	if err != nil {
		t.Fatal(err)
	}
	if got := signed.hash.Load(); got != nil {
		t.Fatalf("WithSignature inherited hash cache: %T", got)
	}
}
