package sgx

import "testing"

func TestPartialAdaptResultWireWidths(t *testing.T) {
	challenge, response := make([]byte, 64), make([]byte, 64)
	challenge[47], response[47] = 7, 8
	result, err := partialAdaptResultFromBuffers(make([]byte, 96), challenge, response, make([]byte, 96), make([]byte, 65))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ProofChallenge) != 48 || result.ProofChallenge[47] != 7 || len(result.ProofResponse) != 48 || result.ProofResponse[47] != 8 {
		t.Fatal("host truncated an enclave scalar instead of preserving fixed48")
	}
	challenge[47] = 9
	if result.ProofChallenge[47] != 7 {
		t.Fatal("host output aliases the mutable enclave buffer")
	}
	if _, err := partialAdaptResultFromBuffers(make([]byte, 96), make([]byte, 32), response, make([]byte, 96), make([]byte, 65)); err == nil {
		t.Fatal("accepted the legacy truncated challenge width")
	}
}
