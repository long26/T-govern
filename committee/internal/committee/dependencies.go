package committee

import (
	"context"
	"fmt"

	"committee-mvp/internal/attestation"
	"committee-mvp/internal/chain"
)

// Option customizes service dependencies for different runtimes.
type Option func(*Service)

func WithQuoteVerifier(v attestation.Verifier) Option {
	return func(s *Service) {
		s.quoteVerifier = v
	}
}

func WithChainGateway(g chain.Gateway) Option {
	return func(s *Service) {
		s.chainGateway = g
	}
}

func (s *Service) VerifyQuote(ctx context.Context, quote []byte, policy attestation.Policy) (attestation.Result, error) {
	if s.quoteVerifier == nil {
		return attestation.Result{}, fmt.Errorf("quote verifier not configured")
	}
	return s.quoteVerifier.Verify(ctx, quote, policy)
}
