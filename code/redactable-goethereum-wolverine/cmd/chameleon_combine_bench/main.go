package main

import (
    "flag"
    "fmt"
    "math/big"
    "time"

    "github.com/ethereum/go-ethereum/chameleon"
)

func main() {
    var k int
    var iter int
    flag.IntVar(&k, "k", 3, "number of shares to combine (this sets n=k and t=k-1)")
    flag.IntVar(&iter, "iter", 20, "measurements iterations")
    flag.Parse()

    if k < 1 {
        fmt.Println("k must be >= 1")
        return
    }

    // Set parameters: we simulate n = k parties and threshold t = k-1
    n := k
    t := k - 1

    pp, err := chameleon.Setup()
    if err != nil {
        fmt.Println("Setup error:", err)
        return
    }

    keys, err := chameleon.SimulatedDistKG(pp, t, n)
    if err != nil {
        fmt.Println("SimulatedDistKG error:", err)
        return
    }

    // Use first key to compute initial hashOutput (h,r)
    key := keys[0]
    m := chameleon.Message([]byte("combine-bench-message"))
    mPrime := chameleon.Message([]byte("combine-bench-message-prime"))

    // Generate an r (random point) and compute hashOutput
    r := chameleon.RandomPointG1(pp)
    hashOut, err := chameleon.Hash(key, m, r)
    if err != nil {
        fmt.Println("Hash error:", err)
        return
    }

    // Prepare shares by calling PartialAdapt for each key (simulates real shares)
    sharesAll := make([]*chameleon.AdaptationShare, 0, n)
    for i := 0; i < n; i++ {
        s, err := chameleon.PartialAdapt(keys[i], m, mPrime)
        if err != nil {
            fmt.Printf("PartialAdapt error for idx %d: %v\n", i, err)
            return
        }
        sharesAll = append(sharesAll, s)
    }

    // choose first k shares for Combine
    shares := sharesAll[:k]

    // Warmup
    for i := 0; i < 3; i++ {
        if _, err := chameleon.Combine(pp, shares, t, n, hashOut, m, mPrime); err != nil {
            fmt.Println("Combine warmup error:", err)
            return
        }
    }

    var total time.Duration
    var min time.Duration = time.Hour
    var max time.Duration

    for i := 0; i < iter; i++ {
        start := time.Now()
        rPrime, err := chameleon.Combine(pp, shares, t, n, hashOut, m, mPrime)
        dur := time.Since(start)
        if err != nil {
            fmt.Println("Combine error:", err)
            return
        }
        // use rPrime to avoid compiler optimizing call away
        _ = big.NewInt(0).SetBytes(rPrime.Bytes())

        //fmt.Printf("iter %2d: %s\n", i+1, dur)
        total += dur
        if dur < min {
            min = dur
        }
        if dur > max {
            max = dur
        }
    }

    avg := time.Duration(int64(total) / int64(iter))
    fmt.Printf("\nCombine timing (k=%d,t=%d,n=%d): avg=%s min=%s max=%s (N=%d)\n", k, t, n, avg, min, max, iter)
}
