package eth

import (
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	ctypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestNodeIDEnvironmentOverrideAndStability 测试 NITCH_NODE_ID 环境变量对 localNodeID 的稳定覆盖与向后兼容。
func TestNodeIDEnvironmentOverrideAndStability(t *testing.T) {
	km := &KeyManager{}
	if km.GetLocalNodeID() != "" {
		t.Fatalf("expected empty localNodeID by default, got %s", km.GetLocalNodeID())
	}

	// 设置 NITCH_NODE_ID 环境变量
	testEnvID := "node-env-stable-123"
	os.Setenv("NITCH_NODE_ID", testEnvID)
	defer os.Unsetenv("NITCH_NODE_ID")

	if km.GetLocalNodeID() != NodeID(testEnvID) {
		t.Fatalf("expected localNodeID from env %s, got %s", testEnvID, km.GetLocalNodeID())
	}

	// 显式 SetLocalNodeID 优先级高于环境变量
	explicitID := NodeID("explicit-node-456")
	km.SetLocalNodeID(explicitID)
	if km.GetLocalNodeID() != explicitID {
		t.Fatalf("expected explicit localNodeID %s, got %s", explicitID, km.GetLocalNodeID())
	}

	// 清除显式设置后恢复环境变量
	km.SetLocalNodeID("")
	if km.GetLocalNodeID() != NodeID(testEnvID) {
		t.Fatalf("expected fallback to env %s, got %s", testEnvID, km.GetLocalNodeID())
	}
}

// TestPrepareReshareReceiverWithRealPublicParams 测试普通节点使用真实 public params 加入，
// 禁止假 secret share，且在无可用公钥时返回明确阻塞错误。
func TestPrepareReshareReceiverWithRealPublicParams(t *testing.T) {
	keys := testReshareKeys(t)
	pp := keys[0].PP

	activeDesc, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-4"}, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}
	pendingDesc, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-5"}, 1, 2, 200)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc pending: %v", err)
	}

	// 情况 1: 新节点 node-5 具备真实 public params 与 GlobalPK，但无 SecretShare
	km := &KeyManager{
		localNodeID:      "node-5",
		publicParams:     pp,
		activeCommittee:  activeDesc,
		pendingCommittee: pendingDesc,
		localKey: &chameleon.NITCHKey{
			PP:         pp,
			GlobalPK:   keys[0].GlobalPK,
			GlobalPKG1: keys[0].GlobalPKG1,
			Threshold:  1,
			NumParties: 4,
		},
	}

	if err := km.PrepareReshareReceiver(); err != nil {
		t.Fatalf("PrepareReshareReceiver with real public params failed: %v", err)
	}
	if km.localKey.SecretShare != nil {
		t.Fatalf("security violation: secret share must be nil before extraction, got %v", km.localKey.SecretShare)
	}

	// 情况 2: 新节点完全缺失公钥与观察者通道，必须返回明确阻塞错误 ErrReceiverMissingPublicKey
	prevSource := systemReshareKeySource
	systemReshareKeySource = func() (*chameleon.NITCHKey, error) {
		return nil, ErrReceiverMissingPublicKey
	}
	defer func() { systemReshareKeySource = prevSource }()

	kmMissing := &KeyManager{
		localNodeID:      "node-5",
		activeCommittee:  activeDesc,
		pendingCommittee: pendingDesc,
	}
	if err := kmMissing.PrepareReshareReceiver(); err != ErrReceiverMissingPublicKey {
		t.Fatalf("expected ErrReceiverMissingPublicKey, got: %v", err)
	}
}

// TestDealerOnlySessionConvergenceAndActiveHandoff 测试 dealer-only 节点的重试循环收敛与 boundary handoff 更新
func TestDealerOnlySessionConvergenceAndActiveHandoff(t *testing.T) {
	keys := testReshareKeys(t)
	pp := keys[0].PP

	// 旧委员会 4 人 {node-1, node-2, node-3, node-4}, 新委员会 {node-2, node-3, node-4, node-5}
	// node-1 为 dealer-only (老成员，新代被轮换出去)
	activeDesc, _ := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-4"}, 1, 1, 100)
	pendingDesc, _ := BootstrapActiveCommitteeDesc([]NodeID{"node-2", "node-3", "node-4", "node-5"}, 1, 2, 200)

	km := &KeyManager{
		localNodeID:       "node-1",
		nodeIndex:         0,
		isCommitteeMember: true,
		publicParams:      pp,
		localKey:          keys[0],
		activeCommittee:   activeDesc,
		pendingCommittee:  pendingDesc,
	}

	sessionID := "test-session-dealer-only"
	cfg := ReshareSessionConfig{
		OldCommittee: []int{1, 2, 3, 4},
		NewCommittee: []int{2, 3, 4, 5},
		NewThreshold: 1,
	}
	deal, shares, err := km.StartReshare(sessionID, cfg)
	if err != nil {
		t.Fatalf("StartReshare failed: %v", err)
	}
	if deal == nil || len(shares) == 0 {
		t.Fatalf("expected dealer to produce deal and shares")
	}

	// 初始状态下会话未结束
	if km.ReshareSessionFinished(sessionID) {
		t.Fatalf("expected session not finished initially")
	}

	// 模拟接收到新委员会成员发来的 VerifyKey
	vk1 := chameleon.NewDKRSVerifyKey(keys[1])
	vk2 := chameleon.NewDKRSVerifyKey(keys[2])

	// 第一张验证密钥到达 (门限=1，要求 threshold+1 = 2 张)
	km.mu.Lock()
	km.applyReshareVerifyKeyLocked(vk1)
	km.mu.Unlock()

	if km.ReshareSessionFinished(sessionID) {
		t.Fatalf("expected session not finished with only 1 verify key")
	}

	// 第二张验证密钥到达，满足 threshold+1，应自动收敛结束 dealer 会话并完成 active handoff
	km.mu.Lock()
	km.applyReshareVerifyKeyLocked(vk2)
	km.mu.Unlock()

	if !km.ReshareSessionFinished(sessionID) {
		t.Fatalf("expected dealer session to be finished after threshold+1 verify keys")
	}
	// 验证 active 描述符已交接为新委员会，且本节点退为非委员会成员
	active := km.ActiveCommittee()
	if active == nil || active.Epoch != 2 {
		t.Fatalf("expected active committee epoch 2 after handoff, got %+v", active)
	}
	if km.IsCommitteeMember() {
		t.Fatalf("expected dealer-only node to step down from committee membership")
	}
}

// TestDynamicRoutingDoesNotBindRelayPeer 测试动态路由中 relay peer 严禁被误绑定为目标 NodeID，
// 且 active dealer 与 pending receiver 的目标索引不混淆。
func TestDynamicRoutingDoesNotBindRelayPeer(t *testing.T) {
	pm := &ProtocolManager{
		peers:                       newPeerSet(),
		pendingCommitteePeers:       make(map[int]string),
		pendingCommitteeDirectPeers: make(map[int]string),
		activeCommitteePeers:        make(map[int]string),
		activeCommitteeDirectPeers:  make(map[int]string),
		nodeIDToPeerID:              make(map[NodeID]string),
		peerIDToNodeID:              make(map[string]NodeID),
	}

	// 创建模拟 peer
	relayPeerID := "relay-peer-aaa"
	pRelay := &peer{id: relayPeerID}
	pm.peers.Register(pRelay)

	// 构造来自真实 dealer 的 deal 消息，但经由 relayPeer 转发 (msg.Relay.Origin != p.id)
	msgRelayed := ctypes.ReshareMessage{
		SessionID: "sess-1",
		Deal: &chameleon.DKRSDeal{
			DealerIndex: big.NewInt(1),
		},
		Relay: ctypes.RelayMetadata{
			Origin: "real-origin-peer-bbb",
			TTL:    3,
		},
	}

	km := &KeyManager{
		activeCommittee: &CommitteeDescriptor{
			Epoch:   1,
			Members: []NodeID{"dealer-node-1"},
		},
	}
	globalKeyManager = km

	// 处理转发消息
	pm.noteResharePeer(msgRelayed, pRelay)

	// 验证：relayPeer 绝对不能被绑定为 dealer-node-1
	if pm.nodeIDToPeerID["dealer-node-1"] == relayPeerID {
		t.Fatalf("security violation: relay peer %s was mistakenly bound as target NodeID dealer-node-1", relayPeerID)
	}

	// 构造直接来自目标来源的消息 (msg.Relay.Origin == p.id)
	msgDirect := ctypes.ReshareMessage{
		SessionID: "sess-1",
		Deal: &chameleon.DKRSDeal{
			DealerIndex: big.NewInt(1),
		},
		Relay: ctypes.RelayMetadata{
			Origin: relayPeerID,
			TTL:    4,
		},
	}
	pm.noteResharePeer(msgDirect, pRelay)

	// 直接来源允许关联绑定
	if pm.nodeIDToPeerID["dealer-node-1"] != relayPeerID {
		t.Fatalf("expected direct origin peer %s to be bound to dealer-node-1", relayPeerID)
	}

	// 测试 active dealer 与 pending receiver 的目标索引不混淆
	pm.activeCommitteeDirectPeers[1] = "active-peer-1"
	pm.pendingCommitteeDirectPeers[1] = "pending-peer-1"

	pActive := &peer{id: "active-peer-1"}
	pm.peers.Register(pActive)

	pPending := &peer{id: "pending-peer-1"}
	pm.peers.Register(pPending)

	// 会话必须处于进行中：targetPeerForReshare 对 InputRequest 有一道守卫
	// ——会话一旦结束，active 索引已指向下一 epoch，旧 dealer 请求必须
	// fan-out 而非发给现占位的无关节点。真实路径上 InputRequest 总在
	// advanceReshare(sessionID) 内发出，此时 StartReshare 已把会话置为活跃。
	km.mu.Lock()
	km.resharePending = true
	km.reshareSessionID = "sess-1"
	km.mu.Unlock()

	// 发往 DealerIndex=1 的 InputRequest 必须路由到 active-peer-1
	reqMsg := ctypes.ReshareMessage{
		SessionID: "sess-1",
		InputRequest: &ctypes.ReshareInputRequest{
			DealerIndex: big.NewInt(1),
		},
	}
	dealerTarget := pm.targetPeerForReshare(1, reqMsg)
	if dealerTarget == nil || dealerTarget.id != "active-peer-1" {
		t.Fatalf("expected InputRequest to route to active-peer-1, got %v", dealerTarget)
	}

	// 发往 ReceiverIndex=1 的 Share 必须路由到 pending-peer-1
	shareMsg := ctypes.ReshareMessage{
		SessionID: "sess-1",
		Share: &chameleon.DKRSShare{
			ToIndex: big.NewInt(1),
		},
	}
	receiverTarget := pm.targetPeerForReshare(1, shareMsg)
	if receiverTarget == nil || receiverTarget.id != "pending-peer-1" {
		t.Fatalf("expected Share to route to pending-peer-1, got %v", receiverTarget)
	}
}

// TestStatusExposesNextTauAndDesignDocFields 测试 ApplyBoundaryEvent 记录 NextTau，
// 以及 ElectionStatus / ReshareStatus 完整暴露设计文档指定字段。
func TestStatusExposesNextTauAndDesignDocFields(t *testing.T) {
	activeDesc, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2"}, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}
	km := &KeyManager{
		localNodeID:     "node-observer",
		activeCommittee: activeDesc,
	}

	testTau := common.HexToHash("0xaaaa")
	testNextTau := common.HexToHash("0xbbbb")
	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 2
	cfg.Threshold = 1
	electResult := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"node-a", "node-b"})

	ev := &BoundaryEvent{
		Epoch:            1,
		FinalizedEpoch:   1,
		TargetEpoch:      2,
		PreviousBoundary: 100,
		CurrentBoundary:  200,
		Tau:              testTau,
		NextTau:          testNextTau,
		ValidSolutions:   []Solution{},
		ElectionResult:   electResult,
	}

	_, _, _, err = km.ApplyBoundaryEvent(ev)
	// observer 节点在两代之外返回 ErrNodeOutsideRotation，但状态已采纳
	if err != nil && !errors.Is(err, ErrNodeOutsideRotation) {
		t.Fatalf("ApplyBoundaryEvent unexpected error: %v", err)
	}

	status := km.ElectionStatus()

	// 验证 NextTau 被记录为新 epoch 的 tau
	if status["tau"] != testNextTau.Hex() {
		t.Fatalf("expected status[tau] == %s (NextTau), got %v", testNextTau.Hex(), status["tau"])
	}
	if status["nextTau"] != testNextTau.Hex() {
		t.Fatalf("expected status[nextTau] == %s, got %v", testNextTau.Hex(), status["nextTau"])
	}

	// 验证设计文档规定的字段均存在且正确
	if status["currentBoundary"] != uint64(200) {
		t.Fatalf("expected currentBoundary 200, got %v", status["currentBoundary"])
	}
	if status["previousBoundary"] != uint64(100) {
		t.Fatalf("expected previousBoundary 100, got %v", status["previousBoundary"])
	}
	if status["epoch"] != uint64(2) {
		t.Fatalf("expected epoch 2, got %v", status["epoch"])
	}
	if status["selectedCommittee"] == nil {
		t.Fatalf("expected selectedCommittee to be populated")
	}
}

// TestObserverBoundaryPromotion 测试普通观察者在 boundary election event 后将 pending 确定性推进为 active，
// 且不启动/等待 reshare，不获取 secret share，不变成 committee member，并同步更新路由。
func TestObserverBoundaryPromotion(t *testing.T) {
	// 1. 构造一个旧活跃委员会 (Epoch 1, 4 members)
	activeDesc, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-4"}, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}

	// 2. 观察者节点：outside-observer-99 既不在 Epoch 1，也不在后续 Epoch 2
	observerKM := &KeyManager{
		localNodeID:       "outside-observer-99",
		isCommitteeMember: false,
		nodeIndex:         -1,
		peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
		pendingProposals:  make(map[string]*ProposalShares),
	}
	if err := observerKM.BootstrapActiveCommittee(activeDesc.Members, activeDesc.Threshold, activeDesc.Epoch, activeDesc.ActivateAt); err != nil {
		t.Fatalf("BootstrapActiveCommittee observer: %v", err)
	}

	// 确认观察者初始状态
	if observerKM.IsCommitteeMember() {
		t.Fatal("observer must not be committee member initially")
	}
	if observerKM.GetLocalKey() != nil {
		t.Fatal("observer localKey must be nil initially")
	}
	if observerKM.ActiveCommittee().Epoch != 1 {
		t.Fatalf("expected active epoch 1, got %d", observerKM.ActiveCommittee().Epoch)
	}

	// 3. 构建 Epoch 2 的 ElectionResult (4 members: node-1, node-2, node-3, node-5)
	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	resultEpoch2 := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"node-1", "node-2", "node-3", "node-5"})
	expectedEpoch2Desc := resultEpoch2.Descriptor.Clone()
	expectedEpoch2Desc.Members = sortDescriptorMembers(&expectedEpoch2Desc)

	evEpoch2 := &BoundaryEvent{
		Epoch:            1,
		FinalizedEpoch:   1,
		TargetEpoch:      2,
		PreviousBoundary: 100,
		CurrentBoundary:  200,
		Tau:              DeriveTau(common.HexToHash("0x1111"), 1),
		ElectionResult:   resultEpoch2,
	}

	// 4. 设置 globalKeyManager 以便 NewReshareBoundaryHandler 使用
	prevKM := globalKeyManager
	globalKeyManager = observerKM
	defer func() { globalKeyManager = prevKM }()

	// 构造 ProtocolManager 模拟路由更新
	pm := &ProtocolManager{
		activeCommitteePeers:        make(map[int]string),
		activeCommitteeDirectPeers:  make(map[int]string),
		pendingCommitteePeers:       make(map[int]string),
		pendingCommitteeDirectPeers: make(map[int]string),
	}
	handler := NewReshareBoundaryHandler(pm)

	// 5. 执行 handler 处理 boundary 事件
	if err := handler(evEpoch2); err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}

	// 6. 验证：pending 描述符已确定性推进为 active 描述符
	active := observerKM.ActiveCommittee()
	if active == nil {
		t.Fatal("active committee must not be nil after promotion")
	}
	if active.Epoch != 2 {
		t.Fatalf("expected active committee epoch 2, got %d", active.Epoch)
	}
	if active.CommitteeID != expectedEpoch2Desc.CommitteeID {
		t.Fatalf("expected active committeeID %s, got %s", expectedEpoch2Desc.CommitteeID, active.CommitteeID)
	}
	if observerKM.PendingCommittee() != nil {
		t.Fatal("pending committee must be cleared after activation")
	}

	// 7. 验证安全约束：不假设 secret share，不激活本地 NITCH key，不把 observer 变成 committee
	if observerKM.IsCommitteeMember() {
		t.Fatal("observer must NOT become a committee member")
	}
	if observerKM.GetLocalKey() != nil && observerKM.GetLocalKey().SecretShare != nil {
		t.Fatal("observer must NOT have secret share")
	}

	// 8. 验证 route-visible state 已重置 pending 路由
	if len(pm.pendingCommitteePeers) != 0 {
		t.Fatal("pending committee peers must be cleared")
	}
}

// TestNextEpochOldCommittee 验证普通观察者在 epoch N 推进 active 描述符后，
// 在 epoch N+1 boundary election 中能正确从 active descriptor 得到 OldCommittee。
func TestNextEpochOldCommittee(t *testing.T) {
	// 1. 初始化 Epoch 1
	activeDesc1, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-4"}, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}

	observerKM := &KeyManager{
		localNodeID:       "node-candidate-observer",
		isCommitteeMember: false,
		nodeIndex:         -1,
		peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
		pendingProposals:  make(map[string]*ProposalShares),
	}
	if err := observerKM.BootstrapActiveCommittee(activeDesc1.Members, activeDesc1.Threshold, activeDesc1.Epoch, activeDesc1.ActivateAt); err != nil {
		t.Fatalf("BootstrapActiveCommittee: %v", err)
	}

	// 2. Epoch 2 选举产生：members = [node-1, node-2, node-3, node-5] (node-candidate-observer 仍是观察者)
	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	resultEpoch2 := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"node-1", "node-2", "node-3", "node-5"})
	expectedEpoch2Desc := resultEpoch2.Descriptor.Clone()
	expectedEpoch2Desc.Members = sortDescriptorMembers(&expectedEpoch2Desc)

	evEpoch2 := &BoundaryEvent{
		Epoch:            1,
		FinalizedEpoch:   1,
		TargetEpoch:      2,
		PreviousBoundary: 100,
		CurrentBoundary:  200,
		Tau:              DeriveTau(common.HexToHash("0x1111"), 1),
		ElectionResult:   resultEpoch2,
	}

	prevKM := globalKeyManager
	globalKeyManager = observerKM
	defer func() { globalKeyManager = prevKM }()

	handler := NewReshareBoundaryHandler(nil)
	if err := handler(evEpoch2); err != nil {
		t.Fatalf("Epoch 2 boundary handler failed: %v", err)
	}

	// 确认观察者 active 已升为 Epoch 2
	if observerKM.ActiveCommittee().Epoch != 2 {
		t.Fatalf("expected observer active epoch 2, got %d", observerKM.ActiveCommittee().Epoch)
	}

	// 3. Epoch 3 选举产生：此时 node-candidate-observer 被选中加入新委员会！
	// members = [node-2, node-3, node-5, node-candidate-observer]
	resultEpoch3 := rotationElectedResult(t, cfg, 3, 300, []CandidateID{
		"node-2", "node-3", "node-5", CandidateID(observerKM.localNodeID),
	})
	expectedEpoch3Desc := resultEpoch3.Descriptor.Clone()
	expectedEpoch3Desc.Members = sortDescriptorMembers(&expectedEpoch3Desc)

	// 采纳 Epoch 3 选举结果
	if err := observerKM.AdoptElectionResult(resultEpoch3); err != nil {
		t.Fatalf("AdoptElectionResult Epoch 3 failed: %v", err)
	}

	// 4. 基于当前状态构建 ResharePlan
	plan, err := observerKM.BuildResharePlanFromState()
	if err != nil {
		t.Fatalf("BuildResharePlanFromState failed: %v", err)
	}

	// 5. 核心验证：下一 epoch 的 OldCommittee 必须正确反映 Epoch 2 描述符，而非陈旧的 Epoch 1！
	if plan.OldDescriptor.Epoch != 2 {
		t.Fatalf("expected OldDescriptor epoch 2, got %d", plan.OldDescriptor.Epoch)
	}
	if plan.OldDescriptor.CommitteeID != expectedEpoch2Desc.CommitteeID {
		t.Fatalf("expected OldDescriptor CommitteeID %s, got %s",
			expectedEpoch2Desc.CommitteeID, plan.OldDescriptor.CommitteeID)
	}
	// OldCommittee 必须包含 4 个成员，对应 Epoch 2
	if len(plan.Config.OldCommittee) != 4 {
		t.Fatalf("expected OldCommittee length 4, got %d", len(plan.Config.OldCommittee))
	}
	// 本节点身份：在 Epoch 2 (Old) 不是 Dealer，在 Epoch 3 (New) 是 Receiver
	if plan.LocalRole.IsDealer {
		t.Fatal("node must NOT be dealer for OldCommittee")
	}
	if !plan.LocalRole.IsReceiver {
		t.Fatal("node MUST be receiver for NewCommittee")
	}
}

// TestNodeIDFallback 测试 CandidateFromEnv 与 InitKeyManager/cmd 的回退逻辑一致性。
func TestNodeIDFallback(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyHex := common.Bytes2Hex(crypto.FromECDSA(key))

	// 1. NITCH_POW_CANDIDATE_ID 优先
	env1 := map[string]string{
		"NITCH_POW_CANDIDATE_ID": "cand-priority",
		"NITCH_NODE_ID":          "node-fallback",
		"NITCH_DATA_DIR":         "/data/fallback",
		"NITCH_POW_WORKER_KEY":   keyHex,
	}
	c1, ok, err := CandidateFromEnv(func(k string) string { return env1[k] })
	if err != nil || !ok || c1.CandidateID != "cand-priority" {
		t.Fatalf("expected cand-priority, got cand=%v, ok=%v, err=%v", c1, ok, err)
	}

	// 2. 无 CANDIDATE_ID 时回退到 NITCH_NODE_ID
	env2 := map[string]string{
		"NITCH_NODE_ID":        "node-fallback",
		"NITCH_DATA_DIR":       "/data/fallback",
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	c2, ok, err := CandidateFromEnv(func(k string) string { return env2[k] })
	if err != nil || !ok || c2.CandidateID != "node-fallback" {
		t.Fatalf("expected node-fallback, got cand=%v, ok=%v, err=%v", c2, ok, err)
	}

	// 3. 无 NODE_ID 时回退到 NITCH_DATA_DIR
	env3 := map[string]string{
		"NITCH_DATA_DIR":       "/data/fallback-dir",
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	c3, ok, err := CandidateFromEnv(func(k string) string { return env3[k] })
	if err != nil || !ok || c3.CandidateID != "/data/fallback-dir" {
		t.Fatalf("expected /data/fallback-dir, got cand=%v, ok=%v, err=%v", c3, ok, err)
	}

	// 4. 无 NODE_ID 时回退到 NITCH_DATADIR
	env4 := map[string]string{
		"NITCH_DATADIR":        "/data/datadir-fallback",
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	c4, ok, err := CandidateFromEnv(func(k string) string { return env4[k] })
	if err != nil || !ok || c4.CandidateID != "/data/datadir-fallback" {
		t.Fatalf("expected /data/datadir-fallback, got cand=%v, ok=%v, err=%v", c4, ok, err)
	}

	// 5. 回退到 KeyManager.GetLocalNodeID()
	prevKM := globalKeyManager
	testKM := &KeyManager{localNodeID: "km-local-node-id"}
	globalKeyManager = testKM
	defer func() { globalKeyManager = prevKM }()

	env5 := map[string]string{
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	c5, ok, err := CandidateFromEnv(func(k string) string { return env5[k] })
	if err != nil || !ok || c5.CandidateID != "km-local-node-id" {
		t.Fatalf("expected km-local-node-id, got cand=%v, ok=%v, err=%v", c5, ok, err)
	}

	// 6. 全空环境不报错，静默返回 ok=false
	c6, ok, err := CandidateFromEnv(func(k string) string { return "" })
	// 注意此时 testKM 还在，但因为没有 worker key/pk，不应触发 candidate 创建
	if err != nil || ok || c6 != nil {
		t.Fatalf("expected nil, false, nil on empty worker env; got cand=%v, ok=%v, err=%v", c6, ok, err)
	}

	// 7. 仅有 NITCH_NODE_ID 无 worker key/pk：普通节点正常启动，不报错
	c7, ok, err := CandidateFromEnv(func(k string) string {
		if k == "NITCH_NODE_ID" {
			return "just-a-node"
		}
		return ""
	})
	if err != nil || ok || c7 != nil {
		t.Fatalf("expected non-candidate node with NODE_ID to return nil, false, nil; got cand=%v, ok=%v, err=%v", c7, ok, err)
	}

	// 8. 有 worker key 但没有任何 candidate/node ID：返回明确错误
	globalKeyManager = nil
	env8 := map[string]string{
		"NITCH_POW_WORKER_KEY": keyHex,
	}
	c8, ok, err := CandidateFromEnv(func(k string) string { return env8[k] })
	if err == nil || ok || c8 != nil {
		t.Fatalf("expected error for missing candidate ID, got cand=%v, ok=%v, err=%v", c8, ok, err)
	}
	if !strings.Contains(err.Error(), "candidate ID missing") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestObserverDoesNotForgeKeysAndReceiverExplicitBlockOnMissingPP 验证普通观察者推进时绝不伪造密钥材料，
// 且新当选 receiver 在缺失真实 public params 时返回明确阻塞错误 ErrReceiverMissingPublicKey，不伪造参数。
func TestObserverDoesNotForgeKeysAndReceiverExplicitBlockOnMissingPP(t *testing.T) {
	activeDesc, err := BootstrapActiveCommitteeDesc([]NodeID{"node-1", "node-2", "node-3", "node-4"}, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}

	observerKM := &KeyManager{
		localNodeID:       "plain-observer",
		isCommitteeMember: false,
		nodeIndex:         -1,
	}
	if err := observerKM.BootstrapActiveCommittee(activeDesc.Members, activeDesc.Threshold, activeDesc.Epoch, activeDesc.ActivateAt); err != nil {
		t.Fatalf("BootstrapActiveCommittee: %v", err)
	}

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1

	// 1. 作为普通观察者观察 Epoch 2
	result2 := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"node-1", "node-2", "node-3", "node-5"})
	ev2 := &BoundaryEvent{
		Epoch:          1,
		TargetEpoch:    2,
		ElectionResult: result2,
	}

	if _, err := observerKM.ObserveBoundaryActivation(ev2); err != nil {
		t.Fatalf("ObserveBoundaryActivation failed: %v", err)
	}

	// 观察者状态：严禁伪造 localKey 或 secret share
	if observerKM.localKey != nil {
		t.Fatalf("observer localKey must remain nil, got %v", observerKM.localKey)
	}
	if observerKM.publicParams != nil {
		t.Fatalf("observer publicParams must remain nil, got %v", observerKM.publicParams)
	}
	if observerKM.IsCommitteeMember() {
		t.Fatal("observer must not be committee member")
	}

	// 2. 在 Epoch 3 中，该节点被选举为 receiver
	result3 := rotationElectedResult(t, cfg, 3, 300, []CandidateID{"node-2", "node-3", "node-5", "plain-observer"})
	if err := observerKM.AdoptElectionResult(result3); err != nil {
		t.Fatalf("AdoptElectionResult failed: %v", err)
	}

	// 禁用外部观察者通道
	prevSource := systemReshareKeySource
	systemReshareKeySource = func() (*chameleon.NITCHKey, error) {
		return nil, ErrReceiverMissingPublicKey
	}
	defer func() { systemReshareKeySource = prevSource }()

	// 必须返回明确阻塞错误，严禁绕过密码学或伪造密钥
	err = observerKM.PrepareReshareReceiver()
	if !errors.Is(err, ErrReceiverMissingPublicKey) {
		t.Fatalf("expected ErrReceiverMissingPublicKey, got: %v", err)
	}
	if observerKM.localKey != nil && observerKM.localKey.SecretShare != nil {
		t.Fatal("security violation: receiver must not have forged secret share")
	}
}
