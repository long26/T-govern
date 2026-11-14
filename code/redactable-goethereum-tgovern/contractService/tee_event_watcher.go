package contractService

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/storage"
)

type TEEEventKind uint8

const (
	TEEEventRotationPreparationRequested TEEEventKind = iota + 1
	TEEEventVRFSubmitted
	TEEEventActiveSetRotated
	TEEEventRegistered
	TEEEventRegistrationEvidenceRecorded
	TEEEventExitPending
	TEEEventExited
	TEEEventRevoked
	TEEEventEraseProofDeadlineSet
	TEEEventActiveSetCheckpointed
	TEEEventStakePending
	TEEEventPendingCancelled
	TEEEventPkCHSet
	TEEEventRedactAuthorized
	TEEEventRedactRequestPayloadRecorded
	TEEEventRedactExecuted
	TEEEventRedactExpired
	TEEEventRedactRejected
	TEEEventCommitteePubKeyRegistered
	TEEEventTrustedMREnclaveSet
)

type TEEEvent struct {
	Kind        TEEEventKind
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	LogIndex    uint
	Removed     bool

	RotationRequested            *contracts.ContractsTEERotationPreparationRequested
	VRFSubmitted                 *contracts.ContractsTEEVRFSubmitted
	ActiveSetCheckpointed        *contracts.ContractsActiveSetCheckpointed
	ActiveSetRotated             *contracts.ContractsActiveSetRotated
	StakePending                 *contracts.ContractsTEEStakePending
	PendingCancelled             *contracts.ContractsPendingTEECancelled
	Registered                   *contracts.ContractsTEERegistered
	RegistrationEvidenceRecorded *contracts.ContractsTEERegistrationEvidenceRecorded
	PkCHSet                      *contracts.ContractsPkCHSet
	RedactAuthorized             *contracts.ContractsRedactAuthorized
	RedactRequestPayloadRecorded *contracts.ContractsRedactRequestPayloadRecorded
	RedactExecuted               *contracts.ContractsRedactExecuted
	RedactExpired                *contracts.ContractsRedactExpired
	RedactRejected               *contracts.ContractsRedactRejected
	ExitPending                  *contracts.ContractsTEEExitPending
	Exited                       *contracts.ContractsTEEExited
	Revoked                      *contracts.ContractsTEERevoked
	EraseProofDeadlineSet        *contracts.ContractsTEEEraseProofDeadlineSet
	CommitteePubKeyRegistered    *contracts.ContractsCommitteePubKeyRegistered
	TrustedMREnclaveSet          *contracts.ContractsTrustedMREnclaveSet
}

type TEEEventWatcherConfig struct {
	FromBlock     uint64
	PollInterval  time.Duration
	Confirmations uint64
	BatchSize     uint64
	CursorStore   TEEEventCursorStore
	CursorKey     string

	// HeadCh optionally drives scans from local chain head events instead of
	// the wall-clock ticker. Scans stay cursor-based and idempotent, so the
	// ticker below keeps running as a fallback: a dropped or delayed head
	// notification only postpones discovery to the next tick, never loses
	// events. A nil channel disables head-driven mode entirely.
	HeadCh <-chan struct{}
}

type TEEEventCursorStore interface {
	Get(key string) string
	Save(key string, value string) error
}

type storageCursorStore struct{}

func (storageCursorStore) Get(key string) string {
	return storage.Get(key)
}

func (storageCursorStore) Save(key string, value string) error {
	return storage.Save(key, value)
}

type teeEventParser func(types.Log) (TEEEvent, error)

type TEEEventWatcher struct {
	service *ContractService
	cfg     TEEEventWatcherConfig

	topics  []common.Hash
	parsers map[common.Hash]teeEventParser

	cursorKey     string
	lastScanned   uint64
	seen          map[teeLogKey]struct{}
	seenResetSize int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type teeLogKey struct {
	BlockHash common.Hash
	TxHash    common.Hash
	Index     uint
}

func NewTEEEventWatcher(service *ContractService, cfg TEEEventWatcherConfig) (*TEEEventWatcher, error) {
	if service == nil {
		return nil, fmt.Errorf("contract service is nil")
	}
	if service.eliClient == nil {
		return nil, fmt.Errorf("contract service backend is nil")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 1000
	}
	if cfg.CursorStore == nil {
		cfg.CursorStore = storageCursorStore{}
	}

	parsers, topics, err := buildTEEEventParsers(service.contract)
	if err != nil {
		return nil, err
	}
	if cfg.CursorKey == "" {
		cfg.CursorKey = defaultTEEEventCursorKey(context.Background(), service)
	}

	lastScanned := uint64(0)
	if cfg.FromBlock > 0 {
		lastScanned = cfg.FromBlock - 1
	}
	if stored := strings.TrimSpace(cfg.CursorStore.Get(cfg.CursorKey)); stored != "" {
		cursor, err := strconv.ParseUint(stored, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid tee event cursor %q: %v", stored, err)
		}
		if cursor > lastScanned {
			lastScanned = cursor
		}
	}

	return &TEEEventWatcher{
		service:       service,
		cfg:           cfg,
		topics:        topics,
		parsers:       parsers,
		cursorKey:     cfg.CursorKey,
		lastScanned:   lastScanned,
		seen:          make(map[teeLogKey]struct{}),
		seenResetSize: 4096,
	}, nil
}

func (w *TEEEventWatcher) Start(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	w.ctx, w.cancel = context.WithCancel(parent)
	w.wg.Add(1)
	go w.loop()
}

func (w *TEEEventWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

func (w *TEEEventWatcher) loop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := w.scanOnce(w.ctx); err != nil && w.ctx.Err() == nil {
			log.Warn("TEE event watcher scan failed", "err", err)
		}
		select {
		case <-w.cfg.HeadCh:
			// Coalesce queued head notifications so one incremental scan
			// covers the whole backlog.
		drain:
			for {
				select {
				case <-w.cfg.HeadCh:
				default:
					break drain
				}
			}
		case <-ticker.C:
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *TEEEventWatcher) scanOnce(ctx context.Context) error {
	header, err := w.service.eliClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	if header == nil || header.Number == nil {
		return fmt.Errorf("latest header is nil")
	}
	head := header.Number.Uint64()
	if head < w.cfg.Confirmations {
		return nil
	}
	safeHead := head - w.cfg.Confirmations
	if safeHead <= w.lastScanned {
		return nil
	}

	toBlock := w.lastScanned + w.cfg.BatchSize
	if toBlock > safeHead {
		toBlock = safeHead
	}
	query := ethereum.FilterQuery{
		Addresses: []common.Address{w.service.contractAddress},
		Topics:    [][]common.Hash{w.topics},
		FromBlock: new(big.Int).SetUint64(w.lastScanned + 1),
		ToBlock:   new(big.Int).SetUint64(toBlock),
	}
	logs, err := w.service.eliClient.FilterLogs(ctx, query)
	if err != nil {
		return err
	}
	for _, rawLog := range logs {
		if len(rawLog.Topics) == 0 {
			continue
		}
		parser := w.parsers[rawLog.Topics[0]]
		if parser == nil {
			continue
		}
		key := teeLogKey{BlockHash: rawLog.BlockHash, TxHash: rawLog.TxHash, Index: rawLog.Index}
		if _, ok := w.seen[key]; ok {
			continue
		}
		ev, err := parser(rawLog)
		if err != nil {
			return err
		}
		ev.BlockNumber = rawLog.BlockNumber
		ev.BlockHash = rawLog.BlockHash
		ev.TxHash = rawLog.TxHash
		ev.LogIndex = rawLog.Index
		ev.Removed = rawLog.Removed
		w.service.teeEventFeed.Send(ev)
		w.seen[key] = struct{}{}
		if len(w.seen) > w.seenResetSize {
			w.seen = make(map[teeLogKey]struct{})
		}
	}
	if err := w.cfg.CursorStore.Save(w.cursorKey, strconv.FormatUint(toBlock, 10)); err != nil {
		return err
	}
	w.lastScanned = toBlock
	return nil
}

func defaultTEEEventCursorKey(ctx context.Context, service *ContractService) string {
	chainID, err := service.ChainID(ctx)
	if err != nil || chainID == nil {
		chainID = big.NewInt(0)
	}
	return fmt.Sprintf("teeEventCursor:%s:%s", chainID.String(), service.contractAddress.Hex())
}

func buildTEEEventParsers(contract *contracts.Contracts) (map[common.Hash]teeEventParser, []common.Hash, error) {
	parsed, err := abi.JSON(strings.NewReader(contracts.ContractsABI))
	if err != nil {
		return nil, nil, err
	}
	add := func(parsers map[common.Hash]teeEventParser, topics *[]common.Hash, name string, parser teeEventParser) error {
		eventDef, ok := parsed.Events[name]
		if !ok {
			return fmt.Errorf("PermissionManagerV3 ABI missing event %s", name)
		}
		topic := eventDef.ID()
		parsers[topic] = parser
		*topics = append(*topics, topic)
		return nil
	}

	parsers := make(map[common.Hash]teeEventParser)
	var topics []common.Hash
	if err := add(parsers, &topics, "TEERotationPreparationRequested", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEERotationPreparationRequested(l)
		return TEEEvent{Kind: TEEEventRotationPreparationRequested, RotationRequested: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEEVRFSubmitted", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEEVRFSubmitted(l)
		return TEEEvent{Kind: TEEEventVRFSubmitted, VRFSubmitted: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "ActiveSetCheckpointed", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseActiveSetCheckpointed(l)
		return TEEEvent{Kind: TEEEventActiveSetCheckpointed, ActiveSetCheckpointed: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "ActiveSetRotated", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseActiveSetRotated(l)
		return TEEEvent{Kind: TEEEventActiveSetRotated, ActiveSetRotated: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEEStakePending", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEEStakePending(l)
		return TEEEvent{Kind: TEEEventStakePending, StakePending: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "PendingTEECancelled", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParsePendingTEECancelled(l)
		return TEEEvent{Kind: TEEEventPendingCancelled, PendingCancelled: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEERegistered", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEERegistered(l)
		return TEEEvent{Kind: TEEEventRegistered, Registered: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEERegistrationEvidenceRecorded", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEERegistrationEvidenceRecorded(l)
		return TEEEvent{Kind: TEEEventRegistrationEvidenceRecorded, RegistrationEvidenceRecorded: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "PkCHSet", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParsePkCHSet(l)
		return TEEEvent{Kind: TEEEventPkCHSet, PkCHSet: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "RedactAuthorized", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseRedactAuthorized(l)
		return TEEEvent{Kind: TEEEventRedactAuthorized, RedactAuthorized: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "RedactRequestPayloadRecorded", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseRedactRequestPayloadRecorded(l)
		return TEEEvent{Kind: TEEEventRedactRequestPayloadRecorded, RedactRequestPayloadRecorded: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "RedactExecuted", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseRedactExecuted(l)
		return TEEEvent{Kind: TEEEventRedactExecuted, RedactExecuted: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "RedactExpired", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseRedactExpired(l)
		return TEEEvent{Kind: TEEEventRedactExpired, RedactExpired: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "RedactRejected", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseRedactRejected(l)
		return TEEEvent{Kind: TEEEventRedactRejected, RedactRejected: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEEExitPending", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEEExitPending(l)
		return TEEEvent{Kind: TEEEventExitPending, ExitPending: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEEExited", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEEExited(l)
		return TEEEvent{Kind: TEEEventExited, Exited: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEERevoked", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEERevoked(l)
		return TEEEvent{Kind: TEEEventRevoked, Revoked: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TEEEraseProofDeadlineSet", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTEEEraseProofDeadlineSet(l)
		return TEEEvent{Kind: TEEEventEraseProofDeadlineSet, EraseProofDeadlineSet: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "CommitteePubKeyRegistered", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseCommitteePubKeyRegistered(l)
		return TEEEvent{Kind: TEEEventCommitteePubKeyRegistered, CommitteePubKeyRegistered: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	if err := add(parsers, &topics, "TrustedMREnclaveSet", func(l types.Log) (TEEEvent, error) {
		ev, err := contract.ParseTrustedMREnclaveSet(l)
		return TEEEvent{Kind: TEEEventTrustedMREnclaveSet, TrustedMREnclaveSet: ev}, err
	}); err != nil {
		return nil, nil, err
	}
	return parsers, topics, nil
}
