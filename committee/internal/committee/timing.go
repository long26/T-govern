package committee

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// timingEnabled turns on per-event latency anchors. Off by default: the anchors
// are for measurement runs, not production, and emitting them per signature
// would dominate the very latency being measured.
//
// Set COMMITTEE_TIMING=1 to enable. Records carry a Unix-nano timestamp so a
// collector can pair events across coordinator and signer processes without
// relying on wall-clock formatting.
var timingEnabled = os.Getenv("COMMITTEE_TIMING") != ""

// timingSeq disambiguates repeated events for the same session (a signer may
// legitimately emit several if the coordinator restarts a session).
var timingSeq uint64

// timing emits one measurement anchor. `fields` are alternating key/value
// pairs, formatted space-separated to match the harness's E2E_TIMING shape.
func timing(event, sessionID string, fields ...any) {
	if !timingEnabled {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "E2E_TIMING stage=committee event=%s session_id=%s ts_ns=%d seq=%d",
		event, sessionID, time.Now().UnixNano(), atomic.AddUint64(&timingSeq, 1))
	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(&b, " %v=%v", fields[i], fields[i+1])
	}
	log.Print(b.String())
}
