package eth

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type reshareRequest struct {
	SessionID    string `json:"sessionId"`
	NewThreshold int    `json:"newThreshold"`
	OldCommittee []int  `json:"oldCommittee"`
	NewCommittee []int  `json:"newCommittee"`
}

// ProposeModification 提交区块/交易编辑申请（Wolverine 论文 Protocol 2: Propose a redaction instruction）
//
// 流程：1) 解析提案 2) Redaction Policy 验证 3) 锻造新 r' 4) 广播 ModifyMsg 5) 本地更新
// 提案格式 JSON: {"BlockNum":"1","Index":0,"ModifyType":1,"PayloadBefore":"","PayloadAfter":"..."}
func (s *Ethereum) ProposeModification(jsonStr string) (string, error) {
	start := time.Now()
	var msg types.ModifiedMessage
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		log.Error("ProposeModification: parse failed", "err", err, "json", jsonStr)
		return "", err
	}

	// Step 1: Redaction Policy 验证（论文 Definition 3.6: RP(C, RI) = 1）
	if err := ValidateRedactionInstruction(s.blockchain, &msg); err != nil {
		log.Error("ProposeModification: redaction policy failed", "err", err)
		return "", err
	}

	blockNumber, _ := big.NewInt(0).SetString(msg.BlockNum, 10)
	block := s.blockchain.GetBlockByNumber(blockNumber.Uint64())
	if block == nil {
		return "", errors.New("block not found")
	}
	proposal := types.RedactTx{
		Msg:      msg,
		BlockNum: blockNumber,
	}
	if int(msg.Index) >= len(block.Transactions()) {
		return "", errors.New("transaction index out of range")
	}
	oldTx := block.Transactions()[msg.Index]
	if err := oldTx.ValidateChameleonHashContext(); err != nil {
		return "", fmt.Errorf("source transaction NITCH context: %w", err)
	}

	// Construct a provisional payload edit solely to derive the canonical new
	// transaction message. Its r is replaced with the threshold-adapted r'
	// before the block is ever broadcast or persisted.
	newBlock, err := s.protocolManager.getNewBlock(block, &msg, msg.Index)
	if err != nil {
		log.Error("get new block Err", "err", err)
		return "", err
	}

	newTx := newBlock.Transactions()[msg.Index]
	oldMsg, err := types.TransactionHashMessage(oldTx)
	if err != nil {
		return "", err
	}
	newMsg, err := types.TransactionHashMessage(newTx)
	if err != nil {
		return "", err
	}

	// 使用 NITCH 阈值变色龙哈希锻造交易级 r'。
	km := GetKeyManager()
	if km == nil {
		log.Error("KeyManager not initialized, cannot propose modification")
		return "", errors.New("KeyManager not initialized")
	}

	// 编辑关联键 "blockNum-Index"，与脚本侧 PERF_ANCHOR 的 join_key 一致（跨节点关联 submit→全员可查）。
	reqID := redactionJoinKey(blockNumber, msg.Index)

	// 检查节点是否为委员会成员
	if !km.IsCommitteeMember() {
		log.Error("Node is not a committee member, cannot propose modification")
		return "", errors.New("node is not a committee member")
	}

	// 使用系统 NITCHKey 计算原始哈希输出（所有节点使用相同的 GlobalPK）
	systemKey, err := chameleon.GetSystemNITCHKey()
	if err != nil {
		log.Error("Failed to get system NITCHKey", "err", err)
		return "", err
	}
	// 性能打点：阶段6 edit_execution · 计算变色龙哈希 + 生成本节点适配份额
	proofStart := time.Now()
	hashOutput, err := chameleon.Hash(systemKey, oldMsg, oldTx.ChameleonR())

	if err != nil {
		log.Error("Failed to compute hash", "err", err)
		return "", err
	}

	// 创建提案ID（使用区块号、Index、原消息和新消息的摘要，避免同一区块的不同提案冲突）
	proposalID := fmt.Sprintf("%s-%v-%x-%x", msg.BlockNum, msg.Index, oldMsg[:8], newMsg[:8])
	proposal.ProposalID = proposalID
	proposal.OldMessage = oldMsg
	proposal.NewMessage = newMsg
	proposal.HashOutput = hashOutput
	km.CreateProposal(proposalID, oldMsg, newMsg, hashOutput)

	// 生成本节点的份额
	localShare, err := km.GenerateAdaptationShare(oldMsg, newMsg)
	if err != nil {
		log.Error("Failed to generate local adaptation share", "err", err)
		return "", err
	}
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_compute_proof_hash",
		"duration_ns", time.Since(proofStart).Nanoseconds(), "join_key", reqID, "status", "ok")

	// 添加本节点的份额
	if err := km.AddShare(proposalID, localShare, oldMsg, newMsg, hashOutput); err != nil {
		log.Error("Failed to add local share", "err", err)
		return "", err
	}

	// 广播提案，请求其他节点生成份额
	s.protocolManager.BroadcastModifyProposal(proposal)

	// 达到阈值时由 AddShare 直接唤醒；定时器仅用于超时和丢包后的重广播。
	sharesReady, err := km.ProposalSharesReady(proposalID)
	if err != nil {
		return "", err
	}
	waitStart := time.Now()
	waitTimeout := time.NewTimer(180 * time.Second)
	defer waitTimeout.Stop()
	rebroadcast := time.NewTicker(5 * time.Second)
	defer rebroadcast.Stop()

waitForShares:
	for {
		select {
		case <-sharesReady:
			break waitForShares
		case <-waitTimeout.C:
			break waitForShares
		case <-rebroadcast.C:
			select {
			case <-sharesReady:
				break waitForShares
			default:
			}
			s.protocolManager.BroadcastModifyProposal(proposal)
		}
	}

	// 检查是否有足够的份额
	if !km.HasEnoughShares(proposalID) {
		collected := km.GetProposalSharesCount(proposalID)
		required := km.GetThreshold() + 1
		log.Warn("Not enough shares collected after waiting",
			"proposalID", proposalID,
			"collected", collected,
			"required", required,
			"waited", time.Since(waitStart))
		return "", fmt.Errorf("insufficient shares: collected %d, required %d", collected, required)
	}

	log.Info("Enough shares collected for proposal",
		"proposalID", proposalID,
		"shares", km.GetProposalSharesCount(proposalID),
		"threshold", km.GetThreshold()+1)

	// 组合份额生成r'
	combineStart := time.Now()
	newR, err := km.CombineShares(proposalID)
	if err != nil {
		log.Error("Failed to combine shares", "err", err, "proposalID", proposalID)
		return "", fmt.Errorf("failed to combine shares: %v", err)
	}
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_combine_shares",
		"duration_ns", time.Since(combineStart).Nanoseconds(), "join_key", reqID, "status", "ok")

	if newR == nil {
		log.Error("Combined share is nil")
		return "", errors.New("threshold forge failed: combined share is nil")
	}
	// Do the same complete collision check receivers perform before broadcasting
	// or mutating local state.
	if !chameleon.Verify(systemKey, hashOutput.Hash, newMsg, newR) {
		log.Error("Combined randomizer failed final chameleon verification",
			"proposalID", proposalID,
			"oldHash", hashOutput.Hash.String(),
			"oldR", oldTx.ChameleonR().String(),
			"newR", newR.String())
		return "", errors.New("threshold forge produced invalid chameleon randomizer")
	}
	newBlock, err = s.protocolManager.getNewBlockWithRandomizer(block, &msg, msg.Index, newR)
	if err != nil {
		return "", err
	}
	cost := time.Since(start)
	log.Info("ProposeModification completed", "proposalID", proposalID, "cost", cost)

	modifyMsg := types.ModifyMsg{
		RedactTx: proposal,
		NewBlock: newBlock,
	}

	// Step 4: 广播 ModifyMsg（论文：committee 将投票/锻造结果广播，此处单节点直接广播完整修改）
	broadcastStart := time.Now()
	s.protocolManager.BroadcastModifyMsg(modifyMsg)
	// 性能打点：阶段6 edit_execution · 共享端点。duration_ns 从 ProposeModification
	// RPC 入口到 ModifyMsg 广播完成（不含本地 apply / 链确认），与 tgovern 同口径；
	// broadcast_duration_ns 仅记录 P2P 广播调用本身的开销。
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_edit_request_to_broadcast",
		"duration_ns", time.Since(start).Nanoseconds(),
		"broadcast_duration_ns", time.Since(broadcastStart).Nanoseconds(),
		"join_key", reqID, "status", "ok")

	// Step 5: 本地更新区块（论文：updateBlockInfo 执行 RI，保持链一致性）
	if err := ValidateModifiedBlockConsistency(s.blockchain, block, newBlock, msg.Index); err != nil {
		log.Error("ProposeModification: consistency check failed", "err", err)
		return "", err
	}
	if err := ValidatePayloadOnlyBlockEdit(block, newBlock, msg.Index, []byte(msg.DataAfter)); err != nil {
		return "", err
	}
	err = s.protocolManager.updateBlockInfo(block, newBlock)
	if err != nil {
		log.Error("updateBlockInfo error", "err", err)
		return "", err
	}
	// 性能打点：阶段6 edit_execution · 提案节点已成功应用编辑（记录 + 全流程总耗时）。
	km.recordRedactionApplied(reqID)
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_edit_applied",
		"duration_ns", time.Since(start).Nanoseconds(), "join_key", reqID,
		"node", "proposer", "status", "ok")
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_edit_flow_total",
		"duration_ns", time.Since(start).Nanoseconds(), "join_key", reqID, "status", "ok")
	return "success!", nil
}

func (s *Ethereum) startDkg() (string, error) {
	km := GetKeyManager()
	if km == nil {
		return "", errors.New("KeyManager not initialized")
	}

	var deal *chameleon.DistKGDeal
	var sharesToSend map[int]*chameleon.DistKGShare
	var err error

	// 检查是否需要生成新的 deal 或重新广播已有的 deal
	if km.NeedToSendDistKGDeal() {
		// 生成新的 deal
		deal, sharesToSend, err = km.StartDistKG()
		if err != nil {
			return "", fmt.Errorf("failed to start DistKG: %w", err)
		}
		log.Info("DistKG: generated new deal and shares")
	} else {
		// 尝试获取已有的 deal 和 shares（用于重新广播）
		deal, sharesToSend, err = km.GetDistKGDealAndShares()
		if err != nil {
			return "", fmt.Errorf("failed to get existing deal: %w", err)
		}
		if deal == nil {
			return "", errors.New("no need to start DistKG: deal already sent or not pending")
		}
		log.Info("DistKG: retrying broadcast with existing deal and shares")
	}

	// 检查是否有连接的 peer
	if s.protocolManager.peers.Len() == 0 {
		return "", errors.New("no peers connected, cannot broadcast DistKG")
	}

	// 广播 deal
	dealSuccess, dealFail := s.protocolManager.BroadcastDistKG(types.DistKGMessage{Deal: deal})
	log.Info("DistKG: broadcast deal", "success", dealSuccess, "failed", dealFail)

	// 广播 shares
	totalShareSuccess := 0
	totalShareFail := 0
	for _, sh := range sharesToSend {
		shareSuccess, shareFail := s.protocolManager.SendDistKGToIndex(bigIntIndex(sh.ToIndex), types.DistKGMessage{Share: sh})
		totalShareSuccess += shareSuccess
		totalShareFail += shareFail
	}
	log.Info("DistKG: broadcast shares", "success", totalShareSuccess, "failed", totalShareFail)

	// 检查是否至少成功发送给一个 peer
	totalSuccess := dealSuccess + totalShareSuccess
	if totalSuccess == 0 {
		return "", fmt.Errorf("failed to send DistKG to any peer (deal: %d success/%d fail, shares: %d success/%d fail). You can retry by calling startDkg() again", dealSuccess, dealFail, totalShareSuccess, totalShareFail)
	}

	return fmt.Sprintf("success! Sent to %d peer(s)", totalSuccess), nil
}

func (s *Ethereum) startReshare(jsonStr string) (string, error) {
	km := GetKeyManager()
	if km == nil {
		return "", errors.New("KeyManager not initialized")
	}

	if km.GetLocalKey() == nil {
		return "", errors.New("local key not initialized")
	}

	var req reshareRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		return "", fmt.Errorf("invalid reshare request: %w", err)
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("reshare-%d", time.Now().UnixNano())
	}
	if len(req.NewCommittee) == 0 {
		return "", errors.New("newCommittee must not be empty")
	}
	if req.NewThreshold <= 0 {
		return "", errors.New("newThreshold must be positive")
	}
	if len(req.OldCommittee) == 0 {
		oldN := km.GetLocalKey().NumParties
		req.OldCommittee = make([]int, 0, oldN)
		for i := 1; i <= oldN; i++ {
			req.OldCommittee = append(req.OldCommittee, i)
		}
	}

	cfg := ReshareSessionConfig{
		OldCommittee: req.OldCommittee,
		NewCommittee: req.NewCommittee,
		NewThreshold: req.NewThreshold,
	}
	rotDealStart := time.Now()
	deal, shares, err := km.StartReshare(req.SessionID, cfg)
	if err != nil {
		return "", err
	}
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_local_deal",
		"duration_ns", time.Since(rotDealStart).Nanoseconds(), "join_key", req.SessionID,
		"node", km.nodeIndex, "status", "ok")

	if s.protocolManager.peers.Len() == 0 {
		return "", errors.New("no peers connected, cannot broadcast Reshare")
	}
	s.protocolManager.advanceReshare(req.SessionID)

	rotBcastStart := time.Now()
	succ, fail := 0, 0
	if deal != nil {
		dealBcastStart := time.Now()
		succ, fail = s.protocolManager.BroadcastReshare(types.ReshareMessage{SessionID: req.SessionID, Deal: deal})
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_deal_broadcast",
			"duration_ns", time.Since(dealBcastStart).Nanoseconds(), "join_key", req.SessionID,
			"node", km.nodeIndex, "peer_count", succ+fail, "status", "ok")
	}
	shareSucc := 0
	shareFail := 0
	if km.ReshareLocalDealerRequired() {
		shareBcastStart := time.Now()
		for _, sh := range shares {
			m := types.ReshareMessage{SessionID: req.SessionID, Share: sh}
			a, b := s.protocolManager.BroadcastReshare(m)
			shareSucc += a
			shareFail += b
		}
		log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_shares_send",
			"duration_ns", time.Since(shareBcastStart).Nanoseconds(), "join_key", req.SessionID,
			"node", km.nodeIndex, "share_count", len(shares), "peer_count", shareSucc+shareFail, "status", "broadcast")
	} else {
		log.Debug("Reshare dealer outside deterministic QUAL, skipping share broadcast", "session", req.SessionID)
	}
	log.Info("E2E_TIMING", "stage", "rotation_flow", "event", "rot_broadcast_deal",
		"duration_ns", time.Since(rotBcastStart).Nanoseconds(), "join_key", req.SessionID,
		"node", km.nodeIndex, "status", "ok")

	totalSuccess := succ + shareSucc
	if totalSuccess == 0 {
		if km.ReshareSessionFinished(req.SessionID) {
			return fmt.Sprintf("success! Session=%s already complete", req.SessionID), nil
		}
		return "", fmt.Errorf("failed to send reshare to any peer (deal:%d/%d shares:%d/%d)", succ, fail, shareSucc, shareFail)
	}
	s.protocolManager.startReshareRetryLoop(req.SessionID)
	return fmt.Sprintf("success! Session=%s sent to %d peer(s)", req.SessionID, totalSuccess), nil
}

// ========== 性能打点 / 完成轮询只读状态 RPC（决策 2，挂在 PublicEthereumAPI） ==========

// redactionJoinKey 构造编辑关联键 "blockNum-Index"。
// 约定与脚本侧 PERF_ANCHOR 的 join_key 完全一致（脚本知道 blockNum 与 Index），
// 用于把脚本 submit/phase_end 锚点与各节点 edit_applied 打点跨节点关联。
func redactionJoinKey(blockNum *big.Int, index uint64) string {
	if blockNum == nil {
		return fmt.Sprintf("-%d", index)
	}
	return fmt.Sprintf("%s-%d", blockNum.String(), index)
}

// DkgStatus 只读返回 DistKG 状态（RPC: eth_dkgStatus），供编排层轮询 system_init 完成。
func (s *Ethereum) DkgStatus() map[string]interface{} {
	km := GetKeyManager()
	if km == nil {
		return map[string]interface{}{"ok": false, "err": "KeyManager not initialized"}
	}
	return km.DkgStatus()
}

// ReshareStatus 只读返回 reshare 状态（RPC: eth_reshareStatus），供编排层轮询每轮 rotation 完成。
func (s *Ethereum) ReshareStatus() map[string]interface{} {
	km := GetKeyManager()
	if km == nil {
		return map[string]interface{}{"ok": false, "err": "KeyManager not initialized"}
	}
	return km.ReshareStatus()
}

// ElectionStatus 只读返回选举与轮换状态（RPC: eth_electionStatus），返回 epoch/boundary/tau/committeeID/pending/solution count/phase。
func (s *Ethereum) ElectionStatus() map[string]interface{} {
	km := GetKeyManager()
	if km == nil {
		return map[string]interface{}{"ok": false, "err": "KeyManager not initialized"}
	}
	return km.ElectionStatus()
}

// RedactionStatus 只读返回本节点已应用编辑（RPC: eth_redactionStatus），供编排层轮询编辑可查。
func (s *Ethereum) RedactionStatus() map[string]interface{} {
	km := GetKeyManager()
	if km == nil {
		return map[string]interface{}{"ok": false, "err": "KeyManager not initialized"}
	}
	return km.RedactionStatus()
}
