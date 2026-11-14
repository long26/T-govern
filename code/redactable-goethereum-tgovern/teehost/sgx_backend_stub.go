//go:build !sgx
// +build !sgx

package teehost

import (
	"context"
	"fmt"
)

type SGXBackend struct{}

var _ TEEProtocolBackend = (*SGXBackend)(nil)
var _ TEEPeerEPKRegistrar = (*SGXBackend)(nil)

func NewSGXBackend() (*SGXBackend, error) {
	return nil, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) Identity(ctx context.Context) (TEEIdentity, error) {
	return TEEIdentity{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) EvalVRF(ctx context.Context, req VRFRequest) (VRFResponse, error) {
	return VRFResponse{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) RegisterPeerEPK(ctx context.Context, peerIndex uint64, peerPK []byte) error {
	return fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DistKGCreateDeal(ctx context.Context, req DistKGCreateDealRequest) (TEEDealResult, error) {
	return TEEDealResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DistKGVerifyShare(ctx context.Context, req DistKGVerifyShareRequest) (bool, error) {
	return false, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DistKGExtract(ctx context.Context, req DistKGExtractRequest) (TEEExtractResult, error) {
	return TEEExtractResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DKRSCreateDeal(ctx context.Context, req DKRSCreateDealRequest) (TEEDealResult, error) {
	return TEEDealResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DKRSVerifyShare(ctx context.Context, req DKRSVerifyShareRequest) (bool, error) {
	return false, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) DKRSExtract(ctx context.Context, req DKRSExtractRequest) (TEEExtractResult, error) {
	return TEEExtractResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) PartialAdapt(ctx context.Context, req PartialAdaptRequest) (PartialAdaptResult, error) {
	return PartialAdaptResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) EraseShare(ctx context.Context, req EraseShareRequest) (EraseShareResult, error) {
	return EraseShareResult{}, fmt.Errorf("teehost SGX backend is not available; rebuild with -tags sgx")
}

func (b *SGXBackend) Close() {}
