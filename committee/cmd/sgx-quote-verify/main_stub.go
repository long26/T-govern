//go:build !sgx_dcap

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "sgx-quote-verify requires Intel/Alibaba Cloud DCAP SDK; rebuild with: go run -tags sgx_dcap ./cmd/sgx-quote-verify --quote tmp/quote.dat")
	os.Exit(2)
}
