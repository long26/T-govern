// Copyright 2014 The go-ethereum Authors
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

// Package eth implements the Ethereum protocol.
package eth

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

type LesServer interface {
	Start(srvr *p2p.Server)
	Stop()
	APIs() []rpc.API
	Protocols() []p2p.Protocol
	SetBloomBitsIndexer(bbIndexer *core.ChainIndexer)
	SetContractBackend(bind.ContractBackend)
}

// Ethereum implements the Ethereum full node service.
type Ethereum struct {
	config *Config

	// Channel for shutting down the service
	shutdownChan chan bool

	// Handlers
	txPool          *core.TxPool
	blockchain      *core.BlockChain
	protocolManager *ProtocolManager
	lesServer       LesServer
	dialCandiates   enode.Iterator

	// DB interfaces
	chainDb ethdb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	APIBackend *EthAPIBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	totalSum  *big.Int
	etherbase common.Address
	powWorker *PowWorker

	networkID     uint64
	netRPCService *ethapi.PublicNetAPI

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and etherbase)
}

func (s *Ethereum) AddLesServer(ls LesServer) {
	s.lesServer = ls
	ls.SetBloomBitsIndexer(s.bloomIndexer)
}

// SetClient sets a rpc client which connecting to our local node.
func (s *Ethereum) SetContractBackend(backend bind.ContractBackend) {
	// Pass the rpc client to les server if it is enabled.
	if s.lesServer != nil {
		s.lesServer.SetContractBackend(backend)
	}
}

// New creates a new Ethereum object (including the
// initialisation of the common Ethereum object)
func New(ctx *node.ServiceContext, config *Config) (*Ethereum, error) {
	// Ensure configuration values are compatible and sane
	if config.SyncMode == downloader.LightSync {
		return nil, errors.New("can't run eth.Ethereum in light sync mode, use les.LightEthereum")
	}
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	if config.Miner.GasPrice == nil || config.Miner.GasPrice.Cmp(common.Big0) <= 0 {
		log.Warn("Sanitizing invalid miner gas price", "provided", config.Miner.GasPrice, "updated", DefaultConfig.Miner.GasPrice)
		config.Miner.GasPrice = new(big.Int).Set(DefaultConfig.Miner.GasPrice)
	}
	if config.NoPruning && config.TrieDirtyCache > 0 {
		config.TrieCleanCache += config.TrieDirtyCache
		config.TrieDirtyCache = 0
	}
	log.Info("Allocated trie memory caches", "clean", common.StorageSize(config.TrieCleanCache)*1024*1024, "dirty", common.StorageSize(config.TrieDirtyCache)*1024*1024)

	// Assemble the Ethereum object
	chainDb, err := ctx.OpenDatabaseWithFreezer("chaindata", config.DatabaseCache, config.DatabaseHandles, config.DatabaseFreezer, "eth/db/chaindata/")
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlockWithOverride(chainDb, config.Genesis, config.OverrideIstanbul, config.OverrideMuirGlacier)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	eth := &Ethereum{
		config:         config,
		chainDb:        chainDb,
		eventMux:       ctx.EventMux,
		accountManager: ctx.AccountManager,
		engine:         CreateConsensusEngine(ctx, chainConfig, &config.Ethash, config.Miner.Notify, config.Miner.Noverify, chainDb),
		shutdownChan:   make(chan bool),
		networkID:      config.NetworkId,
		gasPrice:       config.Miner.GasPrice,
		etherbase:      config.Miner.Etherbase,
		bloomRequests:  make(chan chan *bloombits.Retrieval),
		bloomIndexer:   NewBloomIndexer(chainDb, params.BloomBitsBlocks, params.BloomConfirms),
	}

	bcVersion := rawdb.ReadDatabaseVersion(chainDb)
	var dbVer = "<nil>"
	if bcVersion != nil {
		dbVer = fmt.Sprintf("%d", *bcVersion)
	}
	log.Info("Initialising Ethereum protocol", "versions", ProtocolVersions, "network", config.NetworkId, "dbversion", dbVer)

	if !config.SkipBcVersionCheck {
		if bcVersion != nil && *bcVersion > core.BlockChainVersion {
			return nil, fmt.Errorf("database version is v%d, Geth %s only supports v%d", *bcVersion, params.VersionWithMeta, core.BlockChainVersion)
		} else if bcVersion == nil || *bcVersion < core.BlockChainVersion {
			log.Warn("Upgrade blockchain database version", "from", dbVer, "to", core.BlockChainVersion)
			rawdb.WriteDatabaseVersion(chainDb, core.BlockChainVersion)
		}
	}
	var (
		vmConfig = vm.Config{
			EnablePreimageRecording: config.EnablePreimageRecording,
			EWASMInterpreter:        config.EWASMInterpreter,
			EVMInterpreter:          config.EVMInterpreter,
		}
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:      config.TrieCleanCache,
			TrieCleanNoPrefetch: config.NoPrefetch,
			TrieDirtyLimit:      config.TrieDirtyCache,
			TrieDirtyDisabled:   config.NoPruning,
			TrieTimeLimit:       config.TrieTimeout,
		}
	)
	eth.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, chainConfig, eth.engine, vmConfig, eth.shouldPreserve)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		eth.blockchain.SetHead(compat.RewindTo)
		rawdb.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	eth.bloomIndexer.Start(eth.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = ctx.ResolvePath(config.TxPool.Journal)
	}
	eth.txPool = core.NewTxPool(config.TxPool, chainConfig, eth.blockchain)

	// Permit the downloader to use the trie cache allowance during fast sync
	cacheLimit := cacheConfig.TrieCleanLimit + cacheConfig.TrieDirtyLimit
	checkpoint := config.Checkpoint
	if checkpoint == nil {
		checkpoint = params.TrustedCheckpoints[genesisHash]
	}
	if eth.protocolManager, err = NewProtocolManager(chainConfig, checkpoint, config.SyncMode, config.NetworkId, eth.eventMux, eth.txPool, eth.engine, eth.blockchain, chainDb, cacheLimit, config.Whitelist, strconv.Itoa(ctx.Config.HTTPPort)); err != nil {
		return nil, err
	}
	// 毕设
	eth.miner = miner.New(eth, &config.Miner, chainConfig, eth.EventMux(), eth.engine, eth.isLocalBlock, eth.protocolManager.pkStore)
	eth.miner.SetExtra(makeExtraData(config.Miner.ExtraData))

	eth.APIBackend = &EthAPIBackend{ctx.ExtRPCEnabled(), eth, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.Miner.GasPrice
	}
	eth.APIBackend.gpo = gasprice.NewOracle(eth.APIBackend, gpoParams)

	eth.dialCandiates, err = eth.setupDiscovery(&ctx.Config.P2P)
	if err != nil {
		return nil, err
	}

	// Initialize PoW rotation worker (Phase 3B)
	workerCfg, err := ParsePowWorkerConfigFromEnv(nil)
	if err != nil {
		log.Warn("Failed to parse PoW worker configuration from env", "err", err)
		workerCfg = DefaultPowWorkerConfig()
	}
	eth.powWorker = NewPowWorker(
		workerCfg,
		TxSubmitterFunc(func(tx *types.Transaction) error {
			return eth.txPool.AddLocal(tx)
		}),
		NonceProviderFunc(func(addr common.Address) (uint64, error) {
			return eth.txPool.Nonce(addr), nil
		}),
	)
	eth.powWorker.SetKeyReadyFunc(chameleon.SystemPublicKeyReady)
	if cand, ok, err := CandidateFromEnv(nil); err != nil {
		log.Warn("Failed to configure candidate from env for PoW worker", "err", err)
	} else if ok && cand != nil {
		if err := eth.powWorker.AddCandidate(*cand); err != nil {
			log.Warn("Failed to add candidate to PoW worker", "candidate", cand.CandidateID, "err", err)
		} else {
			log.Info("Registered candidate for PoW worker from env", "candidate", cand.CandidateID, "address", cand.Address.Hex())
		}
	}
	if eth.powWorker.Enabled() && eth.protocolManager != nil && eth.protocolManager.BoundaryCoordinator() != nil {
		eth.protocolManager.BoundaryCoordinator().AddHandler(eth.powWorker.BoundaryHandler())
		log.Info("PoW election worker attached to boundary coordinator",
			"target", workerCfg.Target.Hex(), "systemAddress", workerCfg.SystemAddress.Hex())
	}

	return eth, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"geth",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// CreateConsensusEngine creates the required type of consensus engine instance for an Ethereum service
func CreateConsensusEngine(ctx *node.ServiceContext, chainConfig *params.ChainConfig, config *ethash.Config, notify []string, noverify bool, db ethdb.Database) consensus.Engine {
	// If proof-of-authority is requested, set it up
	if chainConfig.Clique != nil {
		return clique.New(chainConfig.Clique, db)
	}
	// Otherwise assume proof-of-work
	switch config.PowMode {
	case ethash.ModeFake:
		log.Warn("Ethash used in fake mode")
		return ethash.NewFaker()
	case ethash.ModeTest:
		log.Warn("Ethash used in test mode")
		return ethash.NewTester(nil, noverify)
	case ethash.ModeShared:
		log.Warn("Ethash used in shared mode")
		return ethash.NewShared()
	default:
		engine := ethash.New(ethash.Config{
			CacheDir:       ctx.ResolvePath(config.CacheDir),
			CachesInMem:    config.CachesInMem,
			CachesOnDisk:   config.CachesOnDisk,
			DatasetDir:     config.DatasetDir,
			DatasetsInMem:  config.DatasetsInMem,
			DatasetsOnDisk: config.DatasetsOnDisk,
		}, notify, noverify)
		engine.SetThreads(-1) // Disable CPU mining
		return engine
	}
}

// APIs return the collection of RPC services the ethereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Ethereum) APIs() []rpc.API {
	apis := ethapi.GetAPIs(s.APIBackend)

	// Append any APIs exposed explicitly by the les server
	if s.lesServer != nil {
		apis = append(apis, s.lesServer.APIs()...)
	}
	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append any APIs exposed explicitly by the les server
	if s.lesServer != nil {
		apis = append(apis, s.lesServer.APIs()...)
	}

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicEthereumAPI(s),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicMinerAPI(s),
			Public:    true,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "miner",
			Version:   "1.0",
			Service:   NewPrivateMinerAPI(s),
			Public:    false,
		}, {
			Namespace: "eth",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.APIBackend, false),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *Ethereum) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Ethereum) Etherbase() (eb common.Address, err error) {
	s.lock.RLock()
	etherbase := s.etherbase
	s.lock.RUnlock()

	if etherbase != (common.Address{}) {
		return etherbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			etherbase := accounts[0].Address

			s.lock.Lock()
			s.etherbase = etherbase
			s.lock.Unlock()

			log.Info("Etherbase automatically configured", "address", etherbase)
			return etherbase, nil
		}
	}
	return common.Address{}, fmt.Errorf("etherbase must be explicitly specified")
}

// isLocalBlock checks whether the specified block is mined
// by local miner accounts.
//
// We regard two types of accounts as local miner account: etherbase
// and accounts specified via `txpool.locals` flag.
func (s *Ethereum) isLocalBlock(block *types.Block) bool {
	author, err := s.engine.Author(block.Header())
	if err != nil {
		log.Warn("Failed to retrieve block author", "number", block.NumberU64(), "hash", block.Hash(), "err", err)
		return false
	}
	// Check whether the given address is etherbase.
	s.lock.RLock()
	etherbase := s.etherbase
	s.lock.RUnlock()
	if author == etherbase {
		return true
	}
	// Check whether the given address is specified by `txpool.local`
	// CLI flag.
	for _, account := range s.config.TxPool.Locals {
		if account == author {
			return true
		}
	}
	return false
}

// shouldPreserve checks whether we should preserve the given block
// during the chain reorg depending on whether the author of block
// is a local account.
func (s *Ethereum) shouldPreserve(block *types.Block) bool {
	// The reason we need to disable the self-reorg preserving for clique
	// is it can be probable to introduce a deadlock.
	//
	// e.g. If there are 7 available signers
	//
	// r1   A
	// r2     B
	// r3       C
	// r4         D
	// r5   A      [X] F G
	// r6    [X]
	//
	// In the round5, the inturn signer E is offline, so the worst case
	// is A, F and G sign the block of round5 and reject the block of opponents
	// and in the round6, the last available signer B is offline, the whole
	// network is stuck.
	if _, ok := s.engine.(*clique.Clique); ok {
		return false
	}
	return s.isLocalBlock(block)
}

// SetEtherbase sets the mining reward address.
func (s *Ethereum) SetEtherbase(etherbase common.Address) {
	s.lock.Lock()
	s.etherbase = etherbase
	s.lock.Unlock()

	s.miner.SetEtherbase(etherbase)
}

// StartMining starts the miner with the given number of CPU threads. If mining
// is already running, this method adjust the number of threads allowed to use
// and updates the minimum price required by the transaction pool.
func (s *Ethereum) StartMining(threads int) error {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		log.Info("Updated mining threads", "threads", threads)
		if threads == 0 {
			threads = -1 // Disable the miner from within
		}
		th.SetThreads(threads)
	}
	// If the miner was not running, initialize it
	if !s.IsMining() {
		// Propagate the initial price point to the transaction pool
		s.lock.RLock()
		price := s.gasPrice
		s.lock.RUnlock()
		s.txPool.SetGasPrice(price)

		// Configure the local mining address
		eb, err := s.Etherbase()
		if err != nil {
			log.Error("Cannot start mining without etherbase", "err", err)
			return fmt.Errorf("etherbase missing: %v", err)
		}
		if clique, ok := s.engine.(*clique.Clique); ok {
			wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
			if wallet == nil || err != nil {
				log.Error("Etherbase account unavailable locally", "err", err)
				return fmt.Errorf("signer missing: %v", err)
			}
			clique.Authorize(eb, wallet.SignData)
		}
		// If mining is started, we can disable the transaction rejection mechanism
		// introduced to speed sync times.
		atomic.StoreUint32(&s.protocolManager.acceptTxs, 1)

		go s.miner.Start(eb)
	}
	return nil
}

// StopMining terminates the miner, both at the consensus engine level as well as
// at the block creation level.
func (s *Ethereum) StopMining() {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		th.SetThreads(-1)
	}
	// Stop the block creating itself
	s.miner.Stop()
}

func (s *Ethereum) IsMining() bool      { return s.miner.Mining() }
func (s *Ethereum) Miner() *miner.Miner { return s.miner }

func (s *Ethereum) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *Ethereum) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *Ethereum) TxPool() *core.TxPool               { return s.txPool }
func (s *Ethereum) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Ethereum) Engine() consensus.Engine           { return s.engine }
func (s *Ethereum) ChainDb() ethdb.Database            { return s.chainDb }
func (s *Ethereum) IsListening() bool                  { return true } // Always listening
func (s *Ethereum) EthVersion() int                    { return int(ProtocolVersions[0]) }
func (s *Ethereum) NetVersion() uint64                 { return s.networkID }
func (s *Ethereum) Downloader() *downloader.Downloader { return s.protocolManager.downloader }
func (s *Ethereum) Synced() bool                       { return atomic.LoadUint32(&s.protocolManager.acceptTxs) == 1 }
func (s *Ethereum) ArchiveMode() bool                  { return s.config.NoPruning }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *Ethereum) Protocols() []p2p.Protocol {
	protos := make([]p2p.Protocol, len(ProtocolVersions))
	for i, vsn := range ProtocolVersions {
		protos[i] = s.protocolManager.makeProtocol(vsn)
		protos[i].Attributes = []enr.Entry{s.currentEthEntry()}
		protos[i].DialCandidates = s.dialCandiates
	}
	if s.lesServer != nil {
		protos = append(protos, s.lesServer.Protocols()...)
	}
	return protos
}

// Start implements node.Service, starting all internal goroutines needed by the
// Ethereum protocol implementation.
func (s *Ethereum) Start(srvr *p2p.Server) error {
	s.startEthEntryUpdate(srvr.LocalNode())

	// Start the bloom bits servicing goroutines
	s.startBloomHandlers(params.BloomBitsBlocks)

	// Start the RPC service
	s.netRPCService = ethapi.NewPublicNetAPI(srvr, s.NetVersion())

	// Figure out a max peers count based on the server limits
	maxPeers := srvr.MaxPeers
	if s.config.LightServ > 0 {
		if s.config.LightPeers >= srvr.MaxPeers {
			return fmt.Errorf("invalid peer config: light peer count (%d) >= total peer count (%d)", s.config.LightPeers, srvr.MaxPeers)
		}
		maxPeers -= s.config.LightPeers
	}
	// Start the networking layer and the light server if requested
	s.protocolManager.p2pServer = srvr
	s.protocolManager.Start(maxPeers)
	if s.lesServer != nil {
		s.lesServer.Start(srvr)
	}

	// Trigger initial epoch (epoch 1) solution submission once ProtocolManager is started
	// and chain genesis is available.
	if err := s.TriggerInitialEpoch(); err != nil {
		log.Warn("Failed to trigger initial PoW election epoch", "err", err)
	}
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// Ethereum protocol.
func (s *Ethereum) Stop() error {
	s.lock.RLock()
	worker := s.powWorker
	s.lock.RUnlock()
	if worker != nil {
		worker.Stop()
	}

	s.bloomIndexer.Close()
	s.blockchain.Stop()
	s.engine.Close()
	s.protocolManager.Stop()
	if s.lesServer != nil {
		s.lesServer.Stop()
	}
	s.txPool.Stop()
	s.miner.Stop()
	s.eventMux.Stop()

	s.chainDb.Close()
	close(s.shutdownChan)
	return nil
}

// BoundaryCoordinator 返回 protocolManager 关联的 boundary 协调器 (Phase 3B)。
func (s *Ethereum) BoundaryCoordinator() *BoundaryCoordinator {
	if s.protocolManager != nil {
		return s.protocolManager.BoundaryCoordinator()
	}
	return nil
}

// BootstrapActiveCommittee 从指定的稳定 NodeID 列表初始化首个 active descriptor 并同步路由 (Phase 3B)。
func (s *Ethereum) BootstrapActiveCommittee(membersInIndexOrder []NodeID, threshold int, epoch uint64, activateAt uint64) error {
	if s.protocolManager == nil {
		return errors.New("nil ProtocolManager")
	}
	return s.protocolManager.BootstrapActiveCommittee(membersInIndexOrder, threshold, epoch, activateAt)
}

// SetActiveCommitteeProvider 设置初始 active descriptor 发现提供者接口 (Phase 3B)。
func (s *Ethereum) SetActiveCommitteeProvider(provider ActiveCommitteeProvider) {
	if s.protocolManager != nil {
		s.protocolManager.SetActiveCommitteeProvider(provider)
	}
}

// SetPeerNodeIDResolver 设置 peer 到 NodeID 的解析钩子 (Phase 3B)。
func (s *Ethereum) SetPeerNodeIDResolver(resolver func(p *peer) (NodeID, bool)) {
	if s.protocolManager != nil {
		s.protocolManager.SetPeerNodeIDResolver(resolver)
	}
}

// RegisterPeerNodeID 注册 peerID 到 NodeID 映射 (Phase 3B)。
func (s *Ethereum) RegisterPeerNodeID(peerID string, nodeID NodeID) {
	if s.protocolManager != nil {
		s.protocolManager.RegisterPeerNodeID(peerID, nodeID)
	}
}

// UpdateCommitteeRoutes 更新 active 与 pending 委员会路由映射 (Phase 3B)。
func (s *Ethereum) UpdateCommitteeRoutes(active, pending *CommitteeDescriptor) {
	if s.protocolManager != nil {
		s.protocolManager.UpdateCommitteeRoutes(active, pending)
	}
}

// PowWorker 返回关联的 PoW 选举解题 worker。
func (s *Ethereum) PowWorker() *PowWorker {
	return s.powWorker
}

// SetPowWorker 注入或更新 PoW 选举解题 worker。若启用，自动挂载至 BoundaryCoordinator。
func (s *Ethereum) SetPowWorker(w *PowWorker) {
	s.lock.Lock()
	old := s.powWorker
	s.powWorker = w
	s.lock.Unlock()

	if old != nil {
		old.Stop()
	}

	if w != nil {
		if w.KeyReadyFunc() == nil {
			w.SetKeyReadyFunc(chameleon.SystemPublicKeyReady)
		}
		if w.Enabled() && s.protocolManager != nil && s.protocolManager.BoundaryCoordinator() != nil {
			s.protocolManager.BoundaryCoordinator().AddHandler(w.BoundaryHandler())
			if s.blockchain != nil && s.txPool != nil {
				if err := s.triggerInitialEpoch(w); err != nil {
					log.Warn("Failed to trigger initial epoch on SetPowWorker", "err", err)
				}
			}
		}
	}
}

// InitialEpochStatus reports the local candidate's orchestration gate and submission state.
func (s *Ethereum) InitialEpochStatus() map[string]interface{} {
	s.lock.RLock()
	worker := s.powWorker
	s.lock.RUnlock()
	if worker == nil {
		return map[string]interface{}{"enabled": false}
	}
	status := worker.InitialEpochStatus()
	status["keyReady"] = worker.IsKeyReady()
	return status
}

// ReleaseInitialEpoch opens the optional epoch 1 gate without restarting the node.
func (s *Ethereum) ReleaseInitialEpoch() (map[string]interface{}, error) {
	s.lock.RLock()
	worker := s.powWorker
	s.lock.RUnlock()
	if worker == nil || !worker.Enabled() {
		return nil, ErrWorkerDisabled
	}
	if !worker.IsKeyReady() {
		return nil, ErrKeyNotReady
	}
	if s.protocolManager == nil || s.protocolManager.BoundaryCoordinator() == nil || s.blockchain == nil {
		return nil, errors.New("election coordinator is not initialized")
	}
	cfg := s.protocolManager.BoundaryCoordinator().Config()
	if cfg.EpochLength == 0 || s.blockchain.CurrentBlock().NumberU64() >= cfg.InitialBoundary+cfg.EpochLength {
		return nil, fmt.Errorf("initial epoch election window has closed at block %d", s.blockchain.CurrentBlock().NumberU64())
	}
	worker.ReleaseInitialEpoch()
	if err := s.triggerInitialEpoch(worker); err != nil {
		return s.InitialEpochStatus(), err
	}
	return s.InitialEpochStatus(), nil
}

// TriggerInitialEpoch triggers the initial epoch (epoch 1) PoW solution submission.
// It derives tau using cfg.InitialBoundary's canonical block hash (genesis if boundary=0) and epoch=1.
// If the NITCH system public key is ready, it submits immediately to the txpool.
// If the key is not yet ready (DKG in progress), it starts an asynchronous waiter that
// polls chameleon.SystemPublicKeyReady() and submits once ready, without blocking Ethereum.Start.
// Duplicate submissions are strictly prevented. Safe no-op if disabled or no candidates.
func (s *Ethereum) TriggerInitialEpoch() error {
	s.lock.RLock()
	worker := s.powWorker
	s.lock.RUnlock()
	return s.triggerInitialEpoch(worker)
}

func (s *Ethereum) triggerInitialEpoch(worker *PowWorker) error {
	if worker == nil || !worker.Enabled() {
		return nil
	}
	if s.blockchain == nil {
		return errors.New("blockchain not initialized")
	}

	// If candidate was not populated during New() (e.g., nodeID/KeyManager was initialized later in main.go),
	// attempt candidate discovery from environment now.
	if worker.CandidateCount() == 0 {
		if cand, ok, err := CandidateFromEnv(nil); err != nil {
			log.Warn("Failed to configure candidate from env for PoW worker on start", "err", err)
		} else if ok && cand != nil {
			if err := worker.AddCandidate(*cand); err != nil {
				log.Warn("Failed to add candidate to PoW worker on start", "candidate", cand.CandidateID, "err", err)
			} else {
				log.Info("Registered candidate for PoW worker from env on start", "candidate", cand.CandidateID, "address", cand.Address.Hex())
			}
		}
	}

	if worker.CandidateCount() == 0 {
		log.Debug("PoW worker enabled but no candidates registered, skipping initial epoch submission")
		return nil
	}

	var initialBoundary uint64
	if s.protocolManager != nil && s.protocolManager.BoundaryCoordinator() != nil {
		initialBoundary = s.protocolManager.BoundaryCoordinator().Config().InitialBoundary
	}

	initialBlock := s.blockchain.GetBlockByNumber(initialBoundary)
	if initialBlock == nil && initialBoundary == 0 {
		initialBlock = s.blockchain.Genesis()
	}
	if initialBlock == nil {
		return fmt.Errorf("canonical block at initial boundary %d not available", initialBoundary)
	}

	if worker.HasSubmittedInitialEpoch() {
		return nil
	}

	if !worker.IsKeyReady() || worker.InitialReleasePending() {
		log.Info("Initial PoW epoch awaiting key or orchestration release; starting asynchronous waiter",
			"boundary", initialBoundary, "blockHash", initialBlock.Hash().Hex())
		worker.StartInitialEpochWaiter(initialBoundary, initialBlock.Hash(), 500*time.Millisecond)
		return nil
	}

	txs, err := worker.TriggerInitialEpoch(initialBoundary, initialBlock.Hash())
	if err != nil {
		return fmt.Errorf("failed to trigger initial epoch: %w", err)
	}
	if len(txs) > 0 {
		log.Info("Initial epoch solution transaction(s) submitted to txpool",
			"epoch", 1, "count", len(txs), "boundary", initialBoundary, "blockHash", initialBlock.Hash().Hex())
	}
	return nil
}
