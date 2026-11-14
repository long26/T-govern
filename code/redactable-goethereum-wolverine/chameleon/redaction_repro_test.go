package chameleon

import (
	"fmt"
	"math/big"
	"testing"
)

// TestRedactionEditRepro 复现 c32n33 编辑校验失败（eth/handler.go:827
// `chameleon.Verify(key, oldTxHash, newMsg, newR)` 失败）。
//
// 生产流程（eth/validator.go ProposeModification + eth/handler.go 校验）与
// 现有 TestNITCHAdaptation 有三处差异，本测试用矩阵一次覆盖，定位是哪个因子破坏：
//   1) 规模：生产 t=16/n=32，现有测试 t=2/n=8
//   2) 校验用的 r：生产用交易级 ComputeTransactionR 的 oldR，
//      现有测试用 hashOutput.CheckValue（Hash 的*输出*）
//   3) h：生产是 oldTx.Hash()（32 字节截断交易哈希），现有测试用完整 hashOutput.Hash（GT）
//
// 生产组合 = (scale 16/32, verifyR=oldR-input, h=truncated)。若该组合 FAIL 即复现。
func TestRedactionEditRepro(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	cases := []struct{ t, n int }{
		{2, 8}, // 现有测试规模（已知 PASS）
		{4, 16},
		{16, 32}, // 生产规模
	}
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("scale_t%d_n%d", c.t, c.n), func(t *testing.T) {
			keys, err := SimulatedDistKG(pp, c.t, c.n)
			if err != nil {
				t.Fatalf("SimulatedDistKG(t=%d,n=%d): %v", c.t, c.n, err)
			}
			sysKey := keys[0] // 共享 GlobalPK，等同生产 GetSystemNITCHKey()
			oldMsg := Message("redactable-tx-hash-input-old")
			newMsg := Message("redactable-tx-hash-input-new")

			// 生产：oldR 来自交易级 ComputeTransactionR；对照：随机点
			for _, oldRsrc := range []string{"ComputeTransactionR", "RandomPointG1"} {
				var oldR *big.Int
				switch oldRsrc {
				case "ComputeTransactionR":
					oldR, err = ComputeTransactionR(sysKey, big.NewInt(1337), []byte("signed-tx-without-r"))
				case "RandomPointG1":
					oldR = RandomPointG1(pp)
				}
				if err != nil {
					t.Fatalf("oldR(%s): %v", oldRsrc, err)
				}

				// 生产：hashOutput 用 (oldMsg, oldR) 重算 —— 与 validator.go:106 一致
				hashOutput, err := Hash(sysKey, oldMsg, oldR)
				if err != nil {
					t.Fatalf("Hash(oldR=%s): %v", oldRsrc, err)
				}

				// 两种 h：完整 GT（测试口径） vs 32 字节截断（区块哈希口径）
				hVariants := []struct {
					name string
					h    *big.Int
				}{
					{"hFull", hashOutput.Hash},
					{"hTrunc32", truncateToHashBytes(hashOutput.Hash)},
				}
				for _, hv := range hVariants {
					// 两种校验 r：oldR 输入（生产口径） vs CheckValue 输出（测试口径）
					rVariants := []struct {
						name string
						r    *big.Int
					}{
						{"verifyR=oldR-input", oldR},
						{"verifyR=CheckValue-output", hashOutput.CheckValue},
					}
					for _, rv := range rVariants {
						// 旧块校验（handler.go:815）
						oldOK := Verify(sysKey, hv.h, oldMsg, rv.r)

						// 锻造：t+1 个份额 → Combine → newR（validator.go:122/178）
						shares := make([]*AdaptationShare, 0, c.t+1)
						for i := 0; i < c.t+1; i++ {
							sh, err := PartialAdapt(keys[i], oldMsg, newMsg)
							if err != nil {
								t.Fatalf("PartialAdapt i=%d: %v", i, err)
							}
							shares = append(shares, sh)
						}
						newR, err := Combine(pp, shares, c.t, c.n, hashOutput, oldMsg, newMsg)
						if err != nil {
							t.Fatalf("Combine: %v", err)
						}
						// 编辑校验（handler.go:827） —— 生产里这一步 FAIL
						newOK := Verify(sysKey, hv.h, newMsg, newR)

						tag := fmt.Sprintf("oldR=%s/%s/%s", oldRsrc, hv.name, rv.name)
						t.Logf("[%s] oldVerify=%v newVerify=%v  (newR bytes=%d)", tag, oldOK, newOK, len(newR.Bytes()))

						// 仅在旧块也校验通过时，才要求编辑校验通过（否则 r 与 h 本就不匹配，不构成复现）
						if oldOK && !newOK {
							t.Errorf("REPRO [%s]: oldVerify=TRUE but newVerify=FALSE  ← handler.go:827 failure",
								tag)
						}
					}
				}
			}
		})
	}
}
