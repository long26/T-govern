// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/contractService"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/keyStore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/fetcher"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/teehost"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	softResponseLimit = 2 * 1024 * 1024 // Target maximum size of returned blocks, headers or node data.
	estHeaderRlpSize  = 500             // Approximate size of an RLP encoded block header

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	defaultDKRSRelayMaxPeers = 64
	defaultDistKGRelayTTL    = 1
	defaultDKRSRelayTTL      = 1
	defaultDKRSSeenMax       = 32768
	defaultDKRSSeenRetention = 15 * time.Minute
)

var (
	syncChallengeTimeout = 15 * time.Second // Time allowance for a node to reply to the sync progress challenge
)

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

// 毕设
type ProtocolManager struct {
	networkID  uint64
	forkFilter forkid.Filter // Fork ID filter, constant across the lifetime of the node

	pkStore *keyStore.PublicKeyStore

	fastSync  uint32 // Flag whether fast sync is enabled (gets disabled if we already have blocks)
	acceptTxs uint32 // Flag whether we're considered synchronised (enables transaction processing)

	checkpointNumber uint64      // Block number for the sync progress validator to cross reference
	checkpointHash   common.Hash // Block hash for the sync progress validator to cross reference

	txpool     txPool
	blockchain *core.BlockChain
	maxPeers   int

	// blockBroadcastPeerLimit caps the full-block recipient subset used when
	// propagating a newly mined block; 0 keeps the legacy sqrt(peer count).
	blockBroadcastPeerLimit int

	downloader   *downloader.Downloader
	blockFetcher *fetcher.BlockFetcher
	txFetcher    *fetcher.TxFetcher
	peers        *peerSet

	eventMux      *event.TypeMux
	txsCh         chan core.NewTxsEvent
	txsSub        event.Subscription
	minedBlockSub *event.TypeMuxSubscription

	whitelist map[uint64]common.Hash

	// channels for fetcher, syncer, txsyncLoop
	newPeerCh   chan *peer
	txsyncCh    chan *txsync
	quitSync    chan struct{}
	noMorePeers chan struct{}

	// wait group is used for graceful shutdowns during downloading
	// and processing
	wg sync.WaitGroup

	// appliedRedactions tracks successfully applied TxModifyMsg instances.
	// Incoming full messages are cached separately so a failed/early apply can
	// be retried instead of being permanently swallowed.
	appliedRedactions  map[[32]byte]struct{}
	pendingRedactions  map[[32]byte]struct{}
	appliedRedLock     sync.Mutex
	txModifyMu         sync.Mutex
	txModifySeen       map[common.Hash]time.Time
	txModifyCache      map[[32]byte]txModifyCacheEntry
	txModifyRequests   map[common.Hash]time.Time
	txModifyRelayTTL   int
	teePeerMu          sync.RWMutex
	teePeers           map[[32]byte]string
	distKGRelayTTL     int
	distKGSeenMu       sync.Mutex
	distKGSeenMessages map[common.Hash]time.Time
	dkrsRelayEnabled   bool
	dkrsRelayMaxPeers  int
	dkrsRelayOrdinary  bool
	dkrsRelayTTL       int
	dkrsSeenMu         sync.Mutex
	dkrsSeenMessages   map[common.Hash]time.Time
	dkrsSeenMax        int
	dkrsSeenRetention  time.Duration

	// Test fields or hooks
	broadcastTxAnnouncesOnly bool // Testing field, disable transaction propagation
	nodeRole                 string
	timingSink               func(stage string, event string, fields ...interface{})
	waitRedactionVisible     func(reqID [32]byte, timeout time.Duration) error
	teeHost                  interface {
		HandleDistKG(context.Context, teehost.DistKGProtocolMessage, teehost.ProtocolNetwork) error
		HandleDKRS(context.Context, teehost.DKRSProtocolMessage, teehost.ProtocolNetwork) error
		HandlePartialAdapt(context.Context, teehost.PartialAdaptProtocolMessage, teehost.ProtocolNetwork) error
		HandleRedactMaterial(context.Context, teehost.RedactionMaterial, teehost.ProtocolNetwork) error
		HandleRedactAuthorizedMessage(context.Context, teehost.RedactAuthorizedMessage, teehost.ProtocolNetwork) error
		HardwareID() [32]byte
	}

	// 添加缺失的字段
	p2pServer *p2p.Server

	port string
}

func (pm *ProtocolManager) SetTEEHost(host interface {
	HandleDistKG(context.Context, teehost.DistKGProtocolMessage, teehost.ProtocolNetwork) error
	HandleDKRS(context.Context, teehost.DKRSProtocolMessage, teehost.ProtocolNetwork) error
	HandlePartialAdapt(context.Context, teehost.PartialAdaptProtocolMessage, teehost.ProtocolNetwork) error
	HandleRedactMaterial(context.Context, teehost.RedactionMaterial, teehost.ProtocolNetwork) error
	HandleRedactAuthorizedMessage(context.Context, teehost.RedactAuthorizedMessage, teehost.ProtocolNetwork) error
	HardwareID() [32]byte
}) {
	pm.teeHost = host
	pm.announceTEEIdentityToPeers()
}

func (pm *ProtocolManager) ConfigureDKRSRelay(enabled bool, maxPeers int, ordinary bool, ttl int) {
	pm.dkrsRelayEnabled = enabled
	if maxPeers <= 0 {
		maxPeers = defaultDKRSRelayMaxPeers
	}
	pm.dkrsRelayMaxPeers = maxPeers
	pm.dkrsRelayOrdinary = ordinary
	if ttl < 0 {
		ttl = 0
	}
	pm.dkrsRelayTTL = ttl
}

func (pm *ProtocolManager) ConfigureDistKGRelay(ttl int) {
	if ttl < 0 {
		ttl = 0
	}
	pm.distKGRelayTTL = ttl
}

func (pm *ProtocolManager) ConfigureTxModifyRelay(ttl int) {
	if ttl < 0 {
		ttl = 0
	}
	pm.txModifyRelayTTL = ttl
}

func (pm *ProtocolManager) localTEEIdentity() (teeIdentityMessage, bool) {
	if pm == nil || pm.teeHost == nil {
		return teeIdentityMessage{}, false
	}
	key := pm.teeHost.HardwareID()
	if key == ([32]byte{}) {
		return teeIdentityMessage{}, false
	}
	return teeIdentityMessage{SigPubKey: key}, true
}

func (pm *ProtocolManager) announceTEEIdentityToPeers() {
	if pm == nil || pm.peers == nil {
		return
	}
	peers := pm.snapshotPeers()
	for _, peer := range peers {
		pm.announceTEEIdentityToPeer(peer)
	}
}

func (pm *ProtocolManager) announceTEEIdentityToPeer(peer *peer) {
	if peer == nil {
		return
	}
	identity, ok := pm.localTEEIdentity()
	if !ok {
		return
	}
	go func() {
		if err := peer.SendTEEIdentity(identity); err != nil {
			log.Warn("Failed to announce TEE identity", "peer", peer.id, "err", err)
		}
	}()
}

func (pm *ProtocolManager) snapshotPeers() []*peer {
	pm.peers.lock.RLock()
	defer pm.peers.lock.RUnlock()
	peers := make([]*peer, 0, len(pm.peers.peers))
	for _, peer := range pm.peers.peers {
		peers = append(peers, peer)
	}
	return peers
}

func (pm *ProtocolManager) registerTEEPeer(peerID string, sigPubKey [32]byte) bool {
	if sigPubKey == ([32]byte{}) {
		return false
	}
	pm.teePeerMu.Lock()
	defer pm.teePeerMu.Unlock()
	if pm.teePeers == nil {
		pm.teePeers = make(map[[32]byte]string)
	}
	_, existed := pm.teePeers[sigPubKey]
	pm.teePeers[sigPubKey] = peerID
	return !existed
}

func (pm *ProtocolManager) unregisterTEEPeer(peerID string) {
	pm.teePeerMu.Lock()
	defer pm.teePeerMu.Unlock()
	for sigPubKey, mappedPeerID := range pm.teePeers {
		if mappedPeerID == peerID {
			delete(pm.teePeers, sigPubKey)
		}
	}
}

func (pm *ProtocolManager) peerForTEESigPubKey(sigPubKey [32]byte) *peer {
	pm.teePeerMu.RLock()
	peerID := pm.teePeers[sigPubKey]
	pm.teePeerMu.RUnlock()
	if peerID == "" {
		return nil
	}
	return pm.peers.Peer(peerID)
}

func distKGMessageFingerprint(msg teehost.DistKGProtocolMessage) (common.Hash, error) {
	msg.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	payload := append([]byte("distkg:"), encoded...)
	return crypto.Keccak256Hash(payload), nil
}

func (pm *ProtocolManager) defaultDistKGRelayTTL() uint64 {
	ttl := pm.distKGRelayTTL
	if ttl < 0 {
		return 0
	}
	return uint64(ttl)
}

func (pm *ProtocolManager) prepareDistKGOutbound(msg teehost.DistKGProtocolMessage) teehost.DistKGProtocolMessage {
	if msg.RelayTTL == 0 {
		msg.RelayTTL = pm.defaultDistKGRelayTTL()
	}
	return msg
}

func (pm *ProtocolManager) markDistKGSeen(fingerprint common.Hash) bool {
	now := time.Now()
	pm.distKGSeenMu.Lock()
	defer pm.distKGSeenMu.Unlock()
	if pm.distKGSeenMessages == nil {
		pm.distKGSeenMessages = make(map[common.Hash]time.Time)
	}
	if seenAt, ok := pm.distKGSeenMessages[fingerprint]; ok && now.Sub(seenAt) < defaultDKRSSeenRetention {
		return false
	}
	pm.distKGSeenMessages[fingerprint] = now
	pruneRelaySeenLocked(pm.distKGSeenMessages, now, defaultDKRSSeenRetention, defaultDKRSSeenMax)
	return true
}

func (pm *ProtocolManager) rememberDistKGMessage(msg teehost.DistKGProtocolMessage) {
	fingerprint, err := distKGMessageFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint TEE DistKG message", "err", err)
		return
	}
	pm.markDistKGSeen(fingerprint)
}

func dkrsMessageFingerprint(msg teehost.DKRSProtocolMessage) (common.Hash, error) {
	msg.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(encoded), nil
}

func (pm *ProtocolManager) defaultDKRSRelayTTL() uint64 {
	ttl := pm.dkrsRelayTTL
	if ttl < 0 {
		return 0
	}
	return uint64(ttl)
}

func (pm *ProtocolManager) prepareDKRSOutbound(msg teehost.DKRSProtocolMessage) teehost.DKRSProtocolMessage {
	if msg.RelayTTL == 0 {
		msg.RelayTTL = pm.defaultDKRSRelayTTL()
	}
	return msg
}

func (pm *ProtocolManager) rememberDKRSMessage(msg teehost.DKRSProtocolMessage) {
	fingerprint, err := dkrsMessageFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint TEE DKRS message", "err", err)
		return
	}
	pm.markDKRSSeen(fingerprint)
}

func partialAdaptMessageFingerprint(msg teehost.PartialAdaptProtocolMessage) (common.Hash, error) {
	msg.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(append([]byte("partial-adapt:"), encoded...)), nil
}

func (pm *ProtocolManager) preparePartialAdaptOutbound(msg teehost.PartialAdaptProtocolMessage) teehost.PartialAdaptProtocolMessage {
	if msg.RelayTTL == 0 {
		msg.RelayTTL = pm.defaultDKRSRelayTTL()
	}
	return msg
}

func (pm *ProtocolManager) rememberPartialAdaptMessage(msg teehost.PartialAdaptProtocolMessage) {
	fingerprint, err := partialAdaptMessageFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint TEE partial adapt message", "err", err)
		return
	}
	pm.markDKRSSeen(fingerprint)
}

func redactMaterialFingerprint(material teehost.RedactionMaterial) (common.Hash, error) {
	material.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(material)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(append([]byte("redact-material:"), encoded...)), nil
}

func (pm *ProtocolManager) prepareRedactMaterialOutbound(material teehost.RedactionMaterial) teehost.RedactionMaterial {
	if material.RelayTTL == 0 {
		material.RelayTTL = pm.defaultDKRSRelayTTL()
	}
	return material
}

func (pm *ProtocolManager) rememberRedactMaterial(material teehost.RedactionMaterial) {
	fingerprint, err := redactMaterialFingerprint(material)
	if err != nil {
		log.Warn("Failed to fingerprint TEE redact material", "err", err)
		return
	}
	pm.markDKRSSeen(fingerprint)
}

func redactAuthorizedFingerprint(msg teehost.RedactAuthorizedMessage) (common.Hash, error) {
	msg.RelayTTL = 0
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(append([]byte("redact-authorized:"), encoded...)), nil
}

func (pm *ProtocolManager) prepareRedactAuthorizedOutbound(msg teehost.RedactAuthorizedMessage) teehost.RedactAuthorizedMessage {
	if msg.RelayTTL == 0 {
		msg.RelayTTL = pm.defaultDKRSRelayTTL()
	}
	return msg
}

func (pm *ProtocolManager) rememberRedactAuthorized(msg teehost.RedactAuthorizedMessage) {
	fingerprint, err := redactAuthorizedFingerprint(msg)
	if err != nil {
		log.Warn("Failed to fingerprint TEE redact authorized message", "err", err)
		return
	}
	pm.markDKRSSeen(fingerprint)
}

func (pm *ProtocolManager) markDKRSSeen(fingerprint common.Hash) bool {
	now := time.Now()
	pm.dkrsSeenMu.Lock()
	defer pm.dkrsSeenMu.Unlock()
	if pm.dkrsSeenMessages == nil {
		pm.dkrsSeenMessages = make(map[common.Hash]time.Time)
	}
	retention := pm.dkrsSeenRetention
	if retention <= 0 {
		retention = defaultDKRSSeenRetention
	}
	if seenAt, ok := pm.dkrsSeenMessages[fingerprint]; ok && now.Sub(seenAt) < retention {
		return false
	}
	pm.dkrsSeenMessages[fingerprint] = now
	pm.pruneDKRSSeenLocked(now, retention)
	return true
}

func (pm *ProtocolManager) pruneDKRSSeenLocked(now time.Time, retention time.Duration) {
	maxSeen := pm.dkrsSeenMax
	if maxSeen <= 0 {
		maxSeen = defaultDKRSSeenMax
	}
	pruneRelaySeenLocked(pm.dkrsSeenMessages, now, retention, maxSeen)
}

func pruneRelaySeenLocked(entries map[common.Hash]time.Time, now time.Time, retention time.Duration, maxSeen int) {
	for fingerprint, seenAt := range entries {
		if now.Sub(seenAt) >= retention {
			delete(entries, fingerprint)
		}
	}
	for len(entries) > maxSeen {
		var oldest common.Hash
		var oldestAt time.Time
		for fingerprint, seenAt := range entries {
			if oldestAt.IsZero() || seenAt.Before(oldestAt) {
				oldest = fingerprint
				oldestAt = seenAt
			}
		}
		delete(entries, oldest)
	}
}

// NewProtocolManager returns a new Ethereum sub protocol manager. The Ethereum sub protocol manages peers capable
// with the Ethereum network.
func NewProtocolManager(config *params.ChainConfig, checkpoint *params.TrustedCheckpoint, mode downloader.SyncMode, networkID uint64, mux *event.TypeMux, txpool txPool, engine consensus.Engine, blockchain *core.BlockChain, chaindb ethdb.Database, cacheLimit int, whitelist map[uint64]common.Hash, port string) (*ProtocolManager, error) {
	// Create the protocol manager with the base fields
	manager := &ProtocolManager{
		networkID:          networkID,
		forkFilter:         forkid.NewFilter(blockchain),
		eventMux:           mux,
		txpool:             txpool,
		blockchain:         blockchain,
		peers:              newPeerSet(),
		whitelist:          whitelist,
		newPeerCh:          make(chan *peer),
		noMorePeers:        make(chan struct{}),
		txsyncCh:           make(chan *txsync),
		quitSync:           make(chan struct{}),
		pkStore:            keyStore.NewPublicKeyStore(),
		port:               port,
		nodeRole:           strings.ToLower(strings.TrimSpace(os.Getenv("E2E_TIMING_NODE_ROLE"))),
		appliedRedactions:  make(map[[32]byte]struct{}),
		pendingRedactions:  make(map[[32]byte]struct{}),
		txModifySeen:       make(map[common.Hash]time.Time),
		txModifyCache:      make(map[[32]byte]txModifyCacheEntry),
		txModifyRequests:   make(map[common.Hash]time.Time),
		txModifyRelayTTL:   defaultTxModifyRelayTTL,
		teePeers:           make(map[[32]byte]string),
		distKGRelayTTL:     defaultDistKGRelayTTL,
		distKGSeenMessages: make(map[common.Hash]time.Time),
		dkrsRelayEnabled:   true,
		dkrsRelayMaxPeers:  defaultDKRSRelayMaxPeers,
		dkrsRelayTTL:       defaultDKRSRelayTTL,
		dkrsSeenMessages:   make(map[common.Hash]time.Time),
		dkrsSeenMax:        defaultDKRSSeenMax,
		dkrsSeenRetention:  defaultDKRSSeenRetention,
	}
	if mode == downloader.FullSync {
		// The database seems empty as the current block is the genesis. Yet the fast
		// block is ahead, so fast sync was enabled for this node at a certain point.
		// The scenarios where this can happen is
		// * if the user manually (or via a bad block) rolled back a fast sync node
		//   below the sync point.
		// * the last fast sync is not finished while user specifies a full sync this
		//   time. But we don't have any recent state for full sync.
		// In these cases however it's safe to reenable fast sync.
		fullBlock, fastBlock := blockchain.CurrentBlock(), blockchain.CurrentFastBlock()
		if fullBlock.NumberU64() == 0 && fastBlock.NumberU64() > 0 {
			manager.fastSync = uint32(1)
			log.Warn("Switch sync mode from full sync to fast sync")
		}
	} else {
		if blockchain.CurrentBlock().NumberU64() > 0 {
			// Print warning log if database is not empty to run fast sync.
			log.Warn("Switch sync mode from fast sync to full sync")
		} else {
			// If fast sync was requested and our database is empty, grant it
			manager.fastSync = uint32(1)
		}
	}
	// If we have trusted checkpoints, enforce them on the chain
	if checkpoint != nil {
		manager.checkpointNumber = (checkpoint.SectionIndex+1)*params.CHTFrequency - 1
		manager.checkpointHash = checkpoint.SectionHead
	}

	// Construct the downloader (long sync) and its backing state bloom if fast
	// sync is requested. The downloader is responsible for deallocating the state
	// bloom when it's done.
	var stateBloom *trie.SyncBloom
	if atomic.LoadUint32(&manager.fastSync) == 1 {
		stateBloom = trie.NewSyncBloom(uint64(cacheLimit), chaindb)
	}
	manager.downloader = downloader.New(manager.checkpointNumber, chaindb, stateBloom, manager.eventMux, blockchain, nil, manager.removePeer)

	// Construct the fetcher (short sync)
	validator := func(header *types.Header) error {
		return engine.VerifyHeader(blockchain, header, true)
	}
	heighter := func() uint64 {
		return blockchain.CurrentBlock().NumberU64()
	}
	inserter := func(blocks types.Blocks) (int, error) {
		// If sync hasn't reached the checkpoint yet, deny importing weird blocks.
		//
		// Ideally we would also compare the head block's timestamp and similarly reject
		// the propagated block if the head is too old. Unfortunately there is a corner
		// case when starting new networks, where the genesis might be ancient (0 unix)
		// which would prevent full nodes from accepting it.
		if manager.blockchain.CurrentBlock().NumberU64() < manager.checkpointNumber {
			log.Warn("Unsynced yet, discarded propagated block", "number", blocks[0].Number(), "hash", blocks[0].Hash())
			return 0, nil
		}
		// If fast sync is running, deny importing weird blocks. This is a problematic
		// clause when starting up a new network, because fast-syncing miners might not
		// accept each others' blocks until a restart. Unfortunately we haven't figured
		// out a way yet where nodes can decide unilaterally whether the network is new
		// or not. This should be fixed if we figure out a solution.
		if atomic.LoadUint32(&manager.fastSync) == 1 {
			log.Warn("Fast syncing, discarded propagated block", "number", blocks[0].Number(), "hash", blocks[0].Hash())
			return 0, nil
		}
		n, err := manager.blockchain.InsertChain(blocks)
		if err == nil {
			atomic.StoreUint32(&manager.acceptTxs, 1) // Mark initial sync done on any fetcher import
		}
		return n, err
	}
	manager.blockFetcher = fetcher.NewBlockFetcher(blockchain.GetBlockByHash, validator, manager.BroadcastBlock, heighter, inserter, manager.removePeer)

	fetchTx := func(peer string, hashes []common.Hash) error {
		p := manager.peers.Peer(peer)
		if p == nil {
			return errors.New("unknown peer")
		}
		return p.RequestTxs(hashes)
	}
	manager.txFetcher = fetcher.NewTxFetcher(txpool.Has, txpool.AddRemotes, fetchTx)

	return manager, nil
}

func (pm *ProtocolManager) makeProtocol(version uint) p2p.Protocol {
	length, ok := protocolLengths[version]
	if !ok {
		panic("makeProtocol for unknown version")
	}

	return p2p.Protocol{
		Name:    protocolName,
		Version: version,
		Length:  length,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			peer := pm.newPeer(int(version), p, rw, pm.txpool.Get)
			select {
			case pm.newPeerCh <- peer:
				pm.wg.Add(1)
				defer pm.wg.Done()
				return pm.handle(peer)
			case <-pm.quitSync:
				return p2p.DiscQuitting
			}
		},
		NodeInfo: func() interface{} {
			return pm.NodeInfo()
		},
		PeerInfo: func(id enode.ID) interface{} {
			if p := pm.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
				return p.Info()
			}
			return nil
		},
	}
}

func (pm *ProtocolManager) removePeer(id string) {
	// Short circuit if the peer was already removed
	peer := pm.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Debug("Removing Ethereum peer", "peer", id)
	pm.unregisterTEEPeer(id)

	// Unregister the peer from the downloader and Ethereum peer set
	pm.downloader.UnregisterPeer(id)
	pm.txFetcher.Drop(id)

	if err := pm.peers.Unregister(id); err != nil {
		log.Error("Peer removal failed", "peer", id, "err", err)
	}
	// Hard disconnect at the networking layer
	if peer != nil {
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
}

func (pm *ProtocolManager) Start(maxPeers int) {
	pm.maxPeers = maxPeers

	// broadcast transactions
	pm.txsCh = make(chan core.NewTxsEvent, txChanSize)
	pm.txsSub = pm.txpool.SubscribeNewTxsEvent(pm.txsCh)
	go pm.txBroadcastLoop()

	// broadcast mined blocks
	// 毕设
	pm.minedBlockSub = pm.eventMux.Subscribe(core.NewMinedBlockEvent{}, core.NewPublicKeyEvent{})
	go pm.minedBroadcastLoop()

	// start sync handlers
	go pm.syncer()
	go pm.txsyncLoop64() // TODO(karalabe): Legacy initial tx echange, drop with eth/64.
}

func (pm *ProtocolManager) Stop() {
	log.Info("Stopping Ethereum protocol")

	pm.txsSub.Unsubscribe()        // quits txBroadcastLoop
	pm.minedBlockSub.Unsubscribe() // quits blockBroadcastLoop

	// Quit the sync loop.
	// After this send has completed, no new peers will be accepted.
	pm.noMorePeers <- struct{}{}

	// Quit fetcher, txsyncLoop.
	close(pm.quitSync)

	// Disconnect existing sessions.
	// This also closes the gate for any new registrations on the peer set.
	// sessions which are already established but not added to pm.peers yet
	// will exit when they try to register.
	pm.peers.Close()

	// Wait for all peer handler goroutines and the loops to come down.
	pm.wg.Wait()

	log.Info("Ethereum protocol stopped")
}

func (pm *ProtocolManager) newPeer(pv int, p *p2p.Peer, rw p2p.MsgReadWriter, getPooledTx func(hash common.Hash) *types.Transaction) *peer {
	return newPeer(pv, p, rw, getPooledTx)
}

// handle 是为管理 ETH 对等体的生命周期而调用的回调。此函数终止的时候，对等体断开连接。
func (pm *ProtocolManager) handle(p *peer) error {
	// Ignore maxPeers if this is a trusted peer
	if pm.peers.Len() >= pm.maxPeers && !p.Peer.Info().Network.Trusted {
		return p2p.DiscTooManyPeers
	}
	p.Log().Debug("Ethereum peer connected", "name", p.Name())

	// Execute the Ethereum handshake
	var (
		genesis = pm.blockchain.Genesis()
		head    = pm.blockchain.CurrentHeader()
		hash    = head.Hash()
		number  = head.Number.Uint64()
		td      = pm.blockchain.GetTd(hash, number)
	)
	if err := p.Handshake(pm.networkID, td, hash, genesis.Hash(), forkid.NewID(pm.blockchain), pm.forkFilter); err != nil {
		p.Log().Debug("Ethereum handshake failed", "err", err)
		return err
	}
	// Register the peer locally
	if err := pm.peers.Register(p); err != nil {
		p.Log().Error("Ethereum peer registration failed", "err", err)
		return err
	}
	defer pm.removePeer(p.id)
	pm.announceTEEIdentityToPeer(p)

	// Register the peer in the downloader. If the downloader considers it banned, we disconnect
	if err := pm.downloader.RegisterPeer(p.id, p.version, p); err != nil {
		return err
	}
	// Propagate existing transactions. new transactions appearing
	// after this will be sent via broadcasts.
	pm.syncTransactions(p)

	// If we have a trusted CHT, reject all peers below that (avoid fast sync eclipse)
	if pm.checkpointHash != (common.Hash{}) {
		// Request the peer's checkpoint header for chain height/weight validation
		if err := p.RequestHeadersByNumber(pm.checkpointNumber, 1, 0, false); err != nil {
			return err
		}
		// Start a timer to disconnect if the peer doesn't reply in time
		p.syncDrop = time.AfterFunc(syncChallengeTimeout, func() {
			p.Log().Warn("Checkpoint challenge timed out, dropping", "addr", p.RemoteAddr(), "type", p.Name())
			pm.removePeer(p.id)
		})
		// Make sure it's cleaned up if the peer dies off
		defer func() {
			if p.syncDrop != nil {
				p.syncDrop.Stop()
				p.syncDrop = nil
			}
		}()
	}
	// If we have any explicit whitelist block hashes, request them
	for number := range pm.whitelist {
		if err := p.RequestHeadersByNumber(number, 1, 0, false); err != nil {
			return err
		}
	}
	// Handle incoming messages until the connection is torn down
	for {
		if err := pm.handleMsg(p); err != nil {
			p.Log().Debug("Ethereum message handling failed", "err", err)
			return err
		}
	}
}
func (pm *ProtocolManager) GetBlockBigInt(block *types.Block) *big.Int {
	BlockHashInput := crypto.Keccak256(
		block.Header().ParentHash.Bytes(),
		block.Header().Number.Bytes(),
		block.Header().Root.Bytes(),
		block.Header().TxHash.Bytes(),
		block.Header().ReceiptHash.Bytes(),
	)
	BlockHashBigInt := new(big.Int).SetBytes(BlockHashInput)
	return BlockHashBigInt
}

// handleMsg 每当从远程接收到入站消息时调用。返回任何错误时，远程连接将断开。
func (pm *ProtocolManager) handleMsg(p *peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > protocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	defer msg.Discard()

	// Handle the message depending on its contents
	switch {
	// 公钥消息
	case msg.Code == PublicKeyMsg:
		return nil
	// 私钥消息
	case msg.Code == PrivateKeyMsg:
		return nil

	case msg.Code == ModifyVerifyMsg:
		log.Warn("Ignoring legacy ModifyVerifyMsg: single-key redaction flow is unsupported")
		return nil

	case msg.Code == TEEDistKGMsg:
		var distKGMsg teehost.DistKGProtocolMessage
		if err := msg.Decode(&distKGMsg); err != nil {
			log.Error("decode TEEDistKGMsg", "err", err)
			return err
		}
		fingerprint, err := distKGMessageFingerprint(distKGMsg)
		if err != nil {
			log.Warn("fingerprint TEEDistKGMsg failed", "err", err)
			return nil
		}
		if !pm.markDistKGSeen(fingerprint) {
			return nil
		}
		if pm.teeHost == nil {
			log.Warn("receive TEEDistKGMsg without TEE host")
			return nil
		}
		if err := pm.teeHost.HandleDistKG(context.Background(), distKGMsg, pm); err != nil {
			log.Warn("handle TEEDistKGMsg failed", "err", err)
			return nil
		}
		if distKGMsg.RelayTTL > 0 {
			relayMsg := distKGMsg
			relayMsg.RelayTTL--
			success, failed := pm.broadcastTEEDistKGExcept(relayMsg, p.id, pm.dkrsRelayMaxPeers)
			if failed > 0 {
				log.Warn("TEE DistKG relay had failed sends", "peer", p.id, "fingerprint", fingerprint, "success", success, "failed", failed)
			}
		}
		return nil

	case msg.Code == TEEDKRSMsg:
		var dkrsMsg teehost.DKRSProtocolMessage
		if err := msg.Decode(&dkrsMsg); err != nil {
			log.Error("decode TEEDKRSMsg", "err", err)
			return err
		}
		fingerprint, err := dkrsMessageFingerprint(dkrsMsg)
		if err != nil {
			log.Warn("fingerprint TEEDKRSMsg failed", "err", err)
			return nil
		}
		firstSeen := pm.markDKRSSeen(fingerprint)
		if !firstSeen {
			return nil
		}
		if pm.teeHost == nil {
			if !pm.dkrsRelayOrdinary {
				log.Warn("receive TEEDKRSMsg without TEE host")
				return nil
			}
		} else {
			if err := pm.teeHost.HandleDKRS(context.Background(), dkrsMsg, pm); err != nil {
				log.Warn("handle TEEDKRSMsg failed", "err", err)
				return nil
			}
		}
		if pm.dkrsRelayEnabled && (pm.teeHost != nil || pm.dkrsRelayOrdinary) {
			if dkrsMsg.RelayTTL == 0 {
				return nil
			}
			relayMsg := dkrsMsg
			relayMsg.RelayTTL--
			success, failed := pm.broadcastTEEDKRSExcept(relayMsg, p.id, pm.dkrsRelayMaxPeers)
			if failed > 0 {
				log.Warn("TEE DKRS relay had failed sends", "peer", p.id, "fingerprint", fingerprint, "success", success, "failed", failed)
			}
		}
		return nil

	case msg.Code == TEEIdentityMsg:
		var identity teeIdentityMessage
		if err := msg.Decode(&identity); err != nil {
			log.Error("decode TEEIdentityMsg", "err", err)
			return err
		}
		if pm.registerTEEPeer(p.id, identity.SigPubKey) {
			log.Debug("Registered TEE peer identity", "peer", p.id, "sigPubKey", fmt.Sprintf("%x", identity.SigPubKey))
			pm.announceTEEIdentityToPeer(p)
		}
		return nil

	case msg.Code == TEEPartialAdaptMsg:
		var partialMsg teehost.PartialAdaptProtocolMessage
		if err := msg.Decode(&partialMsg); err != nil {
			log.Error("decode TEEPartialAdaptMsg", "err", err)
			return err
		}
		// Legacy aggregate packets remain decodable but are neither consumed nor relayed.
		if partialMsg.Contribution == nil || partialMsg.Aggregate != nil {
			return nil
		}
		fingerprint, err := partialAdaptMessageFingerprint(partialMsg)
		if err != nil {
			log.Warn("fingerprint TEEPartialAdaptMsg failed", "err", err)
			return nil
		}
		if !pm.markDKRSSeen(fingerprint) {
			return nil
		}
		// As with TEERedactAuthorizedMsg, a local handling error must not stop
		// the relay: shares only need to reach `threshold` hosts, but the hosts
		// that already have a session depend on their peers forwarding
		// contributions they could not apply themselves.
		if pm.teeHost == nil {
			if !pm.dkrsRelayOrdinary {
				log.Debug("receive TEEPartialAdaptMsg without TEE host", "peer", p.id)
			}
		} else if err := pm.teeHost.HandlePartialAdapt(context.Background(), partialMsg, pm); err != nil {
			log.Warn("handle TEEPartialAdaptMsg failed", "err", err)
		}
		if pm.dkrsRelayEnabled && (pm.teeHost != nil || pm.dkrsRelayOrdinary) && partialMsg.RelayTTL > 0 {
			relayMsg := partialMsg
			relayMsg.RelayTTL--
			success, failed := pm.broadcastTEEPartialAdaptExcept(relayMsg, p.id, pm.dkrsRelayMaxPeers)
			if failed > 0 {
				log.Warn("TEE partial adapt relay had failed sends", "peer", p.id, "fingerprint", fingerprint, "success", success, "failed", failed)
			}
		}
		return nil

	case msg.Code == TEERedactMaterialMsg:
		var material teehost.RedactionMaterial
		if err := msg.Decode(&material); err != nil {
			log.Error("decode TEERedactMaterialMsg", "err", err)
			return err
		}
		fingerprint, err := redactMaterialFingerprint(material)
		if err != nil {
			log.Warn("fingerprint TEERedactMaterialMsg failed", "err", err)
			return nil
		}
		if !pm.markDKRSSeen(fingerprint) {
			return nil
		}
		if pm.teeHost == nil {
			if !pm.dkrsRelayOrdinary {
				log.Warn("receive TEERedactMaterialMsg without TEE host")
				return nil
			}
		} else {
			if err := pm.teeHost.HandleRedactMaterial(context.Background(), material, pm); err != nil {
				log.Warn("handle TEERedactMaterialMsg failed", "err", err)
				return nil
			}
		}
		if pm.dkrsRelayEnabled && (pm.teeHost != nil || pm.dkrsRelayOrdinary) && material.RelayTTL > 0 {
			relayMaterial := material
			relayMaterial.RelayTTL--
			success, failed := pm.broadcastTEERedactMaterialExcept(relayMaterial, p.id, pm.dkrsRelayMaxPeers)
			if failed > 0 {
				log.Warn("TEE redact material relay had failed sends", "peer", p.id, "fingerprint", fingerprint, "success", success, "failed", failed)
			}
		}
		return nil

	case msg.Code == TEERedactAuthorizedMsg:
		var authMsg teehost.RedactAuthorizedMessage
		if err := msg.Decode(&authMsg); err != nil {
			log.Error("decode TEERedactAuthorizedMsg", "err", err)
			return err
		}
		fingerprint, err := redactAuthorizedFingerprint(authMsg)
		if err != nil {
			log.Warn("fingerprint TEERedactAuthorizedMsg failed", "err", err)
			return nil
		}
		if !pm.markDKRSSeen(fingerprint) {
			return nil
		}
		// Local delivery is best effort and MUST NOT gate the relay. Only the
		// TEE hosts bridged directly onto the sealer (GATEWAY_BRIDGE_EDGES) are
		// woken by the committee in the first hop; every other TEE host is
		// reached exclusively by relaying across the TEE overlay. Aborting the
		// relay here after a local handler error (e.g. a partial-adapt that is
		// not ready yet) strands the remaining hosts until the authorization
		// transaction is mined and the contract event wakes them ~5s later.
		if pm.teeHost == nil {
			if !pm.dkrsRelayOrdinary {
				log.Debug("receive TEERedactAuthorizedMsg without TEE host", "peer", p.id)
			}
		} else if err := pm.teeHost.HandleRedactAuthorizedMessage(context.Background(), authMsg, pm); err != nil {
			log.Warn("handle TEERedactAuthorizedMsg failed", "err", err)
		}
		// Unlike the TEE-internal messages above, this one is a committee-to-network
		// broadcast injected at a single gateway node, so every node relays it
		// (like TxModifyMsg) rather than only TEE hosts. Restricting the relay to
		// TEE hosts leaves it one bridging hop short of the overlay and starves
		// the hosts that were not direct first-hop recipients.
		if pm.dkrsRelayEnabled && authMsg.RelayTTL > 0 {
			relayMsg := authMsg
			relayMsg.RelayTTL--
			success, failed := pm.broadcastTEERedactAuthorizedExcept(relayMsg, p.id, pm.dkrsRelayMaxPeers)
			if failed > 0 {
				log.Warn("TEE redact authorized relay had failed sends", "peer", p.id, "fingerprint", fingerprint, "success", success, "failed", failed)
			}
		}
		return nil

	case msg.Code == TxModifyMsg:
		var modifyMsg types.ModifyMsg
		if err := msg.Decode(&modifyMsg); err != nil {
			log.Error("decode TxModifyMsg", "err", err)
			return err
		}
		pm.handleTxModifyGossip(modifyMsg, p)
		return nil

	case msg.Code == TxModifyRequestMsg:
		var req types.TxModifyRequest
		if err := msg.Decode(&req); err != nil {
			log.Error("decode TxModifyRequestMsg", "err", err)
			return err
		}
		pm.handleTxModifyRequest(req, p)
		return nil

	case msg.Code == StatusMsg:
		// Status messages should never arrive after the handshake
		return errResp(ErrExtraStatusMsg, "uncontrolled status message")
	// Block header query, collect the requested headers and reply
	case msg.Code == GetBlockHeadersMsg:
		// Decode the complex header query
		var query getBlockHeadersData
		if err := msg.Decode(&query); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		hashMode := query.Origin.Hash != (common.Hash{})
		first := true
		maxNonCanonical := uint64(100)

		// Gather headers until the fetch or network limits is reached
		var (
			bytes   common.StorageSize
			headers []*types.Header
			unknown bool
		)
		for !unknown && len(headers) < int(query.Amount) && bytes < softResponseLimit && len(headers) < downloader.MaxHeaderFetch {
			// Retrieve the next header satisfying the query
			var origin *types.Header
			if hashMode {
				if first {
					first = false
					origin = pm.blockchain.GetHeaderByHash(query.Origin.Hash)
					if origin != nil {
						query.Origin.Number = origin.Number.Uint64()
					}
				} else {
					origin = pm.blockchain.GetHeader(query.Origin.Hash, query.Origin.Number)
				}
			} else {
				origin = pm.blockchain.GetHeaderByNumber(query.Origin.Number)
			}
			if origin == nil {
				break
			}
			headers = append(headers, origin)
			bytes += estHeaderRlpSize

			// Advance to the next header of the query
			switch {
			case hashMode && query.Reverse:
				// Hash based traversal towards the genesis block
				ancestor := query.Skip + 1
				if ancestor == 0 {
					unknown = true
				} else {
					query.Origin.Hash, query.Origin.Number = pm.blockchain.GetAncestor(query.Origin.Hash, query.Origin.Number, ancestor, &maxNonCanonical)
					unknown = (query.Origin.Hash == common.Hash{})
				}
			case hashMode && !query.Reverse:
				// Hash based traversal towards the leaf block
				var (
					current = origin.Number.Uint64()
					next    = current + query.Skip + 1
				)
				if next <= current {
					infos, _ := json.MarshalIndent(p.Peer.Info(), "", "  ")
					p.Log().Warn("GetBlockHeaders skip overflow attack", "current", current, "skip", query.Skip, "next", next, "attacker", infos)
					unknown = true
				} else {
					if header := pm.blockchain.GetHeaderByNumber(next); header != nil {
						nextHash := header.Hash()
						expOldHash, _ := pm.blockchain.GetAncestor(nextHash, next, query.Skip+1, &maxNonCanonical)
						if expOldHash == query.Origin.Hash {
							query.Origin.Hash, query.Origin.Number = nextHash, next
						} else {
							unknown = true
						}
					} else {
						unknown = true
					}
				}
			case query.Reverse:
				// Number based traversal towards the genesis block
				if query.Origin.Number >= query.Skip+1 {
					query.Origin.Number -= query.Skip + 1
				} else {
					unknown = true
				}

			case !query.Reverse:
				// Number based traversal towards the leaf block
				query.Origin.Number += query.Skip + 1
			}
		}
		return p.SendBlockHeaders(headers)

	case msg.Code == BlockHeadersMsg:
		// A batch of headers arrived to one of our previous requests
		var headers []*types.Header
		if err := msg.Decode(&headers); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// If no headers were received, but we're expencting a checkpoint header, consider it that
		if len(headers) == 0 && p.syncDrop != nil {
			// Stop the timer either way, decide later to drop or not
			p.syncDrop.Stop()
			p.syncDrop = nil

			// If we're doing a fast sync, we must enforce the checkpoint block to avoid
			// eclipse attacks. Unsynced nodes are welcome to connect after we're done
			// joining the network
			if atomic.LoadUint32(&pm.fastSync) == 1 {
				p.Log().Warn("Dropping unsynced node during fast sync", "addr", p.RemoteAddr(), "type", p.Name())
				return errors.New("unsynced node cannot serve fast sync")
			}
		}
		// Filter out any explicitly requested headers, deliver the rest to the downloader
		filter := len(headers) == 1
		if filter {
			// If it's a potential sync progress check, validate the content and advertised chain weight
			if p.syncDrop != nil && headers[0].Number.Uint64() == pm.checkpointNumber {
				// Disable the sync drop timer
				p.syncDrop.Stop()
				p.syncDrop = nil

				// Validate the header and either drop the peer or continue
				if headers[0].Hash() != pm.checkpointHash {
					return errors.New("checkpoint hash mismatch")
				}
				return nil
			}
			// Otherwise if it's a whitelisted block, validate against the set
			if want, ok := pm.whitelist[headers[0].Number.Uint64()]; ok {
				if hash := headers[0].Hash(); want != hash {
					p.Log().Info("Whitelist mismatch, dropping peer", "number", headers[0].Number.Uint64(), "hash", hash, "want", want)
					return errors.New("whitelist block mismatch")
				}
				p.Log().Debug("Whitelist block verified", "number", headers[0].Number.Uint64(), "hash", want)
			}
			// Irrelevant of the fork checks, send the header to the fetcher just in case
			headers = pm.blockFetcher.FilterHeaders(p.id, headers, time.Now())
		}
		if len(headers) > 0 || !filter {
			err := pm.downloader.DeliverHeaders(p.id, headers)
			if err != nil {
				log.Debug("Failed to deliver headers", "err", err)
			}
		}

	case msg.Code == GetBlockBodiesMsg:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// Gather blocks until the fetch or network limits is reached
		var (
			hash   common.Hash
			bytes  int
			bodies []rlp.RawValue
		)
		for bytes < softResponseLimit && len(bodies) < downloader.MaxBlockFetch {
			// Retrieve the hash of the next block
			if err := msgStream.Decode(&hash); err == rlp.EOL {
				break
			} else if err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			// Retrieve the requested block body, stopping if enough was found
			if data := pm.blockchain.GetBodyRLP(hash); len(data) != 0 {
				bodies = append(bodies, data)
				bytes += len(data)
			}
		}
		return p.SendBlockBodiesRLP(bodies)

	case msg.Code == BlockBodiesMsg:
		// A batch of block bodies arrived to one of our previous requests
		var request blockBodiesData
		if err := msg.Decode(&request); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Deliver them all to the downloader for queuing
		transactions := make([][]*types.Transaction, len(request))
		uncles := make([][]*types.Header, len(request))

		for i, body := range request {
			transactions[i] = body.Transactions
			uncles[i] = body.Uncles
		}
		// Filter out any explicitly requested bodies, deliver the rest to the downloader
		filter := len(transactions) > 0 || len(uncles) > 0
		if filter {
			transactions, uncles = pm.blockFetcher.FilterBodies(p.id, transactions, uncles, time.Now())
		}
		if len(transactions) > 0 || len(uncles) > 0 || !filter {
			err := pm.downloader.DeliverBodies(p.id, transactions, uncles)
			if err != nil {
				log.Debug("Failed to deliver bodies", "err", err)
			}
		}

	case p.version >= eth63 && msg.Code == GetNodeDataMsg:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// Gather state data until the fetch or network limits is reached
		var (
			hash  common.Hash
			bytes int
			data  [][]byte
		)
		for bytes < softResponseLimit && len(data) < downloader.MaxStateFetch {
			// Retrieve the hash of the next state entry
			if err := msgStream.Decode(&hash); err == rlp.EOL {
				break
			} else if err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			// Retrieve the requested state entry, stopping if enough was found
			if entry, err := pm.blockchain.TrieNode(hash); err == nil {
				data = append(data, entry)
				bytes += len(entry)
			}
		}
		return p.SendNodeData(data)

	case p.version >= eth63 && msg.Code == NodeDataMsg:
		// A batch of node state data arrived to one of our previous requests
		var data [][]byte
		if err := msg.Decode(&data); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Deliver all to the downloader
		if err := pm.downloader.DeliverNodeData(p.id, data); err != nil {
			log.Debug("Failed to deliver node state data", "err", err)
		}

	case p.version >= eth63 && msg.Code == GetReceiptsMsg:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// Gather state data until the fetch or network limits is reached
		var (
			hash     common.Hash
			bytes    int
			receipts []rlp.RawValue
		)
		for bytes < softResponseLimit && len(receipts) < downloader.MaxReceiptFetch {
			// Retrieve the hash of the next block
			if err := msgStream.Decode(&hash); err == rlp.EOL {
				break
			} else if err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			// Retrieve the requested block's receipts, skipping if unknown to us
			results := pm.blockchain.GetReceiptsByHash(hash)
			if results == nil {
				if header := pm.blockchain.GetHeaderByHash(hash); header == nil || header.ReceiptHash != types.EmptyRootHash {
					continue
				}
			}
			// If known, encode and queue for response packet
			if encoded, err := rlp.EncodeToBytes(results); err != nil {
				log.Error("Failed to encode receipt", "err", err)
			} else {
				receipts = append(receipts, encoded)
				bytes += len(encoded)
			}
		}
		return p.SendReceiptsRLP(receipts)

	case p.version >= eth63 && msg.Code == ReceiptsMsg:
		// A batch of receipts arrived to one of our previous requests
		var receipts [][]*types.Receipt
		if err := msg.Decode(&receipts); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Deliver all to the downloader
		if err := pm.downloader.DeliverReceipts(p.id, receipts); err != nil {
			log.Debug("Failed to deliver receipts", "err", err)
		}

	case msg.Code == NewBlockHashesMsg:
		var announces newBlockHashesData
		if err := msg.Decode(&announces); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		// Mark the hashes as present at the remote node
		for _, block := range announces {
			p.MarkBlock(block.Hash)
		}
		// Schedule all the unknown hashes for retrieval
		unknown := make(newBlockHashesData, 0, len(announces))
		for _, block := range announces {
			if !pm.blockchain.HasBlock(block.Hash, block.Number) {
				unknown = append(unknown, block)
			}
		}
		for _, block := range unknown {
			pm.blockFetcher.Notify(p.id, block.Hash, block.Number, time.Now(), p.RequestOneHeader, p.RequestBodies)
		}

	case msg.Code == NewBlockMsg:
		// Retrieve and decode the propagated block
		var request newBlockData
		if err := msg.Decode(&request); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		if hash := types.CalcUncleHash(request.Block.Uncles()); hash != request.Block.UncleHash() {
			log.Warn("Propagated block has invalid uncles", "have", hash, "exp", request.Block.UncleHash())
			break // TODO(karalabe): return error eventually, but wait a few releases
		}
		if hash := types.DeriveSha(request.Block.Transactions()); hash != request.Block.TxHash() {
			log.Warn("Propagated block has invalid body", "have", hash, "exp", request.Block.TxHash())
			break // TODO(karalabe): return error eventually, but wait a few releases
		}
		if err := request.sanityCheck(); err != nil {
			return err
		}
		request.Block.ReceivedAt = msg.ReceivedAt
		request.Block.ReceivedFrom = p
		log.Info("E2E_TIMING", "stage", "block_validation", "event", "received_new_block_msg", "peer", p.id, "block", request.Block.Hash().Hex(), "number", request.Block.NumberU64(), "txs", len(request.Block.Transactions()), "duration_ns", int64(0))

		// Mark the peer as owning the block and schedule it for import
		enqueueStart := time.Now()
		p.MarkBlock(request.Block.Hash())
		pm.blockFetcher.Enqueue(p.id, request.Block)
		log.Info("E2E_TIMING", "stage", "block_validation", "event", "enqueue_received_block", "peer", p.id, "block", request.Block.Hash().Hex(), "number", request.Block.NumberU64(), "duration_ns", time.Since(enqueueStart).Nanoseconds())

		// Assuming the block is importable by the peer, but possibly not yet done so,
		// calculate the head hash and TD that the peer truly must have.
		var (
			trueHead = request.Block.ParentHash()
			trueTD   = new(big.Int).Sub(request.TD, request.Block.Difficulty())
		)
		// Update the peer's total difficulty if better than the previous
		if _, td := p.Head(); trueTD.Cmp(td) > 0 {
			p.SetHead(trueHead, trueTD)

			// Schedule a sync if above ours. Note, this will not fire a sync for a gap of
			// a single block (as the true TD is below the propagated block), however this
			// scenario should easily be covered by the fetcher.
			currentBlock := pm.blockchain.CurrentBlock()
			if trueTD.Cmp(pm.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())) > 0 {
				go pm.synchronise(p)
			}
		}

	case msg.Code == NewPooledTransactionHashesMsg && p.version >= eth65:
		// New transaction announcement arrived, make sure we have
		// a valid and fresh chain to handle them
		if atomic.LoadUint32(&pm.acceptTxs) == 0 {
			break
		}
		var hashes []common.Hash
		if err := msg.Decode(&hashes); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Schedule all the unknown hashes for retrieval
		for _, hash := range hashes {
			p.MarkTransaction(hash)
		}
		pm.txFetcher.Notify(p.id, hashes)

	case msg.Code == GetPooledTransactionsMsg && p.version >= eth65:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// Gather transactions until the fetch or network limits is reached
		var (
			hash   common.Hash
			bytes  int
			hashes []common.Hash
			txs    []rlp.RawValue
		)
		for bytes < softResponseLimit {
			// Retrieve the hash of the next block
			if err := msgStream.Decode(&hash); err == rlp.EOL {
				break
			} else if err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			// Retrieve the requested transaction, skipping if unknown to us
			tx := pm.txpool.Get(hash)
			if tx == nil {
				continue
			}
			// If known, encode and queue for response packet
			if encoded, err := rlp.EncodeToBytes(tx); err != nil {
				log.Error("Failed to encode transaction", "err", err)
			} else {
				hashes = append(hashes, hash)
				txs = append(txs, encoded)
				bytes += len(encoded)
			}
		}
		return p.SendPooledTransactionsRLP(hashes, txs)

	case msg.Code == TransactionMsg || (msg.Code == PooledTransactionsMsg && p.version >= eth65):
		receiveStart := time.Now()
		// Transactions arrived, make sure we have a valid and fresh chain to handle them
		if atomic.LoadUint32(&pm.acceptTxs) == 0 {
			break
		}
		// Transactions can be processed, parse all of them and deliver to the pool
		var txs []*types.Transaction
		if err := msg.Decode(&txs); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		for i, tx := range txs {
			// Validate and mark the remote transaction
			if tx == nil {
				return errResp(ErrDecode, "transaction %d is nil", i)
			}
			hash, err := tx.HashWithError()
			if err != nil {
				log.Debug("Deferring remote transactions without a canonical chameleon key", "err", err)
				return nil
			}
			p.MarkTransaction(hash)
		}
		pm.txFetcher.Enqueue(p.id, txs, msg.Code == PooledTransactionsMsg)
		log.Info("E2E_TIMING", "stage", "transaction_validation", "event", "received_p2p_transactions", "peer", p.id, "tx_count", len(txs), "pooled", msg.Code == PooledTransactionsMsg, "duration_ns", time.Since(receiveStart).Nanoseconds())

	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

// BroadcastBlock will either propagate a block to a subset of its peers, or
// will only announce its availability (depending what's requested).
func (pm *ProtocolManager) BroadcastBlock(block *types.Block, propagate bool) {
	start := time.Now()
	hash := block.Hash()
	peers := pm.peers.PeersWithoutBlock(hash)
	recipients := 0
	defer func() {
		log.Info("E2E_TIMING", "stage", "block_generation", "event", "broadcast_block_enqueue", "block", hash.Hex(), "number", block.NumberU64(), "propagate", propagate, "peer_candidates", len(peers), "peer_recipients", recipients, "duration_ns", time.Since(start).Nanoseconds())
	}()

	// If propagation is requested, send to a subset of the peer
	if propagate {
		// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
		var td *big.Int
		if parent := pm.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
			td = new(big.Int).Add(block.Difficulty(), pm.blockchain.GetTd(block.ParentHash(), block.NumberU64()-1))
		} else {
			log.Error("Propagating dangling block", "number", block.Number(), "hash", hash)
			return
		}
		// Send the block to a subset of our peers, bounded by the configured
		// recipient limit (0 keeps the legacy sqrt(peer count) subset).
		limit := int(math.Sqrt(float64(len(peers))))
		if pm.blockBroadcastPeerLimit > 0 {
			limit = pm.blockBroadcastPeerLimit
		}
		if limit > len(peers) {
			limit = len(peers)
		}
		transfer := peers[:limit]
		for _, peer := range transfer {
			peer.AsyncSendNewBlock(block, td)
		}
		recipients = len(transfer)
		log.Trace("Propagated block", "hash", hash, "recipients", len(transfer), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
		return
	}
	// Otherwise if the block is indeed in out own chain, announce it
	if pm.blockchain.HasBlock(hash, block.NumberU64()) {
		for _, peer := range peers {
			peer.AsyncSendNewBlockHash(block)
		}
		log.Trace("Announced block", "hash", hash, "recipients", len(peers), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
	}
}

// BroadcastTransactions will propagate a batch of transactions to all peers which are not known to
// already have the given transaction.
func (pm *ProtocolManager) BroadcastTransactions(txs types.Transactions, propagate bool) {
	start := time.Now()
	defer func() {
		log.Info("E2E_TIMING", "stage", "transaction_generation", "event", "broadcast_transactions_enqueue", "tx_count", len(txs), "propagate", propagate, "duration_ns", time.Since(start).Nanoseconds())
	}()
	var (
		txset = make(map[*peer][]common.Hash)
		annos = make(map[*peer][]common.Hash)
	)
	// Broadcast transactions to a batch of peers not knowing about it
	if propagate {
		for _, tx := range txs {
			peers := pm.peers.PeersWithoutTx(tx.Hash())

			// Send the block to a subset of our peers
			transfer := peers[:int(math.Sqrt(float64(len(peers))))]
			for _, peer := range transfer {
				txset[peer] = append(txset[peer], tx.Hash())
			}
			log.Trace("Broadcast transaction", "hash", tx.Hash(), "recipients", len(peers))
		}
		for peer, hashes := range txset {
			peer.AsyncSendTransactions(hashes)
		}
		return
	}
	// Otherwise only broadcast the announcement to peers
	for _, tx := range txs {
		peers := pm.peers.PeersWithoutTx(tx.Hash())
		for _, peer := range peers {
			annos[peer] = append(annos[peer], tx.Hash())
		}
	}
	for peer, hashes := range annos {
		if peer.version >= eth65 {
			peer.AsyncSendPooledTransactionHashes(hashes)
		} else {
			peer.AsyncSendTransactions(hashes)
		}
	}
}

// Mined broadcast loop
// /////////flag
// 毕设
func (pm *ProtocolManager) minedBroadcastLoop() {
	// automatically stops if unsubscribe
	for obj := range pm.minedBlockSub.Chan() {
		if ev, ok := obj.Data.(core.NewMinedBlockEvent); ok {
			log.Info("broadcast start")
			pm.BroadcastBlock(ev.Block, true)  // First propagate block to peers
			pm.BroadcastBlock(ev.Block, false) // Only then announce to the rest
		}
		if _, ok := obj.Data.(core.NewPublicKeyEvent); ok {
			log.Warn("Ignoring legacy NewPublicKeyEvent: per-node chameleon key submission is unsupported")

		}
		//if _, ok := obj.Data.(core.NewRedactTxEvent); ok {
		//	pm.BroadcastModifyProposal()
		//}
	}
}

func (pm *ProtocolManager) txBroadcastLoop() {
	for {
		select {
		case event := <-pm.txsCh:
			// For testing purpose only, disable propagation
			if pm.broadcastTxAnnouncesOnly {
				pm.BroadcastTransactions(event.Txs, false)
				continue
			}
			pm.BroadcastTransactions(event.Txs, true)  // First propagate transactions to peers
			pm.BroadcastTransactions(event.Txs, false) // Only then announce to the rest

		// Err() channel will be closed when unsubscribing.
		case <-pm.txsSub.Err():
			return
		}
	}
}

// NodeInfo represents a short summary of the Ethereum sub-protocol metadata
// known about the host peer.
type NodeInfo struct {
	Network    uint64              `json:"network"`    // Ethereum network ID (1=Frontier, 2=Morden, Ropsten=3, Rinkeby=4)
	Difficulty *big.Int            `json:"difficulty"` // Total difficulty of the host's blockchain
	Genesis    common.Hash         `json:"genesis"`    // SHA3 hash of the host's genesis block
	Config     *params.ChainConfig `json:"config"`     // Chain configuration for the fork rules
	Head       common.Hash         `json:"head"`       // SHA3 hash of the host's best owned block
}

// NodeInfo retrieves some protocol metadata about the running host node.
func (pm *ProtocolManager) NodeInfo() *NodeInfo {
	currentBlock := pm.blockchain.CurrentBlock()
	return &NodeInfo{
		Network:    pm.networkID,
		Difficulty: pm.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64()),
		Genesis:    pm.blockchain.Genesis().Hash(),
		Config:     pm.blockchain.Config(),
		Head:       currentBlock.Hash(),
	}
}

// 广播交易修改提案
// 毕设
func (pm *ProtocolManager) BroadcastModifyProposal(proposalTx types.ProposalTx) {
	ps := pm.peers.peers
	for _, peer := range ps {
		if err := peer.SendModifyProposal(proposalTx); err != nil {
			log.Error("BroadcastModifyProposal error", "err", err)
		}
	}
	log.Error("BroadcastModifyProposal Success!")
}

// 广播交易修改信息
// 毕设
func (pm *ProtocolManager) BroadcastModifyMsg(msg types.ModifyMsg) {
	if err := validatePayloadOnlyModify(&msg.ProposalTx.Msg); err != nil {
		log.Warn("Refusing to broadcast unsupported redaction", "err", err, "reqID", bytes32ToHexString(msg.ReqID))
		return
	}
	msg = pm.prepareTxModifyOutbound(msg)
	pm.cacheTxModifyMsg(msg)
	pm.markTxModifySeen(msg)
	success, failed := pm.broadcastTxModifyMsgExcept(msg, "")
	log.Info("BroadcastModifyMsg Success!", "reqID", bytes32ToHexString(msg.ReqID), "success", success, "failed", failed)
}

func (pm *ProtocolManager) broadcastTxModifyMsgExcept(msg types.ModifyMsg, excludePeerID string) (int, int) {
	success, failed := 0, 0
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if err := peer.SendModifyMsg(msg); err != nil {
			failed++
			log.Warn("BroadcastModifyMsg error", "peer", peer.id, "reqID", bytes32ToHexString(msg.ReqID), "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastTEEDistKG(msg teehost.DistKGProtocolMessage) (int, int) {
	msg = pm.prepareDistKGOutbound(msg)
	return pm.broadcastTEEDistKGExcept(msg, "", 0)
}

func (pm *ProtocolManager) broadcastTEEDistKGExcept(msg teehost.DistKGProtocolMessage, excludePeerID string, maxPeers int) (int, int) {
	success, failed := 0, 0
	attempted := 0
	if maxPeers <= 0 {
		maxPeers = math.MaxInt32
	}
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if attempted >= maxPeers {
			break
		}
		attempted++
		if err := peer.SendTEEDistKG(msg); err != nil {
			failed++
			log.Warn("BroadcastTEEDistKG error", "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastDistKG(ctx context.Context, msg teehost.DistKGProtocolMessage) error {
	msg = pm.prepareDistKGOutbound(msg)
	pm.rememberDistKGMessage(msg)
	success, failed := pm.broadcastTEEDistKGExcept(msg, "", 0)
	if success == 0 && failed > 0 {
		return fmt.Errorf("failed to broadcast TEE DistKG to %d peers", failed)
	}
	return nil
}

func (pm *ProtocolManager) SendDistKGShare(ctx context.Context, toIndex int, msg teehost.DistKGProtocolMessage) error {
	if toIndex < 1 || msg.Share == nil || msg.Share.ToIndex != uint64(toIndex) {
		return errors.New("invalid TEE DistKG share recipient")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	router, ok := pm.teeHost.(interface {
		DistKGRecipient(int) ([32]byte, bool)
	})
	if !ok {
		return pm.BroadcastDistKG(ctx, msg)
	}
	recipient, ok := router.DistKGRecipient(toIndex)
	log.Debug("TEE DistKG share route", "fromIndex", msg.Share.FromIndex, "toIndex", toIndex, "recipient", common.Bytes2Hex(recipient[:]), "resolved", ok)
	if !ok {
		return pm.BroadcastDistKG(ctx, msg)
	}
	if recipient == pm.teeHost.HardwareID() {
		return pm.teeHost.HandleDistKG(ctx, msg, pm)
	}
	msg = pm.prepareDistKGOutbound(msg)
	fingerprint, err := distKGMessageFingerprint(msg)
	if err != nil {
		return err
	}
	firstSeen := pm.markDistKGSeen(fingerprint)
	if target := pm.peerForTEESigPubKey(recipient); target != nil {
		if err := target.SendTEEDistKG(msg); err == nil {
			return nil
		} else if !firstSeen {
			return err
		}
		// The direct attempt failed; exclude that connection from the fallback.
		success, failed := pm.broadcastTEEDistKGExcept(msg, target.id, 0)
		if success == 0 {
			return fmt.Errorf("TEE DistKG direct send and fallback failed (%d fallback failures)", failed)
		}
		return nil
	}
	if !firstSeen {
		return nil
	}
	return pm.BroadcastDistKG(ctx, msg)
}

func (pm *ProtocolManager) BroadcastTEEDKRS(msg teehost.DKRSProtocolMessage) (int, int) {
	msg = pm.prepareDKRSOutbound(msg)
	return pm.broadcastTEEDKRSExcept(msg, "", 0)
}

func (pm *ProtocolManager) broadcastTEEDKRSExcept(msg teehost.DKRSProtocolMessage, excludePeerID string, maxPeers int) (int, int) {
	success, failed := 0, 0
	attempted := 0
	if maxPeers <= 0 {
		maxPeers = math.MaxInt32
	}
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if attempted >= maxPeers {
			break
		}
		attempted++
		if err := peer.SendTEEDKRS(msg); err != nil {
			failed++
			log.Warn("BroadcastTEEDKRS error", "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastDKRS(ctx context.Context, msg teehost.DKRSProtocolMessage) error {
	msg = pm.prepareDKRSOutbound(msg)
	pm.rememberDKRSMessage(msg)
	success, failed := pm.BroadcastTEEDKRS(msg)
	if success == 0 && failed > 0 {
		return fmt.Errorf("failed to broadcast TEE DKRS to %d peers", failed)
	}
	return nil
}

func (pm *ProtocolManager) SendDKRS(ctx context.Context, toSigPubKey [32]byte, msg teehost.DKRSProtocolMessage) error {
	if toSigPubKey == ([32]byte{}) {
		return errors.New("missing TEE DKRS recipient")
	}
	msg = pm.prepareDKRSOutbound(msg)
	fingerprint, err := dkrsMessageFingerprint(msg)
	if err != nil {
		return fmt.Errorf("fingerprint TEE DKRS for %x: %w", toSigPubKey, err)
	}
	firstSeen := pm.markDKRSSeen(fingerprint)
	peer := pm.peerForTEESigPubKey(toSigPubKey)
	if peer == nil {
		if !firstSeen {
			return nil
		}
		log.Warn("TEE DKRS recipient not connected; fallback broadcasting DKRS", "recipient", fmt.Sprintf("%x", toSigPubKey), "fingerprint", fingerprint)
		return pm.fallbackBroadcastDKRS(msg, "", nil)
	}
	if err := peer.SendTEEDKRS(msg); err != nil {
		sendErr := fmt.Errorf("send TEE DKRS to %x: %w", toSigPubKey, err)
		if !firstSeen {
			return nil
		}
		log.Warn("TEE DKRS direct send failed; fallback broadcasting DKRS", "recipient", fmt.Sprintf("%x", toSigPubKey), "peer", peer.id, "fingerprint", fingerprint, "err", err)
		return pm.fallbackBroadcastDKRS(msg, peer.id, sendErr)
	}
	return nil
}

func (pm *ProtocolManager) fallbackBroadcastDKRS(msg teehost.DKRSProtocolMessage, excludePeerID string, cause error) error {
	success, failed := pm.broadcastTEEDKRSExcept(msg, excludePeerID, 0)
	if success == 0 {
		if cause != nil {
			return cause
		}
		if failed > 0 {
			return fmt.Errorf("failed to fallback broadcast TEE DKRS to %d peers", failed)
		}
	}
	return nil
}

func (pm *ProtocolManager) BroadcastTEEPartialAdapt(msg teehost.PartialAdaptProtocolMessage) (int, int) {
	msg = pm.preparePartialAdaptOutbound(msg)
	return pm.broadcastTEEPartialAdaptExcept(msg, "", 0)
}

func (pm *ProtocolManager) broadcastTEEPartialAdaptExcept(msg teehost.PartialAdaptProtocolMessage, excludePeerID string, maxPeers int) (int, int) {
	success, failed := 0, 0
	attempted := 0
	if maxPeers <= 0 {
		maxPeers = math.MaxInt32
	}
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if attempted >= maxPeers {
			break
		}
		attempted++
		if err := peer.SendTEEPartialAdapt(msg); err != nil {
			failed++
			log.Warn("BroadcastTEEPartialAdapt error", "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastPartialAdapt(ctx context.Context, msg teehost.PartialAdaptProtocolMessage) error {
	msg = pm.preparePartialAdaptOutbound(msg)
	pm.rememberPartialAdaptMessage(msg)
	success, failed := pm.broadcastTEEPartialAdaptExcept(msg, "", 0)
	if success == 0 && failed > 0 {
		return fmt.Errorf("failed to broadcast TEE partial adapt to %d peers", failed)
	}
	return nil
}

func (pm *ProtocolManager) BroadcastTEERedactMaterial(material teehost.RedactionMaterial) (int, int) {
	material = pm.prepareRedactMaterialOutbound(material)
	return pm.broadcastTEERedactMaterialExcept(material, "", 0)
}

func (pm *ProtocolManager) broadcastTEERedactMaterialExcept(material teehost.RedactionMaterial, excludePeerID string, maxPeers int) (int, int) {
	success, failed := 0, 0
	attempted := 0
	if maxPeers <= 0 {
		maxPeers = math.MaxInt32
	}
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if attempted >= maxPeers {
			break
		}
		attempted++
		if err := peer.SendTEERedactMaterial(material); err != nil {
			failed++
			log.Warn("BroadcastTEERedactMaterial error", "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastRedactMaterial(ctx context.Context, material teehost.RedactionMaterial) error {
	material = pm.prepareRedactMaterialOutbound(material)
	pm.rememberRedactMaterial(material)
	success, failed := pm.broadcastTEERedactMaterialExcept(material, "", 0)
	if success == 0 && failed > 0 {
		return fmt.Errorf("failed to broadcast TEE redact material to %d peers", failed)
	}
	return nil
}

func (pm *ProtocolManager) BroadcastTEERedactAuthorized(msg teehost.RedactAuthorizedMessage) (int, int) {
	msg = pm.prepareRedactAuthorizedOutbound(msg)
	return pm.broadcastTEERedactAuthorizedExcept(msg, "", 0)
}

func (pm *ProtocolManager) broadcastTEERedactAuthorizedExcept(msg teehost.RedactAuthorizedMessage, excludePeerID string, maxPeers int) (int, int) {
	success, failed := 0, 0
	attempted := 0
	if maxPeers <= 0 {
		maxPeers = math.MaxInt32
	}
	for _, peer := range pm.snapshotPeers() {
		if excludePeerID != "" && peer.id == excludePeerID {
			continue
		}
		if attempted >= maxPeers {
			break
		}
		attempted++
		if err := peer.SendTEERedactAuthorized(msg); err != nil {
			failed++
			log.Warn("BroadcastTEERedactAuthorized error", "err", err)
			continue
		}
		success++
	}
	return success, failed
}

func (pm *ProtocolManager) BroadcastRedactAuthorized(ctx context.Context, msg teehost.RedactAuthorizedMessage) error {
	msg = pm.prepareRedactAuthorizedOutbound(msg)
	pm.rememberRedactAuthorized(msg)
	success, failed := pm.broadcastTEERedactAuthorizedExcept(msg, "", 0)
	if success == 0 && failed > 0 {
		return fmt.Errorf("failed to broadcast TEE redact authorized message to %d peers", failed)
	}
	return nil
}

// waitVisibleInitialBackoff controls the first retry delay when an edit's
// ExecuteRedaction tx is not yet visible on the LOCAL chain (TxModify gossip
// usually outruns block import by one hop on non-miner nodes). Historically
// 500ms, which forced every lost race into a ~500ms stall; 1ms with x2
// exponential growth catches short (block-propagation-sized) gaps in
// ~15-60ms while reaching the old 500ms point after ~10 cheap local
// eth_calls. Cap (5s), jitter, and the 2min deadline are unchanged.
// Override with TEE_WAIT_VISIBLE_INITIAL_BACKOFF_MS.
var waitVisibleInitialBackoff = func() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("TEE_WAIT_VISIBLE_INITIAL_BACKOFF_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return time.Millisecond
}()

func waitRedactionExecutionVisible(reqID [32]byte, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	backoff := waitVisibleInitialBackoff
	const maxBackoff = 5 * time.Second
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lastErr error
	for time.Now().Before(deadline) {
		executed, err := contractService.IsRedactionExecuted(reqID)
		if err == nil && executed {
			return nil
		}
		lastErr = err

		sleep := backoff
		if jitterRange := int64(backoff / 2); jitterRange > 0 {
			sleep += time.Duration(rng.Int63n(jitterRange+1)) - backoff/4
		}
		if remaining := time.Until(deadline); sleep > remaining {
			sleep = remaining
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
	if lastErr != nil {
		return fmt.Errorf("redaction request execution not visible on-chain: %w", lastErr)
	}
	return errors.New("redaction request is not executed on-chain")
}

func (pm *ProtocolManager) handleTxModifyMsg(modifyMsg types.ModifyMsg) {
	if err := validatePayloadOnlyModify(&modifyMsg.ProposalTx.Msg); err != nil {
		log.Warn("Cannot apply unsupported redaction", "err", err, "reqID", bytes32ToHexString(modifyMsg.ReqID))
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "unsupported_modify_type", "error", err)
		return
	}
	applyKey := txModifyMsgKey(modifyMsg)
	if !pm.beginTxModifyApply(applyKey) {
		return
	}
	applied := false
	defer func() {
		pm.finishTxModifyApply(applyKey, applied)
	}()

	// ledger_update 起点（与 nitch 对齐：apply 去重后、本地处理前；排除 gossip 层中继耗时）
	pm.emitLedgerTiming("tx_modify_received", modifyMsg)
	luStart := time.Now()
	luStep := time.Now()
	proposalTx := modifyMsg.ProposalTx
	blockNumber := proposalTx.Msg.BlockNum
	if blockNumber == nil {
		log.Warn("Cannot apply modify message: block number missing", "reqID", bytes32ToHexString(modifyMsg.ReqID))
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "block_number_missing")
		return
	}
	oldBlock := pm.blockchain.GetBlockByNumber(blockNumber.Uint64())
	if oldBlock == nil {
		log.Warn("Cannot apply modify message: block not found", "number", blockNumber)
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "block_not_found")
		return
	}
	newBlock := modifyMsg.NewBlock
	if newBlock == nil {
		log.Warn("Cannot apply modify message: new block missing", "number", blockNumber)
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "new_block_missing")
		return
	}
	pm.emitLedgerTiming("lu_getblock", modifyMsg,
		"duration_ns", time.Since(luStep).Nanoseconds(), "elapsed_ns", time.Since(luStart).Nanoseconds())
	luStep = time.Now()
	if modifyMsg.ReqID != ([32]byte{}) {
		waitVisible := waitRedactionExecutionVisible
		if pm.waitRedactionVisible != nil {
			waitVisible = pm.waitRedactionVisible
		}
		if err := waitVisible(modifyMsg.ReqID, 2*time.Minute); err != nil {
			log.Warn("Redaction execution not visible before modify message handling timed out", "err", err, "reqID", bytes32ToHexString(modifyMsg.ReqID))
			pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "execution_not_visible", "error", err)
			return
		}
	}
	pm.emitLedgerTiming("lu_wait_visible", modifyMsg,
		"duration_ns", time.Since(luStep).Nanoseconds(), "elapsed_ns", time.Since(luStart).Nanoseconds())
	luStep = time.Now()

	if err := validateRedactedBlock(oldBlock, newBlock, &proposalTx.Msg, func(event string, fields ...interface{}) {
		pm.emitLedgerTiming(event, modifyMsg, fields...)
	}); err != nil {
		log.Error("redacted block validation failed", "err", err)
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "validation_failed", "error", err)
		return
	}
	pm.emitLedgerTiming("lu_validate", modifyMsg,
		"duration_ns", time.Since(luStep).Nanoseconds(), "elapsed_ns", time.Since(luStart).Nanoseconds())
	luStep = time.Now()

	log.Info("modify validation pass")
	if err := pm.updateBlockInfo(oldBlock, newBlock); err != nil {
		log.Warn("Cannot apply modify message: update block failed", "err", err, "number", blockNumber)
		pm.emitLedgerTiming("tx_modify_failed", modifyMsg, "reason", "update_failed", "error", err)
		return
	}
	pm.emitLedgerTiming("lu_update", modifyMsg,
		"duration_ns", time.Since(luStep).Nanoseconds(), "elapsed_ns", time.Since(luStart).Nanoseconds())
	applied = true
	pm.emitLedgerTiming("local_block_updated", modifyMsg)
	log.Info("modify Block Info", "port", pm.port, "reqID", bytes32ToHexString(modifyMsg.ReqID), "block", blockNumber.Uint64())
}

func (pm *ProtocolManager) isONTimingNode() bool {
	return pm != nil && pm.teeHost == nil && pm.nodeRole == "on"
}

func (pm *ProtocolManager) emitLedgerTiming(event string, modifyMsg types.ModifyMsg, fields ...interface{}) {
	if !pm.isONTimingNode() {
		return
	}
	blockNumber := uint64(0)
	if modifyMsg.ProposalTx.Msg.BlockNum != nil {
		blockNumber = modifyMsg.ProposalTx.Msg.BlockNum.Uint64()
	}
	base := []interface{}{
		"join_key", common.Bytes2Hex(modifyMsg.ReqID[:]),
		"node_id", pm.port,
		"node_role", pm.nodeRole,
		"block_number", blockNumber,
		"tx_index", modifyMsg.ProposalTx.Msg.Index,
	}
	base = append(base, fields...)
	if pm.timingSink != nil {
		pm.timingSink("ledger_update", event, base...)
		return
	}
	contextFields := []interface{}{"stage", "ledger_update", "event", event}
	log.Info("E2E_TIMING", append(contextFields, base...)...)
}

func (pm *ProtocolManager) getTransaction(blockNumber uint64, index uint64) (tx *types.Transaction, err error) {
	blockHash := pm.blockchain.ReadCanonicalHash(blockNumber)
	if blockHash == (common.Hash{}) {
		log.Error("Canonical block not found", "number", blockNumber)
		return nil, fmt.Errorf("canonical block not found for %d", blockNumber)
	}
	originalBlock := pm.blockchain.ReadBlock(blockHash, blockNumber)
	if originalBlock == nil {
		log.Error("Block not found", "number", blockNumber, "hash", blockHash)
		return nil, fmt.Errorf("block data missing for %d (%s)", blockNumber, blockHash.Hex())
	}
	transactions := originalBlock.Transactions()
	if int(index) >= len(transactions) {
		log.Error("Tx index out of range", "index", index)
		return nil, fmt.Errorf("tx index out of range")
	}
	return transactions[index], nil
}
