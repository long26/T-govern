package storage

import (
	"encoding/json"
	"os"
	"sync"
)

var (
	storage = make(map[string]string)
	mu      sync.RWMutex // 读写锁保证并发安全
)

// 初始化时从文件加载数据
func init() {
	data, err := os.ReadFile("storage.json")
	if err == nil {
		_ = json.Unmarshal(data, &storage)
	}
}

// 保存数据到内存并持久化到文件
func Save(key string, value string) error {
	mu.Lock()
	defer mu.Unlock()

	storage[key] = value

	// 序列化并写入文件
	data, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("storage.json", data, 0644)
}

// 读取数据
func Get(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	return storage[key]
}

// 删除数据
func Delete(key string) error {
	mu.Lock()
	defer mu.Unlock()

	delete(storage, key)

	data, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("storage.json", data, 0644)
}
