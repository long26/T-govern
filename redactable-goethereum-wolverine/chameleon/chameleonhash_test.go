package chameleon

import (
	"math/big"
	"testing"
)

// TestNITCHBasic 测试基本的NITCH功能
func TestNITCHBasic(t *testing.T) {
	// 初始化公开参数
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 生成分布式密钥（模拟）
	tThreshold := 2 // 阈值
	nParties := 8   // 总成员数（2 的幂）
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	if len(keys) != nParties {
		t.Fatalf("expected %d keys, got %d", nParties, len(keys))
	}

	// 测试哈希计算：Hash 需要非 nil 的 r，使用 RandomPointG1 生成
	m := Message("test message")
	r := RandomPointG1(pp)
	if r == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	if got := len(r.Bytes()); got != g1CompressedPointByteSize {
		t.Fatalf("compressed r length = %d, want %d", got, g1CompressedPointByteSize)
	}
	hashOutput, err := Hash(keys[0], m, r)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}

	if hashOutput.Hash == nil {
		t.Fatal("Hash output is nil")
	}

	if hashOutput.CheckValue == nil {
		t.Fatal("CheckValue is nil")
	}

	// 验证哈希
	if !Verify(keys[0], hashOutput.Hash, m, hashOutput.CheckValue) {
		t.Fatal("Hash verification failed")
	}

	t.Logf("Basic NITCH test passed: hash = %x", hashOutput.Hash)
}

// TestNITCHAdaptation 测试阈值适应（修改消息）
func TestNITCHAdaptation(t *testing.T) {
	// 初始化公开参数
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 生成分布式密钥
	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	// 步骤1：计算初始哈希（需提供有效 r）
	m := Message("original message")
	r0 := RandomPointG1(pp)
	if r0 == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	hashOutput, err := Hash(keys[0], m, r0)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}

	h := hashOutput.Hash
	r := hashOutput.CheckValue

	// 验证初始哈希
	if !Verify(keys[0], h, m, r) {
		t.Fatal("Initial hash verification failed")
	}

	// 步骤2：每个成员独立计算份额
	mPrime := Message("new message")
	var shares []*AdaptationShare

	// 收集 t+1 个份额
	for i := 0; i < tThreshold+1; i++ {
		share, err := PartialAdapt(keys[i], m, mPrime)
		if err != nil {
			t.Fatalf("PartialAdapt failed for member %d: %v", i, err)
		}

		// 验证份额
		if !PartialVerify(keys[i], share, m, mPrime) {
			t.Fatalf("PartialVerify failed for member %d", i)
		}

		shares = append(shares, share)
	}

	// 步骤3：收集者组合出 r'
	rPrime, err := Combine(pp, shares, tThreshold, nParties, hashOutput, m, mPrime)
	if err != nil {
		t.Fatalf("Combine failed: %v", err)
	}

	if rPrime == nil {
		t.Fatal("Combine returned nil")
	}
	if got := len(rPrime.Bytes()); got != g1CompressedPointByteSize {
		t.Fatalf("compressed rPrime length = %d, want %d", got, g1CompressedPointByteSize)
	}

	// 步骤4：验证修改后的哈希
	if !Verify(keys[0], h, mPrime, rPrime) {
		t.Fatal("Modified hash verification failed")
	}

	t.Logf("NITCH adaptation test passed: successfully changed message from '%s' to '%s'", string(m), string(mPrime))
}

// TestNITCHInsufficientShares 测试份额不足的情况
func TestNITCHInsufficientShares(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	m := Message("original message")
	mPrime := Message("new message")
	r0 := RandomPointG1(pp)
	if r0 == nil {
		t.Fatal("RandomPointG1 returned nil")
	}
	hashOutput, err := Hash(keys[0], m, r0)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}

	// 只收集 t 个份额（应该失败）
	var shares []*AdaptationShare
	for i := 0; i < tThreshold; i++ {
		share, err := PartialAdapt(keys[i], m, mPrime)
		if err != nil {
			t.Fatalf("PartialAdapt failed: %v", err)
		}
		shares = append(shares, share)
	}

	// 应该返回错误
	_, err = Combine(pp, shares, tThreshold, nParties, hashOutput, m, mPrime)
	if err == nil {
		t.Fatal("Expected error for insufficient shares, but got nil")
	}

	if err != ErrInsufficientShares {
		t.Fatalf("Expected ErrInsufficientShares, got: %v", err)
	}

	t.Logf("Insufficient shares test passed: correctly rejected %d shares (need %d)", len(shares), tThreshold+1)
}

// TestComputeInverseShare 测试 ComputeInverseShare 函数
func TestComputeInverseShare(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 生成一个随机私钥
	privKey := randomScalar(pp.p)
	if privKey == nil {
		t.Fatal("Failed to generate random private key")
	}

	// 测试 ComputeInverseShare
	n := 5
	invShare := ComputeInverseShare(privKey, n)
	if invShare == nil {
		t.Fatal("ComputeInverseShare returned nil")
	}

	// 验证逆元份额的有效性：invShare 应该是 sk^{-1}/n mod q
	// 在持锁下读取 Q，避免与其它 pairing 操作并发
	pairingMu.Lock()
	q := pairing.G1.Q()
	pairingMu.Unlock()
	if invShare.Cmp(q) >= 0 {
		t.Errorf("invShare should be less than q, got %x", invShare)
	}

	t.Logf("ComputeInverseShare test passed: invShare = %x", invShare)
}

// TestMultipleMessages 测试多个消息的哈希和修改
func TestMultipleMessages(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	messages := []Message{
		Message("message 1"),
		Message("message 2"),
		Message("message 3"),
	}

	var hashes []*big.Int
	var rs []*big.Int

	// 计算所有消息的哈希（每条消息使用独立的 r）
	for _, m := range messages {
		r := RandomPointG1(pp)
		if r == nil {
			t.Fatalf("RandomPointG1 returned nil for message '%s'", string(m))
		}
		hashOutput, err := Hash(keys[0], m, r)
		if err != nil {
			t.Fatalf("Hash failed for message '%s': %v", string(m), err)
		}
		hashes = append(hashes, hashOutput.Hash)
		rs = append(rs, hashOutput.CheckValue)

		// 验证每个哈希
		if !Verify(keys[0], hashOutput.Hash, m, hashOutput.CheckValue) {
			t.Fatalf("Verification failed for message '%s'", string(m))
		}
	}

	// 修改第一个消息
	mPrime := Message("modified message 1")
	hashOutput := &HashOutput{
		Hash:       hashes[0],
		CheckValue: rs[0],
	}

	var shares []*AdaptationShare
	for i := 0; i < tThreshold+1; i++ {
		share, err := PartialAdapt(keys[i], messages[0], mPrime)
		if err != nil {
			t.Fatalf("PartialAdapt failed: %v", err)
		}
		if !PartialVerify(keys[i], share, messages[0], mPrime) {
			t.Fatalf("PartialVerify failed for member %d", i)
		}
		shares = append(shares, share)
	}

	rPrime, err := Combine(pp, shares, tThreshold, nParties, hashOutput, messages[0], mPrime)
	if err != nil {
		t.Fatalf("Combine failed: %v", err)
	}

	// 验证修改后的哈希
	if !Verify(keys[0], hashes[0], mPrime, rPrime) {
		t.Fatal("Modified hash verification failed")
	}

	// 验证其他消息的哈希没有改变
	for i := 1; i < len(messages); i++ {
		if !Verify(keys[0], hashes[i], messages[i], rs[i]) {
			t.Fatalf("Hash for message %d changed unexpectedly", i)
		}
	}

	t.Logf("Multiple messages test passed: successfully modified first message while keeping others unchanged")
}

// TestComputeTransactionR 测试交易级随机数：确定性生成 r，且能反序列化回 G1 并用于 Hash/Verify。
func TestComputeTransactionR(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	tThreshold := 2
	nParties := 8
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	chainID := big.NewInt(1337)
	txDigest := []byte("signed transaction without chameleon randomizer")

	r, err := ComputeTransactionR(keys[0], chainID, txDigest)
	if err != nil {
		t.Fatalf("ComputeTransactionR failed: %v", err)
	}
	if r == nil {
		t.Fatal("ComputeTransactionR returned nil r")
	}
	if got := len(r.Bytes()); got != g1CompressedPointByteSize {
		t.Fatalf("compressed transaction r length = %d, want %d", got, g1CompressedPointByteSize)
	}

	// 私有链不兼容历史未压缩 R：96 字节编码必须被严格拒绝。
	rPoint, err := bigIntToG1Point(r)
	if err != nil {
		t.Fatalf("compressed r decode failed: %v", err)
	}
	pairingMu.Lock()
	uncompressedR := pairing.G1.ToBytes(rPoint)
	pairingMu.Unlock()
	if got := len(uncompressedR); got != g1PointByteSize {
		t.Fatalf("uncompressed fixture length = %d, want %d", got, g1PointByteSize)
	}
	if _, err := bigIntToG1Point(new(big.Int).SetBytes(uncompressedR)); err == nil {
		t.Fatal("96-byte uncompressed r was accepted")
	}

	// 使用该 r 计算哈希并验证
	m := Message("transaction message")
	h, _ := Hash(keys[0], m, r)
	if h == nil {
		t.Fatal("ComputeHashWithR returned nil")
	}
	if !Verify(keys[0], h.Hash, m, r) {
		t.Fatal("Verify failed for r produced by ComputeTransactionR")
	}

	// 同一输入应得到相同 r（确定性）
	r2, err := ComputeTransactionR(keys[0], chainID, txDigest)
	if err != nil {
		t.Fatalf("ComputeTransactionR (second call) failed: %v", err)
	}
	if r.Cmp(r2) != 0 {
		t.Error("ComputeTransactionR should be deterministic: two calls gave different r")
	}

	t.Logf("ComputeTransactionR test passed: r bytes length=%d", len(r.Bytes()))
}

func TestPowerOfTwoConstraintInDistKG(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if _, err := SimulatedDistKG(pp, 2, 5); err == nil {
		t.Fatal("expected SimulatedDistKG to fail when n is not a power of two")
	}

	if _, _, err := CreateDistKGDeal(pp, 1, 2, 5); err == nil {
		t.Fatal("expected CreateDistKGDeal to fail when n is not a power of two")
	}
}

func TestPowerOfTwoConstraintInCombine(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	_, err = Combine(pp, nil, 1, 5, nil, Message("m"), Message("m2"))
	if err == nil {
		t.Fatal("expected Combine to fail when numParties is not a power of two")
	}
}

func TestCombineRejectsDuplicateShareIndex(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	shares := []*AdaptationShare{
		{Index: big.NewInt(1)},
		{Index: big.NewInt(1)},
	}

	_, err = Combine(pp, shares, 1, 8, nil, Message("m"), Message("m2"))
	if err == nil {
		t.Fatal("expected duplicate share index error")
	}
}

func TestCombineRejectsOutOfRangeShareIndex(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	shares := []*AdaptationShare{
		{Index: big.NewInt(1)},
		{Index: big.NewInt(9)},
	}

	_, err = Combine(pp, shares, 1, 8, nil, Message("m"), Message("m2"))
	if err == nil {
		t.Fatal("expected out-of-range share index error")
	}
}

func TestLagrangeCoefficientCalculatorGnarkFFTEquivalence(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	n := 8
	selected := []int{1, 2, 3, 5}
	indices := make([]*big.Int, 0, len(selected))
	for _, idx := range selected {
		xi, err := evaluationPointForIndex(pp.p, n, idx)
		if err != nil {
			t.Fatalf("evaluationPointForIndex failed: %v", err)
		}
		indices = append(indices, xi)
	}

	gnarkFFT, err := lagrangeCoefficientsAtZeroGnarkFFT(indices, pp.p)
	if err != nil {
		t.Fatalf("gnark FFT calculator failed: %v", err)
	}

	if len(gnarkFFT) != len(indices) {
		t.Fatalf("coefficient length mismatch: want=%d got=%d", len(indices), len(gnarkFFT))
	}

	checksum := big.NewInt(0)
	for _, coeff := range gnarkFFT {
		checksum.Add(checksum, coeff)
		checksum.Mod(checksum, pp.p)
	}
	if checksum.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("lagrange checksum mismatch: got %s", checksum.String())
	}
}

func TestDKRSReSharingSuccess(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	oldT := 3
	oldN := 8
	oldKeys, err := SimulatedDistKG(pp, oldT, oldN)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}

	newT := 3
	newN := 8
	qual := make([]int, 0, oldN)
	deals := make(map[int]*DKRSDeal)
	dealPriv := make(map[int]*DKRSDealPrivate)
	for i := 1; i <= oldN; i++ {
		qual = append(qual, i)
		d, priv, err := CreateDKRSDeal(pp, i, oldKeys[i-1].SecretShare, oldT, newT, newN)
		if err != nil {
			t.Fatalf("CreateDKRSDeal failed for dealer %d: %v", i, err)
		}
		d.OldNumParties = big.NewInt(int64(oldN))
		deals[i] = d
		dealPriv[i] = priv
	}

	newKeys := make([]*NITCHKey, newN)
	for j := 1; j <= newN; j++ {
		sharesToMe := make(map[int]*big.Int)
		for i := 1; i <= oldN; i++ {
			share := dealPriv[i].Shares[j]
			ok, err := VerifyDKRSShare(pp, deals[i], share, j)
			if err != nil {
				t.Fatalf("VerifyDKRSShare failed for dealer=%d receiver=%d: %v", i, j, err)
			}
			if !ok {
				t.Fatalf("VerifyDKRSShare returned false for dealer=%d receiver=%d", i, j)
			}
			sharesToMe[i] = share
		}
		k, err := DKRSExtract(pp, oldKeys[0], j, qual, deals, sharesToMe)
		if err != nil {
			t.Fatalf("DKRSExtract failed for new member %d: %v", j, err)
		}
		newKeys[j-1] = k
	}

	if newKeys[0] == nil {
		t.Fatal("new key is nil")
	}

	pairingMu.Lock()
	oldPKBytes := pairing.G2.ToBytes(oldKeys[0].GlobalPK)
	newPKBytes := pairing.G2.ToBytes(newKeys[0].GlobalPK)
	oldPKG1Bytes := pairing.G1.ToBytes(oldKeys[0].GlobalPKG1)
	newPKG1Bytes := pairing.G1.ToBytes(newKeys[0].GlobalPKG1)
	pairingMu.Unlock()
	if string(oldPKBytes) != string(newPKBytes) {
		t.Fatal("global PK changed after DKRS")
	}
	if string(oldPKG1Bytes) != string(newPKG1Bytes) {
		t.Fatal("global PK G1 projection changed after DKRS")
	}

	m := Message("dkrs-old-message")
	mPrime := Message("dkrs-new-message")
	r := RandomPointG1(pp)
	hashOutput, err := Hash(newKeys[0], m, r)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if !Verify(newKeys[0], hashOutput.Hash, m, hashOutput.CheckValue) {
		t.Fatal("hash verify failed for pre-adaptation message")
	}

	shares := make([]*AdaptationShare, 0, newT+1)
	for i := 0; i < newT+1; i++ {
		s, err := PartialAdapt(newKeys[i], m, mPrime)
		if err != nil {
			t.Fatalf("PartialAdapt failed: %v", err)
		}
		if !PartialVerify(newKeys[i], s, m, mPrime) {
			t.Fatal("PartialVerify failed for reshared key")
		}
		shares = append(shares, s)
	}

	rPrime, err := Combine(pp, shares, newT, newN, hashOutput, m, mPrime)
	if err != nil {
		t.Fatalf("Combine failed: %v", err)
	}
	if !Verify(newKeys[0], hashOutput.Hash, mPrime, rPrime) {
		t.Fatal("post-DKRS adaptation verification failed")
	}
}

func TestDKRSConstraints(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if _, _, err := CreateDKRSDeal(pp, 1, big.NewInt(1), 2, 2, 6); err == nil {
		t.Fatal("expected CreateDKRSDeal to fail when newN is not a power of two")
	}

	oldKeys, err := SimulatedDistKG(pp, 2, 8)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}
	deal, _, err := CreateDKRSDeal(pp, 1, oldKeys[0].SecretShare, 2, 2, 8)
	if err != nil {
		t.Fatalf("CreateDKRSDeal failed: %v", err)
	}
	deal.OldNumParties = big.NewInt(8)

	_, err = DKRSExtract(pp, oldKeys[0], 1, []int{1}, map[int]*DKRSDeal{1: deal}, map[int]*big.Int{})
	if err == nil {
		t.Fatal("expected DKRSExtract to fail when share is missing")
	}
}
