package types

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestReshareWireSessionIDsAreNotTruncated(t *testing.T) {
	a := "first-prefix:" + strings.Repeat("same-suffix", 20)
	b := "other-prefix:" + strings.Repeat("same-suffix", 20)
	if ReshareWireSessionID(a) == ReshareWireSessionID(b) {
		t.Fatal("truncated distinct long operation names to the same ID")
	}
	if ReshareWireSessionID(a) != ReshareWireSessionID(ReshareWireSessionName(a)) {
		t.Fatal("decoded session ID was hashed a second time")
	}
}

func TestReshareTaggedEnvelopeHasSession32AndSingleTTL(t *testing.T) {
	msg := ReshareMessage{SessionID: "golden", RelayTTL: 1, InputRequest: &ReshareInputRequest{Nonce: [32]byte{9}, Attempt: 1}}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil || len(fields) != 9 {
		t.Fatalf("envelope fields: %v", err)
	}
	var id []byte
	if err := rlp.DecodeBytes(fields[0], &id); err != nil || len(id) != 32 {
		t.Fatalf("session is not32 bytes: %v", err)
	}
	if !bytes.Equal(fields[1], []byte{1}) || !bytes.Equal(fields[2], []byte{5}) {
		t.Fatal("TTL/kind do not match the TEE envelope layout")
	}
	var decoded ReshareMessage
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if ReshareWireSessionID(decoded.SessionID) != ReshareWireSessionID(msg.SessionID) || decoded.InputRequest.Nonce != msg.InputRequest.Nonce {
		t.Fatal("envelope round trip changed operation or nonce")
	}
}
