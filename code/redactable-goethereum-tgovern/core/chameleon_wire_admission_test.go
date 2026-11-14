package core

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestDecodedRandomizerTransactionAdmission(t *testing.T) {
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pkCH := big.NewInt(61)
	pool.SetChameleonKeyProvider(&testChameleonProvider{pkCH: pkCH, ready: true})
	tx := transaction(0, 100000, key)
	tx.SetChameleonHashFields(pkCH, big.NewInt(67))
	expected := tx.Hash()
	encoded, err := rlp.EncodeToBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.Transaction
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if errs := pool.AddRemotesSync([]*types.Transaction{&decoded}); errs[0] != nil {
		t.Fatalf("rejected randomizer-only transaction: %v", errs[0])
	}
	if decoded.Hash() != expected || pool.Get(expected) == nil || decoded.SubPublicKey().Cmp(pkCH) != 0 {
		t.Fatal("admission did not bind key before hash lookup")
	}
}

func TestRandomizerJournalLoadsWithInitialKeyProvider(t *testing.T) {
	initial, key := setupChameleonTxPool(t)
	initial.Stop()
	pkCH := big.NewInt(61)
	tx := transaction(0, 100000, key)
	tx.SetChameleonHashFields(pkCH, big.NewInt(67))
	expected := tx.Hash()
	encoded, err := rlp.EncodeToBytes(tx)
	if err != nil {
		t.Fatal(err)
	}
	config := testTxPoolConfig
	config.Journal = filepath.Join(t.TempDir(), "transactions.rlp")
	if err := os.WriteFile(config.Journal, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	restored := NewTxPool(config, params.TestChainConfig, initial.chain, &testChameleonProvider{pkCH: pkCH, ready: true})
	defer restored.Stop()
	if tx := restored.Get(expected); tx == nil || tx.SubPublicKey().Cmp(pkCH) != 0 {
		t.Fatal("journal entry was lost before the canonical key provider was installed")
	}
}
