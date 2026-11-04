package attestation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAliyunVerifierVerifySuccess(t *testing.T) {
	script := writeVerifierScript(t, 0, "")
	quote := testSGXQuote(0xaa, 0xbb, 9)

	v := AliyunVerifier{Command: script, Args: []string{"--quote", quotePathPlaceholder}}
	res, err := v.Verify(context.Background(), quote, Policy{ExpectedMeasurement: strings.Repeat("aa", 32), MinSecurityVersion: 8})
	if err != nil {
		t.Fatalf("verify quote: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected accepted result")
	}
	if res.Claims["mrenclave"] != strings.Repeat("aa", 32) || res.Claims["mrsigner"] != strings.Repeat("bb", 32) {
		t.Fatalf("unexpected claims: %#v", res.Claims)
	}
}

func TestAliyunVerifierVerifyCommandRejects(t *testing.T) {
	script := writeVerifierScript(t, 2, "bad quote")
	quote := testSGXQuote(0xaa, 0xbb, 1)

	v := AliyunVerifier{Command: script}
	res, err := v.Verify(context.Background(), quote, Policy{})
	if err == nil {
		t.Fatalf("expected rejection")
	}
	if !strings.Contains(res.Reason, "bad quote") {
		t.Fatalf("unexpected reason: %s", res.Reason)
	}
}

func testSGXQuote(mrEnclaveByte, mrSignerByte byte, isvSVN uint16) []byte {
	quote := make([]byte, sgxQuoteHeaderSize+sgxReportBodySize)
	for i := 0; i < 32; i++ {
		quote[sgxQuoteHeaderSize+sgxReportMREnclaveOffset+i] = mrEnclaveByte
		quote[sgxQuoteHeaderSize+sgxReportMRSignerOffset+i] = mrSignerByte
	}
	quote[sgxQuoteHeaderSize+sgxReportISVSVNOffset] = byte(isvSVN)
	quote[sgxQuoteHeaderSize+sgxReportISVSVNOffset+1] = byte(isvSVN >> 8)
	return quote
}

func writeVerifierScript(t *testing.T, exitCode int, message string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script test requires unix shell")
	}
	path := filepath.Join(t.TempDir(), "verify-quote.sh")
	body := "#!/bin/sh\n"
	if message != "" {
		body += "echo " + message + "\n"
	}
	body += "exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}
