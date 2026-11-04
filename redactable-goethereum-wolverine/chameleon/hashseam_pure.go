// hashseam_pure.go — pure-Go implementations of the NITCH protocol seam.
//
// The public entry points in Chameleonhash.go (CreateDistKGDeal,
// VerifyDistKGShare, DistKGExtract, CreateDKRSDeal, VerifyDKRSShare,
// DKRSExtract, PartialAdapt, PartialVerify) validate their inputs and then
// dispatch to the pureXxx functions in this file. Keeping the pure
// implementations as ordinary unexported functions (instead of hiding them
// behind a build tag) lets tests reach them directly even once a build-tagged
// selector can route the same seam to a cgo-backed implementation.
//
// The pure functions are self-contained: they acquire pairingMu themselves and
// may use the package helpers defined in Chameleonhash.go unchanged. Where the
// public wrapper has already parsed *big.Int fields into plain ints (threshold
// and party counts), the parsed values are passed in as extra parameters so
// that the pure body does not repeat input validation.
package chameleon

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/log"

	bls12381 "github.com/kilic/bls12-381"
)

// Domain separation tags for the two NITCH hash primitives. The values are
// fixed by the cross-language equivalence contract with the C++/MIRACL
// implementation (sgx/enclave/nitch/types.hpp,
// sgx/enclave/src/miracl_bls12381.cpp).
const (
	nitchH1DST = "NITCH_BLS12381_H1"
	nitchH2DST = "NITCH_BLS12381_H2"
)

// ====================== H1 / H2 hash primitives (pure path) ======================

// hashToG1 hashes a message to a point in G1（对外入口，内部会加锁）
func hashToG1(m Message) (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return hashToG1Unlocked(m)
}

// hashToG1Unlocked 在已持 pairingMu 时调用，避免死锁。
//
// H1 是 RFC 9380 hash-to-curve（BLS12381G1_XMD:SHA-256_SSWU_RO_，即 kilic 的
// G1.HashToCurve），DST 为 NITCH_H1，与 C++ MiraclBls12381::hash_to_g1
// 字节一致。HashToCurve 会使用引擎共享 scratch，pairingMu 契约不变。
func hashToG1Unlocked(m Message) (*bls12381.PointG1, error) {
	point, err := pairing.G1.HashToCurve(m, []byte(nitchH1DST))
	if err != nil {
		return nil, err
	}
	return point, nil
}

// hashToScalar maps a transcript to a scalar via expand_message_xmd with the
// NITCH H2 DST, matching the C++ MiraclBls12381::hash_to_scalar byte-for-byte:
// XMD output is 64 bytes; the final 48 bytes (scalar_from_bytes_mod_order's
// right-aligned 48-byte window) are reduced mod p.
func hashToScalar(data []byte, p *big.Int) *big.Int {
	okm, err := expandMessageXMD(data, []byte(nitchH2DST), 64)
	if err != nil || len(okm) != 64 {
		return big.NewInt(0) // unreachable; H2 DST is 17 bytes
	}
	v := new(big.Int).SetBytes(okm[16:64])
	return v.Mod(v, p)
}

// expandMessageXMD implements expand_message_xmd (RFC 9380 §5.3.1) with
// SHA-256. The construction is byte-identical to kilic's
// expandMsgSHA256XMD (hash_to_field.go) and to MIRACL's core::XMD_Expand used
// by the C++ side:
//
//	b_0 = H(Z_pad || msg || I2OSP(len,2) || 0x00 || DST || I2OSP(len(DST),1))
//	b_1 = H(b_0 || I2OSP(1,1) || DST || I2OSP(len(DST),1))
//	b_i = H(strxor(b_0, b_{i-1}) || I2OSP(i,1) || DST || I2OSP(len(DST),1))
//	OKM = b_1 || ... || b_ell, truncated to lenInBytes
func expandMessageXMD(msg, dst []byte, lenInBytes int) ([]byte, error) {
	h := sha256.New()
	domainLen := uint8(len(dst))
	if len(dst) > 255 {
		return nil, errors.New("invalid domain length")
	}
	// DST_prime = DST || I2OSP(len(DST), 1)
	// b_0 = H(Z_pad || msg || l_i_b_str || I2OSP(0, 1) || DST_prime)
	_, _ = h.Write(make([]byte, h.BlockSize()))
	_, _ = h.Write(msg)
	_, _ = h.Write([]byte{uint8(lenInBytes >> 8), uint8(lenInBytes)})
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(dst)
	_, _ = h.Write([]byte{domainLen})
	b0 := h.Sum(nil)

	// b_1 = H(b_0 || I2OSP(1, 1) || DST_prime)
	h.Reset()
	_, _ = h.Write(b0)
	_, _ = h.Write([]byte{1})
	_, _ = h.Write(dst)
	_, _ = h.Write([]byte{domainLen})
	b1 := h.Sum(nil)

	// b_i = H(strxor(b_0, b_(i - 1)) || I2OSP(i, 1) || DST_prime)
	ell := (lenInBytes + h.Size() - 1) / h.Size()
	bi := b1
	out := make([]byte, lenInBytes)
	for i := 1; i < ell; i++ {
		h.Reset()
		// b_i = H(strxor(b_0, b_(i - 1)) || I2OSP(i, 1) || DST_prime)
		tmp := make([]byte, h.Size())
		for j := 0; j < h.Size(); j++ {
			tmp[j] = b0[j] ^ bi[j]
		}
		_, _ = h.Write(tmp)
		_, _ = h.Write([]byte{1 + uint8(i)})
		_, _ = h.Write(dst)
		_, _ = h.Write([]byte{domainLen})

		// b_1 || ... || b_(ell - 1)
		copy(out[(i-1)*h.Size():i*h.Size()], bi[:])
		bi = h.Sum(nil)
	}
	// b_ell
	copy(out[(ell-1)*h.Size():], bi[:])
	return out[:lenInBytes], nil
}

// ====================== DistKG (Protocol 1) pure implementations ======================

// pureCreateDistKGDeal is the pure-Go implementation behind CreateDistKGDeal.
// The wrapper has already validated t, n and dealerIndex.
func pureCreateDistKGDeal(pp *PublicParameters, dealerIndex, t, n int) (*DistKGDeal, *DistKGDealPrivate, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()

	// 随机选择 f_i(z) = a_{i,0} + a_{i,1}z + ... + a_{i,t}z^t
	coeffs := make([]*big.Int, t+1)
	for k := 0; k <= t; k++ {
		coeffs[k] = randomScalar(pp.p)
	}

	// A_{i,k} = g1^{a_{i,k}}, B_{i,0} = g2^{a_{i,0}}
	A := make([]*bls12381.PointG1, t+1)
	for k := 0; k <= t; k++ {
		A[k] = pairing.G1.New()
		pairing.G1.MulScalar(A[k], pp.g1, bigIntToFr(coeffs[k]))
	}
	B0 := pairing.G2.New()
	pairing.G2.MulScalar(B0, pp.g2, bigIntToFr(coeffs[0]))

	// 份额 s_{i,j} = f_i(ω^j)
	shares := make(map[int]*big.Int)
	for j := 1; j <= n; j++ {
		x, err := evaluationPointForIndex(pp.p, n, j)
		if err != nil {
			return nil, nil, err
		}
		shares[j] = evaluatePolynomial(coeffs, x, pp.p)
	}

	// 序列化 A、B0 以及公开参数（保证所有节点使用同一 g1,g2）
	aBytes := make([]byte, 0)
	for k := 0; k <= t; k++ {
		aBytes = append(aBytes, pairing.G1.ToBytes(A[k])...)
	}
	b0Bytes := pairing.G2.ToBytes(B0)
	g1Bytes := pairing.G1.ToBytes(pp.g1)
	g2Bytes := pairing.G2.ToBytes(pp.g2)
	pHex := pp.p.Text(16)

	return &DistKGDeal{
			DealerIndex: big.NewInt(int64(dealerIndex)),
			Threshold:   big.NewInt(int64(t)),
			NumParties:  big.NewInt(int64(n)),
			A:           aBytes,
			B0:          b0Bytes,
			G1Bytes:     g1Bytes,
			G2Bytes:     g2Bytes,
			P:           pHex,
		}, &DistKGDealPrivate{
			DealerIndex: dealerIndex,
			Coeffs:      coeffs,
			Shares:      shares,
		}, nil
}

// pureVerifyDistKGShare is the pure-Go implementation behind VerifyDistKGShare.
// The wrapper has already validated deal/shareSij/receiverIndex and parsed t/n
// from the deal.
func pureVerifyDistKGShare(pp *PublicParameters, deal *DistKGDeal, shareSij *big.Int, receiverIndex, t, n int) (bool, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()

	if len(deal.A) != (t+1)*g1PointByteSize {
		return false, fmt.Errorf("invalid deal A length")
	}

	// 解析 A_{i,0},...,A_{i,t}
	// 解析失败的承诺块必须报错而非继续运算：FromBytes 失败返回 nil 点，
	// 后续 MulScalar/Add 会 panic（P2P 恶意 deal 可触达）。与 cgo 侧的
	// (false, NITCH_ERR_CRYPTO) 语义对齐。
	A := make([]*bls12381.PointG1, t+1)
	var err error
	for k := 0; k <= t; k++ {
		A[k], err = pairing.G1.FromBytes(deal.A[k*g1PointByteSize : (k+1)*g1PointByteSize])
		if err != nil {
			return false, fmt.Errorf("invalid commitment A[%d]: %w", k, err)
		}
	}
	B0, err := pairing.G2.FromBytes(deal.B0)
	if err != nil {
		return false, err
	}

	// 验证配对：e(g1, B_{i,0}) = e(A_{i,0}, g2)
	pairing.Reset()
	pairing.AddPair(pp.g1, B0)
	e1 := pairing.Result()
	pairing.Reset()
	pairing.AddPair(A[0], pp.g2)
	e2 := pairing.Result()
	if !e1.Equal(e2) {
		return false, nil
	}

	// 验证 g1^{s_{i,j}} = sum_{k=0}^t ((ω^j)^k * A_{i,k})
	jBig, err := evaluationPointForIndex(pp.p, n, receiverIndex)
	if err != nil {
		return false, err
	}
	jPower := big.NewInt(1)
	rhs := pairing.G1.New()
	for k := 0; k <= t; k++ {
		term := pairing.G1.New()
		pairing.G1.MulScalar(term, A[k], bigIntToFr(jPower))
		pairing.G1.Add(rhs, rhs, term)
		jPower.Mul(jPower, jBig)
		jPower.Mod(jPower, pp.p)
	}
	lhs := pairing.G1.New()
	pairing.G1.MulScalar(lhs, pp.g1, bigIntToFr(shareSij))
	return bytes.Equal(pairing.G1.ToBytes(lhs), pairing.G1.ToBytes(rhs)), nil
}

// pureDistKGExtract is the pure-Go implementation behind DistKGExtract. The
// wrapper has already validated qual/deals/sharesToJ and parsed t/n from the
// first QUAL deal.
func pureDistKGExtract(pp *PublicParameters, myIndex int, qual []int, deals map[int]*DistKGDeal, sharesToJ map[int]*big.Int, t, n int) (*NITCHKey, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()

	// pk = sum_{i in QUAL} B_{i,0} (G2 上相加)
	// 与 verify 相同：解析失败必须报错（nil 点会导致 Add panic，且 B0/A0
	// 来自 P2P deal，属不可信输入）。
	pk := pairing.G2.New()
	for _, i := range qual {
		B0, err := pairing.G2.FromBytes(deals[i].B0)
		if err != nil {
			return nil, fmt.Errorf("invalid B0 from dealer %d: %w", i, err)
		}
		pairing.G2.Add(pk, pk, B0)
	}

	// g1^α = sum_{i in QUAL} A_{i,0}（用于交易级随机数派生等）
	pkG1 := pairing.G1.New()
	for _, i := range qual {
		A0, err := pairing.G1.FromBytes(deals[i].A[0:g1PointByteSize])
		if err != nil {
			return nil, fmt.Errorf("invalid A0 from dealer %d: %w", i, err)
		}
		pairing.G1.Add(pkG1, pkG1, A0)
	}

	// sk_j = sum_{i in QUAL} s_{i,j}
	skj := new(big.Int)
	for _, i := range qual {
		skj.Add(skj, sharesToJ[i])
	}
	skj.Mod(skj, pp.p)

	// vk_j = g1^{sk_j}
	vk := pairing.G1.New()
	pairing.G1.MulScalar(vk, pp.g1, bigIntToFr(skj))

	return &NITCHKey{
		PP:              pp,
		GlobalPK:        pk,
		GlobalPKG1:      pkG1,
		Threshold:       t,
		NumParties:      n,
		SecretShare:     skj,
		VerificationKey: vk,
		Index:           myIndex,
	}, nil
}

// ====================== DKRS (re-sharing) pure implementations ======================

// pureCreateDKRSDeal is the pure-Go implementation behind CreateDKRSDeal.
// The wrapper has already validated the parameters.
func pureCreateDKRSDeal(pp *PublicParameters, dealerIndex int, oldSecretShare *big.Int, oldThreshold int, newThreshold int, newN int) (*DKRSDeal, *DKRSDealPrivate, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()

	coeffs := make([]*big.Int, newThreshold+1)
	coeffs[0] = new(big.Int).Mod(new(big.Int).Set(oldSecretShare), pp.p)
	for k := 1; k <= newThreshold; k++ {
		coeffs[k] = randomScalar(pp.p)
	}

	A := make([]*bls12381.PointG1, newThreshold+1)
	for k := 0; k <= newThreshold; k++ {
		A[k] = pairing.G1.New()
		pairing.G1.MulScalar(A[k], pp.g1, bigIntToFr(coeffs[k]))
	}

	shares := make(map[int]*big.Int)
	for j := 1; j <= newN; j++ {
		x, err := evaluationPointForIndex(pp.p, newN, j)
		if err != nil {
			return nil, nil, err
		}
		shares[j] = evaluatePolynomial(coeffs, x, pp.p)
	}

	aBytes := make([]byte, 0)
	for k := 0; k <= newThreshold; k++ {
		aBytes = append(aBytes, pairing.G1.ToBytes(A[k])...)
	}

	b0 := pairing.G2.New()
	pairing.G2.MulScalar(b0, pp.g2, bigIntToFr(coeffs[0]))
	deal := &DKRSDeal{
		DealerIndex:   big.NewInt(int64(dealerIndex)),
		OldThreshold:  big.NewInt(int64(oldThreshold)),
		OldNumParties: big.NewInt(0),
		NewThreshold:  big.NewInt(int64(newThreshold)),
		NewNumParties: big.NewInt(int64(newN)),
		A:             aBytes,
		B0:            pairing.G2.ToBytes(b0),
	}
	priv := &DKRSDealPrivate{
		DealerIndex: dealerIndex,
		Coeffs:      coeffs,
		Shares:      shares,
	}
	return deal, priv, nil
}

// pureVerifyDKRSShare is the pure-Go implementation behind VerifyDKRSShare.
// The wrapper has already validated the parameters (including the deal A
// length) and parsed newT/newN from the deal.
func pureVerifyDKRSShare(pp *PublicParameters, deal *DKRSDeal, shareSij *big.Int, receiverIndex, newT, newN int) (bool, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()

	A := make([]*bls12381.PointG1, newT+1)
	for k := 0; k <= newT; k++ {
		point, err := pairing.G1.FromBytes(deal.A[k*g1PointByteSize : (k+1)*g1PointByteSize])
		if err != nil {
			return false, err
		}
		A[k] = point
	}

	xj, err := evaluationPointForIndex(pp.p, newN, receiverIndex)
	if err != nil {
		return false, err
	}
	jPower := big.NewInt(1)
	rhs := pairing.G1.New()
	for k := 0; k <= newT; k++ {
		term := pairing.G1.New()
		pairing.G1.MulScalar(term, A[k], bigIntToFr(jPower))
		pairing.G1.Add(rhs, rhs, term)
		jPower.Mul(jPower, xj)
		jPower.Mod(jPower, pp.p)
	}
	lhs := pairing.G1.New()
	pairing.G1.MulScalar(lhs, pp.g1, bigIntToFr(shareSij))
	return bytes.Equal(pairing.G1.ToBytes(lhs), pairing.G1.ToBytes(rhs)), nil
}

// pureDKRSExtract is the pure-Go implementation behind DKRSExtract. The
// wrapper has already validated the parameters and parsed
// newThreshold/newN/oldN from the deal and current key.
func pureDKRSExtract(pp *PublicParameters, currentKey *NITCHKey, myNewIndex int, oldQual []int, deals map[int]*DKRSDeal, sharesToMe map[int]*big.Int, newThreshold, newN, oldN int) (*NITCHKey, error) {
	oldDomainPoints := make([]*big.Int, len(oldQual))
	for idx, i := range oldQual {
		if i < 1 || i > oldN {
			return nil, fmt.Errorf("old qualifier index out of range: %d", i)
		}
		if _, ok := deals[i]; !ok || deals[i] == nil {
			return nil, fmt.Errorf("missing deal for qualified member %d", i)
		}
		xi, err := evaluationPointForIndex(pp.p, oldN, i)
		if err != nil {
			return nil, err
		}
		oldDomainPoints[idx] = xi
	}

	coeffs, err := lagrangeCoefficientsAtZeroGnarkFFT(oldDomainPoints, pp.p)
	if err != nil {
		return nil, fmt.Errorf("failed to compute old-domain lagrange coefficients: %w", err)
	}

	skj := new(big.Int)
	for idx, i := range oldQual {
		sij, ok := sharesToMe[i]
		if !ok || sij == nil {
			return nil, fmt.Errorf("missing share from %d for myself %d", i, myNewIndex)
		}
		term := new(big.Int).Mul(coeffs[idx], sij)
		skj.Add(skj, term)
		skj.Mod(skj, pp.p)
	}

	pairingMu.Lock()
	defer pairingMu.Unlock()
	vk := pairing.G1.New()
	pairing.G1.MulScalar(vk, pp.g1, bigIntToFr(skj))

	return &NITCHKey{
		PP:              pp,
		GlobalPK:        currentKey.GlobalPK,
		GlobalPKG1:      currentKey.GlobalPKG1,
		Threshold:       newThreshold,
		NumParties:      newN,
		SecretShare:     skj,
		VerificationKey: vk,
		Index:           myNewIndex,
	}, nil
}

// ====================== Adaptation pure implementations ======================

// purePartialAdapt is the pure-Go implementation behind PartialAdapt.
func purePartialAdapt(key *NITCHKey, m, mPrime Message) (*AdaptationShare, error) {
	// 步骤1：计算 y = H_1(m) * H_1(m')^{-1}
	start := time.Now()
	// 以下所有 pairing.G1 运算（含 generateNIZKProof）在 pairingMu 保护下进行，
	// 避免 kilic 引擎 scratch 被并发覆盖。
	pairingMu.Lock()
	defer pairingMu.Unlock()
	h1m, err := hashToG1Unlocked(m)
	if err != nil {
		return nil, err
	}

	h1mPrime, err := hashToG1Unlocked(mPrime)
	if err != nil {
		return nil, err
	}

	// 计算 y = h1m * h1mPrime^{-1}（在 G_1 群中）
	// 注意：在加法群中，逆元通过取负得到
	y := pairing.G1.New()
	invH1mPrime := pairing.G1.New()
	pairing.G1.Neg(invH1mPrime, h1mPrime) // 计算 H_1(m') 的逆元
	pairing.G1.Add(y, h1m, invH1mPrime)   // y = H_1(m) + (-H_1(m')) = H_1(m) * H_1(m')^{-1}

	// 步骤2：计算份额 c_i = y^{sk_i}
	// 在 G_1 群中，指数运算 y^{sk_i} 等价于标量乘法 y * sk_i
	c_i := pairing.G1.New()
	skFr := bigIntToFr(key.SecretShare)
	pairing.G1.MulScalar(c_i, y, skFr) // c_i = y * sk_i = y^{sk_i}

	// 步骤3：生成零知识证明 π_i
	// 证明：知道 sk_i 使得 vk_i = g_1^{sk_i} 且 c_i = y^{sk_i}
	// 这等价于证明 (g_1, vk_i, y, c_i) 构成一个迪菲-赫尔曼元组
	proof, err := generateNIZKProof(key, y, c_i)
	if err != nil {
		return nil, err
	}
	adaptationTime := time.Since(start)
	log.Info("PartialAdapt completed", "memberIndex", key.Index, "adaptationTime", adaptationTime)
	// 返回投票 s_i = (i, c_i, π_i)
	return &AdaptationShare{
		Index: big.NewInt(int64(key.Index)),
		Share: c_i,
		Proof: proof,
	}, nil
}

// purePartialVerify is the pure-Go implementation behind PartialVerify.
func purePartialVerify(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	// 验证 NIZK 证明 π_i
	start := time.Now()
	if !verifyNIZKProof(key, share, m, mPrime) {
		return false
	}
	/*

		// 验证份额一致性：重新计算 y 并验证 c_i 的正确性
		h1m, err1 := hashToG1(m)
		h1mPrime, err2 := hashToG1(mPrime)
		if err1 != nil || err2 != nil {
			return false
		}
		y := pairing.G1.New()
		invH1mPrime := pairing.G1.New()
		pairing.G1.Neg(invH1mPrime, h1mPrime)
		pairing.G1.Add(y, h1m, invH1mPrime)
	*/
	// 注意：我们无法直接验证 c_i = y^{sk_i}（因为不知道 sk_i）
	// 但 NIZK 证明已经保证了这一点，所以这里主要验证消息一致性
	verifyTime := time.Since(start)
	log.Info("PartialVerify completed", "memberIndex", share.Index, "verifyTime", verifyTime)
	return true
}
