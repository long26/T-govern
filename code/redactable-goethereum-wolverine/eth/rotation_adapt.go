package eth

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// Phase 3A：运行时 CommitteeDescriptor/KeyManager 状态接入 + boundary election 到现有
// reshare 的适配层（docs/nitch_pow_rotation_design.md §3/§8）。
//
// 设计约束：
//   - node_registry 只做身份发现；rotation 不读取/写入其 committee 字段；
//   - ReshareSessionConfig 的 int 列表必须来自 active/pending descriptor 的成员位置，
//     不能使用 registry index；
//   - 旧 key、StartReshare、advanceReshare、ReshareCanExtract、ReshareExtractAndApply、
//     SameGlobalPublicKey 生命周期全部复用，不改 DKRS complaint/QUAL；
//   - 不伪造 share，不绕过现有密码学。
//
// 不在本阶段范围：P2P route 按 descriptor 重建（§9）、chain observer 生命周期（§10）、
// PoW worker。

var (
	// ErrNoActiveCommittee 表示尚未设置运行时 Active 委员会描述符（需要先 bootstrap）。
	ErrNoActiveCommittee = errors.New("no active committee descriptor")

	// ErrNoPendingCommittee 表示尚未设置运行时 Pending 委员会描述符。
	ErrNoPendingCommittee = errors.New("no pending committee descriptor")

	// ErrNotElectionTargetEpoch 表示选举结果的 epoch 不晚于 active 描述符的下一epoch。
	ErrNotElectionTargetEpoch = errors.New("elected epoch does not follow the active committee epoch")

	// ErrNodeOutsideRotation 表示本节点既不在旧委员会也不在新委员会。
	ErrNodeOutsideRotation = errors.New("node is outside the current committee rotation")

	// ErrReceiverMissingPublicKey 表示新成员尚无当前代的 PP/GlobalPK 公钥视图，无法安全
	// 进入 DKRS 提取前置条件。这是显式阻塞而不是绕过：占位密钥只能携带观察者通道
	// （由广播的 DistKG Deal/Complaint 推导的 system key）学到的公开信息，绝不携带
	// SecretShare；学不到就阻塞，直到观察者通道就绪。
	ErrReceiverMissingPublicKey = errors.New("reshare receiver lacks the current-generation public key view")

	// ErrCommitteeSizeNotPowerOfTwo 表示描述符规模不满足 DKRS 域（2 的幂）约束。
	ErrCommitteeSizeNotPowerOfTwo = errors.New("committee size must be a power of two for DKRS")
)

// systemReshareKeySource 是 PrepareReshareReceiver 的观察者公钥来源，默认为
// chameleon.GetSystemNITCHKey。抽出来只为可测试性（阻塞路径需要确定性的空通道）。
var systemReshareKeySource = chameleon.GetSystemNITCHKey

// ResharePlan 是 boundary election 之后、StartReshare 之前的完整会话规划：
// 由 pending（elected）与 active（旧）委员会描述符确定性派生。
type ResharePlan struct {
	// SessionID 是 epoch-scoped 会话 ID（见 BuildReshareSessionID）。
	SessionID string
	// Config 是适配后的 ReshareSessionConfig；int 列表取自各 descriptor 的成员位置
	// （1..len(Members)），与 registry index 无关。
	Config ReshareSessionConfig
	// OldDescriptor / NextDescriptor 分别是本次会话 dealer 侧与 receiver 侧的描述符副本。
	OldDescriptor  CommitteeDescriptor
	NextDescriptor CommitteeDescriptor
	// LocalRole 是本节点在同一 NodeID 口径下解释 old/new 描述符得到的角色。
	LocalRole ReshareRole
}

// descriptorIndexList 把 descriptor 成员位置展开为 DKRS 用的 1-based 索引列表
// [1..n]。Members 的顺序即本 epoch 的索引顺序：
//   - pending/elected descriptor 按 NodeID 升序规范化（adoptElection 时），
//     对输入排列不敏感；
//   - bootstrap active descriptor 必须按"当前代既有 DKRS 索引"顺序提供成员
//     （见 BootstrapActiveCommittee），因此这里不做二次排序——排序会破坏旧代索引复现。
func descriptorIndexList(desc *CommitteeDescriptor) []int {
	if desc == nil || len(desc.Members) == 0 {
		return nil
	}
	out := make([]int, len(desc.Members))
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// sortDescriptorMembers 返回按 NodeID 升序的成员副本（设计 §3.2：elected descriptor
// 的 DKRS 索引由 NodeID 升序重新生成）。
// sameIntList 报告两个 int 列表是否逐元素相等。
func sameIntList(a, b []int) bool {
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

func sortDescriptorMembers(desc *CommitteeDescriptor) []NodeID {
	if desc == nil || len(desc.Members) == 0 {
		return nil
	}
	sorted := make([]NodeID, len(desc.Members))
	copy(sorted, desc.Members)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
}

// roleForDescriptors 在 NodeID 身份口径下计算 Dealer/Receiver 角色。
// old/new 索引均取自各 descriptor 的成员位置，与 km.nodeIndex（registry 顺序）无关。
func roleForDescriptors(localNodeID NodeID, oldDesc, nextDesc *CommitteeDescriptor) (ReshareRole, error) {
	var role ReshareRole
	if localNodeID == "" {
		return role, errors.New("local NodeID is not set")
	}
	if oldDesc != nil {
		if idx, ok := oldDesc.MemberIndex(localNodeID); ok {
			role.IsDealer = true
			role.DealerIndex = idx
		}
	}
	if nextDesc != nil {
		if idx, ok := nextDesc.MemberIndex(localNodeID); ok {
			role.IsReceiver = true
			role.ReceiverIndex = idx
		}
	}
	if !role.IsDealer && !role.IsReceiver {
		return role, fmt.Errorf("%w: %s", ErrNodeOutsideRotation, localNodeID)
	}
	return role, nil
}

func validatePlanDescriptor(desc *CommitteeDescriptor, name string) error {
	if desc == nil {
		return fmt.Errorf("%s descriptor is nil", name)
	}
	if len(desc.Members) == 0 {
		return fmt.Errorf("%s descriptor has no members", name)
	}
	if desc.Threshold < 0 || desc.Threshold >= len(desc.Members) {
		return fmt.Errorf("%s descriptor threshold %d invalid for %d members", name, desc.Threshold, len(desc.Members))
	}
	return nil
}

func isPowerOfTwoCommitteeSize(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// BuildReshareSessionID 生成 epoch-scoped 的 DKRS 会话 ID：
//
//	"nitch-reshare-v1:" || hex(committeeID)
//
// committeeID 本身是 epoch/threshold/activateAt/members 的确定性 Keccak（见
// ComputeCommitteeID），因此同一 election 结果在所有节点得到相同 sessionID（复用
// StartReshare 的幂等/缓存键），不同 epoch 的会话天然不同。
func BuildReshareSessionID(committeeID common.Hash) string {
	return "nitch-reshare-v1:" + hexutil.Encode(committeeID.Bytes())[2:]
}

// BuildResharePlan 把 pending（elected）与 active（旧）描述符适配成现有 StartReshare
// 所需的 ReshareSessionConfig。纯函数，不触碰 KeyManager 锁。
//
//   - OldCommittee = active descriptor 的成员位置 1..n（dealer 身份域，复现旧代索引）；
//   - NewCommittee = pending descriptor 的成员位置 1..n（receiver 身份域，位置即新域点索引）；
//   - NewThreshold = pending descriptor 的 Threshold；
//   - OldThreshold / OldQual 仍由 KeyManager 从 localKey 与 reshareRequiredQualLocked()
//     确定性派生（现状保持，不新增调用方输入）。
func BuildResharePlan(localNodeID NodeID, active, pending *CommitteeDescriptor) (*ResharePlan, error) {
	if err := validatePlanDescriptor(active, "active"); err != nil {
		return nil, err
	}
	if err := validatePlanDescriptor(pending, "pending"); err != nil {
		return nil, err
	}
	if !isPowerOfTwoCommitteeSize(len(active.Members)) || !isPowerOfTwoCommitteeSize(len(pending.Members)) {
		return nil, fmt.Errorf("%w: old=%d new=%d", ErrCommitteeSizeNotPowerOfTwo, len(active.Members), len(pending.Members))
	}
	role, err := roleForDescriptors(localNodeID, active, pending)
	if err != nil {
		return nil, err
	}
	return &ResharePlan{
		SessionID: BuildReshareSessionID(pending.CommitteeID),
		Config: ReshareSessionConfig{
			OldCommittee: descriptorIndexList(active),
			NewCommittee: descriptorIndexList(pending),
			NewThreshold: pending.Threshold,
		},
		OldDescriptor:  active.Clone(),
		NextDescriptor: pending.Clone(),
		LocalRole:      role,
	}, nil
}

// BootstrapActiveCommittee 为"从既有 registry/ DistKG 代"接入运行时轮换构造首个
// active 描述符。membersInIndexOrder 必须按当前代既有 DKRS 索引顺序给出（第 i 个
// 成员持有旧索引 i+1 的份额），来源限于身份发现；本函数不读取 node_registry 的
// committee 字段。threshold 优先取本节点已完成 DistKG 的 localKey（与 dealer 侧
// CreateDKRSDeal 使用的 oldThreshold 一致），未提供时用调用方显式值。
func (km *KeyManager) BootstrapActiveCommittee(membersInIndexOrder []NodeID, threshold int, epoch uint64, activateAt uint64) error {
	if km == nil {
		return errors.New("nil KeyManager")
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if km.localKey != nil && km.localKey.SecretShare != nil {
		threshold = km.localKey.Threshold
	}
	desc, err := BootstrapActiveCommitteeDesc(membersInIndexOrder, threshold, epoch, activateAt)
	if err != nil {
		return err
	}
	km.setActiveCommitteeLocked(desc)
	log.Info("Bootstrapped active committee descriptor",
		"epoch", desc.Epoch, "size", len(desc.Members), "threshold", desc.Threshold, "id", desc.CommitteeID)
	return nil
}

// BootstrapActiveCommitteeDesc 是 BootstrapActiveCommittee 的纯函数核心。
func BootstrapActiveCommitteeDesc(membersInIndexOrder []NodeID, threshold int, epoch uint64, activateAt uint64) (*CommitteeDescriptor, error) {
	if len(membersInIndexOrder) == 0 {
		return nil, errors.New("bootstrap committee needs at least one member")
	}
	if threshold < 0 || threshold >= len(membersInIndexOrder) {
		return nil, fmt.Errorf("bootstrap threshold %d invalid for %d members", threshold, len(membersInIndexOrder))
	}
	seen := make(map[NodeID]bool, len(membersInIndexOrder))
	for _, m := range membersInIndexOrder {
		if m == "" {
			return nil, errors.New("bootstrap committee contains an empty NodeID")
		}
		if seen[m] {
			return nil, fmt.Errorf("bootstrap committee contains duplicate NodeID %s", m)
		}
		seen[m] = true
	}
	members := make([]NodeID, len(membersInIndexOrder))
	copy(members, membersInIndexOrder)
	return &CommitteeDescriptor{
		Epoch:       epoch,
		Members:     members,
		Threshold:   threshold,
		CommitteeID: ComputeCommitteeID(epoch, members, threshold, activateAt),
		ActivateAt:  activateAt,
	}, nil
}

// AdoptElectionResult 将 boundary 选举结果落为运行时 Pending 委员会（不激活）。
// 校验：descriptor 完整、CommitteeID 与内容一致（防篡改/错配）、epoch 单调衔接。
func (km *KeyManager) AdoptElectionResult(result *ElectionResult) error {
	if km == nil {
		return errors.New("nil KeyManager")
	}
	if result == nil {
		return errors.New("nil election result")
	}
	desc := result.Descriptor
	if err := validatePlanDescriptor(&desc, "elected"); err != nil {
		return err
	}
	if want := ComputeCommitteeID(desc.Epoch, desc.Members, desc.Threshold, desc.ActivateAt); want != desc.CommitteeID {
		return fmt.Errorf("elected committee ID mismatch: have %s want %s", desc.CommitteeID, want)
	}
	km.mu.Lock()
	defer km.mu.Unlock()
	if km.activeCommittee == nil {
		return ErrNoActiveCommittee
	}
	if desc.Epoch <= km.activeCommittee.Epoch {
		return fmt.Errorf("%w: active=%d elected=%d", ErrNotElectionTargetEpoch, km.activeCommittee.Epoch, desc.Epoch)
	}
	if desc.Epoch > km.activeCommittee.Epoch+1 {
		log.Warn("Election adopted across skipped epochs", "active", km.activeCommittee.Epoch, "elected", desc.Epoch)
	}
	if km.pendingCommittee != nil && desc.Epoch == km.pendingCommittee.Epoch && desc.CommitteeID != km.pendingCommittee.CommitteeID {
		return fmt.Errorf("conflicting election result for pending epoch %d: have %s want %s",
			desc.Epoch, km.pendingCommittee.CommitteeID, desc.CommitteeID)
	}
	// Pending 规范化为 NodeID 升序：descriptor 成员位置即 DKRS 新域点索引（设计 §3.2）。
	canonical := desc.Clone()
	canonical.Members = sortDescriptorMembers(&canonical)
	km.pendingCommittee = &canonical
	km.lastElectionEpoch = canonical.Epoch
	km.lastBoundary = canonical.ActivateAt
	return nil
}

// BuildResharePlanFromState 基于当前 active/pending 描述符与本地 NodeID 构造会话规划。
func (km *KeyManager) BuildResharePlanFromState() (*ResharePlan, error) {
	if km == nil {
		return nil, errors.New("nil KeyManager")
	}
	km.mu.RLock()
	localID := km.localNodeIDLocked()
	var active, pending *CommitteeDescriptor
	if km.activeCommittee != nil {
		cp := km.activeCommittee.Clone()
		active = &cp
	}
	if km.pendingCommittee != nil {
		cp := km.pendingCommittee.Clone()
		pending = &cp
	}
	km.mu.RUnlock()
	return BuildResharePlan(localID, active, pending)
}

// PrepareReshareReceiver 补齐新成员（启动时非委员会、被 election 选中）进入 pending
// receiver 所需的 localKey 前置条件。
//
// 占位 localKey 只携带公钥视图（PP/GlobalPK/GlobalPKG1/Threshold/NumParties），来源
// 限定为真实 public params 或观察者通道（systemReshareKeySource）：
//   - 绝不设置 SecretShare——新成员的真实份额只能来自后续 ReshareExtractAndApply；
//   - 与 active 描述符的 (t,n) 必须一致，防止学到上一代/下一代公钥；
//   - 老委员会成员已有真实份额，直接通过；
//   - 真实 public params 或观察者通道均不可用时返回 ErrReceiverMissingPublicKey（显式阻塞，不绕过密码学）。
func (km *KeyManager) PrepareReshareReceiver() error {
	if km == nil {
		return errors.New("nil KeyManager")
	}
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.pendingCommittee == nil {
		return ErrNoPendingCommittee
	}
	localID := km.localNodeIDLocked()
	if localID == "" {
		return errors.New("local NodeID is not set")
	}
	if _, ok := km.pendingCommittee.MemberIndex(localID); !ok {
		return fmt.Errorf("%w: %s", ErrNodeOutsideRotation, localID)
	}
	if km.localKey != nil && km.localKey.SecretShare != nil {
		return nil // 老委员会成员（或已提取过的节点）已具备真实份额
	}

	// 若本地已有真实 public params 与合法 GlobalPK（无假 secret share），直接允许 pending receiver 就绪
	if km.localKey != nil && km.localKey.GlobalPK != nil && km.localKey.GlobalPKG1 != nil && (km.localKey.PP != nil || km.publicParams != nil) {
		if km.publicParams == nil {
			km.publicParams = km.localKey.PP
		}
		if km.localKey.PP == nil {
			km.localKey.PP = km.publicParams
		}
		if km.activeCommittee != nil {
			if km.localKey.Threshold != km.activeCommittee.Threshold || km.localKey.NumParties != len(km.activeCommittee.Members) {
				return fmt.Errorf("local public key (t=%d,n=%d) does not match active committee (t=%d,n=%d)",
					km.localKey.Threshold, km.localKey.NumParties, km.activeCommittee.Threshold, len(km.activeCommittee.Members))
			}
		}
		log.Info("Reshare receiver prepared from existing local public key and params",
			"node", localID, "threshold", km.localKey.Threshold, "numParties", km.localKey.NumParties)
		return nil
	}

	sysKey, err := systemReshareKeySource()
	if err != nil {
		return ErrReceiverMissingPublicKey
	}
	if sysKey == nil || sysKey.PP == nil || sysKey.GlobalPK == nil || sysKey.GlobalPKG1 == nil {
		return ErrReceiverMissingPublicKey
	}
	if km.activeCommittee != nil {
		if sysKey.Threshold != km.activeCommittee.Threshold || sysKey.NumParties != len(km.activeCommittee.Members) {
			return fmt.Errorf("observed public key (t=%d,n=%d) does not match active committee (t=%d,n=%d)",
				sysKey.Threshold, sysKey.NumParties, km.activeCommittee.Threshold, len(km.activeCommittee.Members))
		}
	}
	if km.localKey != nil && !chameleon.SameGlobalPublicKey(km.localKey, sysKey) {
		return ErrReceiverMissingPublicKey
	}
	if km.publicParams == nil {
		km.publicParams = sysKey.PP
	}
	if km.localKey == nil {
		km.localKey = &chameleon.NITCHKey{
			PP:         km.publicParams,
			GlobalPK:   sysKey.GlobalPK,
			GlobalPKG1: sysKey.GlobalPKG1,
			Threshold:  sysKey.Threshold,
			NumParties: sysKey.NumParties,
			// 无 SecretShare / VerificationKey / Index：仅公钥视图占位。
		}
		log.Info("Reshare receiver prepared from observed system public key",
			"node", localID, "threshold", sysKey.Threshold, "numParties", sysKey.NumParties)
	}
	return nil
}

// ApplyBoundaryEvent 是 boundary election 到现有 reshare 的适配入口：
//
//	ElectionResult → PendingCommittee → ReshareSessionConfig → StartReshare（复用）
//
// 返回会话规划与本节点 Deal/Shares（调用方用既有 BroadcastReshare / SendReshareToIndex /
// startReshareRetryLoop 推进；本层不触碰 P2P route 构建）。完全处于两代委员会之外的
// 观察者返回 ErrNodeOutsideRotation（其 pending 状态仍会被采纳，用于跟踪链上进度）。
func (km *KeyManager) ApplyBoundaryEvent(ev *BoundaryEvent) (*ResharePlan, *chameleon.DKRSDeal, map[int]*chameleon.DKRSShare, error) {
	if km == nil {
		return nil, nil, nil, errors.New("nil KeyManager")
	}
	if ev == nil {
		return nil, nil, nil, errors.New("nil boundary event")
	}
	if ev.Err != nil {
		return nil, nil, nil, fmt.Errorf("boundary event carries error: %w", ev.Err)
	}
	if ev.ElectionResult == nil {
		return nil, nil, nil, errors.New("boundary event carries no election result")
	}
	if err := km.AdoptElectionResult(ev.ElectionResult); err != nil {
		return nil, nil, nil, err
	}
	km.mu.Lock()
	km.lastElectionEpoch = ev.TargetEpoch
	km.lastBoundary = ev.CurrentBoundary
	km.lastPreviousBoundary = ev.PreviousBoundary
	if (ev.NextTau != common.Hash{}) {
		km.lastTau = ev.NextTau
	} else {
		km.lastTau = ev.Tau
	}
	km.lastNextTau = ev.NextTau
	km.lastSolutionCount = len(ev.ValidSolutions)
	km.mu.Unlock()
	return km.StartElectedReshare()
}

// StartElectedReshare 在 pending 描述符已落的前提下直接启动已选举会话
// （ApplyBoundaryEvent 的无 BoundaryEvent 变体，便于编排层与测试复用）。
func (km *KeyManager) StartElectedReshare() (*ResharePlan, *chameleon.DKRSDeal, map[int]*chameleon.DKRSShare, error) {
	if km == nil {
		return nil, nil, nil, errors.New("nil KeyManager")
	}
	plan, err := km.BuildResharePlanFromState()
	if err != nil {
		return nil, nil, nil, err
	}
	if plan.LocalRole.IsReceiver {
		if err := km.PrepareReshareReceiver(); err != nil {
			// 状态与适配层已就绪（pending 已落），但本节点缺少公钥视图：明确阻塞。
			return plan, nil, nil, err
		}
	}
	deal, shares, err := km.StartReshare(plan.SessionID, plan.Config)
	if err != nil {
		return plan, nil, nil, err
	}
	return plan, deal, shares, nil
}

// ActivateObservedCommittee 供普通观察者（既非 dealer 也非 receiver）在同一 boundary election event 后，
// 将 pending descriptor 确定性推进为 active，且不启动/等待 reshare。
//
// 观察者既不持有上一代 secret share，也不参与 DKRS 生成新份额；
// 本方法严禁假设/生成 secret share，严禁激活本地 NITCH 密钥，严禁将 observer 变为 committee 成员。
// 仅安全同步活跃描述符、纪元与相关元数据，以保证下一轮 election/reshare 能正确从 active descriptor 得到 OldCommittee。
func (km *KeyManager) ActivateObservedCommittee() (*CommitteeDescriptor, error) {
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

	// 严格遵循观察者安全边界：
	// 1. 不假设/生成 secret share；
	// 2. 不激活本地 NITCH key（保留原有 localKey 不变或仅公钥视图）；
	// 3. 不将观察者误判为委员会成员；
	if km.localKey == nil || km.localKey.SecretShare == nil {
		km.isCommitteeMember = false
		km.nodeIndex = -1
	}

	cp := desc.Clone()
	log.Info("Observer active committee deterministically advanced",
		"epoch", cp.Epoch, "committeeID", cp.CommitteeID, "members", len(cp.Members), "threshold", cp.Threshold)
	return &cp, nil
}

// ObserveBoundaryActivation 是普通观察者处理 boundary election 的明确入口。
// 若传入 ev != nil，则先采纳选举结果并记录 boundary 元数据，随后将 pending descriptor 确定性推进为 active；
// 若 ev == nil，则直接将当前已有的 pending descriptor 推进为 active。
func (km *KeyManager) ObserveBoundaryActivation(ev *BoundaryEvent) (*CommitteeDescriptor, error) {
	if km == nil {
		return nil, errors.New("nil KeyManager")
	}
	if ev != nil {
		if ev.Err != nil {
			return nil, fmt.Errorf("boundary event carries error: %w", ev.Err)
		}
		if ev.ElectionResult != nil {
			if err := km.AdoptElectionResult(ev.ElectionResult); err != nil {
				return nil, err
			}
		}
		km.mu.Lock()
		km.lastElectionEpoch = ev.TargetEpoch
		km.lastBoundary = ev.CurrentBoundary
		km.lastPreviousBoundary = ev.PreviousBoundary
		if (ev.NextTau != common.Hash{}) {
			km.lastTau = ev.NextTau
		} else {
			km.lastTau = ev.Tau
		}
		km.lastNextTau = ev.NextTau
		km.lastSolutionCount = len(ev.ValidSolutions)
		km.mu.Unlock()
	}
	return km.ActivateObservedCommittee()
}

// NewReshareBoundaryHandler 返回接入 BoundaryCoordinator 的 handler：把 boundary
// election 适配为运行时状态更新 + StartReshare 会话启动 + 既有广播/重试机制。
// 只复用现有 transport 接口（BroadcastReshare / startReshareRetryLoop），不修改
// P2P route 的构建逻辑（§9 属后续阶段）。手工 eth_startReshare 回归入口保持不变。
func NewReshareBoundaryHandler(pm *ProtocolManager) BoundaryHandler {
	return func(ev *BoundaryEvent) error {
		km := GetKeyManager()
		if km == nil {
			return errors.New("KeyManager not initialized")
		}
		plan, deal, shares, err := km.ApplyBoundaryEvent(ev)
		if err != nil {
			if errors.Is(err, ErrNodeOutsideRotation) {
				log.Debug("Boundary rotation skipped: node outside committees", "boundary", ev.CurrentBoundary)
				// 普通观察者既不是旧 ActiveCommittee dealer、也不是新 PendingCommittee receiver。
				// 在同一 boundary election event 后也必须把 pending descriptor 确定性推进为 active，
				// 且不启动/等待 reshare；更新 committee descriptor/epoch/route-visible state。
				if _, actErr := km.ActivateObservedCommittee(); actErr != nil {
					log.Error("Observer failed to activate observed committee", "boundary", ev.CurrentBoundary, "err", actErr)
					return actErr
				}
				if pm != nil {
					pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
				}
				return nil
			}
			if errors.Is(err, ErrReceiverMissingPublicKey) {
				log.Warn("Boundary rotation blocked: receiver public key unavailable",
					"boundary", ev.CurrentBoundary, "err", err)
				return err
			}
			log.Error("Boundary rotation failed", "boundary", ev.CurrentBoundary, "err", err)
			return err
		}
		log.Info("Elected reshare session started",
			"session", plan.SessionID, "epoch", plan.NextDescriptor.Epoch,
			"committeeID", plan.NextDescriptor.CommitteeID,
			"dealer", plan.LocalRole.IsDealer, "receiver", plan.LocalRole.IsReceiver)
		if pm != nil {
			pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
		}
		if pm == nil {
			return nil
		}
		if deal != nil {
			succ, fail := pm.BroadcastReshare(types.ReshareMessage{SessionID: plan.SessionID, Deal: deal})
			log.Info("Elected reshare deal broadcast", "session", plan.SessionID,
				"epoch", plan.NextDescriptor.Epoch, "success", succ, "failed", fail)
		}
		if deal != nil && shares != nil && km.ReshareLocalDealerRequired() {
			for _, sh := range shares {
				pm.BroadcastReshare(types.ReshareMessage{SessionID: plan.SessionID, Share: sh})
			}
		}
		pm.startReshareRetryLoop(plan.SessionID)
		return nil
	}
}
