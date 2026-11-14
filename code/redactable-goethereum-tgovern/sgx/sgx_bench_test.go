package sgx

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"testing"
	"time"
)

// benchConfig 定义了 (总节点, 在职TEE节点) 的测试配置
type benchConfig struct {
	TotalNodes int // 总节点数 N
	ActiveTEEs int // 在职 TEE 节点数 (threshold + 1)
}

// 测试配置组: (总节点, 在职TEE节点) = (8,5), (16,9), (32,17), (64,33)
var benchConfigs = []benchConfig{
	{8, 5},
	{16, 9},
	{32, 17},
	{64, 33},
}

// threshold 从 activeTEEs 计算: threshold = activeTEEs - 1
func (c benchConfig) threshold() int {
	return c.ActiveTEEs - 1
}

// qualSet 返回参与 extract 的 dealer 索引集合 (1-based)
func (c benchConfig) qualSet() []int {
	qual := make([]int, c.ActiveTEEs)
	for i := 0; i < c.ActiveTEEs; i++ {
		qual[i] = i + 1
	}
	return qual
}

func configName(c benchConfig) string {
	return fmt.Sprintf("N=%d_active=%d", c.TotalNodes, c.ActiveTEEs)
}

type dealerOutput struct {
	Index  int
	A      []byte
	B0     []byte
	Shares []*big.Int
}

// runDKGFlow 执行完整 DKG 流程: CreateDeal x N -> VerifyShare -> Extract 数据准备
// 需要 enclave 已初始化 (Init)
func runDKGFlow(cfg benchConfig) ([]dealerOutput, []int32, []byte, []byte, []byte, error) {
	n := cfg.TotalNodes
	t := cfg.threshold()
	qual := cfg.qualSet()

	dealers := make([]dealerOutput, n)
	for i := 0; i < n; i++ {
		result, err := DistKGCreateDeal(i+1, t, n, nil, nil, nil, nil, 0, 0)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("DistKGCreateDeal(%d): %w", i+1, err)
		}
		parsed := ParseSGXShares(result.SharesSerialized, n)
		shares := make([]*big.Int, n)
		for j, s := range parsed {
			shares[j] = s
		}
		dealers[i] = dealerOutput{i + 1, result.CommitmentsA, result.CommitmentB0, shares}
	}

	for receiver := 1; receiver <= n; receiver++ {
		for dealer := 0; dealer < n; dealer++ {
			share := dealers[dealer].Shares[receiver-1]
			shareBytes := make([]byte, 48)
			share.FillBytes(shareBytes)
			valid, err := DistKGVerifyShare(receiver, dealers[dealer].Index, t, n,
				dealers[dealer].A, dealers[dealer].B0, shareBytes, nil, nil, nil, nil, 0, 0)
			if err != nil {
				return nil, nil, nil, nil, nil, fmt.Errorf("VerifyShare(r=%d,d=%d): %w", receiver, dealers[dealer].Index, err)
			}
			if !valid {
				return nil, nil, nil, nil, nil, fmt.Errorf("VerifyShare(r=%d,d=%d) invalid", receiver, dealers[dealer].Index)
			}
		}
	}

	g1PtSize := 96
	scalarSize := 48
	qualIndices := make([]int32, len(qual))
	b0Data := make([]byte, 0, len(qual)*192)
	a0Data := make([]byte, 0, len(qual)*g1PtSize)
	for idx, i := range qual {
		qualIndices[idx] = int32(i)
		b0Data = append(b0Data, dealers[i-1].B0...)
		a0Data = append(a0Data, dealers[i-1].A[:g1PtSize]...)
	}
	sharesData := make([]byte, 0, len(qual)*scalarSize)
	for _, i := range qual {
		shareBytes := make([]byte, scalarSize)
		dealers[i-1].Shares[0].FillBytes(shareBytes)
		sharesData = append(sharesData, shareBytes...)
	}

	return dealers, qualIndices, b0Data, a0Data, sharesData, nil
}

// =====================================================================
// 注意：每个 benchmark 函数独立 Init/Close，避免跨配置 enclave 状态冲突
// 对于多配置测试，使用独立的顶层 benchmark 函数
// =====================================================================

// =====================================================================
// DistKG CreateDeal Benchmarks (每个配置独立)
// =====================================================================

func BenchmarkDistKGCreateDeal_N8_Active5(b *testing.B)  { benchDistKGCreateDeal(b, benchConfigs[0]) }
func BenchmarkDistKGCreateDeal_N16_Active9(b *testing.B) { benchDistKGCreateDeal(b, benchConfigs[1]) }
func BenchmarkDistKGCreateDeal_N32_Active17(b *testing.B) { benchDistKGCreateDeal(b, benchConfigs[2]) }
func BenchmarkDistKGCreateDeal_N64_Active33(b *testing.B) { benchDistKGCreateDeal(b, benchConfigs[3]) }

func benchDistKGCreateDeal(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DistKGCreateDeal(1, t, n, nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DistKGCreateDeal failed: %v", err)
		}
	}
}

// =====================================================================
// DistKG VerifyShare Benchmarks
// =====================================================================

func BenchmarkDistKGVerifyShare_N8_Active5(b *testing.B)  { benchDistKGVerifyShare(b, benchConfigs[0]) }
func BenchmarkDistKGVerifyShare_N16_Active9(b *testing.B) { benchDistKGVerifyShare(b, benchConfigs[1]) }
func BenchmarkDistKGVerifyShare_N32_Active17(b *testing.B) { benchDistKGVerifyShare(b, benchConfigs[2]) }
func BenchmarkDistKGVerifyShare_N64_Active33(b *testing.B) { benchDistKGVerifyShare(b, benchConfigs[3]) }

func benchDistKGVerifyShare(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes

	dealResult, err := DistKGCreateDeal(1, t, n, nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("Setup DistKGCreateDeal failed: %v", err)
	}
	shares := ParseSGXShares(dealResult.SharesSerialized, n)
	shareBytes := make([]byte, 48)
	shares[0].FillBytes(shareBytes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DistKGVerifyShare(1, 1, t, n,
			dealResult.CommitmentsA, dealResult.CommitmentB0, shareBytes,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DistKGVerifyShare failed: %v", err)
		}
	}
}

// =====================================================================
// DistKG Extract Benchmarks
// =====================================================================

func BenchmarkDistKGExtract_N8_Active5(b *testing.B)  { benchDistKGExtract(b, benchConfigs[0]) }
func BenchmarkDistKGExtract_N16_Active9(b *testing.B) { benchDistKGExtract(b, benchConfigs[1]) }
func BenchmarkDistKGExtract_N32_Active17(b *testing.B) { benchDistKGExtract(b, benchConfigs[2]) }
func BenchmarkDistKGExtract_N64_Active33(b *testing.B) { benchDistKGExtract(b, benchConfigs[3]) }

func benchDistKGExtract(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()

	_, qualIndices, b0Data, a0Data, sharesData, err := runDKGFlow(cfg)
	if err != nil {
		b.Fatalf("DKG flow setup failed: %v", err)
	}
	t := cfg.threshold()
	n := cfg.TotalNodes

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DistKGExtract(1, t, n, qualIndices,
			b0Data, nil, a0Data, nil, sharesData, nil,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DistKGExtract failed: %v", err)
		}
	}
}

// =====================================================================
// DKRS CreateDeal Benchmarks
// =====================================================================

func BenchmarkDKRSCreateDeal_N8_Active5(b *testing.B)  { benchDKRSCreateDeal(b, benchConfigs[0]) }
func BenchmarkDKRSCreateDeal_N16_Active9(b *testing.B) { benchDKRSCreateDeal(b, benchConfigs[1]) }
func BenchmarkDKRSCreateDeal_N32_Active17(b *testing.B) { benchDKRSCreateDeal(b, benchConfigs[2]) }
func BenchmarkDKRSCreateDeal_N64_Active33(b *testing.B) { benchDKRSCreateDeal(b, benchConfigs[3]) }

func benchDKRSCreateDeal(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes

	// 先完成 DKG Extract，使 enclave 内有密钥
	_, qualIndices, b0Data, a0Data, sharesData, err := runDKGFlow(cfg)
	if err != nil {
		b.Fatalf("DKG setup failed: %v", err)
	}
	_, err = DistKGExtract(1, t, n, qualIndices,
		b0Data, nil, a0Data, nil, sharesData, nil,
		nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("DKG Extract setup failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DKRSCreateDeal(1, t, n, nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DKRSCreateDeal failed: %v", err)
		}
	}
}

// =====================================================================
// DKRS VerifyShare Benchmarks
// =====================================================================

func BenchmarkDKRSVerifyShare_N8_Active5(b *testing.B)  { benchDKRSVerifyShare(b, benchConfigs[0]) }
func BenchmarkDKRSVerifyShare_N16_Active9(b *testing.B) { benchDKRSVerifyShare(b, benchConfigs[1]) }
func BenchmarkDKRSVerifyShare_N32_Active17(b *testing.B) { benchDKRSVerifyShare(b, benchConfigs[2]) }
func BenchmarkDKRSVerifyShare_N64_Active33(b *testing.B) { benchDKRSVerifyShare(b, benchConfigs[3]) }

func benchDKRSVerifyShare(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes

	// DKG + Extract
	_, qualIndices, b0Data, a0Data, sharesData, err := runDKGFlow(cfg)
	if err != nil {
		b.Fatalf("DKG setup failed: %v", err)
	}
	_, err = DistKGExtract(1, t, n, qualIndices,
		b0Data, nil, a0Data, nil, sharesData, nil,
		nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("DKG Extract setup failed: %v", err)
	}

	// DKRS CreateDeal
	dkrsDeal, err := DKRSCreateDeal(1, t, n, nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("DKRSCreateDeal setup failed: %v", err)
	}
	shares := ParseSGXShares(dkrsDeal.SharesSerialized, n)
	shareBytes := make([]byte, 48)
	shares[0].FillBytes(shareBytes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DKRSVerifyShare(1, 1, t, n,
			dkrsDeal.CommitmentsA, dkrsDeal.CommitmentB0, shareBytes,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DKRSVerifyShare failed: %v", err)
		}
	}
}

// =====================================================================
// DKRS Extract Benchmarks
// =====================================================================

func BenchmarkDKRSExtract_N8_Active5(b *testing.B)  { benchDKRSExtract(b, benchConfigs[0]) }
func BenchmarkDKRSExtract_N16_Active9(b *testing.B) { benchDKRSExtract(b, benchConfigs[1]) }
func BenchmarkDKRSExtract_N32_Active17(b *testing.B) { benchDKRSExtract(b, benchConfigs[2]) }
func BenchmarkDKRSExtract_N64_Active33(b *testing.B) { benchDKRSExtract(b, benchConfigs[3]) }

func benchDKRSExtract(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes
	scalarSize := 48

	// Phase 1: DKG + Extract for all members
	dealers, dkgQualIndices, dkgB0, dkgA0, dkgShares, err := runDKGFlow(cfg)
	if err != nil {
		b.Fatalf("DKG flow failed: %v", err)
	}
	_, err = DistKGExtract(1, t, n, dkgQualIndices,
		dkgB0, nil, dkgA0, nil, dkgShares, nil,
		nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("DKG Extract failed: %v", err)
	}

	// Phase 2: DKRS CreateDeal + VerifyShare
	type dkrsDealer struct {
		Index  int
		A      []byte
		B0     []byte
		Shares []*big.Int
	}
	dkrsDealers := make([]dkrsDealer, n)
	qual := cfg.qualSet()
	for i := 0; i < n; i++ {
		memberSharesData := make([]byte, 0)
		for _, q := range qual {
			shareBytes := make([]byte, scalarSize)
			dealers[q-1].Shares[i].FillBytes(shareBytes)
			memberSharesData = append(memberSharesData, shareBytes...)
		}
		_, err := DistKGExtract(i+1, t, n, dkgQualIndices,
			dkgB0, nil, dkgA0, nil, memberSharesData, nil,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DKG re-extract for member %d: %v", i+1, err)
		}

		result, err := DKRSCreateDeal(i+1, t, n, nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DKRSCreateDeal(%d): %v", i+1, err)
		}
		parsed := ParseSGXShares(result.SharesSerialized, n)
		shares := make([]*big.Int, n)
		for j, s := range parsed {
			shares[j] = s
		}
		dkrsDealers[i] = dkrsDealer{i + 1, result.CommitmentsA, result.CommitmentB0, shares}
	}

	for receiver := 1; receiver <= n; receiver++ {
		for dealer := 0; dealer < n; dealer++ {
			shareBytes := make([]byte, 48)
			dkrsDealers[dealer].Shares[receiver-1].FillBytes(shareBytes)
			_, err := DKRSVerifyShare(receiver, dkrsDealers[dealer].Index, t, n,
				dkrsDealers[dealer].A, dkrsDealers[dealer].B0, shareBytes,
				nil, nil, nil, nil, 0, 0)
			if err != nil {
				b.Fatalf("DKRS VerifyShare: %v", err)
			}
		}
	}

	// Phase 3: 准备 DKRS Extract 数据
	dkrsQual := cfg.qualSet()
	dkrsQualIndices := make([]int32, len(dkrsQual))
	dkrsB0Data := make([]byte, 0)
	dkrsA0Data := make([]byte, 0)
	dkrsSharesData := make([]byte, 0)
	newG1PtSize := len(dkrsDealers[0].A) / (t + 1)
	for idx, i := range dkrsQual {
		dkrsQualIndices[idx] = int32(i)
		deal := dkrsDealers[i-1]
		dkrsB0Data = append(dkrsB0Data, deal.B0...)
		a0End := newG1PtSize
		if a0End > len(deal.A) {
			a0End = len(deal.A)
		}
		dkrsA0Data = append(dkrsA0Data, deal.A[:a0End]...)
		shareBytes := make([]byte, scalarSize)
		deal.Shares[0].FillBytes(shareBytes)
		dkrsSharesData = append(dkrsSharesData, shareBytes...)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DKRSExtract(1, t, n, n, dkrsQualIndices,
			dkrsB0Data, nil, dkrsA0Data, nil, dkrsSharesData, nil,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("DKRSExtract failed: %v", err)
		}
	}
}

// =====================================================================
// PartialAdapt Benchmarks
// =====================================================================

func BenchmarkPartialAdapt_N8_Active5(b *testing.B)  { benchPartialAdapt(b, benchConfigs[0]) }
func BenchmarkPartialAdapt_N16_Active9(b *testing.B) { benchPartialAdapt(b, benchConfigs[1]) }
func BenchmarkPartialAdapt_N32_Active17(b *testing.B) { benchPartialAdapt(b, benchConfigs[2]) }
func BenchmarkPartialAdapt_N64_Active33(b *testing.B) { benchPartialAdapt(b, benchConfigs[3]) }

func benchPartialAdapt(b *testing.B, cfg benchConfig) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	t := cfg.threshold()
	n := cfg.TotalNodes

	// 先完成 DKG Extract，使 enclave 内有变色龙哈希私钥
	_, qualIndices, b0Data, a0Data, sharesData, err := runDKGFlow(cfg)
	if err != nil {
		b.Fatalf("DKG setup failed: %v", err)
	}
	_, err = DistKGExtract(1, t, n, qualIndices,
		b0Data, nil, a0Data, nil, sharesData, nil,
		nil, nil, nil, nil, 0, 0)
	if err != nil {
		b.Fatalf("DKG Extract setup failed: %v", err)
	}

	sourceMsg := []byte("benchmark source transaction data for redaction")
	targetMsg := []byte("benchmark target transaction data after redaction")
	dReq := []byte("benchmark-redaction-request-hash")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := PartialAdapt(sourceMsg, targetMsg,
			nil, nil, nil, nil,
			dReq, nil, nil, nil,
			nil, nil, nil, 0, 0)
		if err != nil {
			b.Fatalf("PartialAdapt failed: %v", err)
		}
	}
}

// =====================================================================
// 轻量级 ecall 基准
// =====================================================================

func BenchmarkEnclaveInit(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if err := Init(); err != nil {
			b.Fatalf("SGX Init failed: %v", err)
		}
		Close()
	}
}

func BenchmarkGetEnclavePK(b *testing.B) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := GetEnclavePK()
		if err != nil {
			b.Fatalf("GetEnclavePK failed: %v", err)
		}
	}
}

func BenchmarkVRFEval(b *testing.B) {
	if err := Init(); err != nil {
		b.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()

	chainID := make([]byte, 32)
	binary.LittleEndian.PutUint64(chainID, 123)
	contractAddress := make([]byte, 32)
	hardwareID := make([]byte, 32)
	vrfPubKey := make([]byte, 32)
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := VRFEval(chainID, contractAddress, 1, hardwareID, vrfPubKey, seed)
		if err != nil {
			b.Fatalf("VRFEval failed: %v", err)
		}
	}
}

// =====================================================================
// 自定义性能测试：详细计时每个操作的耗时
// =====================================================================

func TestSGXPerformanceDetailed(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("SGX Init failed: %v", err)
	}
	defer Close()

	type timingResult struct {
		Operation string
		Config    string
		Duration  time.Duration
		Count     int
		PerOp     time.Duration
	}

	var results []timingResult

	for _, cfg := range benchConfigs {
		n := cfg.TotalNodes
		th := cfg.threshold()
		cfgName := configName(cfg)
		scalarSize := 48

		t.Logf("\n========== Config: %s (threshold=%d) ==========", cfgName, th)

		// --- DistKG CreateDeal ---
		start := time.Now()
		dealers := make([]dealerOutput, n)
		for i := 0; i < n; i++ {
			result, err := DistKGCreateDeal(i+1, th, n, nil, nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("DistKGCreateDeal(%d) failed: %v", i+1, err)
			}
			parsed := ParseSGXShares(result.SharesSerialized, n)
			shares := make([]*big.Int, n)
			for j, s := range parsed {
				shares[j] = s
			}
			dealers[i] = dealerOutput{i + 1, result.CommitmentsA, result.CommitmentB0, shares}
		}
		elapsed := time.Since(start)
		perDeal := elapsed / time.Duration(n)
		t.Logf("  DistKG CreateDeal x%d: total=%v, per_deal=%v", n, elapsed, perDeal)
		results = append(results, timingResult{"DistKG_CreateDeal", cfgName, elapsed, n, perDeal})

		// --- DistKG VerifyShare (member 1 接收所有 dealer 的份额) ---
		start = time.Now()
		for dealer := 0; dealer < n; dealer++ {
			shareBytes := make([]byte, 48)
			dealers[dealer].Shares[0].FillBytes(shareBytes)
			_, err := DistKGVerifyShare(1, dealers[dealer].Index, th, n,
				dealers[dealer].A, dealers[dealer].B0, shareBytes,
				nil, nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("DistKGVerifyShare failed: %v", err)
			}
		}
		elapsed = time.Since(start)
		perVerify := elapsed / time.Duration(n)
		t.Logf("  DistKG VerifyShare x%d: total=%v, per_verify=%v", n, elapsed, perVerify)
		results = append(results, timingResult{"DistKG_VerifyShare", cfgName, elapsed, n, perVerify})

		// --- DistKG Extract ---
		qual := cfg.qualSet()
		g1PtSize := 96
		qualIndices := make([]int32, len(qual))
		b0Data := make([]byte, 0)
		a0Data := make([]byte, 0)
		for idx, i := range qual {
			qualIndices[idx] = int32(i)
			b0Data = append(b0Data, dealers[i-1].B0...)
			a0Data = append(a0Data, dealers[i-1].A[:g1PtSize]...)
		}
		sharesData := make([]byte, 0)
		for _, i := range qual {
			shareBytes := make([]byte, scalarSize)
			dealers[i-1].Shares[0].FillBytes(shareBytes)
			sharesData = append(sharesData, shareBytes...)
		}
		start = time.Now()
		_, err := DistKGExtract(1, th, n, qualIndices,
			b0Data, nil, a0Data, nil, sharesData, nil,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			t.Fatalf("DistKGExtract failed: %v", err)
		}
		elapsed = time.Since(start)
		t.Logf("  DistKG Extract: %v", elapsed)
		results = append(results, timingResult{"DistKG_Extract", cfgName, elapsed, 1, elapsed})

		// --- DKRS CreateDeal ---
		// 让所有 member 完成 extract
		for i := 0; i < n; i++ {
			memberShares := make([]byte, 0)
			for _, q := range qual {
				shareBytes := make([]byte, scalarSize)
				dealers[q-1].Shares[i].FillBytes(shareBytes)
				memberShares = append(memberShares, shareBytes...)
			}
			_, err := DistKGExtract(i+1, th, n, qualIndices,
				b0Data, nil, a0Data, nil, memberShares, nil,
				nil, nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("DKG re-extract(%d) failed: %v", i+1, err)
			}
		}

		type dkrsDealer struct {
			Index  int
			A      []byte
			B0     []byte
			Shares []*big.Int
		}
		dkrsDealers := make([]dkrsDealer, n)
		start = time.Now()
		for i := 0; i < n; i++ {
			result, err := DKRSCreateDeal(i+1, th, n, nil, nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("DKRSCreateDeal(%d) failed: %v", i+1, err)
			}
			parsed := ParseSGXShares(result.SharesSerialized, n)
			shares := make([]*big.Int, n)
			for j, s := range parsed {
				shares[j] = s
			}
			dkrsDealers[i] = dkrsDealer{i + 1, result.CommitmentsA, result.CommitmentB0, shares}
		}
		elapsed = time.Since(start)
		perDKRSDeal := elapsed / time.Duration(n)
		t.Logf("  DKRS CreateDeal x%d: total=%v, per_deal=%v", n, elapsed, perDKRSDeal)
		results = append(results, timingResult{"DKRS_CreateDeal", cfgName, elapsed, n, perDKRSDeal})

		// --- DKRS VerifyShare ---
		start = time.Now()
		for dealer := 0; dealer < n; dealer++ {
			shareBytes := make([]byte, 48)
			dkrsDealers[dealer].Shares[0].FillBytes(shareBytes)
			_, err := DKRSVerifyShare(1, dkrsDealers[dealer].Index, th, n,
				dkrsDealers[dealer].A, dkrsDealers[dealer].B0, shareBytes,
				nil, nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("DKRSVerifyShare failed: %v", err)
			}
		}
		elapsed = time.Since(start)
		perDKRSVerify := elapsed / time.Duration(n)
		t.Logf("  DKRS VerifyShare x%d: total=%v, per_verify=%v", n, elapsed, perDKRSVerify)
		results = append(results, timingResult{"DKRS_VerifyShare", cfgName, elapsed, n, perDKRSVerify})

		// --- DKRS Extract ---
		dkrsQual := cfg.qualSet()
		dkrsQualIndices := make([]int32, len(dkrsQual))
		dkrsB0Data := make([]byte, 0)
		dkrsA0Data := make([]byte, 0)
		dkrsSharesData := make([]byte, 0)
		newG1PtSize := len(dkrsDealers[0].A) / (th + 1)
		for idx, i := range dkrsQual {
			dkrsQualIndices[idx] = int32(i)
			deal := dkrsDealers[i-1]
			dkrsB0Data = append(dkrsB0Data, deal.B0...)
			a0End := newG1PtSize
			if a0End > len(deal.A) {
				a0End = len(deal.A)
			}
			dkrsA0Data = append(dkrsA0Data, deal.A[:a0End]...)
			shareBytes := make([]byte, scalarSize)
			deal.Shares[0].FillBytes(shareBytes)
			dkrsSharesData = append(dkrsSharesData, shareBytes...)
		}
		start = time.Now()
		_, err = DKRSExtract(1, th, n, n, dkrsQualIndices,
			dkrsB0Data, nil, dkrsA0Data, nil, dkrsSharesData, nil,
			nil, nil, nil, nil, 0, 0)
		if err != nil {
			t.Fatalf("DKRSExtract failed: %v", err)
		}
		elapsed = time.Since(start)
		t.Logf("  DKRS Extract: %v", elapsed)
		results = append(results, timingResult{"DKRS_Extract", cfgName, elapsed, 1, elapsed})

		// --- PartialAdapt ---
		sourceMsg := []byte("benchmark source transaction data for redaction")
		targetMsg := []byte("benchmark target transaction data after redaction")
		dReq := []byte("benchmark-redaction-request-hash")

		partialAdaptCount := 10
		start = time.Now()
		for i := 0; i < partialAdaptCount; i++ {
			_, err := PartialAdapt(sourceMsg, targetMsg,
				nil, nil, nil, nil,
				dReq, nil, nil, nil,
				nil, nil, nil, 0, 0)
			if err != nil {
				t.Fatalf("PartialAdapt failed: %v", err)
			}
		}
		elapsed = time.Since(start)
		perAdapt := elapsed / time.Duration(partialAdaptCount)
		t.Logf("  PartialAdapt x%d: total=%v, per_adapt=%v", partialAdaptCount, elapsed, perAdapt)
		results = append(results, timingResult{"PartialAdapt", cfgName, elapsed, partialAdaptCount, perAdapt})
	}

	// 打印汇总表格
	t.Logf("\n========== Performance Summary ==========")
	t.Logf("%-25s %-20s %12s %12s %8s", "Operation", "Config", "Total", "PerOp", "Count")
	t.Logf("%-25s %-20s %12s %12s %8s", "---", "---", "---", "---", "---")
	for _, r := range results {
		t.Logf("%-25s %-20s %12v %12v %8d", r.Operation, r.Config, r.Duration.Round(time.Microsecond), r.PerOp.Round(time.Microsecond), r.Count)
	}
}
