package committee

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"committee-mvp/internal/chain"
	"committee-mvp/internal/rpc"
	"committee-mvp/internal/wire"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	redactProtocolVersion  uint16 = 1
	redactStatusPending           = "pending"
	redactStatusAuthorized        = "authorized"
	redactStatusRejected          = "rejected"
	redactStatusSubmitted         = "submitted"
)

var (
	redactReqDomain    = ethcrypto.Keccak256Hash([]byte("T_GOVERN_REDACT_REQ_V1"))
	redactAuthDomain   = ethcrypto.Keccak256Hash([]byte("T_GOVERN_REDACT_AUTH_V1"))
	redactRejectDomain = ethcrypto.Keccak256Hash([]byte("T_GOVERN_REDACT_REJECT_V1"))
	redactPolicyDomain = ethcrypto.Keccak256Hash([]byte("T_GOVERN_POLICY_V1"))
)

type redactAuthorizationSession struct {
	requestID              string
	signSessionID          string
	req                    chain.RedactReqFields
	chainID                *big.Int
	contractAddress        string
	dReq                   []byte
	dPolicy                []byte
	policyContextHash      []byte
	authorizationDigest    []byte
	reasonHash             []byte
	rejectionDigest        []byte
	expiresAt              *big.Int
	committeePubKeyVersion *big.Int
	status                 string
	txHash                 string
}

// SubmitRedactRequest validates a v0.8 Req, computes d_req/d_policy/auth digest,
// and starts the committee threshold signing session for Cert_req^auth.
func (s *Service) SubmitRedactRequest(ctx context.Context, params rpc.SubmitRedactRequestParams) (*rpc.SubmitRedactRequestData, error) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return nil, fmt.Errorf("only coordinator can submit redact request")
	}
	if !s.isDKGReady() {
		return nil, fmt.Errorf("committee key not ready yet")
	}

	req, err := normalizeRedactReqFields(params.Req)
	if err != nil {
		return nil, err
	}
	chainID, err := parsePositiveBigInt(params.ChainID, "chain_id")
	if err != nil {
		return nil, err
	}
	contractAddress := "0x0000000000000000000000000000000000000000"
	if addr := strings.TrimSpace(params.ContractAddress); addr != "" && addr != "0x0" {
		var addrErr error
		contractAddress, addrErr = normalizeOwnerAddress(addr)
		if addrErr != nil {
			return nil, fmt.Errorf("invalid contract_address: %w", addrErr)
		}
	}
	expiresAt, err := parsePositiveBigInt(params.ExpiresAt, "expires_at")
	if err != nil {
		return nil, err
	}
	pubKeyVersion, err := parsePositiveBigInt(params.CommitteePubKeyVersion, "committee_pub_key_version")
	if err != nil {
		return nil, err
	}

	if contractAddress != "0x0000000000000000000000000000000000000000" && !params.SkipActiveSetCheck {
		if err := s.validateRedactReqActiveSetBinding(ctx, req); err != nil {
			return nil, err
		}
	}

	dReq, err := computeRedactDReq(chainID, contractAddress, req)
	if err != nil {
		return nil, err
	}
	dPolicy, policyContextHash, err := computeRedactPolicyDigest(params.DPolicy, params.PolicyContext)
	if err != nil {
		return nil, err
	}
	authDigest, err := computeRedactAuthorizationDigest(dReq, dPolicy, pubKeyVersion, expiresAt)
	if err != nil {
		return nil, err
	}

	signSessionID, err := s.startSignRequest(authDigest)
	if err != nil {
		return nil, err
	}
	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" {
		requestID = bytes32Hex(req.ReqID)
	}

	session := &redactAuthorizationSession{
		requestID:              requestID,
		signSessionID:          signSessionID,
		req:                    req,
		chainID:                new(big.Int).Set(chainID),
		contractAddress:        contractAddress,
		dReq:                   append([]byte(nil), dReq...),
		dPolicy:                append([]byte(nil), dPolicy...),
		policyContextHash:      append([]byte(nil), policyContextHash...),
		authorizationDigest:    append([]byte(nil), authDigest...),
		expiresAt:              new(big.Int).Set(expiresAt),
		committeePubKeyVersion: new(big.Int).Set(pubKeyVersion),
		status:                 redactStatusPending,
	}
	s.mu.Lock()
	s.redactSessions[requestID] = session
	s.mu.Unlock()

	return &rpc.SubmitRedactRequestData{
		RequestID:           requestID,
		SessionID:           signSessionID,
		Status:              redactStatusPending,
		DReq:                hex.EncodeToString(dReq),
		DPolicy:             hex.EncodeToString(dPolicy),
		AuthorizationDigest: hex.EncodeToString(authDigest),
	}, nil
}

func (s *Service) validateRedactReqActiveSetBinding(ctx context.Context, req chain.RedactReqFields) error {
	epoch := new(big.Int).SetUint64(req.Epoch)
	chainInfo, err := s.chainGateway.GetActiveSetInfo(ctx, epoch)
	if err != nil {
		return fmt.Errorf("get active set info for epoch %s: %w", epoch.String(), err)
	}
	if chainInfo == nil {
		return fmt.Errorf("active set info is missing for epoch %s", epoch.String())
	}
	activeSetHash, err := parseRequiredBytes32(chainInfo.ActiveSetHash, "chain active_set_hash")
	if err != nil {
		return err
	}
	if activeSetHash != req.BodyHashes[5] {
		return fmt.Errorf("active_set_hash does not match chain active set for epoch %s", epoch.String())
	}
	pkCHHash, err := parseRequiredBytes32(chainInfo.PkCHHash, "chain pk_ch_hash")
	if err != nil {
		return err
	}
	if pkCHHash != req.BodyHashes[6] {
		return fmt.Errorf("pk_ch_hash does not match chain active set for epoch %s", epoch.String())
	}
	return nil
}

// RedactAuthorization returns the aggregated Cert_req^auth if the signing
// threshold has completed, otherwise it returns the pending session metadata.
// It never blocks; use RedactAuthorizationWait to wait for completion.
func (s *Service) RedactAuthorization(ctx context.Context, requestID string) (*rpc.RedactAuthorizationData, error) {
	return s.redactAuthorization(ctx, requestID, 0)
}

// RedactAuthorizationWait is RedactAuthorization with a bounded wait: it blocks
// until the threshold certificate is stored, then returns the authorized
// result. Callers that need the exact completion instant should prefer this
// over polling, which quantizes the observed latency by the poll interval.
func (s *Service) RedactAuthorizationWait(ctx context.Context, requestID string, timeout time.Duration) (*rpc.RedactAuthorizationData, error) {
	if timeout <= 0 {
		timeout = defaultAggregateWait
	}
	return s.redactAuthorization(ctx, requestID, timeout)
}

// defaultAggregateWait bounds RedactAuthorizationWait when the caller does not
// specify one. Threshold signing is sub-second in practice; the allowance
// matches the submit-side deadlines used elsewhere in the harness.
const defaultAggregateWait = 90 * time.Second

func (s *Service) redactAuthorization(ctx context.Context, requestID string, wait time.Duration) (*rpc.RedactAuthorizationData, error) {
	session, err := s.getRedactSession(requestID)
	if err != nil {
		return nil, err
	}
	if len(session.authorizationDigest) != 32 {
		return nil, fmt.Errorf("redact request is not an authorization session: %s", requestID)
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	if !ok && wait > 0 {
		// Block on the session's completion channel instead of polling.
		result, ok = s.waitForAggregate(ctx, session.signSessionID, time.Now().Add(wait))
	}
	out := redactAuthorizationDataFromSession(session)
	if !ok {
		return out, nil
	}
	out.Status = redactStatusAuthorized
	out.AggregateSig = hex.EncodeToString(result.AggregateSig)
	out.AggregatePubKey = hex.EncodeToString(result.AggregatePublicKey)
	out.Bitmap = hex.EncodeToString(result.Bitmap)
	out.CertReqAuth = hex.EncodeToString(result.AggregateSig)

	s.mu.Lock()
	if live := s.redactSessions[session.requestID]; live != nil && live.status == redactStatusPending {
		live.status = redactStatusAuthorized
	}
	s.mu.Unlock()
	return out, nil
}

// SubmitRedactAuthorizationOnChain submits the aggregated authorization
// certificate to the configured chain gateway.
func (s *Service) SubmitRedactAuthorizationOnChain(ctx context.Context, requestID string) (*rpc.SubmitRedactAuthorizationOnChainData, error) {
	session, err := s.getRedactSession(requestID)
	if err != nil {
		return nil, err
	}
	if len(session.authorizationDigest) != 32 {
		return nil, fmt.Errorf("redact request is not an authorization session: %s", requestID)
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	if !ok {
		return nil, fmt.Errorf("redact authorization is not aggregated yet")
	}

	txHash, err := s.chainGateway.SubmitRedactAuthorization(ctx, chain.RedactAuthorizationInput{
		RequestID:              session.requestID,
		Req:                    session.req,
		DPolicy:                append([]byte(nil), session.dPolicy...),
		ExpiresAt:              new(big.Int).Set(session.expiresAt),
		CommitteePubKeyVersion: new(big.Int).Set(session.committeePubKeyVersion),
		AuthorizationDigest:    append([]byte(nil), session.authorizationDigest...),
		AggregateSignature:     append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey:     append([]byte(nil), result.AggregatePublicKey...),
		Bitmap:                 append([]byte(nil), result.Bitmap...),
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if live := s.redactSessions[session.requestID]; live != nil {
		live.status = redactStatusSubmitted
		live.txHash = txHash
	}
	s.mu.Unlock()
	return &rpc.SubmitRedactAuthorizationOnChainData{
		RequestID: requestID,
		SessionID: session.signSessionID,
		TxHash:    txHash,
		Status:    redactStatusSubmitted,
	}, nil
}

// BroadcastRedactAuthorization broadcasts the aggregated authorization
// certificate via the configured chain gateway P2P broadcast endpoint.
func (s *Service) BroadcastRedactAuthorization(ctx context.Context, requestID string) (*rpc.BroadcastRedactAuthorizationData, error) {
	session, err := s.getRedactSession(requestID)
	if err != nil {
		return nil, err
	}
	if len(session.authorizationDigest) != 32 {
		return nil, fmt.Errorf("redact request is not an authorization session: %s", requestID)
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	if !ok {
		return nil, fmt.Errorf("redact authorization is not aggregated yet")
	}

	status, err := s.chainGateway.BroadcastRedactAuthorization(ctx, chain.RedactAuthorizationInput{
		RequestID:              session.requestID,
		Req:                    session.req,
		DReq:                   append([]byte(nil), session.dReq...),
		DPolicy:                append([]byte(nil), session.dPolicy...),
		ExpiresAt:              new(big.Int).Set(session.expiresAt),
		CommitteePubKeyVersion: new(big.Int).Set(session.committeePubKeyVersion),
		AuthorizationDigest:    append([]byte(nil), session.authorizationDigest...),
		AggregateSignature:     append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey:     append([]byte(nil), result.AggregatePublicKey...),
		Bitmap:                 append([]byte(nil), result.Bitmap...),
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if live := s.redactSessions[session.requestID]; live != nil {
		live.status = "broadcasted"
		live.txHash = status
	}
	s.mu.Unlock()
	return &rpc.BroadcastRedactAuthorizationData{
		RequestID: requestID,
		SessionID: session.signSessionID,
		Status:    status,
	}, nil
}

// RejectRedactRequest validates a rejected Req, computes Cert_req^reject digest,
// and starts the committee threshold signing session.
func (s *Service) RejectRedactRequest(ctx context.Context, params rpc.RejectRedactRequestParams) (*rpc.RejectRedactRequestData, error) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return nil, fmt.Errorf("only coordinator can reject redact request")
	}
	if !s.isDKGReady() {
		return nil, fmt.Errorf("committee key not ready yet")
	}

	req, err := normalizeRedactReqFields(params.Req)
	if err != nil {
		return nil, err
	}
	chainID, err := parsePositiveBigInt(params.ChainID, "chain_id")
	if err != nil {
		return nil, err
	}
	contractAddress, err := normalizeOwnerAddress(params.ContractAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid contract_address: %w", err)
	}
	pubKeyVersion, err := parsePositiveBigInt(params.CommitteePubKeyVersion, "committee_pub_key_version")
	if err != nil {
		return nil, err
	}
	reasonHash32, err := parseRequiredBytes32(params.ReasonHash, "reason_hash")
	if err != nil {
		return nil, err
	}

	if err := s.validateRedactReqActiveSetBinding(ctx, req); err != nil {
		return nil, err
	}

	dReq, err := computeRedactDReq(chainID, contractAddress, req)
	if err != nil {
		return nil, err
	}
	dPolicy, policyContextHash, err := computeRedactPolicyDigest(params.DPolicy, params.PolicyContext)
	if err != nil {
		return nil, err
	}
	rejectDigest, err := computeRedactRejectionDigest(dReq, dPolicy, reasonHash32[:], pubKeyVersion)
	if err != nil {
		return nil, err
	}

	signSessionID, err := s.startSignRequest(rejectDigest)
	if err != nil {
		return nil, err
	}
	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" {
		requestID = bytes32Hex(req.ReqID)
	}

	session := &redactAuthorizationSession{
		requestID:              requestID,
		signSessionID:          signSessionID,
		req:                    req,
		chainID:                new(big.Int).Set(chainID),
		contractAddress:        contractAddress,
		dReq:                   append([]byte(nil), dReq...),
		dPolicy:                append([]byte(nil), dPolicy...),
		policyContextHash:      append([]byte(nil), policyContextHash...),
		reasonHash:             append([]byte(nil), reasonHash32[:]...),
		rejectionDigest:        append([]byte(nil), rejectDigest...),
		committeePubKeyVersion: new(big.Int).Set(pubKeyVersion),
		status:                 redactStatusPending,
	}
	s.mu.Lock()
	s.redactSessions[requestID] = session
	s.mu.Unlock()

	return redactRejectionDataFromSession(session), nil
}

// RedactRejection returns the aggregated Cert_req^reject if the signing
// threshold has completed, otherwise it returns the pending session metadata.
func (s *Service) RedactRejection(ctx context.Context, requestID string) (*rpc.RedactRejectionData, error) {
	_ = ctx
	session, err := s.getRedactSession(requestID)
	if err != nil {
		return nil, err
	}
	if len(session.rejectionDigest) != 32 {
		return nil, fmt.Errorf("redact request is not a rejection session: %s", requestID)
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	out := redactRejectionDataFromSession(session)
	if !ok {
		return out, nil
	}
	out.Status = redactStatusRejected
	out.AggregateSig = hex.EncodeToString(result.AggregateSig)
	out.AggregatePubKey = hex.EncodeToString(result.AggregatePublicKey)
	out.Bitmap = hex.EncodeToString(result.Bitmap)
	out.CertReqReject = hex.EncodeToString(result.AggregateSig)

	s.mu.Lock()
	if live := s.redactSessions[session.requestID]; live != nil && live.status == redactStatusPending {
		live.status = redactStatusRejected
	}
	s.mu.Unlock()
	return out, nil
}

// SubmitRedactRejectionOnChain submits the aggregated rejection certificate to
// the configured chain gateway as one contract transaction.
func (s *Service) SubmitRedactRejectionOnChain(ctx context.Context, requestID string) (*rpc.SubmitRedactRejectionOnChainData, error) {
	session, err := s.getRedactSession(requestID)
	if err != nil {
		return nil, err
	}
	if len(session.rejectionDigest) != 32 {
		return nil, fmt.Errorf("redact request is not a rejection session: %s", requestID)
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	if !ok {
		return nil, fmt.Errorf("redact rejection is not aggregated yet")
	}

	txHash, err := s.chainGateway.SubmitRedactRejection(ctx, chain.RedactRejectionInput{
		RequestID:              session.requestID,
		Req:                    session.req,
		DPolicy:                append([]byte(nil), session.dPolicy...),
		ReasonHash:             append([]byte(nil), session.reasonHash...),
		CommitteePubKeyVersion: new(big.Int).Set(session.committeePubKeyVersion),
		RejectionDigest:        append([]byte(nil), session.rejectionDigest...),
		AggregateSignature:     append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey:     append([]byte(nil), result.AggregatePublicKey...),
		Bitmap:                 append([]byte(nil), result.Bitmap...),
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if live := s.redactSessions[session.requestID]; live != nil {
		live.status = redactStatusSubmitted
		live.txHash = txHash
	}
	s.mu.Unlock()
	return &rpc.SubmitRedactRejectionOnChainData{
		RequestID: requestID,
		SessionID: session.signSessionID,
		TxHash:    txHash,
		Status:    redactStatusSubmitted,
	}, nil
}

func (s *Service) getRedactSession(requestID string) (*redactAuthorizationSession, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.redactSessions[requestID]
	if !ok {
		return nil, fmt.Errorf("redact request not found: %s", requestID)
	}
	return cloneRedactSession(session), nil
}

func (s *Service) lookupAggregateResult(sessionID string) (wire.AggResultPayload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.aggResults[sessionID]
	if !ok {
		return wire.AggResultPayload{}, false
	}
	return wire.AggResultPayload{
		Bitmap:             append([]byte(nil), result.Bitmap...),
		AggregateSig:       append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey: append([]byte(nil), result.AggregatePublicKey...),
		Message:            append([]byte(nil), result.Message...),
	}, true
}

func redactAuthorizationDataFromSession(session *redactAuthorizationSession) *rpc.RedactAuthorizationData {
	expiresAt := ""
	if session.expiresAt != nil {
		expiresAt = session.expiresAt.String()
	}
	return &rpc.RedactAuthorizationData{
		RequestID:              session.requestID,
		SessionID:              session.signSessionID,
		Status:                 session.status,
		DReq:                   hex.EncodeToString(session.dReq),
		DPolicy:                hex.EncodeToString(session.dPolicy),
		PolicyContextHash:      hex.EncodeToString(session.policyContextHash),
		AuthorizationDigest:    hex.EncodeToString(session.authorizationDigest),
		ExpiresAt:              expiresAt,
		CommitteePubKeyVersion: session.committeePubKeyVersion.String(),
		TxHash:                 session.txHash,
	}
}

func cloneRedactSession(in *redactAuthorizationSession) *redactAuthorizationSession {
	if in == nil {
		return nil
	}
	out := *in
	out.req = cloneRedactReqFields(in.req)
	out.chainID = new(big.Int).Set(in.chainID)
	out.dReq = append([]byte(nil), in.dReq...)
	out.dPolicy = append([]byte(nil), in.dPolicy...)
	out.policyContextHash = append([]byte(nil), in.policyContextHash...)
	out.authorizationDigest = append([]byte(nil), in.authorizationDigest...)
	out.reasonHash = append([]byte(nil), in.reasonHash...)
	out.rejectionDigest = append([]byte(nil), in.rejectionDigest...)
	if in.expiresAt != nil {
		out.expiresAt = new(big.Int).Set(in.expiresAt)
	}
	out.committeePubKeyVersion = new(big.Int).Set(in.committeePubKeyVersion)
	return &out
}

func normalizeRedactReqFields(in rpc.RedactReqFields) (chain.RedactReqFields, error) {
	if in.Version != redactProtocolVersion {
		return chain.RedactReqFields{}, fmt.Errorf("unsupported redact protocol version: %d", in.Version)
	}
	reqID, err := parseRequiredBytes32(in.ReqID, "req_id")
	if err != nil {
		return chain.RedactReqFields{}, err
	}
	bodyStrings := []struct {
		name  string
		value string
	}{
		{"tx_id", in.BodyHashes.TxID},
		{"m_hash", in.BodyHashes.MHash},
		{"m_prime_hash", in.BodyHashes.MPrimeHash},
		{"pos_hash", in.BodyHashes.PosHash},
		{"meta_hash", in.BodyHashes.MetaHash},
		{"active_set_hash", in.BodyHashes.ActiveSetHash},
		{"pk_ch_hash", in.BodyHashes.PkCHHash},
	}
	var bodyHashes [7][32]byte
	for i, field := range bodyStrings {
		bodyHashes[i], err = parseRequiredBytes32(field.value, field.name)
		if err != nil {
			return chain.RedactReqFields{}, err
		}
	}
	return chain.RedactReqFields{
		Version:    in.Version,
		Epoch:      in.Epoch,
		ReqID:      reqID,
		BodyHashes: bodyHashes,
	}, nil
}

func parseRequiredBytes32(value, field string) ([32]byte, error) {
	out, err := bytes32FromHex(value)
	if err != nil {
		return out, fmt.Errorf("invalid %s: %w", field, err)
	}
	if out == ([32]byte{}) {
		return out, fmt.Errorf("%s is required", field)
	}
	return out, nil
}

func parsePositiveBigInt(value, field string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be a positive decimal integer", field)
	}
	return n, nil
}

func computeRedactDReq(chainID *big.Int, contractAddress string, req chain.RedactReqFields) ([]byte, error) {
	bytes32Type, err := ethabi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, err
	}
	uint256Type, err := ethabi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	addressType, err := ethabi.NewType("address", "", nil)
	if err != nil {
		return nil, err
	}
	uint16Type, err := ethabi.NewType("uint16", "", nil)
	if err != nil {
		return nil, err
	}
	uint64Type, err := ethabi.NewType("uint64", "", nil)
	if err != nil {
		return nil, err
	}
	bytes32ArrayType, err := ethabi.NewType("bytes32[7]", "", nil)
	if err != nil {
		return nil, err
	}
	args := ethabi.Arguments{
		{Type: bytes32Type},
		{Type: uint256Type},
		{Type: addressType},
		{Type: uint16Type},
		{Type: uint64Type},
		{Type: bytes32Type},
		{Type: bytes32ArrayType},
	}
	packed, err := args.Pack(
		redactReqDomain,
		chainID,
		ethcommon.HexToAddress(contractAddress),
		req.Version,
		req.Epoch,
		req.ReqID,
		req.BodyHashes,
	)
	if err != nil {
		return nil, fmt.Errorf("abi pack redact req: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}

func computeRedactPolicyDigest(dPolicyHex string, policyContext json.RawMessage) ([]byte, []byte, error) {
	if strings.TrimSpace(dPolicyHex) != "" {
		dPolicy, err := bytes32FromHex(dPolicyHex)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid d_policy: %w", err)
		}
		if dPolicy == ([32]byte{}) {
			return nil, nil, fmt.Errorf("d_policy is required")
		}
		return dPolicy[:], ethcrypto.Keccak256(nil), nil
	}
	contextHash := ethcrypto.Keccak256(policyContext)
	bytes32Type, err := ethabi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, nil, err
	}
	uint16Type, err := ethabi.NewType("uint16", "", nil)
	if err != nil {
		return nil, nil, err
	}
	args := ethabi.Arguments{{Type: bytes32Type}, {Type: uint16Type}, {Type: bytes32Type}}
	var contextHash32 [32]byte
	copy(contextHash32[:], contextHash)
	packed, err := args.Pack(redactPolicyDomain, redactProtocolVersion, contextHash32)
	if err != nil {
		return nil, nil, fmt.Errorf("abi pack redact policy: %w", err)
	}
	return ethcrypto.Keccak256(packed), contextHash, nil
}

func computeRedactAuthorizationDigest(dReq, dPolicy []byte, pubKeyVersion, expiresAt *big.Int) ([]byte, error) {
	if len(dReq) != 32 {
		return nil, fmt.Errorf("d_req must be 32 bytes")
	}
	if len(dPolicy) != 32 {
		return nil, fmt.Errorf("d_policy must be 32 bytes")
	}
	bytes32Type, err := ethabi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, err
	}
	uint256Type, err := ethabi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	var dReq32 [32]byte
	var dPolicy32 [32]byte
	copy(dReq32[:], dReq)
	copy(dPolicy32[:], dPolicy)
	args := ethabi.Arguments{{Type: bytes32Type}, {Type: bytes32Type}, {Type: bytes32Type}, {Type: uint256Type}, {Type: uint256Type}}
	packed, err := args.Pack(redactAuthDomain, dReq32, dPolicy32, pubKeyVersion, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("abi pack redact authorization: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}

func computeRedactRejectionDigest(dReq, dPolicy, reasonHash []byte, pubKeyVersion *big.Int) ([]byte, error) {
	if len(dReq) != 32 {
		return nil, fmt.Errorf("d_req must be 32 bytes")
	}
	if len(dPolicy) != 32 {
		return nil, fmt.Errorf("d_policy must be 32 bytes")
	}
	if len(reasonHash) != 32 {
		return nil, fmt.Errorf("reason_hash must be 32 bytes")
	}
	bytes32Type, err := ethabi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, err
	}
	uint256Type, err := ethabi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	var dReq32 [32]byte
	var dPolicy32 [32]byte
	var reasonHash32 [32]byte
	copy(dReq32[:], dReq)
	copy(dPolicy32[:], dPolicy)
	copy(reasonHash32[:], reasonHash)
	args := ethabi.Arguments{{Type: bytes32Type}, {Type: bytes32Type}, {Type: bytes32Type}, {Type: bytes32Type}, {Type: uint256Type}}
	packed, err := args.Pack(redactRejectDomain, dReq32, dPolicy32, reasonHash32, pubKeyVersion)
	if err != nil {
		return nil, fmt.Errorf("abi pack redact rejection: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}

func redactRejectionDataFromSession(session *redactAuthorizationSession) *rpc.RedactRejectionData {
	return &rpc.RedactRejectionData{
		RequestID:         session.requestID,
		SessionID:         session.signSessionID,
		Status:            session.status,
		DReq:              hex.EncodeToString(session.dReq),
		DPolicy:           hex.EncodeToString(session.dPolicy),
		ReasonHash:        hex.EncodeToString(session.reasonHash),
		PolicyContextHash: hex.EncodeToString(session.policyContextHash),
		RejectionDigest:   hex.EncodeToString(session.rejectionDigest),
		TxHash:            session.txHash,
	}
}

func cloneRedactReqFields(in chain.RedactReqFields) chain.RedactReqFields {
	out := in
	return out
}

func bytes32Hex(value [32]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}
