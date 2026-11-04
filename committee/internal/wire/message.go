package wire

import (
	"encoding/json"
)

type MessageType uint8

const (
	MsgHello MessageType = iota + 1
	MsgDealShare
	MsgSignRequest
	MsgSignResponse
	MsgAggResult
	MsgQuoteRequest
	MsgQuoteVote
	_
	_
	_
	_
	_
	_
)

// Envelope is the common wire frame used by the protocol.
// Only fields consumed by the protocol are carried. Dead declarations
// (sent_at, protocol, action, trace_id, ttl_millis, error_code, metadata)
// were removed to cut per-message wire bytes; sent_at alone cost 33 bytes
// on every message and was written but never read.
type Envelope struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	RequestID string      `json:"request_id,omitempty"`
	Epoch     uint64      `json:"epoch"`
	Nonce     uint64      `json:"nonce"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Version   string      `json:"version"`
	Payload   []byte      `json:"payload"`
}

type SignRequestPayload struct {
	Message []byte `json:"message"`
}

type DealSharePayload struct {
	DealerIndex    int    `json:"dealer_index"`
	RecipientIndex int    `json:"recipient_index"`
	ShareValue     string `json:"share_value"`
	DealerPubKey   []byte `json:"dealer_pub_key"`
}

type SignResponsePayload struct {
	SignerIndex    int    `json:"signer_index"`
	ShareSignature []byte `json:"share_signature"`
	SignerPubKey   []byte `json:"signer_pub_key"`
}

type AggResultPayload struct {
	Bitmap             []byte `json:"bitmap"`
	AggregateSig       []byte `json:"aggregate_signature"`
	AggregatePublicKey []byte `json:"aggregate_public_key"`
	Message            []byte `json:"message"`
}

type QuoteRequestPayload struct {
	RequestID        string `json:"request_id,omitempty"`
	OwnerAddress     string `json:"owner_address,omitempty"`
	PendingNonce     string `json:"pending_nonce,omitempty"`
	Quote            []byte `json:"quote"`
	QuoteHash        []byte `json:"quote_hash,omitempty"`
	MrEnclave        string `json:"mr_enclave,omitempty"`
	MrSigner         string `json:"mr_signer,omitempty"`
	HardwareID       string `json:"hardware_id,omitempty"`
	CommitteePubKey  string `json:"committee_pub_key,omitempty"`
	SigPubKey        string `json:"sig_pub_key,omitempty"`
	VrfPubKey        string `json:"vrf_pub_key,omitempty"`
	EnclavePK        string `json:"enclave_pk,omitempty"`
	ReportDataHash   string `json:"report_data_hash,omitempty"`
	RegisterCertHash string `json:"register_cert_hash,omitempty"`
	ChallengeHash    string `json:"challenge_hash,omitempty"`
	MessageHash      []byte `json:"message_hash,omitempty"`
	PolicyHint       string `json:"policy_hint,omitempty"`
}

type QuoteVotePayload struct {
	RequestID        string `json:"request_id,omitempty"`
	OwnerAddress     string `json:"owner_address,omitempty"`
	PendingNonce     string `json:"pending_nonce,omitempty"`
	SignerIndex      int    `json:"signer_index"`
	Accepted         bool   `json:"accepted"`
	Reason           string `json:"reason,omitempty"`
	QuoteHash        []byte `json:"quote_hash,omitempty"`
	MrEnclave        string `json:"mr_enclave,omitempty"`
	MrSigner         string `json:"mr_signer,omitempty"`
	HardwareID       string `json:"hardware_id,omitempty"`
	CommitteePubKey  string `json:"committee_pub_key,omitempty"`
	SigPubKey        string `json:"sig_pub_key,omitempty"`
	VrfPubKey        string `json:"vrf_pub_key,omitempty"`
	EnclavePK        string `json:"enclave_pk,omitempty"`
	ReportDataHash   string `json:"report_data_hash,omitempty"`
	RegisterCertHash string `json:"register_cert_hash,omitempty"`
	ChallengeHash    string `json:"challenge_hash,omitempty"`
	MessageHash      []byte `json:"message_hash,omitempty"`
	Signature        []byte `json:"signature,omitempty"`
	SignerPubKey     []byte `json:"signer_pub_key,omitempty"`
}

func EncodePayload(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func DecodePayload(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}
