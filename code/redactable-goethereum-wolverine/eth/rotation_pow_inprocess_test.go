package eth

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	ctypes "github.com/ethereum/go-ethereum/core/types"
)

// TestInProcessPoWRotationFullFlow 是对 PoW 轮换（Phase 3A/3B）的完整闭环验收单测。
// 不依赖外部进程、真实私网或合约：
// 1) 至少 6 个 NodeID，其中至少一个纯 observer（node-6）：既不在旧 active 也不在新 committee；
//    boundary 后 active descriptor 推进到目标 epoch，仍非 committee、无 secret share。
// 2) 至少一轮 5 个有效候选竞争选前 4，明确验证按 (blockNumber,txIndex) 截断和 NodeID index；
//    被淘汰旧 dealer 能正确结束。
// 3) Epoch 1->2 与 Epoch 2->3 都实际执行 reshare message 分发、receiver extract、dealer/observer 状态推进，
//    并在两轮之后都实际验证新代继续能做一次 chameleon PartialAdapt/Combine/Verify。
// 4) PoW worker 产生的 tx 显式 types.Sender/HomesteadSigner 验证 sender 与 candidate address，
//    calldata roundtrip；重复 boundary/event 不重复提交。
// 5) 在每轮激活后调用真实生产 UpdateCommitteeRoutes，断言 active/pending 路由按照 NodeID/epoch 更新，
//    退出节点不在 active，新节点在 active。
// 6) 进程/KeyManager/ProtocolManager 指针前后不变，证明 no-restart。
func TestInProcessPoWRotationFullFlow(t *testing.T) {
	// ---------- 1. 基础环境与配置 ----------
	epochLength := uint64(10)
	initialBoundary := uint64(0)
	committeeSize := 4
	threshold := 1
	systemAddr := DefaultSolutionSystemAddress

	// 6 个节点：
	// 前 4 个（node-1..node-4）为代 1 活跃委员会成员；
	// node-5 初始为观察者，在 Epoch 2 竞选入局；
	// node-6 为纯观察者：在 Epoch 1, 2, 3 全程既非旧 active 也非新 committee 成员。
	nodeIDs := []NodeID{
		"node-1",
		"node-2",
		"node-3",
		"node-4",
		"node-5",
		"node-6",
	}

	keys := testReshareKeys(t) // 4 个旧代真实份额，pp, 门限 1, 规模 4
	pp := keys[0].PP

	// 初始化代 1 (Epoch 1) ActiveCommittee 描述符：node-1..node-4
	activeDescEpoch1, err := BootstrapActiveCommitteeDesc(nodeIDs[:4], threshold, 1, 10)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc Epoch 1: %v", err)
	}

	type nodeInstance struct {
		id     NodeID
		cand   *PowCandidate
		key    *chameleon.NITCHKey
		km     *KeyManager
		pm     *ProtocolManager
		worker *PowWorker
		pool   *fakeTxPool
		kmAddr string
		pmAddr string
	}

	nodes := make(map[NodeID]*nodeInstance)

	for i, nid := range nodeIDs {
		inst := &nodeInstance{
			id:   nid,
			pool: newFakeTxPool(),
		}
		// 生成候选私钥与 Signer
		cand, _ := createTestCandidate(t, CandidateID(nid), []byte(fmt.Sprintf("pk-%s", nid)))
		inst.cand = cand

		// KeyManager 初始化
		km := &KeyManager{
			localNodeID:      nid,
			peerKeys:         make(map[common.Address]*chameleon.NITCHKey),
			pendingProposals: make(map[string]*ProposalShares),
		}

		if i < 4 {
			// 前 4 个节点初始为活跃委员会成员，拥有真实密钥
			km.localKey = keys[i]
			km.publicParams = pp
			km.isCommitteeMember = true
			km.nodeIndex = i
			km.committeeSize = committeeSize
			km.threshold = threshold
			km.numParties = committeeSize
			if err := km.BootstrapActiveCommittee(activeDescEpoch1.Members, threshold, 1, 10); err != nil {
				t.Fatalf("node %s bootstrap active committee: %v", nid, err)
			}
			inst.key = keys[i]
		} else {
			// node-5 和 node-6 初始为普通观察者：无 secret share，非 committee member
			km.isCommitteeMember = false
			km.nodeIndex = -1
			km.committeeSize = committeeSize
			km.threshold = threshold
			km.numParties = committeeSize
			if err := km.BootstrapActiveCommittee(activeDescEpoch1.Members, threshold, 1, 10); err != nil {
				t.Fatalf("node %s bootstrap active committee: %v", nid, err)
			}
		}

		// ProtocolManager 初始化
		pm := &ProtocolManager{
			peers:                       newPeerSet(),
			pendingCommitteePeers:       make(map[int]string),
			pendingCommitteeDirectPeers: make(map[int]string),
			activeCommitteePeers:        make(map[int]string),
			activeCommitteeDirectPeers:  make(map[int]string),
			nodeIDToPeerID:              make(map[NodeID]string),
			peerIDToNodeID:              make(map[string]NodeID),
		}

		// 为 PM 关联所有 peer
		for _, otherID := range nodeIDs {
			peerID := fmt.Sprintf("peer-%s", otherID)
			p := &peer{id: peerID, rw: &reshareCountingRW{}}
			pm.peers.Register(p)
			pm.nodeIDToPeerID[otherID] = peerID
			pm.peerIDToNodeID[peerID] = otherID
		}
		pm.UpdateCommitteeRoutes(km.ActiveCommittee(), nil)

		// PowWorker 初始化
		workerCfg := PowWorkerConfig{
			Enabled:       true,
			Target:        MaxTarget,
			SystemAddress: systemAddr,
			GasLimit:      100000,
			GasPrice:      big.NewInt(1),
		}
		worker := NewPowWorker(workerCfg, inst.pool, inst.pool)
		if err := worker.AddCandidate(*cand); err != nil {
			t.Fatalf("node %s worker AddCandidate: %v", nid, err)
		}

		inst.km = km
		inst.pm = pm
		inst.worker = worker
		inst.kmAddr = fmt.Sprintf("%p", km)
		inst.pmAddr = fmt.Sprintf("%p", pm)

		nodes[nid] = inst
	}

	// 观察者全局公钥通道设置（使非委员会节点能以合法公钥视图准备 receiver）
	withStubSystemKey(t, keys[0])

	// 构造 fake canonical chain / mockBlockReader
	reader := newMockBlockReader()
	for b := uint64(0); b <= 30; b++ {
		reader.addBlock(b)
	}

	electCfg := ElectionConfig{
		EpochLength:     epochLength,
		InitialBoundary: initialBoundary,
		CommitteeSize:   committeeSize,
		Threshold:       threshold,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	// ---------- 2. 第一轮 (Epoch 1 -> Epoch 2 轮换) ----------
	// 计算 Epoch 1 的 Tau (源于 boundary block 0)
	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)

	// 5 个有效候选节点竞争选前 4：
	// node-2, node-3, node-4, node-5, node-1 全部出有效解。
	// node-6 作为纯观察者不出解。
	participatingEpoch1 := []NodeID{"node-2", "node-3", "node-4", "node-5", "node-1"}
	epoch1WorkerTxs := make(map[NodeID]*ctypes.Transaction)

	for _, nid := range participatingEpoch1 {
		inst := nodes[nid]
		ev := &BoundaryEvent{
			FinalizedEpoch:  0,
			TargetEpoch:     1,
			CurrentBoundary: 0,
			NextTau:         tau1,
		}
		txs, err := inst.worker.ProcessBoundaryEvent(ev)
		if err != nil {
			t.Fatalf("node %s ProcessBoundaryEvent failed: %v", nid, err)
		}
		if len(txs) != 1 {
			t.Fatalf("node %s expected 1 tx, got %d", nid, len(txs))
		}
		tx := txs[0]

		// 断言 4: 显式 types.Sender / HomesteadSigner 验证 sender 与 candidate address
		sender, err := ctypes.Sender(ctypes.HomesteadSigner{}, tx)
		if err != nil {
			t.Fatalf("node %s types.Sender failed: %v", nid, err)
		}
		if sender != inst.cand.Address {
			t.Fatalf("node %s sender mismatch: got %s want %s", nid, sender.Hex(), inst.cand.Address.Hex())
		}

		// 断言 4: calldata roundtrip 解码与严格字段校验
		sol, err := DecodeSolutionCalldata(tx.Data())
		if err != nil {
			t.Fatalf("node %s DecodeSolutionCalldata failed: %v", nid, err)
		}
		if sol.CandidateID != inst.cand.CandidateID {
			t.Fatalf("node %s candidateID mismatch: got %s want %s", nid, sol.CandidateID, inst.cand.CandidateID)
		}
		if sol.Epoch != 1 {
			t.Fatalf("node %s epoch mismatch: got %d want 1", nid, sol.Epoch)
		}
		if !bytes.Equal(sol.ElectionPK, inst.cand.ElectionPK) {
			t.Fatalf("node %s electionPK mismatch", nid)
		}
		if !VerifySolution(sol, tau1, MaxTarget) {
			t.Fatalf("node %s mined solution failed verification", nid)
		}

		// 断言 4: 重复 boundary/event 不重复提交
		dupTxs, err := inst.worker.ProcessBoundaryEvent(ev)
		if err != nil {
			t.Fatalf("node %s duplicate ProcessBoundaryEvent failed: %v", nid, err)
		}
		if len(dupTxs) != 0 {
			t.Fatalf("node %s expected 0 duplicate txs, got %d", nid, len(dupTxs))
		}

		epoch1WorkerTxs[nid] = tx
	}

	// 区块写入与链上位置排列：
	// node-2 在 block 2 (txIndex 0)
	// node-3 在 block 3 (txIndex 0)
	// node-4 在 block 4 (txIndex 0)
	// node-5 在 block 5 (txIndex 0)
	// node-1 在 block 5 (txIndex 1) —— 同在 block 5，但 txIndex 靠后
	reader.addBlock(2, epoch1WorkerTxs["node-2"])
	reader.addBlock(3, epoch1WorkerTxs["node-3"])
	reader.addBlock(4, epoch1WorkerTxs["node-4"])
	reader.addBlock(5, epoch1WorkerTxs["node-5"], epoch1WorkerTxs["node-1"])

	// 扫描 Epoch 1 解 (区间 0..10)，断言 5 个候选解全量扫描且顺序精确符合 (blockNumber, txIndex)
	scannedSolutions1, err := ScanEpochSolutions(reader, 0, 10, 1, tau1, MaxTarget, systemAddr, nil)
	if err != nil {
		t.Fatalf("ScanEpochSolutions Epoch 1 failed: %v", err)
	}
	if len(scannedSolutions1) != 5 {
		t.Fatalf("expected 5 scanned solutions in Epoch 1, got %d", len(scannedSolutions1))
	}
	if scannedSolutions1[0].CandidateID != "node-2" || scannedSolutions1[0].BlockNumber != 2 || scannedSolutions1[0].TxIndex != 0 {
		t.Fatalf("scanned solution 0 mismatch: %+v", scannedSolutions1[0])
	}
	if scannedSolutions1[1].CandidateID != "node-3" || scannedSolutions1[1].BlockNumber != 3 || scannedSolutions1[1].TxIndex != 0 {
		t.Fatalf("scanned solution 1 mismatch: %+v", scannedSolutions1[1])
	}
	if scannedSolutions1[2].CandidateID != "node-4" || scannedSolutions1[2].BlockNumber != 4 || scannedSolutions1[2].TxIndex != 0 {
		t.Fatalf("scanned solution 2 mismatch: %+v", scannedSolutions1[2])
	}
	if scannedSolutions1[3].CandidateID != "node-5" || scannedSolutions1[3].BlockNumber != 5 || scannedSolutions1[3].TxIndex != 0 {
		t.Fatalf("scanned solution 3 mismatch: %+v", scannedSolutions1[3])
	}
	if scannedSolutions1[4].CandidateID != "node-1" || scannedSolutions1[4].BlockNumber != 5 || scannedSolutions1[4].TxIndex != 1 {
		t.Fatalf("scanned solution 4 mismatch: %+v", scannedSolutions1[4])
	}

	// 在 boundary head 10 触发所有 6 个节点的 coordinator
	var receivedEpoch1Events []*BoundaryEvent
	var nodeCoords = make(map[NodeID]*BoundaryCoordinator)

	for _, nid := range nodeIDs {
		inst := nodes[nid]
		coord := NewBoundaryCoordinator(electCfg, reader, nil)
		nodeCoords[nid] = coord

		currKM := inst.km
		currPM := inst.pm

		handler := func(ev *BoundaryEvent) error {
			prevKM := globalKeyManager
			globalKeyManager = currKM
			defer func() { globalKeyManager = prevKM }()

			h := NewReshareBoundaryHandler(currPM)
			return h(ev)
		}
		coord.RegisterHandler(handler)

		evs, err := coord.ProcessChainHead(reader.GetBlockByNumber(10))
		if err != nil {
			t.Fatalf("node %s coordinator ProcessChainHead(10) failed: %v", nid, err)
		}
		if len(evs) != 1 {
			t.Fatalf("node %s expected 1 event, got %d", nid, len(evs))
		}
		receivedEpoch1Events = append(receivedEpoch1Events, evs[0])
	}

	// 断言 2: 验证 5 选 4 选举结果、截断机制与 NodeID 索引
	firstEv := receivedEpoch1Events[0]
	if firstEv.TargetEpoch != 2 {
		t.Fatalf("expected TargetEpoch 2, got %d", firstEv.TargetEpoch)
	}
	if len(firstEv.ElectionResult.RankedWinners) != 4 {
		t.Fatalf("expected 4 ranked winners, got %d", len(firstEv.ElectionResult.RankedWinners))
	}
	expectedWinnersEpoch1 := []CandidateID{"node-2", "node-3", "node-4", "node-5"}
	for r, exp := range expectedWinnersEpoch1 {
		actual := firstEv.ElectionResult.RankedWinners[r].CandidateID
		if actual != exp {
			t.Fatalf("winner rank %d mismatch: got %s want %s", r, actual, exp)
		}
	}
	// node-1 明确因 (blockNumber=5, txIndex=1) 劣于 node-5 (blockNumber=5, txIndex=0) 被淘汰
	for _, w := range firstEv.ElectionResult.RankedWinners {
		if w.CandidateID == "node-1" {
			t.Fatal("node-1 must be truncated and NOT be in RankedWinners")
		}
	}

	// 断言 2: 成员列表按 NodeID 字典序稳定排序并分配 1-based index
	expectedMembersEpoch2 := []NodeID{"node-2", "node-3", "node-4", "node-5"}
	for i, exp := range expectedMembersEpoch2 {
		actual := firstEv.ElectionResult.Descriptor.Members[i]
		if actual != exp {
			t.Fatalf("descriptor member %d mismatch: got %s want %s", i, actual, exp)
		}
		idx := firstEv.ElectionResult.Indices[exp]
		if idx != i+1 {
			t.Fatalf("node %s index mismatch: got %d want %d", exp, idx, i+1)
		}
	}

	// 断言 1: node-6 为纯观察者：不在旧 active (node-1..4)，也不在新 committee (node-2..5)
	// 在 ProcessChainHead(10) 后其 active descriptor 确定性推进到 Epoch 2，但仍非 committee、无 secret share
	km6 := nodes["node-6"].km
	if km6.ActiveCommittee() == nil || km6.ActiveCommittee().Epoch != 2 {
		t.Fatalf("node-6 pure observer active committee epoch mismatch: %+v", km6.ActiveCommittee())
	}
	if km6.IsCommitteeMember() {
		t.Fatal("node-6 pure observer must NOT be committee member")
	}
	if km6.GetNodeIndex() != -1 {
		t.Fatalf("node-6 pure observer nodeIndex must be -1, got %d", km6.GetNodeIndex())
	}
	if km6.GetLocalKey() != nil && km6.GetLocalKey().SecretShare != nil {
		t.Fatal("node-6 pure observer must possess NO secret share")
	}

	// ---------- 3. Epoch 1 -> 2 DKRS Reshare 会话执行 ----------
	sessionIDEpoch2 := BuildReshareSessionID(firstEv.ElectionResult.Descriptor.CommitteeID)

	type dealerData struct {
		deal   *chameleon.DKRSDeal
		shares map[int]*chameleon.DKRSShare
	}
	dealersDataEpoch1 := make(map[NodeID]dealerData)

	dealersEpoch1 := []NodeID{"node-1", "node-2", "node-3", "node-4"}
	for _, did := range dealersEpoch1 {
		dkm := nodes[did].km
		dkm.mu.RLock()
		cache, ok := dkm.reshareOutboundCache[sessionIDEpoch2]
		dkm.mu.RUnlock()
		if !ok || cache == nil || cache.deal == nil {
			t.Fatalf("dealer %s missing cached deal in session %s", did, sessionIDEpoch2)
		}
		dealersDataEpoch1[did] = dealerData{
			deal:   cache.deal,
			shares: cache.shares,
		}
	}

	pendingEpoch2 := nodes["node-2"].km.PendingCommittee()
	if pendingEpoch2 == nil {
		t.Fatal("expected pending committee in node-2")
	}

	receiversEpoch2 := []NodeID{"node-2", "node-3", "node-4", "node-5"}
	for _, rid := range receiversEpoch2 {
		rkm := nodes[rid].km
		recvIdx, ok := pendingEpoch2.MemberIndex(rid)
		if !ok {
			t.Fatalf("receiver %s not in pending committee", rid)
		}
		for did, ddata := range dealersDataEpoch1 {
			dealMsg := ctypes.ReshareMessage{
				SessionID: sessionIDEpoch2,
				Deal:      ddata.deal,
			}
			if _, err := rkm.OnReshareMessage(dealMsg); err != nil {
				t.Fatalf("deliver deal to %s from %s failed: %v", rid, did, err)
			}
			sh := ddata.shares[recvIdx]
			if sh == nil {
				t.Fatalf("dealer %s has no share for receiver index %d", did, recvIdx)
			}
			shareMsg := ctypes.ReshareMessage{
				SessionID: sessionIDEpoch2,
				Share:     sh,
			}
			if _, err := rkm.OnReshareMessage(shareMsg); err != nil {
				t.Fatalf("deliver share to %s from %s failed: %v", rid, did, err)
			}
		}

		if !rkm.ReshareCanExtract() {
			t.Fatalf("receiver %s cannot extract shares after receiving all deals and shares", rid)
		}
		if err := rkm.ReshareExtractAndApply(); err != nil {
			t.Fatalf("receiver %s ReshareExtractAndApply failed: %v", rid, err)
		}

		if rkm.ActiveCommittee() == nil || rkm.ActiveCommittee().Epoch != 2 {
			t.Fatalf("receiver %s ActiveCommittee epoch mismatch: %+v", rid, rkm.ActiveCommittee())
		}
		if !rkm.IsCommitteeMember() {
			t.Fatalf("receiver %s must be marked as committee member", rid)
		}
		if rkm.GetLocalKey() == nil || rkm.GetLocalKey().SecretShare == nil {
			t.Fatalf("receiver %s must possess valid secret share", rid)
		}
	}

	// 断言 2: 被淘汰旧 dealer (node-1) 接收 VerifyKey 并正确结束 dealer 会话收敛退出
	km1 := nodes["node-1"].km
	vk2 := chameleon.NewDKRSVerifyKey(nodes["node-2"].km.GetLocalKey())
	vk3 := chameleon.NewDKRSVerifyKey(nodes["node-3"].km.GetLocalKey())
	if _, err := km1.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionIDEpoch2, VerifyKey: vk2}); err != nil {
		t.Fatalf("deliver vk2 to node-1 failed: %v", err)
	}
	if _, err := km1.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionIDEpoch2, VerifyKey: vk3}); err != nil {
		t.Fatalf("deliver vk3 to node-1 failed: %v", err)
	}
	if err := km1.FinishDealerSession(sessionIDEpoch2); err != nil {
		t.Fatalf("eliminated old dealer node-1 FinishDealerSession failed: %v", err)
	}
	if km1.IsCommitteeMember() {
		t.Fatal("eliminated dealer node-1 must step down from committee membership")
	}
	if km1.ActiveCommittee().Epoch != 2 {
		t.Fatalf("node-1 active committee epoch must advance to 2, got %d", km1.ActiveCommittee().Epoch)
	}
	if km1.ActiveCommittee().Contains("node-1") {
		t.Fatal("node-1 active committee must NOT contain node-1")
	}
	if !km1.ActiveCommittee().Contains("node-5") {
		t.Fatal("node-1 active committee MUST contain new member node-5")
	}

	// 断言 5: 第一轮激活后调用真实生产 UpdateCommitteeRoutes，断言 active/pending 路由按 NodeID/epoch 更新
	for _, nid := range nodeIDs {
		inst := nodes[nid]
		inst.pm.UpdateCommitteeRoutes(inst.km.ActiveCommittee(), inst.km.PendingCommittee())

		if inst.km.PendingCommittee() != nil {
			t.Fatalf("node %s pending committee must be nil after activation", nid)
		}
		if len(inst.pm.activeCommitteePeers) != 4 {
			t.Fatalf("node %s active routes count mismatch: got %d want 4", nid, len(inst.pm.activeCommitteePeers))
		}
		if inst.pm.activeCommitteePeers[1] != "peer-node-2" {
			t.Fatalf("node %s route 1 mismatch: got %s want peer-node-2", nid, inst.pm.activeCommitteePeers[1])
		}
		if inst.pm.activeCommitteePeers[2] != "peer-node-3" {
			t.Fatalf("node %s route 2 mismatch: got %s want peer-node-3", nid, inst.pm.activeCommitteePeers[2])
		}
		if inst.pm.activeCommitteePeers[3] != "peer-node-4" {
			t.Fatalf("node %s route 3 mismatch: got %s want peer-node-4", nid, inst.pm.activeCommitteePeers[3])
		}
		if inst.pm.activeCommitteePeers[4] != "peer-node-5" {
			t.Fatalf("node %s route 4 mismatch: got %s want peer-node-5", nid, inst.pm.activeCommitteePeers[4])
		}
		// 断言退出节点 node-1 不在 active
		for idx, pID := range inst.pm.activeCommitteePeers {
			if pID == "peer-node-1" {
				t.Fatalf("node %s: retired node-1 must NOT be in active routes at index %d", nid, idx)
			}
			if pID == "peer-node-6" {
				t.Fatalf("node %s: observer node-6 must NOT be in active routes at index %d", nid, idx)
			}
		}
	}

	// 断言 6: 验证不重启，KM/PM 实例指针前后不变
	for _, nid := range nodeIDs {
		inst := nodes[nid]
		if fmt.Sprintf("%p", inst.km) != inst.kmAddr {
			t.Fatalf("node %s KeyManager pointer changed after Epoch 1: %p vs %s", nid, inst.km, inst.kmAddr)
		}
		if fmt.Sprintf("%p", inst.pm) != inst.pmAddr {
			t.Fatalf("node %s ProtocolManager pointer changed after Epoch 1: %p vs %s", nid, inst.pm, inst.pmAddr)
		}
	}

	// 断言 3 (第一轮): 验证 Epoch 2 新代能成功进行一次完整的变色龙哈希 Hash / PartialAdapt / Combine / Verify
	msg1Old := chameleon.Message("pow-rotation-epoch1-message")
	msg1New := chameleon.Message("pow-rotation-epoch2-message")
	randR1 := chameleon.RandomPointG1(pp)
	hOut1, err := chameleon.Hash(keys[0], msg1Old, randR1)
	if err != nil {
		t.Fatalf("chameleon.Hash Epoch 1 failed: %v", err)
	}

	var adaptShares1 []*chameleon.AdaptationShare
	for _, rid := range []NodeID{"node-2", "node-3"} { // threshold+1 = 2 份
		rkm := nodes[rid].km
		sh, err := chameleon.PartialAdapt(rkm.GetLocalKey(), msg1Old, msg1New)
		if err != nil {
			t.Fatalf("PartialAdapt %s failed: %v", rid, err)
		}
		adaptShares1 = append(adaptShares1, sh)
	}

	newR1, err := chameleon.Combine(pp, adaptShares1, threshold, committeeSize, hOut1, msg1Old, msg1New)
	if err != nil {
		t.Fatalf("chameleon.Combine Epoch 1 failed: %v", err)
	}
	if !chameleon.Verify(keys[0], hOut1.Hash, msg1New, newR1) {
		t.Fatal("chameleon.Verify failed after Epoch 1 DKRS rotation")
	}

	// ---------- 4. 第二轮 (Epoch 2 -> Epoch 3 轮换) ----------
	// 在 Epoch 2，ActiveCommittee 已经是 {node-2, node-3, node-4, node-5}
	// 在 block 10 计算 Epoch 2 的 Tau，挖掘 block 11..19 内的解
	tau2 := DeriveTau(reader.GetBlockByNumber(10).Hash(), 2)

	// 本轮参与候选：node-1（重新参选入局！）, node-2, node-4, node-5
	// node-3 不出解（模拟其在 Epoch 3 被轮换出局，成为 dealer-only）
	// node-6 仍不出解（保持纯 observer）
	participatingEpoch2 := []NodeID{"node-1", "node-2", "node-4", "node-5"}
	blockForCand2 := map[NodeID]uint64{
		"node-1": 12,
		"node-2": 13,
		"node-4": 14,
		"node-5": 15,
	}

	for _, nid := range participatingEpoch2 {
		inst := nodes[nid]
		ev := &BoundaryEvent{
			FinalizedEpoch:  1,
			TargetEpoch:     2,
			CurrentBoundary: 10,
			NextTau:         tau2,
		}
		txs, err := inst.worker.ProcessBoundaryEvent(ev)
		if err != nil {
			t.Fatalf("Epoch 2 node %s ProcessBoundaryEvent failed: %v", nid, err)
		}
		if len(txs) != 1 {
			t.Fatalf("Epoch 2 node %s expected 1 tx, got %d", nid, len(txs))
		}
		tx := txs[0]

		// 断言 4: 第二轮 worker tx 显式 types.Sender / HomesteadSigner 校验
		sender, err := ctypes.Sender(ctypes.HomesteadSigner{}, tx)
		if err != nil {
			t.Fatalf("Epoch 2 node %s types.Sender failed: %v", nid, err)
		}
		if sender != inst.cand.Address {
			t.Fatalf("Epoch 2 node %s sender mismatch: got %s want %s", nid, sender.Hex(), inst.cand.Address.Hex())
		}

		// 断言 4: 第二轮 calldata roundtrip
		sol, err := DecodeSolutionCalldata(tx.Data())
		if err != nil {
			t.Fatalf("Epoch 2 node %s DecodeSolutionCalldata failed: %v", nid, err)
		}
		if sol.CandidateID != inst.cand.CandidateID {
			t.Fatalf("Epoch 2 node %s candidateID mismatch: got %s want %s", nid, sol.CandidateID, inst.cand.CandidateID)
		}
		if sol.Epoch != 2 {
			t.Fatalf("Epoch 2 node %s epoch mismatch: got %d want 2", nid, sol.Epoch)
		}
		if !bytes.Equal(sol.ElectionPK, inst.cand.ElectionPK) {
			t.Fatalf("Epoch 2 node %s electionPK mismatch", nid)
		}
		if !VerifySolution(sol, tau2, MaxTarget) {
			t.Fatalf("Epoch 2 node %s solution verification failed", nid)
		}

		// 断言 4: 重复事件不重复提交
		dupTxs, err := inst.worker.ProcessBoundaryEvent(ev)
		if err != nil {
			t.Fatalf("Epoch 2 node %s duplicate event error: %v", nid, err)
		}
		if len(dupTxs) != 0 {
			t.Fatalf("Epoch 2 node %s duplicate event produced %d txs", nid, len(dupTxs))
		}

		reader.addBlock(blockForCand2[nid], tx)
	}

	// 扫描 Epoch 2 解 (区间 10..20)
	scannedSolutions2, err := ScanEpochSolutions(reader, 10, 20, 2, tau2, MaxTarget, systemAddr, nil)
	if err != nil {
		t.Fatalf("ScanEpochSolutions Epoch 2 failed: %v", err)
	}
	if len(scannedSolutions2) != 4 {
		t.Fatalf("expected 4 scanned solutions in Epoch 2, got %d", len(scannedSolutions2))
	}

	// 在 boundary head 20 触发所有 6 个节点的 coordinator
	var receivedEpoch2Events []*BoundaryEvent
	for _, nid := range nodeIDs {
		coord := nodeCoords[nid]
		evs, err := coord.ProcessChainHead(reader.GetBlockByNumber(20))
		if err != nil {
			t.Fatalf("Epoch 2 node %s ProcessChainHead(20) failed: %v", nid, err)
		}
		if len(evs) != 1 {
			t.Fatalf("Epoch 2 node %s expected 1 event, got %d", nid, len(evs))
		}
		receivedEpoch2Events = append(receivedEpoch2Events, evs[0])
	}

	evEpoch3 := receivedEpoch2Events[0]
	if evEpoch3.TargetEpoch != 3 {
		t.Fatalf("expected TargetEpoch 3, got %d", evEpoch3.TargetEpoch)
	}
	if len(evEpoch3.ElectionResult.RankedWinners) != 4 {
		t.Fatalf("expected 4 winners in Epoch 2, got %d", len(evEpoch3.ElectionResult.RankedWinners))
	}

	// 验证参与第二轮轮换的各节点 OldCommittee 严格继承 Epoch 2 的 Active 描述符 {node-2, node-3, node-4, node-5}
	participatingOrDealerNodes := []NodeID{"node-1", "node-2", "node-3", "node-4", "node-5"}
	for _, nid := range participatingOrDealerNodes {
		inst := nodes[nid]
		inst.km.mu.RLock()
		activeNow := inst.km.activeCommittee
		inst.km.mu.RUnlock()

		if activeNow == nil || activeNow.Epoch != 2 {
			t.Fatalf("node %s active committee epoch before Epoch 3 reshare must be 2, got %+v", nid, activeNow)
		}
		if activeNow.Contains("node-1") {
			t.Fatalf("node %s OldCommittee must NOT contain node-1", nid)
		}
		if !activeNow.Contains("node-5") {
			t.Fatalf("node %s OldCommittee MUST contain node-5", nid)
		}
		if !activeNow.Contains("node-3") {
			t.Fatalf("node %s OldCommittee MUST contain node-3", nid)
		}

		plan, err := inst.km.BuildResharePlanFromState()
		if err != nil {
			t.Fatalf("node %s BuildResharePlanFromState failed: %v", nid, err)
		}
		if plan.OldDescriptor.Epoch != 2 {
			t.Fatalf("node %s OldDescriptor epoch mismatch: got %d want 2", nid, plan.OldDescriptor.Epoch)
		}
		if len(plan.Config.OldCommittee) != 4 {
			t.Fatalf("node %s OldCommittee length mismatch: got %d want 4", nid, len(plan.Config.OldCommittee))
		}
		switch nid {
		case "node-1":
			if plan.LocalRole.IsDealer || !plan.LocalRole.IsReceiver {
				t.Fatalf("node-1 role mismatch: expected receiver-only, got %+v", plan.LocalRole)
			}
		case "node-3":
			if !plan.LocalRole.IsDealer || plan.LocalRole.IsReceiver {
				t.Fatalf("node-3 role mismatch: expected dealer-only, got %+v", plan.LocalRole)
			}
		case "node-2", "node-4", "node-5":
			if !plan.LocalRole.IsDealer || !plan.LocalRole.IsReceiver {
				t.Fatalf("%s role mismatch: expected dealer+receiver, got %+v", nid, plan.LocalRole)
			}
		}
	}

	// 断言 1: node-6 纯观察者在 boundary head 20 后 active descriptor 推进到 Epoch 3，仍非 committee、无 secret share
	if km6.ActiveCommittee() == nil || km6.ActiveCommittee().Epoch != 3 {
		t.Fatalf("node-6 pure observer active committee epoch mismatch after head 20: %+v", km6.ActiveCommittee())
	}
	if km6.IsCommitteeMember() {
		t.Fatal("node-6 pure observer must NOT be committee member after head 20")
	}
	if km6.GetNodeIndex() != -1 {
		t.Fatalf("node-6 pure observer nodeIndex must be -1, got %d", km6.GetNodeIndex())
	}
	if km6.GetLocalKey() != nil && km6.GetLocalKey().SecretShare != nil {
		t.Fatal("node-6 pure observer must possess NO secret share after head 20")
	}

	// ---------- 5. 断言 3: Epoch 2 -> Epoch 3 实际执行 reshare message 分发、receiver extract、dealer/observer 推进 ----------
	sessionIDEpoch3 := BuildReshareSessionID(evEpoch3.ElectionResult.Descriptor.CommitteeID)

	dealersDataEpoch2 := make(map[NodeID]dealerData)
	dealersEpoch2 := []NodeID{"node-2", "node-3", "node-4", "node-5"} // Epoch 2 活跃委员会成员
	for _, did := range dealersEpoch2 {
		dkm := nodes[did].km
		dkm.mu.RLock()
		cache, ok := dkm.reshareOutboundCache[sessionIDEpoch3]
		dkm.mu.RUnlock()
		if !ok || cache == nil || cache.deal == nil {
			t.Fatalf("dealer %s missing cached deal in session %s", did, sessionIDEpoch3)
		}
		dealersDataEpoch2[did] = dealerData{
			deal:   cache.deal,
			shares: cache.shares,
		}
	}

	pendingEpoch3 := nodes["node-2"].km.PendingCommittee()
	if pendingEpoch3 == nil {
		t.Fatal("expected pending committee in node-2 for Epoch 3")
	}
	// 新委员会成员按 NodeID 字典序：node-1 (1), node-2 (2), node-4 (3), node-5 (4)
	expectedMembersEpoch3 := []NodeID{"node-1", "node-2", "node-4", "node-5"}
	for i, exp := range expectedMembersEpoch3 {
		if pendingEpoch3.Members[i] != exp {
			t.Fatalf("pending committee member %d mismatch: got %s want %s", i, pendingEpoch3.Members[i], exp)
		}
	}

	// 投递 deals 和 shares 到所有 Epoch 3 receiver (node-1, node-2, node-4, node-5)
	receiversEpoch3 := []NodeID{"node-1", "node-2", "node-4", "node-5"}
	for _, rid := range receiversEpoch3 {
		rkm := nodes[rid].km
		recvIdx, ok := pendingEpoch3.MemberIndex(rid)
		if !ok {
			t.Fatalf("receiver %s not in pending committee for Epoch 3", rid)
		}
		for did, ddata := range dealersDataEpoch2 {
			dealMsg := ctypes.ReshareMessage{
				SessionID: sessionIDEpoch3,
				Deal:      ddata.deal,
			}
			if _, err := rkm.OnReshareMessage(dealMsg); err != nil {
				t.Fatalf("Epoch 3: deliver deal to %s from %s failed: %v", rid, did, err)
			}
			sh := ddata.shares[recvIdx]
			if sh == nil {
				t.Fatalf("Epoch 3: dealer %s has no share for receiver index %d", did, recvIdx)
			}
			shareMsg := ctypes.ReshareMessage{
				SessionID: sessionIDEpoch3,
				Share:     sh,
			}
			if _, err := rkm.OnReshareMessage(shareMsg); err != nil {
				t.Fatalf("Epoch 3: deliver share to %s from %s failed: %v", rid, did, err)
			}
		}

		// 验证 receiver 是否可以提取
		if !rkm.ReshareCanExtract() {
			t.Fatalf("Epoch 3 receiver %s cannot extract shares", rid)
		}
		if err := rkm.ReshareExtractAndApply(); err != nil {
			t.Fatalf("Epoch 3 receiver %s ReshareExtractAndApply failed: %v", rid, err)
		}

		// 验证 receiver 运行时状态已切换至 Epoch 3
		if rkm.ActiveCommittee() == nil || rkm.ActiveCommittee().Epoch != 3 {
			t.Fatalf("Epoch 3 receiver %s ActiveCommittee epoch mismatch: %+v", rid, rkm.ActiveCommittee())
		}
		if !rkm.IsCommitteeMember() {
			t.Fatalf("Epoch 3 receiver %s must be marked as committee member", rid)
		}
		if rkm.GetLocalKey() == nil || rkm.GetLocalKey().SecretShare == nil {
			t.Fatalf("Epoch 3 receiver %s must possess valid secret share", rid)
		}
	}

	// 验证第二轮被淘汰旧 dealer (node-3) 接收 VerifyKey 并正确收敛结束 dealer 会话
	km3 := nodes["node-3"].km
	vkNode1 := chameleon.NewDKRSVerifyKey(nodes["node-1"].km.GetLocalKey())
	vkNode2 := chameleon.NewDKRSVerifyKey(nodes["node-2"].km.GetLocalKey())
	if _, err := km3.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionIDEpoch3, VerifyKey: vkNode1}); err != nil {
		t.Fatalf("deliver vkNode1 to node-3 failed: %v", err)
	}
	if _, err := km3.OnReshareMessage(ctypes.ReshareMessage{SessionID: sessionIDEpoch3, VerifyKey: vkNode2}); err != nil {
		t.Fatalf("deliver vkNode2 to node-3 failed: %v", err)
	}
	if err := km3.FinishDealerSession(sessionIDEpoch3); err != nil {
		t.Fatalf("Epoch 3 node-3 FinishDealerSession failed: %v", err)
	}
	if km3.IsCommitteeMember() {
		t.Fatal("dealer-only node-3 must step down from committee membership in Epoch 3")
	}
	if km3.ActiveCommittee().Epoch != 3 {
		t.Fatalf("node-3 active committee epoch must advance to 3, got %d", km3.ActiveCommittee().Epoch)
	}
	if km3.ActiveCommittee().Contains("node-3") {
		t.Fatal("node-3 active committee must NOT contain node-3")
	}
	if !km3.ActiveCommittee().Contains("node-1") {
		t.Fatal("node-3 active committee MUST contain re-joined node-1")
	}

	// 断言 5: 第二轮激活后调用真实生产 UpdateCommitteeRoutes，断言 active/pending 路由按 NodeID/epoch 更新
	for _, nid := range nodeIDs {
		inst := nodes[nid]
		inst.pm.UpdateCommitteeRoutes(inst.km.ActiveCommittee(), inst.km.PendingCommittee())

		if inst.km.PendingCommittee() != nil {
			t.Fatalf("node %s pending committee must be nil after Epoch 3 activation", nid)
		}
		if len(inst.pm.activeCommitteePeers) != 4 {
			t.Fatalf("node %s active routes count mismatch: got %d want 4", nid, len(inst.pm.activeCommitteePeers))
		}
		// 路由映射按升序 NodeID: 1: node-1, 2: node-2, 3: node-4, 4: node-5
		if inst.pm.activeCommitteePeers[1] != "peer-node-1" {
			t.Fatalf("node %s route 1 mismatch: got %s want peer-node-1", nid, inst.pm.activeCommitteePeers[1])
		}
		if inst.pm.activeCommitteePeers[2] != "peer-node-2" {
			t.Fatalf("node %s route 2 mismatch: got %s want peer-node-2", nid, inst.pm.activeCommitteePeers[2])
		}
		if inst.pm.activeCommitteePeers[3] != "peer-node-4" {
			t.Fatalf("node %s route 3 mismatch: got %s want peer-node-4", nid, inst.pm.activeCommitteePeers[3])
		}
		if inst.pm.activeCommitteePeers[4] != "peer-node-5" {
			t.Fatalf("node %s route 4 mismatch: got %s want peer-node-5", nid, inst.pm.activeCommitteePeers[4])
		}
		// 退出节点 node-3 与纯观察者 node-6 绝对不在 active 路由中
		for idx, pID := range inst.pm.activeCommitteePeers {
			if pID == "peer-node-3" {
				t.Fatalf("node %s: retired node-3 must NOT be in active routes at index %d", nid, idx)
			}
			if pID == "peer-node-6" {
				t.Fatalf("node %s: observer node-6 must NOT be in active routes at index %d", nid, idx)
			}
		}
	}

	// 断言 6: 验证不重启，全部 6 个节点的 KM/PM 实例指针与初始指针严格一致
	for _, nid := range nodeIDs {
		inst := nodes[nid]
		if fmt.Sprintf("%p", inst.km) != inst.kmAddr {
			t.Fatalf("node %s KeyManager pointer changed across full flow: %p vs %s", nid, inst.km, inst.kmAddr)
		}
		if fmt.Sprintf("%p", inst.pm) != inst.pmAddr {
			t.Fatalf("node %s ProtocolManager pointer changed across full flow: %p vs %s", nid, inst.pm, inst.pmAddr)
		}
	}

	// 断言 3 (第二轮): 验证 Epoch 3 新代（含重新加入的 node-1 和持续存续的 node-2）
	// 能继续做一次完整的变色龙哈希 Hash / PartialAdapt / Combine / Verify！
	msg3Old := chameleon.Message("pow-rotation-epoch3-old-message")
	msg3New := chameleon.Message("pow-rotation-epoch3-new-message")
	randR3 := chameleon.RandomPointG1(pp)
	hOut3, err := chameleon.Hash(keys[0], msg3Old, randR3)
	if err != nil {
		t.Fatalf("chameleon.Hash Epoch 3 failed: %v", err)
	}

	var adaptShares3 []*chameleon.AdaptationShare
	for _, rid := range []NodeID{"node-1", "node-2"} { // threshold+1 = 2 份，包含重新提取密钥的 node-1
		rkm := nodes[rid].km
		sh, err := chameleon.PartialAdapt(rkm.GetLocalKey(), msg3Old, msg3New)
		if err != nil {
			t.Fatalf("PartialAdapt %s Epoch 3 failed: %v", rid, err)
		}
		adaptShares3 = append(adaptShares3, sh)
	}

	newR3, err := chameleon.Combine(pp, adaptShares3, threshold, committeeSize, hOut3, msg3Old, msg3New)
	if err != nil {
		t.Fatalf("chameleon.Combine Epoch 3 failed: %v", err)
	}
	if !chameleon.Verify(keys[0], hOut3.Hash, msg3New, newR3) {
		t.Fatal("chameleon.Verify failed after Epoch 3 DKRS rotation")
	}
}

// TestInProcessPoWRotationIntegrationSeamsAndBlocking 验证 integration seam 以及真实环境依赖阻塞项
func TestInProcessPoWRotationIntegrationSeamsAndBlocking(t *testing.T) {
	// 验证缺失公钥通道时的阻塞 (ErrReceiverMissingPublicKey)
	withEmptySystemKey(t)

	km := &KeyManager{
		localNodeID: "new-isolated-node",
	}
	desc, _ := BootstrapActiveCommitteeDesc([]NodeID{"n1", "n2", "n3", "n4"}, 1, 1, 100)
	_ = km.BootstrapActiveCommittee(desc.Members, desc.Threshold, desc.Epoch, desc.ActivateAt)

	cfg := DefaultElectionConfig()
	res := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"n1", "n2", "n3", "new-isolated-node"})
	_ = km.AdoptElectionResult(res)

	// 此时无公钥来源，必须返回明确的阻塞错误，绝不伪造
	err := km.PrepareReshareReceiver()
	if err != ErrReceiverMissingPublicKey {
		t.Fatalf("expected ErrReceiverMissingPublicKey, got: %v", err)
	}

	// 验证 ProtocolManager 路由缝合：当目标索引路由存在但目标 peer 无法写入（例如连接已断开返回 err）时可控失败
	pm := &ProtocolManager{
		peers:                       newPeerSet(),
		pendingCommitteePeers:       make(map[int]string),
		pendingCommitteeDirectPeers: make(map[int]string),
		activeCommitteePeers:        make(map[int]string),
		activeCommitteeDirectPeers:  make(map[int]string),
	}
	failingRW := &reshareCountingRW{err: fmt.Errorf("network disconnect")}
	pFail := &peer{id: "peer-failing", rw: failingRW}
	pm.peers.Register(pFail)
	pm.activeCommitteeDirectPeers[1] = "peer-failing"
	pm.activeCommitteePeers[1] = "peer-failing"

	succ, fail := pm.SendReshareToIndex(1, ctypes.ReshareMessage{SessionID: "test-sess"})
	if succ != 0 || fail != 1 {
		t.Fatalf("expected SendReshareToIndex to fail gracefully on disconnected peer: succ=%d fail=%d", succ, fail)
	}
}
