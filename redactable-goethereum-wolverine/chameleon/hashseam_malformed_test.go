package chameleon

import (
	"math/big"
	"testing"
)

// Regression: 恶意 deal.A(某 96B 块非法) 曾使 pureVerifyDistKGShare panic
// (nil 点进 MulScalar)。修复后必须 (false, err)。
func TestVerifyDistKGShareRejectsMalformedCommitment(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatal(err)
	}
	deal, _, err := CreateDistKGDeal(pp, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	// 破坏第一个承诺块中段(避开可读前缀) — 非法曲线点
	deal.A[40] ^= 0xff
	deal.A[41] ^= 0xff
	ok, err := VerifyDistKGShare(pp, deal, big.NewInt(1), 1)
	if ok {
		t.Fatal("malformed commitment must not verify")
	}
	if err == nil {
		t.Fatal("malformed commitment must return an error, not (false, nil)")
	}
	t.Logf("rejected with: %v", err)
}
