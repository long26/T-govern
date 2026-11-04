package p2p

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Stop must return within a bounded time even at committee scale, where
// dialLoop redials a peer whose connections were just closed. Before the
// bound, stopWg.Wait() could block indefinitely: every tracked goroutine had
// exited (live=0) yet the counter never reached zero, because a reconnect that
// landed between the stopped flag and the Wait added to the group after waiting
// had begun.
func TestStopReturnsWithinGraceAtCommitteeScale(t *testing.T) {
	const n = 32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := map[string]string{}
	for i := 0; i < n; i++ {
		addrs[fmt.Sprintf("n%d", i)] = fmt.Sprintf("127.0.0.1:%d", 30000+i)
	}
	nets := make([]*StaticNetwork, n)
	for i := 0; i < n; i++ {
		nets[i] = NewStaticNetwork(fmt.Sprintf("n%d", i), addrs[fmt.Sprintf("n%d", i)], addrs)
		if err := nets[i].Start(ctx); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	// Stop early, while many dials are still in flight.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		for _, x := range nets {
			x.Stop(context.Background())
		}
		close(done)
	}()
	select {
	case <-done:
		t.Logf("all 32 Stop returned after %v", time.Since(start))
	case <-time.After(stopGracePeriod + 15*time.Second):
		t.Fatalf("Stop still blocked after %v", time.Since(start))
	}
}

// The grace period must not slow down the normal case: with no peers to redial,
// Stop returns promptly.
func TestStopIsFastWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := map[string]string{"a": "127.0.0.1:31101", "b": "127.0.0.1:31102"}
	na := NewStaticNetwork("a", addrs["a"], addrs)
	nb := NewStaticNetwork("b", addrs["b"], addrs)
	na.Start(ctx)
	nb.Start(ctx)
	time.Sleep(1500 * time.Millisecond)

	start := time.Now()
	done := make(chan struct{})
	go func() { na.Stop(context.Background()); nb.Stop(context.Background()); close(done) }()
	select {
	case <-done:
		if el := time.Since(start); el > stopGracePeriod {
			t.Fatalf("idle Stop took %v; grace period should not be paid when idle", el)
		}
		t.Logf("idle Stop returned after %v", time.Since(start))
	case <-time.After(stopGracePeriod + 5*time.Second):
		t.Fatal("idle Stop blocked")
	}
}
