package core

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon/txstamp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

type testChameleonProvider struct {
	pkCH            *big.Int
	ready           bool
	activationBlock uint64
}

func (p *testChameleonProvider) CurrentPkCH() (*big.Int, bool) {
	if !p.ready || p.pkCH == nil {
		return nil, false
	}
	return new(big.Int).Set(p.pkCH), true
}

func (p *testChameleonProvider) PolicyPkCHForBlock(number uint64) (*big.Int, bool) {
	if !p.ready || p.pkCH == nil || number <= p.activationBlock {
		return nil, false
	}
	return new(big.Int).Set(p.pkCH), true
}

func TestValidateChameleonFields(t *testing.T) {
	pkCH := big.NewInt(17)
	r := big.NewInt(19)
	tx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	if err := validateChameleonFields(tx, pkCH, true); !errors.Is(err, ErrMissingChameleonFields) {
		t.Fatalf("nil chameleon fields error = %v, want ErrMissingChameleonFields", err)
	}
	if err := validateChameleonFields(tx, nil, false); err != nil {
		t.Fatalf("legacy transaction rejected before pkCH ready: %v", err)
	}

	tx.SetChameleonHashFields(pkCH, r)
	if err := validateChameleonFields(tx, pkCH, true); err != nil {
		t.Fatalf("canonical pkCH fields rejected: %v", err)
	}

	tx.SetChameleonHashFields(big.NewInt(23), r)
	if err := validateChameleonFields(tx, pkCH, true); !errors.Is(err, ErrInvalidChameleonPublicKey) {
		t.Fatalf("rogue pk error = %v, want ErrInvalidChameleonPublicKey", err)
	}

	tx.SetChameleonHashFields(pkCH, nil)
	if err := validateChameleonFields(tx, pkCH, true); !errors.Is(err, ErrInvalidChameleonFields) {
		t.Fatalf("partial fields error = %v, want ErrInvalidChameleonFields", err)
	}
}

func TestStampIfReadySetsCanonicalPkCH(t *testing.T) {
	pkCH := big.NewInt(29)

	tx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	if err := txstamp.StampIfReady(tx, &testChameleonProvider{pkCH: pkCH, ready: true}); err != nil {
		t.Fatalf("stamp transaction: %v", err)
	}
	if tx.SubPublicKey() == nil || tx.SubPublicKey().Cmp(pkCH) != 0 {
		t.Fatalf("SubPublicKey = %v, want pkCH %v", tx.SubPublicKey(), pkCH)
	}
	if tx.ChameleonR() == nil || tx.ChameleonR().Sign() == 0 {
		t.Fatalf("ChameleonR was not generated")
	}
}

func TestTxPoolRejectsMissingChameleonFieldsWhenPkCHReady(t *testing.T) {
	pkCH := big.NewInt(29)
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pool.SetChameleonKeyProvider(&testChameleonProvider{pkCH: pkCH, ready: true})

	tx := transaction(0, 100000, key)
	if errs := pool.AddRemotesSync([]*types.Transaction{tx}); !errors.Is(errs[0], ErrMissingChameleonFields) {
		t.Fatalf("add missing chameleon transaction error = %v, want ErrMissingChameleonFields", errs[0])
	}
	if tx.SubPublicKey() != nil || tx.ChameleonR() != nil {
		t.Fatalf("txpool unexpectedly stamped transaction: spk=%v r=%v", tx.SubPublicKey(), tx.ChameleonR())
	}
}

func TestTxPoolLeavesLegacyTxWhenPkCHNotReady(t *testing.T) {
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pool.SetChameleonKeyProvider(&testChameleonProvider{ready: false})

	tx := transaction(0, 100000, key)
	if errs := pool.AddRemotesSync([]*types.Transaction{tx}); errs[0] != nil {
		t.Fatalf("add transaction: %v", errs[0])
	}
	if tx.SubPublicKey() != nil || tx.ChameleonR() != nil {
		t.Fatalf("pkCH-not-ready transaction was stamped: spk=%v r=%v", tx.SubPublicKey(), tx.ChameleonR())
	}
}

func TestTxPoolRejectsRoguePkCH(t *testing.T) {
	pkCH := big.NewInt(31)
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pool.SetChameleonKeyProvider(&testChameleonProvider{pkCH: pkCH, ready: true})

	tx := transaction(0, 100000, key)
	tx.SetChameleonHashFields(big.NewInt(37), big.NewInt(41))
	if errs := pool.AddRemotesSync([]*types.Transaction{tx}); !errors.Is(errs[0], ErrInvalidChameleonPublicKey) {
		t.Fatalf("add rogue transaction error = %v, want ErrInvalidChameleonPublicKey", errs[0])
	}
}

func TestTxPoolAcceptsExplicitCanonicalPkCH(t *testing.T) {
	pkCH := big.NewInt(61)
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pool.SetChameleonKeyProvider(&testChameleonProvider{pkCH: pkCH, ready: true})

	tx := transaction(0, 100000, key)
	tx.SetChameleonHashFields(pkCH, big.NewInt(67))
	if errs := pool.AddRemotesSync([]*types.Transaction{tx}); errs[0] != nil {
		t.Fatalf("add canonical chameleon transaction: %v", errs[0])
	}
	if tx.SubPublicKey().Cmp(pkCH) != 0 || tx.ChameleonR().Cmp(big.NewInt(67)) != 0 {
		t.Fatalf("canonical chameleon fields were unexpectedly modified")
	}
}

func TestTxPoolRejectsChameleonFieldsBeforePkCH(t *testing.T) {
	pool, key := setupChameleonTxPool(t)
	defer pool.Stop()
	pool.SetChameleonKeyProvider(&testChameleonProvider{ready: false})

	tx := transaction(0, 100000, key)
	tx.SetChameleonHashFields(big.NewInt(37), big.NewInt(41))
	if errs := pool.AddRemotesSync([]*types.Transaction{tx}); !errors.Is(errs[0], ErrInvalidChameleonPublicKey) {
		t.Fatalf("add pre-pkCH chameleon transaction error = %v, want ErrInvalidChameleonPublicKey", errs[0])
	}
}

func TestBlockChameleonPolicy(t *testing.T) {
	pkCH := big.NewInt(43)
	goodTx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	goodTx.SetChameleonHashFields(pkCH, big.NewInt(47))
	goodBlock := types.NewBlock(&types.Header{Number: big.NewInt(2)}, []*types.Transaction{goodTx}, nil, nil)
	if err := validateBlockChameleonFields(goodBlock, pkCH, true); err != nil {
		t.Fatalf("canonical block rejected: %v", err)
	}

	legacyTx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	legacyBlock := types.NewBlock(&types.Header{Number: big.NewInt(1)}, []*types.Transaction{legacyTx}, nil, nil)
	if err := validateBlockChameleonFields(legacyBlock, nil, false); err != nil {
		t.Fatalf("legacy block rejected before enforcement: %v", err)
	}
	if err := validateBlockChameleonFields(legacyBlock, pkCH, true); !errors.Is(err, ErrMissingChameleonFields) {
		t.Fatalf("legacy block error = %v, want ErrMissingChameleonFields", err)
	}

	rogueTx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	rogueTx.SetChameleonHashFields(big.NewInt(53), big.NewInt(59))
	rogueBlock := types.NewBlock(&types.Header{Number: big.NewInt(1)}, []*types.Transaction{rogueTx}, nil, nil)
	if err := validateBlockChameleonFields(rogueBlock, nil, false); !errors.Is(err, ErrInvalidChameleonPublicKey) {
		t.Fatalf("pre-pkCH chameleon block error = %v, want ErrInvalidChameleonPublicKey", err)
	}
	if err := validateBlockChameleonFields(rogueBlock, pkCH, true); !errors.Is(err, ErrInvalidChameleonPublicKey) {
		t.Fatalf("rogue block error = %v, want ErrInvalidChameleonPublicKey", err)
	}

	partialTx := types.NewTransaction(0, common.Address{}, big.NewInt(1), params.TxGas, big.NewInt(1), nil)
	partialTx.SetChameleonHashFields(big.NewInt(71), nil)
	partialBlock := types.NewBlock(&types.Header{Number: big.NewInt(1)}, []*types.Transaction{partialTx}, nil, nil)
	if err := validateBlockChameleonFields(partialBlock, nil, false); !errors.Is(err, ErrInvalidChameleonFields) {
		t.Fatalf("partial legacy block error = %v, want ErrInvalidChameleonFields", err)
	}
}

func setupChameleonTxPool(t *testing.T) (*TxPool, *ecdsa.PrivateKey) {
	t.Helper()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 10000000, new(event.Feed)}
	key, _ := crypto.GenerateKey()
	statedb.SetBalance(crypto.PubkeyToAddress(key.PublicKey), new(big.Int).SetUint64(params.Ether))
	return NewTxPool(testTxPoolConfig, params.TestChainConfig, blockchain), key
}
