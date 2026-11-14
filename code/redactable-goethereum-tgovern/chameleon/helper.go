package chameleon

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// 定义自定义类型包装big.Int
type BigInt struct {
	*big.Int
}

// 实现UnmarshalJSON方法用于反序列化

func (bi *BigInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		if bi.Int == nil {
			bi.Int = new(big.Int)
		}
		bi.Int.SetInt64(0) // 默认值设为 0
		return nil
	}

	// 去除引号并尝试解析为十六进制或十进制
	s := strings.Trim(string(data), `"`)
	if bi.Int == nil {
		bi.Int = new(big.Int)
	}

	// 支持十六进制（如 "0x123"）和十进制（如 "123"）
	if _, ok := bi.Int.SetString(s, 0); !ok { // 第二个参数为 0 表示自动检测进制
		return fmt.Errorf("无效的整数格式: %s", s)
	}
	return nil
}

func (bi *BigInt) MarshalJSON() ([]byte, error) {
	if bi.Int == nil {
		return json.Marshal("0x0") // 默认输出十六进制 0
	}
	// 输出为十六进制字符串（例如 "0x123"）
	return json.Marshal(fmt.Sprintf("0x%x", bi.Int))
}

type PQGData struct {
	PValue BigInt `json:"pValue"`
	QValue BigInt `json:"qValue"`
	GValue BigInt `json:"gValue"`
	RValue BigInt `json:"rValue"`
	PubKey BigInt `json:"pubKey"`
	PriKey BigInt `json:"priKey"`
}
