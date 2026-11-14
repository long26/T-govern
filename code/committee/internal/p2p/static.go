package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"committee-mvp/internal/wire"
)

type peerWriter struct {
	conn net.Conn
	enc  *json.Encoder
	mu   sync.Mutex
}

func (w *peerWriter) write(msg wire.Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(msg); err != nil {
		return err
	}
	return nil
}

// stopGracePeriod bounds how long Stop waits for in-flight connection
// goroutines before giving up. A dialLoop legitimately redials while a peer is
// being torn down, so a reconnect landing between the stopped flag and
// stopWg.Wait() must not block shutdown forever.
const stopGracePeriod = 3 * time.Second

// StaticNetwork is a TCP static network abstraction for MVP bootstrap.
type StaticNetwork struct {
	nodeID     string
	listenAddr string
	peers      map[string]string

	listener net.Listener

	mu      sync.RWMutex
	writers map[string]*peerWriter
	// conns tracks every open connection so Stop can close both directions.
	// Writers only cover outbound dials; without this the read loops block on
	// Decode forever and stopWg.Wait() never returns.
	conns map[net.Conn]struct{}
	// stopped is set under mu before stopWg.Wait so late registrations are
	// rejected instead of racing the Wait.
	stopped bool
	// live tracks goroutines that will call stopWg.Done, for diagnosis.
	live int
	// dialing tracks dialLoop instances still running.
	dialing int
	inCh    chan wire.Envelope
	closed  chan struct{}
	stopWg  sync.WaitGroup
	once    sync.Once
}

func NewStaticNetwork(nodeID, listenAddr string, peers map[string]string) *StaticNetwork {
	peerMap := make(map[string]string, len(peers))
	for id, addr := range peers {
		peerMap[id] = addr
	}
	return &StaticNetwork{
		nodeID:     nodeID,
		listenAddr: listenAddr,
		peers:      peerMap,
		writers:    make(map[string]*peerWriter, len(peerMap)),
		conns:      make(map[net.Conn]struct{}),
		inCh:       make(chan wire.Envelope, 512),
		closed:     make(chan struct{}),
	}
}

func (n *StaticNetwork) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", n.listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", n.listenAddr, err)
	}
	n.listener = ln

	n.stopWg.Add(1)
	go func() {
		defer n.stopWg.Done()
		n.acceptLoop()
	}()

	for peerID, addr := range n.peers {
		if peerID == n.nodeID {
			continue
		}
		n.stopWg.Add(1)
		go func(pid, paddr string) {
			defer n.stopWg.Done()
			n.dialLoop(ctx, pid, paddr)
		}(peerID, addr)
	}
	return nil
}

func (n *StaticNetwork) Stop(context.Context) error {
	n.once.Do(func() {
		close(n.closed)
		if n.listener != nil {
			_ = n.listener.Close()
		}
		n.mu.Lock()
		n.stopped = true
		for id, w := range n.writers {
			_ = w.conn.Close()
			delete(n.writers, id)
		}
		for c := range n.conns {
			_ = c.Close()
		}
		n.mu.Unlock()
		// Bound the wait: dialLoop legitimately redials while a peer is being
		// torn down, and a reconnect that lands between the stopped flag and
		// this Wait would otherwise block forever. Shutdown is best-effort —
		// the process is exiting, and a leaked readLoop on a closed conn costs
		// nothing.
		waited := make(chan struct{})
		go func() {
			n.stopWg.Wait()
			close(waited)
		}()
		select {
		case <-waited:
		case <-time.After(stopGracePeriod):
			log.Printf("p2p stop: connection goroutines still parked after %v; proceeding anyway", stopGracePeriod)
		}
		close(n.inCh)
	})
	return nil
}

func (n *StaticNetwork) Publish(msg wire.Envelope) {
	select {
	case <-n.closed:
		return
	default:
	}
	if msg.To == "" || msg.To == n.nodeID {
		n.pushInbound(msg)
	}

	n.mu.RLock()
	defer n.mu.RUnlock()
	if msg.To != "" {
		if msg.To == n.nodeID {
			return
		}
		if w, ok := n.writers[msg.To]; ok {
			if err := w.write(msg); err != nil {
				log.Printf("p2p send failed to=%s err=%v", msg.To, err)
			}
		}
		return
	}
	// Fan out concurrently. Writing to each peer in turn under the lock meant a
	// slow or backpressured peer stalled every peer after it, so one broadcast
	// could cost N times the per-peer block. The writers hold independent
	// mutexes and are safe to run in parallel; peers are collected first so the
	// read lock is released before the writes.
	peers := make([]string, 0, len(n.writers))
	writers := make([]*peerWriter, 0, len(n.writers))
	for peerID, w := range n.writers {
		if peerID == n.nodeID {
			continue
		}
		peers = append(peers, peerID)
		writers = append(writers, w)
	}
	n.mu.RUnlock()
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(peerID string, w *peerWriter) {
			defer wg.Done()
			if err := w.write(msg); err != nil {
				log.Printf("p2p broadcast failed to=%s err=%v", peerID, err)
			}
		}(peers[i], writers[i])
	}
	wg.Wait()
	n.mu.RLock()
}

func (n *StaticNetwork) pushInbound(msg wire.Envelope) {
	select {
	case <-n.closed:
		return
	case n.inCh <- msg:
	default:
	}
}

func (n *StaticNetwork) Subscribe() <-chan wire.Envelope {
	return n.inCh
}

func (n *StaticNetwork) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.closed:
				return
			default:
			}
			log.Printf("p2p accept failed err=%v", err)
			continue
		}
		if !n.registerConn(conn) {
			_ = conn.Close()
			return
		}
		go func(c net.Conn) {
			defer func() { n.mu.Lock(); n.live--; n.mu.Unlock() }()
			defer n.stopWg.Done()
			defer n.removeConn(c)
			n.readLoop(c)
		}(conn)
	}
}

func (n *StaticNetwork) dialLoop(ctx context.Context, peerID, peerAddr string) {
	n.markDialing()
	defer n.unmarkDialing()
	backoff := 250 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.closed:
			return
		default:
		}

		// Watch shutdown while connecting. net.DialTimeout is uncancellable, so
		// a peer that never answers parked this goroutine for the full timeout
		// while Stop waited on stopWg; cancelling on n.closed releases it
		// immediately.
		dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
		stopWatch := make(chan struct{})
		go func() {
			select {
			case <-n.closed:
				dialCancel()
			case <-stopWatch:
			}
		}()
		var dialer net.Dialer
		conn, err := dialer.DialContext(dialCtx, "tcp", peerAddr)
		close(stopWatch)
		dialCancel()
		if err != nil {
			select {
			case <-n.closed:
				return
			default:
			}
			select {
			case <-n.closed:
				return
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 250 * time.Millisecond

		writer := &peerWriter{conn: conn, enc: json.NewEncoder(conn)}
		// Register the conn and the waiter before publishing the writer, so a
		// concurrent Stop either sees the conn (and closes it) or observes the
		// WaitGroup count first — never the reverse.
		if !n.registerConn(conn) {
			_ = conn.Close()
			return
		}
		n.setWriter(peerID, writer)
		go func(c net.Conn) {
			defer func() { n.mu.Lock(); n.live--; n.mu.Unlock() }()
			defer n.stopWg.Done()
			defer n.removeConn(c)
			n.readLoop(c)
		}(conn)

		for {
			select {
			case <-ctx.Done():
				n.clearWriter(peerID, conn)
				_ = conn.Close()
				return
			case <-n.closed:
				n.clearWriter(peerID, conn)
				_ = conn.Close()
				return
			default:
				if !n.isConnAlive(conn) {
					n.clearWriter(peerID, conn)
					_ = conn.Close()
					// Stop may have closed this connection; re-check before
					// redialing so shutdown does not race the reconnect.
					select {
					case <-n.closed:
						return
					case <-ctx.Done():
						return
					default:
					}
					goto REDIAL
				}
				time.Sleep(500 * time.Millisecond)
			}
		}
	REDIAL:
	}
}

func (n *StaticNetwork) readLoop(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()
	dec := json.NewDecoder(conn)
	for {
		var env wire.Envelope
		if err := dec.Decode(&env); err != nil {
			return
		}
		n.pushInbound(env)
	}
}

// registerConn tracks a connection and its readLoop under the same lock Stop
// uses, so Stop either sees the connection (and closes it) or observes the
// WaitGroup count first. Returning false means the network is shutting down and
// the caller must not start a readLoop.
//
// stopWg.Add is only valid before Wait begins, so it is performed here rather
// than by the caller; a bare Add racing Stop's Wait is what left readLoops
// blocked in Decode and hung Stop.
func (n *StaticNetwork) registerConn(c net.Conn) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return false
	}
	if n.conns == nil {
		n.conns = make(map[net.Conn]struct{})
	}
	n.conns[c] = struct{}{}
	n.stopWg.Add(1)
	n.live++
	return true
}

func (n *StaticNetwork) markDialing() {
	n.mu.Lock()
	n.dialing++
	n.mu.Unlock()
}

func (n *StaticNetwork) unmarkDialing() {
	n.mu.Lock()
	n.dialing--
	n.mu.Unlock()
}

// Diagnostics reports the outstanding WaitGroup-dependent goroutines.
func (n *StaticNetwork) Diagnostics() (live, dialing int) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.live, n.dialing
}

func (n *StaticNetwork) removeConn(c net.Conn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.conns, c)
}

func (n *StaticNetwork) setWriter(peerID string, w *peerWriter) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if old, ok := n.writers[peerID]; ok {
		_ = old.conn.Close()
	}
	n.writers[peerID] = w
}

func (n *StaticNetwork) clearWriter(peerID string, conn net.Conn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	w, ok := n.writers[peerID]
	if !ok {
		return
	}
	if w.conn == conn {
		delete(n.writers, peerID)
	}
}

func (n *StaticNetwork) isConnAlive(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	if err := conn.SetReadDeadline(time.Now()); err != nil {
		return false
	}
	_ = conn.SetReadDeadline(time.Time{})
	return true
}
