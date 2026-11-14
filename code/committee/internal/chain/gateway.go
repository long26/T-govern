package chain

import (
	"context"
	"math/big"
)

// Gateway abstracts on-chain read/write operations.
type Gateway interface {
	GetPendingTEE(ctx context.Context, ownerAddress string) (*PendingTEEInfo, error)
	GetTEEStatusByOwner(ctx context.Context, ownerAddress string) (*TEEStatusInfo, error)
	GetActiveSetInfo(ctx context.Context, epoch *big.Int) (*ActiveSetInfo, error)
	SubmitTEERegistration(ctx context.Context, in RegistrationInput) (string, error)
	IsMrSignerRegistered(ctx context.Context, mrSigner string) (bool, error)
	SubmitRedactAuthorization(ctx context.Context, in RedactAuthorizationInput) (string, error)
	BroadcastRedactAuthorization(ctx context.Context, in RedactAuthorizationInput) (string, error)
	SubmitRedactRejection(ctx context.Context, in RedactRejectionInput) (string, error)
	SubmitTEERevocation(ctx context.Context, in TEERevocationInput) (string, error)
}

type PendingTEEInfo struct {
	Exists        bool
	Deposit       *big.Int
	Nonce         *big.Int
	CreatedAt     *big.Int
	ChallengeHash string
}

type TEEStatusInfo struct {
	Status     uint8
	HardwareID string
}

type ActiveSetInfo struct {
	Epoch                  *big.Int
	ActiveTEEs             []string
	ActiveSetHash          string
	PkCHHash               string
	CommitteePubKeyVersion *big.Int
	ActiveThreshold        *big.Int
	ActiveLimit            *big.Int
}

// RegistrationInput is a chain submission payload for TEE registration.
type RegistrationInput struct {
	RequestID          string
	OwnerAddress       string
	PendingNonce       *big.Int
	MREnclave          string
	MRSigner           string
	HardwareID         string
	SigPubKey          string
	VrfPubKey          string
	EnclavePK          string
	RegisterCertHash   string
	ChallengeHash      string
	QuoteHash          []byte
	MessageHash        []byte
	AggregateSignature []byte
	AggregatePublicKey []byte
}

// RedactReqFields mirrors PermissionManagerV3.ReqFields.
type RedactReqFields struct {
	Version    uint16
	Epoch      uint64
	ReqID      [32]byte
	BodyHashes [7][32]byte
}

// RedactAuthorizationInput is the payload for PermissionManagerV3.submitRedactAuthorization.
type RedactAuthorizationInput struct {
	RequestID string
	Req       RedactReqFields
	// DReq is keccak256(abi.encode(T_GOVERN_REDACT_REQ_V1, chainID, contractAddress,
	// version, epoch, reqID, bodyHashes)). The TEE enclave recomputes it from
	// d_req_input and rejects the PartialAdapt when the two disagree, so any
	// path that wakes a TEE host without the contract event must carry it.
	DReq                   []byte
	DPolicy                []byte
	ExpiresAt              *big.Int
	CommitteePubKeyVersion *big.Int
	AuthorizationDigest    []byte
	AggregateSignature     []byte
	AggregatePublicKey     []byte
	Bitmap                 []byte
}

// RedactRejectionInput is the payload for PermissionManagerV3.rejectRedactRequest.
type RedactRejectionInput struct {
	RequestID              string
	Req                    RedactReqFields
	DPolicy                []byte
	ReasonHash             []byte
	CommitteePubKeyVersion *big.Int
	RejectionDigest        []byte
	AggregateSignature     []byte
	AggregatePublicKey     []byte
	Bitmap                 []byte
}

// TEERevocationInput is the payload for PermissionManagerV3.revokeTEE.
type TEERevocationInput struct {
	RequestID              string
	ChainID                *big.Int
	ContractAddress        string
	HardwareID             []byte
	ReasonHash             []byte
	SlashAmount            *big.Int
	CommitteePubKeyVersion *big.Int
	RevocationDigest       []byte
	AggregateSignature     []byte
	AggregatePublicKey     []byte
	Bitmap                 []byte
}
