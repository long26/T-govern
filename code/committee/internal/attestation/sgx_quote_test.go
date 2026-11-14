package attestation

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestParseSGXQuoteMeasurements(t *testing.T) {
	quote := make([]byte, sgxQuoteHeaderSize+sgxReportBodySize)
	mrEnclave := bytes.Repeat([]byte{0xaa}, 32)
	mrSigner := bytes.Repeat([]byte{0xbb}, 32)
	reportDataHash := bytes.Repeat([]byte{0x11}, 32)
	copy(quote[sgxQuoteHeaderSize+sgxReportMREnclaveOffset:], mrEnclave)
	copy(quote[sgxQuoteHeaderSize+sgxReportMRSignerOffset:], mrSigner)
	copy(quote[sgxQuoteHeaderSize+sgxReportDataOffset:], reportDataHash)
	quote[sgxQuoteHeaderSize+sgxReportISVSVNOffset] = 7

	got, err := ParseSGXQuoteMeasurements(quote)
	if err != nil {
		t.Fatalf("parse quote: %v", err)
	}
	if got.MREnclave != hex.EncodeToString(mrEnclave) {
		t.Fatalf("unexpected mrenclave: %s", got.MREnclave)
	}
	if got.MRSigner != hex.EncodeToString(mrSigner) {
		t.Fatalf("unexpected mrsigner: %s", got.MRSigner)
	}
	if got.ISVSVN != 7 {
		t.Fatalf("unexpected isvsvn: %d", got.ISVSVN)
	}
	if got.ReportDataHash != hex.EncodeToString(reportDataHash) {
		t.Fatalf("unexpected report data hash: %s", got.ReportDataHash)
	}
}

func TestDecodeQuoteText(t *testing.T) {
	raw := []byte{0, 1, 2, 0xfa}
	for _, input := range []string{
		hex.EncodeToString(raw),
		"0x" + hex.EncodeToString(raw),
		base64.StdEncoding.EncodeToString(raw),
	} {
		got, err := DecodeQuoteText(input)
		if err != nil {
			t.Fatalf("decode quote %q: %v", input, err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("decode quote %q = %x, want %x", input, got, raw)
		}
	}
}
