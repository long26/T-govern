package types

import (
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

const reshareWireSessionPrefix = "nitch-dkrs-wire-v1:"

// ReshareWireSessionID preserves decoded IDs and domain-separates arbitrary local IDs.
func ReshareWireSessionID(session string) [32]byte {
	if IsReshareWireSessionID(session) {
		decoded, _ := hex.DecodeString(strings.TrimPrefix(session, reshareWireSessionPrefix))
		var id [32]byte
		copy(id[:], decoded)
		return id
	}
	if session == "" {
		return [32]byte{}
	}
	return crypto.Keccak256Hash([]byte("NITCH_DKRS_SESSION_V1"), []byte(session))
}

func IsReshareWireSessionID(session string) bool {
	if !strings.HasPrefix(session, reshareWireSessionPrefix) {
		return false
	}
	value, err := hex.DecodeString(strings.TrimPrefix(session, reshareWireSessionPrefix))
	return err == nil && len(value) == 32
}

// ReshareWireSessionName names a wire ID without claiming to recover its local source string.
func ReshareWireSessionName(session string) string {
	id := ReshareWireSessionID(session)
	return reshareWireSessionPrefix + hex.EncodeToString(id[:])
}

func reshareWireSessionName(id [32]byte) string {
	return reshareWireSessionPrefix + hex.EncodeToString(id[:])
}
