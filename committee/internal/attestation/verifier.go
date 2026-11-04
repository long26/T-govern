package attestation

import (
	"context"
	"errors"
)

var ErrQuoteRejected = errors.New("quote rejected")

// Policy captures attestation checks to be enforced by a verifier.
type Policy struct {
	Profile             string
	ExpectedMeasurement string
	MinSecurityVersion  uint32
}

// Result contains normalized attestation verification output.
type Result struct {
	Accepted bool
	Reason   string
	Claims   map[string]string
}

// Verifier validates TEE quotes under a policy.
type Verifier interface {
	Verify(ctx context.Context, quote []byte, policy Policy) (Result, error)
}

// NoopVerifier is a placeholder verifier for bootstrap stage.
type NoopVerifier struct {
	RejectEmptyQuote bool
}

func (v NoopVerifier) Verify(_ context.Context, quote []byte, _ Policy) (Result, error) {
	if v.RejectEmptyQuote && len(quote) == 0 {
		return Result{Accepted: false, Reason: "empty quote"}, ErrQuoteRejected
	}
	return Result{
		Accepted: true,
		Reason:   "accepted by noop verifier",
		Claims:   map[string]string{"verifier": "noop"},
	}, nil
}
