package types

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestReshareRecoveryRequestTEELayout(t *testing.T) {
	request := ReshareInputRequest{
		DealerIndex: big.NewInt(1), ReceiverIndex: big.NewInt(2), NeedDeal: true, Nonce: [32]byte{3}, Attempt: 1,
	}
	encoded, err := rlp.EncodeToBytes(request)
	if err != nil {
		t.Fatal(err)
	}
	// Same requester-first field order, nonce width and primitive types as TEE recovery.
	golden, err := rlp.EncodeToBytes([]interface{}{uint64(2), uint64(1), true, false, [32]byte{3}, uint64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, golden) || len(encoded) != 39 {
		t.Fatalf("request differs from TEE layout: %x vs %x", encoded, golden)
	}
	var decoded ReshareInputRequest
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DealerIndex.Cmp(request.DealerIndex) != 0 || decoded.ReceiverIndex.Cmp(request.ReceiverIndex) != 0 || decoded.Nonce != request.Nonce {
		t.Fatal("request round trip changed roles or nonce")
	}
	legacy, err := rlp.EncodeToBytes([]interface{}{uint64(2), uint64(1), true, false, uint64(3), uint64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(legacy, &decoded); err == nil {
		t.Fatal("accepted short legacy nonce")
	}
}

func TestReshareRecoveryResponsePresenceAndFixed48(t *testing.T) {
	for _, flags := range []struct{ deal, share bool }{{true, false}, {false, true}, {true, true}, {false, false}} {
		response := ReshareInputResponse{
			DealerIndex: big.NewInt(1), ReceiverIndex: big.NewInt(2), Nonce: [32]byte{3}, HasDeal: flags.deal, HasShare: flags.share,
		}
		if flags.deal {
			response.Deal = &chameleon.DKRSDeal{
				DealerIndex: big.NewInt(1), OldThreshold: big.NewInt(1), OldNumParties: big.NewInt(4),
				NewThreshold: big.NewInt(1), NewNumParties: big.NewInt(4), A: []byte("commitments"),
			}
		}
		if flags.share {
			response.Share = &chameleon.DKRSShare{FromIndex: big.NewInt(1), ToIndex: big.NewInt(2), Share: big.NewInt(7)}
		}
		encoded, err := rlp.EncodeToBytes(response)
		if err != nil {
			t.Fatal(err)
		}
		var wire reshareInputResponseWire
		if err := rlp.DecodeBytes(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.RequesterIndex != 2 || wire.DealerIndex != 1 || wire.Nonce != response.Nonce || wire.HasDeal != flags.deal || wire.HasShare != flags.share {
			t.Fatal("response role/nonce/presence fields changed")
		}
		if flags.share && len(wire.Share.Payload) != 48 {
			t.Fatalf("recovery scalar length %d", len(wire.Share.Payload))
		}
		var decoded ReshareInputResponse
		if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.HasDeal != (decoded.Deal != nil) || decoded.HasShare != (decoded.Share != nil) {
			t.Fatal("response presence does not match payload")
		}
		roundTrip, err := rlp.EncodeToBytes(decoded)
		if err != nil || !bytes.Equal(roundTrip, encoded) {
			t.Fatalf("noncanonical round trip: %v", err)
		}
	}
	invalid := ReshareInputResponse{HasDeal: true}
	if _, err := rlp.EncodeToBytes(invalid); err == nil {
		t.Fatal("accepted presence flag without payload")
	}
	wire := reshareInputResponseWire{Share: reshareShareWire{Payload: []byte{7}}}
	encoded, err := rlp.EncodeToBytes(wire)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ReshareInputResponse
	if err := rlp.DecodeBytes(encoded, &decoded); err == nil {
		t.Fatal("accepted payload without presence flag")
	}
}
