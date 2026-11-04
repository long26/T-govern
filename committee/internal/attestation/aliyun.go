package attestation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const quotePathPlaceholder = "{quote}"

// AliyunVerifier validates SGX quotes with the Alibaba Cloud compatible Intel
// DCAP SDK, then parses the SGX measurements locally.
type AliyunVerifier struct {
	Command string
	Args    []string
	Timeout time.Duration
}

func (v AliyunVerifier) Verify(ctx context.Context, quote []byte, policy Policy) (Result, error) {
	if len(quote) == 0 {
		return Result{Accepted: false, Reason: "empty quote"}, ErrQuoteRejected
	}
	measurements, err := ParseSGXQuoteMeasurements(quote)
	if err != nil {
		return Result{Accepted: false, Reason: err.Error()}, ErrQuoteRejected
	}
	claims := map[string]string{
		"mrenclave": measurements.MREnclave,
		"mrsigner":  measurements.MRSigner,
		"isvsvn":    fmt.Sprintf("%d", measurements.ISVSVN),
	}

	if policy.MinSecurityVersion > 0 && uint32(measurements.ISVSVN) < policy.MinSecurityVersion {
		return Result{Accepted: false, Reason: "isvsvn below minimum", Claims: claims}, ErrQuoteRejected
	}
	if expected := normalizeClaim(policy.ExpectedMeasurement); expected != "" && measurements.MREnclave != expected {
		return Result{Accepted: false, Reason: "mrenclave mismatch", Claims: claims}, ErrQuoteRejected
	}

	if err := v.verifyQuoteValidity(ctx, quote); err != nil {
		return Result{Accepted: false, Reason: err.Error(), Claims: claims}, ErrQuoteRejected
	}
	return Result{Accepted: true, Reason: "accepted by aliyun dcap verifier", Claims: claims}, nil
}

func (v AliyunVerifier) verifyQuoteValidity(ctx context.Context, quote []byte) error {
	if strings.TrimSpace(v.Command) == "" {
		return VerifyQuoteWithAliyunSDK(ctx, quote)
	}
	return v.verifyWithCommand(ctx, quote)
}

func (v AliyunVerifier) verifyWithCommand(ctx context.Context, quote []byte) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		timeout := v.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	tmp, err := os.CreateTemp("", "committee-sgx-quote-*.bin")
	if err != nil {
		return fmt.Errorf("create quote temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(quote); err != nil {
		tmp.Close()
		return fmt.Errorf("write quote temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close quote temp file: %w", err)
	}

	args, usedPlaceholder := replaceQuotePath(v.Args, tmpName)
	if !usedPlaceholder {
		args = append(args, tmpName)
	}

	cmd := exec.CommandContext(ctx, strings.TrimSpace(v.Command), args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	reason := strings.TrimSpace(string(out))
	if reason == "" {
		reason = err.Error()
	}
	return fmt.Errorf("aliyun quote verifier rejected quote: %s", reason)
}

func replaceQuotePath(args []string, quotePath string) ([]string, bool) {
	out := make([]string, 0, len(args))
	used := false
	for _, arg := range args {
		if strings.Contains(arg, quotePathPlaceholder) {
			used = true
			arg = strings.ReplaceAll(arg, quotePathPlaceholder, quotePath)
		}
		out = append(out, arg)
	}
	return out, used
}
