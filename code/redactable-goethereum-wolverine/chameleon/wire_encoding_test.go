package chameleon

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestScalarShareWireFixed48(t *testing.T) {
	for _, scalar := range []*big.Int{new(big.Int), big.NewInt(1), new(big.Int).Sub(scalarWireOrder, big.NewInt(1))} {
		values := []interface{}{
			DistKGShare{big.NewInt(1), big.NewInt(2), scalar},
			DKRSShare{big.NewInt(1), big.NewInt(2), scalar},
			DistKGResponse{big.NewInt(1), big.NewInt(2), scalar},
			DKRSResponse{big.NewInt(1), big.NewInt(2), scalar},
		}
		var first []byte
		for _, value := range values {
			encoded, err := rlp.EncodeToBytes(value)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				FromIndex *big.Int
				ToIndex   *big.Int
				Share     []byte
			}
			if err := rlp.DecodeBytes(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Share) != ScalarWireSize || new(big.Int).SetBytes(wire.Share).Cmp(scalar) != 0 {
				t.Fatalf("%T: share is not fixed48: %x", value, wire.Share)
			}
			if first != nil && !bytes.Equal(first, encoded) {
				t.Fatalf("share and response encoders disagree: %T", value)
			}
			first = encoded
		}
		for _, decoded := range []interface{}{new(DistKGShare), new(DKRSShare), new(DistKGResponse), new(DKRSResponse)} {
			if err := rlp.DecodeBytes(first, decoded); err != nil {
				t.Fatalf("%T: %v", decoded, err)
			}
			roundTrip, err := rlp.EncodeToBytes(decoded)
			if err != nil || !bytes.Equal(first, roundTrip) {
				t.Fatalf("%T: noncanonical round trip: %v", decoded, err)
			}
		}
	}
}

func TestScalarShareWireRejectsMalformed(t *testing.T) {
	for _, scalar := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Set(scalarWireOrder), new(big.Int).Lsh(big.NewInt(1), 512)} {
		if _, err := rlp.EncodeToBytes(DistKGShare{big.NewInt(1), big.NewInt(2), scalar}); err == nil {
			t.Fatalf("accepted invalid scalar %v", scalar)
		}
	}
	for _, length := range []int{0, 1, 32, 47, 49} {
		encoded, err := rlp.EncodeToBytes([]interface{}{uint64(1), uint64(2), make([]byte, length)})
		if err != nil {
			t.Fatal(err)
		}
		for _, decoded := range []interface{}{new(DistKGShare), new(DKRSShare), new(DistKGResponse), new(DKRSResponse)} {
			if err := rlp.DecodeBytes(encoded, decoded); err == nil {
				t.Fatalf("%T accepted %d-byte scalar", decoded, length)
			}
		}
	}
	var outOfRange scalarWire
	scalarWireOrder.FillBytes(outOfRange[:])
	encoded, err := rlp.EncodeToBytes(shareWire{big.NewInt(1), big.NewInt(2), outOfRange})
	if err != nil {
		t.Fatal(err)
	}
	var decoded DKRSShare
	if err := rlp.DecodeBytes(encoded, &decoded); err == nil {
		t.Fatal("accepted field-order scalar instead of rejecting it")
	}
}

func TestAdaptationWireCanonicalPointAndProof(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := SimulatedDistKG(pp, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	oldMessage, newMessage := Message("before"), Message("after")
	share, err := PartialAdapt(keys[0], oldMessage, newMessage)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rlp.EncodeToBytes(share)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil || len(fields) != 3 {
		t.Fatalf("invalid share fields: %v", err)
	}
	var point []byte
	if err := rlp.DecodeBytes(fields[1], &point); err != nil || len(point) != 96 {
		t.Fatalf("point is not canonical96: length=%d err=%v", len(point), err)
	}
	var proof [][]byte
	if err := rlp.DecodeBytes(fields[2], &proof); err != nil || len(proof) != 2 || len(proof[0]) != 48 || len(proof[1]) != 48 {
		t.Fatalf("proof is not two fixed48 scalars: %v", err)
	}
	var decoded AdaptationShare
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !PartialVerify(keys[0], &decoded, oldMessage, newMessage) {
		t.Fatal("wire round trip invalidated the adaptation proof")
	}
	if PartialVerify(keys[0], &decoded, oldMessage, Message("different")) {
		t.Fatal("accepted proof for a different message")
	}
	bad, err := rlp.EncodeToBytes([]interface{}{share.Index, bytes.Repeat([]byte{0xff}, 96), share.Proof})
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(bad, &decoded); err == nil {
		t.Fatal("accepted invalid curve point")
	}
}

func TestProofWireRejectsOutOfRange(t *testing.T) {
	for _, p := range []NIZKProof{{nil, big.NewInt(1)}, {big.NewInt(1), big.NewInt(-1)}, {new(big.Int).Set(scalarWireOrder), big.NewInt(1)}} {
		if _, err := rlp.EncodeToBytes(p); err == nil {
			t.Fatalf("accepted invalid proof %+v", p)
		}
	}
	var bad proofWire
	scalarWireOrder.FillBytes(bad.Z[:])
	encoded, err := rlp.EncodeToBytes(bad)
	if err != nil {
		t.Fatal(err)
	}
	var proof NIZKProof
	if err := rlp.DecodeBytes(encoded, &proof); err == nil {
		t.Fatal("accepted out-of-range proof response")
	}
}
