package chameleon

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/log"
	"sync"
)

var (
	// systemNITCHKeyCache：仅用于哈希计算的“系统公钥视图”
	// - 对于区块/交易的变色龙哈希计算，只需要 PP + GlobalPK(+GlobalPKG1) + (t,n)
	// - 不需要 SecretShare / VerificationKey
	systemMu            sync.RWMutex
	systemNITCHKeyCache *NITCHKey

	// DistKG 观察者：任何节点只要看到 Deal/Complaint（公开信息），就可推导出 pk
	systemPP          *PublicParameters
	systemDistKGDeals map[int]*DistKGDeal
	systemDistKGComps []DistKGComplaint
)

// GetSystemNITCHKey 获取系统 NITCHKey（仅公钥视图）。
// 不再从文件加载/写入；必须由 DistKG 协议消息（Deal/Complaint）推导或由委员会节点显式设置。
func GetSystemNITCHKey() (*NITCHKey, error) {
	systemMu.RLock()
	defer systemMu.RUnlock()
	if systemNITCHKeyCache == nil {
		return nil, fmt.Errorf("system NITCHKey not initialized yet")
	}
	return systemNITCHKeyCache, nil
}

// SystemPublicKeyReady reports whether this node has enough public DistKG
// material to validate transaction-level NITCH hashes. It intentionally does
// not require a local secret share, so ordinary observer nodes can become ready.
func SystemPublicKeyReady() bool {
	systemMu.RLock()
	defer systemMu.RUnlock()
	return systemNITCHKeyCache != nil && systemNITCHKeyCache.PP != nil &&
		systemNITCHKeyCache.GlobalPK != nil && systemNITCHKeyCache.GlobalPKG1 != nil
}

// SameGlobalPublicKey verifies the GlobalPK/GlobalPKG1 invariant across a
// DKRS reshare without exposing or comparing secret shares.
func SameGlobalPublicKey(first, second *NITCHKey) bool {
	if first == nil || second == nil || first.GlobalPK == nil || second.GlobalPK == nil ||
		first.GlobalPKG1 == nil || second.GlobalPKG1 == nil {
		return false
	}
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return bytes.Equal(pairing.G2.ToBytes(first.GlobalPK), pairing.G2.ToBytes(second.GlobalPK)) &&
		bytes.Equal(pairing.G1.ToBytes(first.GlobalPKG1), pairing.G1.ToBytes(second.GlobalPKG1))
}

// SetSystemNITCHKeyFromKey 将委员会节点的本地密钥（或其公钥部分）设置为系统密钥缓存。
// 仅保留哈希计算所需字段。
func SetSystemNITCHKeyFromKey(key *NITCHKey) {
	if key == nil || key.PP == nil || key.GlobalPK == nil || key.GlobalPKG1 == nil {
		return
	}
	systemMu.Lock()
	defer systemMu.Unlock()
	systemPP = key.PP
	systemNITCHKeyCache = &NITCHKey{
		PP:         key.PP,
		GlobalPK:   key.GlobalPK,
		GlobalPKG1: key.GlobalPKG1,
		Threshold:  key.Threshold,
		NumParties: key.NumParties,
	}
}

// ObserveDistKGDeal 观察 DistKG Deal（公开信息），用于推导系统公钥 pk。
func ObserveDistKGDeal(deal *DistKGDeal) {
	if deal == nil {
		return
	}
	systemMu.Lock()
	defer systemMu.Unlock()

	if systemDistKGDeals == nil {
		systemDistKGDeals = make(map[int]*DistKGDeal)
	}
	dealerIdx, err := intFromBig(deal.DealerIndex, "deal.DealerIndex")
	if err != nil {
		return
	}
	systemDistKGDeals[dealerIdx] = deal

	// 若尚未有 pp，则从 Deal 携带的公开参数解析
	if systemPP == nil && len(deal.G1Bytes) > 0 && len(deal.G2Bytes) > 0 && deal.P != "" {
		if pp, err := PublicParametersFromDeal(deal); err == nil {
			systemPP = pp
		}
	}
	tryDeriveSystemKeyLocked()
}

// ObserveDistKGComplaint 观察 DistKG 投诉（公开信息），用于确定 QUAL 并推导 pk。
func ObserveDistKGComplaint(c *DistKGComplaint) {
	if c == nil {
		return
	}
	systemMu.Lock()
	defer systemMu.Unlock()
	systemDistKGComps = append(systemDistKGComps, *c)
	tryDeriveSystemKeyLocked()
}

// tryDeriveSystemKeyLocked 在持有 systemMu 写锁下调用：当公开信息足够时推导系统公钥并填充缓存。
func tryDeriveSystemKeyLocked() {
	if systemNITCHKeyCache != nil || systemPP == nil || systemDistKGDeals == nil {
		return
	}
	// 需要收齐全部 n 个 Deal 才能按 QUAL 计算 pk（与协议 Extraction 一致）
	var deal *DistKGDeal
	for _, d := range systemDistKGDeals {
		deal = d
		break
	}
	if deal == nil {
		return
	}
	n := deal.NumParties
	t := deal.Threshold
	nInt, err := intFromBig(n, "deal.NumParties")
	if err != nil {
		return
	}
	tInt, err := intFromBig(t, "deal.Threshold")
	if err != nil {
		return
	}
	for i := 1; i <= nInt; i++ {
		if systemDistKGDeals[i] == nil {
			return
		}
	}
	qual := BuildQUAL(systemDistKGComps, tInt, nInt)
	if len(qual) < tInt+1 {
		return
	}

	// 计算 pk = sum_{i in QUAL} B0, pkG1 = sum_{i in QUAL} A0（都只依赖公开信息）
	pairingMu.Lock()
	defer pairingMu.Unlock()

	pk := pairing.G2.New()
	pkG1 := pairing.G1.New()
	for _, i := range qual {
		d := systemDistKGDeals[i]
		if d == nil {
			return
		}
		B0, err := pairing.G2.FromBytes(d.B0)
		if err != nil {
			return
		}
		pairing.G2.Add(pk, pk, B0)

		if len(d.A) < g1PointByteSize {
			return
		}
		A0, err := pairing.G1.FromBytes(d.A[0:g1PointByteSize])
		if err != nil {
			return
		}
		pairing.G1.Add(pkG1, pkG1, A0)
	}

	systemNITCHKeyCache = &NITCHKey{
		PP:         systemPP,
		GlobalPK:   pk,
		GlobalPKG1: pkG1,
		Threshold:  tInt,
		NumParties: nInt,
	}
	log.Info("Derived system NITCH public key from DistKG deals", "t", t, "n", n, "qual", len(qual))
}
