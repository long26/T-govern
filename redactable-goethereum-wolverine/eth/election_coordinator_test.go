package eth

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestBoundaryCoordinatorIdempotence(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	epochLength := uint64(10)
	initialBoundary := uint64(0)

	// Add blocks 0..15
	for b := uint64(0); b <= 15; b++ {
		reader.addBlock(b)
	}

	// Calculate tau for epoch 1 from block 0
	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)

	// Add valid solutions in blocks 2 and 5
	tx1 := helperCreateValidSolutionTx(t, 0, systemAddr, 1, "node-1", []byte{1}, tau1)
	tx2 := helperCreateValidSolutionTx(t, 0, systemAddr, 1, "node-2", []byte{2}, tau1)
	reader.addBlock(2, tx1)
	reader.addBlock(5, tx2)

	cfg := ElectionConfig{
		EpochLength:     epochLength,
		InitialBoundary: initialBoundary,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	// 1. Feed block 8 -> no boundary reached
	events, err := coordinator.ProcessChainHead(reader.GetBlockByNumber(8))
	if err != nil {
		t.Fatalf("unexpected error on block 8: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for block 8, got %d", len(events))
	}

	// 2. Feed block 10 -> boundary 10 reached!
	events, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(10))
	if err != nil {
		t.Fatalf("unexpected error on block 10: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event for block 10, got %d", len(events))
	}

	ev := events[0]
	if ev.Epoch != 1 {
		t.Errorf("expected epoch 1, got %d", ev.Epoch)
	}
	if ev.FinalizedEpoch != 1 {
		t.Errorf("expected FinalizedEpoch 1, got %d", ev.FinalizedEpoch)
	}
	if ev.TargetEpoch != 2 {
		t.Errorf("expected TargetEpoch 2, got %d", ev.TargetEpoch)
	}
	if ev.PreviousBoundary != 0 || ev.CurrentBoundary != 10 {
		t.Errorf("boundary interval mismatch: got (%d, %d)", ev.PreviousBoundary, ev.CurrentBoundary)
	}
	if ev.Err != nil {
		t.Fatalf("event had error: %v", ev.Err)
	}
	if ev.ElectionResult == nil || len(ev.ElectionResult.Descriptor.Members) != 2 {
		t.Fatalf("expected election result with 2 members, got %+v", ev.ElectionResult)
	}
	if ev.ElectionResult.Descriptor.Epoch != 2 {
		t.Errorf("expected descriptor epoch 2 (target epoch), got %d", ev.ElectionResult.Descriptor.Epoch)
	}
	expectedCommID := ComputeCommitteeID(2, ev.ElectionResult.Descriptor.Members, 1, 10)
	if ev.ElectionResult.Descriptor.CommitteeID != expectedCommID {
		t.Errorf("expected descriptor CommitteeID %s, got %s", expectedCommID.Hex(), ev.ElectionResult.Descriptor.CommitteeID.Hex())
	}

	// 3. Feed block 10 again -> MUST BE IDEMPOTENT (0 events)
	events, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(10))
	if err != nil {
		t.Fatalf("unexpected error on duplicate block 10: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events on duplicate block 10, got %d", len(events))
	}

	// 4. Feed block 9 (lower/reorg head) -> 0 events
	events, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(9))
	if err != nil {
		t.Fatalf("unexpected error on lower block 9: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events on block 9, got %d", len(events))
	}

	// 5. Feed block 11 -> 0 events (next boundary is 20)
	events, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(11))
	if err != nil {
		t.Fatalf("unexpected error on block 11: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events on block 11, got %d", len(events))
	}

	// 6. Feed block 10 a third time -> still 0 events
	events, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(10))
	if err != nil {
		t.Fatalf("unexpected error on 3rd block 10: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events on 3rd block 10, got %d", len(events))
	}

	// Verify coordinator state inspection
	if coordinator.LastProcessedBoundary() != 10 {
		t.Errorf("expected LastProcessedBoundary 10, got %d", coordinator.LastProcessedBoundary())
	}
	if !coordinator.IsBoundaryProcessed(10) {
		t.Errorf("expected boundary 10 to be marked processed")
	}
	if coordinator.IsBoundaryProcessed(20) {
		t.Errorf("boundary 20 should not be marked processed")
	}
}

func TestBoundaryCoordinatorBatchProgression(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress
	epochLength := uint64(5)
	initialBoundary := uint64(0)

	// Add blocks 0..25
	for b := uint64(0); b <= 25; b++ {
		reader.addBlock(b)
	}

	// Epoch 1 tau (from block 0)
	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)
	reader.addBlock(2, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "node-e1-a", []byte{1}, tau1))
	reader.addBlock(3, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "node-e1-b", []byte{2}, tau1))

	// Epoch 2 tau (from block 5)
	tau2 := DeriveTau(reader.GetBlockByNumber(5).Hash(), 2)
	reader.addBlock(7, helperCreateValidSolutionTx(t, 0, systemAddr, 2, "node-e2-a", []byte{3}, tau2))
	reader.addBlock(8, helperCreateValidSolutionTx(t, 0, systemAddr, 2, "node-e2-b", []byte{4}, tau2))

	// Epoch 3 tau (from block 10)
	tau3 := DeriveTau(reader.GetBlockByNumber(10).Hash(), 3)
	reader.addBlock(12, helperCreateValidSolutionTx(t, 0, systemAddr, 3, "node-e3-a", []byte{5}, tau3))
	reader.addBlock(13, helperCreateValidSolutionTx(t, 0, systemAddr, 3, "node-e3-b", []byte{6}, tau3))

	// Epoch 4 tau (from block 15)
	tau4 := DeriveTau(reader.GetBlockByNumber(15).Hash(), 4)
	reader.addBlock(17, helperCreateValidSolutionTx(t, 0, systemAddr, 4, "node-e4-a", []byte{7}, tau4))
	reader.addBlock(18, helperCreateValidSolutionTx(t, 0, systemAddr, 4, "node-e4-b", []byte{8}, tau4))

	cfg := ElectionConfig{
		EpochLength:     epochLength,
		InitialBoundary: initialBoundary,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	var receivedEvents []*BoundaryEvent
	var mu sync.Mutex
	coordinator.RegisterHandler(func(ev *BoundaryEvent) error {
		mu.Lock()
		defer mu.Unlock()
		receivedEvents = append(receivedEvents, ev)
		return nil
	})

	// 1. Initial head at block 3 -> no boundaries crossed
	evs, err := coordinator.ProcessChainHead(reader.GetBlockByNumber(3))
	if err != nil || len(evs) != 0 {
		t.Fatalf("expected 0 events at block 3, got %d, err=%v", len(evs), err)
	}

	// 2. Batch jump: head jumps from block 3 to block 12!
	// Boundaries 5 and 10 should be processed sequentially!
	evs, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(12))
	if err != nil {
		t.Fatalf("unexpected error on batch jump to block 12: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 boundary events on jump to 12, got %d", len(evs))
	}

	// Check boundary 5 (epoch 1)
	if evs[0].Epoch != 1 || evs[0].CurrentBoundary != 5 || evs[0].PreviousBoundary != 0 {
		t.Errorf("event 0 boundary mismatch: %+v", evs[0])
	}
	if evs[0].FinalizedEpoch != 1 || evs[0].TargetEpoch != 2 {
		t.Errorf("event 0 epoch mismatch: finalized %d, target %d", evs[0].FinalizedEpoch, evs[0].TargetEpoch)
	}
	if evs[0].ElectionResult.Descriptor.Epoch != 2 {
		t.Errorf("event 0 descriptor epoch mismatch: got %d, want 2", evs[0].ElectionResult.Descriptor.Epoch)
	}
	if evs[0].Err != nil {
		t.Errorf("event 0 has error: %v", evs[0].Err)
	}
	if len(evs[0].ValidSolutions) != 2 {
		t.Errorf("event 0 expected 2 solutions, got %d", len(evs[0].ValidSolutions))
	}

	// Check boundary 10 (epoch 2)
	if evs[1].Epoch != 2 || evs[1].CurrentBoundary != 10 || evs[1].PreviousBoundary != 5 {
		t.Errorf("event 1 boundary mismatch: %+v", evs[1])
	}
	if evs[1].FinalizedEpoch != 2 || evs[1].TargetEpoch != 3 {
		t.Errorf("event 1 epoch mismatch: finalized %d, target %d", evs[1].FinalizedEpoch, evs[1].TargetEpoch)
	}
	if evs[1].ElectionResult.Descriptor.Epoch != 3 {
		t.Errorf("event 1 descriptor epoch mismatch: got %d, want 3", evs[1].ElectionResult.Descriptor.Epoch)
	}
	if evs[1].Err != nil {
		t.Errorf("event 1 has error: %v", evs[1].Err)
	}
	if len(evs[1].ValidSolutions) != 2 {
		t.Errorf("event 1 expected 2 solutions, got %d", len(evs[1].ValidSolutions))
	}

	if coordinator.LastProcessedBoundary() != 10 {
		t.Errorf("expected LastProcessedBoundary 10, got %d", coordinator.LastProcessedBoundary())
	}

	// 3. Repeat head at block 12 -> 0 events
	evs, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(12))
	if err != nil || len(evs) != 0 {
		t.Fatalf("expected 0 events on repeat block 12, got %d, err=%v", len(evs), err)
	}

	// 4. Batch jump: head jumps to block 22!
	// Boundaries 15 and 20 should be processed sequentially!
	evs, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(22))
	if err != nil {
		t.Fatalf("unexpected error on batch jump to block 22: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 boundary events on jump to 22, got %d", len(evs))
	}

	if evs[0].Epoch != 3 || evs[0].CurrentBoundary != 15 {
		t.Errorf("event 0 boundary mismatch: %+v", evs[0])
	}
	if evs[0].FinalizedEpoch != 3 || evs[0].TargetEpoch != 4 || evs[0].ElectionResult.Descriptor.Epoch != 4 {
		t.Errorf("event 0 epoch fields mismatch: %+v", evs[0])
	}
	if evs[1].Epoch != 4 || evs[1].CurrentBoundary != 20 {
		t.Errorf("event 1 boundary mismatch: %+v", evs[1])
	}
	if evs[1].FinalizedEpoch != 4 || evs[1].TargetEpoch != 5 || evs[1].ElectionResult.Descriptor.Epoch != 5 {
		t.Errorf("event 1 epoch fields mismatch: %+v", evs[1])
	}

	if coordinator.LastProcessedBoundary() != 20 {
		t.Errorf("expected LastProcessedBoundary 20, got %d", coordinator.LastProcessedBoundary())
	}

	// Verify registered callback also received all 4 events
	mu.Lock()
	defer mu.Unlock()
	if len(receivedEvents) != 4 {
		t.Errorf("expected 4 events received by handler, got %d", len(receivedEvents))
	}
}

type mockSubscription struct {
	errCh chan error
}

func newMockSubscription() *mockSubscription {
	return &mockSubscription{errCh: make(chan error, 1)}
}

func (s *mockSubscription) Err() <-chan error {
	return s.errCh
}

func (s *mockSubscription) Unsubscribe() {}

func TestBoundaryCoordinatorObserverAsync(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress

	for b := uint64(0); b <= 10; b++ {
		reader.addBlock(b)
	}

	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)
	reader.addBlock(1, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "async-node-1", []byte{1}, tau1))
	reader.addBlock(2, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "async-node-2", []byte{2}, tau1))

	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	eventCh := make(chan *BoundaryEvent, 5)
	coordinator.RegisterHandler(func(ev *BoundaryEvent) error {
		eventCh <- ev
		return nil
	})

	headCh := make(chan core.ChainHeadEvent, 5)
	sub := newMockSubscription()

	coordinator.StartObserver(sub, headCh)

	// Send non-boundary head
	headCh <- core.ChainHeadEvent{Block: reader.GetBlockByNumber(5)}
	time.Sleep(10 * time.Millisecond)
	select {
	case ev := <-eventCh:
		t.Fatalf("unexpected event received for block 5: %+v", ev)
	default:
	}

	// Send boundary head
	headCh <- core.ChainHeadEvent{Block: reader.GetBlockByNumber(10)}

	select {
	case ev := <-eventCh:
		if ev.Epoch != 1 || ev.CurrentBoundary != 10 {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for boundary event")
	}

	// Stop observer cleanly
	coordinator.StopObserver()

	// Ensure calling StopObserver multiple times is safe
	coordinator.StopObserver()
}

func TestBoundaryCoordinatorMissingBlocks(t *testing.T) {
	reader := newMockBlockReader()
	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   DefaultSolutionSystemAddress,
	}
	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	// Missing block 0 (prevBlock)
	reader.addBlock(10)
	_, err := coordinator.ProcessChainHead(reader.GetBlockByNumber(10))
	if err == nil {
		t.Error("expected error when previous boundary block is missing, got nil")
	}

	// Missing block 10 (when head is 15 and reader lacks block 10)
	reader.addBlock(0)
	reader.addBlock(15)
	delete(reader.blocks, 10)
	_, err = coordinator.ProcessChainHead(reader.GetBlockByNumber(15))
	if err == nil {
		t.Error("expected error when boundary block is missing during batch jump, got nil")
	}
}

func TestBoundaryCoordinatorInvalidInputs(t *testing.T) {
	reader := newMockBlockReader()

	// 1. Nil head
	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
	}
	coord := NewBoundaryCoordinator(cfg, reader, nil)
	if _, err := coord.ProcessChainHead(nil); err == nil {
		t.Error("expected error on nil head, got nil")
	}

	header := &types.Header{Number: big.NewInt(1)}
	block := types.NewBlock(header, nil, nil, nil)

	// 2. EpochLength == 0 (must be >= 2)
	cfg0 := ElectionConfig{EpochLength: 0}
	coord0 := NewBoundaryCoordinator(cfg0, reader, nil)
	if _, err := coord0.ProcessChainHead(block); err == nil {
		t.Error("expected error on epochLength == 0, got nil")
	}

	// 3. EpochLength == 1 (must be >= 2 because open interval requires at least 1 block)
	cfg1 := ElectionConfig{EpochLength: 1}
	coord1 := NewBoundaryCoordinator(cfg1, reader, nil)
	if _, err := coord1.ProcessChainHead(block); err == nil {
		t.Error("expected error on epochLength == 1, got nil")
	}
}

func TestBoundaryCoordinatorHandlerNoDeadlock(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress

	for b := uint64(0); b <= 10; b++ {
		reader.addBlock(b)
	}

	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)
	reader.addBlock(1, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "deadlock-node-1", []byte{1}, tau1))
	reader.addBlock(2, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "deadlock-node-2", []byte{2}, tau1))

	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	handlerCalled := false
	// Register handler that makes re-entrant calls into coordinator methods.
	// If ProcessChainHead held coordinator mutex while calling handler, this would deadlock!
	coordinator.RegisterHandler(func(ev *BoundaryEvent) error {
		handlerCalled = true
		_ = coordinator.LastProcessedBoundary()
		_ = coordinator.IsBoundaryProcessed(ev.CurrentBoundary)
		_ = coordinator.Config()
		return nil
	})

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_, err := coordinator.ProcessChainHead(reader.GetBlockByNumber(10))
		if err != nil {
			t.Errorf("ProcessChainHead failed: %v", err)
		}
	}()

	select {
	case <-doneCh:
		if !handlerCalled {
			t.Error("expected handler to be called")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK DETECTED: handler calling coordinator methods blocked indefinitely")
	}
}

func TestBoundaryCoordinatorObserverClosedSubErr(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress

	for b := uint64(0); b <= 10; b++ {
		reader.addBlock(b)
	}

	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)
	reader.addBlock(1, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "sub-closed-1", []byte{1}, tau1))
	reader.addBlock(2, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "sub-closed-2", []byte{2}, tau1))

	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	eventCh := make(chan *BoundaryEvent, 5)
	coordinator.RegisterHandler(func(ev *BoundaryEvent) error {
		eventCh <- ev
		return nil
	})

	headCh := make(chan core.ChainHeadEvent, 5)
	sub := newMockSubscription()
	// Close subscription error channel immediately!
	// Observer must handle closed sub.Err() channel without busy-spinning or silently exiting.
	close(sub.errCh)

	coordinator.StartObserver(sub, headCh)

	// Send boundary head
	headCh <- core.ChainHeadEvent{Block: reader.GetBlockByNumber(10)}

	select {
	case ev := <-eventCh:
		if ev.Epoch != 1 || ev.CurrentBoundary != 10 {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event with closed sub.Err() channel")
	}

	coordinator.StopObserver()
}

func TestBoundaryCoordinatorObserverNilSub(t *testing.T) {
	reader := newMockBlockReader()
	systemAddr := DefaultSolutionSystemAddress

	for b := uint64(0); b <= 10; b++ {
		reader.addBlock(b)
	}

	tau1 := DeriveTau(reader.GetBlockByNumber(0).Hash(), 1)
	reader.addBlock(1, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "nil-sub-1", []byte{1}, tau1))
	reader.addBlock(2, helperCreateValidSolutionTx(t, 0, systemAddr, 1, "nil-sub-2", []byte{2}, tau1))

	cfg := ElectionConfig{
		EpochLength:     10,
		InitialBoundary: 0,
		CommitteeSize:   2,
		Threshold:       1,
		Target:          MaxTarget,
		SystemAddress:   systemAddr,
	}

	coordinator := NewBoundaryCoordinator(cfg, reader, nil)

	eventCh := make(chan *BoundaryEvent, 5)
	coordinator.RegisterHandler(func(ev *BoundaryEvent) error {
		eventCh <- ev
		return nil
	})

	headCh := make(chan core.ChainHeadEvent, 5)
	// sub is nil
	coordinator.StartObserver(nil, headCh)

	headCh <- core.ChainHeadEvent{Block: reader.GetBlockByNumber(10)}

	select {
	case ev := <-eventCh:
		if ev.Epoch != 1 || ev.CurrentBoundary != 10 {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event with nil sub")
	}

	coordinator.StopObserver()
}
