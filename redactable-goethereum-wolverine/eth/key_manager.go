package eth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	ctypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// ReshareSessionConfig 描述一次 DKRS 会话的委员会/门限参数。
// 注意：OldThreshold/OldQual 故意不由调用方提供——前者仍读 localKey.Threshold，
// 后者仍由 reshareRequiredQualLocked() 确定性派生（取 OldCommittee 前 t+1 个），
// 以保持当前行为不变。
type ReshareSessionConfig struct {
	OldCommittee []int // 旧委员会节点索引（1-based）
	NewCommittee []int // 新委员会节点索引（1-based），位置即新域点索引
	NewThreshold int
}

// ReshareRole 显式表达本节点在当前会话中的角色。
type ReshareRole struct {
	IsDealer      bool
	DealerIndex   int // = nodeIndex+1（恒为 1-based 节点索引）；IsDealer 为假时无意义
	IsReceiver    bool
	ReceiverIndex int // 新委员会中的 1-based 位置；仅 IsReceiver 为真时有效
}

// KeyManager 管理NITCH密钥的分发和收集
type KeyManager struct {
	// 本节点的密钥信息（DistKG 完成后才非 nil）
	localKey *chameleon.NITCHKey

	// 公开参数（从文件加载或 Setup() 得到）
	publicParams *chameleon.PublicParameters

	// 阈值和节点数
	threshold  int
	numParties int

	// 委员会相关
	committeeSize     int  // 委员会规模（默认5）
	nodeIndex         int  // 本节点的索引（0-based）
	isCommitteeMember bool // 是否为委员会成员

	// 运行时委员会描述符状态 (Phase 3A)
	localNodeID      NodeID
	activeCommittee  *CommitteeDescriptor
	pendingCommittee *CommitteeDescriptor

	// 存储其他节点的公钥（用于验证份额）
	peerKeys map[common.Address]*chameleon.NITCHKey

	// 存储待处理的提案和收集到的份额
	pendingProposals map[string]*ProposalShares
	// Keep threshold completion after CombineShares removes the proposal.
	closedShareCollections sync.Map

	// ========== NITCH 分布式密钥生成 (DistKG) 协议状态 ==========
	distKGPending        bool                          // 是否处于等待 DistKG 完成状态
	distKGStartTime      time.Time                     // 本次 DistKG 从 startDkg/StartDistKG 开始的起始时间
	distKGMyDeal         *chameleon.DistKGDeal         // 本节点广播的 Deal（步骤1）
	distKGMyPrivate      *chameleon.DistKGDealPrivate  // 本节点私密数据（用于生成发给各方的 Share）
	distKGDeals          map[int]*chameleon.DistKGDeal // 收到的各成员 Deal，key=DealerIndex(1-based)
	distKGShares         map[int]*big.Int              // 收到的发给本节点的份额，key=FromIndex(1-based)
	distKGComplaints     []chameleon.DistKGComplaint   // 收集到的投诉（步骤2/3）
	distKGComplaintsSent bool                          // 是否已广播过本节点的投诉，避免重复

	// ========== DKRS 重分享协议状态 ==========
	resharePending           bool
	reshareSessionID         string
	reshareConfig            ReshareSessionConfig // 旧/新委员会与门限（显式会话配置）
	reshareRole              ReshareRole          // 本节点在当前会话中的显式 Dealer/Receiver 角色
	reshareMyDeal            *chameleon.DKRSDeal
	reshareMyPrivate         *chameleon.DKRSDealPrivate
	reshareDeals             map[int]*chameleon.DKRSDeal
	reshareShares            map[int]*big.Int // 发给本节点（新委员会索引）的份额，key=旧委员会成员索引
	reshareComplaints        []chameleon.DKRSComplaint
	reshareComplaintsSent    bool
	reshareVerifyKeys        map[int]*chameleon.DKRSVerifyKey
	reshareAppliedMessages   map[string]struct{}
	reshareRequestedInputs   map[string]reshareInputRequestState
	reshareRequestNonce      uint64
	resharePendingBuffers    map[string]*resharePendingBuffer
	resharePendingOrder      []string
	reshareOutboundCache     map[string]*reshareOutboundSession
	reshareOutboundOrder     []string
	reshareInputsReadyLogged bool
	reshareVerifyStarted     bool
	reshareVerified          bool
	reshareInputRevision     uint64
	reshareQualReadyLogged   bool

	// ========== 性能打点 / 完成轮询状态（决策 2，纯只读查询读取） ==========
	// distKGExtracted 在 DistKGExtractAndApply 成功后置位，供 eth_dkgStatus 轮询判定 system_init 完成。
	distKGExtracted bool
	// reshareExtracted 在 ReshareExtractAndApply 成功后置位，供 eth_reshareStatus 轮询判定每轮 rotation 完成。
	reshareExtracted      bool
	reshareDealerFinished bool   // dealer-only 会话完成（收齐验证密钥或交接）后置位
	reshareLastSession    string // 最近一次（已完成的）reshare 会话 ID
	// redactionsApplied 记录本节点已应用（updateBlockInfo 成功）的编辑，reqID=blockNum-Index。
	redactionsApplied map[string]bool

	// ========== 选举与轮换观测状态 (Phase 3B 只读状态) ==========
	lastElectionEpoch    uint64
	lastBoundary         uint64
	lastPreviousBoundary uint64
	lastTau              common.Hash
	lastNextTau          common.Hash
	lastSolutionCount    int

	mu sync.RWMutex
}

// NodeRegistry 节点注册表，用于跟踪节点启动顺序
type NodeRegistry struct {
	Nodes []RegisteredNode `json:"nodes"`
	mu    sync.Mutex       `json:"-"` // 不序列化
}

// RegisteredNode 已注册的节点信息
type RegisteredNode struct {
	NodeID      string    `json:"node_id"`                // 节点标识（如enode地址）
	Index       int       `json:"index"`                  // 节点索引（0-based）
	IsCommittee *bool     `json:"is_committee,omitempty"` // 是否委员会成员（兼容旧格式）
	Timestamp   time.Time `json:"timestamp"`              // 注册时间
}

// ProposalShares 存储提案的份额信息
type ProposalShares struct {
	ProposalID string
	OldMessage chameleon.Message
	NewMessage chameleon.Message
	HashOutput *chameleon.HashOutput
	Shares     []*chameleon.AdaptationShare
	Threshold  int

	sharesReady       chan struct{}
	sharesReadyClosed bool
}

var (
	globalKeyManager *KeyManager
	keyManagerOnce   sync.Once
	globalRegistry   *NodeRegistry
	registryOnce     sync.Once
)

const (
	// DefaultCommitteeSize 默认委员会规模（前5个节点）
	DefaultCommitteeSize = 5
	// DefaultTotalNodes 默认总节点数
	DefaultTotalNodes = 10
)

// RegisterNode 注册节点并返回节点索引
// 使用文件锁确保原子性，前5个节点成为委员会成员
func RegisterNode(dataDir string, nodeID string) (int, bool, int, error) {
	registryOnce.Do(func() {
		globalRegistry = &NodeRegistry{
			Nodes: make([]RegisteredNode, 0),
		}
	})

	// 将 node_registry.json 放在项目根目录下，而不是 node 文件夹下
	// 通过获取 dataDir 的父目录来找到项目根目录
	projectRoot := filepath.Dir(dataDir)
	registryFile := filepath.Join(projectRoot, "node_registry.json")

	// 使用文件锁确保原子性
	lockFile := registryFile + ".lock"
	lock, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		// 锁文件已存在，等待并重试
		time.Sleep(100 * time.Millisecond)
		return RegisterNode(dataDir, nodeID)
	}
	defer func() {
		lock.Close()
		os.Remove(lockFile)
	}()

	// 读取现有注册表
	registry := &NodeRegistry{Nodes: make([]RegisteredNode, 0)}
	if data, err := ioutil.ReadFile(registryFile); err == nil {
		if err := json.Unmarshal(data, registry); err != nil {
			log.Warn("Failed to parse node registry, creating new one", "err", err)
		}
	}

	committeeSize := getCommitteeSize(registry.Nodes)

	// 检查节点是否已注册
	for _, node := range registry.Nodes {
		if node.NodeID == nodeID {
			isCommittee := isCommitteeNode(node)
			log.Info("Node already registered", "nodeID", nodeID, "index", node.Index, "isCommittee", isCommittee)
			return node.Index, isCommittee, committeeSize, nil
		}
	}

	// 注册新节点
	newIndex := len(registry.Nodes)
	newNode := RegisteredNode{
		NodeID:    nodeID,
		Index:     newIndex,
		Timestamp: time.Now(),
	}
	if hasExplicitCommitteeConfig(registry.Nodes) {
		isCommittee := false
		newNode.IsCommittee = &isCommittee
	} else {
		isCommittee := newIndex < DefaultCommitteeSize
		newNode.IsCommittee = &isCommittee
	}
	registry.Nodes = append(registry.Nodes, newNode)
	committeeSize = getCommitteeSize(registry.Nodes)

	// 保存注册表
	registryData, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return -1, false, 0, fmt.Errorf("failed to marshal registry: %v", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(registryFile), 0700); err != nil {
		return -1, false, 0, fmt.Errorf("failed to create directory: %v", err)
	}

	if err := ioutil.WriteFile(registryFile, registryData, 0644); err != nil {
		return -1, false, 0, fmt.Errorf("failed to write registry file: %v", err)
	}

	isCommittee := isCommitteeNode(newNode)
	log.Info("Node registered successfully",
		"nodeID", nodeID,
		"index", newIndex,
		"isCommittee", isCommittee,
		"totalNodes", len(registry.Nodes))

	return newIndex, isCommittee, committeeSize, nil
}

func isCommitteeNode(node RegisteredNode) bool {
	if node.IsCommittee != nil {
		return *node.IsCommittee
	}
	return node.Index < DefaultCommitteeSize
}

func hasExplicitCommitteeConfig(nodes []RegisteredNode) bool {
	for _, node := range nodes {
		if node.IsCommittee != nil {
			return true
		}
	}
	return false
}

func getCommitteeSize(nodes []RegisteredNode) int {
	committeeSize := 0
	for _, node := range nodes {
		if isCommitteeNode(node) {
			committeeSize++
		}
	}
	if committeeSize == 0 {
		return DefaultCommitteeSize
	}
	return committeeSize
}

// InitKeyManager 初始化密钥管理器
// 在节点启动时调用，从文件读取密钥（如果存在），否则生成新密钥并保存到文件
// dataDir: 数据目录路径，用于存储密钥文件
// threshold: 阈值（委员会成员之间的阈值，默认2）
// nodeID: 节点唯一标识（如enode地址）
func InitKeyManager(dataDir string, threshold int, nodeID string) error {
	keyManagerOnce.Do(func() {
		// 支持 NITCH_NODE_ID 覆盖默认 nodeID，确保 bootstrap/descriptor 拥有稳定一致标识
		if envNodeID := strings.TrimSpace(os.Getenv("NITCH_NODE_ID")); envNodeID != "" {
			nodeID = envNodeID
		} else if nodeID == "" {
			nodeID = dataDir
		}
		if os.Getenv("NITCH_NODE_ID") == "" && nodeID != "" {
			_ = os.Setenv("NITCH_NODE_ID", nodeID)
		}

		// 注册节点，获取节点索引和是否为委员会成员
		nodeIndex, isCommittee, committeeSize, err := RegisterNode(dataDir, nodeID)
		if err != nil {
			log.Error("Failed to register node", "err", err)
			return
		}

		// 如果节点不是委员会成员，不分配密钥
		if !isCommittee {
			log.Info("Node is not a committee member, skipping key initialization",
				"nodeID", nodeID,
				"nodeIndex", nodeIndex)
			globalKeyManager = &KeyManager{
				localNodeID:       NodeID(nodeID),
				nodeIndex:         nodeIndex,
				isCommitteeMember: false,
				committeeSize:     committeeSize,
				numParties:        committeeSize,
				threshold:         threshold,
				peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
				pendingProposals:  make(map[string]*ProposalShares),
			}
			return
		}

		// pp 由首个发出 Deal 的节点提供并随 Deal 广播，其他节点从收到的 Deal 解析
		globalKeyManager = &KeyManager{
			localNodeID:       NodeID(nodeID),
			localKey:          nil,
			publicParams:      nil,
			threshold:         threshold,
			numParties:        committeeSize,
			committeeSize:     committeeSize,
			nodeIndex:         nodeIndex,
			isCommitteeMember: true,
			peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
			pendingProposals:  make(map[string]*ProposalShares),
			distKGPending:     true,
			distKGDeals:       nil,
			distKGShares:      nil,
			distKGComplaints:  nil,
		}

		log.Info("KeyManager initialized for committee member (DistKG)",
			"threshold", threshold,
			"committeeSize", committeeSize,
			"nodeIndex", nodeIndex)
	})

	if globalKeyManager == nil {
		return errors.New("failed to initialize KeyManager")
	}
	return nil
}

// GetKeyManager 获取全局密钥管理器实例
func GetKeyManager() *KeyManager {
	return globalKeyManager
}

// GetLocalKey 获取本节点的密钥
// 如果节点不是委员会成员，返回nil
func (km *KeyManager) GetLocalKey() *chameleon.NITCHKey {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.isCommitteeMember {
		return nil
	}
	return km.localKey
}

// IsCommitteeMember 检查节点是否为委员会成员
func (km *KeyManager) IsCommitteeMember() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.isCommitteeMember
}

// GetNodeIndex 获取节点索引
func (km *KeyManager) GetNodeIndex() int {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.nodeIndex
}

// GetCommitteeSize 获取委员会规模
func (km *KeyManager) GetCommitteeSize() int {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.committeeSize
}

// GetPublicParams 获取公开参数
func (km *KeyManager) GetPublicParams() *chameleon.PublicParameters {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.publicParams
}

// GetThreshold 获取阈值
func (km *KeyManager) GetThreshold() int {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.threshold
}

// localNodeIDLocked 返回当前有效的 NodeID（优先本地设置，其次 NITCH_NODE_ID 环境变量）
func (km *KeyManager) localNodeIDLocked() NodeID {
	if km == nil {
		return ""
	}
	if km.localNodeID != "" {
		return km.localNodeID
	}
	if envID := strings.TrimSpace(os.Getenv("NITCH_NODE_ID")); envID != "" {
		return NodeID(envID)
	}
	return ""
}

// SetLocalNodeID 设置本节点的 NodeID
func (km *KeyManager) SetLocalNodeID(id NodeID) {
	if km == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	km.localNodeID = id
	if km.activeCommittee != nil {
		_, isMember := km.activeCommittee.MemberIndex(id)
		km.isCommitteeMember = isMember
	}
}

// GetLocalNodeID 获取本节点的 NodeID
func (km *KeyManager) GetLocalNodeID() NodeID {
	if km == nil {
		return ""
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.localNodeIDLocked()
}

// ActiveCommittee 返回当前活跃委员会描述符的副本（线程安全）
func (km *KeyManager) ActiveCommittee() *CommitteeDescriptor {
	if km == nil {
		return nil
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	if km.activeCommittee == nil {
		return nil
	}
	cp := km.activeCommittee.Clone()
	return &cp
}

// PendingCommittee 返回当前待激活委员会描述符的副本（线程安全）
func (km *KeyManager) PendingCommittee() *CommitteeDescriptor {
	if km == nil {
		return nil
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	if km.pendingCommittee == nil {
		return nil
	}
	cp := km.pendingCommittee.Clone()
	return &cp
}

// SetActiveCommittee 设置活跃委员会描述符，并动态同步 KeyManager 状态
func (km *KeyManager) SetActiveCommittee(desc *CommitteeDescriptor) {
	if km == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	km.setActiveCommitteeLocked(desc)
}

func (km *KeyManager) setActiveCommitteeLocked(desc *CommitteeDescriptor) {
	if desc == nil {
		km.activeCommittee = nil
		return
	}
	cp := desc.Clone()
	km.activeCommittee = &cp
	km.committeeSize = len(cp.Members)
	km.numParties = len(cp.Members)
	km.threshold = cp.Threshold
	localID := km.localNodeIDLocked()
	if localID != "" {
		if km.localNodeID == "" {
			km.localNodeID = localID
		}
		// 运行时身份一律按 descriptor 解析（Phase 3A）：nodeIndex/isCommitteeMember
		// 不再依赖 node_registry 顺序，而是本节点在描述符中的 epoch-local 位置。
		if idx, isMember := cp.MemberIndex(localID); isMember {
			km.nodeIndex = idx - 1
			km.isCommitteeMember = true
		} else {
			km.isCommitteeMember = false
		}
	}
}

// descriptorModeLocked 报告是否处于运行时描述符模式（Phase 3A）。
// 仅当 active 描述符与本地 NodeID 均已就绪时，身份解析才以 descriptor 为准；
// 否则（含全部现有手工/测试路径）维持 registry bootstrap 索引行为不变。
func (km *KeyManager) descriptorModeLocked() bool {
	return km.activeCommittee != nil && km.localNodeIDLocked() != ""
}

// localDealerIndexLocked 返回本节点在当前 active 委员会中的 1-based dealer 索引；
// 非成员返回 0。描述符模式下按 NodeID 位置解析，否则退回 registry 索引。
func (km *KeyManager) localDealerIndexLocked() int {
	if km.descriptorModeLocked() {
		idx, ok := km.activeCommittee.MemberIndex(km.localNodeIDLocked())
		if !ok {
			return 0
		}
		return idx
	}
	if km.isCommitteeMember {
		return km.nodeIndex + 1
	}
	return 0
}

// reshareRoleForConfigLocked 显式构造本节点在本次会话中的 Dealer/Receiver 角色。
// 描述符模式下按 NodeID 在 active（旧）/pending（新）描述符中的位置解析，
// 与 registry index 完全解耦：被 election 选中的新成员即使 registry 索引恰好
// 落在旧域区间内，也不会被误判为 dealer（dealer 需要 SecretShare）。
// 非描述符模式维持原 registry 索引行为（现有手工 eth_startReshare 与测试）。
func (km *KeyManager) reshareRoleForConfigLocked(cfg ReshareSessionConfig) ReshareRole {
	if !km.descriptorModeLocked() {
		role := ReshareRole{
			IsDealer:    containsInt(cfg.OldCommittee, km.nodeIndex+1),
			DealerIndex: km.nodeIndex + 1,
		}
		if newPos := indexInList(cfg.NewCommittee, km.nodeIndex+1); newPos >= 0 {
			role.IsReceiver = true
			role.ReceiverIndex = newPos + 1
		}
		return role
	}
	role := ReshareRole{}
	localID := km.localNodeIDLocked()
	if idx, ok := km.activeCommittee.MemberIndex(localID); ok {
		role.IsDealer = true
		role.DealerIndex = idx
	}
	if km.pendingCommittee != nil {
		if idx, ok := km.pendingCommittee.MemberIndex(localID); ok {
			role.IsReceiver = true
			role.ReceiverIndex = idx
		}
	} else if role.IsDealer {
		// 仅有 active 描述符（尚未 election）时的手工会话：沿旧的位置语义，
		// 但键值使用描述符 dealer 索引而不是 registry 索引。
		if newPos := indexInList(cfg.NewCommittee, role.DealerIndex); newPos >= 0 {
			role.IsReceiver = true
			role.ReceiverIndex = newPos + 1
		}
	}
	return role
}

// SetPendingCommittee 设置待激活委员会描述符
func (km *KeyManager) SetPendingCommittee(desc *CommitteeDescriptor) {
	if km == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if desc == nil {
		km.pendingCommittee = nil
		return
	}
	cp := desc.Clone()
	km.pendingCommittee = &cp
}

// ActivatePendingCommittee 将 pending 委员会激活为 active 委员会，并清空 pending
func (km *KeyManager) ActivatePendingCommittee() (*CommitteeDescriptor, error) {
	if km == nil {
		return nil, errors.New("nil KeyManager")
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if km.pendingCommittee == nil {
		return nil, errors.New("no pending committee to activate")
	}
	desc := km.pendingCommittee
	km.pendingCommittee = nil
	km.setActiveCommitteeLocked(desc)
	cp := desc.Clone()
	return &cp, nil
}

func peerKeyAddressForIndex(index int) common.Address {
	return common.BigToAddress(big.NewInt(int64(index)))
}

func (km *KeyManager) registerPeerKeyLocked(peerAddr common.Address, key *chameleon.NITCHKey) {
	if key == nil {
		return
	}
	if km.peerKeys == nil {
		km.peerKeys = make(map[common.Address]*chameleon.NITCHKey)
	}
	for addr, existing := range km.peerKeys {
		if existing != nil && existing.Index == key.Index && addr != peerAddr {
			delete(km.peerKeys, addr)
		}
	}
	km.peerKeys[peerAddr] = key
	log.Info("Registered peer key", "peer", peerAddr, "index", key.Index)
}

// RegisterPeerKey 注册其他节点的公钥
func (km *KeyManager) RegisterPeerKey(peerAddr common.Address, key *chameleon.NITCHKey) {
	km.mu.Lock()
	defer km.mu.Unlock()
	km.registerPeerKeyLocked(peerAddr, key)
}

func (km *KeyManager) registerDKRSVerifyKeyLocked(vk *chameleon.DKRSVerifyKey) error {
	if vk == nil || vk.Index == nil {
		return nil
	}
	idx, err := chameleonIndexToInt(vk.Index)
	if err != nil {
		return err
	}
	vkPoint, err := vk.GetVerificationKeyPoint()
	if err != nil {
		return err
	}
	key := &chameleon.NITCHKey{
		PP:              km.publicParams,
		VerificationKey: vkPoint,
		Index:           idx,
		Threshold:       km.threshold,
		NumParties:      km.numParties,
	}
	if km.localKey != nil {
		key.GlobalPK = km.localKey.GlobalPK
		key.GlobalPKG1 = km.localKey.GlobalPKG1
	}
	km.registerPeerKeyLocked(peerKeyAddressForIndex(idx), key)
	return nil
}

// GenerateAdaptationShare 为本节点生成适应份额
// 如果节点不是委员会成员，返回错误
func (km *KeyManager) GenerateAdaptationShare(oldMsg, newMsg chameleon.Message) (*chameleon.AdaptationShare, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if !km.isCommitteeMember {
		return nil, errors.New("node is not a committee member")
	}

	if km.localKey == nil {
		return nil, errors.New("local key not initialized")
	}

	return chameleon.PartialAdapt(km.localKey, oldMsg, newMsg)
}

// AddShare 添加其他节点的份额到提案
func (km *KeyManager) AddShare(proposalID string, share *chameleon.AdaptationShare, oldMsg, newMsg chameleon.Message, hashOutput *chameleon.HashOutput) error {
	if km.ShareCollectionClosed(proposalID) {
		return nil
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	// A different handler may have supplied the threshold share while we waited.
	if km.ShareCollectionClosed(proposalID) {
		return nil
	}

	proposal, exists := km.pendingProposals[proposalID]
	if !exists {
		// 创建新的提案
		proposal = &ProposalShares{
			ProposalID: proposalID,
			OldMessage: oldMsg,
			NewMessage: newMsg,
			HashOutput: hashOutput,
			Shares:     make([]*chameleon.AdaptationShare, 0),
			Threshold:  km.threshold,
		}
		km.pendingProposals[proposalID] = proposal
	}

	// 验证份额：根据 share.Index 找到对应的 peer key 进行验证
	// 由于所有密钥共享相同的 GlobalPK 和 PP，我们可以通过 Index 匹配
	var peerKey *chameleon.NITCHKey
	for _, pk := range km.peerKeys {
		if pk != nil && share.Index != nil && share.Index.Cmp(big.NewInt(int64(pk.Index))) == 0 {
			peerKey = pk
			break
		}
	}

	// 如果找不到对应的 peer key，尝试使用本地密钥（如果 Index 匹配）
	if peerKey == nil && km.localKey != nil && share.Index != nil && share.Index.Cmp(big.NewInt(int64(km.localKey.Index))) == 0 {
		peerKey = km.localKey
	}

	// 如果仍然找不到，记录警告但继续处理（可能是本节点自己的份额）
	if peerKey == nil {
		log.Warn("Peer key not found for share index, skipping verification",
			"proposalID", proposalID,
			"index", share.Index)
		// 注意：如果是本节点自己的份额，会在 GenerateAdaptationShare 时已经验证
	} else {
		// 使用 PartialVerify 验证份额
		if !chameleon.PartialVerify(peerKey, share, oldMsg, newMsg) {
			log.Warn("Share verification failed",
				"proposalID", proposalID,
				"index", share.Index)
			return errors.New("share verification failed")
		}
		log.Info("Share verified successfully",
			"proposalID", proposalID,
			"index", share.Index)
	}

	// 检查是否已存在该索引的份额
	for _, s := range proposal.Shares {
		if (s.Index == nil && share.Index == nil) || (s.Index != nil && share.Index != nil && s.Index.Cmp(share.Index) == 0) {
			log.Warn("Share already exists for this index", "proposalID", proposalID, "index", share.Index)
			return nil
		}
	}

	proposal.Shares = append(proposal.Shares, share)
	if len(proposal.Shares) >= km.threshold+1 {
		km.closedShareCollections.Store(proposalID, struct{}{})
	}
	proposal.notifySharesReadyLocked(km.threshold + 1)
	log.Info("Added share to proposal",
		"proposalID", proposalID,
		"index", share.Index,
		"totalShares", len(proposal.Shares))

	return nil
}

// CombineShares 组合份额生成r'
func (km *KeyManager) CombineShares(proposalID string) (*big.Int, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	proposal, exists := km.pendingProposals[proposalID]
	if !exists {
		return nil, errors.New("proposal not found")
	}

	if len(proposal.Shares) < km.threshold+1 {
		return nil, errors.New("insufficient shares")
	}

	// 调用Combine函数
	rPrime, err := chameleon.Combine(
		km.publicParams,
		proposal.Shares,
		km.threshold,
		km.numParties,
		proposal.HashOutput,
		proposal.OldMessage,
		proposal.NewMessage,
	)

	if err != nil {
		return nil, err
	}

	// 清理提案
	delete(km.pendingProposals, proposalID)

	return rPrime, nil
}

// CreateProposal 创建新的提案并初始化份额收集
func (km *KeyManager) CreateProposal(proposalID string, oldMsg, newMsg chameleon.Message, hashOutput *chameleon.HashOutput) {
	km.mu.Lock()
	defer km.mu.Unlock()

	km.closedShareCollections.Delete(proposalID)
	km.pendingProposals[proposalID] = &ProposalShares{
		ProposalID: proposalID,
		OldMessage: oldMsg,
		NewMessage: newMsg,
		HashOutput: hashOutput,
		Shares:     make([]*chameleon.AdaptationShare, 0),
		Threshold:  km.threshold,
	}
}

// GetProposalSharesCount 获取提案的份额数量
func (km *KeyManager) GetProposalSharesCount(proposalID string) int {
	km.mu.RLock()
	defer km.mu.RUnlock()

	proposal, exists := km.pendingProposals[proposalID]
	if !exists {
		return 0
	}
	return len(proposal.Shares)
}

// ShareCollectionClosed allows late shares to be dropped without taking km.mu.
func (km *KeyManager) ShareCollectionClosed(proposalID string) bool {
	_, closed := km.closedShareCollections.Load(proposalID)
	return closed
}

// ProposalSharesReady returns a notification closed when the threshold is met.
func (km *KeyManager) ProposalSharesReady(proposalID string) (<-chan struct{}, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	proposal, exists := km.pendingProposals[proposalID]
	if !exists {
		return nil, errors.New("proposal not found")
	}
	return proposal.notifySharesReadyLocked(km.threshold + 1), nil
}

// notifySharesReadyLocked is called with the owning KeyManager's mutex held.
func (proposal *ProposalShares) notifySharesReadyLocked(required int) <-chan struct{} {
	if proposal.sharesReady == nil {
		proposal.sharesReady = make(chan struct{})
	}
	if !proposal.sharesReadyClosed && len(proposal.Shares) >= required {
		close(proposal.sharesReady)
		proposal.sharesReadyClosed = true
	}
	return proposal.sharesReady
}

// HasEnoughShares 检查是否有足够的份额
func (km *KeyManager) HasEnoughShares(proposalID string) bool {
	return km.GetProposalSharesCount(proposalID) >= km.threshold+1
}

// ========== NITCH 分布式密钥生成 (DistKG) 协议方法 ==========

// IsDistKGPending 是否正在等待 DistKG 完成（无本地密钥、需与委员会共同生成）
func (km *KeyManager) IsDistKGPending() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.distKGPending
}

// NeedToSendDistKGDeal 是否尚未发出本节点的 Deal（用于在首次有 peer 或收到他人 Deal 时触发广播）
func (km *KeyManager) NeedToSendDistKGDeal() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.distKGPending && km.distKGMyDeal == nil
}

// StartDistKG 步骤1：生成本节点的 Deal 与私密份额，供广播与私发
// 仅在 distKGPending 且为委员会成员时有效。若尚无 publicParams（本节点首个发起），则先调用 Setup() 生成并随 Deal 广播，其他节点从收到的 Deal 解析 pp。
// 返回本节点 Deal 及应发给各成员的 Share 列表（key=ToIndex 1-based）
func (km *KeyManager) StartDistKG() (*chameleon.DistKGDeal, map[int]*chameleon.DistKGShare, error) {
	start := time.Now()
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.distKGPending || !km.isCommitteeMember {
		return nil, nil, errors.New("cannot start DistKG: not pending or not committee")
	}
	// 新一轮 DistKG 启动，清掉上一轮的 extracted 标志。
	km.distKGExtracted = false
	// 记录本次 DistKG 的起始时间（仅在首次开始时设置）
	if km.distKGStartTime.IsZero() {
		km.distKGStartTime = time.Now()
	}
	// 若尚无公开参数（本节点为首个发起），则生成一份并随 Deal 广播，保证全网共用同一 (g1,g2)
	if km.publicParams == nil {
		pp, err := chameleon.Setup()
		if err != nil {
			return nil, nil, fmt.Errorf("Setup() for first Deal: %w", err)
		}
		km.publicParams = pp
	}
	// 委员会内使用 1-based 索引：nodeIndex 0 -> 1, 1 -> 2, ...
	myIndex1Based := km.nodeIndex + 1
	deal, priv, err := chameleon.CreateDistKGDeal(km.publicParams, myIndex1Based, km.threshold, km.committeeSize)
	if err != nil {
		return nil, nil, err
	}
	km.distKGMyDeal = deal
	km.distKGMyPrivate = priv
	if km.distKGDeals == nil {
		km.distKGDeals = make(map[int]*chameleon.DistKGDeal)
		km.distKGShares = make(map[int]*big.Int)
	}
	km.distKGDeals[myIndex1Based] = deal
	km.distKGShares[myIndex1Based] = priv.Shares[myIndex1Based] // 自己的份额（自己发给自己的）

	sharesToSend := make(map[int]*chameleon.DistKGShare)
	for j := 1; j <= km.committeeSize; j++ {
		if s, ok := priv.Shares[j]; ok {
			sharesToSend[j] = &chameleon.DistKGShare{
				FromIndex: big.NewInt(int64(myIndex1Based)),
				ToIndex:   big.NewInt(int64(j)),
				Share:     s,
			}
		}
	}
	// 统计步骤1本地计算耗时以及自 DistKG 启动以来的耗时
	stepDuration := time.Since(start)
	var sinceStart time.Duration
	if !km.distKGStartTime.IsZero() {
		sinceStart = time.Since(km.distKGStartTime)
	}
	log.Info("DistKG timing: step1 StartDistKG completed", "compute", stepDuration, "sinceStart", sinceStart)
	// 性能打点：阶段1 system_init · 启动 DistKG 并生成 Deal（单次，join_key=dkg-1）
	log.Info("E2E_TIMING", "stage", "system_init", "event", "distkg_start",
		"duration_ns", stepDuration.Nanoseconds(), "join_key", "dkg-1",
		"since_start_ns", sinceStart.Nanoseconds(), "node", km.nodeIndex, "status", "ok")
	return deal, sharesToSend, nil
}

// GetDistKGDealAndShares 获取已有的 Deal 和 Shares（用于重新广播）
// 如果已有 deal，返回 deal 和对应的 shares；否则返回 nil
func (km *KeyManager) GetDistKGDealAndShares() (*chameleon.DistKGDeal, map[int]*chameleon.DistKGShare, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.distKGMyDeal == nil || km.distKGMyPrivate == nil {
		return nil, nil, nil
	}

	// 重新生成 shares
	myIndex1Based := km.nodeIndex + 1
	sharesToSend := make(map[int]*chameleon.DistKGShare)
	for j := 1; j <= km.committeeSize; j++ {
		if s, ok := km.distKGMyPrivate.Shares[j]; ok {
			sharesToSend[j] = &chameleon.DistKGShare{
				FromIndex: big.NewInt(int64(myIndex1Based)),
				ToIndex:   big.NewInt(int64(j)),
				Share:     s,
			}
		}
	}
	return km.distKGMyDeal, sharesToSend, nil
}

// OnDistKGDeal 处理收到的 Deal（步骤1 广播）
// 若本节点尚无 publicParams，从 Deal 携带的 G1/G2/P 解析并设为统一公开参数，保证与其它节点使用同一 (g1,g2)
func (km *KeyManager) OnDistKGDeal(deal *chameleon.DistKGDeal) error {
	dealRecvStart := time.Now()
	log.Error("DistKG: received deal")
	if deal == nil {
		return errors.New("nil deal")
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.distKGPending {
		return nil
	}
	// 从首个携带公开参数的 Deal 解析 pp，保证所有节点同一 (g1,g2)
	if km.publicParams == nil {
		pp, err := chameleon.PublicParametersFromDeal(deal)
		if err == nil {
			km.publicParams = pp
			log.Info("DistKG: using public parameters from received deal", "dealerIndex", deal.DealerIndex)
		}
	}
	if km.distKGDeals == nil {
		km.distKGDeals = make(map[int]*chameleon.DistKGDeal)
		km.distKGShares = make(map[int]*big.Int)
	}
	dealerIdx, err := chameleonIndexToInt(deal.DealerIndex)
	if err != nil {
		return err
	}
	km.distKGDeals[dealerIdx] = deal
	// 性能打点：阶段1 system_init · 收到 Deal（单次，join_key=dkg-1）
	log.Info("E2E_TIMING", "stage", "system_init", "event", "distkg_deal_received",
		"duration_ns", time.Since(dealRecvStart).Nanoseconds(), "join_key", "dkg-1",
		"node", km.nodeIndex, "dealer", dealerIdx, "status", "ok")
	return nil
}

// OnDistKGShare 处理收到的份额（步骤1 私密发送）
func (km *KeyManager) OnDistKGShare(share *chameleon.DistKGShare) error {
	recvStart := time.Now()
	if share == nil || share.Share == nil {
		return errors.New("nil share or share value")
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.distKGPending {
		return nil
	}
	myIndex1Based := km.nodeIndex + 1
	toIdx, err := chameleonIndexToInt(share.ToIndex)
	if err != nil {
		return err
	}
	if toIdx != myIndex1Based {
		return nil // 不是发给我的，忽略
	}
	log.Error("DistKG: received share")
	if km.distKGShares == nil {
		km.distKGShares = make(map[int]*big.Int)
	}
	fromIdx, err := chameleonIndexToInt(share.FromIndex)
	if err != nil {
		return err
	}
	km.distKGShares[fromIdx] = share.Share
	// 性能打点：阶段1 system_init · 收到 Share（单次耗时，join_key=dkg-1）
	log.Info("E2E_TIMING", "stage", "system_init", "event", "distkg_share_received",
		"duration_ns", time.Since(recvStart).Nanoseconds(), "join_key", "dkg-1",
		"node", km.nodeIndex, "from", fromIdx, "status", "ok")
	return nil
}

// OnDistKGComplaint 处理投诉（步骤2/3）
func (km *KeyManager) OnDistKGComplaint(c *chameleon.DistKGComplaint) {
	log.Error("DistKG: received OnDistKGComplaint")
	if c == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	km.distKGComplaints = append(km.distKGComplaints, *c)
}

// OnDistKGResponse 处理被投诉方的回应（步骤3：用正确份额覆盖）
func (km *KeyManager) OnDistKGResponse(resp *chameleon.DistKGResponse) {
	log.Error("DistKG: received OnDistKGResponse")
	if resp == nil || resp.Share == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	myIndex1Based := km.nodeIndex + 1
	toIdx, err := chameleonIndexToInt(resp.ToIndex)
	if err != nil || toIdx != myIndex1Based {
		return
	}
	if km.distKGShares == nil {
		km.distKGShares = make(map[int]*big.Int)
	}
	fromIdx, err := chameleonIndexToInt(resp.DealerIndex)
	if err != nil {
		return
	}
	km.distKGShares[fromIdx] = resp.Share
}

// DistKGHasEnoughData 是否已收齐所有委员的 Deal 和发给本节点的 Share（可进入验证/投诉阶段）
func (km *KeyManager) DistKGHasEnoughData() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.distKGPending || km.distKGDeals == nil || km.distKGShares == nil {
		return false
	}
	for i := 1; i <= km.committeeSize; i++ {
		if km.distKGDeals[i] == nil || km.distKGShares[i] == nil {
			return false
		}
	}
	return true
}

// DistKGMissingInputs returns the original dealer inputs still required by a
// committee receiver. It is intentionally read-only so the transport recovery
// loop cannot bypass normal Deal/Share processing.
func (km *KeyManager) DistKGMissingInputs() (missingDeals, missingShares []int) {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.distKGPending || !km.isCommitteeMember {
		return nil, nil
	}
	for i := 1; i <= km.committeeSize; i++ {
		if km.distKGDeals == nil || km.distKGDeals[i] == nil {
			missingDeals = append(missingDeals, i)
		}
		if km.distKGShares == nil || km.distKGShares[i] == nil {
			missingShares = append(missingShares, i)
		}
	}
	return missingDeals, missingShares
}

// DistKGVerifyAndComplain 验证所有收到的份额，返回需要广播的投诉列表（步骤2）
// 仅首次调用时返回投诉并标记已发送，后续调用返回 nil
func (km *KeyManager) DistKGVerifyAndComplain() ([]chameleon.DistKGComplaint, error) {
	start := time.Now()
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.distKGPending || km.publicParams == nil {
		return nil, errors.New("not in DistKG or no public params")
	}
	if km.distKGComplaintsSent {
		return nil, nil
	}
	myIndex1Based := km.nodeIndex + 1
	var complaints []chameleon.DistKGComplaint
	for i := 1; i <= km.committeeSize; i++ {
		deal := km.distKGDeals[i]
		share := km.distKGShares[i]
		if deal == nil || share == nil {
			continue
		}
		ok, err := chameleon.VerifyDistKGShare(km.publicParams, deal, share, myIndex1Based)
		if err != nil || !ok {
			complaints = append(complaints, chameleon.DistKGComplaint{
				ComplainantIndex: big.NewInt(int64(myIndex1Based)),
				DealerIndex:      big.NewInt(int64(i)),
			})
		}
	}
	km.distKGComplaintsSent = true
	// 统计步骤2本地计算耗时以及自 DistKG 启动以来的耗时
	stepDuration := time.Since(start)
	var sinceStart time.Duration
	if !km.distKGStartTime.IsZero() {
		sinceStart = time.Since(km.distKGStartTime)
	}
	log.Info("DistKG timing: step2 VerifyAndComplain completed", "compute", stepDuration, "sinceStart", sinceStart, "complaints", len(complaints))
	return complaints, nil
}

// chameleonIndexToInt converts DistKG index fields (*big.Int) to int.
func chameleonIndexToInt(v *big.Int) (int, error) {
	if v == nil || !v.IsInt64() {
		return 0, errors.New("invalid index")
	}
	x := v.Int64()
	if x < 0 || x > int64(int(^uint(0)>>1)) {
		return 0, errors.New("index out of range")
	}
	return int(x), nil
}

// DistKGCanExtract 根据当前投诉计算 QUAL 后，是否至少有 t+1 名合格成员且本节点已具备提取所需数据
func (km *KeyManager) DistKGCanExtract() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.distKGPending || km.distKGDeals == nil || km.distKGShares == nil {
		return false
	}
	qual := chameleon.BuildQUAL(km.distKGComplaints, km.threshold, km.committeeSize)
	if len(qual) < km.threshold+1 {
		return false
	}
	for _, i := range qual {
		if km.distKGDeals[i] == nil || km.distKGShares[i] == nil {
			return false
		}
	}
	return true
}

// DistKGExtractAndApply 步骤4：根据 QUAL 提取本节点密钥到内存，关闭 distKGPending（不写文件）
func (km *KeyManager) DistKGExtractAndApply() error {
	stepStart := time.Now()
	log.Error("DistKG: extracting key")
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.distKGPending || km.publicParams == nil {
		return errors.New("not in DistKG or no public params")
	}
	qual := chameleon.BuildQUAL(km.distKGComplaints, km.threshold, km.committeeSize)
	if len(qual) < km.threshold+1 {
		return errors.New("QUAL has fewer than t+1 members")
	}
	deals := make(map[int]*chameleon.DistKGDeal)
	sharesToMe := make(map[int]*big.Int)
	for _, i := range qual {
		deals[i] = km.distKGDeals[i]
		sharesToMe[i] = km.distKGShares[i]
	}
	myIndex1Based := km.nodeIndex + 1
	key, err := chameleon.DistKGExtract(km.publicParams, myIndex1Based, qual, deals, sharesToMe)
	if err != nil {
		return err
	}
	km.localKey = key
	// 设置系统 NITCHKey（公钥视图），供区块哈希计算使用
	chameleon.SetSystemNITCHKeyFromKey(key)
	// 计算本次 DistKG 从开始到提取完成的耗时
	var elapsed time.Duration
	if !km.distKGStartTime.IsZero() {
		elapsed = time.Since(km.distKGStartTime)
	}
	stepDuration := time.Since(stepStart)
	km.distKGPending = false
	km.distKGExtracted = true // 性能打点：标记 system_init 完成，供 eth_dkgStatus 轮询
	km.distKGStartTime = time.Time{}
	km.distKGMyDeal = nil
	km.distKGMyPrivate = nil
	km.distKGDeals = nil
	km.distKGShares = nil
	km.distKGComplaints = nil
	log.Info("DistKG timing: step4 ExtractAndApply completed", "compute", stepDuration, "sinceStart", elapsed)
	log.Info("DistKG completed: key extracted", "nodeIndex", km.nodeIndex, "pk", key.GlobalPK, "duration", elapsed)
	// 性能打点：阶段1 system_init · 提取并应用密钥（单次耗时，join_key=dkg-1 跨节点关联）
	log.Info("E2E_TIMING", "stage", "system_init", "event", "distkg_extract_apply",
		"duration_ns", stepDuration.Nanoseconds(), "join_key", "dkg-1",
		"since_start_ns", elapsed.Nanoseconds(), "node", km.nodeIndex, "status", "ok")
	return nil
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func indexInList(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

func isValidCommitteeList(list []int) bool {
	if len(list) == 0 {
		return false
	}
	seen := make(map[int]struct{}, len(list))
	for _, v := range list {
		if v <= 0 {
			return false
		}
		if _, ok := seen[v]; ok {
			return false
		}
		seen[v] = struct{}{}
	}
	return true
}

// StartReshare 启动 DKRS 重分享会话。
// cfg.OldCommittee/cfg.NewCommittee 以节点索引（1-based）表示。
func (km *KeyManager) StartReshare(sessionID string, cfg ReshareSessionConfig) (*chameleon.DKRSDeal, map[int]*chameleon.DKRSShare, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.publicParams == nil || km.localKey == nil {
		return nil, nil, errors.New("local key or public params not initialized")
	}
	if sessionID == "" {
		return nil, nil, errors.New("empty reshare sessionID")
	}
	if ctypes.IsReshareWireSessionID(sessionID) {
		return nil, nil, errors.New("wire session namespace is reserved")
	}
	if !isValidCommitteeList(cfg.OldCommittee) || !isValidCommitteeList(cfg.NewCommittee) {
		return nil, nil, errors.New("invalid old/new committee list")
	}
	if cfg.NewThreshold < 0 || cfg.NewThreshold >= len(cfg.NewCommittee) {
		return nil, nil, errors.New("invalid newThreshold")
	}
	if km.reshareExtracted && km.reshareLastSession == sessionID {
		return nil, nil, nil
	}
	if km.resharePending && km.reshareSessionID == sessionID {
		if cache := km.reshareOutboundCache[sessionID]; cache != nil {
			shares := make(map[int]*chameleon.DKRSShare, len(cache.shares))
			for idx, share := range cache.shares {
				shares[idx] = cloneDKRSShare(share)
			}
			return cloneDKRSDeal(cache.deal), shares, nil
		}
		return nil, nil, nil
	}

	// 显式构造本节点角色：Dealer（在旧委员会）与 Receiver（在新委员会）。
	// Phase 3A：描述符模式下按 NodeID 位置解析（见 reshareRoleForConfigLocked），
	// 使 registry index 与 DKRS 身份完全解耦；非描述符模式维持原行为。
	role := km.reshareRoleForConfigLocked(cfg)
	if !role.IsDealer && !role.IsReceiver {
		return nil, nil, errors.New("node is neither in old nor new committee")
	}
	if role.IsDealer && (km.localKey == nil || km.localKey.SecretShare == nil) {
		// dealer 必须持有旧代真实份额；新成员绝不能凭 registry 索引冒充 dealer。
		return nil, nil, errors.New("dealer has no old-generation secret share")
	}

	// 性能打点：rotation_flow 的统一起点。参数、幂等和角色检查已完成，
	// 但本轮 session state、Deal/Share 尚未初始化或生成；事件名保持
	// reshare_start 以兼容现有 collector，duration_ns=0 表示点事件。
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "reshare_start",
		"duration_ns", 0, "join_key", sessionID,
		"node", km.nodeIndex, "dealer", role.IsDealer, "receiver", role.IsReceiver,
		"status", "ok")

	// 新一轮 reshare 启动，清掉上一轮的 extracted 标志。
	stateInitStart := time.Now()
	km.reshareExtracted = false
	km.resharePending = true
	km.reshareSessionID = sessionID
	km.reshareConfig = ReshareSessionConfig{
		OldCommittee: append([]int(nil), cfg.OldCommittee...),
		NewCommittee: append([]int(nil), cfg.NewCommittee...),
		NewThreshold: cfg.NewThreshold,
	}
	km.reshareRole = role
	km.reshareDeals = make(map[int]*chameleon.DKRSDeal)
	km.reshareShares = make(map[int]*big.Int)
	km.reshareComplaints = nil
	km.reshareComplaintsSent = false
	km.reshareInputsReadyLogged = false
	km.reshareVerifyStarted = false
	km.reshareVerified = false
	km.reshareInputRevision++
	km.reshareQualReadyLogged = false
	km.reshareVerifyKeys = make(map[int]*chameleon.DKRSVerifyKey)
	km.reshareAppliedMessages = make(map[string]struct{})
	km.reshareRequestedInputs = make(map[string]reshareInputRequestState)
	km.reshareRequestNonce = 0
	km.reshareMyDeal = nil
	km.reshareMyPrivate = nil
	km.ensureResharePendingBuffersLocked()
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rotation_session_init",
		"duration_ns", time.Since(stateInitStart).Nanoseconds(), "join_key", sessionID,
		"node", km.nodeIndex, "status", "ok")

	var deal *chameleon.DKRSDeal
	sharesToSend := make(map[int]*chameleon.DKRSShare)
	// receiver-only 节点不造 Deal，仅重放缓冲并等待 dealer 投递。
	if !role.IsDealer {
		km.replayResharePendingLocked(sessionID)
		return nil, nil, nil
	}

	dealCreateStart := time.Now()
	deal, priv, err := chameleon.CreateDKRSDeal(
		km.publicParams,
		role.DealerIndex,
		km.localKey.SecretShare,
		km.localKey.Threshold,
		cfg.NewThreshold,
		len(cfg.NewCommittee),
	)
	if err != nil {
		return nil, nil, err
	}
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_deal_create",
		"duration_ns", time.Since(dealCreateStart).Nanoseconds(), "join_key", sessionID,
		"node", km.nodeIndex, "dealer", role.DealerIndex, "status", "ok")
	deal.OldNumParties = big.NewInt(int64(km.localKey.NumParties))
	km.reshareMyDeal = deal
	km.reshareMyPrivate = priv
	km.reshareDeals[role.DealerIndex] = deal

	shareBuildStart := time.Now()
	for j := 1; j <= len(cfg.NewCommittee); j++ {
		if s, ok := priv.Shares[j]; ok {
			sharesToSend[j] = &chameleon.DKRSShare{
				FromIndex: big.NewInt(int64(role.DealerIndex)),
				ToIndex:   big.NewInt(int64(j)),
				Share:     s,
			}
		}
	}
	// dealer 同时也是 receiver 时，自存发给自己的份额。
	if role.IsReceiver {
		if s, ok := priv.Shares[role.ReceiverIndex]; ok && s != nil {
			km.reshareShares[role.DealerIndex] = new(big.Int).Set(s)
		}
	}
	km.cacheReshareOutboundLocked(sessionID, deal, sharesToSend)
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_share_build",
		"duration_ns", time.Since(shareBuildStart).Nanoseconds(), "join_key", sessionID,
		"node", km.nodeIndex, "share_count", len(sharesToSend), "status", "ok")
	km.replayResharePendingLocked(sessionID)
	return deal, sharesToSend, nil
}

func (km *KeyManager) OnReshareDeal(sessionID string, deal *chameleon.DKRSDeal) error {
	if deal == nil {
		return errors.New("nil reshare deal")
	}
	_, err := km.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionID, Deal: deal})
	return err
}

func (km *KeyManager) OnReshareShare(sessionID string, share *chameleon.DKRSShare) error {
	if share == nil || share.Share == nil {
		return errors.New("nil reshare share")
	}
	_, err := km.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionID, Share: share})
	return err
}

func (km *KeyManager) OnReshareComplaint(sessionID string, c *chameleon.DKRSComplaint) {
	if c == nil {
		return
	}
	_, _ = km.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionID, Complaint: c})
}

func (km *KeyManager) OnReshareResponse(sessionID string, resp *chameleon.DKRSResponse) {
	if resp == nil || resp.Share == nil {
		return
	}
	_, _ = km.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionID, Response: resp})
}

func (km *KeyManager) OnReshareVerifyKey(sessionID string, vk *chameleon.DKRSVerifyKey) {
	if vk == nil || vk.Index == nil {
		return
	}
	_, _ = km.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionID, VerifyKey: vk})
}

func (km *KeyManager) ReshareHasEnoughData() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.resharePending || km.localKey == nil || !km.reshareRole.IsReceiver {
		return false
	}
	qual := km.buildReshareQualLocked()
	if len(qual) < km.localKey.Threshold+1 {
		return false
	}
	return true
}

// MarkReshareInputsReady records the first point at which this receiver has
// enough deterministic-QUAL inputs for verification/extraction.
func (km *KeyManager) MarkReshareInputsReady(sessionID string) (bool, int, int) {
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.resharePending || km.reshareSessionID != sessionID || km.localKey == nil || !km.reshareRole.IsReceiver {
		return false, 0, 0
	}
	required := km.localKey.Threshold + 1
	qual := km.buildReshareQualLocked()
	if len(qual) < required || km.reshareInputsReadyLogged {
		return false, len(qual), required
	}
	km.reshareInputsReadyLogged = true
	return true, len(qual), required
}

// MarkReshareQualReady records the first point at which extraction is allowed.
func (km *KeyManager) MarkReshareQualReady(sessionID string) (bool, int) {
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.resharePending || km.reshareSessionID != sessionID || km.localKey == nil || !km.reshareRole.IsReceiver {
		return false, 0
	}
	qual := km.buildReshareQualLocked()
	if len(qual) < km.localKey.Threshold+1 || km.reshareQualReadyLogged {
		return false, len(qual)
	}
	km.reshareQualReadyLogged = true
	return true, len(qual)
}

// ReshareVerifySharesOnly verifies the deterministic-QUAL shares without
// constructing or broadcasting complaints. It returns counts for timing logs.
func (km *KeyManager) ReshareVerifySharesOnly() (verified, invalid, parallelism int, started bool) {
	km.mu.Lock()
	if !km.resharePending || km.publicParams == nil || !km.reshareRole.IsReceiver || km.reshareVerifyStarted {
		km.mu.Unlock()
		return 0, 0, 0, false
	}
	qual := km.buildReshareQualLocked()
	if len(qual) < km.localKey.Threshold+1 {
		km.mu.Unlock()
		return 0, 0, 0, false
	}
	items := make([]verifyDKRSShareItem, 0, len(qual))
	for _, dealer := range qual {
		deal := km.reshareDeals[dealer]
		share := km.reshareShares[dealer]
		if deal == nil || share == nil {
			continue
		}
		items = append(items, verifyDKRSShareItem{
			dealer: dealer,
			deal:   cloneDKRSDeal(deal),
			share:  new(big.Int).Set(share),
		})
	}
	pp := km.publicParams
	receiverIndex := km.reshareRole.ReceiverIndex
	sessionID := km.reshareSessionID
	inputRevision := km.reshareInputRevision
	km.reshareVerifyStarted = true
	km.mu.Unlock()

	badDealers := verifyDKRSSharesSerial(pp, receiverIndex, items)
	km.mu.Lock()
	if km.resharePending && km.reshareSessionID == sessionID && km.reshareInputRevision == inputRevision {
		km.reshareVerified = len(badDealers) == 0 && len(items) == len(qual)
	}
	km.mu.Unlock()
	parallelism = 1
	return len(items), len(badDealers), parallelism, true
}

// ReshareIsReceiver 报告本节点在当前活跃会话中是否为 Receiver（新委员会成员）。
// 供 advanceReshare 显式门控：dealer-only 节点只负责造 Deal/答 InputRequest，
// 不参与验证/投诉/提取。
func (km *KeyManager) ReshareIsReceiver() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.resharePending && km.reshareRole.IsReceiver
}

func (km *KeyManager) ReshareVerifyAndComplain() ([]chameleon.DKRSComplaint, error) {
	km.mu.RLock()
	if !km.resharePending || km.publicParams == nil {
		km.mu.RUnlock()
		return nil, errors.New("reshare not pending or no public params")
	}
	if km.reshareComplaintsSent || !km.reshareRole.IsReceiver {
		km.mu.RUnlock()
		return nil, nil
	}
	sessionID := km.reshareSessionID
	myNewIndex := km.reshareRole.ReceiverIndex
	pp := km.publicParams
	items := make([]verifyDKRSShareItem, 0, len(km.reshareConfig.OldCommittee))
	complaints := make([]chameleon.DKRSComplaint, 0)
	for _, dealer := range km.reshareConfig.OldCommittee {
		deal := km.reshareDeals[dealer]
		share := km.reshareShares[dealer]
		if deal == nil || share == nil {
			continue
		}
		items = append(items, verifyDKRSShareItem{
			dealer: dealer,
			deal:   cloneDKRSDeal(deal),
			share:  new(big.Int).Set(share),
		})
	}
	km.mu.RUnlock()

	badDealers := verifyDKRSSharesSerial(pp, myNewIndex, items)
	for _, dealer := range badDealers {
		complaints = append(complaints, chameleon.DKRSComplaint{
			ComplainantIndex: big.NewInt(int64(myNewIndex)),
			DealerIndex:      big.NewInt(int64(dealer)),
		})
	}

	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.resharePending || km.reshareSessionID != sessionID || km.reshareComplaintsSent {
		return nil, nil
	}
	km.reshareComplaintsSent = true
	return complaints, nil
}

func verifyDKRSSharesSerial(pp *chameleon.PublicParameters, receiverIndex int, items []verifyDKRSShareItem) []int {
	if len(items) == 0 {
		return nil
	}
	out := make([]int, 0)
	for _, item := range items {
		ok, err := chameleon.VerifyDKRSShare(pp, item.deal, item.share, receiverIndex)
		if err != nil || !ok {
			out = append(out, item.dealer)
		}
	}
	return out
}

type verifyDKRSShareItem struct {
	dealer int
	deal   *chameleon.DKRSDeal
	share  *big.Int
}

func (km *KeyManager) reshareRequiredQualLocked() []int {
	if km.localKey == nil {
		return nil
	}
	required := km.localKey.Threshold + 1
	qual := make([]int, 0, required)
	for _, i := range km.reshareConfig.OldCommittee {
		if i < 1 || i > km.localKey.NumParties {
			continue
		}
		qual = append(qual, i)
		if len(qual) >= required {
			break
		}
	}
	return qual
}

func (km *KeyManager) buildReshareQualLocked() []int {
	requiredQual := km.reshareRequiredQualLocked()
	if len(requiredQual) == 0 {
		return nil
	}
	// Every receiver must extract from the same dealer set. Selecting any locally
	// available t+1 dealers lets different receivers derive shares from different
	// resharing polynomials: the global public key stays unchanged, but the new
	// shares can no longer be interpolated together during redaction.
	out := make([]int, 0, len(requiredQual))
	for _, i := range requiredQual {
		if km.reshareDeals == nil || km.reshareDeals[i] == nil {
			continue
		}
		if km.reshareRole.IsReceiver && (km.reshareShares == nil || km.reshareShares[i] == nil) {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (km *KeyManager) ReshareLocalDealerRequired() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.resharePending || km.localKey == nil {
		return true
	}
	if !km.reshareRole.IsDealer {
		return false
	}
	for _, dealer := range km.reshareRequiredQualLocked() {
		if dealer == km.reshareRole.DealerIndex {
			return true
		}
	}
	return false
}

func (km *KeyManager) ReshareCanExtract() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if !km.resharePending || km.localKey == nil || !km.reshareRole.IsReceiver || !km.reshareVerified {
		return false
	}
	qual := km.buildReshareQualLocked()
	if len(qual) < km.localKey.Threshold+1 {
		return false
	}
	for _, i := range qual {
		if km.reshareDeals[i] == nil || km.reshareShares[i] == nil {
			return false
		}
	}
	return true
}

func (km *KeyManager) ReshareExtractAndApply() error {
	start := time.Now()
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.resharePending || km.localKey == nil || km.publicParams == nil {
		return errors.New("reshare not pending or key not initialized")
	}
	// 提前缓存本次会话 ID，供打点关联（提取成功后 sessionID 会被清空）。
	sid := km.reshareSessionID
	if !km.reshareRole.IsReceiver {
		return errors.New("node is not in new committee")
	}
	if !km.reshareVerified {
		return errors.New("reshare shares have not been successfully verified")
	}
	qual := km.buildReshareQualLocked()
	if len(qual) < km.localKey.Threshold+1 {
		return errors.New("qualified old committee size is less than old threshold+1")
	}
	deals := make(map[int]*chameleon.DKRSDeal)
	sharesToMe := make(map[int]*big.Int)
	for _, i := range qual {
		deals[i] = km.reshareDeals[i]
		sharesToMe[i] = km.reshareShares[i]
	}
	log.Info("Reshare extracting with deterministic qualifier",
		"session", sid, "qual", qual, "receiver", km.reshareRole.ReceiverIndex)

	extractCryptoStart := time.Now()
	newKey, err := chameleon.DKRSExtract(km.publicParams, km.localKey, km.reshareRole.ReceiverIndex, qual, deals, sharesToMe)
	if err != nil {
		return err
	}
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_extract_crypto",
		"duration_ns", time.Since(extractCryptoStart).Nanoseconds(), "join_key", sid,
		"node", km.nodeIndex, "qual_count", len(qual), "status", "ok")
	if !chameleon.SameGlobalPublicKey(km.localKey, newKey) {
		return errors.New("DKRS changed the NITCH global public key")
	}

	applyStart := time.Now()
	km.localKey = newKey
	km.threshold = newKey.Threshold
	km.numParties = newKey.NumParties
	km.committeeSize = len(km.reshareConfig.NewCommittee)
	km.isCommitteeMember = km.reshareRole.IsReceiver
	km.registerPeerKeyLocked(peerKeyAddressForIndex(newKey.Index), newKey)
	chameleon.SetSystemNITCHKeyFromKey(newKey)

	// Phase 3A：成功 extraction（含 SameGlobalPublicKey 检查）后切换运行时委员会。
	// 描述符模式下把 pending 激活为 active，使 nodeIndex/isCommitteeMember/threshold
	// 立即反映新代描述符；非描述符模式（手工回归路径）保持原有数值语义。
	if km.descriptorModeLocked() && km.pendingCommittee != nil {
		newActive := km.pendingCommittee
		km.pendingCommittee = nil
		km.setActiveCommitteeLocked(newActive)
		km.threshold = newKey.Threshold
		km.numParties = newKey.NumParties
		log.Info("Active committee switched after reshare extraction",
			"session", sid, "epoch", newActive.Epoch, "committeeID", newActive.CommitteeID)
	}

	km.resharePending = false
	km.reshareSessionID = ""
	km.reshareConfig = ReshareSessionConfig{}
	km.reshareRole = ReshareRole{}
	km.reshareMyDeal = nil
	km.reshareMyPrivate = nil
	km.reshareDeals = nil
	km.reshareShares = nil
	km.reshareComplaints = nil
	km.reshareComplaintsSent = false
	km.reshareVerifyKeys = nil
	// 性能打点：标记本轮 rotation 完成（供 eth_reshareStatus 轮询）。
	km.reshareExtracted = true
	km.reshareLastSession = sid
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_apply",
		"duration_ns", time.Since(applyStart).Nanoseconds(), "join_key", sid,
		"node", km.nodeIndex, "status", "ok")
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_extracted",
		"duration_ns", 0, "join_key", sid, "node", km.nodeIndex, "status", "ok")
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "reshare_extract_apply",
		"duration_ns", time.Since(start).Nanoseconds(), "join_key", sid,
		"node", km.nodeIndex, "status", "ok")
	return nil
}

// ========== 性能打点 / 完成轮询只读状态 ==========

// recordRedactionApplied 记录本节点成功应用了一次编辑（updateBlockInfo 成功）。
// reqID 约定为 "blockNum-Index"，与脚本侧 PERF_ANCHOR 的 join_key 一致。
func (km *KeyManager) recordRedactionApplied(reqID string) {
	if km == nil || reqID == "" {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if km.redactionsApplied == nil {
		km.redactionsApplied = make(map[string]bool)
	}
	km.redactionsApplied[reqID] = true
}

func (km *KeyManager) hasRedactionApplied(reqID string) bool {
	if km == nil || reqID == "" {
		return false
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	return km.redactionsApplied != nil && km.redactionsApplied[reqID]
}

// DkgStatus 返回只读 DistKG 状态快照（供 eth_dkgStatus 轮询 system_init 完成）。
func (km *KeyManager) DkgStatus() map[string]interface{} {
	out := map[string]interface{}{}
	if km == nil {
		return out
	}
	// DistKGCanExtract 自带锁，先在外层调用，避免与下面的读锁嵌套死锁。
	out["canExtract"] = km.DistKGCanExtract()
	km.mu.RLock()
	defer km.mu.RUnlock()
	out["pending"] = km.distKGPending
	out["extracted"] = km.distKGExtracted
	out["publicKeyReady"] = chameleon.SystemPublicKeyReady()
	deals, shares := 0, 0
	if km.distKGDeals != nil {
		deals = len(km.distKGDeals)
	}
	if km.distKGShares != nil {
		shares = len(km.distKGShares)
	}
	out["dealsRecv"] = deals
	out["sharesRecv"] = shares
	out["committeeSize"] = km.committeeSize
	out["threshold"] = km.threshold
	return out
}

// ReshareStatus 返回只读 reshare 状态快照（供 eth_reshareStatus 轮询每轮 rotation 完成）。
func (km *KeyManager) ReshareStatus() map[string]interface{} {
	out := map[string]interface{}{}
	if km == nil {
		return out
	}
	out["canExtract"] = km.ReshareCanExtract()
	km.mu.RLock()
	defer km.mu.RUnlock()

	thresholdT := km.threshold
	oldCommitteeSize := 0
	newCommitteeSize := km.committeeSize
	if km.resharePending {
		thresholdT = km.reshareConfig.NewThreshold
		oldCommitteeSize = len(km.reshareConfig.OldCommittee)
		newCommitteeSize = len(km.reshareConfig.NewCommittee)
	}
	requiredShares := 0
	if thresholdT >= 0 {
		requiredShares = thresholdT + 1
	}
	dealsRecv, sharesRecv := 0, 0
	for _, deal := range km.reshareDeals {
		if deal != nil {
			dealsRecv++
		}
	}
	for _, share := range km.reshareShares {
		if share != nil {
			sharesRecv++
		}
	}
	missingDealCount, missingShareCount := 0, 0
	if km.resharePending {
		for _, dealer := range km.reshareConfig.OldCommittee {
			if km.reshareDeals == nil || km.reshareDeals[dealer] == nil {
				missingDealCount++
			}
			if km.reshareRole.IsReceiver && (km.reshareShares == nil || km.reshareShares[dealer] == nil) {
				missingShareCount++
			}
		}
	}

	out["pending"] = km.resharePending
	out["extracted"] = km.reshareExtracted
	out["sessionID"] = km.reshareSessionID
	out["lastSession"] = km.reshareLastSession
	out["thresholdT"] = thresholdT
	out["requiredShares"] = requiredShares
	out["oldCommitteeSize"] = oldCommitteeSize
	out["newCommitteeSize"] = newCommitteeSize
	out["myNewIndex"] = km.reshareRole.ReceiverIndex
	out["isDealer"] = km.reshareRole.IsDealer
	out["isReceiver"] = km.reshareRole.IsReceiver
	out["dealerIndex"] = km.reshareRole.DealerIndex
	out["dealsRecv"] = dealsRecv
	out["sharesRecv"] = sharesRecv
	out["missingDealCount"] = missingDealCount
	out["missingShareCount"] = missingShareCount
	out["complaintsRecv"] = len(km.reshareComplaints)
	out["complaintsSent"] = km.reshareComplaintsSent
	out["verifyKeysRecv"] = len(km.reshareVerifyKeys)
	out["requestedInputs"] = len(km.reshareRequestedInputs)

	// Phase 3B: 扩展 election / rotation 只读字段
	var epoch uint64
	var boundary uint64
	tau := km.lastTau
	var committeeID common.Hash
	phase := "idle"

	if km.pendingCommittee != nil {
		epoch = km.pendingCommittee.Epoch
		boundary = km.pendingCommittee.ActivateAt
		committeeID = km.pendingCommittee.CommitteeID
		if km.resharePending {
			phase = "reshare"
		} else {
			phase = "pending"
		}
	} else if km.activeCommittee != nil {
		epoch = km.activeCommittee.Epoch
		boundary = km.activeCommittee.ActivateAt
		committeeID = km.activeCommittee.CommitteeID
		phase = "active"
	} else if km.lastElectionEpoch > 0 {
		epoch = km.lastElectionEpoch
		boundary = km.lastBoundary
	}
	if km.lastBoundary > 0 {
		boundary = km.lastBoundary
	}

	out["epoch"] = epoch
	out["boundary"] = boundary
	out["currentBoundary"] = boundary
	out["previousBoundary"] = km.lastPreviousBoundary
	out["tau"] = tau.Hex()
	if (km.lastNextTau != common.Hash{}) {
		out["nextTau"] = km.lastNextTau.Hex()
	} else {
		out["nextTau"] = tau.Hex()
	}
	out["committeeID"] = committeeID.Hex()
	out["solutionCount"] = km.lastSolutionCount
	out["solution_count"] = km.lastSolutionCount
	out["validSolutionCount"] = km.lastSolutionCount
	out["phase"] = phase
	return out
}

// ElectionStatus 返回只读选举与轮换状态快照（RPC: eth_electionStatus）。
func (km *KeyManager) ElectionStatus() map[string]interface{} {
	out := map[string]interface{}{}
	if km == nil {
		return out
	}
	km.mu.RLock()
	defer km.mu.RUnlock()

	var epoch uint64
	var boundary uint64
	tau := km.lastTau
	var committeeID common.Hash
	pending := km.pendingCommittee != nil
	phase := "idle"

	if km.pendingCommittee != nil {
		epoch = km.pendingCommittee.Epoch
		boundary = km.pendingCommittee.ActivateAt
		committeeID = km.pendingCommittee.CommitteeID
		if km.resharePending {
			phase = "reshare"
		} else {
			phase = "pending"
		}
	} else if km.activeCommittee != nil {
		epoch = km.activeCommittee.Epoch
		boundary = km.activeCommittee.ActivateAt
		committeeID = km.activeCommittee.CommitteeID
		phase = "active"
	} else if km.lastElectionEpoch > 0 {
		epoch = km.lastElectionEpoch
		boundary = km.lastBoundary
	}
	if km.lastBoundary > 0 {
		boundary = km.lastBoundary
	}

	out["epoch"] = epoch
	out["boundary"] = boundary
	out["currentBoundary"] = boundary
	out["previousBoundary"] = km.lastPreviousBoundary
	out["tau"] = tau.Hex()
	if (km.lastNextTau != common.Hash{}) {
		out["nextTau"] = km.lastNextTau.Hex()
	} else {
		out["nextTau"] = tau.Hex()
	}
	out["committeeID"] = committeeID.Hex()
	out["pending"] = pending
	out["solutionCount"] = km.lastSolutionCount
	out["solution_count"] = km.lastSolutionCount
	out["validSolutionCount"] = km.lastSolutionCount
	out["phase"] = phase
	out["pendingReshareSession"] = km.reshareSessionID

	if km.activeCommittee != nil {
		out["activeEpoch"] = km.activeCommittee.Epoch
		out["activeCommitteeID"] = km.activeCommittee.CommitteeID.Hex()
		out["activeSize"] = len(km.activeCommittee.Members)
		out["activeCommittee"] = km.activeCommittee.Members
	}
	if km.pendingCommittee != nil {
		out["pendingEpoch"] = km.pendingCommittee.Epoch
		out["pendingCommitteeID"] = km.pendingCommittee.CommitteeID.Hex()
		out["pendingSize"] = len(km.pendingCommittee.Members)
		out["selectedCommittee"] = km.pendingCommittee.Members
	}
	return out
}

// ReshareDealerCanFinish 报告 dealer-only 节点是否满足完成/收敛条件（收齐新委员会足够多的 VerifyKey）
func (km *KeyManager) ReshareDealerCanFinish() bool {
	if km == nil {
		return false
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.reshareDealerCanFinishLocked()
}

func (km *KeyManager) reshareDealerCanFinishLocked() bool {
	if !km.resharePending || !km.reshareRole.IsDealer || km.reshareRole.IsReceiver {
		return false
	}
	required := km.reshareConfig.NewThreshold + 1
	if km.pendingCommittee != nil {
		required = km.pendingCommittee.Threshold + 1
	}
	if required <= 0 {
		required = 1
	}
	return len(km.reshareVerifyKeys) >= required
}

func (km *KeyManager) finishDealerSessionLocked(sessionID string) {
	sid := km.reshareSessionID
	if sid == "" {
		sid = sessionID
	}
	km.resharePending = false
	km.reshareSessionID = ""
	km.reshareDealerFinished = true
	km.reshareLastSession = sid
	km.reshareRole = ReshareRole{}
	km.reshareConfig = ReshareSessionConfig{}
	km.reshareMyDeal = nil
	km.reshareMyPrivate = nil
	km.reshareDeals = nil
	km.reshareShares = nil
	km.reshareComplaints = nil
	km.reshareComplaintsSent = false
	km.reshareVerifyKeys = nil

	// 在 boundary handoff 更新运行时 active descriptor（如果实验语义允许）
	if km.pendingCommittee != nil {
		newActive := km.pendingCommittee
		km.pendingCommittee = nil
		km.setActiveCommitteeLocked(newActive)
		log.Info("Active committee switched after dealer-only handoff",
			"session", sid, "epoch", newActive.Epoch, "committeeID", newActive.CommitteeID)
	}
}

// FinishDealerSession 显式完成 dealer-only 会话并收敛状态与活跃描述符
func (km *KeyManager) FinishDealerSession(sessionID string) error {
	if km == nil {
		return errors.New("nil KeyManager")
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if !km.resharePending || km.reshareSessionID != sessionID {
		return nil
	}
	if !km.reshareRole.IsDealer || km.reshareRole.IsReceiver {
		return errors.New("not a dealer-only session")
	}
	km.finishDealerSessionLocked(sessionID)
	return nil
}

func (km *KeyManager) ReshareSessionFinished(sessionID string) bool {
	if km == nil || sessionID == "" {
		return true
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	sessionID = km.resolveReshareSessionIDLocked(sessionID)
	if km.reshareExtracted && km.reshareLastSession == sessionID {
		return true
	}
	if km.reshareDealerFinished && km.reshareLastSession == sessionID {
		return true
	}
	return !km.resharePending || km.reshareSessionID != sessionID
}

// RedactionStatus 返回本节点已应用编辑的只读快照（供 eth_redactionStatus 轮询编辑可查）。
func (km *KeyManager) RedactionStatus() map[string]interface{} {
	out := map[string]interface{}{}
	if km == nil {
		return out
	}
	km.mu.RLock()
	defer km.mu.RUnlock()
	applied := make([]string, 0, len(km.redactionsApplied))
	last := ""
	for k := range km.redactionsApplied {
		applied = append(applied, k)
		if k > last {
			last = k
		}
	}
	out["appliedCount"] = len(applied)
	out["appliedReqIDs"] = applied
	out["lastReqID"] = last
	return out
}

// ResetRedactionStatus 清空已应用编辑记录（每轮 perf run 开始前由编排层可选调用）。
func (km *KeyManager) ResetRedactionStatus() {
	if km == nil {
		return
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	km.redactionsApplied = nil
}
