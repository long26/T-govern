package teehost

import (
	"fmt"
	"math/big"
	"strings"
)

const scalarOrderWireHex = "73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001"

// canonicalModulusHex normalizes the host's padded MIRACL output without changing the enclave.
func canonicalModulusHex(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(value) != 64 && len(value) != 96 {
		return "", fmt.Errorf("invalid scalar modulus hex width %d", len(value))
	}
	if len(value) == 96 {
		if value[:32] != strings.Repeat("0", 32) {
			return "", fmt.Errorf("nonzero scalar modulus padding")
		}
		value = value[32:]
	}
	parsed, ok := new(big.Int).SetString(value, 16)
	order, _ := new(big.Int).SetString(scalarOrderWireHex, 16)
	if !ok || parsed.Cmp(order) != 0 {
		return "", fmt.Errorf("unexpected scalar modulus")
	}
	return scalarOrderWireHex, nil
}
