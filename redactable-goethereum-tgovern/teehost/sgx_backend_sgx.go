//go:build sgx
// +build sgx

package teehost

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/sgx"
)

type SGXBackend struct {
	identity TEEIdentity
}

var _ TEEProtocolBackend = (*SGXBackend)(nil)
var _ TEEPeerEPKRegistrar = (*SGXBackend)(nil)

func NewSGXBackend() (*SGXBackend, error) {
	if err := sgx.Init(); err != nil {
		return nil, err
	}
	hardwareID, _, sigPubKey, vrfPubKey, _, err := sgx.GetIdentity()
	if err != nil {
		sgx.Close()
		return nil, err
	}
	if len(hardwareID) != 32 {
		sgx.Close()
		return nil, fmt.Errorf("invalid hardware id length: got %d, want 32", len(hardwareID))
	}
	if len(vrfPubKey) != 32 {
		sgx.Close()
		return nil, fmt.Errorf("invalid vrf pubkey length: got %d, want 32", len(vrfPubKey))
	}
	if len(sigPubKey) != 32 {
		sgx.Close()
		return nil, fmt.Errorf("invalid sig pubkey length: got %d, want 32", len(sigPubKey))
	}
	var identity TEEIdentity
	copy(identity.HardwareID[:], hardwareID)
	copy(identity.SigPubKey[:], sigPubKey)
	copy(identity.VRFPubKey[:], vrfPubKey)
	log.Info("SGX TEE backend initialized", "hardwareID", fmt.Sprintf("%x", hardwareID), "sigPubKey", fmt.Sprintf("%x", sigPubKey), "vrfPubKey", fmt.Sprintf("%x", vrfPubKey))
	return &SGXBackend{identity: identity}, nil
}

func (b *SGXBackend) Identity(ctx context.Context) (TEEIdentity, error) {
	return b.identity, nil
}

func (b *SGXBackend) EvalVRF(ctx context.Context, req VRFRequest) (VRFResponse, error) {
	result, err := sgx.VRFEval(req.ChainID, req.ContractAddress, req.Epoch, req.HardwareID[:], req.VRFPubKey[:], req.Seed[:])
	if err != nil {
		return VRFResponse{}, err
	}
	if len(result.VRFResult) != 32 {
		return VRFResponse{}, fmt.Errorf("invalid vrf result length: got %d, want 32", len(result.VRFResult))
	}
	var response VRFResponse
	copy(response.VRFResult[:], result.VRFResult)
	response.VRFProof = append([]byte(nil), result.VRFProof...)
	return response, nil
}

func (b *SGXBackend) RegisterPeerEPK(ctx context.Context, peerIndex uint64, peerPK []byte) error {
	return sgx.RegisterPeerEPK(peerIndex, peerPK)
}

func (b *SGXBackend) DistKGCreateDeal(ctx context.Context, req DistKGCreateDealRequest) (TEEDealResult, error) {
	result, err := sgx.DistKGCreateDeal(req.DealerIndex, req.Threshold, req.NumParties, req.CertEAMsg, req.CertEASig, req.ActiveTEEList, req.ActiveSetCertInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
	if err != nil {
		return TEEDealResult{}, err
	}
	return teeDealResultFromSGX(result), nil
}

func (b *SGXBackend) DistKGVerifyShare(ctx context.Context, req DistKGVerifyShareRequest) (bool, error) {
	return sgx.DistKGVerifyShare(req.ReceiverIndex, req.DealerIndex, req.Threshold, req.NumParties, req.CommitmentsA, req.CommitmentB0, req.ShareBytes, req.CertEAMsg, req.CertEASig, req.ActiveTEEList, req.ActiveSetCertInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
}

func (b *SGXBackend) DistKGExtract(ctx context.Context, req DistKGExtractRequest) (TEEExtractResult, error) {
	result, err := sgx.DistKGExtract(
		req.MyIndex, req.Threshold, req.NumParties,
		req.QualIndices,
		req.B0Data, req.B0DealerMap,
		req.A0Data, req.A0DealerMap,
		req.SharesData, req.SharesFromMap,
		req.CertEAMsg, req.CertEASig,
		req.ActiveTEEList, req.ActiveSetCertInput,
		req.ActiveTEECount, req.ActiveSetCertInputLen,
		sgx.PkCHSigningInput{
			ChainID:         req.ChainID,
			ContractAddress: req.Contract,
			TEEOwner:        req.TEEOwner,
		},
	)
	if err != nil {
		return TEEExtractResult{}, err
	}
	return teeExtractResultFromSGX(result), nil
}

func (b *SGXBackend) DKRSCreateDeal(ctx context.Context, req DKRSCreateDealRequest) (TEEDealResult, error) {
	result, err := sgx.DKRSCreateDeal(req.DealerIndex, req.NewThreshold, req.NewNumParties, req.CertEAMsg, req.CertEASig, req.ActiveTEEList, req.ActiveSetCertInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
	if err != nil {
		return TEEDealResult{}, err
	}
	return teeDealResultFromSGX(result), nil
}

func (b *SGXBackend) DKRSVerifyShare(ctx context.Context, req DKRSVerifyShareRequest) (bool, error) {
	return sgx.DKRSVerifyShare(req.ReceiverIndex, req.DealerIndex, req.NewThreshold, req.NewNumParties, req.CommitmentsA, req.CommitmentB0, req.ShareBytes, req.CertEAMsg, req.CertEASig, req.ActiveTEEList, req.ActiveSetCertInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
}

func (b *SGXBackend) DKRSExtract(ctx context.Context, req DKRSExtractRequest) (TEEExtractResult, error) {
	result, err := sgx.DKRSExtract(req.MyIndex, req.NewThreshold, req.NewNumParties, req.OldNumParties, req.QualOld, req.B0Data, req.B0DealerMap, req.A0Data, req.A0DealerMap, req.SharesData, req.SharesFromMap, req.CertEAMsg, req.CertEASig, req.ActiveTEEList, req.ActiveSetCertInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
	if err != nil {
		return TEEExtractResult{}, err
	}
	return teeExtractResultFromSGX(result), nil
}

func (b *SGXBackend) PartialAdapt(ctx context.Context, req PartialAdaptRequest) (PartialAdaptResult, error) {
	result, err := sgx.PartialAdapt(req.SourceMessage, req.TargetMessage, req.CertReqAuthMsg, req.CertReqAuthSig, req.CertEAMsg, req.CertEASig, req.DReq, req.DPolicy, req.CommitteePubKeyVersion, req.ExpiresAt, req.ActiveTEEList, req.ActiveSetCertInput, req.DReqInput, req.ActiveTEECount, req.ActiveSetCertInputLen)
	if err != nil {
		return PartialAdaptResult{}, err
	}
	return PartialAdaptResult{
		Contribution:   append([]byte(nil), result.Contribution...),
		ProofChallenge: append([]byte(nil), result.ProofChallenge...),
		ProofResponse:  append([]byte(nil), result.ProofResponse...),
		Y:              append([]byte(nil), result.Y...),
		Sigma:          append([]byte(nil), result.Sigma...),
	}, nil
}

func (b *SGXBackend) EraseShare(ctx context.Context, req EraseShareRequest) (EraseShareResult, error) {
	result, err := sgx.EraseShare(req.Epoch, req.NodeIndex, req.ChainID, req.ContractAddress, req.TEEOwner)
	if err != nil {
		return EraseShareResult{}, err
	}
	if len(result.EraseProofHash) != 32 {
		return EraseShareResult{}, fmt.Errorf("invalid erase proof hash length: got %d, want 32", len(result.EraseProofHash))
	}
	var out EraseShareResult
	copy(out.EraseProofHash[:], result.EraseProofHash)
	out.Proof = append([]byte(nil), result.Proof...)
	return out, nil
}

func (b *SGXBackend) Close() {
	sgx.Close()
}

func teeDealResultFromSGX(result *sgx.SGXDealResult) TEEDealResult {
	if result == nil {
		return TEEDealResult{}
	}
	return TEEDealResult{
		CommitmentsA:     append([]byte(nil), result.CommitmentsA...),
		CommitmentB0:     append([]byte(nil), result.CommitmentB0...),
		SharesSerialized: append([]byte(nil), result.SharesSerialized...),
		G1Bytes:          append([]byte(nil), result.G1Bytes...),
		G2Bytes:          append([]byte(nil), result.G2Bytes...),
		PHex:             result.PHex,
	}
}

func teeExtractResultFromSGX(result *sgx.SGXExtractResult) TEEExtractResult {
	if result == nil {
		return TEEExtractResult{}
	}
	return TEEExtractResult{
		GlobalPK:        append([]byte(nil), result.GlobalPK...),
		GlobalPKG1:      append([]byte(nil), result.GlobalPKG1...),
		VerificationKey: append([]byte(nil), result.VerificationKey...),
		PkCHSignature:   append([]byte(nil), result.PkCHSignature...),
	}
}
