package committee

import (
	"context"
	"time"

	"committee-mvp/internal/attestation"
	"committee-mvp/internal/rpc"
)

type rpcAdapter struct {
	svc *Service
}

func newRPCAdapter(svc *Service) rpc.Service {
	return rpcAdapter{svc: svc}
}

func (a rpcAdapter) SetAutoSign(enabled bool) {
	a.svc.SetAutoSign(enabled)
}

func (a rpcAdapter) CommitteePublicKey() ([]byte, error) {
	return a.svc.CommitteePublicKey()
}

func (a rpcAdapter) VerifyQuote(ctx context.Context, quote []byte, policy attestation.Policy) (attestation.Result, error) {
	return a.svc.VerifyQuote(ctx, quote, policy)
}

func (a rpcAdapter) RegisterTEE(ctx context.Context, requestID string, ownerAddress string, quote []byte, reportData rpc.TEEReportData) (*rpc.RegisterTEEData, error) {
	result, err := a.svc.RegisterTEE(ctx, requestID, ownerAddress, quote, reportData)
	if err != nil {
		return nil, err
	}
	return &rpc.RegisterTEEData{
		Accepted:       result.Accepted,
		Reason:         result.Reason,
		TxHash:         result.TxHash,
		RequestID:      result.RequestID,
		OwnerAddress:   result.OwnerAddress,
		PendingNonce:   result.PendingNonce,
		MrEnclave:      result.MrEnclave,
		MrSigner:       result.MrSigner,
		HardwareID:     result.HardwareID,
		SigPubKey:      result.SigPubKey,
		VrfPubKey:      result.VrfPubKey,
		EnclavePK:      result.EnclavePK,
		ReportDataHash: result.ReportDataHash,
	}, nil
}

func (a rpcAdapter) SubmitRedactRequest(ctx context.Context, params rpc.SubmitRedactRequestParams) (*rpc.SubmitRedactRequestData, error) {
	return a.svc.SubmitRedactRequest(ctx, params)
}

func (a rpcAdapter) RedactAuthorization(ctx context.Context, requestID string) (*rpc.RedactAuthorizationData, error) {
	return a.svc.RedactAuthorization(ctx, requestID)
}

func (a rpcAdapter) RedactAuthorizationWait(ctx context.Context, requestID string, timeout time.Duration) (*rpc.RedactAuthorizationData, error) {
	return a.svc.RedactAuthorizationWait(ctx, requestID, timeout)
}

func (a rpcAdapter) SubmitRedactAuthorizationOnChain(ctx context.Context, requestID string) (*rpc.SubmitRedactAuthorizationOnChainData, error) {
	return a.svc.SubmitRedactAuthorizationOnChain(ctx, requestID)
}

func (a rpcAdapter) BroadcastRedactAuthorization(ctx context.Context, requestID string) (*rpc.BroadcastRedactAuthorizationData, error) {
	return a.svc.BroadcastRedactAuthorization(ctx, requestID)
}

func (a rpcAdapter) RejectRedactRequest(ctx context.Context, params rpc.RejectRedactRequestParams) (*rpc.RejectRedactRequestData, error) {
	return a.svc.RejectRedactRequest(ctx, params)
}

func (a rpcAdapter) RedactRejection(ctx context.Context, requestID string) (*rpc.RedactRejectionData, error) {
	return a.svc.RedactRejection(ctx, requestID)
}

func (a rpcAdapter) SubmitRedactRejectionOnChain(ctx context.Context, requestID string) (*rpc.SubmitRedactRejectionOnChainData, error) {
	return a.svc.SubmitRedactRejectionOnChain(ctx, requestID)
}

func (a rpcAdapter) RevokeTEE(ctx context.Context, params rpc.RevokeTEEParams) (*rpc.RevokeTEEData, error) {
	return a.svc.RevokeTEE(ctx, params)
}

func (a rpcAdapter) TEERevocation(ctx context.Context, requestID string) (*rpc.TEERevocationData, error) {
	return a.svc.TEERevocation(ctx, requestID)
}

func (a rpcAdapter) SubmitTEERevocationOnChain(ctx context.Context, requestID string) (*rpc.SubmitTEERevocationOnChainData, error) {
	return a.svc.SubmitTEERevocationOnChain(ctx, requestID)
}

func (a rpcAdapter) ActiveSetCert(ctx context.Context, params rpc.GetActiveSetCertParams) (*rpc.ActiveSetCertData, error) {
	return a.svc.ActiveSetCert(ctx, params)
}
