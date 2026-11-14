package teehost

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestDKRSDealSharedFieldsAndRequiredB0(t *testing.T) {
	b0 := bytes.Repeat([]byte{0x22}, 192)
	deal := DKRSDealMessage{
		DealerIndex: 1, OldThreshold: 1, OldNumParties: 4, NewThreshold: 1, NewNumParties: 4,
		CommitmentsA: bytes.Repeat([]byte{0x11}, 192), CommitmentB0: b0,
	}
	encoded, err := rlp.EncodeToBytes(deal)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil || len(fields) != 7 {
		t.Fatalf("deal is not six shared fields plus B0: %v", err)
	}
	shared, err := rlp.EncodeToBytes([]interface{}{uint64(1), uint64(1), uint64(4), uint64(1), uint64(4), deal.CommitmentsA})
	if err != nil {
		t.Fatal(err)
	}
	var expected []rlp.RawValue
	if err := rlp.DecodeBytes(shared, &expected); err != nil {
		t.Fatal(err)
	}
	for i := range expected {
		if !bytes.Equal(fields[i], expected[i]) {
			t.Fatalf("shared field %d differs", i)
		}
	}
	var decoded DKRSDealMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.CommitmentB0, b0) {
		t.Fatal("removed or changed the enclave's G2 commitment")
	}
	state := &DKRSSessionState{
		Config: DKRSProtocolConfig{ReceiverIndex: 2, OldThreshold: 1, OldNumParties: 4, NewThreshold: 1, NewNumParties: 4, OldQual: []int{1}},
		Deals:  map[int]*DKRSDealMessage{1: &decoded},
		Shares: map[int]*DKRSShareMessage{1: {FromIndex: 1, ToIndex: 2, Payload: []byte("encrypted-share")}},
	}
	if !validDKRSDealForSession(state, &decoded) {
		t.Fatal("valid shared session parameters rejected")
	}
	decoded.OldThreshold = 2
	if validDKRSDealForSession(state, &decoded) {
		t.Fatal("accepted another old threshold")
	}
	decoded.OldThreshold = 1
	decoded.OldNumParties = 3
	if validDKRSDealForSession(state, &decoded) {
		t.Fatal("accepted another old committee size")
	}
	decoded.OldNumParties = 4
	req, err := buildDKRSExtractRequest(state, []int{1})
	if err != nil || !bytes.Equal(req.B0Data, b0) {
		t.Fatalf("B0 did not reach extraction unchanged: %v", err)
	}
}
