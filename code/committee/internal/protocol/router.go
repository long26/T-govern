package protocol

import (
	"sync"

	"committee-mvp/internal/wire"
)

// HandlerFunc handles a decoded envelope routed by message type.
type HandlerFunc func(msg wire.Envelope)

// Router dispatches wire envelopes to registered message handlers.
type Router struct {
	mu       sync.RWMutex
	handlers map[wire.MessageType][]HandlerFunc
}

func NewRouter() *Router {
	return &Router{handlers: make(map[wire.MessageType][]HandlerFunc)}
}

func (r *Router) Register(msgType wire.MessageType, handler HandlerFunc) {
	if handler == nil {
		return
	}
	r.mu.Lock()
	r.handlers[msgType] = append(r.handlers[msgType], handler)
	r.mu.Unlock()
}

// Dispatch returns true when at least one handler consumed the message type.
func (r *Router) Dispatch(msg wire.Envelope) bool {
	r.mu.RLock()
	handlers := append([]HandlerFunc(nil), r.handlers[msg.Type]...)
	r.mu.RUnlock()
	if len(handlers) == 0 {
		return false
	}
	for _, handler := range handlers {
		handler(msg)
	}
	return true
}
