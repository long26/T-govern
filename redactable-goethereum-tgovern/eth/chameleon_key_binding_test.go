package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestPersistedChameleonKeyRestoresHistoricalHashes(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	chainID := big.NewInt(73333)
	genesis := common.Hash{1}
	binding := chameleonKeyBinding{Genesis: genesis, ChainID: chainID, Key: []byte{8}, ActivationBlock: 10}
	encodedBinding, err := rlp.EncodeToBytes(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Put(chameleonKeyBindingKey, encodedBinding); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(types.NewTransaction(1, common.Address{1}, big.NewInt(2), 50000, big.NewInt(1), []byte("data")), types.NewEIP155Signer(chainID), key)
	if err != nil {
		t.Fatal(err)
	}
	tx.SetChameleonHashFields(big.NewInt(8), big.NewInt(5))
	expectedHash := tx.Hash()
	expectedRoot := types.DeriveSha(types.Transactions{tx})
	block := types.NewBlockWithHeader(&types.Header{Number: big.NewInt(11), Difficulty: big.NewInt(1), TxHash: expectedRoot}).WithBody([]*types.Transaction{tx}, nil)
	rawdb.WriteBlock(db, block)
	loaded := rawdb.ReadBlock(db, block.Hash(), 11)
	if loaded == nil {
		t.Fatal("historical block missing")
	}
	if _, err := loaded.Transactions()[0].HashWithError(); err == nil {
		t.Fatal("decoded historical transaction used an untrusted or absent key")
	}
	restored, err := loadChameleonKeyBinding(db, genesis, chainID)
	if err != nil || restored == nil {
		t.Fatalf("load binding: %v", err)
	}
	release := registerPersistedChameleonKey(restored)
	defer release()
	gotHash, err := loaded.Transactions()[0].HashWithError()
	if err != nil || gotHash != expectedHash || types.DeriveSha(loaded.Transactions()) != expectedRoot {
		t.Fatalf("historical identity/root changed after key restoration: %v", err)
	}
	if _, err := loadChameleonKeyBinding(db, common.Hash{2}, chainID); err == nil {
		t.Fatal("accepted binding from another genesis")
	}
	if _, err := loadChameleonKeyBinding(db, genesis, big.NewInt(73334)); err == nil {
		t.Fatal("accepted binding from another chain ID")
	}
}
