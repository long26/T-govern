package contractService

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/chameleon/txstamp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	teeSelectionVRFReceiptTimeout = 45 * time.Second
	txReceiptPollInterval         = 500 * time.Millisecond
)

type TEEInfo struct {
	SigPubKey [32]byte
	MRENCLAVE [32]byte
	EnclavePK []byte
	Owner     common.Address
	Deposit   *big.Int
	IsActive  bool
}

type PendingTEEInfo struct {
	Exists        bool
	Deposit       *big.Int
	Nonce         *big.Int
	CreatedAt     *big.Int
	ChallengeHash [32]byte
}

type TEEStatusInfo struct {
	Status     uint8
	HardwareID [32]byte
}

type ActiveSetInfo struct {
	Epoch         *big.Int
	ActiveSetHash [32]byte
	// HardwareIDs is kept for API compatibility; values are active TEE sigPubKeys.
	HardwareIDs            [][32]byte
	PkCHHash               [32]byte
	CommitteePubKeyVersion *big.Int
	ActiveThreshold        *big.Int
	ActiveLimit            *big.Int
}

type RedactRequestInfo struct {
	StoredReqID      [32]byte
	Epoch            *big.Int
	DReq             [32]byte
	DPolicy          [32]byte
	Status           uint8
	AuthorizedAt     *big.Int
	ExecutedAt       *big.Int
	ExpiresAt        *big.Int
	RejectReasonHash [32]byte
}

type SystemParams struct {
	CommitteeVersion      *big.Int
	PkCHHash              [32]byte
	CurrentPkCHVersion    *big.Int
	ConfiguredEpochLength *big.Int
	Epoch                 *big.Int
	ActiveThreshold       *big.Int
	ActiveLimit           *big.Int
}

type TEERegistrationEvidence = contracts.PermissionManagerV3TEERegistrationEvidence
type RedactReqFields = contracts.PermissionManagerV3ReqFields

func unsupportedContractMethod(name string) error {
	return fmt.Errorf("%s is not supported by the current PermissionManagerV3 ABI", name)
}

func (s *ContractService) RegisterCommitteePubKey(pubKey [4]*big.Int) (string, error) {
	for i := 0; i < len(pubKey); i++ {
		if pubKey[i] == nil {
			return "", fmt.Errorf("pubKey[%d] is nil", i)
		}
	}

	auth := s.getAuth(context.Background())
	log.Debug("registerCommitteePubKey begin", "from", auth.From.Hex(), "p0", pubKey[0], "p1", pubKey[1], "p2", pubKey[2], "p3", pubKey[3])

	raw := &contracts.ContractsTransactorRaw{Contract: &s.contract.ContractsTransactor}
	tx, err := raw.Transact(auth, "registerCommitteePubKey", pubKey)
	if err != nil {
		log.Error("registerCommitteePubKey failed", "from", auth.From.Hex(), "err", err)
		return "", err
	}
	txHash := tx.Hash().Hex()
	log.Info("registerCommitteePubKey tx sent", "from", auth.From.Hex(), "tx", txHash)
	return txHash, nil
}

func (s *ContractService) SetTrustedMREnclave(mrEnclave [32]byte, trusted bool) (string, error) {
	auth := s.getAuth(context.Background())
	tx, err := s.contract.SetTrustedMREnclave(auth, mrEnclave, trusted)
	if err != nil {
		log.Error("Failed to set trusted MR_ENCLAVE", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

// 离职
func (s *ContractService) ProposeLeave() error {
	return unsupportedContractMethod("ProposeLeave")
}

func (s *ContractService) StakeTEE(value *big.Int) (string, error) {
	auth := s.getAuth(context.Background())
	auth.Value = value
	tx, err := s.contract.StakeTEE(auth)
	if err != nil {
		log.Error("Failed to stake tee", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) CancelPendingTEE() (string, error) {
	auth := s.getAuth(context.Background())
	tx, err := s.contract.CancelPendingTEE(auth)
	if err != nil {
		log.Error("Failed to cancel pending tee", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) RegisterTEE(owner common.Address, pendingNonce *big.Int, mrEnclave [32]byte, hardwareID [32]byte, evidence TEERegistrationEvidence, signature [2]*big.Int) (string, error) {
	auth := s.getAuth(context.Background())
	tx, err := s.contract.RegisterTEE(auth, owner, pendingNonce, mrEnclave, hardwareID, evidence, signature)
	if err != nil {
		log.Error("Failed to register tee", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) SubmitEraseProof(sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (string, error) {
	if epoch == nil || epoch.Sign() < 0 {
		return "", fmt.Errorf("invalid epoch")
	}
	if len(signature) != 65 {
		return "", fmt.Errorf("invalid signature length: got %d, want 65", len(signature))
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.SubmitEraseProof(auth, sigPubKey, epoch, eraseProofHash, signature)
	if err != nil {
		log.Error("Failed to submit tee erase proof", "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) SubmitTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (string, error) {
	if epoch == nil || epoch.Sign() <= 0 {
		return "", fmt.Errorf("invalid epoch")
	}
	if len(vrfProof) != 65 {
		return "", fmt.Errorf("invalid vrf proof length: got %d, want 65", len(vrfProof))
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.SubmitTEESelectionVRF(auth, epoch, sigPubKey, vrfResult, vrfProof)
	if err != nil {
		if strings.Contains(err.Error(), "VRF already submitted") && s.teeSelectionVRFExists(epoch, sigPubKey) {
			log.Info("TEE selection VRF already recorded", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]))
			return "", nil
		}
		log.Error("Failed to submit tee selection vrf", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "err", err)
		return "", err
	}
	txHash := tx.Hash()
	if err := s.waitForTEESelectionVRFReceipt(txHash, epoch, sigPubKey); err != nil {
		log.Error("TEE selection VRF transaction failed", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "tx", txHash.Hex(), "err", err)
		return txHash.Hex(), err
	}
	return txHash.Hex(), nil
}

func (s *ContractService) waitForTEESelectionVRFReceipt(txHash common.Hash, epoch *big.Int, sigPubKey [32]byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), teeSelectionVRFReceiptTimeout)
	defer cancel()

	ticker := time.NewTicker(txReceiptPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		receipt, err := s.eliClient.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil {
			if receipt.Status == types.ReceiptStatusFailed {
				if s.teeSelectionVRFExists(epoch, sigPubKey) {
					return nil
				}
				return fmt.Errorf("transaction %s reverted in block %s", txHash.Hex(), receipt.BlockNumber)
			}
			return nil
		}
		if err != nil {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("receipt timeout for %s: %v", txHash.Hex(), lastErr)
			}
			return fmt.Errorf("receipt timeout for %s", txHash.Hex())
		case <-ticker.C:
		}
	}
}

func (s *ContractService) teeSelectionVRFExists(epoch *big.Int, sigPubKey [32]byte) bool {
	submission, err := s.contract.GetTEESelectionVRF(&bind.CallOpts{Context: context.Background()}, epoch, sigPubKey)
	if err != nil {
		log.Debug("Failed to check tee selection VRF", "epoch", epoch, "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "err", err)
		return false
	}
	return submission.Exists
}

func (s *ContractService) RevokeTEE(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, signature [2]*big.Int) (string, error) {
	for i := 0; i < len(signature); i++ {
		if signature[i] == nil {
			signature[i] = big.NewInt(0)
		}
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.RevokeTEE(auth, sigPubKey, reasonHash, slashAmount, signature)
	if err != nil {
		log.Error("Failed to revoke tee", "sigPubKey", common.Bytes2Hex(sigPubKey[:]), "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) SubmitRedactAuthorization(req RedactReqFields, dPolicy [32]byte, expiresAt *big.Int, signature [2]*big.Int) (string, error) {
	if expiresAt == nil || expiresAt.Sign() <= 0 {
		return "", fmt.Errorf("invalid expiresAt")
	}
	for i := 0; i < len(signature); i++ {
		if signature[i] == nil {
			return "", fmt.Errorf("signature[%d] is nil", i)
		}
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.SubmitRedactAuthorization(auth, req, dPolicy, expiresAt, signature)
	if err != nil {
		log.Error("Failed to submit redact authorization", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) RejectRedactRequest(req RedactReqFields, dPolicy [32]byte, reasonHash [32]byte, signature [2]*big.Int) (string, error) {
	if reasonHash == ([32]byte{}) {
		return "", fmt.Errorf("invalid reasonHash")
	}
	for i := 0; i < len(signature); i++ {
		if signature[i] == nil {
			return "", fmt.Errorf("signature[%d] is nil", i)
		}
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.RejectRedactRequest(auth, req, dPolicy, reasonHash, signature)
	if err != nil {
		log.Error("Failed to reject redact request", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func (s *ContractService) ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (string, error) {
	if reqID == ([32]byte{}) {
		return "", fmt.Errorf("invalid reqID")
	}
	if rPrimeHash == ([32]byte{}) {
		return "", fmt.Errorf("invalid rPrimeHash")
	}
	if proofHash == ([32]byte{}) {
		return "", fmt.Errorf("invalid proofHash")
	}
	if resultHash == ([32]byte{}) {
		return "", fmt.Errorf("invalid resultHash")
	}
	if len(piReqA) == 0 {
		return "", fmt.Errorf("invalid piReqA")
	}
	tAuthStart := time.Now()
	auth := s.getAuth(context.Background())
	tAuthDur := time.Since(tAuthStart)

	tNonceStart := time.Now()
	nonce, err := s.eliClient.PendingNonceAt(context.Background(), auth.From)
	if err != nil {
		log.Error("Failed to retrieve nonce for execute redaction", "err", err)
		return "", err
	}
	tNonceDur := time.Since(tNonceStart)
	auth.Nonce = big.NewInt(int64(nonce))

	origBeforeSign := auth.BeforeSign
	var tStampDur time.Duration
	auth.BeforeSign = func(tx *types.Transaction) error {
		tStart := time.Now()
		defer func() { tStampDur = time.Since(tStart) }()
		if origBeforeSign != nil {
			return origBeforeSign(tx)
		}
		return nil
	}

	origSigner := auth.Signer
	var tSignDur time.Duration
	auth.Signer = func(signer types.Signer, address common.Address, tx *types.Transaction) (*types.Transaction, error) {
		tStart := time.Now()
		defer func() { tSignDur = time.Since(tStart) }()
		return origSigner(signer, address, tx)
	}

	tTxStart := time.Now()
	tx, err := s.contract.ExecuteRedaction(auth, reqID, rPrimeHash, proofHash, resultHash, piReqA)
	if err != nil {
		log.Error("Failed to execute redaction", "err", err)
		return "", err
	}
	tTxTotalDur := time.Since(tTxStart)
	tSendDur := tTxTotalDur - tStampDur - tSignDur
	if tSendDur < 0 {
		tSendDur = 0
	}

	reqIDHex := fmt.Sprintf("0x%x", reqID[:])
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_auth_prep", "req_id", reqIDHex, "duration_ns", tAuthDur.Nanoseconds())
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_nonce_query", "req_id", reqIDHex, "duration_ns", tNonceDur.Nanoseconds())
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_stamp_chameleon", "req_id", reqIDHex, "duration_ns", tStampDur.Nanoseconds())
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_secp256k1_sign", "req_id", reqIDHex, "duration_ns", tSignDur.Nanoseconds())
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_send_txpool", "req_id", reqIDHex, "duration_ns", tSendDur.Nanoseconds())
	return tx.Hash().Hex(), nil
}

// WaitTransactionReceipt waits until the transaction receipt is available or
// the caller's context expires. Receipt status is intentionally returned to the
// caller so the timing boundary can distinguish a mined revert from success.
func (s *ContractService) WaitTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(txReceiptPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		receipt, err := s.eliClient.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil {
			return receipt, nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("receipt wait for %s: %v: %w", txHash.Hex(), lastErr, ctx.Err())
			}
			return nil, fmt.Errorf("receipt wait for %s: %w", txHash.Hex(), ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *ContractService) GetRedactRequest(reqID [32]byte) (*RedactRequestInfo, error) {
	record, err := s.contract.GetRedactRequest(nil, reqID)
	if err != nil {
		return nil, err
	}
	return &RedactRequestInfo{
		StoredReqID:      record.StoredReqID,
		Epoch:            record.Epoch,
		DReq:             record.DReq,
		DPolicy:          record.DPolicy,
		Status:           record.Status,
		AuthorizedAt:     record.AuthorizedAt,
		ExecutedAt:       record.ExecutedAt,
		ExpiresAt:        record.ExpiresAt,
		RejectReasonHash: record.RejectReasonHash,
	}, nil
}

func (s *ContractService) IsRedactionExecuted(reqID [32]byte) (bool, error) {
	return s.contract.IsRedactionExecuted(nil, reqID)
}

func (s *ContractService) ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	if len(piReqA) == 0 {
		return [32]byte{}, fmt.Errorf("invalid piReqA")
	}
	return s.contract.ComputeRedactionProofHash(nil, reqID, dReq, activeSetHash, piReqA)
}

func (s *ContractService) ComputePkCHDigest(teeOwner common.Address, pkCH []byte) ([32]byte, error) {
	if teeOwner == (common.Address{}) {
		return [32]byte{}, fmt.Errorf("invalid tee owner")
	}
	if len(pkCH) == 0 {
		return [32]byte{}, fmt.Errorf("pkCH required")
	}
	return s.contract.ComputePkCHDigest(nil, teeOwner, pkCH)
}

func (s *ContractService) ComputeEraseProofDigest(sigPubKey [32]byte, teeOwner common.Address, epoch *big.Int, eraseProofHash [32]byte) ([32]byte, error) {
	if epoch == nil || epoch.Sign() < 0 {
		return [32]byte{}, fmt.Errorf("invalid epoch")
	}
	return s.contract.ComputeEraseProofDigest(nil, sigPubKey, teeOwner, epoch, eraseProofHash)
}

func (s *ContractService) GetSystemParams() (*SystemParams, error) {
	params, err := s.contract.GetSystemParams(nil)
	if err != nil {
		return nil, err
	}
	return &SystemParams{
		CommitteeVersion:      params.CommitteeVersion,
		PkCHHash:              params.PkCHHash,
		CurrentPkCHVersion:    params.CurrentPkCHVersion,
		ConfiguredEpochLength: params.ConfiguredEpochLength,
		Epoch:                 params.Epoch,
		ActiveThreshold:       params.ActiveThreshold,
		ActiveLimit:           params.ActiveLimit,
	}, nil
}

func (s *ContractService) GetPkCHHash() ([32]byte, error) {
	return s.contract.GetPkCHHash(nil)
}

func (s *ContractService) GetPkCH() ([]byte, error) {
	return s.contract.PkCH(nil)
}

func (s *ContractService) SetPkCH(pkCH []byte, signature []byte) (string, error) {
	if len(pkCH) == 0 {
		return "", fmt.Errorf("pkCH required")
	}
	if len(signature) == 0 {
		return "", fmt.Errorf("pkCH signature required")
	}
	auth := s.getAuth(context.Background())
	tx, err := s.contract.SetPkCH(auth, pkCH, signature)
	if err != nil {
		log.Error("Failed to set pkCH", "err", err)
		return "", err
	}
	return tx.Hash().Hex(), nil
}

// 自质押
func (s *ContractService) Stake(value *big.Int) error {
	_ = value
	return unsupportedContractMethod("Stake")
}

// 委托
func (s *ContractService) Delegate(value *big.Int, address common.Address) error {
	_ = value
	_ = address
	return unsupportedContractMethod("Delegate")
}

// 撤销对验证者的部分/全部委托
func (s *ContractService) RevokeDelegation(value *big.Int, address common.Address) error {
	_ = value
	_ = address
	return unsupportedContractMethod("RevokeDelegation")
}

// 设置每个轮次的区块长度
func (s *ContractService) SetRoundBlocks(round *big.Int) error {
	_ = round
	return unsupportedContractMethod("SetRoundBlocks")
}

// 设置轮次超时阈值
func (s *ContractService) SetRoundTimeout(time *big.Int) error {
	_ = time
	return unsupportedContractMethod("SetRoundTimeout")
}

// 更新最低保证金要求
func (s *ContractService) SetMinDeposit(value *big.Int) error {
	_ = value
	return unsupportedContractMethod("SetMinDeposit")
}

// 指定轮次的活跃节点数量
func (s *ContractService) GetActiveNodeNum(round *big.Int) (*big.Int, error) {
	_ = round
	return nil, unsupportedContractMethod("GetActiveNodeNum")
}

//func (s *ContractService) isInValidators(round *big.Int) (*big.Int, error) {
//	return s.contract.(nil, round)
//}

// 当前候选人数量
func (s *ContractService) GetCandidatesNum() (*big.Int, error) {
	return nil, unsupportedContractMethod("GetCandidatesNum")
}
func (s *ContractService) GetCandidates() ([]common.Address, error) {
	return nil, unsupportedContractMethod("GetCandidates")
}

// 活跃验证者数量
func (s *ContractService) GetValidatorsNum() (*big.Int, error) {
	return nil, unsupportedContractMethod("GetValidatorsNum")
}
func (s *ContractService) GetValidators() ([]common.Address, error) {
	return nil, unsupportedContractMethod("GetValidators")
}

// 申请退出的节点数量
func (s *ContractService) GetLeavingNodeNum() (*big.Int, error) {
	return nil, unsupportedContractMethod("GetLeavingNodeNum")
}

// 特定节点在指定轮次提交的公钥
func (s *ContractService) GetRoundPublicKey(round *big.Int, address common.Address) (*big.Int, error) {
	_ = round
	_ = address
	return nil, unsupportedContractMethod("GetRoundPublicKey")
}

func (s *ContractService) GetActiveTEEs() ([][32]byte, error) {
	return s.contract.GetActiveTEEs(nil)
}

func (s *ContractService) GetActiveSetInfo(epoch *big.Int) (*ActiveSetInfo, error) {
	if epoch == nil || epoch.Sign() < 0 {
		return nil, fmt.Errorf("invalid epoch")
	}
	activeSigPubKeys, err := s.contract.GetActiveSet(nil, epoch)
	if err != nil {
		return nil, err
	}
	activeSetHash, err := s.contract.GetActiveSetHash(nil, epoch)
	if err != nil {
		return nil, err
	}
	params, err := s.contract.GetSystemParams(nil)
	if err != nil {
		return nil, err
	}
	return &ActiveSetInfo{
		Epoch:                  new(big.Int).Set(epoch),
		ActiveSetHash:          activeSetHash,
		HardwareIDs:            activeSigPubKeys,
		PkCHHash:               params.PkCHHash,
		CommitteePubKeyVersion: params.CommitteeVersion,
		ActiveThreshold:        params.ActiveThreshold,
		ActiveLimit:            params.ActiveLimit,
	}, nil
}

func (s *ContractService) GetCandidateTEEs() ([][32]byte, error) {
	return s.contract.GetCandidateTEEs(nil)
}

func (s *ContractService) GetTEEInfo(sigPubKey [32]byte) (*TEEInfo, error) {
	mrEnclave, registeredSigPubKey, owner, deposit, isActive, err := s.contract.GetTEEInfo(nil, sigPubKey)
	if err != nil {
		return nil, err
	}
	enclavePK, err := s.GetTEEEnclavePK(sigPubKey)
	if err != nil {
		return nil, err
	}
	return &TEEInfo{
		SigPubKey: registeredSigPubKey,
		MRENCLAVE: mrEnclave,
		EnclavePK: enclavePK,
		Owner:     owner,
		Deposit:   deposit,
		IsActive:  isActive,
	}, nil
}

func (s *ContractService) GetTEEEnclavePK(sigPubKey [32]byte) ([]byte, error) {
	parts, err := s.contract.GetTEEEnclavePK(nil, sigPubKey)
	if err != nil {
		return nil, err
	}
	enclavePK := make([]byte, 0, 64)
	enclavePK = append(enclavePK, parts[0][:]...)
	enclavePK = append(enclavePK, parts[1][:]...)
	if len(enclavePK) != 64 {
		return nil, fmt.Errorf("invalid enclave public key length: got %d, want 64", len(enclavePK))
	}
	if isZeroBytes(enclavePK) {
		return nil, fmt.Errorf("empty enclave public key for sigPubKey %s", common.Bytes2Hex(sigPubKey[:]))
	}
	return enclavePK, nil
}

func isZeroBytes(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func (s *ContractService) GetPendingTEE(owner common.Address) (*PendingTEEInfo, error) {
	exists, deposit, nonce, createdAt, challengeHash, err := s.contract.GetPendingTEE(nil, owner)
	if err != nil {
		return nil, err
	}
	return &PendingTEEInfo{
		Exists:        exists,
		Deposit:       deposit,
		Nonce:         nonce,
		CreatedAt:     createdAt,
		ChallengeHash: challengeHash,
	}, nil
}

func (s *ContractService) GetRegistrationChallenge(owner common.Address) ([32]byte, error) {
	return s.contract.GetRegistrationChallenge(nil, owner)
}

func (s *ContractService) GetTEEStatusByOwner(owner common.Address) (*TEEStatusInfo, error) {
	status, hardwareID, err := s.contract.GetTEEStatusByOwner(nil, owner)
	if err != nil {
		return nil, err
	}
	return &TEEStatusInfo{
		Status:     status,
		HardwareID: hardwareID,
	}, nil
}

func (s *ContractService) GetActiveTEENum() (*big.Int, error) {
	activeTEEs, err := s.contract.GetActiveTEEs(nil)
	if err != nil {
		return nil, err
	}
	return big.NewInt(int64(len(activeTEEs))), nil
}

func (s *ContractService) getAuth(ctx context.Context) *bind.TransactOpts {
	auth := bind.NewKeyedTransactor(s.privateKey)
	auth.GasLimit = s.gasLimit
	if auth.GasLimit == 0 {
		auth.GasLimit = 30000000
	}
	auth.GasPrice, _ = s.eliClient.SuggestGasPrice(ctx)
	auth.BeforeSign = func(tx *types.Transaction) error {
		return txstamp.StampIfReady(tx, s.pkCHProvider)
	}
	return auth
}

// 解析交易input字段
func (s *ContractService) ParseTransactionInput(hash common.Hash) (string, error) {
	tx, _, err := s.eliClient.TransactionByHash(context.Background(), hash)
	if err != nil {
		log.Error("Failed to parse transaction", "err", err)
		return "", err
	}

	input := tx.Data()
	if len(input) == 0 {
		return "", nil
	}

	if utf8.Valid(input) {
		return string(input), nil
	}
	return fmt.Sprintf("0x%x", input), nil
}

// 处理新区块
//func (s *ContractService) handleNewBlock(block *types.Block) {
//	// 示例：每100个区块触发一次
//	if block.NumberU64()%100 == 0 {
//		if err := s.callContractLogic(); err != nil {
//			log.Error("Block-triggered contract call failed", "block", block.NumberU64(), "err", err)
//		}
//	}
//}

// 核心调用逻辑
//func (s *ContractService) callContractLogic() error {
//	// 获取当前值
//	current, err := s.contract.GetProtocolValue(nil)
//
//	if err != nil {
//		return err
//	}
//
//	// 设置新值（示例逻辑）
//	newValue := new(big.Int).Add(current, big.NewInt(1))
//	tx, err := s.contract.SetProtocolValue(s.prepareTransactOpts(), newValue)
//	if err != nil {
//		return err
//	}
//
//	// 等待交易上链
//	receipt, err := bind.WaitMined(context.Background(), s.eliClient, tx)
//	if err != nil {
//		return err
//	}
//
//	log.Info("Contract value updated",
//		"txHash", tx.Hash(),
//		"block", receipt.BlockNumber,
//		"newValue", newValue)
//
//	return nil
//}
//
//// 准备交易参数
//func (s *ContractService) prepareTransactOpts() *bind.TransactOpts {
//	nonce := s.txPool.Nonce(s.signer.From)
//	return &bind.TransactOpts{
//		From:     s.signer.From,
//		Signer:   s.signer.Signer,
//		Nonce:    new(big.Int).SetUint64(nonce),
//		GasPrice: s.gasPrice,
//		GasLimit: s.gasLimit,
//		Context:  context.Background(),
//	}
//}

// 加载私钥文件
//func loadSignerKey(keyFile, passphrase string) (*ecdsa.PrivateKey, error) {
//	keyData, err := os.ReadFile(keyFile)
//	if err != nil {
//		return nil, fmt.Errorf("read key file failed: %v", err)
//	}
//
//	key, err := keystore.DecryptKey(keyData, passphrase)
//	if err != nil {
//		return nil, fmt.Errorf("decrypt key failed: %v", err)
//	}
//
//	return key.PrivateKey, nil
//}
//
//// 集成到下载器完成回调（示例）
//func (s *ContractService) AfterSyncCompleted() {
//	// 当同步完成时触发调用
//	if err := s.callContractLogic(); err != nil {
//		log.Error("Post-sync contract call failed", "err", err)
//	}
//}
