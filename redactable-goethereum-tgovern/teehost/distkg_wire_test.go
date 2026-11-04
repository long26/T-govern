package teehost

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestDistKGModulusCanonicalHostBoundary(t *testing.T) {
	for _, input := range []string{scalarOrderWireHex, strings.Repeat("0", 32) + scalarOrderWireHex, "0x" + strings.ToUpper(scalarOrderWireHex)} {
		got, err := canonicalModulusHex(input)
		if err != nil || got != scalarOrderWireHex {
			t.Fatalf("normalize %q: %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "17", strings.Repeat("1", 32) + scalarOrderWireHex, strings.Repeat("g", 64), strings.Repeat("0", 64)} {
		if _, err := canonicalModulusHex(input); err == nil {
			t.Fatalf("accepted noncanonical or wrong modulus %q", input)
		}
	}
}

func TestDistKGWireMatchesSharedNullableSlots(t *testing.T) {
	deal := &DistKGDealMessage{
		DealerIndex: 1, Threshold: 1, NumParties: 4, CommitmentsA: []byte("a"), CommitmentB0: []byte("b0"),
		G1Bytes: []byte("g1"), G2Bytes: []byte("g2"), PHex: strings.Repeat("0", 32) + scalarOrderWireHex,
	}
	encoded, err := rlp.EncodeToBytes(DistKGProtocolMessage{RelayTTL: 1, Deal: deal})
	if err != nil {
		t.Fatal(err)
	}
	nilSlot := rlp.RawValue{0xc0}
	commonDeal := []interface{}{uint64(1), uint64(1), uint64(4), []byte("a"), []byte("b0"), []byte("g1"), []byte("g2"), scalarOrderWireHex}
	golden, err := rlp.EncodeToBytes([]interface{}{commonDeal, nilSlot, nilSlot, nilSlot, nilSlot, nilSlot, nilSlot, uint64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, golden) {
		t.Fatalf("DKG wire differs from shared slots: %x vs %x", encoded, golden)
	}
	if len(deal.PHex) != 96 {
		t.Fatal("wire conversion mutated enclave output")
	}
	var decoded DistKGProtocolMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil || decoded.Deal == nil || decoded.Deal.PHex != scalarOrderWireHex {
		t.Fatalf("invalid canonical round trip: %v", err)
	}
	bad := distKGProtocolMessageRLP{
		Deal: decoded.Deal, Share: &DistKGShareMessage{FromIndex: 1, ToIndex: 2},
		Response: nilSlot, InputRequest: nilSlot, InputResponse: nilSlot,
	}
	encoded, err = rlp.EncodeToBytes(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(encoded, &decoded); err == nil {
		t.Fatal("accepted ambiguous multiple DKG payloads")
	}
	bad.Deal = nil
	bad.Response = rlp.RawValue{0x80}
	encoded, err = rlp.EncodeToBytes(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(encoded, &decoded); err == nil {
		t.Fatal("accepted unsupported reserved DKG slot")
	}
}
