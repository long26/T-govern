package attestation

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	sgxQuoteHeaderSize = 48
	sgxReportBodySize  = 384

	sgxReportMREnclaveOffset = 64
	sgxReportMRSignerOffset  = 128
	sgxReportISVSVNOffset    = 258
	sgxReportDataOffset      = 320
)

// SGXQuoteMeasurements contains the enclave identity fields from an SGX quote.
type SGXQuoteMeasurements struct {
	MREnclave      string
	MRSigner       string
	ISVSVN         uint16
	ReportData     string
	ReportDataHash string
}

// DecodeQuoteText accepts common operator-facing quote encodings.
func DecodeQuoteText(input string) ([]byte, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("quote is required")
	}

	hexInput := strings.TrimPrefix(strings.TrimPrefix(input, "0x"), "0X")
	hexInput = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t', ':':
			return -1
		default:
			return r
		}
	}, hexInput)
	if len(hexInput)%2 == 0 && len(hexInput) > 0 && isHexString(hexInput) {
		out, err := hex.DecodeString(hexInput)
		if err == nil {
			return out, nil
		}
	}

	if out, err := base64.StdEncoding.DecodeString(input); err == nil {
		return out, nil
	}
	if out, err := base64.RawStdEncoding.DecodeString(input); err == nil {
		return out, nil
	}
	if out, err := base64.URLEncoding.DecodeString(input); err == nil {
		return out, nil
	}
	if out, err := base64.RawURLEncoding.DecodeString(input); err == nil {
		return out, nil
	}

	return []byte(input), nil
}

// ParseSGXQuoteMeasurements parses MRENCLAVE/MRSIGNER from a DCAP SGX quote.
func ParseSGXQuoteMeasurements(quote []byte) (SGXQuoteMeasurements, error) {
	if len(quote) < sgxQuoteHeaderSize+sgxReportBodySize {
		return SGXQuoteMeasurements{}, fmt.Errorf("sgx quote too short: got %d bytes, need at least %d", len(quote), sgxQuoteHeaderSize+sgxReportBodySize)
	}

	report := quote[sgxQuoteHeaderSize : sgxQuoteHeaderSize+sgxReportBodySize]
	isvSVN := uint16(report[sgxReportISVSVNOffset]) | uint16(report[sgxReportISVSVNOffset+1])<<8
	reportData := report[sgxReportDataOffset : sgxReportDataOffset+64]
	return SGXQuoteMeasurements{
		MREnclave:      hex.EncodeToString(report[sgxReportMREnclaveOffset : sgxReportMREnclaveOffset+32]),
		MRSigner:       hex.EncodeToString(report[sgxReportMRSignerOffset : sgxReportMRSignerOffset+32]),
		ISVSVN:         isvSVN,
		ReportData:     hex.EncodeToString(reportData),
		ReportDataHash: hex.EncodeToString(reportData[:32]),
	}, nil
}

func isHexString(s string) bool {
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}
