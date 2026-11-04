package eth

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const timingMarkerSchema = 3

// ledgerObserverIdentity returns the explicit ON-equivalent identity injected by
// nitch_perf. Only this role is allowed to emit the SGX L5-aligned ledger headline.
func ledgerObserverIdentity(getenv func(string) string) (int, bool) {
	if strings.TrimSpace(getenv("NITCH_NODE_ROLE")) != "ledger_observer" {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimSpace(getenv("NITCH_NODE_INDEX")))
	if err != nil || index < 1 {
		return 0, false
	}
	return index, true
}

type ledgerObserverTiming struct {
	nodeIndex int
	reqID     string
	block     uint64
	txIndex   uint64
	startedAt time.Time
	finished  bool
	failure   string
}

func beginLedgerObserverTiming(reqID string, block, txIndex uint64) *ledgerObserverTiming {
	nodeIndex, ok := ledgerObserverIdentity(os.Getenv)
	if !ok {
		return nil
	}
	timing := &ledgerObserverTiming{
		nodeIndex: nodeIndex,
		reqID:     reqID,
		block:     block,
		txIndex:   txIndex,
		startedAt: time.Now(),
		failure:   "processing_aborted",
	}
	log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
		"stage", "ledger_update", "event", "tx_modify_received",
		"duration_ns", 0, "join_key", reqID, "block", block, "tx_index", txIndex,
		"node_id", nodeIndex, "node_role", "ledger_observer", "status", "ok")
	return timing
}

func logLedgerObserverDuplicate(reqID string, block, txIndex uint64, reason string) {
	nodeIndex, ok := ledgerObserverIdentity(os.Getenv)
	if !ok {
		return
	}
	log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
		"stage", "ledger_update", "event", "tx_modify_duplicate",
		"duration_ns", 0, "join_key", reqID, "block", block, "tx_index", txIndex,
		"node_id", nodeIndex, "node_role", "ledger_observer",
		"status", "duplicate", "reason", reason)
}

// step records a ledger_update sub-stage breakpoint: duration_ns is the step's
// own cost, elapsed_ns is cumulative since tx_modify_received. Emitted only on
// the observer node (same gate as the headline anchors).
func (timing *ledgerObserverTiming) step(name string, stepStart time.Time) {
	if timing == nil || timing.finished {
		return
	}
	log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
		"stage", "ledger_update", "event", name,
		"duration_ns", time.Since(stepStart).Nanoseconds(),
		"elapsed_ns", time.Since(timing.startedAt).Nanoseconds(),
		"join_key", timing.reqID, "block", timing.block, "tx_index", timing.txIndex,
		"node_id", timing.nodeIndex, "node_role", "ledger_observer", "status", "ok")
}

func (timing *ledgerObserverTiming) fail(reason string) {
	if timing != nil && reason != "" {
		timing.failure = reason
	}
}

// finish records an unsuccessful terminal event. It is intended to be deferred
// immediately after tx_modify_received so every aborted validation/update path is visible.
func (timing *ledgerObserverTiming) finish() {
	if timing == nil || timing.finished {
		return
	}
	timing.finished = true
	log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
		"stage", "ledger_update", "event", "local_block_update_failed",
		"duration_ns", time.Since(timing.startedAt).Nanoseconds(),
		"join_key", timing.reqID, "block", timing.block, "tx_index", timing.txIndex,
		"node_id", timing.nodeIndex, "node_role", "ledger_observer",
		"status", "failed", "reason", timing.failure)
}

func (timing *ledgerObserverTiming) updated(blockHash common.Hash) {
	if timing == nil || timing.finished {
		return
	}
	timing.finished = true
	log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
		"stage", "ledger_update", "event", "local_block_updated",
		"duration_ns", time.Since(timing.startedAt).Nanoseconds(),
		"join_key", timing.reqID, "block", timing.block, "tx_index", timing.txIndex,
		"block_hash", blockHash, "node_id", timing.nodeIndex,
		"node_role", "ledger_observer", "status", "ok")
}
