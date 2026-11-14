package rpc

import "encoding/json"

const (
	JSONRPCVersion = "2.0"
	DefaultPath    = "/rpc"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type EmptyParams struct{}

type EmptyResult struct{}

type SetAutoSignParams struct {
	Enabled bool `json:"enabled"`
}

type GetPublicKeyResult struct {
	CommitteePubKey string `json:"committee_pub_key"`
}

type VerifyQuoteParams struct {
	Quote string `json:"quote"`
}

type VerifyQuoteResult struct {
	QuoteAccepted bool   `json:"quote_accepted"`
	QuoteReason   string `json:"quote_reason,omitempty"`
}

type TEEReportData struct {
	HardwareID      string `json:"hardware_id"`
	CommitteePubKey string `json:"committee_pub_key"`
	SigPubKey       string `json:"sig_pub_key"`
	VrfPubKey       string `json:"vrf_pub_key"`
	EnclavePK       string `json:"enclave_pk"`
}

type RegisterTEEParams struct {
	Quote        string        `json:"quote"`
	OwnerAddress string        `json:"owner_address"`
	ReportData   TEEReportData `json:"report_data"`
}

type RegisterTEEResult struct {
	RequestID      string `json:"request_id"`
	TxHash         string `json:"tx_hash,omitempty"`
	OwnerAddress   string `json:"owner_address,omitempty"`
	PendingNonce   string `json:"pending_nonce,omitempty"`
	MrEnclave      string `json:"mr_enclave,omitempty"`
	MrSigner       string `json:"mr_signer,omitempty"`
	HardwareID     string `json:"hardware_id,omitempty"`
	SigPubKey      string `json:"sig_pub_key,omitempty"`
	VrfPubKey      string `json:"vrf_pub_key,omitempty"`
	EnclavePK      string `json:"enclave_pk,omitempty"`
	ReportDataHash string `json:"report_data_hash,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type RedactBodyHashes struct {
	TxID          string `json:"tx_id"`
	MHash         string `json:"m_hash"`
	MPrimeHash    string `json:"m_prime_hash"`
	PosHash       string `json:"pos_hash"`
	MetaHash      string `json:"meta_hash"`
	ActiveSetHash string `json:"active_set_hash"`
	PkCHHash      string `json:"pk_ch_hash"`
}

type RedactReqFields struct {
	Version    uint16           `json:"version"`
	Epoch      uint64           `json:"epoch"`
	ReqID      string           `json:"req_id"`
	BodyHashes RedactBodyHashes `json:"body_hashes"`
}

type SubmitRedactRequestParams struct {
	RequestID              string          `json:"request_id,omitempty"`
	ChainID                string          `json:"chain_id"`
	ContractAddress        string          `json:"contract_address"`
	CommitteePubKeyVersion string          `json:"committee_pub_key_version"`
	ExpiresAt              string          `json:"expires_at"`
	Req                    RedactReqFields `json:"req"`
	DPolicy                string          `json:"d_policy,omitempty"`
	PolicyContext          json.RawMessage `json:"policy_context,omitempty"`
	SkipActiveSetCheck     bool            `json:"skip_active_set_check,omitempty"`
}

type SubmitRedactRequestResult struct {
	RequestID           string `json:"request_id"`
	SessionID           string `json:"session_id"`
	Status              string `json:"status"`
	DReq                string `json:"d_req"`
	DPolicy             string `json:"d_policy"`
	AuthorizationDigest string `json:"authorization_digest"`
}

type GetRedactAuthorizationParams struct {
	RequestID string `json:"request_id"`
	// Wait blocks until the threshold certificate is stored instead of
	// returning the pending session immediately. Callers measuring the
	// authorization latency should set this: a poll loop only observes
	// completion on its own cadence, which quantizes the measurement.
	Wait bool `json:"wait,omitempty"`
	// WaitTimeoutMS bounds Wait. Defaults to 90s when omitted or non-positive.
	WaitTimeoutMS int `json:"wait_timeout_ms,omitempty"`
}

type GetRedactAuthorizationResult struct {
	RequestID              string `json:"request_id"`
	SessionID              string `json:"session_id"`
	Status                 string `json:"status"`
	DReq                   string `json:"d_req"`
	DPolicy                string `json:"d_policy"`
	PolicyContextHash      string `json:"policy_context_hash,omitempty"`
	AuthorizationDigest    string `json:"authorization_digest"`
	ExpiresAt              string `json:"expires_at"`
	CommitteePubKeyVersion string `json:"committee_pub_key_version"`
	CertReqAuth            string `json:"cert_req_auth,omitempty"`
	AggregateSig           string `json:"aggregate_sig,omitempty"`
	AggregatePubKey        string `json:"aggregate_pub_key,omitempty"`
	Bitmap                 string `json:"bitmap,omitempty"`
	TxHash                 string `json:"tx_hash,omitempty"`
}

type SubmitRedactAuthorizationOnChainParams struct {
	RequestID string `json:"request_id"`
}

type SubmitRedactAuthorizationOnChainResult struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	TxHash    string `json:"tx_hash"`
	Status    string `json:"status"`
}

type BroadcastRedactAuthorizationParams struct {
	RequestID string `json:"request_id"`
}

type BroadcastRedactAuthorizationResult struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type BroadcastRedactAuthorizationData = BroadcastRedactAuthorizationResult

type RejectRedactRequestParams struct {
	RequestID              string          `json:"request_id,omitempty"`
	ChainID                string          `json:"chain_id"`
	ContractAddress        string          `json:"contract_address"`
	CommitteePubKeyVersion string          `json:"committee_pub_key_version"`
	Req                    RedactReqFields `json:"req"`
	DPolicy                string          `json:"d_policy,omitempty"`
	PolicyContext          json.RawMessage `json:"policy_context,omitempty"`
	ReasonHash             string          `json:"reason_hash"`
}

type RejectRedactRequestResult struct {
	RequestID         string `json:"request_id"`
	SessionID         string `json:"session_id"`
	Status            string `json:"status"`
	DReq              string `json:"d_req"`
	DPolicy           string `json:"d_policy"`
	ReasonHash        string `json:"reason_hash"`
	RejectionDigest   string `json:"rejection_digest"`
	PolicyContextHash string `json:"policy_context_hash,omitempty"`
	CertReqReject     string `json:"cert_req_reject,omitempty"`
	AggregateSig      string `json:"aggregate_sig,omitempty"`
	AggregatePubKey   string `json:"aggregate_pub_key,omitempty"`
	Bitmap            string `json:"bitmap,omitempty"`
	TxHash            string `json:"tx_hash,omitempty"`
}

type GetRedactRejectionParams struct {
	RequestID string `json:"request_id"`
}

type GetRedactRejectionResult = RejectRedactRequestResult

type SubmitRedactRejectionOnChainParams struct {
	RequestID string `json:"request_id"`
}

type SubmitRedactRejectionOnChainResult struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	TxHash    string `json:"tx_hash"`
	Status    string `json:"status"`
}

type RevokeTEEParams struct {
	RequestID              string `json:"request_id,omitempty"`
	ChainID                string `json:"chain_id"`
	ContractAddress        string `json:"contract_address"`
	CommitteePubKeyVersion string `json:"committee_pub_key_version"`
	HardwareID             string `json:"hardware_id"`
	ReasonHash             string `json:"reason_hash"`
	SlashAmount            string `json:"slash_amount"`
}

type RevokeTEEResult struct {
	RequestID              string `json:"request_id"`
	SessionID              string `json:"session_id"`
	Status                 string `json:"status"`
	HardwareID             string `json:"hardware_id"`
	ReasonHash             string `json:"reason_hash"`
	SlashAmount            string `json:"slash_amount"`
	CommitteePubKeyVersion string `json:"committee_pub_key_version"`
	RevocationDigest       string `json:"revocation_digest"`
	AggregateSig           string `json:"aggregate_sig,omitempty"`
	AggregatePubKey        string `json:"aggregate_pub_key,omitempty"`
	Bitmap                 string `json:"bitmap,omitempty"`
	TxHash                 string `json:"tx_hash,omitempty"`
}

type GetTEERevocationParams struct {
	RequestID string `json:"request_id"`
}

type GetTEERevocationResult = RevokeTEEResult

type SubmitTEERevocationOnChainParams struct {
	RequestID string `json:"request_id"`
}

type SubmitTEERevocationOnChainResult struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	TxHash    string `json:"tx_hash"`
	Status    string `json:"status"`
}

type GenerateActiveSetCertParams struct {
	RequestID       string   `json:"request_id,omitempty"`
	ChainID         string   `json:"chain_id"`
	ContractAddress string   `json:"contract_address"`
	Epoch           string   `json:"epoch"`
	ActiveTEEList   []string `json:"active_tee_list,omitempty"`
}

type GetActiveSetCertParams struct {
	Epoch           string   `json:"epoch"`
	ChainID         string   `json:"chain_id,omitempty"`
	ContractAddress string   `json:"contract_address,omitempty"`
	RequestID       string   `json:"request_id,omitempty"`
	ActiveTEEList   []string `json:"active_tee_list,omitempty"`
}

type ActiveSetCertResult struct {
	RequestID              string   `json:"request_id"`
	SessionID              string   `json:"session_id"`
	Status                 string   `json:"status"`
	ChainID                string   `json:"chain_id"`
	ContractAddress        string   `json:"contract_address"`
	Epoch                  string   `json:"epoch"`
	ActiveTEEList          []string `json:"active_tee_list"`
	ActiveSetHash          string   `json:"active_set_hash"`
	ActiveThreshold        string   `json:"active_threshold"`
	ActiveLimit            string   `json:"active_limit"`
	PkCHHash               string   `json:"pk_ch_hash"`
	CommitteePubKeyVersion string   `json:"committee_pub_key_version"`
	ActiveSetCertDigest    string   `json:"active_set_cert_digest"`
	CertEA                 string   `json:"cert_e_a,omitempty"`
	AggregateSig           string   `json:"aggregate_sig,omitempty"`
	AggregatePubKey        string   `json:"aggregate_pub_key,omitempty"`
	Bitmap                 string   `json:"bitmap,omitempty"`
}
