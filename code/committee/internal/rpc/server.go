package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"committee-mvp/internal/attestation"
)

const (
	MethodSetAutoSign       = "committee_setAutoSign"
	MethodGetPublicKey      = "committee_getPublicKey"
	MethodVerifyQuote       = "committee_verifyQuote"
	MethodRegisterTEE       = "committee_registerTEE"
	MethodSubmitRedact      = "committee_submitRedactRequest"
	MethodGetRedactAuth     = "committee_getRedactAuthorization"
	MethodSubmitRedactChain = "committee_submitRedactAuthorizationOnChain"
	MethodBroadcastRedact   = "committee_broadcastRedactAuthorization"
	MethodRejectRedact      = "committee_rejectRedactRequest"
	MethodGetRedactReject   = "committee_getRedactRejection"
	MethodSubmitRejectChain = "committee_submitRedactRejectionOnChain"
	MethodRevokeTEE         = "committee_revokeTEE"
	MethodGetTEERevocation  = "committee_getTEERevocation"
	MethodSubmitRevokeChain = "committee_submitTEERevocationOnChain"
	MethodGetActiveSetCert  = "committee_getActiveSetCert"
)

type Service interface {
	SetAutoSign(enabled bool)
	CommitteePublicKey() ([]byte, error)
	VerifyQuote(ctx context.Context, quote []byte, policy attestation.Policy) (attestation.Result, error)
	RegisterTEE(ctx context.Context, requestID string, ownerAddress string, quote []byte, reportData TEEReportData) (*RegisterTEEData, error)
	SubmitRedactRequest(ctx context.Context, params SubmitRedactRequestParams) (*SubmitRedactRequestData, error)
	RedactAuthorization(ctx context.Context, requestID string) (*RedactAuthorizationData, error)
	RedactAuthorizationWait(ctx context.Context, requestID string, timeout time.Duration) (*RedactAuthorizationData, error)
	SubmitRedactAuthorizationOnChain(ctx context.Context, requestID string) (*SubmitRedactAuthorizationOnChainData, error)
	BroadcastRedactAuthorization(ctx context.Context, requestID string) (*BroadcastRedactAuthorizationData, error)
	RejectRedactRequest(ctx context.Context, params RejectRedactRequestParams) (*RejectRedactRequestData, error)
	RedactRejection(ctx context.Context, requestID string) (*RedactRejectionData, error)
	SubmitRedactRejectionOnChain(ctx context.Context, requestID string) (*SubmitRedactRejectionOnChainData, error)
	RevokeTEE(ctx context.Context, params RevokeTEEParams) (*RevokeTEEData, error)
	TEERevocation(ctx context.Context, requestID string) (*TEERevocationData, error)
	SubmitTEERevocationOnChain(ctx context.Context, requestID string) (*SubmitTEERevocationOnChainData, error)
	ActiveSetCert(ctx context.Context, params GetActiveSetCertParams) (*ActiveSetCertData, error)
}

type RegisterTEEData struct {
	Accepted       bool
	Reason         string
	TxHash         string
	RequestID      string
	OwnerAddress   string
	PendingNonce   string
	MrEnclave      string
	MrSigner       string
	HardwareID     string
	SigPubKey      string
	VrfPubKey      string
	EnclavePK      string
	ReportDataHash string
}

type SubmitRedactRequestData = SubmitRedactRequestResult

type RedactAuthorizationData = GetRedactAuthorizationResult

type SubmitRedactAuthorizationOnChainData = SubmitRedactAuthorizationOnChainResult

type RejectRedactRequestData = RejectRedactRequestResult

type RedactRejectionData = GetRedactRejectionResult

type SubmitRedactRejectionOnChainData = SubmitRedactRejectionOnChainResult

type RevokeTEEData = RevokeTEEResult

type TEERevocationData = GetTEERevocationResult

type SubmitTEERevocationOnChainData = SubmitTEERevocationOnChainResult

type ActiveSetCertData = ActiveSetCertResult

type Server struct {
	svc Service
}

func NewServer(svc Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DefaultPath, s.handleRPC)
	return mux
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, req.ID, -32700, fmt.Sprintf("parse error: %v", err), nil)
		return
	}
	if req.JSONRPC != JSONRPCVersion {
		s.writeError(w, req.ID, -32600, "invalid jsonrpc version", nil)
		return
	}

	result, rpcErr := s.dispatch(r.Context(), req.Method, req.Params)
	if rpcErr != nil {
		s.writeError(w, req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		return
	}
	s.writeResult(w, req.ID, result)
}

func (s *Server) dispatch(ctx context.Context, method string, raw json.RawMessage) (interface{}, *Error) {
	switch method {
	case MethodSetAutoSign:
		var params SetAutoSignParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		s.svc.SetAutoSign(params.Enabled)
		return EmptyResult{}, nil
	case MethodGetPublicKey:
		if _, err := decodeOptionalEmpty(raw); err != nil {
			return nil, invalidParams(err)
		}
		pub, err := s.svc.CommitteePublicKey()
		if err != nil {
			return nil, internalError(err)
		}
		return GetPublicKeyResult{CommitteePubKey: hex.EncodeToString(pub)}, nil
	case MethodVerifyQuote:
		var params VerifyQuoteParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		quote, err := attestation.DecodeQuoteText(params.Quote)
		if err != nil {
			return nil, invalidParams(err)
		}
		verified, err := s.svc.VerifyQuote(ctx, quote, attestation.Policy{})
		if err != nil {
			return nil, &Error{
				Code:    3001,
				Message: err.Error(),
				Data: VerifyQuoteResult{
					QuoteAccepted: verified.Accepted,
					QuoteReason:   verified.Reason,
				},
			}
		}
		return VerifyQuoteResult{QuoteAccepted: verified.Accepted, QuoteReason: verified.Reason}, nil
	case MethodRegisterTEE:
		var params RegisterTEEParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		quote, err := attestation.DecodeQuoteText(params.Quote)
		if err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.RegisterTEE(ctx, "", params.OwnerAddress, quote, params.ReportData)
		if err != nil {
			return nil, internalError(err)
		}
		out := RegisterTEEResult{
			RequestID:      result.RequestID,
			TxHash:         result.TxHash,
			OwnerAddress:   result.OwnerAddress,
			PendingNonce:   result.PendingNonce,
			MrEnclave:      result.MrEnclave,
			MrSigner:       result.MrSigner,
			HardwareID:     result.HardwareID,
			SigPubKey:      result.SigPubKey,
			VrfPubKey:      result.VrfPubKey,
			EnclavePK:      result.EnclavePK,
			ReportDataHash: result.ReportDataHash,
			Reason:         result.Reason,
		}
		if !result.Accepted {
			return nil, &Error{Code: 3002, Message: strings.TrimSpace(result.Reason), Data: out}
		}
		return out, nil
	case MethodSubmitRedact:
		var params SubmitRedactRequestParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.SubmitRedactRequest(ctx, params)
		if err != nil {
			return nil, internalError(err)
		}
		return SubmitRedactRequestResult(*result), nil
	case MethodGetRedactAuth:
		var params GetRedactAuthorizationParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		var (
			result *RedactAuthorizationData
			err    error
		)
		if params.Wait {
			result, err = s.svc.RedactAuthorizationWait(ctx, params.RequestID, time.Duration(params.WaitTimeoutMS)*time.Millisecond)
		} else {
			result, err = s.svc.RedactAuthorization(ctx, params.RequestID)
		}
		if err != nil {
			return nil, internalError(err)
		}
		return GetRedactAuthorizationResult(*result), nil
	case MethodSubmitRedactChain:
		var params SubmitRedactAuthorizationOnChainParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.SubmitRedactAuthorizationOnChain(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return SubmitRedactAuthorizationOnChainResult(*result), nil
	case MethodBroadcastRedact:
		var params BroadcastRedactAuthorizationParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.BroadcastRedactAuthorization(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return BroadcastRedactAuthorizationResult(*result), nil
	case MethodRejectRedact:
		var params RejectRedactRequestParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.RejectRedactRequest(ctx, params)
		if err != nil {
			return nil, internalError(err)
		}
		return RejectRedactRequestResult(*result), nil
	case MethodGetRedactReject:
		var params GetRedactRejectionParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.RedactRejection(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return GetRedactRejectionResult(*result), nil
	case MethodSubmitRejectChain:
		var params SubmitRedactRejectionOnChainParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.SubmitRedactRejectionOnChain(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return SubmitRedactRejectionOnChainResult(*result), nil
	case MethodRevokeTEE:
		var params RevokeTEEParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.RevokeTEE(ctx, params)
		if err != nil {
			return nil, internalError(err)
		}
		return RevokeTEEResult(*result), nil
	case MethodGetTEERevocation:
		var params GetTEERevocationParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.TEERevocation(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return GetTEERevocationResult(*result), nil
	case MethodSubmitRevokeChain:
		var params SubmitTEERevocationOnChainParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.SubmitTEERevocationOnChain(ctx, params.RequestID)
		if err != nil {
			return nil, internalError(err)
		}
		return SubmitTEERevocationOnChainResult(*result), nil
	case MethodGetActiveSetCert:
		var params GetActiveSetCertParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidParams(err)
		}
		result, err := s.svc.ActiveSetCert(ctx, params)
		if err != nil {
			return nil, internalError(err)
		}
		return ActiveSetCertResult(*result), nil
	default:
		return nil, &Error{Code: -32601, Message: "method not found"}
	}
}

func (s *Server) writeResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Response{
		JSONRPC: JSONRPCVersion,
		ID:      ensureID(id),
		Result:  result,
	})
}

func (s *Server) writeError(w http.ResponseWriter, id json.RawMessage, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Response{
		JSONRPC: JSONRPCVersion,
		ID:      ensureID(id),
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func decodeParams(raw json.RawMessage, out interface{}) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, out)
}

func decodeOptionalEmpty(raw json.RawMessage) (EmptyParams, error) {
	var params EmptyParams
	err := decodeParams(raw, &params)
	return params, err
}

func ensureID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func invalidParams(err error) *Error {
	return &Error{Code: -32602, Message: err.Error()}
}

func internalError(err error) *Error {
	return &Error{Code: -32603, Message: err.Error()}
}
