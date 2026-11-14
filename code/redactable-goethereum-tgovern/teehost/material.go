package teehost

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

type RedactionMaterialStore interface {
	Put(context.Context, RedactionMaterial) error
	Get(context.Context, [32]byte) (RedactionMaterial, bool, error)
	Delete(context.Context, [32]byte) error
}

type RedactionMaterial struct {
	RelayTTL   uint64
	ReqID      [32]byte
	Epoch      *big.Int
	BodyHashes [7][32]byte

	BlockNum   *big.Int
	Index      uint64
	ModifyType uint64
	DataAfter  []byte
	Meta       []byte

	SourceMessage []byte
	TargetMessage []byte
	OriginalR     []byte
	DReqInput     []byte

	CertReqAuthMsg         []byte
	CertReqAuthSig         []byte
	DReq                   [32]byte
	DPolicy                [32]byte
	CommitteePubKeyVersion []byte
	ExpiresAt              []byte

	ActiveSetHash       [32]byte
	ActiveTEEList       []byte
	ActiveHardwareIDs   [][32]byte
	ActiveSigPubKeys    [][32]byte
	ActiveSetCertInput  []byte
	ActiveTEECount      uint32
	ActiveSetCertLength uint32
	Threshold           int

	RPrimeHash [32]byte
	ResultHash [32]byte
}

type redactionMaterialRLP struct {
	RelayTTL   uint64
	ReqID      [32]byte
	Epoch      *big.Int
	BodyHashes [7][32]byte

	BlockNum   *big.Int
	Index      uint64
	ModifyType uint64
	DataAfter  []byte
	Meta       []byte

	SourceMessage []byte
	TargetMessage []byte
	OriginalR     []byte
	DReqInput     []byte

	CertReqAuthMsg         []byte
	CertReqAuthSig         []byte
	DReq                   [32]byte
	DPolicy                [32]byte
	CommitteePubKeyVersion []byte
	ExpiresAt              []byte

	ActiveSetHash       [32]byte
	ActiveTEEList       []byte
	ActiveHardwareIDs   [][32]byte
	ActiveSigPubKeys    [][32]byte
	ActiveSetCertInput  []byte
	ActiveTEECount      uint32
	ActiveSetCertLength uint32
	Threshold           uint64

	RPrimeHash [32]byte
	ResultHash [32]byte
}

func (m RedactionMaterial) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, redactionMaterialRLP{
		RelayTTL:               m.RelayTTL,
		ReqID:                  m.ReqID,
		Epoch:                  m.Epoch,
		BodyHashes:             m.BodyHashes,
		BlockNum:               m.BlockNum,
		Index:                  m.Index,
		ModifyType:             m.ModifyType,
		DataAfter:              m.DataAfter,
		Meta:                   m.Meta,
		SourceMessage:          m.SourceMessage,
		TargetMessage:          m.TargetMessage,
		OriginalR:              m.OriginalR,
		DReqInput:              m.DReqInput,
		CertReqAuthMsg:         m.CertReqAuthMsg,
		CertReqAuthSig:         m.CertReqAuthSig,
		DReq:                   m.DReq,
		DPolicy:                m.DPolicy,
		CommitteePubKeyVersion: m.CommitteePubKeyVersion,
		ExpiresAt:              m.ExpiresAt,
		ActiveSetHash:          m.ActiveSetHash,
		ActiveTEEList:          m.ActiveTEEList,
		ActiveHardwareIDs:      m.ActiveHardwareIDs,
		ActiveSigPubKeys:       m.ActiveSigPubKeys,
		ActiveSetCertInput:     m.ActiveSetCertInput,
		ActiveTEECount:         m.ActiveTEECount,
		ActiveSetCertLength:    m.ActiveSetCertLength,
		Threshold:              wireIndex(m.Threshold),
		RPrimeHash:             m.RPrimeHash,
		ResultHash:             m.ResultHash,
	})
}

func (m *RedactionMaterial) DecodeRLP(s *rlp.Stream) error {
	var wire redactionMaterialRLP
	if err := s.Decode(&wire); err != nil {
		return err
	}
	*m = RedactionMaterial{
		RelayTTL:               wire.RelayTTL,
		ReqID:                  wire.ReqID,
		Epoch:                  wire.Epoch,
		BodyHashes:             wire.BodyHashes,
		BlockNum:               wire.BlockNum,
		Index:                  wire.Index,
		ModifyType:             wire.ModifyType,
		DataAfter:              wire.DataAfter,
		Meta:                   wire.Meta,
		SourceMessage:          wire.SourceMessage,
		TargetMessage:          wire.TargetMessage,
		OriginalR:              wire.OriginalR,
		DReqInput:              wire.DReqInput,
		CertReqAuthMsg:         wire.CertReqAuthMsg,
		CertReqAuthSig:         wire.CertReqAuthSig,
		DReq:                   wire.DReq,
		DPolicy:                wire.DPolicy,
		CommitteePubKeyVersion: wire.CommitteePubKeyVersion,
		ExpiresAt:              wire.ExpiresAt,
		ActiveSetHash:          wire.ActiveSetHash,
		ActiveTEEList:          wire.ActiveTEEList,
		ActiveHardwareIDs:      wire.ActiveHardwareIDs,
		ActiveSigPubKeys:       wire.ActiveSigPubKeys,
		ActiveSetCertInput:     wire.ActiveSetCertInput,
		ActiveTEECount:         wire.ActiveTEECount,
		ActiveSetCertLength:    wire.ActiveSetCertLength,
		Threshold:              hostIndex(wire.Threshold),
		RPrimeHash:             wire.RPrimeHash,
		ResultHash:             wire.ResultHash,
	}
	return nil
}

func (m RedactionMaterial) Validate() error {
	if m.ReqID == ([32]byte{}) {
		return fmt.Errorf("reqID is empty")
	}
	if len(m.SourceMessage) > 0 && m.BodyHashes[1] != ([32]byte{}) {
		if crypto.Keccak256Hash(m.SourceMessage) != m.BodyHashes[1] {
			return fmt.Errorf("source message hash mismatch")
		}
	}
	if len(m.TargetMessage) > 0 && m.BodyHashes[2] != ([32]byte{}) {
		if crypto.Keccak256Hash(m.TargetMessage) != m.BodyHashes[2] {
			return fmt.Errorf("target message hash mismatch")
		}
	}
	if m.ActiveSetHash != ([32]byte{}) && m.BodyHashes[5] != ([32]byte{}) && m.ActiveSetHash != m.BodyHashes[5] {
		return fmt.Errorf("active set hash mismatch")
	}
	return nil
}

func (m RedactionMaterial) Clone() RedactionMaterial {
	out := m
	if m.Epoch != nil {
		out.Epoch = new(big.Int).Set(m.Epoch)
	}
	if m.BlockNum != nil {
		out.BlockNum = new(big.Int).Set(m.BlockNum)
	}
	out.DataAfter = append([]byte(nil), m.DataAfter...)
	out.Meta = append([]byte(nil), m.Meta...)
	out.SourceMessage = append([]byte(nil), m.SourceMessage...)
	out.TargetMessage = append([]byte(nil), m.TargetMessage...)
	out.OriginalR = append([]byte(nil), m.OriginalR...)
	out.DReqInput = append([]byte(nil), m.DReqInput...)
	out.CertReqAuthMsg = append([]byte(nil), m.CertReqAuthMsg...)
	out.CertReqAuthSig = append([]byte(nil), m.CertReqAuthSig...)
	out.CommitteePubKeyVersion = append([]byte(nil), m.CommitteePubKeyVersion...)
	out.ExpiresAt = append([]byte(nil), m.ExpiresAt...)
	out.ActiveTEEList = append([]byte(nil), m.ActiveTEEList...)
	out.ActiveHardwareIDs = append([][32]byte(nil), m.ActiveHardwareIDs...)
	out.ActiveSigPubKeys = append([][32]byte(nil), m.ActiveSigPubKeys...)
	out.ActiveSetCertInput = append([]byte(nil), m.ActiveSetCertInput...)
	return out
}

type MemoryRedactionMaterialStore struct {
	mu        sync.Mutex
	materials map[[32]byte]RedactionMaterial
}

func NewMemoryRedactionMaterialStore() *MemoryRedactionMaterialStore {
	return &MemoryRedactionMaterialStore{materials: make(map[[32]byte]RedactionMaterial)}
}

func (s *MemoryRedactionMaterialStore) Put(_ context.Context, material RedactionMaterial) error {
	if err := material.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materials[material.ReqID] = material.Clone()
	return nil
}

func (s *MemoryRedactionMaterialStore) Get(_ context.Context, reqID [32]byte) (RedactionMaterial, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	material, ok := s.materials[reqID]
	if !ok {
		return RedactionMaterial{}, false, nil
	}
	return material.Clone(), true, nil
}

func (s *MemoryRedactionMaterialStore) Delete(_ context.Context, reqID [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.materials, reqID)
	return nil
}

type RedactAuthorizedMessage struct {
	RelayTTL               uint64
	RequestID              string
	Version                uint16
	Epoch                  uint64
	ReqID                  [32]byte
	BodyHashes             [7][32]byte
	DReq                   [32]byte
	DPolicy                [32]byte
	ExpiresAt              *big.Int
	CommitteePubKeyVersion *big.Int
	AuthDigest             [32]byte
	AggregateSig           []byte
	AggregatePubKey        []byte
	Bitmap                 []byte
}

func (m RedactAuthorizedMessage) Clone() RedactAuthorizedMessage {
	out := m
	if m.ExpiresAt != nil {
		out.ExpiresAt = new(big.Int).Set(m.ExpiresAt)
	}
	if m.CommitteePubKeyVersion != nil {
		out.CommitteePubKeyVersion = new(big.Int).Set(m.CommitteePubKeyVersion)
	}
	out.AggregateSig = append([]byte(nil), m.AggregateSig...)
	out.AggregatePubKey = append([]byte(nil), m.AggregatePubKey...)
	out.Bitmap = append([]byte(nil), m.Bitmap...)
	return out
}

func (m RedactAuthorizedMessage) Fingerprint() [32]byte {
	encoded, err := rlp.EncodeToBytes(m)
	if err != nil {
		return crypto.Keccak256Hash(m.ReqID[:], m.AuthDigest[:], m.AggregateSig)
	}
	return crypto.Keccak256Hash(encoded)
}

type RedactAuthorization struct {
	ReqID                  [32]byte
	Epoch                  *big.Int
	DReq                   [32]byte
	DPolicy                [32]byte
	ExpiresAt              *big.Int
	CommitteePubKeyVersion *big.Int
	AuthDigest             [32]byte
	CertReqAuthMsg         []byte
	CertReqAuthSig         []byte
	AggregatePubKey        []byte
	Bitmap                 []byte
	ActiveSetHash          [32]byte
}

