package committee

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"committee-mvp/internal/chain"
	"committee-mvp/internal/rpc"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const teeRevocationStatusSigned = "signed"

var teeRevokeDomain = ethcrypto.Keccak256Hash([]byte("T_GOVERN_TEE_REVOKE_V1"))

type teeRevocationSession struct {
	requestID              string
	signSessionID          string
	chainID                *big.Int
	contractAddress        string
	hardwareID             []byte
	reasonHash             []byte
	slashAmount            *big.Int
	committeePubKeyVersion *big.Int
	revocationDigest       []byte
	status                 string
	txHash                 string
}

// RevokeTEE starts a committee threshold signing round for a TEE revocation.
func (s *Service) RevokeTEE(ctx context.Context, params rpc.RevokeTEEParams) (*rpc.RevokeTEEData, error) {
	_ = ctx
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return nil, fmt.Errorf("only coordinator can revoke tee")
	}
	if !s.isDKGReady() {
		return nil, fmt.Errorf("committee key not ready yet")
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
	hardwareID, err := parseRequiredBytes32(params.HardwareID, "hardware_id")
	if err != nil {
		return nil, err
	}
	reasonHash, err := parseRequiredBytes32(params.ReasonHash, "reason_hash")
	if err != nil {
		return nil, err
	}
	slashAmount, err := parseNonNegativeBigInt(params.SlashAmount, "slash_amount")
	if err != nil {
		return nil, err
	}

	revocationDigest, err := computeTEERevocationDigest(chainID, contractAddress, hardwareID[:], reasonHash[:], slashAmount, pubKeyVersion)
	if err != nil {
		return nil, err
	}
	signSessionID, err := s.startSignRequest(revocationDigest)
	if err != nil {
		return nil, err
	}
	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" {
		requestID, err = newSessionID("tee-revoke", revocationDigest)
		if err != nil {
			return nil, err
		}
	}

	session := &teeRevocationSession{
		requestID:              requestID,
		signSessionID:          signSessionID,
		chainID:                new(big.Int).Set(chainID),
		contractAddress:        contractAddress,
		hardwareID:             append([]byte(nil), hardwareID[:]...),
		reasonHash:             append([]byte(nil), reasonHash[:]...),
		slashAmount:            new(big.Int).Set(slashAmount),
		committeePubKeyVersion: new(big.Int).Set(pubKeyVersion),
		revocationDigest:       append([]byte(nil), revocationDigest...),
		status:                 redactStatusPending,
	}
	s.mu.Lock()
	s.teeRevocations[requestID] = session
	s.mu.Unlock()

	return teeRevocationDataFromSession(session), nil
}

// TEERevocation returns the aggregated revocation certificate if ready.
func (s *Service) TEERevocation(ctx context.Context, requestID string) (*rpc.TEERevocationData, error) {
	_ = ctx
	session, err := s.getTEERevocationSession(requestID)
	if err != nil {
		return nil, err
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	out := teeRevocationDataFromSession(session)
	if !ok {
		return out, nil
	}
	out.Status = teeRevocationStatusSigned
	out.AggregateSig = hex.EncodeToString(result.AggregateSig)
	out.AggregatePubKey = hex.EncodeToString(result.AggregatePublicKey)
	out.Bitmap = hex.EncodeToString(result.Bitmap)

	s.mu.Lock()
	if live := s.teeRevocations[session.requestID]; live != nil && live.status == redactStatusPending {
		live.status = teeRevocationStatusSigned
	}
	s.mu.Unlock()
	return out, nil
}

// SubmitTEERevocationOnChain submits the aggregate revocation signature to GSC.
func (s *Service) SubmitTEERevocationOnChain(ctx context.Context, requestID string) (*rpc.SubmitTEERevocationOnChainData, error) {
	session, err := s.getTEERevocationSession(requestID)
	if err != nil {
		return nil, err
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	if !ok {
		return nil, fmt.Errorf("tee revocation is not aggregated yet")
	}

	txHash, err := s.chainGateway.SubmitTEERevocation(ctx, chain.TEERevocationInput{
		RequestID:              session.requestID,
		ChainID:                new(big.Int).Set(session.chainID),
		ContractAddress:        session.contractAddress,
		HardwareID:             append([]byte(nil), session.hardwareID...),
		ReasonHash:             append([]byte(nil), session.reasonHash...),
		SlashAmount:            new(big.Int).Set(session.slashAmount),
		CommitteePubKeyVersion: new(big.Int).Set(session.committeePubKeyVersion),
		RevocationDigest:       append([]byte(nil), session.revocationDigest...),
		AggregateSignature:     append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey:     append([]byte(nil), result.AggregatePublicKey...),
		Bitmap:                 append([]byte(nil), result.Bitmap...),
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if live := s.teeRevocations[session.requestID]; live != nil {
		live.status = redactStatusSubmitted
		live.txHash = txHash
	}
	s.mu.Unlock()
	return &rpc.SubmitTEERevocationOnChainData{
		RequestID: requestID,
		SessionID: session.signSessionID,
		TxHash:    txHash,
		Status:    redactStatusSubmitted,
	}, nil
}

func (s *Service) getTEERevocationSession(requestID string) (*teeRevocationSession, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.teeRevocations[requestID]
	if !ok {
		return nil, fmt.Errorf("tee revocation not found: %s", requestID)
	}
	return cloneTEERevocationSession(session), nil
}

func teeRevocationDataFromSession(session *teeRevocationSession) *rpc.RevokeTEEData {
	return &rpc.RevokeTEEData{
		RequestID:              session.requestID,
		SessionID:              session.signSessionID,
		Status:                 session.status,
		HardwareID:             "0x" + hex.EncodeToString(session.hardwareID),
		ReasonHash:             "0x" + hex.EncodeToString(session.reasonHash),
		SlashAmount:            session.slashAmount.String(),
		CommitteePubKeyVersion: session.committeePubKeyVersion.String(),
		RevocationDigest:       hex.EncodeToString(session.revocationDigest),
		TxHash:                 session.txHash,
	}
}

func cloneTEERevocationSession(in *teeRevocationSession) *teeRevocationSession {
	if in == nil {
		return nil
	}
	out := *in
	out.chainID = new(big.Int).Set(in.chainID)
	out.hardwareID = append([]byte(nil), in.hardwareID...)
	out.reasonHash = append([]byte(nil), in.reasonHash...)
	out.slashAmount = new(big.Int).Set(in.slashAmount)
	out.committeePubKeyVersion = new(big.Int).Set(in.committeePubKeyVersion)
	out.revocationDigest = append([]byte(nil), in.revocationDigest...)
	return &out
}

func computeTEERevocationDigest(chainID *big.Int, contractAddress string, hardwareID, reasonHash []byte, slashAmount, pubKeyVersion *big.Int) ([]byte, error) {
	if len(hardwareID) != 32 {
		return nil, fmt.Errorf("hardware_id must be 32 bytes")
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
	addressType, err := ethabi.NewType("address", "", nil)
	if err != nil {
		return nil, err
	}
	var hardwareID32 [32]byte
	var reasonHash32 [32]byte
	copy(hardwareID32[:], hardwareID)
	copy(reasonHash32[:], reasonHash)
	args := ethabi.Arguments{{Type: bytes32Type}, {Type: uint256Type}, {Type: addressType}, {Type: bytes32Type}, {Type: bytes32Type}, {Type: uint256Type}, {Type: uint256Type}}
	packed, err := args.Pack(
		teeRevokeDomain,
		chainID,
		ethcommon.HexToAddress(contractAddress),
		hardwareID32,
		reasonHash32,
		slashAmount,
		pubKeyVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("abi pack tee revocation: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}
