package eth

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestValidateRedactedBlockKeepsPlainHeaderHash(t *testing.T) {
	chameleon.P = big.NewInt(23)
	chameleon.Q = big.NewInt(11)
	chameleon.G = big.NewInt(2)
	priv := big.NewInt(3)
	pub := new(big.Int).Exp(chameleon.G, priv, chameleon.P)
	oldR := big.NewInt(5)
	to := common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87")
	oldTx := types.NewTransaction(1, to, big.NewInt(10), 50000, big.NewInt(1), []byte("before"))
	oldTx.SetChameleonHashFields(pub, oldR)
	// [2026-08-30 迁移 nitch] 碰撞锻造与校验共用 32B 域分离摘要消息。
	oldMsg, err := types.TransactionHashMessage(oldTx)
	if err != nil {
		t.Fatal(err)
	}

	newTx := types.NewTransaction(1, to, big.NewInt(10), 50000, big.NewInt(1), []byte("after"))
	newTx.SetChameleonHashFields(pub, oldR)
	newMsg, err := types.TransactionHashMessage(newTx)
	if err != nil {
		t.Fatal(err)
	}
	newTx.SetChameleonR(chameleon.Forge(priv, new(big.Int).SetBytes(oldMsg), oldR, new(big.Int).SetBytes(newMsg)))

	oldBlock := types.NewBlock(&types.Header{
		ParentHash: common.HexToHash("01"),
		Root:       common.HexToHash("02"),
		Difficulty: big.NewInt(1),
		Number:     big.NewInt(2),
		GasLimit:   1000000,
	}, []*types.Transaction{oldTx}, nil, nil)
	newBlock := types.NewBlockWithHeader(oldBlock.Header()).WithBody([]*types.Transaction{newTx}, nil)
	modify := &types.ModifyInfo{Index: 0, ModifyType: 1, DataAfter: []byte("after")}
	if err := validateRedactedBlock(oldBlock, newBlock, modify, nil); err != nil {
		t.Fatalf("valid transaction-level redaction rejected: %v", err)
	}

	badHeader := newBlock.Header()
	badHeader.Extra = []byte("changed")
	badBlock := newBlock.WithSeal(badHeader)
	err = validateRedactedBlock(oldBlock, badBlock, modify, nil)
	if err == nil || !strings.Contains(err.Error(), "block header hash changed") {
		t.Fatalf("header-changing redaction was not rejected: %v", err)
	}
}

func TestRedactionPolicyRejectsNonPayloadModifyTypes(t *testing.T) {
	for _, modifyType := range []uint64{2, 3, 4} {
		err := validatePayloadOnlyModify(&types.ModifyInfo{ModifyType: modifyType})
		if err == nil || !strings.Contains(err.Error(), "only payload redaction") {
			t.Fatalf("ModifyType %d unexpectedly accepted: %v", modifyType, err)
		}
	}
}

func TestGetNewBlockRejectsNonPayloadModificationBeforeBlockLookup(t *testing.T) {
	pm := &ProtocolManager{}
	_, err := pm.getNewBlock(nil, &types.ModifyInfo{ModifyType: 2}, nil)
	if err == nil || !strings.Contains(err.Error(), "only payload redaction") {
		t.Fatalf("non-payload builder request unexpectedly accepted: %v", err)
	}
}

func TestTxModifyGossipRejectsNonPayloadModification(t *testing.T) {
	pm := &ProtocolManager{
		txModifySeen:  make(map[common.Hash]time.Time),
		txModifyCache: make(map[[32]byte]txModifyCacheEntry),
	}
	pm.handleTxModifyGossip(types.ModifyMsg{
		ReqID: [32]byte{1},
		ProposalTx: types.ProposalTx{Msg: types.ModifyInfo{
			ModifyType: 3,
		}},
	}, nil)
	if len(pm.txModifySeen) != 0 || len(pm.txModifyCache) != 0 {
		t.Fatal("non-payload gossip must not be marked or cached")
	}
}
