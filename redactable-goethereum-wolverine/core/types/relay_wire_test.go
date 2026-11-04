package types

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestRelayWireContainsOnlyTTL(t *testing.T) {
	meta := RelayMetadata{MessageID: "not-on-wire", Origin: "local-only", TTL: 1, Hop: 12}
	encoded, err := rlp.EncodeToBytes(meta)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, []byte{1}) {
		t.Fatalf("relay wire = %x, want single TTL byte", encoded)
	}
	var decoded RelayMetadata
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != (RelayMetadata{TTL: 1}) {
		t.Fatalf("decoded remote bookkeeping: %+v", decoded)
	}
}

func TestDistKGWireMinimalRelay(t *testing.T) {
	msg := DistKGMessage{
		Share: &chameleon.DistKGShare{FromIndex: big.NewInt(1), ToIndex: big.NewInt(2), Share: big.NewInt(3)},
		Relay: RelayMetadata{MessageID: "message", Origin: "node", TTL: 1, Hop: 9},
	}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 8 || !bytes.Equal(fields[7], []byte{1}) {
		t.Fatalf("DKG does not end in one TTL field: %x", encoded)
	}
	var decoded DistKGMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Relay != (RelayMetadata{TTL: 1}) || decoded.Share.Share.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("unexpected decoded message: %+v", decoded)
	}
}

func TestReshareWireOmitsDuplicateRelayFields(t *testing.T) {
	msg := ReshareMessage{
		SessionID: "session", VerifyKey: &chameleon.DKRSVerifyKey{Index: big.NewInt(2), VerificationKey: []byte("vk")},
		RelayTTL: 1, Relay: RelayMetadata{TTL: 1, MessageID: "local", Origin: "local", Hop: 1},
	}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 9 || !bytes.Equal(fields[1], []byte{1}) {
		t.Fatalf("reshare does not contain one relay field: %x", encoded)
	}
	var decoded ReshareMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Relay != (RelayMetadata{TTL: 1}) || decoded.RelayTTL != 1 || decoded.SessionID != ReshareWireSessionName(msg.SessionID) {
		t.Fatalf("unexpected relay round trip: %+v", decoded)
	}
}
