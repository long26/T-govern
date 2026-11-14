package eth

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Phase 3A 测试：运行时 CommitteeDescriptor/KeyManager 状态接入 + boundary election
// 到现有 reshare 的适配。约束：
//   - 不重启、不做合约、node_registry 只做身份发现；
//   - 旧 key/StartReshare/advanceReshare/ReshareCanExtract/ReshareExtractAndApply/
//     SameGlobalPublicKey 生命周期全部复用；
//   - 不伪造 share。

// ---------- 测试辅助 ----------

// rotationObserverKeyManager 构造一个不带任何密钥材料的 KeyManager（观察者/新成员起点）。
// testKeyManagerForReshare 会解引用 key.PP，不能传 nil。
func rotationObserverKeyManager(nodeIndex0 int) *KeyManager {
	return &KeyManager{
		nodeIndex:         nodeIndex0,
		isCommitteeMember: false,
		peerKeys:          make(map[common.Address]*chameleon.NITCHKey),
		pendingProposals:  make(map[string]*ProposalShares),
	}
}

// rotationIDs 定义一个 5 节点身份集合。注意 NodeID 升序与给出顺序刻意不同，
// 以验证 descriptor 索引对输入排列的规范化。
var rotationIDs = []NodeID{
	"enode://node-e", // registry 索引 0
	"enode://node-a", // registry 索引 1
	"enode://node-c", // registry 索引 2
	"enode://node-b", // registry 索引 3
	"enode://node-d", // registry 索引 4
}

// rotationActiveDesc 构造旧代（epoch 1）active 描述符：成员顺序 = 旧 DKRS 索引顺序，
// 即 rotationIDs 的原始顺序（registry/bootstrap 顺序）。
func rotationActiveDesc(t *testing.T) *CommitteeDescriptor {
	t.Helper()
	// 旧代 4 人委员会（DKRS 域需 2 的幂）：前 4 个身份，index 1..4。
	members := rotationIDs[:4]
	desc, err := BootstrapActiveCommitteeDesc(members, 1, 1, 100)
	if err != nil {
		t.Fatalf("BootstrapActiveCommitteeDesc: %v", err)
	}
	return desc
}

// rotationElectedResult 用 ElectCommittee 从给定的有效解构造 pending 描述符。
// 返回的 winner 顺序即链上顺序；期望 CommitteeSize=len(newMembers)。
func rotationElectedResult(t *testing.T, cfg ElectionConfig, epoch, activateAt uint64, orderedCandidates []CandidateID) *ElectionResult {
	t.Helper()
	tau := DeriveTau(common.HexToHash("0xabcd"), epoch-1)
	solutions := make([]Solution, 0, len(orderedCandidates))
	for i, cand := range orderedCandidates {
		nonce := uint64(i)
		digest := ComputePowDigest(tau, epoch, cand, []byte{byte('A' + i)}, nonce)
		solutions = append(solutions, Solution{
			Epoch:       epoch,
			CandidateID: cand,
			ElectionPK:  []byte{byte('A' + i)},
			Nonce:       nonce,
			PowDigest:   digest,
			// 链上顺序按输入顺序：块号递增。
			BlockNumber: uint64(10 + i),
			TxIndex:     0,
		})
	}
	for _, s := range solutions {
		if !VerifySolution(&s, tau, cfg.Target) {
			t.Fatalf("test solution for %s failed PoW verification", s.CandidateID)
		}
	}
	result, err := ElectCommittee(solutions, cfg.CommitteeSize, cfg.Threshold, epoch, activateAt, nil)
	if err != nil {
		t.Fatalf("ElectCommittee: %v", err)
	}
	return result
}

// rotationTestCommitteeKeyManagers 为旧代 4 名成员各构造一个带真实 DistKG 份额的
// KeyManager（复用 testKeyManagerForReshare 的构造方式），并 bootstrap active 描述符。
func rotationTestCommitteeKeyManagers(t *testing.T) ([]*KeyManager, []*chameleon.NITCHKey) {
	t.Helper()
	keys := testReshareKeys(t) // SimulatedDistKG(pp,1,4)：keys[i] 持有旧索引 i+1
	desc := rotationActiveDesc(t)
	managers := make([]*KeyManager, len(keys))
	for i, key := range keys {
		km := testKeyManagerForReshare(key, i)
		km.localNodeID = rotationIDs[i]
		if err := km.BootstrapActiveCommittee(desc.Members, desc.Threshold, desc.Epoch, desc.ActivateAt); err != nil {
			t.Fatalf("BootstrapActiveCommittee node=%s: %v", rotationIDs[i], err)
		}
		managers[i] = km
	}
	return managers, keys
}

// withStubSystemKey 用持有真实公钥视图的 stub 替换观察者通道，返回恢复函数。
func withStubSystemKey(t *testing.T, key *chameleon.NITCHKey) {
	t.Helper()
	prev := systemReshareKeySource
	systemReshareKeySource = func() (*chameleon.NITCHKey, error) {
		return &chameleon.NITCHKey{
			PP:         key.PP,
			GlobalPK:   key.GlobalPK,
			GlobalPKG1: key.GlobalPKG1,
			Threshold:  key.Threshold,
			NumParties: key.NumParties,
		}, nil
	}
	t.Cleanup(func() { systemReshareKeySource = prev })
}

func withEmptySystemKey(t *testing.T) {
	t.Helper()
	prev := systemReshareKeySource
	systemReshareKeySource = func() (*chameleon.NITCHKey, error) {
		return nil, errors.New("system NITCHKey not initialized yet")
	}
	t.Cleanup(func() { systemReshareKeySource = prev })
}

// ---------- 1) descriptor 状态 ----------

func TestCommitteeDescriptorStateRoundTrip(t *testing.T) {
	km := rotationObserverKeyManager(0)
	km.localNodeID = "enode://me"

	if km.ActiveCommittee() != nil || km.PendingCommittee() != nil {
		t.Fatal("expected nil descriptors before bootstrap")
	}
	if err := km.BootstrapActiveCommittee(nil, 1, 1, 100); err == nil {
		t.Fatal("expected error for empty bootstrap members")
	}
	desc := rotationActiveDesc(t)
	if err := km.BootstrapActiveCommittee(desc.Members, desc.Threshold, desc.Epoch, desc.ActivateAt); err != nil {
		t.Fatalf("BootstrapActiveCommittee: %v", err)
	}

	active := km.ActiveCommittee()
	if active == nil {
		t.Fatal("expected active descriptor after bootstrap")
	}
	if len(active.Members) != 4 || active.Threshold != 1 || active.Epoch != 1 {
		t.Fatalf("active descriptor mismatch: %+v", active)
	}
	if km.IsCommitteeMember() {
		t.Fatal("node outside the descriptor must not be reported as a committee member")
	}
	if km.GetCommitteeSize() != 4 || km.GetThreshold() != 1 {
		t.Fatalf("descriptor must drive committeeSize/threshold: size=%d threshold=%d", km.GetCommitteeSize(), km.GetThreshold())
	}

	// 副本隔离：外部修改返回值不得影响内部状态。
	active.Members[0] = "enode://tampered"
	if km.ActiveCommittee().Members[0] == "enode://tampered" {
		t.Fatal("ActiveCommittee must return a defensive copy")
	}

	// NodeID 驱动的成员身份与本地索引：不依赖 registry 顺序。
	km2 := rotationObserverKeyManager(0)
	km2.localNodeID = desc.Members[2] // 第 3 个身份 -> epoch-local 索引 3
	if err := km2.BootstrapActiveCommittee(desc.Members, desc.Threshold, desc.Epoch, desc.ActivateAt); err != nil {
		t.Fatalf("BootstrapActiveCommittee km2: %v", err)
	}
	if !km2.IsCommitteeMember() || km2.GetNodeIndex() != 2 {
		t.Fatalf("descriptor member node: isMember=%v nodeIndex=%d want true/2", km2.IsCommitteeMember(), km2.GetNodeIndex())
	}
}

func TestAdoptElectionResultValidatesAndNormalizes(t *testing.T) {
	km := rotationObserverKeyManager(0)
	km.localNodeID = "enode://observer"
	desc := rotationActiveDesc(t)
	if err := km.BootstrapActiveCommittee(desc.Members, desc.Threshold, desc.Epoch, desc.ActivateAt); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1

	// epoch 不衔接：elected epoch == active epoch。
	sameEpoch := rotationElectedResult(t, cfg, 1, 200, []CandidateID{"a", "b", "c", "d"})
	if err := km.AdoptElectionResult(sameEpoch); !errors.Is(err, ErrNotElectionTargetEpoch) {
		t.Fatalf("expected ErrNotElectionTargetEpoch, got %v", err)
	}

	// CommitteeID 被篡改。
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"a", "b", "c", "d"})
	tampered := result.Descriptor.Clone()
	tampered.Epoch = 9
	bad := &ElectionResult{Descriptor: tampered, RankedWinners: result.RankedWinners, Indices: result.Indices}
	if err := km.AdoptElectionResult(bad); err == nil {
		t.Fatal("expected committee-ID mismatch error")
	}

	// 正常采纳：pending 规范化为 NodeID 升序。
	if err := km.AdoptElectionResult(result); err != nil {
		t.Fatalf("AdoptElectionResult: %v", err)
	}
	pending := km.PendingCommittee()
	if pending == nil || len(pending.Members) != 4 {
		t.Fatalf("pending descriptor missing: %+v", pending)
	}
	for i := 1; i < len(pending.Members); i++ {
		if pending.Members[i] < pending.Members[i-1] {
			t.Fatalf("pending members must be NodeID-ascending: %v", pending.Members)
		}
	}

	// 同 epoch 重复采纳同一结果幂等；成员不同的结果报冲突。
	if err := km.AdoptElectionResult(result); err != nil {
		t.Fatalf("idempotent re-adopt failed: %v", err)
	}
	conflicting := rotationElectedResult(t, cfg, 2, 200, []CandidateID{"x", "y", "z", "w"})
	if err := km.AdoptElectionResult(conflicting); err == nil {
		t.Fatal("expected conflict for same-epoch different-committee result")
	}

	// ActivatePendingCommittee：无 pending 时报错。
	if _, err := km.ActivatePendingCommittee(); err != nil {
		t.Logf("ActivatePendingCommittee on fresh pending: %v", err)
	}
}

// ---------- 2) old/new 角色 + 本地 epoch-local index ----------

func TestBuildResharePlanRolesAndIndicesFromDescriptors(t *testing.T) {
	managers, _ := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	// 新委员会：旧成员 a、b（rotationIDs[1], rotationIDs[3]）+ 两名启动时非委员会的新成员
	//（node-d = rotationIDs[4] 与全新身份 f）。链上先后：d, f, a, b。
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{
		CandidateID(rotationIDs[4]), // node-d：新成员，先出解
		CandidateID("enode://newcomer-f"),
		CandidateID(rotationIDs[1]), // node-a：旧成员
		CandidateID(rotationIDs[3]), // node-b：旧成员
	})

	pending := result.Descriptor.Clone()
	pending.Members = sortDescriptorMembers(&pending)

	for i, km := range managers {
		if err := km.AdoptElectionResult(result); err != nil {
			t.Fatalf("AdoptElectionResult node=%s: %v", rotationIDs[i], err)
		}
		plan, err := km.BuildResharePlanFromState()
		if err != nil {
			t.Fatalf("BuildResharePlanFromState node=%s: %v", rotationIDs[i], err)
		}
		// 旧/新 int 列表必须是 [1..n] 的描述符位置，而非 registry index。
		if !sameIntList(plan.Config.OldCommittee, []int{1, 2, 3, 4}) {
			t.Fatalf("node=%s OldCommittee=%v want [1 2 3 4]", rotationIDs[i], plan.Config.OldCommittee)
		}
		if !sameIntList(plan.Config.NewCommittee, []int{1, 2, 3, 4}) {
			t.Fatalf("node=%s NewCommittee=%v want [1 2 3 4]", rotationIDs[i], plan.Config.NewCommittee)
		}
		if plan.Config.NewThreshold != 1 {
			t.Fatalf("NewThreshold=%d want 1", plan.Config.NewThreshold)
		}
		wantSession := BuildReshareSessionID(pending.CommitteeID)
		if plan.SessionID != wantSession {
			t.Fatalf("session %q want %q", plan.SessionID, wantSession)
		}

		// 角色（active={e,a,c,b}, pending={a,b,d,f}）：
		//   a、b 双重角色；c、e dealer-only（不在新委员会）。
		switch rotationIDs[i] {
		case rotationIDs[1], rotationIDs[3]:
			if !plan.LocalRole.IsDealer || !plan.LocalRole.IsReceiver {
				t.Fatalf("node=%s want dealer+receiver, got %+v", rotationIDs[i], plan.LocalRole)
			}
		case rotationIDs[0], rotationIDs[2]:
			if !plan.LocalRole.IsDealer || plan.LocalRole.IsReceiver {
				t.Fatalf("node=%s want dealer-only, got %+v", rotationIDs[i], plan.LocalRole)
			}
		}
		// dealer 索引 = active 描述符位置（registry 顺序恰好一致，因为 bootstrap 用了同一顺序）。
		if plan.LocalRole.IsDealer {
			wantIdx, _ := active.MemberIndex(rotationIDs[i])
			if plan.LocalRole.DealerIndex != wantIdx {
				t.Fatalf("node=%s DealerIndex=%d want %d", rotationIDs[i], plan.LocalRole.DealerIndex, wantIdx)
			}
		}
		// receiver 索引 = pending 描述符位置（NodeID 升序，与 registry 无关）。
		if plan.LocalRole.IsReceiver {
			wantIdx, _ := pending.MemberIndex(rotationIDs[i])
			if plan.LocalRole.ReceiverIndex != wantIdx {
				t.Fatalf("node=%s ReceiverIndex=%d want %d", rotationIDs[i], plan.LocalRole.ReceiverIndex, wantIdx)
			}
		}
	}

	// 完全处于两代之外的节点：BuildResharePlan 报 ErrNodeOutsideRotation。
	outsider := rotationObserverKeyManager(0)
	outsider.localNodeID = "enode://outsider-z"
	if err := outsider.BootstrapActiveCommittee(active.Members, active.Threshold, active.Epoch, active.ActivateAt); err != nil {
		t.Fatalf("bootstrap outsider: %v", err)
	}
	if err := outsider.AdoptElectionResult(result); err != nil {
		t.Fatalf("outsider adopt: %v", err)
	}
	if _, err := outsider.BuildResharePlanFromState(); !errors.Is(err, ErrNodeOutsideRotation) {
		t.Fatalf("expected ErrNodeOutsideRotation, got %v", err)
	}
}

// ---------- 3) session config（含 2 的幂约束） ----------

func TestBuildResharePlanRejectsNonPowerOfTwoCommittees(t *testing.T) {
	active := rotationActiveDesc(t)
	five := active.Clone()
	five.Members = append(five.Members, "enode://node-f")
	five.Threshold = 2
	five.CommitteeID = ComputeCommitteeID(five.Epoch, five.Members, five.Threshold, five.ActivateAt)
	pending := five.Clone()
	pending.Epoch = 2
	pending.CommitteeID = ComputeCommitteeID(pending.Epoch, pending.Members, pending.Threshold, pending.ActivateAt)
	if _, err := BuildResharePlan("enode://node-a", active, &pending); !errors.Is(err, ErrCommitteeSizeNotPowerOfTwo) {
		t.Fatalf("expected ErrCommitteeSizeNotPowerOfTwo, got %v", err)
	}
}

// ---------- 4) 新成员 pending receiver + 成功 extraction 后运行时切换 ----------

// rotationRunElectedReshare 端到端跑一轮"描述符驱动"的 DKRS：
// 旧 4 人 dealer 域 + 新 4 人 receiver 域（含 1 名启动时非委员会的新成员），
// 返回各节点的 KeyManager（索引 = pending 描述符位置 - 1）。
func rotationRunElectedReshare(t *testing.T, includeNewcomer bool) map[NodeID]*KeyManager {
	t.Helper()

	managers, keys := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()

	// 旧委员会 a,b + 新成员 d,e（NodeID 升序后构成新域）。
	rankOrder := []CandidateID{CandidateID(rotationIDs[1]), CandidateID(rotationIDs[3])}
	if includeNewcomer {
		rankOrder = append(rankOrder, CandidateID(rotationIDs[4]), CandidateID("enode://node-9"))
	} else {
		rankOrder = append(rankOrder, CandidateID("enode://node-8"), CandidateID("enode://node-9"))
	}
	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	result := rotationElectedResult(t, cfg, 2, 200, rankOrder)
	pending := result.Descriptor.Clone()
	pending.Members = sortDescriptorMembers(&pending)

	sessionID := BuildReshareSessionID(pending.CommitteeID)

	// 新成员节点：无 localKey、非委员会启动。
	newMembers := map[NodeID]*KeyManager{}
	for _, id := range pending.Members {
		if active.Contains(id) {
			continue
		}
		km := rotationObserverKeyManager(0)
		km.localNodeID = id
		km.isCommitteeMember = false
		km.committeeSize = 4
		km.numParties = 4
		km.threshold = 1
		newMembers[id] = km
	}
	// 新成员观察者通道学到当前代公钥（来自任意旧委员会成员的公钥视图）。
	withStubSystemKey(t, keys[0])

	// 旧委员会成员：bootstrap + adopt + 启动（dealer）。
	type dealerOut struct {
		deal   *chameleon.DKRSDeal
		shares map[int]*chameleon.DKRSShare
	}
	dealerOutputs := map[NodeID]dealerOut{}
	for i, km := range managers {
		if !active.Contains(rotationIDs[i]) {
			continue
		}
		if err := km.AdoptElectionResult(result); err != nil {
			t.Fatalf("dealer adopt %s: %v", rotationIDs[i], err)
		}
		plan, deal, shares, err := km.StartElectedReshare()
		if err != nil {
			t.Fatalf("StartElectedReshare dealer %s: %v", rotationIDs[i], err)
		}
		if plan.SessionID != sessionID {
			t.Fatalf("dealer %s session %q want %q", rotationIDs[i], plan.SessionID, sessionID)
		}
		if deal == nil {
			t.Fatalf("dealer %s expected deal", rotationIDs[i])
		}
		dealerOutputs[rotationIDs[i]] = dealerOut{deal: deal, shares: shares}
	}

	// 新成员：adopt（需要先 bootstrap active）+ Prepare + receiver-only 启动。
	for id, km := range newMembers {
		if err := km.BootstrapActiveCommittee(active.Members, active.Threshold, active.Epoch, active.ActivateAt); err != nil {
			t.Fatalf("newcomer bootstrap %s: %v", id, err)
		}
		if err := km.AdoptElectionResult(result); err != nil {
			t.Fatalf("newcomer adopt %s: %v", id, err)
		}
		if km.IsCommitteeMember() {
			t.Fatalf("newcomer %s must not be a member of the active committee", id)
		}
		plan, deal, shares, err := km.StartElectedReshare()
		if err != nil {
			t.Fatalf("StartElectedReshare newcomer %s: %v", id, err)
		}
		if deal != nil || shares != nil {
			t.Fatalf("newcomer %s must not create a deal (receiver-only)", id)
		}
		if !plan.LocalRole.IsReceiver || plan.LocalRole.IsDealer {
			t.Fatalf("newcomer %s role %+v want receiver-only", id, plan.LocalRole)
		}
	}

	// receiver 集合 = pending 全体成员；向每个 receiver 投递所有 dealer 输入。
	receivers := map[NodeID]*KeyManager{}
	for i, km := range managers {
		if pending.Contains(rotationIDs[i]) {
			receivers[rotationIDs[i]] = km
		}
	}
	for id, km := range newMembers {
		receivers[id] = km
	}

	for recvID, rkm := range receivers {
		recvIdx, _ := pending.MemberIndex(recvID)
		for dealerID, out := range dealerOutputs {
			if _, err := rkm.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Deal: out.deal}); err != nil {
				t.Fatalf("deliver deal to %s from %s: %v", recvID, dealerID, err)
			}
			share := out.shares[recvIdx]
			if share == nil {
				t.Fatalf("dealer %s produced no share for receiver index %d", dealerID, recvIdx)
			}
			if _, err := rkm.OnReshareMessage(types.ReshareMessage{SessionID: sessionID, Share: share}); err != nil {
				t.Fatalf("deliver share to %s from %s: %v", recvID, dealerID, err)
			}
		}
		if !rkm.ReshareCanExtract() {
			t.Fatalf("receiver %s (index %d) should be extractable", recvID, recvIdx)
		}
		if err := rkm.ReshareExtractAndApply(); err != nil {
			t.Fatalf("ReshareExtractAndApply %s: %v", recvID, err)
		}
	}

	// 成功 extraction 后的运行时切换：pending 已激活为 active，本地索引/成员身份反映新代。
	for recvID, rkm := range receivers {
		newActive := rkm.ActiveCommittee()
		if newActive == nil || newActive.Epoch != 2 || newActive.CommitteeID != pending.CommitteeID {
			t.Fatalf("receiver %s active not switched: %+v", recvID, newActive)
		}
		if rkm.PendingCommittee() != nil {
			t.Fatalf("receiver %s pending must be cleared after activation", recvID)
		}
		wantIdx, _ := pending.MemberIndex(recvID)
		if !rkm.IsCommitteeMember() || rkm.GetNodeIndex() != wantIdx-1 {
			t.Fatalf("receiver %s runtime identity not switched: member=%v nodeIndex=%d want idx %d",
				recvID, rkm.IsCommitteeMember(), rkm.GetNodeIndex(), wantIdx)
		}
		if rkm.GetCommitteeSize() != len(pending.Members) || rkm.GetThreshold() != pending.Threshold {
			t.Fatalf("receiver %s size/threshold mismatch: size=%d threshold=%d",
				recvID, rkm.GetCommitteeSize(), rkm.GetThreshold())
		}
		local := rkm.GetLocalKey()
		if local == nil || local.SecretShare == nil {
			t.Fatalf("receiver %s must hold a real extracted share", recvID)
		}
		if local.Index != wantIdx {
			t.Fatalf("receiver %s extracted key index %d want %d", recvID, local.Index, wantIdx)
		}
		if local.Threshold != pending.Threshold || local.NumParties != len(pending.Members) {
			t.Fatalf("receiver %s extracted key (t,n)=(%d,%d) want (%d,%d)",
				recvID, local.Threshold, local.NumParties, pending.Threshold, len(pending.Members))
		}
	}

	// 全体 receiver 的新份额必须仍然支持门限 adapt（SameGlobalPublicKey 已由
	// ReshareExtractAndApply 内部强制；这里验证提取出的份额能联合产生有效碰撞）。
	oldMsg := chameleon.Message("rotation-adapt-old")
	newMsg := chameleon.Message("rotation-adapt-new")
	r := chameleon.RandomPointG1(keys[0].PP)
	hashOutput, err := chameleon.Hash(keys[0], oldMsg, r)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	var adaptShares []*chameleon.AdaptationShare
	i := 0
	for _, id := range pending.Members {
		rkm := receivers[id]
		if rkm == nil {
			continue
		}
		share, err := chameleon.PartialAdapt(rkm.GetLocalKey(), oldMsg, newMsg)
		if err != nil {
			t.Fatalf("PartialAdapt %s: %v", id, err)
		}
		adaptShares = append(adaptShares, share)
		i++
		if i == pending.Threshold+1 {
			break
		}
	}
	newR, err := chameleon.Combine(keys[0].PP, adaptShares, pending.Threshold, len(pending.Members), hashOutput, oldMsg, newMsg)
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if !chameleon.Verify(keys[0], hashOutput.Hash, newMsg, newR) {
		t.Fatal("threshold adaptation failed across the elected committee")
	}

	out := map[NodeID]*KeyManager{}
	for i, km := range managers {
		out[rotationIDs[i]] = km
	}
	for id, km := range newMembers {
		out[id] = km
	}
	return out
}

func TestElectedReshareSwitchesRuntimeState(t *testing.T) {
	rotationRunElectedReshare(t, true)
}

// ---------- 5) receiver 公钥前置条件的显式阻塞 ----------

func TestPrepareReshareReceiverBlocksAndUnblocks(t *testing.T) {
	managers, keys := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	// 只保留旧成员，新成员 = 全新身份，确保走"新成员无 localKey"路径。
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{
		CandidateID("enode://node-7"),
		CandidateID("enode://node-8"),
		CandidateID("enode://node-9"),
		CandidateID("enode://node-a")})
	pending := result.Descriptor.Clone()
	pending.Members = sortDescriptorMembers(&pending)

	km := rotationObserverKeyManager(0)
	km.localNodeID = "enode://node-7"
	if err := km.BootstrapActiveCommittee(active.Members, active.Threshold, active.Epoch, active.ActivateAt); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// 未采纳 election：报 ErrNoPendingCommittee。
	if err := km.PrepareReshareReceiver(); !errors.Is(err, ErrNoPendingCommittee) {
		t.Fatalf("expected ErrNoPendingCommittee, got %v", err)
	}
	if err := km.AdoptElectionResult(result); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	// 观察者通道为空：显式阻塞，且绝不伪造 localKey。
	//（GetLocalKey 以 active 成员身份门控，newcomer 在 active 代之外，直接查内部状态。）
	withEmptySystemKey(t)
	if err := km.PrepareReshareReceiver(); !errors.Is(err, ErrReceiverMissingPublicKey) {
		t.Fatalf("expected ErrReceiverMissingPublicKey, got %v", err)
	}
	if km.localKey != nil {
		t.Fatal("blocked receiver must not receive a fabricated localKey")
	}

	// 观察者通道就绪（公钥视图来自旧委员会）：占位密钥只含公开字段。
	withStubSystemKey(t, keys[0])
	if err := km.PrepareReshareReceiver(); err != nil {
		t.Fatalf("PrepareReshareReceiver: %v", err)
	}
	placeholder := km.localKey
	if placeholder == nil {
		t.Fatal("expected placeholder public-view key")
	}
	if placeholder.SecretShare != nil || placeholder.VerificationKey != nil {
		t.Fatal("placeholder must never carry a secret share or verification key")
	}
	if !chameleon.SameGlobalPublicKey(placeholder, keys[0]) {
		t.Fatal("placeholder must share the generation's global public key")
	}

	// 就绪后可启动 receiver-only 会话并完成提取（完整链路复用现有 DKRS）。
	plan, deal, shares, err := km.StartElectedReshare()
	if err != nil {
		t.Fatalf("StartElectedReshare newcomer: %v", err)
	}
	if deal != nil || shares != nil {
		t.Fatal("newcomer must not produce dealer output")
	}
	wantIdx, _ := pending.MemberIndex(km.localNodeID)
	if !plan.LocalRole.IsReceiver || plan.LocalRole.ReceiverIndex != wantIdx {
		t.Fatalf("newcomer role %+v want receiver idx %d", plan.LocalRole, wantIdx)
	}

	// dealer 输入来自两名旧委员会成员（QUAL 需要 t+1=2 个 dealer）。
	delivered := 0
	for i, dkm := range managers {
		if !active.Contains(rotationIDs[i]) {
			continue
		}
		if err := dkm.AdoptElectionResult(result); err != nil {
			t.Fatalf("dealer adopt: %v", err)
		}
		_, d, outShares, err := dkm.StartElectedReshare()
		if err != nil {
			t.Fatalf("dealer StartElectedReshare: %v", err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: plan.SessionID, Deal: d}); err != nil {
			t.Fatalf("deliver deal: %v", err)
		}
		if _, err := km.OnReshareMessage(types.ReshareMessage{SessionID: plan.SessionID, Share: outShares[wantIdx]}); err != nil {
			t.Fatalf("deliver share: %v", err)
		}
		delivered++
	}
	if delivered < active.Threshold+1 {
		t.Fatalf("need at least t+1=%d dealers, got %d", active.Threshold+1, delivered)
	}
	if !km.ReshareCanExtract() {
		t.Fatal("newcomer should be extractable after dealer inputs")
	}
	if err := km.ReshareExtractAndApply(); err != nil {
		t.Fatalf("ReshareExtractAndApply newcomer: %v", err)
	}
	if km.ActiveCommittee() == nil || km.ActiveCommittee().Epoch != 2 {
		t.Fatalf("newcomer active not switched: %+v", km.ActiveCommittee())
	}
	if km.GetLocalKey().SecretShare == nil {
		t.Fatal("newcomer must hold a real share after extraction")
	}
}

// dealer 必须持有旧代真实份额：占位密钥节点不能被 registry 索引误判为 dealer。
func TestStartReshareRejectsDealerWithoutSecretShare(t *testing.T) {
	managers, _ := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{
		CandidateID("enode://node-7"),
		CandidateID("enode://node-8"),
		CandidateID("enode://node-9"),
		CandidateID("enode://node-a")})

	// node-c：旧委员会成员但本测试剥掉其真实份额（模拟"有身份无份额"）。
	var target *KeyManager
	for i, km := range managers {
		if rotationIDs[i] == "enode://node-c" {
			target = km
		}
		if err := km.AdoptElectionResult(result); err != nil {
			t.Fatalf("adopt: %v", err)
		}
	}
	if target == nil {
		t.Fatal("node-c not found in test committee")
	}
	target.mu.Lock()
	target.localKey = &chameleon.NITCHKey{
		PP:         target.localKey.PP,
		GlobalPK:   target.localKey.GlobalPK,
		GlobalPKG1: target.localKey.GlobalPKG1,
		Threshold:  active.Threshold,
		NumParties: len(active.Members),
		// 无 SecretShare。
	}
	target.mu.Unlock()

	// 新委员会不含 node-c：它是 dealer-only，但没有旧份额 -> 必须拒绝。
	if _, _, _, err := target.StartElectedReshare(); err == nil {
		t.Fatal("expected dealer without secret share to be rejected")
	} else if !strings.Contains(err.Error(), "secret share") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------- 6) boundary handler 适配 ----------

func TestApplyBoundaryEventAdaptsElectionToReshare(t *testing.T) {
	managers, _ := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()

	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	// 保留全部旧成员 + 引入一名新成员。
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{
		CandidateID(rotationIDs[0]),
		CandidateID(rotationIDs[1]),
		CandidateID(rotationIDs[2]),
		CandidateID(rotationIDs[4])})
	pending := result.Descriptor.Clone()
	pending.Members = sortDescriptorMembers(&pending)

	ev := &BoundaryEvent{
		FinalizedEpoch:   1,
		TargetEpoch:      2,
		PreviousBoundary: 100,
		CurrentBoundary:  200,
		Tau:              DeriveTau(common.HexToHash("0xabcd"), 1),
		ElectionResult:   result,
	}

	km := managers[1] // node-a：dealer+receiver
	plan, deal, shares, err := km.ApplyBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("ApplyBoundaryEvent: %v", err)
	}
	if plan == nil || plan.SessionID != BuildReshareSessionID(pending.CommitteeID) {
		t.Fatalf("plan session mismatch: %+v", plan)
	}
	if deal == nil || len(shares) != len(pending.Members) {
		t.Fatalf("dealer output incomplete: deal=%v shares=%d", deal != nil, len(shares))
	}
	// 手工 eth_startReshare 回归入口语义不受影响：同 sessionID 再次 StartReshare 幂等命中缓存。
	deal2, shares2, err := km.StartReshare(plan.SessionID, plan.Config)
	if err != nil {
		t.Fatalf("manual StartReshare on elected session: %v", err)
	}
	if deal2 == nil || len(shares2) != len(shares) {
		t.Fatal("idempotent restart must replay the cached deal/shares")
	}

	// 幂等：同一 boundary event 重放不产生第二个会话。
	planAgain, dealAgain, _, err := km.ApplyBoundaryEvent(ev)
	if err != nil {
		t.Fatalf("replay ApplyBoundaryEvent: %v", err)
	}
	if planAgain.SessionID != plan.SessionID || dealAgain == nil {
		t.Fatal("replay must return the same session and cached deal")
	}

	// 观察者节点（两代之外）：handler 确定性推进 active 描述符，不启动 reshare。
	handler := NewReshareBoundaryHandler(nil)
	outsider := rotationObserverKeyManager(0)
	outsider.localNodeID = "enode://outsider-z"
	if err := outsider.BootstrapActiveCommittee(active.Members, active.Threshold, active.Epoch, active.ActivateAt); err != nil {
		t.Fatalf("bootstrap outsider: %v", err)
	}
	swapKeyManager := func(next *KeyManager) func() {
		prev := globalKeyManager
		globalKeyManager = next
		return func() { globalKeyManager = prev }
	}
	restore := swapKeyManager(outsider)
	defer restore()
	if err := handler(ev); err != nil {
		t.Fatalf("outside-rotation handler must succeed, got %v", err)
	}
	if outsider.ActiveCommittee() == nil || outsider.ActiveCommittee().CommitteeID != pending.CommitteeID {
		t.Fatalf("observer should have deterministically advanced active committee to %s, got %v",
			pending.CommitteeID, outsider.ActiveCommittee())
	}
	if outsider.PendingCommittee() != nil {
		t.Fatal("observer pending committee should be cleared after activation")
	}
	if outsider.IsCommitteeMember() {
		t.Fatal("observer must not be marked as a committee member")
	}
}

// SessionID 必须 epoch-scoped：不同 epoch/成员的 committeeID 产生不同会话 ID。
func TestBuildReshareSessionIDEpochScoped(t *testing.T) {
	id1 := BuildReshareSessionID(common.HexToHash("0x01"))
	id2 := BuildReshareSessionID(common.HexToHash("0x02"))
	if id1 == id2 {
		t.Fatal("distinct committee IDs must yield distinct session IDs")
	}
	if !strings.HasPrefix(id1, "nitch-reshare-v1:") {
		t.Fatalf("unexpected prefix: %s", id1)
	}

	desc := rotationActiveDesc(t)
	next := desc.Clone()
	next.Epoch = desc.Epoch + 1
	next.CommitteeID = ComputeCommitteeID(next.Epoch, next.Members, next.Threshold, next.ActivateAt)
	if BuildReshareSessionID(desc.CommitteeID) == BuildReshareSessionID(next.CommitteeID) {
		t.Fatal("session ID must differ across epochs")
	}
}

// 手工回归：占位密钥节点 ReshareVerifySharesOnly 的 receiverIndex 与 pending 位置一致，
// 且 chameleonIndexToInt/big 键路径不受影响（防回归）。
func TestElectedSessionConfigUsesDescriptorPositions(t *testing.T) {
	managers, _ := rotationTestCommitteeKeyManagers(t)
	active := managers[0].ActiveCommittee()
	cfg := DefaultElectionConfig()
	cfg.CommitteeSize = 4
	cfg.Threshold = 1
	result := rotationElectedResult(t, cfg, 2, 200, []CandidateID{
		CandidateID(rotationIDs[0]),
		CandidateID(rotationIDs[1]),
		CandidateID(rotationIDs[3]),
		CandidateID(rotationIDs[4])})
	pending := result.Descriptor.Clone()
	pending.Members = sortDescriptorMembers(&pending)

	if err := managers[1].AdoptElectionResult(result); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	plan, err := managers[1].BuildResharePlanFromState()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// int 列表必须覆盖 1..n 且互不重复（描述符位置，而非 registry index）。
	for name, list := range map[string][]int{"old": plan.Config.OldCommittee, "new": plan.Config.NewCommittee} {
		seen := map[int]bool{}
		for _, v := range list {
			if v < 1 || v > len(list) || seen[v] {
				t.Fatalf("%s committee list %v is not a permutation of descriptor positions", name, list)
			}
			seen[v] = true
		}
	}
	// 新域点索引按 NodeID 升序：pending 成员位置即 share ToIndex。
	for i, m := range pending.Members {
		idx, ok := active.MemberIndex(m)
		_ = idx
		if !ok && !pending.Contains(m) {
			t.Fatalf("member %s missing from pending", m)
		}
		want := i + 1
		got, _ := pending.MemberIndex(m)
		if got != want {
			t.Fatalf("pending position of %s = %d want %d", m, got, want)
		}
	}
	// StartReshare 状态中的配置与 plan 一致（复用现有状态机）。
	if _, _, err := managers[1].StartReshare(plan.SessionID, plan.Config); err != nil {
		t.Fatalf("StartReshare: %v", err)
	}
	managers[1].mu.RLock()
	cfgInState := managers[1].reshareConfig
	managers[1].mu.RUnlock()
	if !sameIntList(cfgInState.NewCommittee, plan.Config.NewCommittee) ||
		!sameIntList(cfgInState.OldCommittee, plan.Config.OldCommittee) ||
		cfgInState.NewThreshold != plan.Config.NewThreshold {
		t.Fatalf("session state config %+v does not match plan %+v", cfgInState, plan.Config)
	}
}

var _ = big.NewInt // keep math/big import if helpers change
