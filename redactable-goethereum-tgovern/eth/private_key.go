package eth

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"io/ioutil"
	"sync"
)

var (
	keyCache    map[common.Address]*ecdsa.PrivateKey
	cacheMutex  sync.RWMutex
	cacheLoaded bool
)

func init() {
	// 初始化缓存
	keyCache = make(map[common.Address]*ecdsa.PrivateKey)
	cacheLoaded = false
}
func loadKeyCache() error {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// 如果已经加载过缓存，直接返回
	if cacheLoaded {
		return nil
	}
	keyFilePath := "all_privatekeys.json"
	// 读取文件内容
	data, err := ioutil.ReadFile(keyFilePath)
	if err != nil {
		return fmt.Errorf("读取密钥文件失败: %v", err)
	}

	// 解析JSON
	var keyStore struct {
		Nodes map[string]struct {
			Address    string `json:"address"`
			PrivateKey string `json:"privateKey"`
		} `json:"nodes"`
	}

	if err := json.Unmarshal(data, &keyStore); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	// 遍历所有节点，添加到缓存
	for _, nodeInfo := range keyStore.Nodes {
		address := common.HexToAddress(nodeInfo.Address)

		privateKeyBytes, err := hex.DecodeString(nodeInfo.PrivateKey)
		if err != nil {
			return fmt.Errorf("解码私钥失败: %v", err)
		}

		privateKey, err := crypto.ToECDSA(privateKeyBytes)
		if err != nil {
			return fmt.Errorf("转换ECDSA私钥失败: %v", err)
		}

		keyCache[address] = privateKey
	}

	cacheLoaded = true
	return nil
}

// GetPrivateKeyFromAddress 根据地址获取私钥
func GetPrivateKeyFromAddress(address common.Address) (*ecdsa.PrivateKey, error) {
	// 确保缓存已加载
	if !cacheLoaded {
		if err := loadKeyCache(); err != nil {
			return nil, err
		}
	}

	cacheMutex.RLock()
	defer cacheMutex.RUnlock()

	// 从缓存中查找私钥
	if privateKey, exists := keyCache[address]; exists {
		return privateKey, nil
	}

	// 如果缓存中没有，尝试重新加载文件（可能文件已更新）
	if err := reloadKeyCache(); err != nil {
		return nil, err
	}

	// 再次尝试查找
	if privateKey, exists := keyCache[address]; exists {
		return privateKey, nil
	}

	return nil, errors.New("未找到地址对应的私钥")
}

// 重新加载密钥缓存
func reloadKeyCache() error {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// 清空缓存
	for k := range keyCache {
		delete(keyCache, k)
	}
	cacheLoaded = false

	// 重新加载
	return loadKeyCache()
}
