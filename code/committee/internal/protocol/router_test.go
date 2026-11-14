package protocol

import (
	"testing"

	"committee-mvp/internal/wire"
)

func TestRouterDispatch(t *testing.T) {
	router := NewRouter()
	called := false
	router.Register(wire.MsgSignRequest, func(msg wire.Envelope) {
		called = true
		if msg.SessionID != "s1" {
			t.Fatalf("unexpected session id: %s", msg.SessionID)
		}
	})

	ok := router.Dispatch(wire.Envelope{Type: wire.MsgSignRequest, SessionID: "s1"})
	if !ok {
		t.Fatalf("expected dispatch to find handler")
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

func TestRouterDispatchNoHandler(t *testing.T) {
	router := NewRouter()
	ok := router.Dispatch(wire.Envelope{Type: wire.MsgAggResult})
	if ok {
		t.Fatalf("expected dispatch to return false when no handler exists")
	}
}

func TestRouterDispatchMultipleHandlers(t *testing.T) {
	router := NewRouter()
	order := make([]int, 0, 2)
	router.Register(wire.MsgSignResponse, func(wire.Envelope) {
		order = append(order, 1)
	})
	router.Register(wire.MsgSignResponse, func(wire.Envelope) {
		order = append(order, 2)
	})

	ok := router.Dispatch(wire.Envelope{Type: wire.MsgSignResponse})
	if !ok {
		t.Fatalf("expected dispatch to find handlers")
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("unexpected handler order: %v", order)
	}
}
