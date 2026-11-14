package eth

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contractService"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/sgx"
	"github.com/ethereum/go-ethereum/teehost"
)

func parseBigIntFlexible(input string) (*big.Int, error) {
	if input == "" {
		return nil, errors.New("empty coordinate")
	}
	if len(input) > 2 && input[0] == '0' && (input[1] == 'x' || input[1] == 'X') {
		v, ok := new(big.Int).SetString(input[2:], 16)
		if !ok {
			return nil, fmt.Errorf("invalid hex integer: %s", input)
		}
		return v, nil
	}
	v, ok := new(big.Int).SetString(input, 10)
	if !ok {
		return nil, fmt.Errorf("invalid decimal integer: %s", input)
	}
	return v, nil
}

func parseCommitteePubKeyInput(input string) ([4]*big.Int, error) {
	var coords [4]*big.Int
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return coords, errors.New("empty input")
	}

	// Preferred format: {"PubKey":["...","...","...","..."]}
	var obj struct {
		PubKey []string `json:"PubKey"`
	}
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && len(obj.PubKey) > 0 {
		if len(obj.PubKey) != 4 {
			return coords, fmt.Errorf("invalid PubKey length: got %d, want 4", len(obj.PubKey))
		}
		for i := 0; i < 4; i++ {
			v, err := parseBigIntFlexible(obj.PubKey[i])
			if err != nil {
				return coords, fmt.Errorf("invalid PubKey[%d]: %w", i, err)
			}
			coords[i] = v
		}
		return coords, nil
	}

	// Also support top-level JSON array: ["...","...","...","..."]
	var arr []string
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && len(arr) > 0 {
		if len(arr) != 4 {
			return coords, fmt.Errorf("invalid PubKey length: got %d, want 4", len(arr))
		}
		for i := 0; i < 4; i++ {
			v, err := parseBigIntFlexible(arr[i])
			if err != nil {
				return coords, fmt.Errorf("invalid PubKey[%d]: %w", i, err)
			}
			coords[i] = v
		}
		return coords, nil
	}

	// Convenience format: one concatenated hex string (4 * 32-byte coordinates)
	hexStr := trimmed
	if len(hexStr) >= 2 && hexStr[0] == '0' && (hexStr[1] == 'x' || hexStr[1] == 'X') {
		hexStr = hexStr[2:]
	}
	if len(hexStr) != 64*4 {
		return coords, fmt.Errorf("invalid input, expected JSON with 4 coordinates or %d hex chars, got %d", 64*4, len(hexStr))
	}
	for i := 0; i < 4; i++ {
		chunk := hexStr[i*64 : (i+1)*64]
		v, err := parseBigIntFlexible("0x" + chunk)
		if err != nil {
			return coords, fmt.Errorf("invalid concatenated PubKey chunk %d: %w", i, err)
		}
		coords[i] = v
	}
	return coords, nil
}

func parseCommitteePubKeyBytes(input string) ([]byte, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("empty committee public key")
	}
	hexStr := trimmed
	if len(hexStr) >= 2 && hexStr[0] == '0' && (hexStr[1] == 'x' || hexStr[1] == 'X') {
		hexStr = hexStr[2:]
	}
	if len(hexStr) == 64*4 {
		raw, err := hex.DecodeString(hexStr)
		if err != nil {
			return nil, fmt.Errorf("decode committee public key hex: %w", err)
		}
		return raw, nil
	}

	coords, err := parseCommitteePubKeyInput(trimmed)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 128)
	for i, coord := range coords {
		if coord == nil {
			return nil, fmt.Errorf("committee public key coordinate %d is nil", i)
		}
		if coord.Sign() < 0 || coord.BitLen() > 256 {
			return nil, fmt.Errorf("committee public key coordinate %d does not fit 32 bytes", i)
		}
		chunk := make([]byte, 32)
		coord.FillBytes(chunk)
		out = append(out, chunk...)
	}
	return out, nil
}

var (
	//validators            = make(map[common.Address]bool)
	validatorMutex sync.RWMutex
	//modificationThreshold uint64 = 1 // 默认门限值为1
)

// 从数据库加载验证器信息
// 删了
//func loadValidators(db ethdb.Database) {
//	validatorMutex.Lock()
//	defer validatorMutex.Unlock()
//
//	data := rawdb.ReadValidators(db)
//	if data != nil {
//		for _, addr := range data {
//			validators[addr] = true
//		}
//	}
//}
//
//// 保存验证器信息到数据库
//func saveValidators(db ethdb.Database) {
//	validatorMutex.RLock()
//	defer validatorMutex.RUnlock()
//
//	var addrs []common.Address
//	for addr := range validators {
//		addrs = append(addrs, addr)
//	}
//	rawdb.WriteValidators(db, addrs)
//}

// SetModificationThreshold 设置修改门限值
//func (e *Ethereum) SetModificationThreshold(threshold uint64) error {
//	if threshold == 0 {
//		return errors.New("threshold must be greater than 0")
//	}
//
//	validatorMutex.Lock()
//	defer validatorMutex.Unlock()
//
//	modificationThreshold = threshold
//	return nil
//}

//IsValidator 检查地址是否为权限节点

// GetModificationThreshold 获取当前修改门限值
//func (e *Ethereum) GetModificationThreshold() uint64 {
//	validatorMutex.RLock()
//	defer validatorMutex.RUnlock()
//
//	return modificationThreshold
//}

// GetValidators 获取所有权限节点
func (e *Ethereum) GetValidators() []common.Address {

	return contractService.GetValidators()
}

// SetValidator 设置验证者地址
//func (s *Ethereum) SetValidator(address common.Address) error {
//	// TODO: 实现验证者设置逻辑
//	// 这里应该添加验证者地址到授权列表中
//	return nil
//}

func (s *Ethereum) Test() string {
	return "this is a test"
}

// SetThreshold 设置门限值
func (s *Ethereum) SetThreshold(threshold uint64) error {
	if threshold == 0 {
		return errors.New("threshold must be greater than 0")
	}

	validatorMutex.Lock()
	defer validatorMutex.Unlock()
	return contractService.SetMinDeposit(big.NewInt(int64(threshold)))
}

// Stake 提交质押
func (e *Ethereum) Stake(value *big.Int) error {
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	//if validators[address] {
	//	return errors.New("validator already exists")
	//}
	return contractService.Stake(value)
	//validators[address] = true
	//saveValidators(e.chainDb)
}

func (e *Ethereum) GetActiveNodeNum(round *big.Int) (*big.Int, error) {
	return contractService.GetActiveNodeNum(round)
}
func (s *Ethereum) GetCandidatesNum() (*big.Int, error) {
	return contractService.GetCandidatesNum()
}
func (s *Ethereum) GetValidatorsNum() (*big.Int, error) {
	return contractService.GetValidatorsNum()
}
func (s *Ethereum) Delegate(value *big.Int, address common.Address) error {
	err := contractService.Delegate(value, address)
	return err
}
func (s *Ethereum) ParseTransactionInput(hash common.Hash) (string, error) {
	return contractService.ParseTransactionInput(hash)
}

// hexStringToBytes32 将十六进制字符串转换为 [32]byte 数组
func hexStringToBytes32(hexStr string) ([32]byte, error) {
	var result [32]byte

	// 移除 0x 前缀（如果有）
	if len(hexStr) >= 2 && hexStr[0] == '0' && (hexStr[1] == 'x' || hexStr[1] == 'X') {
		hexStr = hexStr[2:]
	}

	// 检查长度：32字节 = 64个十六进制字符
	if len(hexStr) != 64 {
		return result, fmt.Errorf("invalid hex length: got %d, want 64", len(hexStr))
	}

	// 解码十六进制字符串
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return result, fmt.Errorf("failed to decode hex string: %v", err)
	}

	// 拷贝到结果数组
	copy(result[:], decoded)
	return result, nil
}

func hexStringToBytes(hexStr string) ([]byte, error) {
	hexStr = strings.TrimSpace(hexStr)
	if len(hexStr) >= 2 && hexStr[0] == '0' && (hexStr[1] == 'x' || hexStr[1] == 'X') {
		hexStr = hexStr[2:]
	}
	if hexStr == "" {
		return nil, errors.New("empty bytes")
	}
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex string: %w", err)
	}
	return decoded, nil
}

func validateEnclavePK(enclavePK []byte) error {
	if len(enclavePK) != 64 {
		return fmt.Errorf("invalid enclave public key length: got %d, want 64", len(enclavePK))
	}
	for _, b := range enclavePK {
		if b != 0 {
			return nil
		}
	}
	return errors.New("enclave public key must not be all zero")
}

func enclavePKToBytes32Pair(enclavePK []byte) ([2][32]byte, error) {
	var out [2][32]byte
	if err := validateEnclavePK(enclavePK); err != nil {
		return out, err
	}
	copy(out[0][:], enclavePK[:32])
	copy(out[1][:], enclavePK[32:])
	return out, nil
}

func unmarshalHexSignatureToG1Coords(sigHex string) ([2]*big.Int, error) {
	var coords [2]*big.Int
	sigHex = strings.TrimSpace(sigHex)
	if len(sigHex) >= 2 && sigHex[0] == '0' && (sigHex[1] == 'x' || sigHex[1] == 'X') {
		sigHex = sigHex[2:]
	}
	if len(sigHex) == 0 {
		return coords, errors.New("empty signature")
	}
	raw, err := hex.DecodeString(sigHex)
	if err != nil {
		return coords, fmt.Errorf("decode signature hex: %w", err)
	}
	if len(raw) != 64 {
		return coords, fmt.Errorf("invalid signature length: got %d, want 64", len(raw))
	}
	p := new(bn256.G1)
	if _, err := p.Unmarshal(raw); err != nil {
		return coords, fmt.Errorf("unmarshal bn256 g1 signature: %w", err)
	}
	norm := p.Marshal()
	coords[0] = new(big.Int).SetBytes(norm[:32])
	coords[1] = new(big.Int).SetBytes(norm[32:])
	return coords, nil
}

func unmarshalOptionalHexSignatureToG1Coords(sigHex string) ([2]*big.Int, error) {
	sigHex = strings.TrimSpace(sigHex)
	if sigHex == "" {
		return [2]*big.Int{big.NewInt(0), big.NewInt(0)}, nil
	}
	return unmarshalHexSignatureToG1Coords(sigHex)
}

func unmarshalHexSignatureBytes(sigHex string) ([]byte, error) {
	sigHex = strings.TrimSpace(sigHex)
	if len(sigHex) >= 2 && sigHex[0] == '0' && (sigHex[1] == 'x' || sigHex[1] == 'X') {
		sigHex = sigHex[2:]
	}
	if len(sigHex) == 0 {
		return nil, errors.New("empty signature")
	}
	raw, err := hex.DecodeString(sigHex)
	if err != nil {
		return nil, fmt.Errorf("decode signature hex: %w", err)
	}
	if len(raw) != 65 {
		return nil, fmt.Errorf("invalid signature length: got %d, want 65", len(raw))
	}
	return raw, nil
}

func bytes32ToHexString(v [32]byte) string {
	return "0x" + hex.EncodeToString(v[:])
}

func bytesToHexString(v []byte) string {
	return "0x" + hex.EncodeToString(v)
}

func decodeHexOrText(input string) ([]byte, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("empty bytes")
	}
	if len(input) >= 2 && input[0] == '0' && (input[1] == 'x' || input[1] == 'X') {
		raw, err := hex.DecodeString(input[2:])
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	return []byte(input), nil
}

func decodeOptionalHexOrText(input string) ([]byte, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	return decodeHexOrText(input)
}

type redactShareProofInput struct {
	SigPubKey string `json:"SigPubKey"`
	SI        string `json:"SI"`
	Sigma     string `json:"Sigma"`
}

type redactShareProofABI struct {
	SigPubKey [32]byte
	SI        []byte
	Sigma     []byte
}

func encodePiReqAFromShareProofs(proofs []redactShareProofInput) ([]byte, error) {
	if len(proofs) == 0 {
		return nil, errors.New("PiReqA or ShareProofs required")
	}
	shareProofType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "sigPubKey", Type: "bytes32"},
		{Name: "sI", Type: "bytes"},
		{Name: "sigma", Type: "bytes"},
	})
	if err != nil {
		return nil, err
	}
	values := make([]redactShareProofABI, 0, len(proofs))
	for i, proof := range proofs {
		sigPubKey, err := hexStringToBytes32(proof.SigPubKey)
		if err != nil {
			return nil, fmt.Errorf("invalid ShareProofs[%d].SigPubKey: %v", i, err)
		}
		sI, err := decodeHexOrText(proof.SI)
		if err != nil {
			return nil, fmt.Errorf("invalid ShareProofs[%d].SI: %v", i, err)
		}
		sigma, err := unmarshalHexSignatureBytes(proof.Sigma)
		if err != nil {
			return nil, fmt.Errorf("invalid ShareProofs[%d].Sigma: %v", i, err)
		}
		values = append(values, redactShareProofABI{
			SigPubKey: sigPubKey,
			SI:        sI,
			Sigma:     sigma,
		})
	}
	args := abi.Arguments{{Type: shareProofType}}
	return args.Pack(values)
}

func resolvePiReqA(piReqA string, shareProofs []redactShareProofInput) ([]byte, error) {
	if strings.TrimSpace(piReqA) != "" {
		return decodeHexOrText(piReqA)
	}
	return encodePiReqAFromShareProofs(shareProofs)
}

func (s *Ethereum) StakeTEE(jsonStr string) (string, error) {
	start := time.Now()
	var temp struct {
		Value string `json:"Value"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		log.Error("解析出错:", "err", err, "str", jsonStr)
		return "", err
	}

	value, err := parseBigIntFlexible(temp.Value)
	if err != nil {
		return "", fmt.Errorf("invalid Value: %v", err)
	}
	if value.Sign() <= 0 {
		return "", errors.New("Value must be positive")
	}

	txHash, err := contractService.StakeTEE(value)
	if err != nil {
		log.Error("StakeTEE error", "err", err)
		return "", err
	}
	log.Info("E2E_TIMING", "stage", "system_init", "event", "eth_stakeTEE_rpc", "tx", txHash, "duration_ns", time.Since(start).Nanoseconds())
	return txHash, nil
}

func (s *Ethereum) CancelPendingTEE() (string, error) {
	return contractService.CancelPendingTEE()
}

func (s *Ethereum) RegisterTEE(jsonStr string) (string, error) {
	start := time.Now()
	var temp struct {
		Owner            string `json:"Owner"`
		PendingNonce     string `json:"PendingNonce"`
		MR_ENCLAVE       string `json:"MR_ENCLAVE"`
		HardwareId       string `json:"HardwareId"`
		SigPubKey        string `json:"SigPubKey"`
		VrfPubKey        string `json:"VrfPubKey"`
		EnclavePK        string `json:"EnclavePK"`
		MrSigner         string `json:"MrSigner"`
		QuoteHash        string `json:"QuoteHash"`
		RegisterCertHash string `json:"RegisterCertHash"`
		ChallengeHash    string `json:"ChallengeHash"`
		Signature        string `json:"Signature"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		log.Error("解析出错:", "err", err, "str", jsonStr)
		return "", err
	}

	if !common.IsHexAddress(temp.Owner) {
		return "", errors.New("invalid Owner address")
	}
	owner := common.HexToAddress(temp.Owner)

	pendingNonce, err := parseBigIntFlexible(temp.PendingNonce)
	if err != nil {
		return "", fmt.Errorf("invalid PendingNonce: %v", err)
	}
	if pendingNonce.Sign() < 0 {
		return "", errors.New("PendingNonce must be non-negative")
	}

	// 转换十六进制字符串为 [32]byte
	mrEnclave, err := hexStringToBytes32(temp.MR_ENCLAVE)
	if err != nil {
		return "", fmt.Errorf("invalid MR_ENCLAVE: %v", err)
	}

	hardwareId, err := hexStringToBytes32(temp.HardwareId)
	if err != nil {
		return "", fmt.Errorf("invalid HardwareId: %v", err)
	}

	sigPubKey, err := hexStringToBytes32(temp.SigPubKey)
	if err != nil {
		return "", fmt.Errorf("invalid SigPubKey: %v", err)
	}
	vrfPubKey, err := hexStringToBytes32(temp.VrfPubKey)
	if err != nil {
		return "", fmt.Errorf("invalid VrfPubKey: %v", err)
	}
	enclavePK, err := hexStringToBytes(temp.EnclavePK)
	if err != nil {
		return "", fmt.Errorf("invalid EnclavePK: %v", err)
	}
	enclavePKParts, err := enclavePKToBytes32Pair(enclavePK)
	if err != nil {
		return "", err
	}
	mrSigner, err := hexStringToBytes32(temp.MrSigner)
	if err != nil {
		return "", fmt.Errorf("invalid MrSigner: %v", err)
	}
	quoteHash, err := hexStringToBytes32(temp.QuoteHash)
	if err != nil {
		return "", fmt.Errorf("invalid QuoteHash: %v", err)
	}
	registerCertHash, err := hexStringToBytes32(temp.RegisterCertHash)
	if err != nil {
		return "", fmt.Errorf("invalid RegisterCertHash: %v", err)
	}
	challengeHash, err := hexStringToBytes32(temp.ChallengeHash)
	if err != nil {
		return "", fmt.Errorf("invalid ChallengeHash: %v", err)
	}

	signature, err := unmarshalHexSignatureToG1Coords(temp.Signature)
	if err != nil {
		return "", fmt.Errorf("invalid Signature: %v", err)
	}

	evidence := contractService.TEERegistrationEvidence{
		SigPubKey:        sigPubKey,
		VrfPubKey:        vrfPubKey,
		EnclavePK:        enclavePKParts,
		MrSigner:         mrSigner,
		QuoteHash:        quoteHash,
		RegisterCertHash: registerCertHash,
		ChallengeHash:    challengeHash,
	}
	txHash, err := contractService.RegisterTEE(owner, pendingNonce, mrEnclave, hardwareId, evidence, signature)
	if err != nil {
		log.Error("RegisterTEE error", "err", err)
		return "", err
	}
	log.Info("E2E_TIMING", "stage", "system_init", "event", "eth_registerTEE_rpc", "tx", txHash, "owner", owner.Hex(), "hardware_id", temp.HardwareId, "sig_pub_key", temp.SigPubKey, "duration_ns", time.Since(start).Nanoseconds())

	return txHash, nil
}

func (s *Ethereum) SubmitRedactAuthorization(jsonStr string) (string, error) {
	start := time.Now()
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	var temp struct {
		RequestID  string   `json:"RequestID"`
		Version    uint16   `json:"Version"`
		Epoch      string   `json:"Epoch"`
		ReqID      string   `json:"ReqID"`
		BodyHashes []string `json:"BodyHashes"`
		DPolicy    string   `json:"DPolicy"`
		ExpiresAt  string   `json:"ExpiresAt"`
		Signature  string   `json:"Signature"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return "", fmt.Errorf("invalid submit redact authorization json: %v", err)
	}
	if temp.Version == 0 {
		return "", errors.New("Version is required")
	}
	epoch, err := parseBigIntFlexible(temp.Epoch)
	if err != nil || epoch.Sign() < 0 || !epoch.IsUint64() {
		return "", fmt.Errorf("invalid Epoch")
	}
	reqID, err := hexStringToBytes32(temp.ReqID)
	if err != nil {
		return "", fmt.Errorf("invalid ReqID: %v", err)
	}
	if len(temp.BodyHashes) != 7 {
		return "", fmt.Errorf("invalid BodyHashes length: got %d want 7", len(temp.BodyHashes))
	}
	var bodyHashes [7][32]byte
	for i, value := range temp.BodyHashes {
		bodyHashes[i], err = hexStringToBytes32(value)
		if err != nil {
			return "", fmt.Errorf("invalid BodyHashes[%d]: %v", i, err)
		}
	}
	dPolicy, err := hexStringToBytes32(temp.DPolicy)
	if err != nil {
		return "", fmt.Errorf("invalid DPolicy: %v", err)
	}
	expiresAt, err := parseBigIntFlexible(temp.ExpiresAt)
	if err != nil || expiresAt.Sign() <= 0 {
		return "", fmt.Errorf("invalid ExpiresAt")
	}
	signature, err := unmarshalHexSignatureToG1Coords(temp.Signature)
	if err != nil {
		return "", fmt.Errorf("invalid Signature: %v", err)
	}

	req := contractService.RedactReqFields{
		Version:    temp.Version,
		Epoch:      epoch.Uint64(),
		ReqID:      reqID,
		BodyHashes: bodyHashes,
	}
	txHash, err := contractService.SubmitRedactAuthorization(req, dPolicy, expiresAt, signature)
	if err == nil {
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "eth_submit_redact_authorization", "req_id", bytes32ToHexString(reqID), "tx", txHash, "duration_ns", time.Since(start).Nanoseconds())
	}
	return txHash, err
}

func (s *Ethereum) BroadcastRedactAuthorization(jsonStr string) (map[string]interface{}, error) {
	start := time.Now()
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	var temp struct {
		RequestID              string   `json:"RequestID"`
		Version                uint16   `json:"Version"`
		Epoch                  string   `json:"Epoch"`
		ReqID                  string   `json:"ReqID"`
		BodyHashes             []string `json:"BodyHashes"`
		DReq                   string   `json:"DReq"`
		DPolicy                string   `json:"DPolicy"`
		ExpiresAt              string   `json:"ExpiresAt"`
		CommitteePubKeyVersion string   `json:"CommitteePubKeyVersion"`
		AuthDigest             string   `json:"AuthDigest"`
		Signature              string   `json:"Signature"`
		AggregateSig           string   `json:"AggregateSig"`
		AggregatePubKey        string   `json:"AggregatePubKey"`
		Bitmap                 string   `json:"Bitmap"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return nil, fmt.Errorf("invalid broadcast redact authorization json: %v", err)
	}
	if temp.Version == 0 {
		return nil, errors.New("Version is required")
	}
	epoch, err := parseBigIntFlexible(temp.Epoch)
	if err != nil || epoch.Sign() < 0 || !epoch.IsUint64() {
		return nil, fmt.Errorf("invalid Epoch")
	}
	reqID, err := hexStringToBytes32(temp.ReqID)
	if err != nil {
		return nil, fmt.Errorf("invalid ReqID: %v", err)
	}
	if len(temp.BodyHashes) != 7 {
		return nil, fmt.Errorf("invalid BodyHashes length: got %d want 7", len(temp.BodyHashes))
	}
	var bodyHashes [7][32]byte
	for i, value := range temp.BodyHashes {
		bodyHashes[i], err = hexStringToBytes32(value)
		if err != nil {
			return nil, fmt.Errorf("invalid BodyHashes[%d]: %v", i, err)
		}
	}
	dPolicy, err := hexStringToBytes32(temp.DPolicy)
	if err != nil {
		return nil, fmt.Errorf("invalid DPolicy: %v", err)
	}
	var dReq [32]byte
	if strings.TrimSpace(temp.DReq) != "" {
		dReq, err = hexStringToBytes32(temp.DReq)
		if err != nil {
			return nil, fmt.Errorf("invalid DReq: %v", err)
		}
	}
	var authDigest [32]byte
	if strings.TrimSpace(temp.AuthDigest) != "" {
		authDigest, err = hexStringToBytes32(temp.AuthDigest)
		if err != nil {
			return nil, fmt.Errorf("invalid AuthDigest: %v", err)
		}
	}
	expiresAt, err := parseBigIntFlexible(temp.ExpiresAt)
	if err != nil || expiresAt.Sign() <= 0 {
		return nil, fmt.Errorf("invalid ExpiresAt")
	}
	var cpkv *big.Int
	if strings.TrimSpace(temp.CommitteePubKeyVersion) != "" {
		cpkv, err = parseBigIntFlexible(temp.CommitteePubKeyVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid CommitteePubKeyVersion: %v", err)
		}
	}
	sigStr := strings.TrimSpace(temp.AggregateSig)
	if sigStr == "" {
		sigStr = strings.TrimSpace(temp.Signature)
	}
	sigBytes, err := decodeOptionalHexOrText(sigStr)
	if err != nil || len(sigBytes) == 0 {
		return nil, fmt.Errorf("invalid Signature/AggregateSig: %v", err)
	}
	aggPubKeyBytes, err := decodeOptionalHexOrText(temp.AggregatePubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid AggregatePubKey: %v", err)
	}
	bitmapBytes, err := decodeOptionalHexOrText(temp.Bitmap)
	if err != nil {
		return nil, fmt.Errorf("invalid Bitmap: %v", err)
	}

	msg := teehost.RedactAuthorizedMessage{
		RelayTTL:               s.protocolManager.defaultDKRSRelayTTL(),
		RequestID:              temp.RequestID,
		Version:                temp.Version,
		Epoch:                  epoch.Uint64(),
		ReqID:                  reqID,
		BodyHashes:             bodyHashes,
		DReq:                   dReq,
		DPolicy:                dPolicy,
		ExpiresAt:              expiresAt,
		CommitteePubKeyVersion: cpkv,
		AuthDigest:             authDigest,
		AggregateSig:           sigBytes,
		AggregatePubKey:        aggPubKeyBytes,
		Bitmap:                 bitmapBytes,
	}

	status := "broadcasted"
	if host, ok := s.protocolManager.teeHost.(interface {
		HandleRedactAuthorizedMessage(context.Context, teehost.RedactAuthorizedMessage, teehost.ProtocolNetwork) error
	}); ok && host != nil {
		if err := host.HandleRedactAuthorizedMessage(context.Background(), msg, s.protocolManager); err != nil {
			log.Warn("Local TEE host HandleRedactAuthorizedMessage failed", "err", err)
		} else {
			status = "registered_and_broadcasted"
		}
	}
	if err := s.protocolManager.BroadcastRedactAuthorized(context.Background(), msg); err != nil {
		log.Warn("BroadcastRedactAuthorized to peers failed", "err", err)
	}
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "eth_broadcast_redact_authorization", "req_id", bytes32ToHexString(reqID), "duration_ns", time.Since(start).Nanoseconds())
	return map[string]interface{}{
		"Status":    status,
		"ReqID":     bytes32ToHexString(reqID),
		"RequestID": temp.RequestID,
		"Epoch":     epoch.String(),
	}, nil
}

func (s *Ethereum) RejectRedactRequest(jsonStr string) (string, error) {
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	var temp struct {
		RequestID  string   `json:"RequestID"`
		Version    uint16   `json:"Version"`
		Epoch      string   `json:"Epoch"`
		ReqID      string   `json:"ReqID"`
		BodyHashes []string `json:"BodyHashes"`
		DPolicy    string   `json:"DPolicy"`
		ReasonHash string   `json:"ReasonHash"`
		Signature  string   `json:"Signature"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return "", fmt.Errorf("invalid reject redact request json: %v", err)
	}
	if temp.Version == 0 {
		return "", errors.New("Version is required")
	}
	epoch, err := parseBigIntFlexible(temp.Epoch)
	if err != nil || epoch.Sign() < 0 || !epoch.IsUint64() {
		return "", fmt.Errorf("invalid Epoch")
	}
	reqID, err := hexStringToBytes32(temp.ReqID)
	if err != nil {
		return "", fmt.Errorf("invalid ReqID: %v", err)
	}
	if len(temp.BodyHashes) != 7 {
		return "", fmt.Errorf("invalid BodyHashes length: got %d want 7", len(temp.BodyHashes))
	}
	var bodyHashes [7][32]byte
	for i, value := range temp.BodyHashes {
		bodyHashes[i], err = hexStringToBytes32(value)
		if err != nil {
			return "", fmt.Errorf("invalid BodyHashes[%d]: %v", i, err)
		}
	}
	dPolicy, err := hexStringToBytes32(temp.DPolicy)
	if err != nil {
		return "", fmt.Errorf("invalid DPolicy: %v", err)
	}
	reasonHash, err := hexStringToBytes32(temp.ReasonHash)
	if err != nil {
		return "", fmt.Errorf("invalid ReasonHash: %v", err)
	}
	if reasonHash == ([32]byte{}) {
		return "", errors.New("ReasonHash is required")
	}
	signature, err := unmarshalHexSignatureToG1Coords(temp.Signature)
	if err != nil {
		return "", fmt.Errorf("invalid Signature: %v", err)
	}

	req := contractService.RedactReqFields{
		Version:    temp.Version,
		Epoch:      epoch.Uint64(),
		ReqID:      reqID,
		BodyHashes: bodyHashes,
	}
	return contractService.RejectRedactRequest(req, dPolicy, reasonHash, signature)
}

func (s *Ethereum) BuildRedactRequest(jsonStr string) (map[string]interface{}, error) {
	start := time.Now()
	var temp struct {
		BlockNum     string `json:"BlockNum"`
		Index        uint64 `json:"Index"`
		ModifyType   uint64 `json:"ModifyType"`
		DataAfter    string `json:"DataAfter"`
		RedactReason string `json:"RedactReason"`
		Meta         string `json:"Meta"`
		ReqID        string `json:"ReqID"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return nil, fmt.Errorf("invalid build redact request json: %v", err)
	}
	num, err := parseBigIntFlexible(temp.BlockNum)
	if err != nil || num.Sign() < 0 {
		return nil, errors.New("invalid BlockNum")
	}
	block := s.blockchain.GetBlockByNumber(num.Uint64())
	if block == nil {
		return nil, fmt.Errorf("block not found: %s", num)
	}
	msg := types.ModifyInfo{
		BlockNum:     num,
		RedactReason: temp.RedactReason,
		Index:        temp.Index,
		ModifyType:   temp.ModifyType,
		DataAfter:    []byte(temp.DataAfter),
	}
	bodyHashes, txID, err := s.redactBodyHashes(block, &msg, temp.Meta)
	if err != nil {
		return nil, err
	}
	params, err := contractService.GetSystemParams()
	if err != nil {
		return nil, err
	}
	if params.Epoch == nil || !params.Epoch.IsUint64() {
		return nil, errors.New("invalid current epoch")
	}
	var reqID [32]byte
	if strings.TrimSpace(temp.ReqID) != "" {
		reqID, err = hexStringToBytes32(temp.ReqID)
		if err != nil {
			return nil, fmt.Errorf("invalid ReqID: %v", err)
		}
	} else {
		seed := append([]byte{}, txID[:]...)
		seed = append(seed, bodyHashes[1][:]...)
		seed = append(seed, bodyHashes[2][:]...)
		seed = append(seed, []byte(fmt.Sprintf("%d:%d:%d", num.Uint64(), temp.Index, time.Now().UnixNano()))...)
		reqID = crypto.Keccak256Hash(seed)
	}
	bodyHashStrings := make([]string, 0, len(bodyHashes))
	for _, h := range bodyHashes {
		bodyHashStrings = append(bodyHashStrings, bytes32ToHexString(h))
	}
	req := contractService.RedactReqFields{
		Version:    1,
		Epoch:      params.Epoch.Uint64(),
		ReqID:      reqID,
		BodyHashes: bodyHashes,
	}
	result := map[string]interface{}{
		"RequestID":  bytes32ToHexString(reqID),
		"Version":    req.Version,
		"Epoch":      fmt.Sprintf("%d", req.Epoch),
		"ReqID":      bytes32ToHexString(reqID),
		"BodyHashes": bodyHashStrings,
		"Req": map[string]interface{}{
			"version":     req.Version,
			"epoch":       fmt.Sprintf("%d", req.Epoch),
			"req_id":      bytes32ToHexString(reqID),
			"body_hashes": bodyHashStrings,
		},
		"TxID":                   bytes32ToHexString(txID),
		"CommitteePubKeyVersion": params.CommitteeVersion,
		"PkCHHash":               bytes32ToHexString(params.PkCHHash),
	}
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "eth_build_redact_request", "req_id", bytes32ToHexString(reqID), "block_number", num.Uint64(), "tx_index", temp.Index, "duration_ns", time.Since(start).Nanoseconds())
	return result, nil
}

func (s *Ethereum) SendTEERedactionMaterial(jsonStr string) (map[string]interface{}, error) {
	start := time.Now()
	material, bodyHashStrings, err := s.buildTEERedactionMaterial(jsonStr)
	if err != nil {
		return nil, err
	}
	status := "broadcasted"
	if host, ok := s.protocolManager.teeHost.(interface {
		RegisterRedactionMaterial(context.Context, teehost.RedactionMaterial, bool) error
	}); ok && host != nil {
		if err := host.RegisterRedactionMaterial(context.Background(), material, true); err != nil {
			return nil, err
		}
		status = "registered_and_broadcasted"
	} else if err := s.protocolManager.BroadcastRedactMaterial(context.Background(), material); err != nil {
		return nil, err
	}
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "eth_send_tee_redaction_material", "req_id", bytes32ToHexString(material.ReqID), "duration_ns", time.Since(start).Nanoseconds())
	return map[string]interface{}{
		"Status":     status,
		"ReqID":      bytes32ToHexString(material.ReqID),
		"BlockNum":   material.BlockNum.String(),
		"Index":      material.Index,
		"ModifyType": material.ModifyType,
		"BodyHashes": bodyHashStrings,
	}, nil
}

func (s *Ethereum) buildTEERedactionMaterial(jsonStr string) (material teehost.RedactionMaterial, bodyHashStrings []string, err error) {
	var temp struct {
		BlockNum               string `json:"BlockNum"`
		Index                  uint64 `json:"Index"`
		ModifyType             uint64 `json:"ModifyType"`
		DataAfter              string `json:"DataAfter"`
		RedactReason           string `json:"RedactReason"`
		Meta                   string `json:"Meta"`
		ReqID                  string `json:"ReqID"`
		DReq                   string `json:"DReq"`
		DPolicy                string `json:"DPolicy"`
		DReqInput              string `json:"DReqInput"`
		CertReqAuthMsg         string `json:"CertReqAuthMsg"`
		CertReqAuthSig         string `json:"CertReqAuthSig"`
		CommitteePubKeyVersion string `json:"CommitteePubKeyVersion"`
		ExpiresAt              string `json:"ExpiresAt"`
		ActiveTEEList          string `json:"ActiveTEEList"`
		ActiveSetCertInput     string `json:"ActiveSetCertInput"`
		ActiveTEECount         uint32 `json:"ActiveTEECount"`
		ActiveSetCertLength    uint32 `json:"ActiveSetCertLength"`
		Threshold              int    `json:"Threshold"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid TEE redaction material json: %v", err)
	}
	if temp.ModifyType != 1 {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("TEE automatic redaction currently requires ModifyType 1")
	}
	num, err := parseBigIntFlexible(temp.BlockNum)
	if err != nil || num.Sign() < 0 {
		return teehost.RedactionMaterial{}, nil, errors.New("invalid BlockNum")
	}
	block := s.blockchain.GetBlockByNumber(num.Uint64())
	if block == nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("block not found: %s", num)
	}
	msg := types.ModifyInfo{
		BlockNum:     num,
		RedactReason: temp.RedactReason,
		Index:        temp.Index,
		ModifyType:   temp.ModifyType,
		DataAfter:    []byte(temp.DataAfter),
	}
	bodyHashes, txID, source, target, originalR, err := s.redactionMaterialMessages(block, &msg, temp.Meta)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, err
	}
	params, err := contractService.GetSystemParams()
	if err != nil {
		return teehost.RedactionMaterial{}, nil, err
	}
	if params.Epoch == nil || !params.Epoch.IsUint64() {
		return teehost.RedactionMaterial{}, nil, errors.New("invalid current epoch")
	}
	activeSet, err := contractService.GetActiveSetInfo(params.Epoch)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, err
	}
	var reqID [32]byte
	if strings.TrimSpace(temp.ReqID) != "" {
		reqID, err = hexStringToBytes32(temp.ReqID)
		if err != nil {
			return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid ReqID: %v", err)
		}
	} else {
		seed := append([]byte{}, txID[:]...)
		seed = append(seed, bodyHashes[1][:]...)
		seed = append(seed, bodyHashes[2][:]...)
		seed = append(seed, []byte(fmt.Sprintf("%d:%d:%d", num.Uint64(), temp.Index, time.Now().UnixNano()))...)
		reqID = crypto.Keccak256Hash(seed)
	}
	materialSpan := s.beginEditTiming("material_load", "join_key", bytes32ToHexString(reqID), "block", num.Uint64(), "tx_index", temp.Index)
	defer func() { materialSpan(err) }()
	dReq, err := optionalBytes32(temp.DReq, "DReq")
	if err != nil {
		return teehost.RedactionMaterial{}, nil, err
	}
	dPolicy, err := optionalBytes32(temp.DPolicy, "DPolicy")
	if err != nil {
		return teehost.RedactionMaterial{}, nil, err
	}
	dReqInput, err := decodeOptionalHexOrText(temp.DReqInput)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid DReqInput: %v", err)
	}
	certReqAuthMsg, err := decodeOptionalHexOrText(temp.CertReqAuthMsg)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid CertReqAuthMsg: %v", err)
	}
	certReqAuthSig, err := decodeOptionalHexOrText(temp.CertReqAuthSig)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid CertReqAuthSig: %v", err)
	}
	committeeVersion, err := decodeOptionalHexOrText(temp.CommitteePubKeyVersion)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid CommitteePubKeyVersion: %v", err)
	}
	if len(committeeVersion) == 0 && params.CommitteeVersion != nil {
		committeeVersion = params.CommitteeVersion.Bytes()
	}
	expiresAt, err := decodeOptionalHexOrText(temp.ExpiresAt)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid ExpiresAt: %v", err)
	}
	activeTEEList, err := decodeOptionalHexOrText(temp.ActiveTEEList)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid ActiveTEEList: %v", err)
	}
	if len(activeTEEList) == 0 {
		activeTEEList = encodeHardwareIDs(activeSet.HardwareIDs)
	}
	activeSetCertInput, err := decodeOptionalHexOrText(temp.ActiveSetCertInput)
	if err != nil {
		return teehost.RedactionMaterial{}, nil, fmt.Errorf("invalid ActiveSetCertInput: %v", err)
	}
	activeTEECount := temp.ActiveTEECount
	if activeTEECount == 0 {
		activeTEECount = uint32(len(activeSet.HardwareIDs))
	}
	activeSetCertLength := temp.ActiveSetCertLength
	if activeSetCertLength == 0 {
		activeSetCertLength = uint32(len(activeSetCertInput))
	}
	threshold := temp.Threshold
	if threshold == 0 && params.ActiveThreshold != nil {
		threshold = int(params.ActiveThreshold.Int64())
	}
	if threshold == 0 {
		threshold = len(activeSet.HardwareIDs)
	}
	sigPubKeys := make([][32]byte, 0, len(activeSet.HardwareIDs))
	for _, sigPubKey := range activeSet.HardwareIDs {
		teeInfo, err := contractService.GetTEEInfo(sigPubKey)
		if err != nil {
			return teehost.RedactionMaterial{}, nil, err
		}
		sigPubKeys = append(sigPubKeys, teeInfo.SigPubKey)
	}
	material = teehost.RedactionMaterial{
		ReqID:                  reqID,
		Epoch:                  new(big.Int).Set(params.Epoch),
		BodyHashes:             bodyHashes,
		BlockNum:               new(big.Int).Set(num),
		Index:                  temp.Index,
		ModifyType:             temp.ModifyType,
		DataAfter:              []byte(temp.DataAfter),
		Meta:                   []byte(temp.Meta),
		SourceMessage:          source,
		TargetMessage:          target,
		OriginalR:              originalR,
		DReqInput:              dReqInput,
		CertReqAuthMsg:         certReqAuthMsg,
		CertReqAuthSig:         certReqAuthSig,
		DReq:                   dReq,
		DPolicy:                dPolicy,
		CommitteePubKeyVersion: committeeVersion,
		ExpiresAt:              expiresAt,
		ActiveSetHash:          activeSet.ActiveSetHash,
		ActiveTEEList:          activeTEEList,
		ActiveHardwareIDs:      activeSet.HardwareIDs,
		ActiveSigPubKeys:       sigPubKeys,
		ActiveSetCertInput:     activeSetCertInput,
		ActiveTEECount:         activeTEECount,
		ActiveSetCertLength:    activeSetCertLength,
		Threshold:              threshold,
	}
	bodyHashStrings = make([]string, 0, len(bodyHashes))
	for _, h := range bodyHashes {
		bodyHashStrings = append(bodyHashStrings, bytes32ToHexString(h))
	}
	return material, bodyHashStrings, nil
}

func (s *Ethereum) ExecuteRedaction(jsonStr string) (string, error) {
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	var temp struct {
		ReqID       string                  `json:"ReqID"`
		RPrimeHash  string                  `json:"RPrimeHash"`
		ProofHash   string                  `json:"ProofHash"`
		ResultHash  string                  `json:"ResultHash"`
		PiReqA      string                  `json:"PiReqA"`
		ShareProofs []redactShareProofInput `json:"ShareProofs"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return "", fmt.Errorf("invalid execute redaction json: %v", err)
	}
	reqID, err := hexStringToBytes32(temp.ReqID)
	if err != nil {
		return "", fmt.Errorf("invalid ReqID: %v", err)
	}
	rPrimeHash, err := hexStringToBytes32(temp.RPrimeHash)
	if err != nil {
		return "", fmt.Errorf("invalid RPrimeHash: %v", err)
	}
	resultHash, err := hexStringToBytes32(temp.ResultHash)
	if err != nil {
		return "", fmt.Errorf("invalid ResultHash: %v", err)
	}
	piReqA, err := resolvePiReqA(temp.PiReqA, temp.ShareProofs)
	if err != nil {
		return "", fmt.Errorf("invalid PiReqA: %v", err)
	}
	var proofHash [32]byte
	if strings.TrimSpace(temp.ProofHash) != "" {
		proofHash, err = hexStringToBytes32(temp.ProofHash)
		if err != nil {
			return "", fmt.Errorf("invalid ProofHash: %v", err)
		}
	} else {
		proofHash, err = computeRedactionProofHashFromChain(reqID, piReqA)
		if err != nil {
			return "", err
		}
	}
	return contractService.ExecuteRedaction(reqID, rPrimeHash, proofHash, resultHash, piReqA)
}

func (s *Ethereum) ExecuteLocalRedaction(jsonStr string) (map[string]interface{}, error) {
	return nil, errors.New("eth_executeLocalRedaction is unsupported: local single-key chameleon redaction has been removed; use eth_executeTEEClusterRedaction with a threshold-generated RPrime")
}

func (s *Ethereum) ExecuteTEEClusterRedaction(jsonStr string) (result map[string]interface{}, err error) {
	start := time.Now()
	validatorMutex.Lock()
	defer validatorMutex.Unlock()
	entrySpan := s.beginEditTiming("entry_validation", "node", s.etherbase.Hex())
	defer func() { entrySpan(err) }()

	var temp struct {
		ReqID       string                  `json:"ReqID"`
		BlockNum    string                  `json:"BlockNum"`
		Index       uint64                  `json:"Index"`
		ModifyType  uint64                  `json:"ModifyType"`
		DataAfter   string                  `json:"DataAfter"`
		RPrime      string                  `json:"RPrime"`
		NewR        string                  `json:"NewR"`
		PiReqA      string                  `json:"PiReqA"`
		ShareProofs []redactShareProofInput `json:"ShareProofs"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return nil, fmt.Errorf("invalid tee cluster redaction json: %v", err)
	}
	reqID, err := hexStringToBytes32(temp.ReqID)
	if err != nil {
		return nil, fmt.Errorf("invalid ReqID: %v", err)
	}
	num, err := parseBigIntFlexible(temp.BlockNum)
	if err != nil || num.Sign() < 0 {
		return nil, errors.New("invalid BlockNum")
	}
	piReqA, err := resolvePiReqA(temp.PiReqA, temp.ShareProofs)
	if err != nil {
		return nil, fmt.Errorf("invalid PiReqA: %v", err)
	}
	rPrimeInput := strings.TrimSpace(temp.RPrime)
	if rPrimeInput == "" {
		rPrimeInput = strings.TrimSpace(temp.NewR)
	}
	rPrime, err := parseBigIntFlexible(rPrimeInput)
	if err != nil || rPrime.Sign() == 0 {
		return nil, errors.New("invalid RPrime")
	}
	result, err = s.executeRedactionEditFlow(reqID, num, temp.Index, temp.ModifyType, temp.DataAfter, piReqA, rPrime)
	if err == nil {
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "eth_execute_tee_cluster_redaction", "req_id", bytes32ToHexString(reqID), "duration_ns", time.Since(start).Nanoseconds())
	}
	return result, err
}

func (s *Ethereum) ExecuteTEEClusterRedactionFromMaterial(ctx context.Context, req teehost.RedactionExecutionRequest) (map[string]interface{}, error) {
	_ = ctx
	if req.BlockNum == nil {
		return nil, errors.New("BlockNum is required")
	}
	if len(req.PiReqA) == 0 {
		return nil, errors.New("PiReqA is required")
	}
	if len(req.RPrime) == 0 {
		return nil, errors.New("RPrime is required")
	}
	payload := map[string]interface{}{
		"ReqID":      bytes32ToHexString(req.ReqID),
		"BlockNum":   req.BlockNum.String(),
		"Index":      req.Index,
		"ModifyType": req.ModifyType,
		"DataAfter":  string(req.DataAfter),
		"RPrime":     new(big.Int).SetBytes(req.RPrime).String(),
		"PiReqA":     bytesToHexString(req.PiReqA),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return s.ExecuteTEEClusterRedaction(string(raw))
}

func (s *Ethereum) executeRedactionEditFlow(reqID [32]byte, num *big.Int, index uint64, modifyType uint64, dataAfter string, piReqA []byte, externalRPrime *big.Int) (result map[string]interface{}, err error) {
	flowStart := time.Now()
	flowSpan := s.beginEditTiming("edit_flow", "join_key", bytes32ToHexString(reqID), "node", s.etherbase.Hex(), "block", num.Uint64(), "tx_index", index)
	defer func() { flowSpan(err) }()
	validationSpan := s.beginEditTiming("validation", "join_key", bytes32ToHexString(reqID), "block", num.Uint64(), "tx_index", index)
	if err := validatePayloadOnlyModify(&types.ModifyInfo{ModifyType: modifyType}); err != nil {
		validationSpan(err)
		return nil, err
	}
	validationSpan(nil)
	if externalRPrime == nil {
		return nil, errors.New("transaction redaction requires threshold-generated RPrime")
	}
	block := s.blockchain.GetBlockByNumber(num.Uint64())
	if block == nil {
		return nil, fmt.Errorf("block not found: %s", num)
	}
	msg := types.ModifyInfo{
		BlockNum:   num,
		Index:      index,
		ModifyType: modifyType,
		DataAfter:  []byte(dataAfter),
	}
	buildSpan := s.beginEditTiming("block_load_build", "join_key", bytes32ToHexString(reqID), "block", num.Uint64(), "tx_index", index)
	buildStart := time.Now()
	newBlock, err := s.protocolManager.getNewBlock(block, &msg, externalRPrime, "req_id", bytes32ToHexString(reqID))
	if err != nil {
		buildSpan(err)
		return nil, err
	}
	buildSpan(nil)
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_build_new_block", "req_id", bytes32ToHexString(reqID), "block_number", num.Uint64(), "tx_index", index, "duration_ns", time.Since(buildStart).Nanoseconds())
	// [perf 2026-09-05] 移除冗余的 validateRedactedBlock 校验。
	// getNewBlock 内部已直接校验变色龙哈希碰撞等值，此处对自身构建的本地块
	// 重复调用 validateRedactedBlock 属纯冗余计算（消除 ~3.8ms 延迟）。
	rPrimeSpan := s.beginEditTiming("rprime_validation", "join_key", bytes32ToHexString(reqID), "block", num.Uint64(), "tx_index", index)
	rPrimeSpan(nil)
	newR, err := redactionRPrime(newBlock, index, modifyType)
	if err != nil {
		return nil, err
	}
	rPrimeHash := crypto.Keccak256Hash(newR.Bytes())

	proposal := types.ProposalTx{
		Proposer: s.etherbase,
		R:        newR,
		Msg:      msg,
	}
	modifyMsg := types.ModifyMsg{
		ProposalTx: proposal,
		NewR:       newR,
		NewBlock:   newBlock,
		ReqID:      reqID,
		PiReqA:     piReqA,
	}
	s.protocolManager.markTxModifyApplied(txModifyMsgKey(modifyMsg))
	broadcastStart := time.Now()
	s.protocolManager.BroadcastModifyMsg(modifyMsg)
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_broadcast_modify_msg", "req_id", bytes32ToHexString(reqID), "duration_ns", time.Since(broadcastStart).Nanoseconds())

	if err := s.protocolManager.updateBlockInfo(block, newBlock); err != nil {
		return nil, err
	}

	proofSpan := s.beginEditTiming("proof_hash", "join_key", bytes32ToHexString(reqID), "block", num.Uint64(), "tx_index", index)
	proofHash, err := computeRedactionProofHashFromChain(reqID, piReqA)
	if err != nil {
		proofSpan(err)
		return nil, err
	}
	proofSpan(nil)
	modifyMsg.ProofHash = proofHash
	resultHash := crypto.Keccak256Hash(newBlock.Hash().Bytes())
	submitStart := time.Now()
	txHash, err := contractService.ExecuteRedaction(reqID, rPrimeHash, proofHash, resultHash, piReqA)
	if err != nil {
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "execute_redaction_submit_failed", "join_key", common.Bytes2Hex(reqID[:]), "tee_host", s.etherbase.Hex(), "node_id", s.protocolManager.port, "reason", "submit_error", "error", err)
		log.Warn("executeRedaction submit failed; checking whether request was already executed", "req_id", bytes32ToHexString(reqID), "err", err)
		waitStart := time.Now()
		if waitErr := waitRedactionExecuted(reqID, 2*time.Minute); waitErr != nil {
			return nil, fmt.Errorf("executeRedaction failed: %v; request not observed executed: %w", err, waitErr)
		}
		txHash = "already-executed"
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_gsc_executed_visible", "req_id", bytes32ToHexString(reqID), "tx", txHash, "duration_ns", time.Since(waitStart).Nanoseconds(), "status", "observed_after_submit_error")
	} else {
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_submit_execute_onchain", "req_id", bytes32ToHexString(reqID), "tx", txHash, "duration_ns", time.Since(submitStart).Nanoseconds())
		receiptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		receipt, receiptErr := contractService.WaitTransactionReceipt(receiptCtx, common.HexToHash(txHash))
		cancel()
		if receiptErr != nil {
			log.Info("E2E_TIMING", "stage", "edit_execution", "event", "execute_redaction_receipt_failed", "join_key", common.Bytes2Hex(reqID[:]), "tx_hash", txHash, "tee_host", s.etherbase.Hex(), "node_id", s.protocolManager.port, "reason", "receipt_wait_error", "error", receiptErr)
			return nil, receiptErr
		}
		receiptBlock := uint64(0)
		if receipt.BlockNumber != nil {
			receiptBlock = receipt.BlockNumber.Uint64()
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			log.Info("E2E_TIMING", "stage", "edit_execution", "event", "execute_redaction_receipt_failed", "join_key", common.Bytes2Hex(reqID[:]), "tx_hash", txHash, "tee_host", s.etherbase.Hex(), "node_id", s.protocolManager.port, "receipt_block_number", receiptBlock, "status", receipt.Status, "reason", "status_0")
			return nil, fmt.Errorf("execute redaction transaction %s failed in block %d", txHash, receiptBlock)
		}
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "execute_redaction_receipt_success", "join_key", common.Bytes2Hex(reqID[:]), "tx_hash", txHash, "tee_host", s.etherbase.Hex(), "node_id", s.protocolManager.port, "receipt_block_number", receiptBlock, "status", receipt.Status)
		waitStart := time.Now()
		if err := waitRedactionExecuted(reqID, 2*time.Minute); err != nil {
			return nil, err
		}
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_gsc_executed_visible", "req_id", bytes32ToHexString(reqID), "tx", txHash, "duration_ns", time.Since(waitStart).Nanoseconds())
	}

	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_edit_flow_total", "req_id", bytes32ToHexString(reqID), "tx", txHash, "duration_ns", time.Since(flowStart).Nanoseconds())
	return map[string]interface{}{
		"Status":     "executed",
		"ReqID":      bytes32ToHexString(reqID),
		"TxHash":     txHash,
		"NewR":       newR.String(),
		"RPrimeHash": bytes32ToHexString(rPrimeHash),
		"ProofHash":  bytes32ToHexString(proofHash),
		"ResultHash": bytes32ToHexString(resultHash),
		"PiReqA":     bytesToHexString(piReqA),
	}, nil
}

func (s *Ethereum) GetRedactRequest(reqIDHex string) (map[string]interface{}, error) {
	reqID, err := hexStringToBytes32(reqIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ReqID: %v", err)
	}
	record, err := contractService.GetRedactRequest(reqID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"StoredReqID":      bytes32ToHexString(record.StoredReqID),
		"Epoch":            record.Epoch,
		"DReq":             bytes32ToHexString(record.DReq),
		"DPolicy":          bytes32ToHexString(record.DPolicy),
		"Status":           record.Status,
		"AuthorizedAt":     record.AuthorizedAt,
		"ExecutedAt":       record.ExecutedAt,
		"ExpiresAt":        record.ExpiresAt,
		"RejectReasonHash": bytes32ToHexString(record.RejectReasonHash),
	}, nil
}

func (s *Ethereum) IsRedactionExecuted(reqIDHex string) (bool, error) {
	reqID, err := hexStringToBytes32(reqIDHex)
	if err != nil {
		return false, fmt.Errorf("invalid ReqID: %v", err)
	}
	return contractService.IsRedactionExecuted(reqID)
}

func (s *Ethereum) RequestTxModify(reqIDHex string, blockNumStr string, index uint64) (map[string]interface{}, error) {
	reqID, err := hexStringToBytes32(reqIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ReqID: %v", err)
	}
	blockNum, err := parseBigIntFlexible(blockNumStr)
	if err != nil {
		return nil, fmt.Errorf("invalid block number: %v", err)
	}
	return s.protocolManager.requestTxModify(reqID, blockNum, index)
}

func (s *Ethereum) GetSystemParams() (map[string]interface{}, error) {
	params, err := contractService.GetSystemParams()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"CommitteeVersion":      params.CommitteeVersion,
		"PkCHHash":              bytes32ToHexString(params.PkCHHash),
		"CurrentPkCHVersion":    params.CurrentPkCHVersion,
		"ConfiguredEpochLength": params.ConfiguredEpochLength,
		"Epoch":                 params.Epoch,
		"ActiveThreshold":       params.ActiveThreshold,
		"ActiveLimit":           params.ActiveLimit,
	}, nil
}

func (s *Ethereum) GetPkCHHash() (string, error) {
	hash, err := contractService.GetPkCHHash()
	if err != nil {
		return "", err
	}
	return bytes32ToHexString(hash), nil
}

func (s *Ethereum) SetPkCH(pkCH string, signature string) (string, error) {
	start := time.Now()
	raw, err := decodeHexOrText(pkCH)
	if err != nil {
		return "", fmt.Errorf("invalid pkCH: %v", err)
	}
	sigBytes, err := unmarshalHexSignatureBytes(signature)
	if err != nil {
		return "", fmt.Errorf("invalid pkCH signature: %v", err)
	}
	txHash, err := contractService.SetPkCH(raw, sigBytes)
	if err == nil {
		log.Info("E2E_TIMING", "stage", "system_init", "event", "eth_setPkCH_rpc", "tx", txHash, "duration_ns", time.Since(start).Nanoseconds())
	}
	return txHash, err
}

func (s *Ethereum) GetTEEIdentity() (map[string]interface{}, error) {
	hardwareID, committeePubKey, sigPubKey, vrfPubKey, enclavePK, err := sgx.GetIdentity()
	if err != nil {
		return nil, err
	}
	if len(hardwareID) != 32 {
		return nil, fmt.Errorf("invalid hardware id length: got %d, want 32", len(hardwareID))
	}
	if len(committeePubKey) != 128 {
		return nil, fmt.Errorf("invalid committee public key length: got %d, want 128", len(committeePubKey))
	}
	if len(sigPubKey) != 32 {
		return nil, fmt.Errorf("invalid signature public key length: got %d, want 32", len(sigPubKey))
	}
	if len(vrfPubKey) != 32 {
		return nil, fmt.Errorf("invalid vrf public key length: got %d, want 32", len(vrfPubKey))
	}
	if err := validateEnclavePK(enclavePK); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"HardwareID":      bytesToHexString(hardwareID),
		"CommitteePubKey": bytesToHexString(committeePubKey),
		"SigPubKey":       bytesToHexString(sigPubKey),
		"VRFPubKey":       bytesToHexString(vrfPubKey),
		"EnclavePK":       bytesToHexString(enclavePK),
	}, nil
}

func (s *Ethereum) GetTEEQuote(committeePubKeyInput string) (map[string]interface{}, error) {
	start := time.Now()
	committeePubKey, err := parseCommitteePubKeyBytes(committeePubKeyInput)
	if err != nil {
		return nil, fmt.Errorf("invalid committee public key: %w", err)
	}
	if len(committeePubKey) != 128 {
		return nil, fmt.Errorf("invalid committee public key length: got %d, want 128", len(committeePubKey))
	}

	validatorMutex.Lock()
	defer validatorMutex.Unlock()
	if err := sgx.SetCommitteePubKey(committeePubKey); err != nil {
		return nil, fmt.Errorf("set committee public key in enclave: %w", err)
	}
	quote, hardwareID, enclaveCommitteePubKey, sigPubKey, vrfPubKey, enclavePK, err := sgx.GenerateQuote()
	if err != nil {
		return nil, err
	}
	if len(quote) == 0 {
		return nil, errors.New("empty SGX quote")
	}
	if len(hardwareID) != 32 {
		return nil, fmt.Errorf("invalid hardware id length: got %d, want 32", len(hardwareID))
	}
	if len(enclaveCommitteePubKey) != 128 {
		return nil, fmt.Errorf("invalid enclave committee public key length: got %d, want 128", len(enclaveCommitteePubKey))
	}
	if !bytes.Equal(enclaveCommitteePubKey, committeePubKey) {
		return nil, errors.New("enclave committee public key does not match requested key")
	}
	if len(sigPubKey) != 32 {
		return nil, fmt.Errorf("invalid signature public key length: got %d, want 32", len(sigPubKey))
	}
	if len(vrfPubKey) != 32 {
		return nil, fmt.Errorf("invalid vrf public key length: got %d, want 32", len(vrfPubKey))
	}
	if err := validateEnclavePK(enclavePK); err != nil {
		return nil, err
	}
	log.Info("E2E_TIMING", "stage", "system_init", "event", "eth_get_tee_quote_rpc", "quote_bytes", len(quote), "duration_ns", time.Since(start).Nanoseconds())
	return map[string]interface{}{
		"Quote":           bytesToHexString(quote),
		"HardwareID":      bytesToHexString(hardwareID),
		"CommitteePubKey": bytesToHexString(enclaveCommitteePubKey),
		"SigPubKey":       bytesToHexString(sigPubKey),
		"VRFPubKey":       bytesToHexString(vrfPubKey),
		"EnclavePK":       bytesToHexString(enclavePK),
	}, nil
}

func (s *Ethereum) RegisterCommitteePubKey(jsonStr string) (string, error) {
	start := time.Now()
	coords, err := parseCommitteePubKeyInput(jsonStr)
	if err != nil {
		log.Error("RegisterCommitteePubKey parse failed", "err", err, "payload", jsonStr)
		return "", err
	}

	log.Info("RegisterCommitteePubKey parsed", "p0", coords[0], "p1", coords[1], "p2", coords[2], "p3", coords[3])
	txHash, err := contractService.RegisterCommitteePubKey(coords)
	if err != nil {
		log.Error("RegisterCommitteePubKey contract call failed", "err", err)
		return "", err
	}
	log.Info("RegisterCommitteePubKey success", "tx", txHash)
	log.Info("E2E_TIMING", "stage", "system_init", "event", "eth_register_committee_pubkey_rpc", "duration_ns", time.Since(start).Nanoseconds())
	return txHash, nil
}

func (s *Ethereum) SetTrustedMREnclave(mrEnclave string, trusted bool) (string, error) {
	start := time.Now()
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	mr, err := hexStringToBytes32(mrEnclave)
	if err != nil {
		return "", fmt.Errorf("invalid MR_ENCLAVE: %v", err)
	}
	txHash, err := contractService.SetTrustedMREnclave(mr, trusted)
	if err == nil {
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "eth_setTrustedMREnclave_rpc", "tx", txHash, "trusted", trusted, "duration_ns", time.Since(start).Nanoseconds())
	}
	return txHash, err
}

//
//func (e *Ethereum) IsValidator(address common.Address) bool {
//	return contractService.is
//}

// ProposeModification 提交交易修改申请
func (s *Ethereum) ProposeModification(jsonStr string) (string, error) {
	return "", errors.New("eth_proposeModification is unsupported: legacy single-key redaction flow has been removed; use submitRedactAuthorization plus executeTEEClusterRedaction")
}
func deduplicate(validators, addresses []common.Address) []common.Address {
	// 使用 map 来跟踪已经出现的元素
	seen := make(map[common.Address]bool)
	result := make([]common.Address, 0)

	for _, v := range addresses {
		if !seen[v] {
			seen[v] = true
		}
	}

	for _, v := range validators {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}

	return result
}

func (s *Ethereum) SubmitEraseProof(sigPubKeyHex string, epoch *big.Int, eraseProofHash string, signature string) (string, error) {
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	sigPubKey, err := hexStringToBytes32(sigPubKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid sigPubKey")
	}
	if epoch == nil || epoch.Sign() < 0 {
		return "", errors.New("invalid epoch")
	}
	proofHash, err := hexStringToBytes32(eraseProofHash)
	if err != nil {
		return "", fmt.Errorf("invalid eraseProofHash: %v", err)
	}
	sigBytes, err := unmarshalHexSignatureBytes(signature)
	if err != nil {
		return "", fmt.Errorf("invalid erase proof signature: %v", err)
	}
	return contractService.SubmitEraseProof(sigPubKey, epoch, proofHash, sigBytes)
}

func (s *Ethereum) SubmitTEESelectionVRF(jsonStr string) (string, error) {
	start := time.Now()
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	var temp struct {
		Epoch      string `json:"Epoch"`
		HardwareId string `json:"HardwareId"`
		SigPubKey  string `json:"SigPubKey"`
		VrfResult  string `json:"VrfResult"`
		VrfProof   string `json:"VrfProof"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		log.Error("解析出错:", "err", err, "str", jsonStr)
		return "", err
	}

	epoch, err := parseBigIntFlexible(temp.Epoch)
	if err != nil {
		return "", fmt.Errorf("invalid Epoch: %v", err)
	}
	if epoch.Sign() <= 0 {
		return "", errors.New("Epoch must be positive")
	}
	teeKeyInput := temp.SigPubKey
	if strings.TrimSpace(teeKeyInput) == "" {
		log.Warn("eth_submitTEESelectionVRF using legacy HardwareId input as sigPubKey")
		teeKeyInput = temp.HardwareId
	}
	sigPubKey, err := hexStringToBytes32(teeKeyInput)
	if err != nil {
		return "", fmt.Errorf("invalid SigPubKey: %v", err)
	}
	vrfResult, err := hexStringToBytes32(temp.VrfResult)
	if err != nil {
		return "", fmt.Errorf("invalid VrfResult: %v", err)
	}
	vrfProof, err := unmarshalHexSignatureBytes(temp.VrfProof)
	if err != nil {
		return "", fmt.Errorf("invalid VrfProof: %v", err)
	}

	txHash, err := contractService.SubmitTEESelectionVRF(epoch, sigPubKey, vrfResult, vrfProof)
	if err == nil {
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "eth_submitTEESelectionVRF_rpc", "tx", txHash, "epoch", epoch.String(), "sig_pub_key", teeKeyInput, "duration_ns", time.Since(start).Nanoseconds())
	}
	return txHash, err
}

func (s *Ethereum) RevokeTEE(sigPubKeyHex string, reasonHash string, slashAmount *big.Int, signature string) (string, error) {
	validatorMutex.Lock()
	defer validatorMutex.Unlock()

	sigPubKey, err := hexStringToBytes32(sigPubKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid sigPubKey")
	}
	reason, err := hexStringToBytes32(reasonHash)
	if err != nil {
		return "", fmt.Errorf("invalid reasonHash: %v", err)
	}
	if slashAmount == nil || slashAmount.Sign() < 0 {
		return "", errors.New("invalid slash amount")
	}
	sigCoords, err := unmarshalHexSignatureToG1Coords(signature)
	if err != nil {
		return "", fmt.Errorf("invalid signature: %v", err)
	}
	return contractService.RevokeTEE(sigPubKey, reason, slashAmount, sigCoords)
}

// GetTEENum 获取 TEE 数量
func (s *Ethereum) GetTEENum() (*big.Int, error) {
	return contractService.GetTEENum()
}

// GetTEEInfo 获取 TEE 信息。参数是 sigPubKey；旧名称仅为 RPC 兼容。
func (s *Ethereum) GetTEEInfo(sigPubKey [32]byte) (map[string]interface{}, error) {
	teeInfo, err := contractService.GetTEEInfo(sigPubKey)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"SigPubKey": teeInfo.SigPubKey,
		"MRENCLAVE": teeInfo.MRENCLAVE,
		"EnclavePK": bytesToHexString(teeInfo.EnclavePK),
		"Owner":     teeInfo.Owner,
		"Deposit":   teeInfo.Deposit,
		"IsActive":  teeInfo.IsActive,
	}

	return result, nil
}

func (s *Ethereum) GetPendingTEE(ownerAddress string) (map[string]interface{}, error) {
	if !common.IsHexAddress(ownerAddress) {
		return nil, errors.New("invalid owner address")
	}

	pending, err := contractService.GetPendingTEE(common.HexToAddress(ownerAddress))
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"Exists":        pending.Exists,
		"Deposit":       pending.Deposit,
		"PendingNonce":  pending.Nonce,
		"CreatedAt":     pending.CreatedAt,
		"ChallengeHash": bytes32ToHexString(pending.ChallengeHash),
	}, nil
}

func (s *Ethereum) GetRegistrationChallenge(ownerAddress string) (string, error) {
	if !common.IsHexAddress(ownerAddress) {
		return "", errors.New("invalid owner address")
	}
	challengeHash, err := contractService.GetRegistrationChallenge(common.HexToAddress(ownerAddress))
	if err != nil {
		return "", err
	}
	return "0x" + common.Bytes2Hex(challengeHash[:]), nil
}

func (s *Ethereum) GetTEEStatusByOwner(ownerAddress string) (map[string]interface{}, error) {
	if !common.IsHexAddress(ownerAddress) {
		return nil, errors.New("invalid owner address")
	}

	status, err := contractService.GetTEEStatusByOwner(common.HexToAddress(ownerAddress))
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"Status":     status.Status,
		"HardwareId": bytes32ToHexString(status.HardwareID),
	}, nil
}

// GetActiveTEENum 获取活跃 TEE 数量
func (s *Ethereum) GetActiveTEEs() ([]string, error) {
	activeTEEs, err := contractService.GetActiveTEEs()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, tee := range activeTEEs {
		result = append(result, bytes32ToHexString(tee))
	}
	return result, nil
}

func (s *Ethereum) GetActiveSetInfo(epochString string) (map[string]interface{}, error) {
	epochString = strings.TrimSpace(epochString)
	if epochString == "" {
		return nil, errors.New("epoch is required")
	}
	epoch, ok := new(big.Int).SetString(epochString, 10)
	if !ok || epoch.Sign() < 0 {
		return nil, errors.New("invalid epoch")
	}
	info, err := contractService.GetActiveSetInfo(epoch)
	if err != nil {
		return nil, err
	}
	activeTEEs := make([]string, 0, len(info.HardwareIDs))
	for _, tee := range info.HardwareIDs {
		activeTEEs = append(activeTEEs, bytes32ToHexString(tee))
	}
	return map[string]interface{}{
		"Epoch":                  info.Epoch,
		"ActiveSigPubKeys":       activeTEEs,
		"ActiveTEEs":             activeTEEs,
		"ActiveSetHash":          bytes32ToHexString(info.ActiveSetHash),
		"PkCHHash":               bytes32ToHexString(info.PkCHHash),
		"CommitteePubKeyVersion": info.CommitteePubKeyVersion,
		"ActiveThreshold":        info.ActiveThreshold,
		"ActiveLimit":            info.ActiveLimit,
	}, nil
}

func (s *Ethereum) GetCandidateTEEs() ([]string, error) {
	candidateTEEs, err := contractService.GetCandidateTEEs()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, tee := range candidateTEEs {
		result = append(result, bytes32ToHexString(tee))
	}
	return result, nil
}

func (s *Ethereum) redactBodyHashes(block *types.Block, msg *types.ModifyInfo, meta string) ([7][32]byte, [32]byte, error) {
	var bodyHashes [7][32]byte
	var txID [32]byte
	if block == nil {
		return bodyHashes, txID, errors.New("nil block")
	}
	if msg == nil {
		return bodyHashes, txID, errors.New("nil modify info")
	}
	if meta == "" {
		meta = msg.RedactReason
	}
	newBlock, err := s.protocolManager.getNewBlock(block, msg, nil)
	if err != nil {
		return bodyHashes, txID, err
	}
	txs := block.Transactions()
	newTxs := newBlock.Transactions()
	if int(msg.Index) >= len(txs) || int(msg.Index) >= len(newTxs) {
		return bodyHashes, txID, errors.New("tx index out of range")
	}
	txID = txs[msg.Index].Hash()
	oldBytes, err := types.EncodeTxToBytesNoChameleonR(txs[msg.Index])
	if err != nil {
		return bodyHashes, txID, err
	}
	newBytes, err := types.EncodeTxToBytesNoChameleonR(newTxs[msg.Index])
	if err != nil {
		return bodyHashes, txID, err
	}
	bodyHashes[1] = crypto.Keccak256Hash(oldBytes)
	bodyHashes[2] = crypto.Keccak256Hash(newBytes)
	bodyHashes[0] = txID
	bodyHashes[3] = crypto.Keccak256Hash([]byte(fmt.Sprintf("%d:%d:%d", block.NumberU64(), msg.Index, msg.ModifyType)))
	bodyHashes[4] = crypto.Keccak256Hash([]byte(meta))

	params, err := contractService.GetSystemParams()
	if err != nil {
		return bodyHashes, txID, err
	}
	if params.Epoch == nil {
		return bodyHashes, txID, errors.New("current epoch unavailable")
	}
	activeSet, err := contractService.GetActiveSetInfo(params.Epoch)
	if err != nil {
		return bodyHashes, txID, err
	}
	bodyHashes[5] = activeSet.ActiveSetHash
	bodyHashes[6] = params.PkCHHash
	if bodyHashes[5] == ([32]byte{}) {
		return bodyHashes, txID, errors.New("active set hash is empty")
	}
	if bodyHashes[6] == ([32]byte{}) {
		return bodyHashes, txID, errors.New("pkCH hash is empty")
	}
	return bodyHashes, txID, nil
}

func (s *Ethereum) redactionMaterialMessages(block *types.Block, msg *types.ModifyInfo, meta string) ([7][32]byte, [32]byte, []byte, []byte, []byte, error) {
	var bodyHashes [7][32]byte
	var txID [32]byte
	if block == nil {
		return bodyHashes, txID, nil, nil, nil, errors.New("nil block")
	}
	if msg == nil {
		return bodyHashes, txID, nil, nil, nil, errors.New("nil modify info")
	}
	if msg.ModifyType != 1 {
		return bodyHashes, txID, nil, nil, nil, fmt.Errorf("TEE automatic redaction currently requires ModifyType 1")
	}
	newBlock, err := s.protocolManager.getNewBlock(block, msg, nil)
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, err
	}
	txs := block.Transactions()
	newTxs := newBlock.Transactions()
	if int(msg.Index) >= len(txs) || int(msg.Index) >= len(newTxs) {
		return bodyHashes, txID, nil, nil, nil, errors.New("tx index out of range")
	}
	txOld := txs[msg.Index]
	if txOld.SubPublicKey() == nil || txOld.SubPublicKey().Sign() == 0 || txOld.ChameleonR() == nil || txOld.ChameleonR().Sign() == 0 {
		return bodyHashes, txID, nil, nil, nil, errors.New("old transaction missing chameleon hash fields")
	}
	// [2026-08-30 迁移 nitch] 进入 PartialAdapt 的 Source/TargetMessage 为
	// 32B 域分离摘要（types.TransactionHashMessage），与接收端校验、tx.Hash()
	// 使用同一消息定义；enclave 对传入字节做 hash-to-G1，换摘要无需改动 sgx 侧。
	source, err := types.TransactionHashMessage(txOld)
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, err
	}
	target, err := types.TransactionHashMessage(newTxs[msg.Index])
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, err
	}
	originalR, err := chameleon.NITCHBLSBigIntToR(txOld.ChameleonR())
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, fmt.Errorf("invalid original chameleon R: %w", err)
	}
	bodyHashes[0] = txOld.Hash()
	bodyHashes[1] = crypto.Keccak256Hash(source)
	bodyHashes[2] = crypto.Keccak256Hash(target)
	bodyHashes[3] = crypto.Keccak256Hash([]byte(fmt.Sprintf("%d:%d:%d", block.NumberU64(), msg.Index, msg.ModifyType)))
	if meta == "" {
		meta = msg.RedactReason
	}
	bodyHashes[4] = crypto.Keccak256Hash([]byte(meta))
	params, err := contractService.GetSystemParams()
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, err
	}
	if params.Epoch == nil {
		return bodyHashes, txID, nil, nil, nil, errors.New("current epoch unavailable")
	}
	activeSet, err := contractService.GetActiveSetInfo(params.Epoch)
	if err != nil {
		return bodyHashes, txID, nil, nil, nil, err
	}
	bodyHashes[5] = activeSet.ActiveSetHash
	bodyHashes[6] = params.PkCHHash
	if bodyHashes[5] == ([32]byte{}) {
		return bodyHashes, txID, nil, nil, nil, errors.New("active set hash is empty")
	}
	if bodyHashes[6] == ([32]byte{}) {
		return bodyHashes, txID, nil, nil, nil, errors.New("pkCH hash is empty")
	}
	return bodyHashes, bodyHashes[0], source, target, originalR, nil
}

func optionalBytes32(input string, name string) ([32]byte, error) {
	if strings.TrimSpace(input) == "" {
		return [32]byte{}, nil
	}
	value, err := hexStringToBytes32(input)
	if err != nil {
		return [32]byte{}, fmt.Errorf("invalid %s: %v", name, err)
	}
	return value, nil
}

func encodeHardwareIDs(ids [][32]byte) []byte {
	out := make([]byte, 0, len(ids)*32)
	for _, id := range ids {
		out = append(out, id[:]...)
	}
	return out
}

func (s *Ethereum) beginEditTiming(event string, fields ...interface{}) func(error) {
	started := time.Now()
	base := append([]interface{}{"node", s.etherbase.Hex()}, fields...)
	log.Info("E2E_TIMING", append([]interface{}{"stage", "edit_execution", "event", event + "_start"}, base...)...)
	return func(err error) {
		status, reason := "ok", "completed"
		if err != nil {
			status, reason = "error", "error"
		}
		end := append([]interface{}{}, base...)
		end = append(end, "status", status, "reason", reason, "duration_ns", time.Since(started).Nanoseconds())
		log.Info("E2E_TIMING", append([]interface{}{"stage", "edit_execution", "event", event + "_end"}, end...)...)
	}
}

func computeRedactionProofHashFromChain(reqID [32]byte, piReqA []byte) ([32]byte, error) {
	record, err := contractService.GetRedactRequest(reqID)
	if err != nil {
		return [32]byte{}, err
	}
	if record.StoredReqID == ([32]byte{}) {
		return [32]byte{}, errors.New("redact request not found")
	}
	if record.Epoch == nil {
		return [32]byte{}, errors.New("redact request epoch unavailable")
	}
	activeSet, err := contractService.GetActiveSetInfo(record.Epoch)
	if err != nil {
		return [32]byte{}, err
	}
	return contractService.ComputeRedactionProofHash(reqID, record.DReq, activeSet.ActiveSetHash, piReqA)
}

func waitRedactionExecuted(reqID [32]byte, timeout time.Duration) (err error) {
	started := time.Now()
	fields := []interface{}{"join_key", bytes32ToHexString(reqID), "wait", "redaction_executed_visibility"}
	log.Info("E2E_TIMING", append([]interface{}{"stage", "edit_execution", "event", "wait_redaction_executed_start"}, fields...)...)
	defer func() {
		status, reason := "ok", "completed"
		if err != nil {
			status, reason = "error", "timeout_or_query_error"
		}
		end := append([]interface{}{}, fields...)
		end = append(end, "status", status, "reason", reason, "duration_ns", time.Since(started).Nanoseconds())
		log.Info("E2E_TIMING", append([]interface{}{"stage", "edit_execution", "event", "wait_redaction_executed_end"}, end...)...)
	}()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		executed, err := contractService.IsRedactionExecuted(reqID)
		if err == nil && executed {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for redaction execution")
}
