package main

import (
    "fmt"
    "time"

    "github.com/ethereum/go-ethereum/chameleon"
)

func main() {
    // 准备参数
    pp, err := chameleon.Setup()
    if err != nil {
        fmt.Println("Setup error:", err)
        return
    }

    // 生成小规模分布式密钥（t=1, n=3）用于基准
    keys, err := chameleon.SimulatedDistKG(pp, 5, 10)
    if err != nil {
        fmt.Println("SimulatedDistKG error:", err)
        return
    }
    key := keys[0]

    msg := chameleon.Message([]byte("benchmark message"))

    // 使用确定性或随机的 r；这里使用随机点生成器
    r := chameleon.RandomPointG1(pp)

    // 预热若干次
    for i := 0; i < 3; i++ {
        if _, err := chameleon.Hash(key, msg, r); err != nil {
            fmt.Println("Hash error (warmup):", err)
            return
        }
    }

    // 正式测量若干次
    const N = 1000
    var total time.Duration
    var min time.Duration = time.Hour
    var max time.Duration

    for i := 0; i < N; i++ {
        start := time.Now()
        _, err := chameleon.Hash(key, msg, r)
        if err != nil {
            fmt.Println("Hash error:", err)
            return
        }
        d := time.Since(start)
        //fmt.Printf("iter %2d: %s\n", i+1, d)
        total += d
        if d < min {
            min = d
        }
        if d > max {
            max = d
        }
    }

    avg := time.Duration(int64(total) / int64(N))
    fmt.Printf("\nResults: avg=%s min=%s max=%s (N=%d)\n", avg, min, max, N)
}
