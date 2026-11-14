package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func alignedTransaction(t *testing.T, chainID *big.Int) *Transaction {
	t.Helper()
	key, _ := defaultTestKey()
	tx, err := SignTx(NewTransaction(1, common.Address{1}, big.NewInt(2), 50000, big.NewInt(1), []byte("data")), NewEIP155Signer(chainID), key)
	if err != nil {
		t.Fatal(err)
	}
	tx.SetChameleonHashFields(big.NewInt(8), big.NewInt(5))
	return tx
}

func TestAlignedTransactionWireAndTrustedKey(t *testing.T) {
	chainID := big.NewInt(73331)
	tx := alignedTransaction(t, chainID)
	expected, err := tx.HashWithError()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rlp.EncodeToBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil || len(fields) != 10 {
		t.Fatalf("not a ten-field transaction: %d fields, %v", len(fields), err)
	}
	legacy, err := EncodeTxToBytesNoChameleonR(tx)
	if err != nil {
		t.Fatal(err)
	}
	var legacyFields []rlp.RawValue
	if err := rlp.DecodeBytes(legacy, &legacyFields); err != nil || len(legacyFields) != 9 {
		t.Fatalf("message preimage is not legacy9: %v", err)
	}
	for i := range legacyFields {
		if !bytes.Equal(fields[i], legacyFields[i]) {
			t.Fatalf("legacy field %d changed", i)
		}
	}
	var decoded Transaction
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.data.SubPublicKey != nil {
		t.Fatal("decoded an untrusted transaction public key")
	}
	if _, err := decoded.HashWithError(); !errors.Is(err, ErrChameleonKeyNotReady) {
		t.Fatalf("missing key did not fail explicitly: %v", err)
	}
	if decoded.hash.Load() != nil {
		t.Fatal("cached an invalid hash while the key was unavailable")
	}
	release := RegisterChameleonKeySource(chainID, func() (*big.Int, bool) { return big.NewInt(8), true })
	defer release()
	got, err := decoded.HashWithError()
	if err != nil || got != expected {
		t.Fatalf("decoded hash differs after trusted key registration: %x/%x %v", got, expected, err)
	}
	if gotRoot, expectedRoot := DeriveSha(Transactions{&decoded}), DeriveSha(Transactions{tx}); gotRoot != expectedRoot {
		t.Fatal("storage round trip changed transaction root")
	}
	if decoded.Size() != common.StorageSize(len(encoded)) {
		t.Fatal("transaction size does not match reduced encoding")
	}
	body, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"subPublicKey", "piHash", "plHash"} {
		if _, found := object[field]; found {
			t.Fatalf("transmitted obsolete JSON extension %s", field)
		}
	}
	var fromJSON Transaction
	if err := json.Unmarshal(body, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.Hash() != expected {
		t.Fatal("JSON round trip lost canonical key resolution")
	}
	oldFormat, err := rlp.EncodeToBytes(append(append([]rlp.RawValue(nil), fields...), rlp.RawValue{0x80}, rlp.RawValue{0x80}, rlp.RawValue{0x80}))
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(oldFormat, &fromJSON); err == nil {
		t.Fatal("accepted incompatible thirteen-field encoding")
	}
}

func TestAlignedTransactionKeySourceReleaseOrder(t *testing.T) {
	chainID := big.NewInt(73332)
	tx := alignedTransaction(t, chainID)
	tx.data.SubPublicKey = nil
	first := RegisterChameleonKeySource(chainID, func() (*big.Int, bool) { return big.NewInt(8), true })
	second := RegisterChameleonKeySource(chainID, func() (*big.Int, bool) { return big.NewInt(9), true })
	defer second()
	defer first()
	first()
	if key := tx.SubPublicKey(); key == nil || key.Cmp(big.NewInt(9)) != 0 {
		t.Fatal("releasing older source disturbed newer source")
	}
	second()
	if key := tx.SubPublicKey(); key != nil {
		t.Fatal("revived a released source")
	}
}

func TestAlignedTransactionSizeInvalidatesWithRandomizer(t *testing.T) {
	tx := NewTransaction(1, common.Address{1}, big.NewInt(1), 50000, big.NewInt(1), []byte("data"))
	before := tx.Size()
	tx.SetChameleonR(big.NewInt(5))
	encoded, err := rlp.EncodeToBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Size() != common.StorageSize(len(encoded)) || tx.Size() == before {
		t.Fatal("randomizer did not invalidate the cached wire size")
	}
}
