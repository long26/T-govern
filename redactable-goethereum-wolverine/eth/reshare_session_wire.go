package eth

import "github.com/ethereum/go-ethereum/core/types"

// ResolveReshareSessionID binds a fixed wire ID back to a known local session name.
func (km *KeyManager) ResolveReshareSessionID(session string) string {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.resolveReshareSessionIDLocked(session)
}

func (km *KeyManager) resolveReshareSessionIDLocked(session string) string {
	if !types.IsReshareWireSessionID(session) {
		return session
	}
	matches := func(local string) bool {
		return local != "" && !types.IsReshareWireSessionID(local) && types.ReshareWireSessionName(local) == session
	}
	for _, local := range []string{km.reshareSessionID, km.reshareLastSession} {
		if matches(local) {
			return local
		}
	}
	// Both caches are already bounded; no unbounded wire-to-local alias table is needed.
	for local := range km.reshareOutboundCache {
		if matches(local) {
			return local
		}
	}
	for local := range km.resharePendingBuffers {
		if matches(local) {
			return local
		}
	}
	return session
}

func (km *KeyManager) restoreResharePendingWireSessionLocked(session string) {
	wireName := types.ReshareWireSessionName(session)
	pending := km.resharePendingBuffers[wireName]
	if pending == nil || wireName == session {
		return
	}
	for _, key := range pending.order {
		msg, ok := pending.messages[key]
		if !ok {
			continue
		}
		msg.SessionID = session
		km.bufferReshareMessageLocked(msg)
	}
	delete(km.resharePendingBuffers, wireName)
	filtered := km.resharePendingOrder[:0]
	for _, name := range km.resharePendingOrder {
		if name != wireName {
			filtered = append(filtered, name)
		}
	}
	km.resharePendingOrder = filtered
}
