package chain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const defaultRPCTimeout = 5 * time.Second

var hexAddressPattern = regexp.MustCompile(`^(0x)?[0-9a-fA-F]{40}$`)

// IPCGateway queries chain state through a local geth IPC endpoint.
type IPCGateway struct {
	ipcPath string
	nextID  uint64
	timeout time.Duration
}

func NewIPCGateway(ctx context.Context, ipcPath string) (*IPCGateway, error) {
	path := strings.TrimSpace(ipcPath)
	if path == "" {
		return nil, fmt.Errorf("ipc path is required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial ipc endpoint: %w", err)
	}
	_ = conn.Close()
	return &IPCGateway{ipcPath: path, timeout: defaultRPCTimeout}, nil
}

func (g *IPCGateway) Close() {}

func (g *IPCGateway) GetPendingTEE(ctx context.Context, ownerAddress string) (*PendingTEEInfo, error) {
	normalizedAddress, err := normalizeHexAddress(ownerAddress)
	if err != nil {
		return nil, err
	}
	result, err := g.callRPC(ctx, "eth_getPendingTEE", []any{normalizedAddress})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Exists        bool            `json:"Exists"`
		Deposit       json.RawMessage `json:"Deposit"`
		PendingNonce  json.RawMessage `json:"PendingNonce"`
		CreatedAt     json.RawMessage `json:"CreatedAt"`
		ChallengeHash string          `json:"ChallengeHash"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode pending tee result: %w", err)
	}

	deposit, err := parseJSONBigInt(payload.Deposit)
	if err != nil {
		return nil, fmt.Errorf("decode pending tee deposit: %w", err)
	}
	nonce, err := parseJSONBigInt(payload.PendingNonce)
	if err != nil {
		return nil, fmt.Errorf("decode pending tee nonce: %w", err)
	}
	createdAt, err := parseJSONBigInt(payload.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("decode pending tee createdAt: %w", err)
	}

	return &PendingTEEInfo{
		Exists:        payload.Exists,
		Deposit:       deposit,
		Nonce:         nonce,
		CreatedAt:     createdAt,
		ChallengeHash: with0xPrefix(payload.ChallengeHash),
	}, nil
}

func (g *IPCGateway) GetTEEStatusByOwner(ctx context.Context, ownerAddress string) (*TEEStatusInfo, error) {
	normalizedAddress, err := normalizeHexAddress(ownerAddress)
	if err != nil {
		return nil, err
	}
	result, err := g.callRPC(ctx, "eth_getTEEStatusByOwner", []any{normalizedAddress})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Status     uint8  `json:"Status"`
		HardwareID string `json:"HardwareId"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode tee status result: %w", err)
	}

	return &TEEStatusInfo{
		Status:     payload.Status,
		HardwareID: strings.TrimSpace(payload.HardwareID),
	}, nil
}

func (g *IPCGateway) GetActiveSetInfo(ctx context.Context, epoch *big.Int) (*ActiveSetInfo, error) {
	if epoch == nil || epoch.Sign() < 0 {
		return nil, fmt.Errorf("invalid epoch")
	}
	result, err := g.callRPC(ctx, "eth_getActiveSetInfo", []any{epoch.String()})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Epoch                  json.RawMessage `json:"Epoch"`
		ActiveTEEs             []string        `json:"ActiveTEEs"`
		ActiveSetHash          string          `json:"ActiveSetHash"`
		PkCHHash               string          `json:"PkCHHash"`
		CommitteePubKeyVersion json.RawMessage `json:"CommitteePubKeyVersion"`
		ActiveThreshold        json.RawMessage `json:"ActiveThreshold"`
		ActiveLimit            json.RawMessage `json:"ActiveLimit"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode active set info result: %w", err)
	}
	decodedEpoch, err := parseJSONBigInt(payload.Epoch)
	if err != nil {
		return nil, fmt.Errorf("decode active set epoch: %w", err)
	}
	committeeVersion, err := parseJSONBigInt(payload.CommitteePubKeyVersion)
	if err != nil {
		return nil, fmt.Errorf("decode committee pubkey version: %w", err)
	}
	activeThreshold, err := parseJSONBigInt(payload.ActiveThreshold)
	if err != nil {
		return nil, fmt.Errorf("decode active threshold: %w", err)
	}
	activeLimit, err := parseJSONBigInt(payload.ActiveLimit)
	if err != nil {
		return nil, fmt.Errorf("decode active limit: %w", err)
	}
	return &ActiveSetInfo{
		Epoch:                  decodedEpoch,
		ActiveTEEs:             append([]string(nil), payload.ActiveTEEs...),
		ActiveSetHash:          with0xPrefix(payload.ActiveSetHash),
		PkCHHash:               with0xPrefix(payload.PkCHHash),
		CommitteePubKeyVersion: committeeVersion,
		ActiveThreshold:        activeThreshold,
		ActiveLimit:            activeLimit,
	}, nil
}

func (g *IPCGateway) SubmitTEERegistration(ctx context.Context, in RegistrationInput) (string, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return "", fmt.Errorf("ipc gateway is not initialized")
	}
	normalizedOwner, err := normalizeHexAddress(in.OwnerAddress)
	if err != nil {
		return "", err
	}
	if in.PendingNonce == nil {
		return "", fmt.Errorf("pending nonce is required")
	}
	payload := struct {
		Owner            string `json:"Owner"`
		PendingNonce     string `json:"PendingNonce"`
		MREnclave        string `json:"MR_ENCLAVE"`
		HardwareID       string `json:"HardwareId"`
		SigPubKey        string `json:"SigPubKey"`
		VrfPubKey        string `json:"VrfPubKey"`
		EnclavePK        string `json:"EnclavePK"`
		MRSigner         string `json:"MrSigner"`
		QuoteHash        string `json:"QuoteHash"`
		RegisterCertHash string `json:"RegisterCertHash"`
		ChallengeHash    string `json:"ChallengeHash"`
		Signature        string `json:"Signature"`
	}{
		Owner:            normalizedOwner,
		PendingNonce:     in.PendingNonce.String(),
		MREnclave:        with0xPrefix(in.MREnclave),
		HardwareID:       with0xPrefix(in.HardwareID),
		SigPubKey:        with0xPrefix(in.SigPubKey),
		VrfPubKey:        with0xPrefix(in.VrfPubKey),
		EnclavePK:        with0xPrefix(in.EnclavePK),
		MRSigner:         with0xPrefix(in.MRSigner),
		QuoteHash:        "0x" + hex.EncodeToString(in.QuoteHash),
		RegisterCertHash: with0xPrefix(in.RegisterCertHash),
		ChallengeHash:    with0xPrefix(in.ChallengeHash),
		Signature:        "0x" + hex.EncodeToString(in.AggregateSignature),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal register tee payload: %w", err)
	}

	result, err := g.callRPC(ctx, "eth_registerTEE", []any{string(payloadJSON)})
	if err != nil {
		return "", err
	}

	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", fmt.Errorf("decode tee registration result: %w", err)
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", fmt.Errorf("empty tx hash from eth_registerTEE")
	}
	return txHash, nil
}

func (g *IPCGateway) SubmitRedactAuthorization(ctx context.Context, in RedactAuthorizationInput) (string, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return "", fmt.Errorf("ipc gateway is not initialized")
	}
	if in.ExpiresAt == nil {
		return "", fmt.Errorf("expires_at is required")
	}
	if len(in.DPolicy) != 32 {
		return "", fmt.Errorf("d_policy must be 32 bytes")
	}
	if len(in.AggregateSignature) == 0 {
		return "", fmt.Errorf("aggregate signature is required")
	}
	bodyHashes := make([]string, 0, len(in.Req.BodyHashes))
	for _, h := range in.Req.BodyHashes {
		bodyHashes = append(bodyHashes, "0x"+hex.EncodeToString(h[:]))
	}
	payload := struct {
		RequestID  string   `json:"RequestID"`
		Version    uint16   `json:"Version"`
		Epoch      string   `json:"Epoch"`
		ReqID      string   `json:"ReqID"`
		BodyHashes []string `json:"BodyHashes"`
		DPolicy    string   `json:"DPolicy"`
		ExpiresAt  string   `json:"ExpiresAt"`
		Signature  string   `json:"Signature"`
	}{
		RequestID:  strings.TrimSpace(in.RequestID),
		Version:    in.Req.Version,
		Epoch:      new(big.Int).SetUint64(in.Req.Epoch).String(),
		ReqID:      "0x" + hex.EncodeToString(in.Req.ReqID[:]),
		BodyHashes: bodyHashes,
		DPolicy:    "0x" + hex.EncodeToString(in.DPolicy),
		ExpiresAt:  in.ExpiresAt.String(),
		Signature:  "0x" + hex.EncodeToString(in.AggregateSignature),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal redact authorization payload: %w", err)
	}

	result, err := g.callRPC(ctx, "eth_submitRedactAuthorization", []any{string(payloadJSON)})
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", fmt.Errorf("decode redact authorization result: %w", err)
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", fmt.Errorf("empty tx hash from eth_submitRedactAuthorization")
	}
	return txHash, nil
}

func (g *IPCGateway) BroadcastRedactAuthorization(ctx context.Context, in RedactAuthorizationInput) (string, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return "", fmt.Errorf("ipc gateway is not initialized")
	}
	if in.ExpiresAt == nil {
		return "", fmt.Errorf("expires_at is required")
	}
	if len(in.DPolicy) != 32 {
		return "", fmt.Errorf("d_policy must be 32 bytes")
	}
	if len(in.DReq) != 32 {
		return "", fmt.Errorf("d_req must be 32 bytes")
	}
	if len(in.AggregateSignature) == 0 {
		return "", fmt.Errorf("aggregate signature is required")
	}
	bodyHashes := make([]string, 0, len(in.Req.BodyHashes))
	for _, h := range in.Req.BodyHashes {
		bodyHashes = append(bodyHashes, "0x"+hex.EncodeToString(h[:]))
	}
	cpkv := ""
	if in.CommitteePubKeyVersion != nil {
		cpkv = in.CommitteePubKeyVersion.String()
	}
	payload := struct {
		RequestID              string   `json:"RequestID"`
		Version                uint16   `json:"Version"`
		Epoch                  string   `json:"Epoch"`
		ReqID                  string   `json:"ReqID"`
		BodyHashes             []string `json:"BodyHashes"`
		DReq                   string   `json:"DReq"`
		DPolicy                string   `json:"DPolicy"`
		ExpiresAt              string   `json:"ExpiresAt"`
		CommitteePubKeyVersion string   `json:"CommitteePubKeyVersion,omitempty"`
		AuthDigest             string   `json:"AuthDigest,omitempty"`
		AggregateSig           string   `json:"AggregateSig"`
		AggregatePubKey        string   `json:"AggregatePubKey,omitempty"`
		Bitmap                 string   `json:"Bitmap,omitempty"`
	}{
		RequestID:              strings.TrimSpace(in.RequestID),
		Version:                in.Req.Version,
		Epoch:                  new(big.Int).SetUint64(in.Req.Epoch).String(),
		ReqID:                  "0x" + hex.EncodeToString(in.Req.ReqID[:]),
		BodyHashes:             bodyHashes,
		DReq:                   "0x" + hex.EncodeToString(in.DReq),
		DPolicy:                "0x" + hex.EncodeToString(in.DPolicy),
		ExpiresAt:              in.ExpiresAt.String(),
		CommitteePubKeyVersion: cpkv,
		AuthDigest:             "0x" + hex.EncodeToString(in.AuthorizationDigest),
		AggregateSig:           "0x" + hex.EncodeToString(in.AggregateSignature),
		AggregatePubKey:        "0x" + hex.EncodeToString(in.AggregatePublicKey),
		Bitmap:                 "0x" + hex.EncodeToString(in.Bitmap),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal broadcast redact authorization payload: %w", err)
	}

	result, err := g.callRPC(ctx, "eth_broadcastRedactAuthorization", []any{string(payloadJSON)})
	if err != nil {
		return "", err
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("decode broadcast redact authorization result: %w", err)
	}
	status, _ := resp["Status"].(string)
	if status == "" {
		status = "broadcasted"
	}
	return status, nil
}

func (g *IPCGateway) SubmitRedactRejection(ctx context.Context, in RedactRejectionInput) (string, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return "", fmt.Errorf("ipc gateway is not initialized")
	}
	if len(in.DPolicy) != 32 {
		return "", fmt.Errorf("d_policy must be 32 bytes")
	}
	if len(in.ReasonHash) != 32 {
		return "", fmt.Errorf("reason_hash must be 32 bytes")
	}
	if len(in.AggregateSignature) == 0 {
		return "", fmt.Errorf("aggregate signature is required")
	}
	bodyHashes := make([]string, 0, len(in.Req.BodyHashes))
	for _, h := range in.Req.BodyHashes {
		bodyHashes = append(bodyHashes, "0x"+hex.EncodeToString(h[:]))
	}
	payload := struct {
		RequestID  string   `json:"RequestID"`
		Version    uint16   `json:"Version"`
		Epoch      string   `json:"Epoch"`
		ReqID      string   `json:"ReqID"`
		BodyHashes []string `json:"BodyHashes"`
		DPolicy    string   `json:"DPolicy"`
		ReasonHash string   `json:"ReasonHash"`
		Signature  string   `json:"Signature"`
	}{
		RequestID:  strings.TrimSpace(in.RequestID),
		Version:    in.Req.Version,
		Epoch:      new(big.Int).SetUint64(in.Req.Epoch).String(),
		ReqID:      "0x" + hex.EncodeToString(in.Req.ReqID[:]),
		BodyHashes: bodyHashes,
		DPolicy:    "0x" + hex.EncodeToString(in.DPolicy),
		ReasonHash: "0x" + hex.EncodeToString(in.ReasonHash),
		Signature:  "0x" + hex.EncodeToString(in.AggregateSignature),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal redact rejection payload: %w", err)
	}

	result, err := g.callRPC(ctx, "eth_rejectRedactRequest", []any{string(payloadJSON)})
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", fmt.Errorf("decode redact rejection result: %w", err)
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", fmt.Errorf("empty tx hash from eth_rejectRedactRequest")
	}
	return txHash, nil
}

func (g *IPCGateway) SubmitTEERevocation(ctx context.Context, in TEERevocationInput) (string, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return "", fmt.Errorf("ipc gateway is not initialized")
	}
	if len(in.HardwareID) != 32 {
		return "", fmt.Errorf("hardware_id must be 32 bytes")
	}
	if len(in.ReasonHash) != 32 {
		return "", fmt.Errorf("reason_hash must be 32 bytes")
	}
	if in.SlashAmount == nil || in.SlashAmount.Sign() < 0 {
		return "", fmt.Errorf("slash_amount must be non-negative")
	}
	if len(in.AggregateSignature) == 0 {
		return "", fmt.Errorf("aggregate signature is required")
	}
	result, err := g.callRPC(ctx, "eth_revokeTEE", []any{
		"0x" + hex.EncodeToString(in.HardwareID),
		"0x" + hex.EncodeToString(in.ReasonHash),
		in.SlashAmount.String(),
		"0x" + hex.EncodeToString(in.AggregateSignature),
	})
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", fmt.Errorf("decode tee revocation result: %w", err)
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", fmt.Errorf("empty tx hash from eth_revokeTEE")
	}
	return txHash, nil
}

func (g *IPCGateway) IsMrSignerRegistered(ctx context.Context, mrSigner string) (bool, error) {
	_ = ctx
	_ = mrSigner
	// Placeholder for contract query. Keep returning "not registered" for now.
	return false, nil
}

func (g *IPCGateway) callRPC(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	if g == nil || strings.TrimSpace(g.ipcPath) == "" {
		return nil, fmt.Errorf("ipc gateway is not initialized")
	}
	requestID := atomic.AddUint64(&g.nextID, 1)
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  method,
		Params:  params,
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.ipcPath)
	if err != nil {
		return nil, fmt.Errorf("dial ipc endpoint: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(selectDeadline(ctx, g.timeout)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return nil, fmt.Errorf("send rpc request: %w", err)
	}

	var response rpcResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, fmt.Errorf("read rpc response: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.ID != requestID {
		return nil, fmt.Errorf("mismatched rpc response id: got %d want %d", response.ID, requestID)
	}
	return response.Result, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func normalizeHexAddress(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if !hexAddressPattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid hex address: %s", address)
	}
	if !strings.HasPrefix(trimmed, "0x") && !strings.HasPrefix(trimmed, "0X") {
		trimmed = "0x" + trimmed
	}
	return strings.ToLower(trimmed), nil
}

func with0xPrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		return value
	}
	return "0x" + value
}

func parseDecimalBigInt(value string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return big.NewInt(0), nil
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		value = value[2:]
		base = 16
	}
	parsed, ok := new(big.Int).SetString(value, base)
	if !ok {
		return nil, fmt.Errorf("invalid integer quantity: %s", value)
	}
	return parsed, nil
}

func parseJSONBigInt(value json.RawMessage) (*big.Int, error) {
	if len(value) == 0 || string(value) == "null" {
		return big.NewInt(0), nil
	}
	var asString string
	if err := json.Unmarshal(value, &asString); err == nil {
		return parseDecimalBigInt(asString)
	}
	var asNumber json.Number
	if err := json.Unmarshal(value, &asNumber); err == nil {
		return parseDecimalBigInt(asNumber.String())
	}
	return nil, fmt.Errorf("invalid big integer json: %s", string(value))
}

func selectDeadline(ctx context.Context, fallback time.Duration) time.Time {
	deadline := time.Now().Add(fallback)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}
