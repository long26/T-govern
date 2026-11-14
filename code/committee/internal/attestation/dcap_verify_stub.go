//go:build !sgx_dcap

package attestation

import (
	"context"
	"fmt"
)

// VerifyQuoteWithAliyunSDK is available when built with -tags sgx_dcap.
func VerifyQuoteWithAliyunSDK(_ context.Context, _ []byte) error {
	return fmt.Errorf("aliyun dcap sdk verifier is not linked; rebuild with -tags sgx_dcap or configure aliyun_quote_verify_command")
}
