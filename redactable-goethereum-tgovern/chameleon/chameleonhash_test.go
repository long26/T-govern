package chameleon

import (
	"math/big"
	"testing"
)

func TestChameleonHash(t *testing.T) {
	// 初始化变色龙哈希
	hexStr := "1d1c29cd1834d6d7dfe2d37aeda5ef398e0a3502618be3f4033e4530abaa309f"

	// 创建 big.Int 对象
	P = new(big.Int)

	// 将十六进制字符串解析到 big.Int 中
	if _, success := P.SetString(hexStr, 16); !success {
		panic("解析十六进制字符串失败")
	}
	hexStr1 := "e8e14e68c1a6b6beff169bd76d2f79cc7051a8130c5f1fa019f229855d5184f"

	// 创建 big.Int 对象
	Q = new(big.Int)

	// 将十六进制字符串解析到 big.Int 中
	if _, success := Q.SetString(hexStr1, 16); !success {
		panic("解析十六进制字符串失败")
	}
	hexStr2 := "8acb9db88ef01d45402ec56e8ee0bc5c07c07b40d8ac8c0a45f8408242d266a"

	// 创建 big.Int 对象
	G = new(big.Int)

	// 将十六进制字符串解析到 big.Int 中
	if _, success := G.SetString(hexStr2, 16); !success {
		panic("解析十六进制字符串失败")
	}
	t.Logf("系统参数: P = %x, Q = %x, G = %x", P, Q, G)

	// 生成密钥对
	pubKey, privKey := KeyGen()
	t.Logf("公钥: %x", pubKey)
	t.Logf("私钥: %x", privKey)
	result := make([]byte, 32)
	pubKey.FillBytes(result)
	var str [32]byte
	copy(str[:], result)
	t.Logf("公钥: %x", str)

	// 测试基本哈希功能
	//msg1 := big.NewInt(123)
	//r, _ := GenerateR(Q)
	//t.Logf("消息1: %d, 随机数r: %x", msg1, r)
	//hash1 := Hash(pubKey, msg1, r)
	//t.Logf("哈希值1: %x", hash1)
	//
	//// 验证相同输入产生相同哈希
	//hash1Repeat := Hash(pubKey, msg1, r)
	//t.Logf("重复计算的哈希值1: %x", hash1Repeat)
	//if hash1.Cmp(hash1Repeat) != 0 {
	//	t.Error("相同输入应产生相同的哈希值")
	//}
	//
	//// 测试锻造功能
	//msg2 := big.NewInt(456)
	//t.Logf("消息2: %d", msg2)
	//newR := Forge(privKey, msg1, r, msg2)
	//t.Logf("锻造后的随机数r': %x", newR)
	//hash2 := Hash(pubKey, msg2, newR)
	//t.Logf("锻造后的哈希值2: %x", hash2)
	//
	//// 验证锻造后哈希值相等
	//if hash1.Cmp(hash2) != 0 {
	//	t.Errorf("锻造后的哈希值应相等: 期望 %x, 得到 %x", hash1, hash2)
	//}
	//
	//// 测试错误的私钥
	//wrongPrivKey, _ := KeyGen()
	//t.Logf("错误的私钥: %x", wrongPrivKey)
	//wrongR := Forge(wrongPrivKey, msg1, r, msg2)
	//t.Logf("使用错误私钥锻造的随机数r': %x", wrongR)
	//wrongHash := Hash(pubKey, msg2, wrongR)
	//t.Logf("使用错误私钥锻造的哈希值: %x", wrongHash)
	//
	//// 验证使用错误私钥锻造的哈希值不匹配
	//if hash1.Cmp(wrongHash) == 0 {
	//	t.Error("使用错误私钥锻造应失败")
	//}
	//
	//// 测试边界条件
	//// 1. 使用零消息
	//zeroMsg := big.NewInt(0)
	//t.Logf("零消息测试: msg = %d", zeroMsg)
	//zeroHash := Hash(pubKey, zeroMsg, r)
	//t.Logf("零消息的哈希值: %x", zeroHash)
	//if zeroHash == nil {
	//	t.Error("零消息应该能够正常计算哈希值")
	//}
	//
	//// 2. 使用大数消息
	//bigMsg := new(big.Int).Exp(big.NewInt(2), big.NewInt(1024), nil)
	//t.Logf("大数消息测试: msg = %x", bigMsg)
	//bigHash := Hash(pubKey, bigMsg, r)
	//t.Logf("大数消息的哈希值: %x", bigHash)
	//if bigHash == nil {
	//	t.Error("大数消息应该能够正常计算哈希值")
	//}
	//
	//// 3. 验证不同消息产生不同的原始哈希
	//msg3 := big.NewInt(789)
	//t.Logf("消息3: %d", msg3)
	//hash3 := Hash(pubKey, msg3, r)
	//t.Logf("消息3的哈希值: %x", hash3)
	//if hash1.Cmp(hash3) == 0 {
	//	t.Error("不同消息应产生不同的原始哈希值")
	//}
}
