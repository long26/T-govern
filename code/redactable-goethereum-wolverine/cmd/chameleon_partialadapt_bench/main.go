package main

import (
    "fmt"
    "time"

    "github.com/ethereum/go-ethereum/chameleon"
)

func main() {
    pp, err := chameleon.Setup()
    if err != nil {
        fmt.Println("Setup error:", err)
        return
    }

    // 模拟门限密钥（t=1,n=3），并选择一个成员 key
    keys, err := chameleon.SimulatedDistKG(pp, 1, 3)
    if err != nil {
        fmt.Println("SimulatedDistKG error:", err)
        return
    }
    key := keys[0]

    m := chameleon.Message([]byte("original message for partial adapt"))
    mPrime := chameleon.Message([]byte("adapted message"))

    // 预热几次
    for i := 0; i < 3; i++ {
        share, err := chameleon.PartialAdapt(key, m, mPrime)
        if err != nil {
            fmt.Println("PartialAdapt warmup error:", err)
            return
        }
        if !chameleon.PartialVerify(key, share, m, mPrime) {
            fmt.Println("PartialVerify failed during warmup")
            return
        }
    }

    const N = 1000


    var total time.Duration
    var min time.Duration = time.Hour
    var max time.Duration

    // For PartialVerify timing
    var totalVerify time.Duration
    var minVerify time.Duration = time.Hour
    var maxVerify time.Duration

    for i := 0; i < N; i++ {
        start := time.Now()
        share, err := chameleon.PartialAdapt(key, m, mPrime)
        dur := time.Since(start)
        if err != nil {
            fmt.Println("PartialAdapt error at iter", i, err)
            return
        }

        // 单独计时 PartialVerify
        vStart := time.Now()
        ok := chameleon.PartialVerify(key, share, m, mPrime)
        vDur := time.Since(vStart)

        if !ok {
            fmt.Println("PartialVerify failed at iter", i)
            return
        }

      //  fmt.Printf("iter %3d: adapt=%s verify=%s\n", i+1, dur, vDur)

        // accumulate adapt stats
        total += dur
        if dur < min {
            min = dur
        }
        if dur > max {
            max = dur
        }

        // accumulate verify stats
        totalVerify += vDur
        if vDur < minVerify {
            minVerify = vDur
        }
        if vDur > maxVerify {
            maxVerify = vDur
        }
    }

    avg := time.Duration(int64(total) / int64(N))
    avgVerify := time.Duration(int64(totalVerify) / int64(N))
    fmt.Printf("\nPartialAdapt timing: avg=%s min=%s max=%s (N=%d)\n", avg, min, max, N)
    fmt.Printf("PartialVerify timing: avg=%s min=%s max=%s (N=%d)\n", avgVerify, minVerify, maxVerify, N)
}
