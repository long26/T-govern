//go:build sgx_dcap

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"committee-mvp/internal/attestation"
)

func main() {
	quotePath := flag.String("quote", "tmp/quote.dat", "path to SGX DCAP quote")
	timeout := flag.Duration("timeout", 8*time.Second, "quote verification timeout")
	allowAdvisory := flag.Bool("allow-advisory", false, "accept non-terminal DCAP results as successful")
	flag.Parse()

	quote, err := os.ReadFile(*quotePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read quote: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := attestation.AliyunVerifier{}.Verify(ctx, quote, attestation.Policy{})
	if err != nil {
		if *allowAdvisory && result.Claims != nil {
			fmt.Printf("quote_verification=advisory reason=%q mrenclave=%s mrsigner=%s isvsvn=%s\n",
				result.Reason, result.Claims["mrenclave"], result.Claims["mrsigner"], result.Claims["isvsvn"])
			return
		}
		fmt.Fprintf(os.Stderr, "quote_verification=failed reason=%q\n", result.Reason)
		os.Exit(1)
	}

	fmt.Printf("quote_verification=ok mrenclave=%s mrsigner=%s isvsvn=%s\n",
		result.Claims["mrenclave"], result.Claims["mrsigner"], result.Claims["isvsvn"])
}
