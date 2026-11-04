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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
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
	"github.com/ethereum/go-ethereum/trie"
)

const (
	softResponseLimit = 2 * 1024 * 1024 // Target maximum size of returned blocks, headers or node data.
	estHeaderRlpSize  = 500             // Approximate size of an RLP encoded block header

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096
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

	// Test fields or hooks
	broadcastTxAnnouncesOnly bool // Testing field, disable transaction propagation

	// 添加缺失的字段
	p2pServer *p2p.Server

	port string

	reshareSeenMu               sync.Mutex
	reshareSeen                 map[common.Hash]time.Time
	reshareSeenOrder            []common.Hash
	reshareRetryMu              sync.Mutex
	reshareRetryLive            map[string]struct{}
	reshareRelayTTL             uint64
	modifySeenMu                sync.Mutex
	modifySeen                  map[common.Hash]time.Time
	modifySeenOrder             []common.Hash
	modifyRelayTTL              uint64
	committeePeerMu             sync.RWMutex
	committeePeers              map[int]string
	committeeDirectPeers        map[int]string
	pendingCommitteePeers       map[int]string
	pendingCommitteeDirectPeers map[int]string
	activeCommitteePeers        map[int]string
	activeCommitteeDirectPeers  map[int]string
	peerNodeIDResolver          func(p *peer) (NodeID, bool)
	nodeIDToPeerID              map[NodeID]string
	peerIDToNodeID              map[string]NodeID

	// Boundary & Election 观测器与生命周期 (Phase 3B)
	boundaryMu              sync.Mutex
	boundaryCoordinator     *BoundaryCoordinator
	boundaryHeadCh          chan core.ChainHeadEvent
	boundaryHeadSub         event.Subscription
	candidateNodeMapper     func(CandidateID) (NodeID, bool)
	activeCommitteeProvider ActiveCommitteeProvider

	distKGSeenMu         sync.Mutex
	distKGSeen           map[common.Hash]time.Time
	distKGSeenOrder      []common.Hash
	distKGRelayTTL       uint64
	distKGRecoveryMu     sync.Mutex
	distKGRecoveryLive   bool
	distKGRecoveryDeals  map[int]*chameleon.DistKGDeal
	distKGOutboundShares map[int]*chameleon.DistKGShare
	distKGRequested      map[string]distKGInputRequestState
	distKGRequestNonce   uint64
}

// NewProtocolManager returns a new Ethereum sub protocol manager. The Ethereum sub protocol manages peers capable
// with the Ethereum network.
func NewProtocolManager(config *params.ChainConfig, checkpoint *params.TrustedCheckpoint, mode downloader.SyncMode, networkID uint64, mux *event.TypeMux, txpool txPool, engine consensus.Engine, blockchain *core.BlockChain, chaindb ethdb.Database, cacheLimit int, whitelist map[uint64]common.Hash, portOpt ...string) (*ProtocolManager, error) {
	port := ""
	if len(portOpt) > 0 {
		port = portOpt[0]
	}
	// Create the protocol manager with the base fields
	manager := &ProtocolManager{
		networkID:                   networkID,
		forkFilter:                  forkid.NewFilter(blockchain),
		eventMux:                    mux,
		txpool:                      txpool,
		blockchain:                  blockchain,
		peers:                       newPeerSet(),
		whitelist:                   whitelist,
		newPeerCh:                   make(chan *peer),
		noMorePeers:                 make(chan struct{}),
		txsyncCh:                    make(chan *txsync),
		quitSync:                    make(chan struct{}),
		pkStore:                     keyStore.NewPublicKeyStore(),
		port:                        port,
		reshareSeen:                 make(map[common.Hash]time.Time),
		reshareRetryLive:            make(map[string]struct{}),
		reshareRelayTTL:             configuredRelayTTL("NITCH_RESHARE_RELAY_TTL", reshareRelayDefaultTTL),
		modifySeen:                  make(map[common.Hash]time.Time),
		modifyRelayTTL:              configuredRelayTTL("NITCH_MODIFY_RELAY_TTL", modifyRelayDefaultTTL),
		committeePeers:              make(map[int]string),
		committeeDirectPeers:        make(map[int]string),
		pendingCommitteePeers:       make(map[int]string),
		pendingCommitteeDirectPeers: make(map[int]string),
		activeCommitteePeers:        make(map[int]string),
		activeCommitteeDirectPeers:  make(map[int]string),
		nodeIDToPeerID:              make(map[NodeID]string),
		peerIDToNodeID:              make(map[string]NodeID),
		distKGSeen:                  make(map[common.Hash]time.Time),
		distKGRelayTTL:              configuredRelayTTL("NITCH_DISTKG_RELAY_TTL", distKGRelayDefaultTTL),
		distKGRecoveryDeals:         make(map[int]*chameleon.DistKGDeal),
		distKGOutboundShares:        make(map[int]*chameleon.DistKGShare),
		distKGRequested:             make(map[string]distKGInputRequestState),
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

	// Phase 3B: 解析选举配置并初始化 BoundaryCoordinator
	electionCfg, err := ParseElectionConfigFromEnv(nil)
	if err != nil {
		log.Warn("Failed to parse election config from env, using default", "err", err)
		electionCfg = DefaultElectionConfig()
	}
	if blockchain != nil {
		manager.boundaryCoordinator = NewBoundaryCoordinator(electionCfg, blockchain, manager.resolveCandidateNodeID)
		manager.boundaryCoordinator.RegisterHandler(NewReshareBoundaryHandler(manager))
	}
	manager.bootstrapActiveCommitteeFromEnv(electionCfg)

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
	pm.minedBlockSub = pm.eventMux.Subscribe(core.NewMinedBlockEvent{})
	go pm.minedBroadcastLoop()

	// start sync handlers
	go pm.syncer()
	go pm.txsyncLoop64() // TODO(karalabe): Legacy initial tx echange, drop with eth/64.

	// start boundary coordinator chain head observer (Phase 3B)
	pm.startBoundaryObserver()
}

func (pm *ProtocolManager) Stop() {
	log.Info("Stopping Ethereum protocol")

	pm.txsSub.Unsubscribe()        // quits txBroadcastLoop
	pm.minedBlockSub.Unsubscribe() // quits blockBroadcastLoop
	pm.stopBoundaryObserver()      // quits boundary coordinator chain-head observer (Phase 3B)

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

	// Phase 3B: 注册新对等体的 NodeID 并更新路由
	if nodeID, ok := pm.ResolvePeerNodeID(p); ok && nodeID != "" {
		pm.RegisterPeerNodeID(p.id, nodeID)
	}

	// Register the peer in the downloader. If the downloader considers it banned, we disconnect
	if err := pm.downloader.RegisterPeer(p.id, p.version, p); err != nil {
		return err
	}
	// Propagate existing transactions. new transactions appearing
	// after this will be sent via broadcasts.
	pm.syncTransactions(p)

	//// 若本节点正在等待 DistKG 且尚未发出 Deal，则立即生成并广播（首个发起或随他人一起发起）
	//if km := GetKeyManager(); km != nil && km.NeedToSendDistKGDeal() {
	//	deal, sharesToSend, err := km.StartDistKG()
	//	if err == nil {
	//		_, _ = pm.BroadcastDistKG(types.DistKGMessage{Deal: deal})
	//		for _, sh := range sharesToSend {
	//			_, _ = pm.BroadcastDistKG(types.DistKGMessage{Share: sh})
	//		}
	//		log.Info("DistKG: broadcast my deal and shares after peer connected")
	//	}
	//}

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

// acceptsTransactions also permits NITCH's initial election at genesis. Waiting
// for the first imported block here drops the one-shot solution transactions on
// non-mining relays, so a sparse network can never collect all candidates.
func (pm *ProtocolManager) acceptsTransactions() bool {
	if atomic.LoadUint32(&pm.acceptTxs) != 0 {
		return true
	}
	if atomic.LoadUint32(&pm.fastSync) != 0 || pm.checkpointNumber != 0 ||
		pm.boundaryCoordinator == nil || !chameleon.SystemPublicKeyReady() {
		return false
	}
	head := pm.blockchain.CurrentBlock()
	cfg := pm.boundaryCoordinator.Config()
	if head.NumberU64() != 0 || cfg.InitialBoundary != 0 ||
		!pm.blockchain.Config().IsNITCH(head.Number()) {
		return false
	}
	// Handshake still checks genesis/network identity, and the fetcher/txpool
	// still validate and deduplicate every transaction before it is relayed.
	if atomic.CompareAndSwapUint32(&pm.acceptTxs, 0, 1) {
		log.Info("Enabled NITCH genesis transaction relay", "block", head.NumberU64())
	}
	return true
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
	case msg.Code == ModifyVerifyMsg:
		// 验证交易修改提案并生成份额
		var redactTx types.RedactTx
		if err := msg.Decode(&redactTx); err != nil {
			return err
		}
		if redactTx.CollectorID == "" && p != nil {
			redactTx.CollectorID = p.id
		}
		messageID, firstSeen := pm.noteModifyProposalSeen(redactTx)
		if !firstSeen {
			modifyDuplicateMeter.Inc(1)
			log.Debug("Modify proposal duplicate dropped", "message_id", messageID,
				"received_from", p.id, "hop", redactTx.Relay.Hop, "ttl", redactTx.Relay.TTL)
			return nil
		}
		relaySuccess, relayFail := pm.relayModifyProposal(redactTx, p)
		log.Debug("Relayed Modify proposal", "message_id", messageID,
			"join_key", redactionJoinKey(redactTx.BlockNum, redactTx.Msg.Index),
			"success", relaySuccess, "failed", relayFail)

		km := GetKeyManager()
		if km == nil {
			log.Debug("KeyManager not initialized; relaying Modify proposal only", "message_id", messageID)
			return nil
		}
		if !km.IsCommitteeMember() {
			// Ordinary/light nodes participate in the sparse relay graph but must
			// never attempt threshold operations. Returning the membership error
			// from GenerateAdaptationShare would tear down the eth peer and can
			// partition the very path needed by the final TxModify message.
			log.Debug("Non-committee node; relaying Modify proposal only", "message_id", messageID)
			return nil
		}

		// 获取原始区块
		block := pm.blockchain.GetBlockByNumber(redactTx.BlockNum.Uint64())
		if block == nil {
			log.Warn("Block not found for proposal", "blockNum", redactTx.BlockNum)
			return nil
		}

		if err := ValidateRedactionInstruction(pm.blockchain, &redactTx.Msg); err != nil {
			log.Warn("Rejected invalid payload redaction proposal", "err", err)
			return nil
		}
		newBlock, err := pm.getNewBlock(block, &redactTx.Msg, redactTx.Msg.Index)
		if err != nil {
			log.Error("Failed to get new block", "err", err)
			return err
		}
		oldTx := block.Transactions()[redactTx.Msg.Index]
		newTx := newBlock.Transactions()[redactTx.Msg.Index]
		oldMsg, err := types.TransactionHashMessage(oldTx)
		if err != nil {
			return err
		}
		newMsg, err := types.TransactionHashMessage(newTx)
		if err != nil {
			return err
		}
		if (len(redactTx.OldMessage) > 0 && !bytes.Equal(redactTx.OldMessage, oldMsg)) ||
			(len(redactTx.NewMessage) > 0 && !bytes.Equal(redactTx.NewMessage, newMsg)) {
			log.Warn("Rejected proposal with non-canonical NITCH transaction message")
			return nil
		}

		hashOutput := redactTx.HashOutput
		if hashOutput == nil {
			// 使用系统 NITCHKey 计算原始哈希输出（所有节点使用相同的 GlobalPK）
			systemKey, err := chameleon.GetSystemNITCHKey()
			if err != nil {
				log.Error("Failed to get system NITCHKey", "err", err)
				return err
			}

			hashOutput, err = chameleon.Hash(systemKey, oldMsg, oldTx.ChameleonR())
			if err != nil {
				log.Error("Failed to compute hash", "err", err)
				return err
			}
		}

		// 生成份额
		share, err := km.GenerateAdaptationShare(oldMsg, newMsg)
		if err != nil {
			log.Error("Failed to generate adaptation share", "err", err)
			return err
		}

		// 创建提案ID（与提案者使用相同的ID生成方式）
		// 包含 BlockNum、Index、oldMsg 摘要和 newMsg 摘要，保证与提案者一致且区分同一块的不同提案
		proposalID := fmt.Sprintf("%s-%v-%x-%x", redactTx.BlockNum.String(), redactTx.Msg.Index, oldMsg[:8], newMsg[:8])
		if redactTx.ProposalID != "" {
			proposalID = redactTx.ProposalID
		}

		// 发送份额回给提案者
		shareMsg := types.KeyShareMessage{
			ProposalID:  proposalID,
			CollectorID: redactTx.CollectorID,
			Share:       share,
			OldMessage:  oldMsg,
			NewMessage:  newMsg,
			HashOutput:  hashOutput,
		}

		// 广播份额给所有节点（提案者会接收）
		pm.BroadcastKeyShare(shareMsg)
		log.Info("Generated and broadcasted adaptation share", "index", share.Index, "proposalID", proposalID)

		return nil

	case msg.Code == KeyShareMsg:
		// 接收其他节点的密钥份额
		var shareMsg types.KeyShareMessage
		if err := msg.Decode(&shareMsg); err != nil {
			return err
		}
		km := GetKeyManager()
		if km != nil && km.ShareCollectionClosed(shareMsg.ProposalID) &&
			shareMsg.CollectorID != "" && shareMsg.CollectorID == pm.localPeerID() {
			log.Debug("Late key share dropped at collector", "proposalID", shareMsg.ProposalID)
			return nil
		}
		messageID, firstSeen := pm.noteKeyShareSeen(shareMsg)
		if !firstSeen {
			modifyDuplicateMeter.Inc(1)
			log.Debug("Key share duplicate dropped", "message_id", messageID,
				"received_from", p.id, "hop", shareMsg.Relay.Hop, "ttl", shareMsg.Relay.TTL)
			return nil
		}
		// 先本地验收并唤醒收集者，避免同步转发阻塞阈值齐备。
		// 非收集者即使已停止本地收集，也仍需转发给其他节点。
		defer func() {
			relaySuccess, relayFail := pm.relayKeyShare(shareMsg, p)
			log.Debug("Relayed key share", "message_id", messageID,
				"proposalID", shareMsg.ProposalID, "success", relaySuccess, "failed", relayFail)
		}()

		if km == nil {
			log.Warn("KeyManager not initialized, cannot process share")
			return nil
		}
		if km.ShareCollectionClosed(shareMsg.ProposalID) {
			return nil
		}

		// 添加份额到提案
		err := km.AddShare(
			shareMsg.ProposalID,
			shareMsg.Share,
			shareMsg.OldMessage,
			shareMsg.NewMessage,
			shareMsg.HashOutput,
		)

		if err != nil {
			log.Error("Failed to add share", "err", err)
			return err
		}

		log.Info("Received adaptation share",
			"proposalID", shareMsg.ProposalID,
			"index", shareMsg.Share.Index,
			"totalShares", km.GetProposalSharesCount(shareMsg.ProposalID))

		// 如果收集到足够的份额，记录日志
		// 注意：份额组合由提案者在 validator.go 的 ProposeModification 中主动执行
		if km.HasEnoughShares(shareMsg.ProposalID) {
			log.Info("Enough shares collected for proposal",
				"proposalID", shareMsg.ProposalID,
				"shares", km.GetProposalSharesCount(shareMsg.ProposalID),
				"threshold", km.GetThreshold()+1)
		}

		return nil

	case msg.Code == DistKGMsg:
		// NITCH 分布式密钥生成协议消息
		var distMsg types.DistKGMessage
		if err := msg.Decode(&distMsg); err != nil {
			log.Error("Failed to decode DistKG message", "err", err)
			return err
		}
		messageID, firstSeen := pm.noteDistKGSeen(distMsg)
		if !firstSeen {
			distKGDuplicateMeter.Inc(1)
			log.Debug("DistKG relay duplicate dropped", "message_id", messageID,
				"received_from", p.id, "hop", distMsg.Relay.Hop, "ttl", distMsg.Relay.TTL)
			return nil
		}
		defer pm.relayDistKGMessage(distMsg, p, messageID)
		deal := distMsg.Deal
		share := distMsg.Share
		if distMsg.InputResponse != nil {
			if deal != nil || share != nil {
				log.Warn("DistKG input response mixed with top-level input", "message_id", messageID)
				return nil
			}
			var err error
			deal, share, err = distKGResponseInputs(distMsg.InputResponse)
			if err != nil {
				log.Warn("Invalid DistKG input response", "message_id", messageID, "err", err)
				return nil
			}
			pm.noteCommitteePeer(bigIntIndex(distMsg.InputResponse.DealerIndex), p)
		}
		// 任何节点都可以仅通过公开的 Deal/Complaint 推导系统公钥 pk（用于后续区块哈希验证）
		if deal != nil {
			if err := pm.recordDistKGDeal(deal); err != nil {
				log.Warn("Rejected DistKG deal", "message_id", messageID, "err", err)
				return nil
			}
			chameleon.ObserveDistKGDeal(deal)
		}
		if distMsg.Complaint != nil {
			chameleon.ObserveDistKGComplaint(distMsg.Complaint)
		}
		km := GetKeyManager()
		if km == nil {
			log.Debug("KeyManager not initialized; relaying DistKG message only", "message_id", messageID)
			return nil
		}
		if distMsg.InputRequest != nil {
			requester := bigIntIndex(distMsg.InputRequest.RequesterIndex)
			dealer := bigIntIndex(distMsg.InputRequest.DealerIndex)
			if requester <= 0 || dealer <= 0 || (!distMsg.InputRequest.NeedDeal && !distMsg.InputRequest.NeedShare) {
				log.Warn("Invalid DistKG input request", "message_id", messageID)
				return nil
			}
			pm.noteCommitteePeer(requester, p)
			if resp := pm.buildDistKGInputResponse(distMsg.InputRequest); resp != nil {
				log.Info("DistKG replaying cached input", "dealer", dealer, "requester", requester,
					"need_deal", distMsg.InputRequest.NeedDeal, "need_share", distMsg.InputRequest.NeedShare,
					"nonce", distMsg.InputRequest.Nonce)
				_, _ = pm.SendDistKGToIndex(requester, *resp)
			}
		}
		if deal != nil {
			pm.noteCommitteePeer(bigIntIndex(deal.DealerIndex), p)
			if err := km.OnDistKGDeal(deal); err != nil {
				log.Warn("DistKG deal processing failed", "dealer", deal.DealerIndex, "err", err)
				return nil
			}
			// 若本节点也在等待 DistKG 且尚未发出自己的 Deal，则立即生成并广播
			if km.NeedToSendDistKGDeal() {
				deal, sharesToSend, err := km.StartDistKG()
				if err == nil {
					_, _ = pm.BroadcastDistKG(types.DistKGMessage{Deal: deal})
					for _, sh := range sharesToSend {
						_, _ = pm.SendDistKGToIndex(bigIntIndex(sh.ToIndex), types.DistKGMessage{Share: sh})
					}
				}
			}
		}
		if share != nil {
			pm.noteCommitteePeer(bigIntIndex(share.FromIndex), p)
			if err := km.OnDistKGShare(share); err != nil {
				log.Warn("DistKG share processing failed", "dealer", share.FromIndex, "err", err)
				return nil
			}
		}
		if distMsg.Complaint != nil {
			km.OnDistKGComplaint(distMsg.Complaint)
		}
		if distMsg.Response != nil {
			km.OnDistKGResponse(distMsg.Response)
		}
		// 处理验证密钥广播
		if distMsg.VerifyKey != nil {
			vk := distMsg.VerifyKey
			pm.noteCommitteePeer(bigIntIndex(vk.Index), p)
			log.Info("DistKG: received verification key", "index", vk.Index)

			pp := km.GetPublicParams()
			if pp == nil {
				log.Warn("DistKG: PublicParams not available")
				return nil
			}

			// 反序列化G1点
			vkPoint, err := vk.GetVerificationKeyPoint()
			if err != nil {
				log.Error("DistKG: failed to deserialize verification key", "index", vk.Index, "err", err)
				return nil
			}

			// 创建NITCHKey对象用于注册（使用dummy地址，因为AddShare按Index匹配）
			dummyAddr := common.HexToAddress(fmt.Sprintf("00000000000000000000000000000000000%02d", vk.Index))
			// 从*big.Int获取int值
			indexInt := int(vk.Index.Int64())
			key := &chameleon.NITCHKey{
				PP:              pp,
				VerificationKey: vkPoint,
				Index:           indexInt,
			}

			// 注册到peerKeys
			km.RegisterPeerKey(dummyAddr, key)
			log.Info("DistKG: registered peer verification key", "index", vk.Index)
		}
		// 数据收齐后：验证并广播投诉（仅一次），然后若可提取则提取
		if km.DistKGHasEnoughData() {
			complaints, _ := km.DistKGVerifyAndComplain()
			for _, c := range complaints {
				cCopy := c
				_, _ = pm.BroadcastDistKG(types.DistKGMessage{Complaint: &cCopy})
			}
			if km.DistKGCanExtract() {
				if err := km.DistKGExtractAndApply(); err != nil {
					log.Warn("DistKG extract failed", "err", err)
				} else {
					// DistKG提取成功后，广播本节点的验证密钥
					if localKey := km.GetLocalKey(); localKey != nil {
						verifyKey := chameleon.NewDistKGVerifyKey(localKey)
						_, _ = pm.BroadcastDistKG(types.DistKGMessage{VerifyKey: verifyKey})
						log.Info("DistKG: broadcasted verification key", "index", localKey.Index)
					}
				}
			}
		}
		return nil

	case msg.Code == ReshareMsg:
		var reshareMsg types.ReshareMessage
		if err := msg.Decode(&reshareMsg); err != nil {
			log.Error("Failed to decode Reshare message", "err", err)
			return err
		}
		messageID, firstSeen := pm.noteReshareSeen(reshareMsg)
		if !firstSeen {
			reshareDuplicateMeter.Inc(1)
			log.Debug("Reshare relay duplicate dropped", "message_id", messageID,
				"received_from", p.id, "hop", reshareMsg.Relay.Hop, "ttl", reshareMsg.Relay.TTL)
			return nil
		}
		pm.noteResharePeer(reshareMsg, p)
		km := GetKeyManager()
		if km == nil {
			defer pm.relayReshareMessage(reshareMsg, p, messageID)
			log.Debug("KeyManager not initialized; relaying Reshare message only", "message_id", messageID)
			return nil
		}
		if km.ReshareMessageTargetsLocal(reshareMsg) {
			log.Debug("Reshare targeted message reached destination", "message_id", messageID,
				"type", reshareMessageKind(reshareMsg), "target", reshareTargetIndex(reshareMsg),
				"session", reshareMsg.SessionID)
		} else {
			defer pm.relayReshareMessage(reshareMsg, p, messageID)
		}
		if _, err := km.OnReshareMessage(reshareMsg); err != nil {
			log.Warn("Reshare message processing failed", "session", reshareMsg.SessionID, "err", err)
		}
		if reshareMsg.InputRequest != nil {
			if resp := km.BuildReshareInputResponse(reshareMsg.SessionID, reshareMsg.InputRequest); resp != nil {
				_, _ = pm.SendReshareToIndex(reshareTargetIndex(*resp), *resp)
			}
		}
		pm.advanceReshare(reshareMsg.SessionID)
		return nil

	case msg.Code == TxModifyMsg:
		log.Info("Received modify message")
		// Wolverine 论文 Protocol 2: 接收编辑消息，验证变色龙哈希后更新本地区块
		var modifyMsg types.ModifyMsg
		if err := msg.Decode(&modifyMsg); err != nil {
			return err
		}
		redactTx := modifyMsg.RedactTx
		blockNumber := redactTx.BlockNum
		modStart := time.Now()
		// 编辑关联键 "blockNum-Index"（与提案节点 + 脚本 PERF_ANCHOR 一致，跨节点关联端到端 span）。
		reqID := redactionJoinKey(redactTx.BlockNum, redactTx.Msg.Index)
		messageID, firstSeen := pm.noteModifySeen(modifyMsg)
		if !firstSeen {
			modifyDuplicateMeter.Inc(1)
			logLedgerObserverDuplicate(reqID, blockNumber.Uint64(), redactTx.Msg.Index, "relay_dedup")
			log.Debug("TxModifyMsg: duplicate modify message", "join_key", reqID)
			return nil
		}
		defer pm.relayModifyMsg(modifyMsg, p)
		log.Debug("TxModifyMsg accepted for processing", "message_id", messageID,
			"origin", modifyMsg.Relay.Origin, "received_from", p.id,
			"hop", modifyMsg.Relay.Hop, "ttl", modifyMsg.Relay.TTL)
		if km := GetKeyManager(); km != nil && km.hasRedactionApplied(reqID) {
			logLedgerObserverDuplicate(reqID, blockNumber.Uint64(), redactTx.Msg.Index, "already_applied")
			log.Debug("TxModifyMsg: duplicate redaction already applied", "join_key", reqID)
			return nil
		}
		ledgerTiming := beginLedgerObserverTiming(reqID, blockNumber.Uint64(), redactTx.Msg.Index)
		defer ledgerTiming.finish()
		// All receivers report the same three local stages, independent of role.
		logLocalStep := func(event string, startedAt time.Time) {
			log.Info("E2E_TIMING", "marker_schema", timingMarkerSchema,
				"stage", "ledger_update", "event", event,
				"duration_ns", time.Since(startedAt).Nanoseconds(), "join_key", reqID,
				"block", blockNumber.Uint64(), "tx_index", redactTx.Msg.Index,
				"node", pm.port, "status", "ok")
		}
		luStep := time.Now()
		// 本地链为唯一锚点：消息只携带新块，旧块必须已在本地同步。
		// 下方 payload-only 重建、一致性和 chameleon 校验均以本地旧块为准。
		localOldBlock := pm.blockchain.GetBlockByNumber(blockNumber.Uint64())
		if localOldBlock == nil {
			ledgerTiming.fail("old_block_not_synced")
			log.Warn("TxModifyMsg: old block not synced locally, rejecting", "number", blockNumber)
			return nil
		}
		oldBlock := localOldBlock
		newBlock := modifyMsg.NewBlock
		if newBlock == nil {
			ledgerTiming.fail("new_block_missing")
			log.Warn("TxModifyMsg: new block not found in message", "number", blockNumber)
			return nil
		}

		// Redaction policy 验证（论文 RP(C, RI) = 1）
		logLocalStep("lu_getblock", luStep)
		luValidate := time.Now()
		luStep = time.Now()
		if err := ValidateRedactionInstruction(pm.blockchain, &redactTx.Msg); err != nil {
			ledgerTiming.fail("redaction_policy_failed")
			log.Warn("TxModifyMsg: redaction policy failed", "err", err)
			return nil
		}
		if err := ValidatePayloadOnlyBlockEdit(oldBlock, newBlock, redactTx.Msg.Index, []byte(redactTx.Msg.DataAfter)); err != nil {
			ledgerTiming.fail("payload_only_check_failed")
			log.Warn("TxModifyMsg: payload-only check failed", "err", err)
			return nil
		}
		if int(redactTx.Msg.Index) >= len(newBlock.Transactions()) {
			ledgerTiming.fail("new_transaction_missing")
			return nil
		}
		ledgerTiming.step("lu_policy", luStep)
		luStep = time.Now()
		expectedBlock, err := pm.getLedgerUpdateBlockWithRandomizer(oldBlock, &redactTx.Msg, redactTx.Msg.Index, newBlock.Transactions()[redactTx.Msg.Index].ChameleonR())
		if err != nil {
			ledgerTiming.fail("expected_block_build_failed")
			return err
		}
		expectedRLP, err := rlp.EncodeToBytes(expectedBlock)
		if err != nil {
			ledgerTiming.fail("expected_block_encode_failed")
			return err
		}
		receivedRLP, err := rlp.EncodeToBytes(newBlock)
		if err != nil {
			ledgerTiming.fail("received_block_encode_failed")
			return err
		}
		if !bytes.Equal(expectedRLP, receivedRLP) {
			ledgerTiming.fail("noncanonical_block_body")
			log.Warn("TxModifyMsg: received block differs from canonical payload-only reconstruction")
			return nil
		}
		ledgerTiming.step("lu_build_expected", luStep)
		luStep = time.Now()
		if err := ValidateModifiedBlockConsistency(pm.blockchain, oldBlock, newBlock, redactTx.Msg.Index); err != nil {
			ledgerTiming.fail("consistency_check_failed")
			log.Warn("TxModifyMsg: consistency check failed", "err", err)
			return nil
		}

		// 获取系统 NITCHKey
		ledgerTiming.step("lu_consistency", luStep)
		luStep = time.Now()
		key, err := chameleon.GetSystemNITCHKey()
		if err != nil {
			ledgerTiming.fail("system_key_unavailable")
			log.Warn("Failed to get system NITCHKey", "err", err)
			return errors.New("failed to get system NITCHKey")
		}

		oldTx := oldBlock.Transactions()[redactTx.Msg.Index]
		newTx := newBlock.Transactions()[redactTx.Msg.Index]
		oldMsg, err := types.TransactionHashMessage(oldTx)
		if err != nil {
			ledgerTiming.fail("old_message_failed")
			return err
		}
		newMsg, err := types.TransactionHashMessage(newTx)
		if err != nil {
			ledgerTiming.fail("new_message_failed")
			return err
		}
		if !bytes.Equal(redactTx.OldMessage, oldMsg) || !bytes.Equal(redactTx.NewMessage, newMsg) {
			ledgerTiming.fail("noncanonical_message")
			log.Warn("TxModifyMsg: proposal message does not match transaction preimage")
			return nil
		}

		// [perf 2026-09-03 对齐 tee-connect 同名改动] oldHash 改读缓存：
		// oldTx 来自本地链，其 chameleon 哈希在导入时已由 tx.Hash() 计算并原子
		// 缓存（HashWithError 用同一 GetSystemNITCHKey + TransactionHashMessage
		// + r，参数与下方显式重算逐字节相同），重算是纯重复。缓存未命中时
		// Hash() 惰性重算，行为不劣于改前。恢复=还原为显式 chameleon.Hash 调用。
		oldHash := oldTx.Hash()
		newR := newTx.ChameleonR()
		if newR == nil {
			ledgerTiming.fail("new_randomizer_missing")
			return errors.New("edited transaction is missing chameleon randomizer")
		}
		newOutput, err := chameleon.Hash(key, newMsg, newR)
		// [perf 2026-09-03 删除冗余 Verify] Verify(hash,key,msg,r) 内部就是用
		// 相同参数重算 e(r,g2)*e(H1(m),pk) 再与 hash 比较；此处若走到比较，说明
		// oldHash==Hash(new) 已成立，Verify 恒真，不提供额外拒绝力，却多付一次
		// 2-pairing（且占用全局 pairing 锁）。恢复=在 if 条件后补
		// !chameleon.Verify(key, oldHash.Big(), newMsg, newR)。
		if err != nil || newOutput == nil || newOutput.Hash == nil ||
			oldHash != common.BytesToHash(newOutput.Hash.Bytes()) {
			ledgerTiming.fail("new_hash_verification_failed")
			log.Warn("Transaction chameleon hash verification failed, rejecting block edit",
				"oldHash", oldHash.Big(),
				"oldR", oldTx.ChameleonR(),
				"newR", newR.String())
			return errors.New("transaction chameleon hash verification failed")
		}

		// 执行编辑：更新本地区块（论文：Modifying transactions）
		ledgerTiming.step("lu_chameleon", luStep)
		logLocalStep("lu_validate", luValidate)
		log.Info("Chameleon hash verified, applying block edit",
			"blockNumber", blockNumber.Uint64(),
			"oldHash", oldHash.Big().String(),
			"newR", newR.String())

		luStep = time.Now()
		// [安全修复] 写前复查：上方校验基于进入时的 canonical，若期间 reorg 使目标高度
		// 的 canonical 变化，则放弃本次应用，避免把基于旧 canonical 的 newBlock 写成分叉。
		if current := pm.blockchain.GetBlockByNumber(blockNumber.Uint64()); current == nil || current.Hash() != oldBlock.Hash() {
			ledgerTiming.fail("canonical_changed_during_validation")
			log.Warn("TxModifyMsg: canonical block changed during validation, dropping edit",
				"number", blockNumber, "expected", oldBlock.Hash())
			return nil
		}
		if err = pm.updateBlockInfo(oldBlock, newBlock); err != nil {
			ledgerTiming.fail("update_block_info_failed")
			log.Error("Failed to update block info", "err", err)
			return err
		}
		logLocalStep("lu_update", luStep)
		ledgerTiming.updated(newBlock.Hash())

		// 性能打点：阶段6 edit_execution · 本节点已成功应用编辑（端到端 span 锚点，带 join_key + node）。
		// 收集器按 join_key 聚合 submit→各节点 edit_applied，取 max(ts)−min(ts) 为端到端延迟。
		if km := GetKeyManager(); km != nil {
			km.recordRedactionApplied(reqID)
		}
		log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_edit_applied",
			"duration_ns", time.Since(modStart).Nanoseconds(), "join_key", reqID,
			"node", pm.port, "status", "ok")

		log.Info("Block edit applied successfully", "port", pm.port, "blockNumber", blockNumber.Uint64())
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
		nbStart := time.Now()
		var request newBlockData
		if err := msg.Decode(&request); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		// 性能打点：阶段5 block_validation · 收到新区块消息（解码校验耗时，mean）
		log.Info("E2E_TIMING", "stage", "block_validation", "event", "received_new_block_msg",
			"duration_ns", time.Since(nbStart).Nanoseconds(), "block", request.Block.NumberU64(),
			"status", "ok")
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

		// Mark the peer as owning the block and schedule it for import
		p.MarkBlock(request.Block.Hash())
		enqueueStart := time.Now()
		pm.blockFetcher.Enqueue(p.id, request.Block)
		// 性能打点：阶段5 block_validation · 收到区块入队 fetcher（mean）
		log.Info("E2E_TIMING", "stage", "block_validation", "event", "enqueue_received_block",
			"duration_ns", time.Since(enqueueStart).Nanoseconds(), "block", request.Block.NumberU64(),
			"status", "ok")

		// Assuming the block is importable by the peer, but possibly not yet done so,
		// calculate the head hash and TD that the peer truly must have.
		// 类 PoS 链下区块 Difficulty() 可能为 nil，Sub 会 nil 解引用崩溃，兜底成 0。
		blockDiff := request.Block.Difficulty()
		if blockDiff == nil {
			blockDiff = new(big.Int)
		}
		var (
			trueHead = request.Block.ParentHash()
			trueTD   = new(big.Int).Sub(request.TD, blockDiff)
		)
		// Update the peer's total difficulty if better than the previous
		_, td := p.Head()
		if td == nil {
			td = new(big.Int)
		}
		if trueTD.Cmp(td) > 0 {
			p.SetHead(trueHead, trueTD)

			// Schedule a sync if above ours. Note, this will not fire a sync for a gap of
			// a single block (as the true TD is below the propagated block), however this
			// scenario should easily be covered by the fetcher.
			currentBlock := pm.blockchain.CurrentBlock()
			currentTD := pm.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
			if currentTD == nil {
				currentTD = new(big.Int)
			}
			if trueTD.Cmp(currentTD) > 0 {
				go pm.synchronise(p)
			}
		}

	case msg.Code == NewPooledTransactionHashesMsg && p.version >= eth65:
		// New transaction announcement arrived, make sure we have
		// a valid and fresh chain to handle them
		if !pm.acceptsTransactions() {
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
		// Transactions arrived, make sure we have a valid and fresh chain to handle them
		if !pm.acceptsTransactions() {
			break
		}
		// Transactions can be processed, parse all of them and deliver to the pool
		txStart := time.Now()
		var txs []*types.Transaction
		if err := msg.Decode(&txs); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		for i, tx := range txs {
			// Validate and mark the remote transaction
			if tx == nil {
				return errResp(ErrDecode, "transaction %d is nil", i)
			}
			p.MarkTransaction(tx.Hash())
		}
		pm.txFetcher.Enqueue(p.id, txs, msg.Code == PooledTransactionsMsg)
		// 性能打点：阶段3 transaction_validation · 收到 P2P 交易并交付池（mean，带批量大小）
		log.Info("E2E_TIMING", "stage", "transaction_validation", "event", "received_p2p_transactions",
			"duration_ns", time.Since(txStart).Nanoseconds(), "batch", len(txs), "status", "ok")

	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

// BroadcastBlock will either propagate a block to a subset of its peers, or
// will only announce its availability (depending what's requested).
func (pm *ProtocolManager) BroadcastBlock(block *types.Block, propagate bool) {
	bStart := time.Now()
	hash := block.Hash()
	peers := pm.peers.PeersWithoutBlock(hash)

	// If propagation is requested, send to a subset of the peer
	if propagate {
		// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
		var td *big.Int
		if parent := pm.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
			// 本链为类 PoS（mergeForkBlock:0 / terminalTotalDifficulty:0），广播的区块
			// Difficulty() 可能为 nil；直接 Add 会 nil 解引用导致节点 SIGSEGV 崩溃，兜底成 0。
			diff := block.Difficulty()
			if diff == nil {
				diff = new(big.Int)
			}
			parentTd := pm.blockchain.GetTd(block.ParentHash(), block.NumberU64()-1)
			if parentTd == nil {
				log.Debug("Propagating block with missing parent TD", "number", block.Number(), "hash", hash, "parent", block.ParentHash())
				parentTd = new(big.Int)
			}
			td = new(big.Int).Add(diff, parentTd)
		} else {
			log.Error("Propagating dangling block", "number", block.Number(), "hash", hash)
			return
		}
		// Send the block to a subset of our peers
		transfer := peers[:int(math.Sqrt(float64(len(peers))))]
		for _, peer := range transfer {
			peer.AsyncSendNewBlock(block, td)
		}
		log.Trace("Propagated block", "hash", hash, "recipients", len(transfer), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
		// 性能打点：阶段4 block_generation · 广播区块入队（propagate 路径，mean）
		log.Info("E2E_TIMING", "stage", "block_generation", "event", "broadcast_block_enqueue",
			"duration_ns", time.Since(bStart).Nanoseconds(), "block", block.NumberU64(),
			"recipients", len(transfer), "mode", "propagate", "status", "ok")
		return
	}
	// Otherwise if the block is indeed in out own chain, announce it
	if pm.blockchain.HasBlock(hash, block.NumberU64()) {
		for _, peer := range peers {
			peer.AsyncSendNewBlockHash(block)
		}
		log.Trace("Announced block", "hash", hash, "recipients", len(peers), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
		// 性能打点：阶段4 block_generation · 广播区块入队（announce 路径，mean）
		log.Info("E2E_TIMING", "stage", "block_generation", "event", "broadcast_block_enqueue",
			"duration_ns", time.Since(bStart).Nanoseconds(), "block", block.NumberU64(),
			"recipients", len(peers), "mode", "announce", "status", "ok")
	}
}

// BroadcastTransactions will propagate a batch of transactions to all peers which are not known to
// already have the given transaction.
func (pm *ProtocolManager) BroadcastTransactions(txs types.Transactions, propagate bool) {
	btxStart := time.Now()
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
		// 性能打点：阶段2 transaction_generation · 广播交易入队（propagate 路径，mean）
		log.Info("E2E_TIMING", "stage", "transaction_generation", "event", "broadcast_transactions_enqueue",
			"duration_ns", time.Since(btxStart).Nanoseconds(), "txs", len(txs),
			"mode", "propagate", "status", "ok")
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
	// 性能打点：阶段2 transaction_generation · 广播交易入队（announce 路径，mean）
	log.Info("E2E_TIMING", "stage", "transaction_generation", "event", "broadcast_transactions_enqueue",
		"duration_ns", time.Since(btxStart).Nanoseconds(), "txs", len(txs),
		"mode", "announce", "status", "ok")
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
// BroadcastKeyShare 广播密钥份额给所有节点
func (pm *ProtocolManager) BroadcastKeyShare(shareMsg types.KeyShareMessage) {
	shareMsg = pm.prepareKeyShareOutbound(shareMsg)
	_, _ = pm.noteKeyShareSeen(shareMsg)
	success, failed := 0, 0
	for _, peer := range pm.modifyPeersSnapshot("") {
		if err := peer.SendKeyShare(shareMsg); err != nil {
			log.Error("Failed to send key share", "peer", peer.id, "err", err)
			failed++
		} else {
			success++
		}
	}
	log.Info("BroadcastKeyShare finished", "proposalID", shareMsg.ProposalID, "index", shareMsg.Share.Index, "success", success, "failed", failed)
}

// BroadcastDistKG 广播 NITCH 分布式密钥生成协议消息（Deal/Share/Complaint/Response）
// 返回成功发送的 peer 数量和失败的 peer 数量
func (pm *ProtocolManager) BroadcastDistKG(msg types.DistKGMessage) (successCount, failCount int) {
	pm.cacheDistKGOutbound(msg)
	msg = pm.prepareDistKGOutbound(msg)
	messageID, _ := pm.noteDistKGSeen(msg)
	successCount, failCount = pm.broadcastDistKGExcept(msg, "")
	log.Debug("DistKG outbound broadcast", "message_id", messageID, "origin", msg.Relay.Origin,
		"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL, "success", successCount, "failed", failCount)
	return successCount, failCount
}

func bigIntIndex(v *big.Int) int {
	if v == nil || !v.IsInt64() {
		return 0
	}
	return int(v.Int64())
}

func (pm *ProtocolManager) noteCommitteePeer(index int, p *peer) {
	if pm == nil || p == nil || index <= 0 {
		return
	}
	pm.committeePeerMu.Lock()
	if pm.committeePeers == nil {
		pm.committeePeers = make(map[int]string)
	}
	pm.committeePeers[index] = p.id
	pm.committeePeerMu.Unlock()
}

func (pm *ProtocolManager) noteResharePeer(msg types.ReshareMessage, p *peer) {
	if pm == nil || p == nil {
		return
	}
	// Phase 3B: 若 peer 能够解析出稳定 NodeID，记录 peerID <-> NodeID 映射
	if nodeID, ok := pm.ResolvePeerNodeID(p); ok && nodeID != "" {
		pm.RegisterPeerNodeID(p.id, nodeID)
	}

	// 动态描述符关联学习：仅当对端就是消息的直接认证发送者（非 relay 节点）时，
	// 才允许通过消息索引将 peer 绑定到对应 NodeID；严禁将 relay peer 误绑定为目标 NodeID。
	if p != nil && msg.Relay.Origin != "" && msg.Relay.Origin == p.id {
		km := GetKeyManager()
		var pending *CommitteeDescriptor
		var active *CommitteeDescriptor
		if km != nil {
			pending = km.PendingCommittee()
			active = km.ActiveCommittee()
		}
		if pending != nil && msg.InputRequest != nil {
			rxIdx := bigIntIndex(msg.InputRequest.ReceiverIndex)
			if nodeID, ok := pending.IndexToMember(rxIdx); ok {
				pm.RegisterPeerNodeID(p.id, nodeID)
			}
		}
		if active != nil && msg.Deal != nil {
			dlIdx := bigIntIndex(msg.Deal.DealerIndex)
			if nodeID, ok := active.IndexToMember(dlIdx); ok {
				pm.RegisterPeerNodeID(p.id, nodeID)
			}
		}
	}

	// 现有学习路径（保持向后兼容）
	if msg.Deal != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.Deal.DealerIndex), p)
	}
	if msg.Share != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.Share.FromIndex), p)
	}
	if msg.Response != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.Response.DealerIndex), p)
	}
	if msg.VerifyKey != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.VerifyKey.Index), p)
	}
	if msg.InputRequest != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.InputRequest.ReceiverIndex), p)
	}
	if msg.InputResponse != nil {
		pm.noteCommitteePeer(bigIntIndex(msg.InputResponse.DealerIndex), p)
	}
	// committeePeers records a useful reverse relay path, not proof of a direct
	// connection. Record a direct route separately only when the sender is the
	// authenticated relay origin of the message.
	if p != nil && msg.Relay.Origin != "" && msg.Relay.Origin == p.id {
		pm.noteDirectCommitteePeer(reshareOriginIndex(msg), p)
	}
}

func (pm *ProtocolManager) noteDirectCommitteePeer(index int, p *peer) {
	if pm == nil || p == nil || index <= 0 {
		return
	}
	pm.committeePeerMu.Lock()
	if pm.committeeDirectPeers == nil {
		pm.committeeDirectPeers = make(map[int]string)
	}
	pm.committeeDirectPeers[index] = p.id
	pm.committeePeerMu.Unlock()
}

func (pm *ProtocolManager) committeePeer(index int) *peer {
	if pm == nil || pm.peers == nil || index <= 0 {
		return nil
	}
	pm.committeePeerMu.RLock()
	var id string
	if pm.pendingCommitteePeers != nil && pm.pendingCommitteePeers[index] != "" {
		id = pm.pendingCommitteePeers[index]
	} else if pm.committeePeers != nil {
		id = pm.committeePeers[index]
	}
	pm.committeePeerMu.RUnlock()
	if id == "" {
		return nil
	}
	return pm.peers.Peer(id)
}

func (pm *ProtocolManager) directCommitteePeer(index int) *peer {
	if pm == nil || pm.peers == nil || index <= 0 {
		return nil
	}
	pm.committeePeerMu.RLock()
	var id string
	if pm.pendingCommitteeDirectPeers != nil && pm.pendingCommitteeDirectPeers[index] != "" {
		id = pm.pendingCommitteeDirectPeers[index]
	} else if pm.committeeDirectPeers != nil {
		id = pm.committeeDirectPeers[index]
	}
	pm.committeePeerMu.RUnlock()
	if id == "" {
		return nil
	}
	return pm.peers.Peer(id)
}

// targetPeerForReshare 根据消息类型与目标索引，优先选择 pending 或 active 描述符对应的正确对端
func (pm *ProtocolManager) targetPeerForReshare(index int, msg types.ReshareMessage) *peer {
	if pm == nil || pm.peers == nil || index <= 0 {
		return nil
	}
	// Once this session has finished, active-committee indices describe the
	// next epoch. An old dealer request must be fanned out instead of sent
	// to the unrelated node now occupying that index.
	if msg.InputRequest != nil {
		if km := GetKeyManager(); km != nil && km.ReshareSessionFinished(msg.SessionID) {
			return nil
		}
	}
	pm.committeePeerMu.RLock()
	defer pm.committeePeerMu.RUnlock()

	var peerID string
	isDealerTarget := (msg.InputRequest != nil || msg.Complaint != nil)
	if isDealerTarget {
		// InputRequest, Complaint 发送给 active committee (OldCommittee) 中的 dealer
		if id, ok := pm.activeCommitteeDirectPeers[index]; ok && id != "" {
			peerID = id
		} else if id, ok := pm.activeCommitteePeers[index]; ok && id != "" {
			peerID = id
		} else if len(pm.pendingCommitteePeers) == 0 {
			// 仅在无 pending 委员会时（单委员会/legacy模式），才回退到通用路由，严防混淆 receiver 路由
			if id, ok := pm.committeeDirectPeers[index]; ok && id != "" {
				peerID = id
			} else if id, ok := pm.committeePeers[index]; ok && id != "" {
				peerID = id
			}
		}
	} else {
		// Share, InputResponse, Response 等发给 pending committee (NewCommittee) 中的 receiver
		if id, ok := pm.pendingCommitteeDirectPeers[index]; ok && id != "" {
			peerID = id
		} else if id, ok := pm.pendingCommitteePeers[index]; ok && id != "" {
			peerID = id
		} else if id, ok := pm.committeeDirectPeers[index]; ok && id != "" {
			peerID = id
		} else if id, ok := pm.committeePeers[index]; ok && id != "" {
			peerID = id
		}
	}

	if peerID != "" {
		return pm.peers.Peer(peerID)
	}
	return nil
}

// SetPeerNodeIDResolver 设置 peer 到 NodeID 的解析回调钩子
func (pm *ProtocolManager) SetPeerNodeIDResolver(resolver func(p *peer) (NodeID, bool)) {
	if pm == nil {
		return
	}
	pm.committeePeerMu.Lock()
	pm.peerNodeIDResolver = resolver
	pm.committeePeerMu.Unlock()
	pm.rebuildRoutes()
}

// RegisterPeerNodeID 将 peerID（前缀hex、完整enode URL或enode ID）与 NodeID 绑定并刷新路由
func (pm *ProtocolManager) RegisterPeerNodeID(peerID string, nodeID NodeID) {
	if pm == nil || peerID == "" || nodeID == "" {
		return
	}
	pm.committeePeerMu.Lock()
	if pm.nodeIDToPeerID == nil {
		pm.nodeIDToPeerID = make(map[NodeID]string)
	}
	if pm.peerIDToNodeID == nil {
		pm.peerIDToNodeID = make(map[string]NodeID)
	}
	pm.nodeIDToPeerID[nodeID] = peerID
	pm.peerIDToNodeID[peerID] = nodeID
	pm.committeePeerMu.Unlock()

	pm.rebuildRoutes()
}

// RegisterNodePeer 是 RegisterPeerNodeID 的参数倒置别名，方便调用
func (pm *ProtocolManager) RegisterNodePeer(nodeID NodeID, peerID string) {
	pm.RegisterPeerNodeID(peerID, nodeID)
}

// ResolvePeerNodeID 解析给定 peer 的 NodeID
func (pm *ProtocolManager) ResolvePeerNodeID(p *peer) (NodeID, bool) {
	if pm == nil || p == nil {
		return "", false
	}
	pm.committeePeerMu.RLock()
	if nodeID, ok := pm.peerIDToNodeID[p.id]; ok && nodeID != "" {
		pm.committeePeerMu.RUnlock()
		return nodeID, true
	}
	if pm.peerNodeIDResolver != nil {
		resolver := pm.peerNodeIDResolver
		pm.committeePeerMu.RUnlock()
		if nodeID, ok := resolver(p); ok && nodeID != "" {
			pm.committeePeerMu.Lock()
			pm.peerIDToNodeID[p.id] = nodeID
			pm.nodeIDToPeerID[nodeID] = p.id
			pm.committeePeerMu.Unlock()
			return nodeID, true
		}
		pm.committeePeerMu.RLock()
	}
	pm.committeePeerMu.RUnlock()

	// 基于 p2p.Peer / enode 属性的回退探测
	if p.Peer != nil {
		enodeID := p.Peer.ID().String()
		pm.committeePeerMu.RLock()
		if nodeID, ok := pm.peerIDToNodeID[enodeID]; ok && nodeID != "" {
			pm.committeePeerMu.RUnlock()
			return nodeID, true
		}
		pm.committeePeerMu.RUnlock()
		if p.Node() != nil {
			enodeURL := p.Node().String()
			pm.committeePeerMu.RLock()
			if nodeID, ok := pm.peerIDToNodeID[enodeURL]; ok && nodeID != "" {
				pm.committeePeerMu.RUnlock()
				return nodeID, true
			}
			pm.committeePeerMu.RUnlock()
		}
	}
	return "", false
}

func (pm *ProtocolManager) resolveNodeIDPeerLocked(nodeID NodeID) *peer {
	if pm == nil || pm.peers == nil || nodeID == "" {
		return nil
	}
	if peerID, ok := pm.nodeIDToPeerID[nodeID]; ok && peerID != "" {
		if p := pm.peers.Peer(peerID); p != nil {
			return p
		}
		for _, p := range pm.peers.peers {
			if pm.matchPeerLocked(p, peerID) {
				return p
			}
		}
	}
	for _, p := range pm.peers.peers {
		if NodeID(p.id) == nodeID {
			return p
		}
		if p.Peer != nil {
			if NodeID(p.Peer.ID().String()) == nodeID {
				return p
			}
			if p.Node() != nil && NodeID(p.Node().String()) == nodeID {
				return p
			}
		}
		if pm.peerNodeIDResolver != nil {
			if id, ok := pm.peerNodeIDResolver(p); ok && id == nodeID {
				return p
			}
		}
	}
	return nil
}

func (pm *ProtocolManager) matchPeerLocked(p *peer, id string) bool {
	if p == nil || id == "" {
		return false
	}
	if p.id == id {
		return true
	}
	if p.Peer != nil {
		if p.Peer.ID().String() == id {
			return true
		}
		if p.Node() != nil && p.Node().String() == id {
			return true
		}
	}
	return false
}

// UpdateCommitteeRoutes 按 Active/Pending CommitteeDescriptor 的 NodeID->epoch-local index 重建路由
func (pm *ProtocolManager) UpdateCommitteeRoutes(active, pending *CommitteeDescriptor) {
	if pm == nil {
		return
	}
	pm.committeePeerMu.Lock()
	defer pm.committeePeerMu.Unlock()
	pm.updateCommitteeRoutesLocked(active, pending)
}

func (pm *ProtocolManager) updateCommitteeRoutesLocked(active, pending *CommitteeDescriptor) {
	if pm.pendingCommitteePeers == nil {
		pm.pendingCommitteePeers = make(map[int]string)
		pm.pendingCommitteeDirectPeers = make(map[int]string)
		pm.activeCommitteePeers = make(map[int]string)
		pm.activeCommitteeDirectPeers = make(map[int]string)
	}

	if pending != nil {
		pm.pendingCommitteePeers = make(map[int]string)
		pm.pendingCommitteeDirectPeers = make(map[int]string)
		for i, nodeID := range pending.Members {
			idx := i + 1
			if p := pm.resolveNodeIDPeerLocked(nodeID); p != nil {
				pm.pendingCommitteePeers[idx] = p.id
				pm.pendingCommitteeDirectPeers[idx] = p.id
				if pm.committeePeers == nil {
					pm.committeePeers = make(map[int]string)
				}
				if pm.committeeDirectPeers == nil {
					pm.committeeDirectPeers = make(map[int]string)
				}
				pm.committeePeers[idx] = p.id
				pm.committeeDirectPeers[idx] = p.id
			}
		}
	}

	if active != nil {
		pm.activeCommitteePeers = make(map[int]string)
		pm.activeCommitteeDirectPeers = make(map[int]string)
		for i, nodeID := range active.Members {
			idx := i + 1
			if p := pm.resolveNodeIDPeerLocked(nodeID); p != nil {
				pm.activeCommitteePeers[idx] = p.id
				pm.activeCommitteeDirectPeers[idx] = p.id
				if pending == nil {
					if pm.committeePeers == nil {
						pm.committeePeers = make(map[int]string)
					}
					if pm.committeeDirectPeers == nil {
						pm.committeeDirectPeers = make(map[int]string)
					}
					pm.committeePeers[idx] = p.id
					pm.committeeDirectPeers[idx] = p.id
				}
			}
		}
	}
}

func (pm *ProtocolManager) rebuildRoutes() {
	if pm == nil {
		return
	}
	km := GetKeyManager()
	if km != nil {
		pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
	}
}

// ObserveBoundaryActivation 处理观察者节点的 boundary 激活，并同步路由状态。
func (pm *ProtocolManager) ObserveBoundaryActivation(ev *BoundaryEvent) error {
	km := GetKeyManager()
	if km == nil {
		return errors.New("KeyManager not initialized")
	}
	if _, err := km.ObserveBoundaryActivation(ev); err != nil {
		return err
	}
	if pm != nil {
		pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
	}
	return nil
}

// BoundaryCoordinator 返回 protocolManager 关联的 boundary 协调器
func (pm *ProtocolManager) BoundaryCoordinator() *BoundaryCoordinator {
	if pm == nil {
		return nil
	}
	pm.boundaryMu.Lock()
	defer pm.boundaryMu.Unlock()
	return pm.boundaryCoordinator
}

// SetBoundaryCoordinator 设置或替换 BoundaryCoordinator
func (pm *ProtocolManager) SetBoundaryCoordinator(bc *BoundaryCoordinator) {
	if pm == nil {
		return
	}
	pm.boundaryMu.Lock()
	defer pm.boundaryMu.Unlock()
	pm.boundaryCoordinator = bc
}

// SetCandidateNodeMapper 设置 CandidateID 到 NodeID 的映射函数
func (pm *ProtocolManager) SetCandidateNodeMapper(mapper func(CandidateID) (NodeID, bool)) {
	if pm == nil {
		return
	}
	pm.candidateNodeMapper = mapper
	if pm.boundaryCoordinator != nil {
		pm.boundaryCoordinator.mu.Lock()
		pm.boundaryCoordinator.nodeMapper = pm.resolveCandidateNodeID
		pm.boundaryCoordinator.mu.Unlock()
	}
}

func (pm *ProtocolManager) resolveCandidateNodeID(cid CandidateID) (NodeID, bool) {
	if pm.candidateNodeMapper != nil {
		return pm.candidateNodeMapper(cid)
	}
	return NodeID(cid), true
}

func (pm *ProtocolManager) startBoundaryObserver() {
	pm.boundaryMu.Lock()
	defer pm.boundaryMu.Unlock()

	if pm.boundaryCoordinator == nil || pm.blockchain == nil {
		return
	}
	if pm.boundaryHeadSub != nil {
		return // 避免重复启动
	}
	pm.boundaryHeadCh = make(chan core.ChainHeadEvent, 128)
	pm.boundaryHeadSub = pm.blockchain.SubscribeChainHeadEvent(pm.boundaryHeadCh)
	pm.boundaryCoordinator.StartObserver(pm.boundaryHeadSub, pm.boundaryHeadCh)
	log.Info("Boundary coordinator chain head observer started")
}

func (pm *ProtocolManager) stopBoundaryObserver() {
	pm.boundaryMu.Lock()
	defer pm.boundaryMu.Unlock()

	if pm.boundaryCoordinator == nil {
		return
	}
	pm.boundaryCoordinator.StopObserver()
	if pm.boundaryHeadSub != nil {
		pm.boundaryHeadSub.Unsubscribe()
		pm.boundaryHeadSub = nil
	}
	pm.boundaryHeadCh = nil
	log.Info("Boundary coordinator chain head observer stopped")
}

// StartBoundaryObserver 导出启动 chain-head 观测器接口（幂等）
func (pm *ProtocolManager) StartBoundaryObserver() {
	pm.startBoundaryObserver()
}

// StopBoundaryObserver 导出停止 chain-head 观测器接口（幂等）
func (pm *ProtocolManager) StopBoundaryObserver() {
	pm.stopBoundaryObserver()
}

// SetActiveCommitteeProvider 设置初始 active descriptor 发现提供者接口
func (pm *ProtocolManager) SetActiveCommitteeProvider(provider ActiveCommitteeProvider) {
	if pm == nil {
		return
	}
	pm.activeCommitteeProvider = provider
}

// BootstrapActiveCommittee 从指定的稳定 NodeID 列表初始化首个 active descriptor
func (pm *ProtocolManager) BootstrapActiveCommittee(membersInIndexOrder []NodeID, threshold int, epoch uint64, activateAt uint64) error {
	km := GetKeyManager()
	if km == nil {
		return errors.New("KeyManager not initialized")
	}
	if err := km.BootstrapActiveCommittee(membersInIndexOrder, threshold, epoch, activateAt); err != nil {
		return err
	}
	pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
	return nil
}

// BootstrapActiveCommitteeFromProvider 尝试通过配置的 ActiveCommitteeProvider bootstrap
func (pm *ProtocolManager) BootstrapActiveCommitteeFromProvider() error {
	if pm == nil {
		return errors.New("nil ProtocolManager")
	}
	if pm.activeCommitteeProvider == nil {
		return errors.New("no ActiveCommitteeProvider configured")
	}
	members, threshold, epoch, activateAt, err := pm.activeCommitteeProvider.GetInitialActiveCommittee()
	if err != nil {
		return err
	}
	return pm.BootstrapActiveCommittee(members, threshold, epoch, activateAt)
}

func parseNodeIDsFromCommaSeparated(raw string) []NodeID {
	parts := strings.Split(raw, ",")
	var out []NodeID
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, NodeID(trimmed))
		}
	}
	return out
}

func (pm *ProtocolManager) bootstrapActiveCommitteeFromEnv(electionCfg ElectionConfig) {
	km := GetKeyManager()
	if km == nil || km.ActiveCommittee() != nil {
		return
	}
	rawMembers := os.Getenv("NITCH_ACTIVE_COMMITTEE_MEMBERS")
	if rawMembers == "" {
		// 严禁以 node_registry 注册顺序假装 NodeID
		log.Debug("Active committee not bootstrapped from env (NITCH_ACTIVE_COMMITTEE_MEMBERS unset)")
		return
	}
	members := parseNodeIDsFromCommaSeparated(rawMembers)
	if len(members) == 0 {
		return
	}
	threshold := electionCfg.Threshold
	if rawThresh := os.Getenv("NITCH_ACTIVE_COMMITTEE_THRESHOLD"); rawThresh != "" {
		if t, err := strconv.Atoi(rawThresh); err == nil && t >= 0 && t < len(members) {
			threshold = t
		}
	}
	var epoch uint64
	if rawEpoch := os.Getenv("NITCH_ACTIVE_COMMITTEE_EPOCH"); rawEpoch != "" {
		if ep, err := strconv.ParseUint(rawEpoch, 10, 64); err == nil {
			epoch = ep
		}
	}
	var activateAt uint64 = electionCfg.InitialBoundary
	if rawAct := os.Getenv("NITCH_ACTIVE_COMMITTEE_ACTIVATE_AT"); rawAct != "" {
		if act, err := strconv.ParseUint(rawAct, 10, 64); err == nil {
			activateAt = act
		}
	}
	if err := km.BootstrapActiveCommittee(members, threshold, epoch, activateAt); err != nil {
		log.Warn("Failed to bootstrap active committee from env", "err", err)
	} else {
		pm.UpdateCommitteeRoutes(km.ActiveCommittee(), km.PendingCommittee())
	}
}

func (pm *ProtocolManager) SendDistKGToIndex(index int, msg types.DistKGMessage) (successCount, failCount int) {
	pm.cacheDistKGOutbound(msg)
	msg = pm.prepareDistKGOutbound(msg)
	messageID, _ := pm.noteDistKGSeen(msg)
	if peer := pm.committeePeer(index); peer != nil {
		if err := peer.SendDistKG(msg); err != nil {
			log.Warn("Failed to send targeted DistKG message", "target", index, "peer", peer.id, "err", err)
			return pm.broadcastDistKGExcept(msg, "")
		}
		log.Debug("DistKG targeted outbound", "message_id", messageID, "target", index,
			"peer", peer.id, "ttl", msg.Relay.TTL)
		return 1, 0
	}
	return pm.broadcastDistKGExcept(msg, "")
}

// BroadcastReshare 广播 DKRS 重分享协议消息（Deal/Share/Complaint/Response/VerifyKey）。
func (pm *ProtocolManager) BroadcastReshare(msg types.ReshareMessage) (successCount, failCount int) {
	msg = pm.prepareReshareOutbound(msg)
	messageID, _ := pm.noteReshareSeen(msg)
	for _, peer := range pm.resharePeersSnapshot("") {
		if err := peer.SendReshare(msg); err != nil {
			log.Warn("Failed to send Reshare message", "peer", peer.id, "err", err)
			failCount++
		} else {
			successCount++
		}
	}
	log.Debug("Reshare outbound broadcast", "message_id", messageID, "origin", msg.Relay.Origin,
		"hop", msg.Relay.Hop, "ttl", msg.Relay.TTL, "success", successCount, "failed", failCount)
	return successCount, failCount
}

// BroadcastModifyProposal 广播修改提案，请求其他节点生成份额
func (pm *ProtocolManager) BroadcastModifyProposal(redactTx types.RedactTx) {
	pm.ensureModifyCollector(&redactTx)
	redactTx = pm.prepareModifyProposalOutbound(redactTx)
	_, _ = pm.noteModifyProposalSeen(redactTx)
	success, failed := 0, 0
	for _, peer := range pm.modifyPeersSnapshot("") {
		if err := peer.SendModifyProposal(redactTx); err != nil {
			log.Error("BroadcastModifyProposal error", "err", err)
			failed++
		} else {
			success++
		}
	}
	log.Info("BroadcastModifyProposal finished", "join_key", redactionJoinKey(redactTx.BlockNum, redactTx.Msg.Index), "success", success, "failed", failed)
}

// 广播交易修改信息
// 毕设
func (pm *ProtocolManager) BroadcastModifyMsg(msg types.ModifyMsg) {
	start := time.Now()
	// 编辑关联键（与提案节点一致，便于跨节点关联广播→应用）。
	reqID := redactionJoinKey(msg.RedactTx.BlockNum, msg.RedactTx.Msg.Index)
	msg = pm.prepareModifyOutbound(msg)
	_, _ = pm.noteModifySeen(msg)
	recipients := pm.broadcastModifyMsgOnce(msg, reqID)
	log.Info("BroadcastModifyMsg success", "join_key", reqID, "recipients", recipients)
	// 性能打点：阶段6 edit_execution · 广播修改消息入队
	log.Info("E2E_TIMING", "stage", "edit_execution", "event", "redaction_broadcast_modify_msg",
		"duration_ns", time.Since(start).Nanoseconds(), "join_key", reqID,
		"recipients", recipients, "status", "ok")
}

func (pm *ProtocolManager) broadcastModifyMsgOnce(msg types.ModifyMsg, reqID string) int {
	ps := pm.peers.Peers()
	for _, peer := range ps {
		if err := peer.SendModifyMsg(msg); err != nil {
			log.Warn("BroadcastModifyMsg send failed", "join_key", reqID, "peer", peer.id, "err", err)
		}
	}
	return len(ps)
}
