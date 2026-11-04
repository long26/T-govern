package rpc

import (
	"encoding/json"
	"testing"
)

// The harness sends these exact keys; a rename would silently disable the
// blocking path and reintroduce polling-quantized measurements.
func TestGetRedactAuthorizationParamsWaitWireFormat(t *testing.T) {
	var p GetRedactAuthorizationParams
	if err := decodeParams(json.RawMessage(`{"request_id":"r1","wait":true,"wait_timeout_ms":90000}`), &p); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !p.Wait {
		t.Fatal(`"wait" did not decode to true`)
	}
	if p.WaitTimeoutMS != 90000 {
		t.Fatalf("wait_timeout_ms = %d, want 90000", p.WaitTimeoutMS)
	}

	// Absent fields must keep the non-blocking behaviour that existing
	// callers rely on.
	var zero GetRedactAuthorizationParams
	if err := decodeParams(json.RawMessage(`{"request_id":"r1"}`), &zero); err != nil {
		t.Fatalf("decode minimal params: %v", err)
	}
	if zero.Wait || zero.WaitTimeoutMS != 0 {
		t.Fatalf("absent wait fields must default off, got %+v", zero)
	}
}
