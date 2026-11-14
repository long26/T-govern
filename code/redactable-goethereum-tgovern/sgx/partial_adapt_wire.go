package sgx

import "fmt"

// partialAdaptResultFromBuffers preserves the unchanged enclave's full scalar output.
func partialAdaptResultFromBuffers(contribution, challenge, response, y, sigma []byte) (*SGXPartialAdaptResult, error) {
	if len(contribution) != 96 || len(challenge) < 48 || len(response) < 48 || len(y) != 96 || len(sigma) != 65 {
		return nil, fmt.Errorf("invalid partial adaptation output widths: %d/%d/%d/%d/%d", len(contribution), len(challenge), len(response), len(y), len(sigma))
	}
	return &SGXPartialAdaptResult{
		Contribution:   append([]byte(nil), contribution...),
		ProofChallenge: append([]byte(nil), challenge[:48]...),
		ProofResponse:  append([]byte(nil), response[:48]...),
		Y:              append([]byte(nil), y...),
		Sigma:          append([]byte(nil), sigma...),
	}, nil
}
