package contractService

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io/ioutil"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

var (
	serviceInstance *ContractService
	initOnce        sync.Once
	initErr         error
)

// ContractService 核心服务结构体
type ContractService struct {
	contract *contracts.Contracts
	signer   *bind.TransactOpts
	txPool   *core.TxPool
	gasPrice *big.Int
	gasLimit uint64
	// 事件系统
	scope      event.SubscriptionScope
	newBlockCh chan core.ChainHeadEvent

	contractAddress common.Address

	privateKey *ecdsa.PrivateKey

	eliClient EthereumClient

	teeEventFeed event.Feed
	teeWatcherMu sync.Mutex
	teeWatcher   *TEEEventWatcher

	pkCHProvider *PkCHProvider
}

func Init(port string, contractAddress common.Address, privateKey *ecdsa.PrivateKey) error {
	initOnce.Do(func() {

		content, err := ioutil.ReadFile("privatekey.txt")
		if err != nil {
			log.Warn("Failed to read private key: ", err)
		}
		cleaned := strings.TrimSpace(string(content))
		privateKey, err = crypto.HexToECDSA(cleaned)
		if err != nil {
			log.Warn("Failed to parse private key:", err)
		}
		serviceInstance, initErr = NewContractService(port, contractAddress, privateKey)
		log.Info("Initializing Contract Service", "contractAddress", contractAddress.Hex(), "privateKey", privateKey, "instance", serviceInstance)
	})
	return initErr
}

func Instance() (*ContractService, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance, nil
}

// 创建合约实例
func NewContractService(port string, contractAddress common.Address, privateKey *ecdsa.PrivateKey) (*ContractService, error) {
	// 加载交易签名者
	url := "http://localhost:" + port
	ethClient, err := ethclient.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to dial eth client: %v", err)
	}
	return NewContractServiceWithBackend(ethClient, contractAddress, privateKey)
}

// NewContractServiceWithBackend creates a contract service over an already-open backend.
// It is primarily used by tests and in-process integrations.
func NewContractServiceWithBackend(backend EthereumClient, contractAddress common.Address, privateKey *ecdsa.PrivateKey) (*ContractService, error) {
	if backend == nil {
		return nil, fmt.Errorf("backend is nil")
	}
	if privateKey == nil {
		return nil, fmt.Errorf("private key is nil")
	}
	//// 初始化合约实例
	contract, err := contracts.NewContracts(contractAddress, backend)
	if err != nil {
		return nil, fmt.Errorf("failed to bind contract: %v", err)
	}
	log.Info("Contract Service Initialized", "contract", contract)
	var gasPrice *big.Int
	gasPrice, _ = backend.SuggestGasPrice(context.Background())
	contractService := &ContractService{
		//config:     config,
		//backend:  eth,
		contract: contract,
		//signer:     bind.NewKeyedTransactor(key),
		//txPool:     eth.TxPool(),
		contractAddress: contractAddress,
		gasPrice:        gasPrice,
		gasLimit:        30000000,
		eliClient:       backend,
		newBlockCh:      make(chan core.ChainHeadEvent, 10),
		privateKey:      privateKey,
	}
	contractService.pkCHProvider = NewPkCHProvider(contractService)

	return contractService, nil
}

func StartTEEEventWatcher(ctx context.Context, cfg TEEEventWatcherConfig) error {
	if serviceInstance == nil {
		return fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.StartTEEEventWatcher(ctx, cfg)
}

func StopTEEEventWatcher() {
	if serviceInstance != nil {
		serviceInstance.StopTEEEventWatcher()
	}
}

func SubscribeTEEEvents(ch chan<- TEEEvent) event.Subscription {
	if serviceInstance == nil {
		return event.NewSubscription(func(<-chan struct{}) error {
			return fmt.Errorf("contract service not initialized")
		})
	}
	return serviceInstance.SubscribeTEEEvents(ch)
}

func (s *ContractService) SubscribeTEEEvents(ch chan<- TEEEvent) event.Subscription {
	return s.teeEventFeed.Subscribe(ch)
}

func (s *ContractService) StartTEEEventWatcher(ctx context.Context, cfg TEEEventWatcherConfig) error {
	s.teeWatcherMu.Lock()
	defer s.teeWatcherMu.Unlock()
	if s.teeWatcher != nil {
		return fmt.Errorf("tee event watcher already started")
	}
	watcher, err := NewTEEEventWatcher(s, cfg)
	if err != nil {
		return err
	}
	s.teeWatcher = watcher
	watcher.Start(ctx)
	return nil
}

func (s *ContractService) StopTEEEventWatcher() {
	s.teeWatcherMu.Lock()
	watcher := s.teeWatcher
	s.teeWatcher = nil
	s.teeWatcherMu.Unlock()
	if watcher != nil {
		watcher.Stop()
	}
}

func (s *ContractService) ContractAddress() common.Address {
	return s.contractAddress
}

func (s *ContractService) ChainID(ctx context.Context) (*big.Int, error) {
	if chainIDClient, ok := s.eliClient.(interface {
		ChainID(context.Context) (*big.Int, error)
	}); ok {
		return chainIDClient.ChainID(ctx)
	}
	if networkIDClient, ok := s.eliClient.(interface {
		NetworkID(context.Context) (*big.Int, error)
	}); ok {
		return networkIDClient.NetworkID(ctx)
	}
	return big.NewInt(1337), nil
}

type EthereumClient interface {
	bind.ContractBackend
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	TransactionByHash(ctx context.Context, txHash common.Hash) (*types.Transaction, bool, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
}

func DefaultTEEEventWatcherConfig() TEEEventWatcherConfig {
	return TEEEventWatcherConfig{
		PollInterval:  time.Second,
		Confirmations: 1,
		BatchSize:     1000,
	}
}

func ProposeLeave() error {
	return serviceInstance.ProposeLeave()
}

func StakeTEE(value *big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.StakeTEE(value)
}

func CancelPendingTEE() (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.CancelPendingTEE()
}

func RegisterTEE(owner common.Address, pendingNonce *big.Int, mrEnclave [32]byte, hardwareID [32]byte, evidence TEERegistrationEvidence, signature [2]*big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.RegisterTEE(owner, pendingNonce, mrEnclave, hardwareID, evidence, signature)
}

func RegisterCommitteePubKey(pubKey [4]*big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	log.Info("RegisterCommitteePubKey request", "p0", pubKey[0], "p1", pubKey[1], "p2", pubKey[2], "p3", pubKey[3])
	return serviceInstance.RegisterCommitteePubKey(pubKey)
}

func SetTrustedMREnclave(mrEnclave [32]byte, trusted bool) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.SetTrustedMREnclave(mrEnclave, trusted)
}

func SubmitEraseProof(sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.SubmitEraseProof(sigPubKey, epoch, eraseProofHash, signature)
}

func SubmitTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.SubmitTEESelectionVRF(epoch, sigPubKey, vrfResult, vrfProof)
}

func RevokeTEE(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, signature [2]*big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.RevokeTEE(sigPubKey, reasonHash, slashAmount, signature)
}

func SubmitRedactAuthorization(req RedactReqFields, dPolicy [32]byte, expiresAt *big.Int, signature [2]*big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.SubmitRedactAuthorization(req, dPolicy, expiresAt, signature)
}

func RejectRedactRequest(req RedactReqFields, dPolicy [32]byte, reasonHash [32]byte, signature [2]*big.Int) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.RejectRedactRequest(req, dPolicy, reasonHash, signature)
}

func ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.ExecuteRedaction(reqID, rPrimeHash, proofHash, resultHash, piReqA)
}

func WaitTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.WaitTransactionReceipt(ctx, txHash)
}

func GetRedactRequest(reqID [32]byte) (*RedactRequestInfo, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetRedactRequest(reqID)
}

func IsRedactionExecuted(reqID [32]byte) (bool, error) {
	if serviceInstance == nil {
		return false, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.IsRedactionExecuted(reqID)
}

func ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	if serviceInstance == nil {
		return [32]byte{}, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.ComputeRedactionProofHash(reqID, dReq, activeSetHash, piReqA)
}

func GetSystemParams() (*SystemParams, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetSystemParams()
}

func GetPkCHHash() ([32]byte, error) {
	if serviceInstance == nil {
		return [32]byte{}, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetPkCHHash()
}

func SetPkCH(pkCH []byte, signature []byte) (string, error) {
	if serviceInstance == nil {
		return "", fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.SetPkCH(pkCH, signature)
}

func Stake(value *big.Int) error {
	return serviceInstance.Stake(value)
}

// 投票
func Delegate(value *big.Int, address common.Address) error {
	return serviceInstance.Delegate(value, address)
}

// 撤销投票
func RevokeDelegation(value *big.Int, address common.Address) error {
	return serviceInstance.RevokeDelegation(value, address)
}

// 设置每个轮次的区块长度
func SetRoundBlocks(round *big.Int) error {
	return serviceInstance.SetRoundBlocks(round)
}

// 设置轮次超时阈值
func SetRoundTimeout(time *big.Int) error {
	return serviceInstance.SetRoundTimeout(time)
}

// 更新最低保证金要求
func SetMinDeposit(value *big.Int) error {
	return serviceInstance.SetMinDeposit(value)
}

// 获取轮次的活跃节点数量
func GetActiveNodeNum(round *big.Int) (*big.Int, error) {
	return serviceInstance.GetActiveNodeNum(round)
}

// 当前候选人数量
func GetCandidatesNum() (*big.Int, error) {
	return serviceInstance.GetCandidatesNum()
}

func GetCandidates() []common.Address {
	if serviceInstance == nil {
		log.Error("Contract Service not initialized")
		return make([]common.Address, 0)
	}
	candidates, err := serviceInstance.GetCandidates()
	if err != nil {
		log.Error("GetCandidates error", "err", err)
		return make([]common.Address, 0)
	}
	return candidates
}

// 活跃验证者数量
func GetValidatorsNum() (*big.Int, error) {
	return serviceInstance.GetValidatorsNum()
}

func GetValidators() []common.Address {
	if serviceInstance == nil {
		log.Error("Contract Service not initialized")
		return make([]common.Address, 0)
	}
	validators, err := serviceInstance.GetValidators()
	if err != nil {
		log.Error("GetValidators error", "err", err)
		return make([]common.Address, 0)
	}
	return validators
}

// 申请退出的节点数量
func GetLeavingNodeNum() (*big.Int, error) {
	return serviceInstance.GetLeavingNodeNum()
}

// 特定节点在指定轮次提交的公钥
func GetRoundPublicKey(round *big.Int, address common.Address) (*big.Int, error) {

	return serviceInstance.GetRoundPublicKey(round, address)
}

func GetActiveTEEs() ([][32]byte, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetActiveTEEs()
}

func GetActiveSetInfo(epoch *big.Int) (*ActiveSetInfo, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetActiveSetInfo(epoch)
}

func GetCandidateTEEs() ([][32]byte, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetCandidateTEEs()
}

func GetTEEInfo(sigPubKey [32]byte) (*TEEInfo, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetTEEInfo(sigPubKey)
}

func GetTEEEnclavePK(sigPubKey [32]byte) ([]byte, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetTEEEnclavePK(sigPubKey)
}

func GetPendingTEE(owner common.Address) (*PendingTEEInfo, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetPendingTEE(owner)
}

func GetRegistrationChallenge(owner common.Address) ([32]byte, error) {
	if serviceInstance == nil {
		return [32]byte{}, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetRegistrationChallenge(owner)
}

func GetTEEStatusByOwner(owner common.Address) (*TEEStatusInfo, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetTEEStatusByOwner(owner)
}

func GetTEENum() (*big.Int, error) {
	if serviceInstance == nil {
		return nil, fmt.Errorf("contract service not initialized")
	}
	return serviceInstance.GetActiveTEENum()
}

func ParseTransactionInput(hash common.Hash) (string, error) {
	return serviceInstance.ParseTransactionInput(hash)
}
