package eth

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestParseCommitteePubKeyBytesConcatenatedHex(t *testing.T) {
	want := make([]byte, 128)
	for i := range want {
		want[i] = byte(i)
	}
	got, err := parseCommitteePubKeyBytes("0x" + hex.EncodeToString(want))
	if err != nil {
		t.Fatalf("parseCommitteePubKeyBytes failed: %v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("unexpected pubkey bytes:\n got %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
}

func TestParseCommitteePubKeyBytesJSONArray(t *testing.T) {
	coords := make([]string, 4)
	want := make([]byte, 0, 128)
	for i := 0; i < 4; i++ {
		chunk := strings.Repeat(fmt.Sprintf("%02x", byte(i+1)), 32)
		coords[i] = "0x" + chunk
		raw, _ := hex.DecodeString(chunk)
		want = append(want, raw...)
	}
	input := fmt.Sprintf(`["%s","%s","%s","%s"]`, coords[0], coords[1], coords[2], coords[3])
	got, err := parseCommitteePubKeyBytes(input)
	if err != nil {
		t.Fatalf("parseCommitteePubKeyBytes failed: %v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("unexpected pubkey bytes:\n got %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
}

func TestParseCommitteePubKeyBytesRejectsWrongLength(t *testing.T) {
	if _, err := parseCommitteePubKeyBytes("0x1234"); err == nil {
		t.Fatal("expected parseCommitteePubKeyBytes to reject short hex input")
	}
}
