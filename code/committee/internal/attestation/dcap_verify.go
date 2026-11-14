//go:build sgx_dcap

package attestation

/*
#cgo LDFLAGS: -lsgx_dcap_quoteverify
#include <stdint.h>
#include <time.h>
#include <sgx_dcap_quoteverify.h>

static quote3_error_t verify_quote_untrusted(
	const uint8_t *quote,
	uint32_t quote_size,
	uint32_t *collateral_expiration_status,
	sgx_ql_qv_result_t *verification_result
) {
	return sgx_qv_verify_quote(
		quote,
		quote_size,
		NULL,
		time(NULL),
		collateral_expiration_status,
		verification_result,
		NULL,
		0,
		NULL
	);
}
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

const (
	sgxQLQVResultOK                         = 0x0000
	sgxQLQVResultConfigNeeded               = 0xa001
	sgxQLQVResultOutOfDate                  = 0xa002
	sgxQLQVResultOutOfDateConfigNeeded      = 0xa003
	sgxQLQVResultSwHardeningNeeded          = 0xa007
	sgxQLQVResultConfigAndSwHardeningNeeded = 0xa008
)

// VerifyQuoteWithAliyunSDK calls Intel SGX DCAP Quote Verification Library.
// Alibaba Cloud SGX remote attestation is compatible with Intel SGX DCAP and
// uses /etc/sgx_default_qcnl.conf to fetch collateral from Alibaba Cloud PCCS.
func VerifyQuoteWithAliyunSDK(ctx context.Context, quote []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(quote) == 0 {
		return fmt.Errorf("empty quote")
	}
	if len(quote) > int(^uint32(0)) {
		return fmt.Errorf("quote too large: %d bytes", len(quote))
	}

	var collateralExpirationStatus C.uint32_t
	var verificationResult C.sgx_ql_qv_result_t
	ret := C.verify_quote_untrusted(
		(*C.uint8_t)(unsafe.Pointer(&quote[0])),
		C.uint32_t(len(quote)),
		&collateralExpirationStatus,
		&verificationResult,
	)
	if ret != 0 {
		return fmt.Errorf("sgx_qv_verify_quote failed: quote3_error=0x%04x", uint32(ret))
	}
	if collateralExpirationStatus != 0 {
		return fmt.Errorf("sgx quote collateral expired: status=%d", uint32(collateralExpirationStatus))
	}

	switch uint32(verificationResult) {
	case sgxQLQVResultOK,
		sgxQLQVResultConfigNeeded,
		sgxQLQVResultOutOfDate,
		sgxQLQVResultOutOfDateConfigNeeded,
		sgxQLQVResultSwHardeningNeeded,
		sgxQLQVResultConfigAndSwHardeningNeeded:
		return nil
	default:
		return fmt.Errorf("sgx quote verification failed: %s", quoteVerificationResultName(uint32(verificationResult)))
	}
}

func quoteVerificationResultName(result uint32) string {
	switch result {
	case sgxQLQVResultOK:
		return "OK"
	case sgxQLQVResultConfigNeeded:
		return "CONFIG_NEEDED"
	case sgxQLQVResultOutOfDate:
		return "OUT_OF_DATE"
	case sgxQLQVResultOutOfDateConfigNeeded:
		return "OUT_OF_DATE_CONFIG_NEEDED"
	case 0xa004:
		return "INVALID_SIGNATURE"
	case 0xa005:
		return "REVOKED"
	case 0xa006:
		return "UNSPECIFIED"
	case sgxQLQVResultSwHardeningNeeded:
		return "SW_HARDENING_NEEDED"
	case sgxQLQVResultConfigAndSwHardeningNeeded:
		return "CONFIG_AND_SW_HARDENING_NEEDED"
	default:
		return fmt.Sprintf("UNKNOWN(0x%04x)", result)
	}
}
