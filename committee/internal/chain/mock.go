package chain

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
)

// MockGateway is an in-memory chain gateway used for integration testing.
type MockGateway struct {
	mu            sync.RWMutex
	pendingTEEs   map[string]PendingTEEInfo
	teeStatuses   map[string]TEEStatusInfo
	activeSets    map[string]ActiveSetInfo
	registrations []RegistrationInput
	mrSigners     map[string]bool
	redactSubmits []RedactAuthorizationInput
	redactRejects []RedactRejectionInput
	teeRevokes    []TEERevocationInput
	txSeq         uint64
}

func NewMockGateway() *MockGateway {
	return &MockGateway{
		pendingTEEs: make(map[string]PendingTEEInfo),
		teeStatuses: make(map[string]TEEStatusInfo),
		activeSets:  make(map[string]ActiveSetInfo),
		mrSigners:   make(map[string]bool),
	}
}

func (m *MockGateway) SetPendingTEE(address string, info *PendingTEEInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := addressKey(address)
	if info == nil {
		delete(m.pendingTEEs, key)
		return
	}
	m.pendingTEEs[key] = clonePendingTEEInfo(*info)
}

func (m *MockGateway) GetPendingTEE(_ context.Context, ownerAddress string) (*PendingTEEInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.pendingTEEs[addressKey(ownerAddress)]; ok {
		cloned := clonePendingTEEInfo(info)
		return &cloned, nil
	}
	return &PendingTEEInfo{Exists: false, Deposit: big.NewInt(0), Nonce: big.NewInt(0), CreatedAt: big.NewInt(0)}, nil
}

func (m *MockGateway) SetTEEStatus(address string, info *TEEStatusInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := addressKey(address)
	if info == nil {
		delete(m.teeStatuses, key)
		return
	}
	m.teeStatuses[key] = *info
}

func (m *MockGateway) GetTEEStatusByOwner(_ context.Context, ownerAddress string) (*TEEStatusInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.teeStatuses[addressKey(ownerAddress)]; ok {
		cloned := info
		return &cloned, nil
	}
	return &TEEStatusInfo{}, nil
}

func (m *MockGateway) SetActiveSetInfo(epoch *big.Int, info *ActiveSetInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := epochKey(epoch)
	if info == nil {
		delete(m.activeSets, key)
		return
	}
	m.activeSets[key] = cloneActiveSetInfo(*info)
}

func (m *MockGateway) GetActiveSetInfo(_ context.Context, epoch *big.Int) (*ActiveSetInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.activeSets[epochKey(epoch)]; ok {
		cloned := cloneActiveSetInfo(info)
		return &cloned, nil
	}
	return &ActiveSetInfo{
		Epoch:                  cloneBigInt(epoch),
		CommitteePubKeyVersion: big.NewInt(0),
		ActiveThreshold:        big.NewInt(0),
		ActiveLimit:            big.NewInt(0),
	}, nil
}

func (m *MockGateway) SubmitTEERegistration(_ context.Context, in RegistrationInput) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeq++
	m.registrations = append(m.registrations, cloneRegistrationInput(in))
	return fmt.Sprintf("mock-tx-%d", m.txSeq), nil
}

func (m *MockGateway) SubmitRedactAuthorization(_ context.Context, in RedactAuthorizationInput) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeq++
	m.redactSubmits = append(m.redactSubmits, cloneRedactAuthorizationInput(in))
	return fmt.Sprintf("mock-redact-tx-%d", m.txSeq), nil
}

func (m *MockGateway) BroadcastRedactAuthorization(_ context.Context, in RedactAuthorizationInput) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeq++
	m.redactSubmits = append(m.redactSubmits, cloneRedactAuthorizationInput(in))
	return fmt.Sprintf("mock-broadcast-%d", m.txSeq), nil
}

func (m *MockGateway) SubmitRedactRejection(_ context.Context, in RedactRejectionInput) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeq++
	m.redactRejects = append(m.redactRejects, cloneRedactRejectionInput(in))
	return fmt.Sprintf("mock-redact-reject-tx-%d", m.txSeq), nil
}

func (m *MockGateway) SubmitTEERevocation(_ context.Context, in TEERevocationInput) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeq++
	m.teeRevokes = append(m.teeRevokes, cloneTEERevocationInput(in))
	return fmt.Sprintf("mock-tee-revoke-tx-%d", m.txSeq), nil
}

func (m *MockGateway) LastRedactAuthorization() (RedactAuthorizationInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.redactSubmits) == 0 {
		return RedactAuthorizationInput{}, false
	}
	return cloneRedactAuthorizationInput(m.redactSubmits[len(m.redactSubmits)-1]), true
}

func (m *MockGateway) LastRedactRejection() (RedactRejectionInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.redactRejects) == 0 {
		return RedactRejectionInput{}, false
	}
	return cloneRedactRejectionInput(m.redactRejects[len(m.redactRejects)-1]), true
}

func (m *MockGateway) LastTEERevocation() (TEERevocationInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.teeRevokes) == 0 {
		return TEERevocationInput{}, false
	}
	return cloneTEERevocationInput(m.teeRevokes[len(m.teeRevokes)-1]), true
}

func (m *MockGateway) SetMrSignerRegistered(mrSigner string, registered bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(mrSigner))
	if key == "" {
		return
	}
	if !registered {
		delete(m.mrSigners, key)
		return
	}
	m.mrSigners[key] = true
}

func (m *MockGateway) IsMrSignerRegistered(_ context.Context, mrSigner string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(mrSigner))
	if key == "" {
		return false, nil
	}
	return m.mrSigners[key], nil
}

func (m *MockGateway) RegistrationCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.registrations)
}

func (m *MockGateway) LastRegistration() (RegistrationInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.registrations) == 0 {
		return RegistrationInput{}, false
	}
	return cloneRegistrationInput(m.registrations[len(m.registrations)-1]), true
}

func addressKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func epochKey(epoch *big.Int) string {
	if epoch == nil {
		return "0"
	}
	return epoch.String()
}

func cloneRegistrationInput(in RegistrationInput) RegistrationInput {
	out := RegistrationInput{
		RequestID:          in.RequestID,
		OwnerAddress:       in.OwnerAddress,
		MREnclave:          in.MREnclave,
		MRSigner:           in.MRSigner,
		HardwareID:         in.HardwareID,
		SigPubKey:          in.SigPubKey,
		VrfPubKey:          in.VrfPubKey,
		EnclavePK:          in.EnclavePK,
		RegisterCertHash:   in.RegisterCertHash,
		ChallengeHash:      in.ChallengeHash,
		QuoteHash:          append([]byte(nil), in.QuoteHash...),
		MessageHash:        append([]byte(nil), in.MessageHash...),
		AggregateSignature: append([]byte(nil), in.AggregateSignature...),
		AggregatePublicKey: append([]byte(nil), in.AggregatePublicKey...),
	}
	if in.PendingNonce != nil {
		out.PendingNonce = new(big.Int).Set(in.PendingNonce)
	}
	return out
}

func cloneRedactAuthorizationInput(in RedactAuthorizationInput) RedactAuthorizationInput {
	out := RedactAuthorizationInput{
		RequestID:           in.RequestID,
		Req:                 in.Req,
		DReq:                append([]byte(nil), in.DReq...),
		DPolicy:             append([]byte(nil), in.DPolicy...),
		AuthorizationDigest: append([]byte(nil), in.AuthorizationDigest...),
		AggregateSignature:  append([]byte(nil), in.AggregateSignature...),
		AggregatePublicKey:  append([]byte(nil), in.AggregatePublicKey...),
		Bitmap:              append([]byte(nil), in.Bitmap...),
	}
	if in.ExpiresAt != nil {
		out.ExpiresAt = new(big.Int).Set(in.ExpiresAt)
	}
	if in.CommitteePubKeyVersion != nil {
		out.CommitteePubKeyVersion = new(big.Int).Set(in.CommitteePubKeyVersion)
	}
	return out
}

func cloneRedactRejectionInput(in RedactRejectionInput) RedactRejectionInput {
	out := RedactRejectionInput{
		RequestID:          in.RequestID,
		Req:                in.Req,
		DPolicy:            append([]byte(nil), in.DPolicy...),
		ReasonHash:         append([]byte(nil), in.ReasonHash...),
		RejectionDigest:    append([]byte(nil), in.RejectionDigest...),
		AggregateSignature: append([]byte(nil), in.AggregateSignature...),
		AggregatePublicKey: append([]byte(nil), in.AggregatePublicKey...),
		Bitmap:             append([]byte(nil), in.Bitmap...),
	}
	if in.CommitteePubKeyVersion != nil {
		out.CommitteePubKeyVersion = new(big.Int).Set(in.CommitteePubKeyVersion)
	}
	return out
}

func cloneTEERevocationInput(in TEERevocationInput) TEERevocationInput {
	out := TEERevocationInput{
		RequestID:          in.RequestID,
		ContractAddress:    in.ContractAddress,
		HardwareID:         append([]byte(nil), in.HardwareID...),
		ReasonHash:         append([]byte(nil), in.ReasonHash...),
		RevocationDigest:   append([]byte(nil), in.RevocationDigest...),
		AggregateSignature: append([]byte(nil), in.AggregateSignature...),
		AggregatePublicKey: append([]byte(nil), in.AggregatePublicKey...),
		Bitmap:             append([]byte(nil), in.Bitmap...),
	}
	if in.ChainID != nil {
		out.ChainID = new(big.Int).Set(in.ChainID)
	}
	if in.SlashAmount != nil {
		out.SlashAmount = new(big.Int).Set(in.SlashAmount)
	}
	if in.CommitteePubKeyVersion != nil {
		out.CommitteePubKeyVersion = new(big.Int).Set(in.CommitteePubKeyVersion)
	}
	return out
}

func clonePendingTEEInfo(in PendingTEEInfo) PendingTEEInfo {
	out := PendingTEEInfo{Exists: in.Exists}
	out.Deposit = cloneBigInt(in.Deposit)
	out.Nonce = cloneBigInt(in.Nonce)
	out.CreatedAt = cloneBigInt(in.CreatedAt)
	out.ChallengeHash = in.ChallengeHash
	return out
}

func cloneActiveSetInfo(in ActiveSetInfo) ActiveSetInfo {
	return ActiveSetInfo{
		Epoch:                  cloneBigInt(in.Epoch),
		ActiveTEEs:             append([]string(nil), in.ActiveTEEs...),
		ActiveSetHash:          in.ActiveSetHash,
		PkCHHash:               in.PkCHHash,
		CommitteePubKeyVersion: cloneBigInt(in.CommitteePubKeyVersion),
		ActiveThreshold:        cloneBigInt(in.ActiveThreshold),
		ActiveLimit:            cloneBigInt(in.ActiveLimit),
	}
}

func cloneBigInt(in *big.Int) *big.Int {
	if in == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(in)
}
