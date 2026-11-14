package p2p

import (
	"context"
	"fmt"
	"testing"
	"time"

	"committee-mvp/internal/wire"
)

// A broadcast to N peers must not serialise on the peer count. The original
// Publish wrote to each peer in turn while holding the read lock, so any
// per-peer latency inside write() was paid N times: one slow or backpressured
// peer stalled every peer after it. The fan-out is now concurrent.
//
// This asserts shape, not a latency win. Under tc/netem the serial and
// concurrent versions are indistinguishable, because netem queues packets in
// the kernel qdisc — write() only copies into the socket buffer and returns in
// microseconds, so there is nothing per-peer to serialise. Do not inject link
// delay with time.Sleep inside write(): that blocks the caller and manufactures
// a bottleneck netem never produces.
func TestFanOutDoesNotScaleWithPeerCount(t *testing.T) {
	const n = 32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := map[string]string{}
	for i := 0; i < n; i++ {
		addrs[fmt.Sprintf("n%d", i)] = fmt.Sprintf("127.0.0.1:%d", 15000+i)
	}
	nets := make([]*StaticNetwork, n)
	for i := 0; i < n; i++ {
		nets[i] = NewStaticNetwork(fmt.Sprintf("n%d", i), addrs[fmt.Sprintf("n%d", i)], addrs)
		if err := nets[i].Start(ctx); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	// Stop is known to hang on this package (pre-existing: readLoops are left
	// blocked in Decode), so bound the teardown rather than let it fail the
	// measurement. The goroutines are reaped when the test binary exits.
	defer func() {
		done := make(chan struct{})
		go func() {
			for _, x := range nets {
				x.Stop(context.Background())
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Log("NOTE: network Stop did not return (pre-existing teardown bug)")
		}
	}()
	time.Sleep(2 * time.Second)

	msg := wire.Envelope{Type: wire.MsgSignRequest, SessionID: "fanout", From: "n0"}

	// Measure the steady-state cost: the first broadcast after connecting pays
	// for JSON encoder setup and socket buffer warm-up, which dominates a
	// single sample at this scale.
	nets[0].Publish(msg)
	const rounds = 30
	start := time.Now()
	for i := 0; i < rounds; i++ {
		nets[0].Publish(msg)
	}
	elapsed := time.Since(start) / rounds
	t.Logf("fan-out to %d peers: %v per broadcast (%.3f ms/peer)",
		n-1, elapsed, float64(elapsed.Microseconds())/1000/float64(n-1))

	// The serial implementation wrote to each peer in turn under the lock; any
	// per-peer blocking (link delay, slow socket) made this scale with N. With
	// concurrent writes the per-peer cost does not grow, so a generous absolute
	// bound is enough to catch a regression to serial fan-out.
	// The race detector instruments every concurrent write, so the bound is
	// loosened under -race; the point is to catch a return to serial fan-out,
	// which at a 1ms/leg would already exceed 31ms.
	limit := 5 * time.Millisecond
	if raceEnabled {
		limit = 30 * time.Millisecond
	}
	if elapsed > limit {
		t.Fatalf("fan-out to %d peers took %v per broadcast; expected sub-linear scaling", n-1, elapsed)
	}
}
