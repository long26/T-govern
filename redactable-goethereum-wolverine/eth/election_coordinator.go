package eth

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

// BoundaryEvent represents the outcome of detecting and processing an epoch boundary.
type BoundaryEvent struct {
	Epoch            uint64          `json:"epoch"` // Retained for compatibility (equals FinalizedEpoch)
	FinalizedEpoch   uint64          `json:"finalized_epoch"`
	TargetEpoch      uint64          `json:"target_epoch"`
	PreviousBoundary uint64          `json:"previous_boundary"`
	CurrentBoundary  uint64          `json:"current_boundary"`
	BoundaryBlock    *types.Block    `json:"-"`
	Tau              common.Hash     `json:"tau"`
	NextTau          common.Hash     `json:"next_tau"`
	ValidSolutions   []Solution      `json:"valid_solutions"`
	ElectionResult   *ElectionResult `json:"election_result,omitempty"`
	Err              error           `json:"error,omitempty"`
}

// BoundaryHandler is a callback invoked when an epoch boundary is reached and processed.
type BoundaryHandler func(event *BoundaryEvent) error

// BoundaryCoordinator observes canonical chain progress, identifies epoch boundaries,
// scans solutions, and executes committee elections deterministically and idempotently.
// It guarantees that each boundary is processed at most once.
type BoundaryCoordinator struct {
	mu sync.RWMutex

	cfg        ElectionConfig
	reader     BlockReader
	nodeMapper func(CandidateID) (NodeID, bool)
	handlers   []BoundaryHandler

	lastProcessedBoundary uint64
	processedBoundaries   map[uint64]bool

	quit chan struct{}
	done chan struct{}
}

// NewBoundaryCoordinator creates a new BoundaryCoordinator with the given election configuration,
// block reader, and optional node ID mapper.
func NewBoundaryCoordinator(
	cfg ElectionConfig,
	reader BlockReader,
	nodeMapper func(CandidateID) (NodeID, bool),
) *BoundaryCoordinator {
	processed := make(map[uint64]bool)
	processed[cfg.InitialBoundary] = true

	return &BoundaryCoordinator{
		cfg:                   cfg,
		reader:                reader,
		nodeMapper:            nodeMapper,
		lastProcessedBoundary: cfg.InitialBoundary,
		processedBoundaries:   processed,
	}
}

// RegisterHandler sets the callback to be invoked when a boundary is processed.
// It replaces any previously registered handlers.
func (c *BoundaryCoordinator) RegisterHandler(h BoundaryHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h != nil {
		c.handlers = []BoundaryHandler{h}
	} else {
		c.handlers = nil
	}
}

// AddHandler appends an additional callback to be invoked when a boundary is processed.
func (c *BoundaryCoordinator) AddHandler(h BoundaryHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h != nil {
		c.handlers = append(c.handlers, h)
	}
}

// LastProcessedBoundary returns the highest boundary block number that has been processed.
func (c *BoundaryCoordinator) LastProcessedBoundary() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastProcessedBoundary
}

// IsBoundaryProcessed returns true if the specified boundary block number has already been processed.
func (c *BoundaryCoordinator) IsBoundaryProcessed(boundary uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.processedBoundaries[boundary]
}

// Config returns the current ElectionConfig.
func (c *BoundaryCoordinator) Config() ElectionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// ProcessChainHead checks if the given canonical head block has crossed one or more epoch boundaries.
// If one or more boundaries are reached, it scans the corresponding open interval(s), derives tau,
// verifies solutions, executes deterministic election, and dispatches BoundaryEvents.
//
// Guaranteed properties:
// - Strictly idempotent: duplicate or old head blocks produce no events and do not re-scan.
// - Handles batch progression: if head jumped past multiple boundaries, each boundary is processed in order.
// - Thread-safe.
func (c *BoundaryCoordinator) ProcessChainHead(head *types.Block) ([]*BoundaryEvent, error) {
	if head == nil {
		return nil, errors.New("nil head block")
	}

	c.mu.Lock()
	if c.cfg.EpochLength < 2 {
		c.mu.Unlock()
		return nil, errors.New("invalid epoch length: must be >= 2")
	}

	headNum := head.NumberU64()
	var (
		events   []*BoundaryEvent
		handlers = make([]BoundaryHandler, len(c.handlers))
		loopErr  error
	)
	copy(handlers, c.handlers)

	for {
		nextBoundary := c.lastProcessedBoundary + c.cfg.EpochLength
		if nextBoundary > headNum {
			break
		}

		if c.processedBoundaries[nextBoundary] {
			c.lastProcessedBoundary = nextBoundary
			continue
		}

		prevBoundary := c.lastProcessedBoundary

		// Determine the boundary block
		var boundaryBlock *types.Block
		if nextBoundary == headNum {
			boundaryBlock = head
		} else {
			boundaryBlock = c.reader.GetBlockByNumber(nextBoundary)
			if boundaryBlock == nil {
				loopErr = fmt.Errorf("missing boundary block at height %d", nextBoundary)
				break
			}
		}

		// Determine the previous boundary block (needed to derive tau)
		prevBlock := c.reader.GetBlockByNumber(prevBoundary)
		if prevBlock == nil {
			loopErr = fmt.Errorf("missing previous boundary block at height %d", prevBoundary)
			break
		}

		finalizedEpoch := (nextBoundary - c.cfg.InitialBoundary) / c.cfg.EpochLength
		targetEpoch := finalizedEpoch + 1
		tau := DeriveTau(prevBlock.Hash(), finalizedEpoch)
		nextTau := DeriveTau(boundaryBlock.Hash(), targetEpoch)

		// Scan solutions in open interval (prevBoundary, nextBoundary)
		solutions, scanErr := ScanEpochSolutions(
			c.reader,
			prevBoundary,
			nextBoundary,
			finalizedEpoch,
			tau,
			c.cfg.Target,
			c.cfg.SystemAddress,
			c.nodeMapper,
		)

		var electResult *ElectionResult
		var eventErr error
		if scanErr != nil {
			eventErr = scanErr
		} else {
			electResult, eventErr = ElectCommittee(
				solutions,
				c.cfg.CommitteeSize,
				c.cfg.Threshold,
				targetEpoch,
				nextBoundary,
				c.nodeMapper,
			)
		}

		event := &BoundaryEvent{
			Epoch:            finalizedEpoch,
			FinalizedEpoch:   finalizedEpoch,
			TargetEpoch:      targetEpoch,
			PreviousBoundary: prevBoundary,
			CurrentBoundary:  nextBoundary,
			BoundaryBlock:    boundaryBlock,
			Tau:              tau,
			NextTau:          nextTau,
			ValidSolutions:   solutions,
			ElectionResult:   electResult,
			Err:              eventErr,
		}

		c.processedBoundaries[nextBoundary] = true
		c.lastProcessedBoundary = nextBoundary
		events = append(events, event)
	}
	c.mu.Unlock()

	// Invoke external handlers outside coordinator lock to prevent deadlocks
	for _, h := range handlers {
		if h == nil {
			continue
		}
		for _, ev := range events {
			if hErr := h(ev); hErr != nil {
				log.Error("Boundary handler returned error", "boundary", ev.CurrentBoundary, "err", hErr)
			}
		}
	}

	return events, loopErr
}

// StartObserver launches an asynchronous goroutine listening to chain-head notifications from
// sub/headCh. This runs entirely at the eth protocol layer, outside any core blockchain locks.
func (c *BoundaryCoordinator) StartObserver(sub event.Subscription, headCh <-chan core.ChainHeadEvent) {
	c.mu.Lock()
	if c.quit != nil {
		c.mu.Unlock()
		return
	}
	c.quit = make(chan struct{})
	c.done = make(chan struct{})
	c.mu.Unlock()

	var subErrCh <-chan error
	if sub != nil {
		subErrCh = sub.Err()
	}

	go func() {
		defer close(c.done)
		if sub != nil {
			defer sub.Unsubscribe()
		}
		for {
			select {
			case ev, ok := <-headCh:
				if !ok {
					return
				}
				if ev.Block != nil {
					_, _ = c.ProcessChainHead(ev.Block)
				}
			case <-c.quit:
				return
			case err, ok := <-subErrCh:
				if !ok {
					subErrCh = nil
					continue
				}
				if err != nil {
					log.Warn("Chain head subscription error in boundary coordinator", "err", err)
					return
				}
			}
		}
	}()
}

// StopObserver stops the background observer loop if running.
func (c *BoundaryCoordinator) StopObserver() {
	c.mu.Lock()
	if c.quit == nil {
		c.mu.Unlock()
		return
	}
	close(c.quit)
	done := c.done
	c.quit = nil
	c.mu.Unlock()

	<-done
}
