package attestation

import (
	"context"
	"errors"
	"testing"
)

func TestNoopVerifierAcceptsQuote(t *testing.T) {
	v := NoopVerifier{RejectEmptyQuote: true}
	res, err := v.Verify(context.Background(), []byte("quote-bytes"), Policy{Profile: "sgx"})
	if err != nil {
		t.Fatalf("verify quote: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected accepted result")
	}
	if res.Claims["verifier"] != "noop" {
		t.Fatalf("unexpected claims: %v", res.Claims)
	}
}

func TestNoopVerifierRejectsEmptyQuote(t *testing.T) {
	v := NoopVerifier{RejectEmptyQuote: true}
	res, err := v.Verify(context.Background(), nil, Policy{})
	if !errors.Is(err, ErrQuoteRejected) {
		t.Fatalf("expected ErrQuoteRejected, got=%v", err)
	}
	if res.Accepted {
		t.Fatalf("expected rejected result")
	}
}
