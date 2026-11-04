package eth

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/core/types"
)

func testModifyProposal(relay types.RelayMetadata) types.RedactTx {
	return types.RedactTx{
		BlockNum:    big.NewInt(11),
		CreateTime:  time.Unix(100, 200),
		ProposalID:  "proposal-1",
		CollectorID: "collector",
		OldMessage:  chameleon.Message("old"),
		NewMessage:  chameleon.Message("new"),
		HashOutput:  &chameleon.HashOutput{Hash: big.NewInt(13), CheckValue: big.NewInt(14)},
		Msg:         types.ModifiedMessage{BlockNum: "11", Index: 0, ModifyType: 1, DataAfter: "after"},
		Relay:       relay,
	}
}

func TestModifyProposalFingerprintIgnoresRelayMetadata(t *testing.T) {
	first, err := modifyProposalFingerprint(testModifyProposal(types.RelayMetadata{TTL: 8, Hop: 0}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := modifyProposalFingerprint(testModifyProposal(types.RelayMetadata{
		MessageID: "other", Origin: "node", TTL: 1, Hop: 7,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("relay metadata changed proposal fingerprint: %s != %s", first, second)
	}
}

func TestModifySeenCacheDropsDuplicate(t *testing.T) {
	pm := &ProtocolManager{}
	msg := testModifyProposal(types.RelayMetadata{})
	_, first := pm.noteModifyProposalSeen(msg)
	_, second := pm.noteModifyProposalSeen(msg)
	if !first || second {
		t.Fatalf("first=%v second=%v", first, second)
	}
}
