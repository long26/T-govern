package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
)

func main() {
	var n int
	var t int
	var iter int
	var warmup int

	flag.IntVar(&n, "n", 8, "number of parties (must be power of two)")
	flag.IntVar(&t, "t", 3, "threshold (must satisfy t < n)")
	flag.IntVar(&iter, "iter", 20, "measurement iterations")
	flag.IntVar(&warmup, "warmup", 3, "warmup iterations")
	flag.Parse()

	if n <= 0 {
		fmt.Println("n must be > 0")
		return
	}
	if t < 0 {
		fmt.Println("t must be >= 0")
		return
	}
	if t >= n {
		fmt.Println("t must be < n")
		return
	}
	if iter <= 0 {
		fmt.Println("iter must be > 0")
		return
	}
	if warmup < 0 {
		fmt.Println("warmup must be >= 0")
		return
	}

	pp, err := chameleon.Setup()
	if err != nil {
		fmt.Println("Setup error:", err)
		return
	}

	for i := 0; i < warmup; i++ {
		keys, err := chameleon.SimulatedDistKG(pp, t, n)
		if err != nil {
			fmt.Println("SimulatedDistKG warmup error:", err)
			return
		}
		if len(keys) != n {
			fmt.Printf("SimulatedDistKG warmup unexpected keys length: got %d want %d\n", len(keys), n)
			return
		}
	}

	var total time.Duration
	min := time.Hour
	var max time.Duration

	for i := 0; i < iter; i++ {
		start := time.Now()
		keys, err := chameleon.SimulatedDistKG(pp, t, n)
		dur := time.Since(start)
		if err != nil {
			fmt.Println("SimulatedDistKG error:", err)
			return
		}
		if len(keys) != n {
			fmt.Printf("SimulatedDistKG unexpected keys length: got %d want %d\n", len(keys), n)
			return
		}

		// fmt.Printf("iter %2d: %s\n", i+1, dur)
		total += dur
		if dur < min {
			min = dur
		}
		if dur > max {
			max = dur
		}
	}

	avg := time.Duration(int64(total) / int64(iter))
	fmt.Printf("\nSimulatedDistKG timing (t=%d,n=%d): avg=%s min=%s max=%s (N=%d)\n", t, n, avg, min, max, iter)
}
