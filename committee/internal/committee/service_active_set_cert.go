package committee

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"committee-mvp/internal/chain"
	"committee-mvp/internal/rpc"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const activeSetCertStatusCertified = "certified"

var activeSetCertDomain = ethcrypto.Keccak256Hash([]byte("T_GOVERN_ACTIVE_SET_CERT_V1"))

var errActiveSetCertNotFound = errors.New("active set certificate not found")

type activeSetCertSession struct {
	requestID              string
	signSessionID          string
	chainID                *big.Int
	contractAddress        string
	epoch                  *big.Int
	activeTEEList          []string
	activeSetHash          []byte
	activeThreshold        *big.Int
	activeLimit            *big.Int
	pkCHHash               []byte
	committeePubKeyVersion *big.Int
	activeSetCertDigest    []byte
	status                 string
}

// GenerateActiveSetCert queries GSC active-set facts through the chain gateway
// and starts a committee threshold signing session for Cert_e^A.
func (s *Service) GenerateActiveSetCert(ctx context.Context, params rpc.GenerateActiveSetCertParams) (*rpc.ActiveSetCertData, error) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return nil, fmt.Errorf("only coordinator can generate active set certificate")
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
	epoch, err := parseNonNegativeBigInt(params.Epoch, "epoch")
	if err != nil {
		return nil, err
	}
	if !epoch.IsUint64() {
		return nil, fmt.Errorf("epoch exceeds uint64")
	}

	chainInfo, err := s.chainGateway.GetActiveSetInfo(ctx, epoch)
	if err != nil {
		return nil, err
	}
	session, err := buildActiveSetCertSession(params.RequestID, chainID, contractAddress, epoch, chainInfo, params.ActiveTEEList)
	if err != nil {
		return nil, err
	}
	signSessionID, err := s.startSignRequest(session.activeSetCertDigest)
	if err != nil {
		return nil, err
	}
	session.signSessionID = signSessionID
	session.status = redactStatusPending

	s.mu.Lock()
	s.activeSetCerts[session.requestID] = session
	s.activeSetCertByEpoch[activeSetCertLookupKey(session.chainID, session.contractAddress, session.epoch)] = session.requestID
	s.activeSetCertByEpoch[session.epoch.String()] = session.requestID
	s.mu.Unlock()

	return activeSetCertDataFromSession(session), nil
}

// ActiveSetCert returns a previously generated Cert_e^A once the aggregate
// signing round has completed.
func (s *Service) ActiveSetCert(ctx context.Context, params rpc.GetActiveSetCertParams) (*rpc.ActiveSetCertData, error) {
	session, err := s.getActiveSetCertSession(params)
	if err != nil {
		if !isActiveSetCertNotFound(err) {
			return nil, err
		}
		generated, genErr := s.GenerateActiveSetCert(ctx, rpc.GenerateActiveSetCertParams{
			RequestID:       params.RequestID,
			ChainID:         params.ChainID,
			ContractAddress: params.ContractAddress,
			Epoch:           params.Epoch,
			ActiveTEEList:   params.ActiveTEEList,
		})
		if genErr != nil {
			return nil, genErr
		}
		return generated, nil
	}
	result, ok := s.lookupAggregateResult(session.signSessionID)
	out := activeSetCertDataFromSession(session)
	if !ok {
		return out, nil
	}
	out.Status = activeSetCertStatusCertified
	out.AggregateSig = hex.EncodeToString(result.AggregateSig)
	out.AggregatePubKey = hex.EncodeToString(result.AggregatePublicKey)
	out.Bitmap = hex.EncodeToString(result.Bitmap)
	out.CertEA = hex.EncodeToString(result.AggregateSig)

	s.mu.Lock()
	if live := s.activeSetCerts[session.requestID]; live != nil && live.status == redactStatusPending {
		live.status = activeSetCertStatusCertified
	}
	s.mu.Unlock()
	return out, nil
}

func buildActiveSetCertSession(requestID string, chainID *big.Int, contractAddress string, epoch *big.Int, chainInfo *chain.ActiveSetInfo, suppliedActiveTEEList []string) (*activeSetCertSession, error) {
	if chainInfo == nil {
		return nil, fmt.Errorf("active set info is missing")
	}
	activeTEEList, err := normalizeActiveTEEList(chainInfo.ActiveTEEs)
	if err != nil {
		return nil, fmt.Errorf("invalid chain active_tee_list: %w", err)
	}
	if len(activeTEEList) == 0 {
		return nil, fmt.Errorf("active set is empty for epoch %s", epoch.String())
	}
	if len(suppliedActiveTEEList) > 0 {
		supplied, err := normalizeActiveTEEList(suppliedActiveTEEList)
		if err != nil {
			return nil, fmt.Errorf("invalid supplied active_tee_list: %w", err)
		}
		if !equalStringSlices(activeTEEList, supplied) {
			return nil, fmt.Errorf("supplied active_tee_list does not match chain active set")
		}
	}
	activeSetHash, err := parseRequiredBytes32(chainInfo.ActiveSetHash, "active_set_hash")
	if err != nil {
		return nil, err
	}
	pkCHHash, err := parseRequiredBytes32(chainInfo.PkCHHash, "pk_ch_hash")
	if err != nil {
		return nil, err
	}
	if chainInfo.CommitteePubKeyVersion == nil || chainInfo.CommitteePubKeyVersion.Sign() <= 0 {
		return nil, fmt.Errorf("committee_pub_key_version is required")
	}
	if chainInfo.ActiveThreshold == nil || chainInfo.ActiveThreshold.Sign() <= 0 {
		return nil, fmt.Errorf("active_threshold is required")
	}
	if chainInfo.ActiveLimit == nil || chainInfo.ActiveLimit.Sign() <= 0 {
		return nil, fmt.Errorf("active_limit is required")
	}
	digest, err := computeActiveSetCertDigest(chainID, contractAddress, epoch, activeSetHash, chainInfo.ActiveThreshold, chainInfo.ActiveLimit, pkCHHash, chainInfo.CommitteePubKeyVersion)
	if err != nil {
		return nil, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = activeSetCertLookupKey(chainID, contractAddress, epoch)
	}
	return &activeSetCertSession{
		requestID:              requestID,
		chainID:                new(big.Int).Set(chainID),
		contractAddress:        contractAddress,
		epoch:                  new(big.Int).Set(epoch),
		activeTEEList:          activeTEEList,
		activeSetHash:          append([]byte(nil), activeSetHash[:]...),
		activeThreshold:        new(big.Int).Set(chainInfo.ActiveThreshold),
		activeLimit:            new(big.Int).Set(chainInfo.ActiveLimit),
		pkCHHash:               append([]byte(nil), pkCHHash[:]...),
		committeePubKeyVersion: new(big.Int).Set(chainInfo.CommitteePubKeyVersion),
		activeSetCertDigest:    append([]byte(nil), digest...),
	}, nil
}

func computeActiveSetCertDigest(chainID *big.Int, contractAddress string, epoch *big.Int, activeSetHash [32]byte, activeThreshold, activeLimit *big.Int, pkCHHash [32]byte, committeePubKeyVersion *big.Int) ([]byte, error) {
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
	args := ethabi.Arguments{
		{Type: bytes32Type},
		{Type: uint256Type},
		{Type: addressType},
		{Type: uint16Type},
		{Type: uint64Type},
		{Type: bytes32Type},
		{Type: uint256Type},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: uint256Type},
	}
	packed, err := args.Pack(
		activeSetCertDomain,
		chainID,
		ethcommon.HexToAddress(contractAddress),
		redactProtocolVersion,
		epoch.Uint64(),
		activeSetHash,
		activeThreshold,
		activeLimit,
		pkCHHash,
		committeePubKeyVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("abi pack active set cert: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}

func (s *Service) getActiveSetCertSession(params rpc.GetActiveSetCertParams) (*activeSetCertSession, error) {
	requestID := strings.TrimSpace(params.RequestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if requestID == "" {
		epoch, err := parseNonNegativeBigInt(params.Epoch, "epoch")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(params.ChainID) != "" || strings.TrimSpace(params.ContractAddress) != "" {
			chainID, err := parsePositiveBigInt(params.ChainID, "chain_id")
			if err != nil {
				return nil, err
			}
			contractAddress, err := normalizeOwnerAddress(params.ContractAddress)
			if err != nil {
				return nil, fmt.Errorf("invalid contract_address: %w", err)
			}
			requestID = s.activeSetCertByEpoch[activeSetCertLookupKey(chainID, contractAddress, epoch)]
		} else {
			requestID = s.activeSetCertByEpoch[epoch.String()]
		}
	}
	if requestID == "" {
		return nil, errActiveSetCertNotFound
	}
	session, ok := s.activeSetCerts[requestID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errActiveSetCertNotFound, requestID)
	}
	return cloneActiveSetCertSession(session), nil
}

func activeSetCertDataFromSession(session *activeSetCertSession) *rpc.ActiveSetCertData {
	return &rpc.ActiveSetCertData{
		RequestID:              session.requestID,
		SessionID:              session.signSessionID,
		Status:                 session.status,
		ChainID:                session.chainID.String(),
		ContractAddress:        session.contractAddress,
		Epoch:                  session.epoch.String(),
		ActiveTEEList:          append([]string(nil), session.activeTEEList...),
		ActiveSetHash:          hex.EncodeToString(session.activeSetHash),
		ActiveThreshold:        session.activeThreshold.String(),
		ActiveLimit:            session.activeLimit.String(),
		PkCHHash:               hex.EncodeToString(session.pkCHHash),
		CommitteePubKeyVersion: session.committeePubKeyVersion.String(),
		ActiveSetCertDigest:    hex.EncodeToString(session.activeSetCertDigest),
	}
}

func cloneActiveSetCertSession(in *activeSetCertSession) *activeSetCertSession {
	if in == nil {
		return nil
	}
	out := *in
	out.chainID = new(big.Int).Set(in.chainID)
	out.epoch = new(big.Int).Set(in.epoch)
	out.activeTEEList = append([]string(nil), in.activeTEEList...)
	out.activeSetHash = append([]byte(nil), in.activeSetHash...)
	out.activeThreshold = new(big.Int).Set(in.activeThreshold)
	out.activeLimit = new(big.Int).Set(in.activeLimit)
	out.pkCHHash = append([]byte(nil), in.pkCHHash...)
	out.committeePubKeyVersion = new(big.Int).Set(in.committeePubKeyVersion)
	out.activeSetCertDigest = append([]byte(nil), in.activeSetCertDigest...)
	return &out
}

func parseNonNegativeBigInt(value, field string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative decimal integer", field)
	}
	return n, nil
}

func isActiveSetCertNotFound(err error) bool {
	return errors.Is(err, errActiveSetCertNotFound)
}

func normalizeActiveTEEList(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for i, value := range in {
		parsed, err := bytes32FromHex(value)
		if err != nil {
			return nil, fmt.Errorf("active_tee_list[%d]: %w", i, err)
		}
		if parsed == ([32]byte{}) {
			return nil, fmt.Errorf("active_tee_list[%d] is zero", i)
		}
		out = append(out, "0x"+hex.EncodeToString(parsed[:]))
	}
	return out, nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func activeSetCertLookupKey(chainID *big.Int, contractAddress string, epoch *big.Int) string {
	parts := []string{strings.ToLower(strings.TrimSpace(contractAddress)), epochKeyForCert(epoch)}
	if chainID != nil {
		parts = append([]string{chainID.String()}, parts...)
	}
	return strings.Join(parts, ":")
}

func epochKeyForCert(epoch *big.Int) string {
	if epoch == nil {
		return "0"
	}
	return epoch.String()
}
