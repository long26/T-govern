package teehost

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contractService"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
	"github.com/ethereum/go-ethereum/event"
)

type fakeContractBackend struct {
	feed             event.Feed
	address          common.Address
	chainID          *big.Int
	submitted        int
	setPkCHCalls     int
	executeCalls     int
	eraseCalls       int
	pkCH             []byte
	pkCHSignature    []byte
	activeSetInfo    *contractService.ActiveSetInfo
	activeSetByEpoch map[string]*contractService.ActiveSetInfo
	teeInfo          *contractService.TEEInfo
	enclavePKs       map[[32]byte][]byte
	systemParams     *contractService.SystemParams
	lastExecutedReq  [32]byte
	lastExecutedPi   []byte
	lastEraseEpoch   *big.Int
	submitErrs       []error
	receipt          *types.Receipt
	receiptErr       error
	receiptWaitCalls int
	executeTx        string
	executeErr       error
}

func (c *fakeContractBackend) SubscribeTEEEvents(ch chan<- contractService.TEEEvent) event.Subscription {
	return c.feed.Subscribe(ch)
}

func (c *fakeContractBackend) SubmitTEESelectionVRF(epoch *big.Int, hardwareID [32]byte, vrfResult [32]byte, vrfProof []byte) (string, error) {
	c.submitted++
	if len(c.submitErrs) != 0 {
		err := c.submitErrs[0]
		c.submitErrs = c.submitErrs[1:]
		if err != nil {
			return "", err
		}
	}
	return common.Hash{0x01}.Hex(), nil
}

func (c *fakeContractBackend) GetActiveTEEs() ([][32]byte, error) {
	if c.activeSetInfo != nil {
		return append([][32]byte(nil), c.activeSetInfo.HardwareIDs...), nil
	}
	return [][32]byte{hostTestHash("hardware")}, nil
}

func (c *fakeContractBackend) GetActiveSetInfo(epoch *big.Int) (*contractService.ActiveSetInfo, error) {
	if c.activeSetByEpoch != nil && epoch != nil {
		if info := c.activeSetByEpoch[epoch.String()]; info != nil {
			return cloneHostTestActiveSetInfo(info), nil
		}
	}
	if c.activeSetInfo != nil {
		return c.activeSetInfo, nil
	}
	return &contractService.ActiveSetInfo{
		Epoch:           new(big.Int).Set(epoch),
		HardwareIDs:     [][32]byte{hostTestHash("hardware")},
		ActiveThreshold: big.NewInt(1),
		ActiveLimit:     big.NewInt(1),
	}, nil
}

func (c *fakeContractBackend) GetTEEInfo(hardwareID [32]byte) (*contractService.TEEInfo, error) {
	if c.teeInfo != nil {
		return c.teeInfo, nil
	}
	return &contractService.TEEInfo{
		SigPubKey: hostTestHash("sig-pubkey"),
		Owner:     common.HexToAddress("0x1111222233334444555566667777888899990000"),
		Deposit:   big.NewInt(1),
		IsActive:  true,
	}, nil
}

func (c *fakeContractBackend) GetTEEEnclavePK(sigPubKey [32]byte) ([]byte, error) {
	if c.enclavePKs != nil {
		epk := c.enclavePKs[sigPubKey]
		if len(epk) == 0 {
			return nil, fmt.Errorf("missing enclave public key for %x", sigPubKey)
		}
		return append([]byte(nil), epk...), nil
	}
	return hostTestEPK(sigPubKey), nil
}

func (c *fakeContractBackend) GetSystemParams() (*contractService.SystemParams, error) {
	if c.systemParams != nil {
		return c.systemParams, nil
	}
	return &contractService.SystemParams{
		CommitteeVersion:   big.NewInt(1),
		Epoch:              big.NewInt(1),
		ActiveThreshold:    big.NewInt(1),
		ActiveLimit:        big.NewInt(1),
		CurrentPkCHVersion: big.NewInt(0),
	}, nil
}

func (c *fakeContractBackend) VerifyRedactAuthorization(context.Context, contractService.RedactReqFields, [32]byte, [32]byte, *big.Int, *big.Int, [32]byte, []byte) error {
	return nil
}

func (c *fakeContractBackend) GetPkCH() ([]byte, error) {
	return append([]byte(nil), c.pkCH...), nil
}

func (c *fakeContractBackend) SetPkCH(pkCH []byte, signature []byte) (string, error) {
	c.setPkCHCalls++
	c.pkCH = append([]byte(nil), pkCH...)
	c.pkCHSignature = append([]byte(nil), signature...)
	return common.Hash{0x02}.Hex(), nil
}

func (c *fakeContractBackend) ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	payload := append(reqID[:], dReq[:]...)
	payload = append(payload, activeSetHash[:]...)
	payload = append(payload, piReqA...)
	return crypto.Keccak256Hash(payload), nil
}

func (c *fakeContractBackend) ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (string, error) {
	c.executeCalls++
	c.lastExecutedReq = reqID
	c.lastExecutedPi = append([]byte(nil), piReqA...)
	if c.executeErr != nil {
		return "", c.executeErr
	}
	if c.executeTx != "" {
		return c.executeTx, nil
	}
	return common.Hash{0x03}.Hex(), nil
}

func (c *fakeContractBackend) WaitTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	c.receiptWaitCalls++
	if c.receiptErr != nil {
		return nil, c.receiptErr
	}
	if c.receipt != nil {
		return c.receipt, nil
	}
	return &types.Receipt{Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(1)}, nil
}

func (c *fakeContractBackend) SubmitEraseProof(hardwareID [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (string, error) {
	c.eraseCalls++
	c.lastEraseEpoch = new(big.Int).Set(epoch)
	return common.Hash{0x04}.Hex(), nil
}

func (c *fakeContractBackend) ContractAddress() common.Address {
	return c.address
}

func (c *fakeContractBackend) ChainID(ctx context.Context) (*big.Int, error) {
	if c.chainID == nil {
		return big.NewInt(1), nil
	}
	return new(big.Int).Set(c.chainID), nil
}

type fakeRedactionExecutor struct {
	calls int
	last  RedactionExecutionRequest
}

func (e *fakeRedactionExecutor) ExecuteTEEClusterRedactionFromMaterial(ctx context.Context, req RedactionExecutionRequest) (map[string]interface{}, error) {
	e.calls++
	e.last = req
	return map[string]interface{}{"Status": "executed"}, nil
}

type blockingRedactionExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func newBlockingRedactionExecutor() *blockingRedactionExecutor {
	return &blockingRedactionExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (e *blockingRedactionExecutor) ExecuteTEEClusterRedactionFromMaterial(ctx context.Context, req RedactionExecutionRequest) (map[string]interface{}, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	e.once.Do(func() { close(e.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
		return map[string]interface{}{"Status": "executed"}, nil
	}
}

func (e *blockingRedactionExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type fakeTEEBackend struct {
	identity TEEIdentity
	calls    int
	eval     func(context.Context, VRFRequest) (VRFResponse, error)
}

func (t *fakeTEEBackend) Identity(ctx context.Context) (TEEIdentity, error) {
	return t.identity, nil
}

func (t *fakeTEEBackend) EvalVRF(ctx context.Context, req VRFRequest) (VRFResponse, error) {
	t.calls++
	if t.eval != nil {
		return t.eval(ctx, req)
	}
	return VRFResponse{VRFResult: hostTestHash("vrf-result"), VRFProof: make([]byte, 65)}, nil
}

func (t *fakeTEEBackend) Close() {}

func TestHostSkipsDuplicateRotationEvents(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf-pubkey")}
	contract := &fakeContractBackend{address: common.HexToAddress("0x1111222233334444555566667777888899990000"), chainID: big.NewInt(1337)}
	tee := &fakeTEEBackend{identity: identity}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity

	ev := contractService.TEEEvent{
		Kind: contractService.TEEEventRotationPreparationRequested,
		RotationRequested: &contracts.ContractsTEERotationPreparationRequested{
			NextEpoch: big.NewInt(1),
			Seed:      hostTestHash("seed"),
		},
	}
	if err := host.handleEvent(context.Background(), ev); err != nil {
		t.Fatalf("handle first event: %v", err)
	}
	if err := host.handleEvent(context.Background(), ev); err != nil {
		t.Fatalf("handle duplicate event: %v", err)
	}
	if contract.submitted != 1 {
		t.Fatalf("submit count = %d, want 1", contract.submitted)
	}
	if tee.calls != 1 {
		t.Fatalf("tee eval calls = %d, want 1", tee.calls)
	}
}

func TestHostMarksSubmittedVRFEvents(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf-pubkey")}
	contract := &fakeContractBackend{address: common.HexToAddress("0x1111222233334444555566667777888899990000"), chainID: big.NewInt(1337)}
	tee := &fakeTEEBackend{identity: identity}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventVRFSubmitted,
		VRFSubmitted: &contracts.ContractsTEEVRFSubmitted{
			Epoch:      big.NewInt(3),
			HardwareId: identity.HardwareID,
		},
	}); err != nil {
		t.Fatalf("handle submitted event: %v", err)
	}
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventRotationPreparationRequested,
		RotationRequested: &contracts.ContractsTEERotationPreparationRequested{
			NextEpoch: big.NewInt(3),
			Seed:      hostTestHash("seed"),
		},
	}); err != nil {
		t.Fatalf("handle rotation event: %v", err)
	}
	if contract.submitted != 0 {
		t.Fatalf("submit count = %d, want 0", contract.submitted)
	}
}

func TestHostRotationRequestUsesPaddedContractAddressForVRF(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf-pubkey")}
	contractAddr := common.HexToAddress("0x1111222233334444555566667777888899990000")
	tee := &fakeTEEBackend{
		identity: identity,
		eval: func(ctx context.Context, req VRFRequest) (VRFResponse, error) {
			if len(req.ContractAddress) != 32 {
				t.Fatalf("contract address length = %d, want 32", len(req.ContractAddress))
			}
			if !bytes.Equal(req.ContractAddress, common.LeftPadBytes(contractAddr.Bytes(), 32)) {
				t.Fatalf("contract address = %x, want %x", req.ContractAddress, common.LeftPadBytes(contractAddr.Bytes(), 32))
			}
			return VRFResponse{VRFResult: hostTestHash("vrf-result"), VRFProof: make([]byte, 65)}, nil
		},
	}
	contract := &fakeContractBackend{
		address: contractAddr,
		chainID: big.NewInt(1337),
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	if err := host.handleRotationRequested(context.Background(), big.NewInt(8), hostTestHash("seed"), rotationTrigger{}); err != nil {
		t.Fatalf("handle rotation requested: %v", err)
	}
	if contract.submitted != 1 {
		t.Fatalf("vrf submissions = %d, want 1", contract.submitted)
	}
}

func TestHostRetriesSelectionVRFSubmissionFailure(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf-pubkey")}
	contract := &fakeContractBackend{
		address:    common.HexToAddress("0x1111222233334444555566667777888899990000"),
		chainID:    big.NewInt(1337),
		submitErrs: []error{fmt.Errorf("receipt status failed")},
	}
	tee := &fakeTEEBackend{identity: identity}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity

	if err := host.handleRotationRequested(context.Background(), big.NewInt(8), hostTestHash("seed"), rotationTrigger{}); err != nil {
		t.Fatalf("handle rotation requested: %v", err)
	}
	if contract.submitted != 2 {
		t.Fatalf("vrf submissions = %d, want 2", contract.submitted)
	}
	if !host.isSubmitted(big.NewInt(8)) {
		t.Fatalf("host did not keep epoch marked submitted after retry success")
	}
}

func TestHostIntegrationSubmitsVRFFromWatcherEvent(t *testing.T) {
	backend, contract, address, key, service := deployHostTestContract(t)
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newHostTestTransactor(key, nil), committeePubKeyForHostTest(committeeSecret)); err != nil {
		t.Fatalf("register committee key: %v", err)
	}
	backend.Commit()
	hardwareID := hostTestHash("host-tee")
	registerTEEForHostTest(t, backend, contract, key, committeeSecret, hardwareID)

	sigPubKey := hostAddressAsBytes32(crypto.PubkeyToAddress(key.PublicKey))
	identity := TEEIdentity{HardwareID: hardwareID, SigPubKey: sigPubKey, VRFPubKey: sigPubKey}
	tee := &fakeTEEBackend{
		identity: identity,
		eval: func(ctx context.Context, req VRFRequest) (VRFResponse, error) {
			digest, err := contract.ComputeVRFDigest(nil, new(big.Int).SetUint64(req.Epoch), req.HardwareID, req.VRFPubKey, req.Seed)
			if err != nil {
				return VRFResponse{}, err
			}
			proof, err := crypto.Sign(digest[:], key)
			if err != nil {
				return VRFResponse{}, err
			}
			return VRFResponse{VRFResult: deriveHostVRFResult(req.Seed, req.VRFPubKey, proof), VRFProof: proof}, nil
		},
	}

	host, err := NewHost(service, tee, HostConfig{EventBuffer: 16})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := host.Start(ctx); err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Stop()

	store := &hostMemoryCursorStore{values: make(map[string]string)}
	if err := service.StartTEEEventWatcher(ctx, contractService.TEEEventWatcherConfig{
		PollInterval:  10 * time.Millisecond,
		Confirmations: 0,
		BatchSize:     1000,
		CursorStore:   store,
		CursorKey:     "host-integration-cursor",
	}); err != nil {
		t.Fatalf("start watcher: %v", err)
	}
	defer service.StopTEEEventWatcher()

	advanceToRotationPreparationForHostTest(t, backend, contract)
	if _, err := contract.PrepareRotation(newHostTestTransactor(key, nil)); err != nil {
		t.Fatalf("prepare rotation: %v", err)
	}
	backend.Commit()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		backend.Commit()
		submission, err := contract.GetTEESelectionVRF(nil, big.NewInt(1), sigPubKey)
		if err != nil {
			t.Fatalf("get vrf submission: %v", err)
		}
		if submission.Exists {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("host did not submit VRF for contract %s", address.Hex())
}

func TestHostActiveSetStartsDistKGAndSetsPkCH(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		result: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("share")}),
		},
		extract: TEEExtractResult{
			GlobalPK:        []byte("pk-ch"),
			VerificationKey: []byte("vk"),
			PkCHSignature:   bytes.Repeat([]byte{0x5a}, 65),
		},
	}
	contract := &fakeContractBackend{
		address: common.HexToAddress("0x2222333344445555666677778888999900001111"),
		chainID: big.NewInt(1),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(1),
			ActiveSetHash:   hostTestHash("active-set"),
			HardwareIDs:     [][32]byte{identity.HardwareID},
			ActiveThreshold: big.NewInt(1),
			ActiveLimit:     big.NewInt(1),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	event := contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetCheckpointed,
		ActiveSetCheckpointed: &contracts.ContractsActiveSetCheckpointed{
			Epoch:         big.NewInt(1),
			ActiveSetHash: hostTestHash("active-set"),
			HardwareIds:   [][32]byte{identity.HardwareID},
		},
	}
	if err := host.handleEvent(context.Background(), event); err != nil {
		t.Fatalf("handle active set: %v", err)
	}
	if contract.setPkCHCalls != 1 {
		t.Fatalf("set pkCH calls = %d, want 1", contract.setPkCHCalls)
	}
	if string(contract.pkCH) != "pk-ch" {
		t.Fatalf("pkCH = %q, want pk-ch", string(contract.pkCH))
	}
	if !bytes.Equal(tee.extractReq.ChainID, big.NewInt(1).FillBytes(make([]byte, 32))) {
		t.Fatalf("extract chainID = %x", tee.extractReq.ChainID)
	}
	if !bytes.Equal(tee.extractReq.Contract, common.LeftPadBytes(contract.address.Bytes(), 32)) {
		t.Fatalf("extract contract = %x, want %x", tee.extractReq.Contract, common.LeftPadBytes(contract.address.Bytes(), 32))
	}
	owner := common.HexToAddress("0x1111222233334444555566667777888899990000")
	if !bytes.Equal(tee.extractReq.TEEOwner, common.LeftPadBytes(owner.Bytes(), 32)) {
		t.Fatalf("extract owner = %x, want %x", tee.extractReq.TEEOwner, common.LeftPadBytes(owner.Bytes(), 32))
	}
	if !bytes.Equal(contract.pkCHSignature, tee.extract.PkCHSignature) {
		t.Fatalf("pkCH signature = %x, want %x", contract.pkCHSignature, tee.extract.PkCHSignature)
	}
	if err := host.handleEvent(context.Background(), event); err != nil {
		t.Fatalf("handle duplicate active set: %v", err)
	}
	if contract.setPkCHCalls != 1 {
		t.Fatalf("duplicate set pkCH calls = %d, want 1", contract.setPkCHCalls)
	}
}

func TestHostRotatedActiveSetStartsDKRS(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("share")}),
		},
		extract: TEEExtractResult{VerificationKey: []byte("vk")},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     [][32]byte{identity.HardwareID},
			ActiveThreshold: big.NewInt(1),
			ActiveLimit:     big.NewInt(1),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.activeSet = [][32]byte{identity.HardwareID}
	seedHostPeerEPKs(t, host, identity.HardwareID)
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   [][32]byte{identity.HardwareID},
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 1 || tee.dkrsDealReq.NewNumParties != 1 {
		t.Fatalf("dkrs was not started with local active index: %#v", tee.dkrsDealReq)
	}
	if got := len(tee.peerEPKs); got != 1 {
		t.Fatalf("registered peer EPK count = %d, want 1", got)
	}
}

func TestHostRotatedActiveSetOldOnlyDealsDKRS(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), identity.HardwareID, hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("new-1"), hostTestHash("new-2"), hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 2 || tee.dkrsDealReq.NewNumParties != 3 {
		t.Fatalf("old-only DKRS dealer request = %#v, want dealer 2/new parties 3", tee.dkrsDealReq)
	}
	if network.dkrsSendCount() != 3 {
		t.Fatalf("old-only DKRS sends = %d, want 3", network.dkrsSendCount())
	}
	for _, index := range []uint64{1, 2, 3} {
		if len(tee.peerEPKs[index]) != 64 {
			t.Fatalf("missing registered EPK at index %d", index)
		}
	}
	if tee.dkrsExtractReq.MyIndex != 0 {
		t.Fatalf("old-only node unexpectedly extracted DKRS share: %#v", tee.dkrsExtractReq)
	}
	if host.isActive() {
		t.Fatal("old-only node remained active after rotation")
	}
}

func TestHostRotatedActiveSetNewOnlyReceivesDKRS(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("new-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), hostTestHash("old-2"), hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("new-1"), identity.HardwareID, hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		extract:        TEEExtractResult{VerificationKey: []byte("reshared-vk")},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = false
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 0 {
		t.Fatalf("new-only node unexpectedly created DKRS deal: %#v", tee.dkrsDealReq)
	}
	sessionID := host.dkrsSession.Config.SessionID
	for _, index := range []uint64{1, 2, 3} {
		if len(tee.peerEPKs[index]) != 64 {
			t.Fatalf("missing registered EPK at index %d", index)
		}
	}
	for i := 1; i <= 3; i++ {
		deal := DKRSProtocolMessage{SessionID: sessionID, Deal: &DKRSDealMessage{
			DealerIndex:   uint64(i),
			OldThreshold:  1,
			OldNumParties: uint64(len(oldSet)),
			NewThreshold:  1,
			NewNumParties: 3,
			CommitmentsA:  bytes.Repeat([]byte{byte(i)}, 96),
			CommitmentB0:  bytes.Repeat([]byte{byte(i + 10)}, 192),
		}}
		if err := host.HandleDKRS(context.Background(), deal, network); err != nil {
			t.Fatalf("handle DKRS deal %d: %v", i, err)
		}
		share := DKRSProtocolMessage{SessionID: sessionID, Share: &DKRSShareMessage{FromIndex: uint64(i), ToIndex: 2, Payload: []byte{byte(i), 0xcc}}}
		if err := host.HandleDKRS(context.Background(), share, network); err != nil {
			t.Fatalf("handle DKRS share %d: %v", i, err)
		}
	}
	if tee.dkrsExtractReq.MyIndex != 2 {
		t.Fatalf("new-only DKRS extract MyIndex = %d, want 2", tee.dkrsExtractReq.MyIndex)
	}
	if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("new-only DKRS qual = %v, want %v", got, want)
	}
	if !host.isActive() {
		t.Fatal("new-only node did not become active after rotation")
	}
}

func TestHostSkipsDKRSDealerWhenOldEpochShareMissingFreshness(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("overlap-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("overlap-1"), identity.HardwareID, hostTestHash("overlap-3")}
	newSet := [][32]byte{hostTestHash("new-1"), identity.HardwareID, hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	host.dkrsSession = &DKRSSessionState{
		Config: DKRSProtocolConfig{
			ReceiverIndex: 2,
			SessionID:     hostTestHash("epoch-1-dkrs-session"),
			NewEpoch:      big.NewInt(1),
			NewThreshold:  1,
			NewNumParties: 3,
			OldQual:       []int{1, 2, 3},
		},
		Deals:         make(map[int]*DKRSDealMessage),
		Shares:        make(map[int]*DKRSShareMessage),
		InvalidShares: make(map[int]struct{}),
	}
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)

	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 0 {
		t.Fatalf("DKRS dealer deal was created despite missing fresh share: %#v", tee.dkrsDealReq)
	}
	if host.dkrsSession == nil || host.dkrsSession.Config.DealerIndex != 0 || host.dkrsSession.Config.ReceiverIndex != 2 {
		t.Fatalf("freshness guard should leave receiver-only DKRS session, got %#v", host.dkrsSession)
	}
	if network.dkrsSendCount() != 0 {
		t.Fatalf("freshness guard sent dealer shares = %d, want 0", network.dkrsSendCount())
	}
}

func TestHostDKRSExtractsOnCanonicalQualPrefix(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("new-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), hostTestHash("old-2"), hostTestHash("old-3"), hostTestHash("old-4")}
	newSet := [][32]byte{hostTestHash("new-1"), identity.HardwareID, hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		extract:        TEEExtractResult{VerificationKey: []byte("reshared-vk")},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = false
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	sessionID := host.dkrsSession.Config.SessionID
	handleDealerInput := func(i int) {
		t.Helper()
		deal := DKRSProtocolMessage{SessionID: sessionID, Deal: &DKRSDealMessage{
			DealerIndex:   uint64(i),
			OldThreshold:  1,
			OldNumParties: uint64(len(oldSet)),
			NewThreshold:  1,
			NewNumParties: 3,
			CommitmentsA:  bytes.Repeat([]byte{byte(i)}, 96),
			CommitmentB0:  bytes.Repeat([]byte{byte(i + 10)}, 192),
		}}
		if err := host.HandleDKRS(context.Background(), deal, network); err != nil {
			t.Fatalf("handle DKRS deal %d: %v", i, err)
		}
		share := DKRSProtocolMessage{SessionID: sessionID, Share: &DKRSShareMessage{FromIndex: uint64(i), ToIndex: 2, Payload: []byte{byte(i), 0xcc}}}
		if err := host.HandleDKRS(context.Background(), share, network); err != nil {
			t.Fatalf("handle DKRS share %d: %v", i, err)
		}
	}
	// ActiveThreshold=2 -> fixed dealer prefix = the first 3 entries of OldQual.
	for _, i := range []int{1, 2} {
		handleDealerInput(i)
	}
	if tee.dkrsExtractReq.MyIndex != 0 {
		t.Fatalf("DKRS extracted before the fixed dealer prefix was complete: %#v", tee.dkrsExtractReq)
	}
	// Dealer 4 sits outside the fixed prefix: extraction must not wait for it.
	handleDealerInput(3)
	if tee.dkrsExtractReq.MyIndex == 0 {
		t.Fatalf("DKRS did not extract once the fixed dealer prefix was complete")
	}
	if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual = %v, want fixed prefix %v", got, want)
	}
	// A late non-prefix input must not change the extraction set.
	handleDealerInput(4)
	if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DKRS qual changed after non-prefix input: %v, want %v", got, want)
	}
}

func TestHostRotatedActiveSetOverlapRekeysDKRSEPKIndexesByPhase(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("overlap"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), identity.HardwareID, hostTestHash("old-3")}
	newSet := [][32]byte{identity.HardwareID, hostTestHash("new-2"), hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 2 {
		t.Fatalf("overlap dealer index = %d, want 2", tee.dkrsDealReq.DealerIndex)
	}
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 1, newSet[0])
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 2, newSet[1])
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 1, oldSet[0])
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 2, oldSet[1])
}

func TestHostRotatedActiveSetSameMembershipReorderedUsesDifferentDKRSEPKIndexNamespaces(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("same-a"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{identity.HardwareID, hostTestHash("same-b"), hostTestHash("same-c")}
	newSet := [][32]byte{oldSet[1], oldSet[2], identity.HardwareID}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, oldSet...)
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 1 {
		t.Fatalf("dealer index = %d, want old index 1", tee.dkrsDealReq.DealerIndex)
	}
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 1, newSet[0])
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 3, identity.HardwareID)
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 1, identity.HardwareID)
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 3, oldSet[2])
	if bytes.Equal(tee.dkrsDealPeerEPKs[1], tee.peerEPKs[1]) {
		t.Fatal("DKRS new receiver EPK namespace unexpectedly matches old dealer namespace at index 1")
	}
}

func TestHostRotatedActiveSetUsesCachedPreviousEpochForDKRS(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-1"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{identity.HardwareID, hostTestHash("old-2"), hostTestHash("old-3"), hostTestHash("old-4")}
	newSet := [][32]byte{hostTestHash("new-1"), identity.HardwareID, hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	oldInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(0),
		ActiveSetHash:   hostTestHash("active-set-0"),
		HardwareIDs:     oldSet,
		ActiveThreshold: big.NewInt(3),
		ActiveLimit:     big.NewInt(4),
	}
	newInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(1),
		ActiveSetHash:   hostTestHash("active-set-1"),
		HardwareIDs:     newSet,
		ActiveThreshold: big.NewInt(3),
		ActiveLimit:     big.NewInt(4),
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetByEpoch: map[string]*contractService.ActiveSetInfo{
			"0": oldInfo,
			"1": newInfo,
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(0)
	host.activeSet = append([][32]byte(nil), oldSet...)
	host.activeSetByEpoch["0"] = append([][32]byte(nil), oldSet...)
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	host.SetProtocolNetwork(&fakeProtocolNetwork{})

	if err := host.handleActiveSet(context.Background(), big.NewInt(1), newInfo.ActiveSetHash, newSet, false, rotationTrigger{}); err != nil {
		t.Fatalf("sync active set: %v", err)
	}
	if err := host.handleActiveSet(context.Background(), big.NewInt(1), newInfo.ActiveSetHash, newSet, true, rotationTrigger{}); err != nil {
		t.Fatalf("handle rotated active set: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 1 {
		t.Fatalf("dealer index = %d, want old epoch index 1", tee.dkrsDealReq.DealerIndex)
	}
	if tee.dkrsDealReq.NewNumParties != len(newSet) {
		t.Fatalf("new parties = %d, want %d", tee.dkrsDealReq.NewNumParties, len(newSet))
	}
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 1, newSet[0])
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 2, newSet[1])
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 1, oldSet[0])
	assertRegisteredEPKForHardware(t, tee.peerEPKs, 4, oldSet[3])
}

func TestHostRotatedDroppedOldUsesCachedPreviousEpochForDKRS(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-dropped"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{identity.HardwareID, hostTestHash("old-2"), hostTestHash("old-3"), hostTestHash("old-4")}
	newSet := [][32]byte{hostTestHash("new-1"), hostTestHash("new-2"), hostTestHash("new-3"), hostTestHash("new-4")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3"), []byte("to-new-4")}),
		},
	}
	oldInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(0),
		ActiveSetHash:   hostTestHash("active-set-0"),
		HardwareIDs:     oldSet,
		ActiveThreshold: big.NewInt(3),
		ActiveLimit:     big.NewInt(4),
	}
	newInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(1),
		ActiveSetHash:   hostTestHash("active-set-1"),
		HardwareIDs:     newSet,
		ActiveThreshold: big.NewInt(3),
		ActiveLimit:     big.NewInt(4),
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetByEpoch: map[string]*contractService.ActiveSetInfo{
			"0": oldInfo,
			"1": newInfo,
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(0)
	host.activeSet = append([][32]byte(nil), oldSet...)
	host.activeSetByEpoch["0"] = append([][32]byte(nil), oldSet...)
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)

	if err := host.handleActiveSet(context.Background(), big.NewInt(1), newInfo.ActiveSetHash, newSet, false, rotationTrigger{}); err != nil {
		t.Fatalf("sync active set: %v", err)
	}
	if host.isActive() {
		t.Fatal("dropped old node remained active after sync")
	}
	if err := host.handleActiveSet(context.Background(), big.NewInt(1), newInfo.ActiveSetHash, newSet, true, rotationTrigger{}); err != nil {
		t.Fatalf("handle rotated active set: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 1 {
		t.Fatalf("dealer index = %d, want cached old epoch index 1", tee.dkrsDealReq.DealerIndex)
	}
	if network.dkrsSendCount() != len(newSet) {
		t.Fatalf("DKRS sends = %d, want %d", network.dkrsSendCount(), len(newSet))
	}
	if host.isActive() {
		t.Fatal("dropped old node became active after rotation")
	}
}

func TestHostDefersEraseUntilDKRSDealerBroadcast(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-dropped"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{identity.HardwareID, hostTestHash("old-2"), hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("new-1"), hostTestHash("new-2"), hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	newInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(1),
		ActiveSetHash:   hostTestHash("active-set-1"),
		HardwareIDs:     newSet,
		ActiveThreshold: big.NewInt(2),
		ActiveLimit:     big.NewInt(3),
	}
	contract := &fakeContractBackend{
		chainID:       big.NewInt(1),
		pkCH:          []byte("existing-pk-ch"),
		activeSetInfo: newInfo,
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(0)
	host.activeSet = append([][32]byte(nil), oldSet...)
	host.activeSetByEpoch["0"] = append([][32]byte(nil), oldSet...)
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)

	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventExitPending,
		ExitPending: &contracts.ContractsTEEExitPending{
			HardwareId: identity.HardwareID,
			FromEpoch:  big.NewInt(0),
			NextEpoch:  big.NewInt(1),
		},
	}); err != nil {
		t.Fatalf("handle exit pending: %v", err)
	}
	if contract.eraseCalls != 0 {
		t.Fatalf("erase calls before DKRS = %d, want 0", contract.eraseCalls)
	}
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(1),
			ActiveSetHash: newInfo.ActiveSetHash,
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotated active set: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 1 {
		t.Fatalf("dealer index = %d, want 1", tee.dkrsDealReq.DealerIndex)
	}
	if network.dkrsSendCount() != len(newSet) {
		t.Fatalf("DKRS sends = %d, want %d", network.dkrsSendCount(), len(newSet))
	}
	if contract.eraseCalls != 1 || contract.lastEraseEpoch.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("erase calls/epoch after DKRS = %d/%v, want 1/0", contract.eraseCalls, contract.lastEraseEpoch)
	}
}

func TestHostRotatedActiveSetRoutesDKRSAcrossMembershipChange(t *testing.T) {
	oldSet := [][32]byte{hostTestHash("old-1"), hostTestHash("overlap"), hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("overlap"), hostTestHash("new-2"), hostTestHash("new-3")}
	activeInfo := &contractService.ActiveSetInfo{
		Epoch:           big.NewInt(2),
		ActiveSetHash:   hostTestHash("active-set-2"),
		HardwareIDs:     newSet,
		ActiveThreshold: big.NewInt(2),
		ActiveLimit:     big.NewInt(3),
	}
	network := newDKRSRotationTestNetwork(newSet)
	hosts := make(map[[32]byte]*Host)
	tees := make(map[[32]byte]*fakeProtocolTEEBackend)
	for _, hardwareID := range [][32]byte{oldSet[0], oldSet[1], oldSet[2], newSet[1], newSet[2]} {
		identity := TEEIdentity{HardwareID: hardwareID, VRFPubKey: hostTestHash("vrf-" + common.Bytes2Hex(hardwareID[:]))}
		tee := &fakeProtocolTEEBackend{
			fakeTEEBackend: fakeTEEBackend{identity: identity},
			dkrsResult: TEEDealResult{
				CommitmentsA:     bytes.Repeat(hardwareID[:1], 96),
				CommitmentB0:     bytes.Repeat(hardwareID[1:2], 192),
				SharesSerialized: dkrsRotationSharesForTest(hardwareID, len(newSet)),
			},
			extract: TEEExtractResult{VerificationKey: append([]byte("reshared-vk-"), hardwareID[0])},
		}
		contract := &fakeContractBackend{
			chainID:       big.NewInt(1),
			pkCH:          []byte("existing-pk-ch"),
			activeSetInfo: activeInfo,
		}
		host, err := NewHost(contract, tee, HostConfig{})
		if err != nil {
			t.Fatalf("new host %x: %v", hardwareID, err)
		}
		host.identity = identity
		host.active = indexOfHardwareID(oldSet, hardwareID) > 0
		host.currentEpoch = big.NewInt(1)
		host.activeSet = append([][32]byte(nil), oldSet...)
		seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
		host.SetProtocolNetwork(network.ForHost(host))
		network.AddHost(host)
		hosts[hardwareID] = host
		tees[hardwareID] = tee
	}
	rotation := contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: activeInfo.ActiveSetHash,
			HardwareIds:   newSet,
		},
	}
	for _, hardwareID := range [][32]byte{oldSet[0], oldSet[1], newSet[1], newSet[2], oldSet[2]} {
		if err := hosts[hardwareID].handleEvent(context.Background(), rotation); err != nil {
			t.Fatalf("handle rotation for %x: %v", hardwareID, err)
		}
	}
	if tees[oldSet[0]].dkrsDealReq.DealerIndex != 1 {
		t.Fatalf("old-1 dealer index = %d, want 1", tees[oldSet[0]].dkrsDealReq.DealerIndex)
	}
	if tees[oldSet[1]].dkrsDealReq.DealerIndex != 2 {
		t.Fatalf("overlap dealer index = %d, want 2", tees[oldSet[1]].dkrsDealReq.DealerIndex)
	}
	if tees[oldSet[2]].dkrsDealReq.DealerIndex != 3 {
		t.Fatalf("old-3 dealer index = %d, want 3", tees[oldSet[2]].dkrsDealReq.DealerIndex)
	}
	for _, hardwareID := range oldSet {
		assertRegisteredEPKForHardware(t, tees[hardwareID].dkrsDealPeerEPKs, 1, newSet[0])
		assertRegisteredEPKForHardware(t, tees[hardwareID].dkrsDealPeerEPKs, 2, newSet[1])
		assertRegisteredEPKForHardware(t, tees[hardwareID].dkrsDealPeerEPKs, 3, newSet[2])
	}
	for newIndex, hardwareID := range newSet {
		tee := tees[hardwareID]
		wantIndex := newIndex + 1
		if tee.dkrsExtractReq.MyIndex != wantIndex {
			t.Fatalf("receiver %x extract index = %d, want %d", hardwareID, tee.dkrsExtractReq.MyIndex, wantIndex)
		}
		if got, want := tee.dkrsExtractReq.QualOld, []int32{1, 2, 3}; !reflect.DeepEqual(got, want) {
			t.Fatalf("receiver %x qual = %v, want %v", hardwareID, got, want)
		}
		assertRegisteredEPKForHardware(t, tee.peerEPKs, 1, oldSet[0])
		assertRegisteredEPKForHardware(t, tee.peerEPKs, 2, oldSet[1])
		assertRegisteredEPKForHardware(t, tee.peerEPKs, 3, oldSet[2])
		if !hosts[hardwareID].isActive() {
			t.Fatalf("new member %x is not active after rotation", hardwareID)
		}
	}
	if hosts[oldSet[0]].isActive() || hosts[oldSet[2]].isActive() {
		t.Fatal("old-only members remained active after rotation")
	}
	if got := network.privateShares; got != 8 {
		t.Fatalf("private DKRS shares routed = %d, want 8", got)
	}
}

func TestHostEraseDeadlineSubmitsProof(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}
	tee := &fakeProtocolTEEBackend{fakeTEEBackend: fakeTEEBackend{identity: identity}}
	contract := &fakeContractBackend{chainID: big.NewInt(1)}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.activeSet = [][32]byte{identity.HardwareID}
	host.currentEpoch = big.NewInt(7)
	host.activeSetByEpoch["7"] = [][32]byte{identity.HardwareID}
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventEraseProofDeadlineSet,
		EraseProofDeadlineSet: &contracts.ContractsTEEEraseProofDeadlineSet{
			HardwareId: identity.HardwareID,
			ExitEpoch:  big.NewInt(7),
		},
	}); err != nil {
		t.Fatalf("handle erase deadline: %v", err)
	}
	if contract.eraseCalls != 1 || contract.lastEraseEpoch.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("erase calls/epoch = %d/%v, want 1/7", contract.eraseCalls, contract.lastEraseEpoch)
	}
}

func TestHostMaterialThenAuthorizationExecutesPartialAdapt(t *testing.T) {
	reqID := hostTestHash("req")
	dReq := hostTestHash("dreq")
	dPolicy := hostTestHash("dpolicy")
	activeHash := hostTestHash("active")
	source := []byte("source")
	target := []byte("target")
	identity := TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		partialResult: PartialAdaptResult{
			Contribution: []byte("share"),
			Sigma:        make([]byte, 65),
		},
	}
	contract := &fakeContractBackend{chainID: big.NewInt(1)}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.activeSet = [][32]byte{identity.HardwareID}
	network := &fakeProtocolNetwork{}
	host.SetProtocolNetwork(network)
	material := RedactionMaterial{
		ReqID:             reqID,
		SourceMessage:     source,
		TargetMessage:     target,
		DReq:              dReq,
		DPolicy:           dPolicy,
		ActiveSetHash:     activeHash,
		ActiveHardwareIDs: [][32]byte{identity.HardwareID},
		ActiveSigPubKeys:  [][32]byte{hostTestHash("sig-pubkey")},
		Threshold:         1,
		RPrimeHash:        hostTestHash("rprime"),
		ResultHash:        hostTestHash("result"),
	}
	material.BodyHashes[1] = crypto.Keccak256Hash(source)
	material.BodyHashes[2] = crypto.Keccak256Hash(target)
	material.BodyHashes[5] = activeHash
	if err := host.RegisterRedactionMaterial(context.Background(), material, true); err != nil {
		t.Fatalf("register material: %v", err)
	}
	if len(network.materials) != 1 {
		t.Fatalf("material broadcasts = %d, want 1", len(network.materials))
	}
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventRedactAuthorized,
		RedactAuthorized: &contracts.ContractsRedactAuthorized{
			ReqID:         reqID,
			DReq:          dReq,
			DPolicy:       dPolicy,
			ActiveSetHash: activeHash,
		},
	}); err != nil {
		t.Fatalf("handle redact authorized: %v", err)
	}
	if contract.executeCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", contract.executeCalls)
	}
	if contract.lastExecutedReq != reqID || len(contract.lastExecutedPi) == 0 {
		t.Fatalf("unexpected executed request/pi: %x %d", contract.lastExecutedReq, len(contract.lastExecutedPi))
	}
}

func TestHostEmitsExecuteSubmissionBeforeReceiptOutcome(t *testing.T) {
	reqID := hostTestHash("req-receipt-race")
	dReq := hostTestHash("dreq-receipt-race")
	dPolicy := hostTestHash("dpolicy-receipt-race")
	activeHash := hostTestHash("active-receipt-race")

	runHost := func(name string, status uint64, executeTx string) (*Host, []string, error) {
		identity := TEEIdentity{
			HardwareID: hostTestHash("hardware-" + name),
			SigPubKey:  hostTestHash("sig-" + name),
			VRFPubKey:  hostTestHash("vrf-" + name),
		}
		tee := &fakeProtocolTEEBackend{
			fakeTEEBackend: fakeTEEBackend{identity: identity},
			partialResult: PartialAdaptResult{
				Contribution: []byte("share-" + name),
				Sigma:        make([]byte, 65),
			},
		}
		contract := &fakeContractBackend{
			chainID:   big.NewInt(1),
			executeTx: executeTx,
			receipt: &types.Receipt{
				Status:      status,
				BlockNumber: big.NewInt(17),
			},
		}
		events := make([]string, 0, 2)
		host, err := NewHost(contract, tee, HostConfig{
			TimingSink: func(stage string, event string, fields ...interface{}) {
				events = append(events, stage+":"+event)
			},
		})
		if err != nil {
			t.Fatalf("new host %s: %v", name, err)
		}
		host.identity = identity
		host.active = true
		host.activeSet = [][32]byte{identity.SigPubKey}
		host.SetProtocolNetwork(&fakeProtocolNetwork{})

		source := []byte("source-" + name)
		target := []byte("target-" + name)
		material := RedactionMaterial{
			ReqID:             reqID,
			SourceMessage:     source,
			TargetMessage:     target,
			DReq:              dReq,
			DPolicy:           dPolicy,
			ActiveSetHash:     activeHash,
			ActiveHardwareIDs: [][32]byte{identity.HardwareID},
			ActiveSigPubKeys:  [][32]byte{identity.SigPubKey},
			Threshold:         1,
			RPrimeHash:        hostTestHash("rprime-receipt-race"),
			ResultHash:        hostTestHash("result-receipt-race"),
		}
		material.BodyHashes[1] = crypto.Keccak256Hash(source)
		material.BodyHashes[2] = crypto.Keccak256Hash(target)
		material.BodyHashes[5] = activeHash
		if err := host.RegisterRedactionMaterial(context.Background(), material, false); err != nil {
			t.Fatalf("register material %s: %v", name, err)
		}
		err = host.handleEvent(context.Background(), contractService.TEEEvent{
			Kind: contractService.TEEEventRedactAuthorized,
			RedactAuthorized: &contracts.ContractsRedactAuthorized{
				ReqID:         reqID,
				Epoch:         big.NewInt(3),
				DReq:          dReq,
				DPolicy:       dPolicy,
				ActiveSetHash: activeHash,
			},
		})
		return host, events, err
	}

	failedHost, failedEvents, err := runHost("failed", types.ReceiptStatusFailed, "")
	if err == nil {
		t.Fatal("status=0 receipt unexpectedly completed redaction")
	}
	successHost, successEvents, err := runHost("success", types.ReceiptStatusSuccessful, "")
	if err != nil {
		t.Fatalf("status=1 receipt failed: %v", err)
	}

	allEvents := append(failedEvents, successEvents...)
	if got := countTimingEvent(allEvents, "edit_execution:execute_redaction_tx_submitted"); got != 2 {
		t.Fatalf("submission timing events = %d, want 2; events=%v", got, allEvents)
	}
	if got := countTimingEvent(allEvents, "edit_execution:execute_redaction_receipt_success"); got != 1 {
		t.Fatalf("success receipt timing events = %d, want 1; events=%v", got, allEvents)
	}
	if got := countTimingEvent(allEvents, "edit_execution:execute_redaction_receipt_failed"); got != 1 {
		t.Fatalf("failed receipt timing events = %d, want 1; events=%v", got, allEvents)
	}
	if _, ok := failedHost.executedRedact[reqID]; ok {
		t.Fatal("failed receipt marked request executed")
	}
	if _, ok := successHost.executedRedact[reqID]; !ok {
		t.Fatal("successful receipt did not mark request executed")
	}

	invalidHost, invalidEvents, err := runHost("invalid", types.ReceiptStatusSuccessful, "not-a-hash")
	if err == nil {
		t.Fatal("invalid transaction hash unexpectedly completed redaction")
	}
	if got := countTimingEvent(invalidEvents, "edit_execution:execute_redaction_tx_submitted"); got != 0 {
		t.Fatalf("invalid hash submission events = %d, want 0; events=%v", got, invalidEvents)
	}
	if got := countTimingEvent(invalidEvents, "edit_execution:execute_redaction_submit_failed"); got != 1 {
		t.Fatalf("invalid hash failure events = %d, want 1; events=%v", got, invalidEvents)
	}
	if _, ok := invalidHost.executedRedact[reqID]; ok {
		t.Fatal("invalid transaction hash marked request executed")
	}
}

func countTimingEvent(events []string, want string) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}

func TestHostPartialAdaptCombinesRPrimeAndCallsExecutor(t *testing.T) {
	pp, err := chameleon.NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup nitch: %v", err)
	}
	keys, err := chameleon.NITCHBLSSimulatedDistKG(pp, 0, 1)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	source := []byte("source transaction payload")
	target := []byte("target transaction payload")
	originalR, err := chameleon.NITCHBLSComputeR(keys[0], []byte("block-1"), source)
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	share, err := chameleon.NITCHBLSPartialAdapt(keys[0], source, target)
	if err != nil {
		t.Fatalf("partial adapt: %v", err)
	}
	reqID := hostTestHash("req-combine")
	dReq := hostTestHash("dreq-combine")
	dPolicy := hostTestHash("dpolicy-combine")
	activeHash := hostTestHash("active-combine")
	identity := TEEIdentity{HardwareID: hostTestHash("hardware-combine"), VRFPubKey: hostTestHash("vrf")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		partialResult: PartialAdaptResult{
			Contribution:   share.Share,
			ProofChallenge: share.Proof.Challenge.Bytes(),
			ProofResponse:  share.Proof.Response.Bytes(),
			Sigma:          make([]byte, 65),
		},
	}
	executor := &fakeRedactionExecutor{}
	contract := &fakeContractBackend{chainID: big.NewInt(1)}
	host, err := NewHost(contract, tee, HostConfig{RedactionExecutor: executor})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.activeSet = [][32]byte{identity.HardwareID}
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	material := RedactionMaterial{
		ReqID:             reqID,
		BlockNum:          big.NewInt(11),
		Index:             2,
		ModifyType:        1,
		DataAfter:         []byte("new-data"),
		SourceMessage:     source,
		TargetMessage:     target,
		OriginalR:         originalR,
		DReq:              dReq,
		DPolicy:           dPolicy,
		ActiveSetHash:     activeHash,
		ActiveHardwareIDs: [][32]byte{identity.HardwareID},
		ActiveSigPubKeys:  [][32]byte{hostTestHash("sig-pubkey")},
		Threshold:         1,
	}
	material.BodyHashes[1] = crypto.Keccak256Hash(source)
	material.BodyHashes[2] = crypto.Keccak256Hash(target)
	material.BodyHashes[5] = activeHash
	if err := host.RegisterRedactionMaterial(context.Background(), material, false); err != nil {
		t.Fatalf("register material: %v", err)
	}
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventRedactAuthorized,
		RedactAuthorized: &contracts.ContractsRedactAuthorized{
			ReqID:         reqID,
			DReq:          dReq,
			DPolicy:       dPolicy,
			ActiveSetHash: activeHash,
		},
	}); err != nil {
		t.Fatalf("handle redact authorized: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if executor.last.ReqID != reqID || executor.last.BlockNum.Cmp(big.NewInt(11)) != 0 || len(executor.last.PiReqA) == 0 {
		t.Fatalf("executor request not populated: %#v", executor.last)
	}
	if len(executor.last.RPrime) == 0 || bytes.Equal(executor.last.RPrime, originalR) {
		t.Fatalf("executor rprime was not combined")
	}
}

func TestHostPartialAdaptDoesNotExecuteSameReqIDConcurrently(t *testing.T) {
	pp, err := chameleon.NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup nitch: %v", err)
	}
	keys, err := chameleon.NITCHBLSSimulatedDistKG(pp, 0, 1)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	source := []byte("source transaction payload")
	target := []byte("target transaction payload")
	originalR, err := chameleon.NITCHBLSComputeR(keys[0], []byte("block-1"), source)
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	share, err := chameleon.NITCHBLSPartialAdapt(keys[0], source, target)
	if err != nil {
		t.Fatalf("partial adapt: %v", err)
	}
	reqID := hostTestHash("req-concurrent")
	dReq := hostTestHash("dreq-concurrent")
	dPolicy := hostTestHash("dpolicy-concurrent")
	activeHash := hostTestHash("active-concurrent")
	identity := TEEIdentity{HardwareID: hostTestHash("hardware-concurrent"), VRFPubKey: hostTestHash("vrf-concurrent")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		partialResult: PartialAdaptResult{
			Contribution:   share.Share,
			ProofChallenge: share.Proof.Challenge.Bytes(),
			ProofResponse:  share.Proof.Response.Bytes(),
			Sigma:          make([]byte, 65),
		},
	}
	executor := newBlockingRedactionExecutor()
	contract := &fakeContractBackend{chainID: big.NewInt(1)}
	host, err := NewHost(contract, tee, HostConfig{RedactionExecutor: executor})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.activeSet = [][32]byte{identity.HardwareID}
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	material := RedactionMaterial{
		ReqID:             reqID,
		BlockNum:          big.NewInt(11),
		Index:             2,
		ModifyType:        1,
		DataAfter:         []byte("new-data"),
		SourceMessage:     source,
		TargetMessage:     target,
		OriginalR:         originalR,
		DReq:              dReq,
		DPolicy:           dPolicy,
		ActiveSetHash:     activeHash,
		ActiveHardwareIDs: [][32]byte{identity.HardwareID},
		ActiveSigPubKeys:  [][32]byte{hostTestHash("sig-pubkey")},
		Threshold:         1,
	}
	material.BodyHashes[1] = crypto.Keccak256Hash(source)
	material.BodyHashes[2] = crypto.Keccak256Hash(target)
	material.BodyHashes[5] = activeHash
	if err := host.RegisterRedactionMaterial(context.Background(), material, false); err != nil {
		t.Fatalf("register material: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- host.handleEvent(context.Background(), contractService.TEEEvent{
			Kind: contractService.TEEEventRedactAuthorized,
			RedactAuthorized: &contracts.ContractsRedactAuthorized{
				ReqID:         reqID,
				DReq:          dReq,
				DPolicy:       dPolicy,
				ActiveSetHash: activeHash,
			},
		})
	}()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	for i := 0; i < 8; i++ {
		if err := host.maybeExecutePartialAdapt(context.Background(), reqID); err != nil {
			t.Fatalf("duplicate maybeExecutePartialAdapt: %v", err)
		}
	}
	if executor.callCount() != 1 {
		t.Fatalf("executor calls while first execution is in flight = %d, want 1", executor.callCount())
	}
	close(executor.release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("handle redact authorized: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handle redact authorized did not finish")
	}
}

func TestHostP2PRedactAuthorizedTriggersExecution(t *testing.T) {
	pp, err := chameleon.NITCHBLSSetup()
	if err != nil {
		t.Fatalf("setup nitch: %v", err)
	}
	keys, err := chameleon.NITCHBLSSimulatedDistKG(pp, 0, 1)
	if err != nil {
		t.Fatalf("simulated dkg: %v", err)
	}
	source := []byte("source transaction payload p2p")
	target := []byte("target transaction payload p2p")
	originalR, err := chameleon.NITCHBLSComputeR(keys[0], []byte("block-1"), source)
	if err != nil {
		t.Fatalf("compute r: %v", err)
	}
	share, err := chameleon.NITCHBLSPartialAdapt(keys[0], source, target)
	if err != nil {
		t.Fatalf("partial adapt: %v", err)
	}
	reqID := hostTestHash("req-p2p")
	dReq := hostTestHash("dreq-p2p")
	dPolicy := hostTestHash("dpolicy-p2p")
	activeHash := hostTestHash("active-p2p")
	identity := TEEIdentity{HardwareID: hostTestHash("hardware-p2p"), VRFPubKey: hostTestHash("vrf-p2p")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		partialResult: PartialAdaptResult{
			Contribution:   share.Share,
			ProofChallenge: share.Proof.Challenge.Bytes(),
			ProofResponse:  share.Proof.Response.Bytes(),
			Sigma:          make([]byte, 65),
		},
	}
	executor := newBlockingRedactionExecutor()
	host, err := NewHost(&fakeContractBackend{chainID: big.NewInt(1)}, tee, HostConfig{
		MaterialStore:     NewMemoryRedactionMaterialStore(),
		RedactionExecutor: executor,
	})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.activeSet = [][32]byte{identity.HardwareID}
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	material := RedactionMaterial{
		ReqID:             reqID,
		BlockNum:          big.NewInt(11),
		Index:             2,
		ModifyType:        1,
		DataAfter:         []byte("new-data"),
		SourceMessage:     source,
		TargetMessage:     target,
		OriginalR:         originalR,
		DReq:              dReq,
		DPolicy:           dPolicy,
		ActiveSetHash:     activeHash,
		ActiveHardwareIDs: [][32]byte{identity.HardwareID},
		ActiveSigPubKeys:  [][32]byte{hostTestHash("sig-pubkey")},
		Threshold:         1,
	}
	material.BodyHashes[1] = crypto.Keccak256Hash(source)
	material.BodyHashes[2] = crypto.Keccak256Hash(target)
	material.BodyHashes[5] = activeHash
	if err := host.RegisterRedactionMaterial(context.Background(), material, false); err != nil {
		t.Fatalf("register material: %v", err)
	}

	authMsg := RedactAuthorizedMessage{
		RequestID:              "req-p2p-1",
		Version:                1,
		Epoch:                  1,
		ReqID:                  reqID,
		BodyHashes:             material.BodyHashes,
		DReq:                   dReq,
		DPolicy:                dPolicy,
		ExpiresAt:              big.NewInt(999999),
		CommitteePubKeyVersion: big.NewInt(1),
		AuthDigest:             hostTestHash("auth-digest"),
		AggregateSig:           []byte("bls-agg-sig"),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- host.HandleRedactAuthorizedMessage(context.Background(), authMsg, host.protocolNetwork())
	}()

	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start on P2P authorization")
	}

	close(executor.release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("HandleRedactAuthorizedMessage failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleRedactAuthorizedMessage timed out")
	}
	if executor.callCount() != 1 {
		t.Fatalf("executor callCount = %d, want 1", executor.callCount())
	}
}

func TestHostRejectsMaterialHashMismatch(t *testing.T) {
	host, err := NewHost(&fakeContractBackend{chainID: big.NewInt(1)}, &fakeTEEBackend{identity: TEEIdentity{HardwareID: hostTestHash("hardware"), VRFPubKey: hostTestHash("vrf")}}, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	material := RedactionMaterial{
		ReqID:         hostTestHash("req"),
		SourceMessage: []byte("source"),
	}
	material.BodyHashes[1] = hostTestHash("wrong")
	if err := host.RegisterRedactionMaterial(context.Background(), material, false); err == nil {
		t.Fatal("expected material hash mismatch")
	}
}

type hostMemoryCursorStore struct {
	values map[string]string
}

func (s *hostMemoryCursorStore) Get(key string) string {
	return s.values[key]
}

func (s *hostMemoryCursorStore) Save(key string, value string) error {
	s.values[key] = value
	return nil
}

func deployHostTestContract(t *testing.T) (*backends.SimulatedBackend, *contracts.Contracts, common.Address, *ecdsa.PrivateKey, *contractService.ContractService) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	backend := backends.NewSimulatedBackend(core.GenesisAlloc{
		owner: {Balance: big.NewInt(10000000000)},
	}, 80000000)
	loadHostTestChameleonParams(t)

	auth := newHostTestTransactor(key, nil)
	auth.From = owner
	address, _, contract, err := contracts.DeployContracts(auth, backend, big.NewInt(50), big.NewInt(100), big.NewInt(1), big.NewInt(1), big.NewInt(1))
	if err != nil {
		t.Fatalf("deploy contract: %v", err)
	}
	backend.Commit()
	service, err := contractService.NewContractServiceWithBackend(backend, address, key)
	if err != nil {
		t.Fatalf("new contract service: %v", err)
	}
	return backend, contract, address, key, service
}

func loadHostTestChameleonParams(t *testing.T) {
	t.Helper()
	rawHelper, err := os.ReadFile("../helper.json")
	if err != nil {
		t.Fatalf("read helper.json: %v", err)
	}
	var pqg chameleon.PQGData
	if err := json.Unmarshal(rawHelper, &pqg); err != nil {
		t.Fatalf("decode helper.json: %v", err)
	}
	chameleon.P = pqg.PValue.Int
	chameleon.Q = pqg.QValue.Int
	chameleon.G = pqg.GValue.Int
	chameleon.R = pqg.RValue.Int
}

func advanceToRotationPreparationForHostTest(t *testing.T, backend *backends.SimulatedBackend, contract *contracts.Contracts) {
	t.Helper()
	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	target := new(big.Int).Sub(deadline, big.NewInt(10))
	if target.Sign() < 0 {
		target.SetInt64(0)
	}
	for latestHostTestBlock(t, backend).Cmp(target) < 0 {
		backend.Commit()
	}
}

func latestHostTestBlock(t *testing.T, backend *backends.SimulatedBackend) *big.Int {
	t.Helper()
	header, err := backend.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("latest block: %v", err)
	}
	return new(big.Int).Set(header.Number)
}

func newHostTestTransactor(key *ecdsa.PrivateKey, value *big.Int) *bind.TransactOpts {
	auth := bind.NewKeyedTransactor(key)
	auth.Value = value
	auth.Context = context.Background()
	return auth
}

func TestHostRotationLoadsDKRSEPKsFromContract(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), identity.HardwareID, hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("new-1"), hostTestHash("new-2"), hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{
		fakeTEEBackend: fakeTEEBackend{identity: identity},
		dkrsResult: TEEDealResult{
			CommitmentsA:     bytes.Repeat([]byte{0x11}, 96),
			CommitmentB0:     bytes.Repeat([]byte{0x22}, 192),
			SharesSerialized: framedSharesForTest([][]byte{[]byte("to-new-1"), []byte("to-new-2"), []byte("to-new-3")}),
		},
	}
	contract := &fakeContractBackend{
		chainID: big.NewInt(1),
		pkCH:    []byte("existing-pk-ch"),
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	seedHostPeerEPKs(t, host, append(oldSet, newSet...)...)
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	if err := host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	}); err != nil {
		t.Fatalf("handle rotation: %v", err)
	}
	if tee.dkrsDealReq.DealerIndex != 2 || tee.dkrsDealReq.NewNumParties != 3 {
		t.Fatalf("DKRS deal not created: %#v", tee.dkrsDealReq)
	}
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 1, newSet[0])
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 2, newSet[1])
	assertRegisteredEPKForHardware(t, tee.dkrsDealPeerEPKs, 3, newSet[2])
}

func TestHostRotationFailsWhenContractEPKMissing(t *testing.T) {
	identity := TEEIdentity{HardwareID: hostTestHash("old-2"), VRFPubKey: hostTestHash("vrf")}
	oldSet := [][32]byte{hostTestHash("old-1"), identity.HardwareID, hostTestHash("old-3")}
	newSet := [][32]byte{hostTestHash("new-1"), hostTestHash("new-2"), hostTestHash("new-3")}
	tee := &fakeProtocolTEEBackend{fakeTEEBackend: fakeTEEBackend{identity: identity}}
	contract := &fakeContractBackend{
		chainID:    big.NewInt(1),
		pkCH:       []byte("existing-pk-ch"),
		enclavePKs: map[[32]byte][]byte{},
		activeSetInfo: &contractService.ActiveSetInfo{
			Epoch:           big.NewInt(2),
			ActiveSetHash:   hostTestHash("active-set-2"),
			HardwareIDs:     newSet,
			ActiveThreshold: big.NewInt(2),
			ActiveLimit:     big.NewInt(3),
		},
	}
	host, err := NewHost(contract, tee, HostConfig{})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	host.identity = identity
	host.active = true
	host.currentEpoch = big.NewInt(1)
	host.activeSet = oldSet
	host.SetProtocolNetwork(&fakeProtocolNetwork{})
	err = host.handleEvent(context.Background(), contractService.TEEEvent{
		Kind: contractService.TEEEventActiveSetRotated,
		ActiveSetRotated: &contracts.ContractsActiveSetRotated{
			Epoch:         big.NewInt(2),
			ActiveSetHash: hostTestHash("active-set-2"),
			HardwareIds:   newSet,
		},
	})
	if err == nil {
		t.Fatal("expected rotation to fail when contract EPK is missing")
	}
	if tee.dkrsDealReq.DealerIndex != 0 {
		t.Fatalf("DKRS deal unexpectedly created: %#v", tee.dkrsDealReq)
	}
}

func hostTestHash(label string) [32]byte {
	return crypto.Keccak256Hash([]byte(label))
}

func hostTestEPK(sigPubKey [32]byte) []byte {
	epk := make([]byte, 64)
	copy(epk[:32], sigPubKey[:])
	tail := hostTestHash("epk-" + common.Bytes2Hex(sigPubKey[:]))
	copy(epk[32:], tail[:])
	return epk
}

func seedHostPeerEPKs(t *testing.T, host *Host, hardwareIDs ...[32]byte) {
	t.Helper()
	contract, ok := host.contract.(*fakeContractBackend)
	if !ok {
		t.Fatalf("seed peer EPKs requires fakeContractBackend")
	}
	if contract.enclavePKs == nil {
		contract.enclavePKs = make(map[[32]byte][]byte)
	}
	seen := make(map[[32]byte]struct{})
	for _, hardwareID := range hardwareIDs {
		if _, ok := seen[hardwareID]; ok {
			continue
		}
		seen[hardwareID] = struct{}{}
		contract.enclavePKs[hardwareID] = hostTestEPK(hardwareID)
	}
}

func assertRegisteredEPKForHardware(t *testing.T, registrations map[uint64][]byte, index uint64, hardwareID [32]byte) {
	t.Helper()
	epk := registrations[index]
	if len(epk) != 64 {
		t.Fatalf("registered EPK at index %d has length %d, want 64", index, len(epk))
	}
	if !bytes.Equal(epk[:32], hardwareID[:]) {
		t.Fatalf("registered EPK at index %d belongs to %x, want %x", index, epk[:32], hardwareID)
	}
}

type dkrsRotationTestNetwork struct {
	hosts         map[[32]byte]*Host
	privateShares int
}

type dkrsRotationHostNetwork struct {
	bus   *dkrsRotationTestNetwork
	owner *Host
}

func newDKRSRotationTestNetwork(newSet [][32]byte) *dkrsRotationTestNetwork {
	bus := &dkrsRotationTestNetwork{
		hosts: make(map[[32]byte]*Host),
	}
	return bus
}

func (n *dkrsRotationTestNetwork) AddHost(host *Host) {
	n.hosts[host.identity.HardwareID] = host
}

func (n *dkrsRotationTestNetwork) ForHost(host *Host) ProtocolNetwork {
	return &dkrsRotationHostNetwork{bus: n, owner: host}
}

func (n *dkrsRotationHostNetwork) BroadcastDistKG(ctx context.Context, msg DistKGProtocolMessage) error {
	return nil
}

func (n *dkrsRotationHostNetwork) SendDistKGShare(ctx context.Context, toIndex int, msg DistKGProtocolMessage) error {
	return nil
}

func (n *dkrsRotationHostNetwork) BroadcastDKRS(ctx context.Context, msg DKRSProtocolMessage) error {
	for hardwareID, host := range n.bus.hosts {
		if hardwareID == n.owner.identity.HardwareID {
			continue
		}
		if err := host.HandleDKRS(ctx, cloneDKRSProtocolMessage(msg), host.protocolNetwork()); err != nil {
			return err
		}
	}
	return nil
}

func (n *dkrsRotationHostNetwork) SendDKRS(ctx context.Context, toSigPubKey [32]byte, msg DKRSProtocolMessage) error {
	if msg.Share != nil {
		n.bus.privateShares++
	}
	if toSigPubKey == n.owner.identity.HardwareID {
		return nil
	}
	host := n.bus.hosts[toSigPubKey]
	if host == nil {
		return nil
	}
	return host.HandleDKRS(ctx, cloneDKRSProtocolMessage(msg), host.protocolNetwork())
}

func (n *dkrsRotationHostNetwork) BroadcastPartialAdapt(ctx context.Context, msg PartialAdaptProtocolMessage) error {
	return nil
}

func (n *dkrsRotationHostNetwork) BroadcastRedactMaterial(ctx context.Context, material RedactionMaterial) error {
	return nil
}

func (n *dkrsRotationHostNetwork) BroadcastRedactAuthorized(ctx context.Context, msg RedactAuthorizedMessage) error {
	return nil
}

func dkrsRotationSharesForTest(dealer [32]byte, receivers int) []byte {
	shares := make([][]byte, 0, receivers)
	for i := 1; i <= receivers; i++ {
		payload := append([]byte{byte(i)}, dealer[:]...)
		shares = append(shares, payload)
	}
	return framedSharesForTest(shares)
}

func cloneHostTestActiveSetInfo(info *contractService.ActiveSetInfo) *contractService.ActiveSetInfo {
	if info == nil {
		return nil
	}
	out := *info
	out.Epoch = cloneBigInt(info.Epoch)
	out.ActiveThreshold = cloneBigInt(info.ActiveThreshold)
	out.ActiveLimit = cloneBigInt(info.ActiveLimit)
	out.CommitteePubKeyVersion = cloneBigInt(info.CommitteePubKeyVersion)
	out.HardwareIDs = append([][32]byte(nil), info.HardwareIDs...)
	return &out
}

func hostAddressAsBytes32(addr common.Address) [32]byte {
	var out [32]byte
	copy(out[12:], addr.Bytes())
	return out
}

func registerTEEForHostTest(t *testing.T, backend *backends.SimulatedBackend, contract *contracts.Contracts, key *ecdsa.PrivateKey, committeeSecret *big.Int, hardwareID [32]byte) {
	t.Helper()
	owner := crypto.PubkeyToAddress(key.PublicKey)
	if _, err := contract.StakeTEE(newHostTestTransactor(key, big.NewInt(5))); err != nil {
		t.Fatalf("stake tee: %v", err)
	}
	backend.Commit()
	challengeHash, err := contract.GetRegistrationChallenge(nil, owner)
	if err != nil {
		t.Fatalf("get challenge: %v", err)
	}
	exists, _, pendingNonce, _, _, err := contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee: %v", err)
	}
	if !exists {
		t.Fatalf("pending tee missing")
	}
	evidence := contracts.PermissionManagerV3TEERegistrationEvidence{
		SigPubKey:        hostAddressAsBytes32(owner),
		VrfPubKey:        hostAddressAsBytes32(owner),
		EnclavePK:        hostTestEnclavePKParts(hardwareID),
		MrSigner:         hostTestHash("mrsigner"),
		QuoteHash:        hostTestHash("quote"),
		RegisterCertHash: hostTestHash("cert"),
		ChallengeHash:    challengeHash,
	}
	mrEnclave := hostTestHash("mr")
	digest := computeTEERegistrationDigestForHostTest(t, owner, pendingNonce, mrEnclave, hardwareID, evidence)
	signature := committeeSignatureForHostTest(digest, committeeSecret)
	if _, err := contract.RegisterTEE(newHostTestTransactor(key, nil), owner, pendingNonce, mrEnclave, hardwareID, evidence, signature); err != nil {
		t.Fatalf("register tee: %v", err)
	}
	backend.Commit()
}

func computeTEERegistrationDigestForHostTest(t *testing.T, owner common.Address, pendingNonce *big.Int, mrEnclave [32]byte, hardwareID [32]byte, evidence contracts.PermissionManagerV3TEERegistrationEvidence) [32]byte {
	t.Helper()
	args := abi.Arguments{
		{Type: hostABIType(t, "address")},
		{Type: hostABIType(t, "uint256")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32[2]")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
		{Type: hostABIType(t, "bytes32")},
	}
	encoded, err := args.Pack(owner, pendingNonce, mrEnclave, hardwareID, evidence.SigPubKey, evidence.VrfPubKey, evidence.EnclavePK, evidence.MrSigner, evidence.QuoteHash, evidence.RegisterCertHash, evidence.ChallengeHash)
	if err != nil {
		t.Fatalf("pack registration digest: %v", err)
	}
	return crypto.Keccak256Hash(encoded)
}

func hostTestEnclavePKParts(seed [32]byte) [2][32]byte {
	var out [2][32]byte
	epk := hostTestEPK(seed)
	copy(out[0][:], epk[:32])
	copy(out[1][:], epk[32:])
	return out
}

func hostABIType(t *testing.T, name string) abi.Type {
	t.Helper()
	typ, err := abi.NewType(name, "", nil)
	if err != nil {
		t.Fatalf("new abi type %s: %v", name, err)
	}
	return typ
}

func deriveHostVRFResult(seed [32]byte, vrfPubKey [32]byte, proof []byte) [32]byte {
	v := proof[64]
	if v < 27 {
		v += 27
	}
	packed := make([]byte, 0, 32+32+32+32+32+1)
	domain := crypto.Keccak256Hash([]byte("T_GOVERN_TEE_VRF_V1"))
	packed = append(packed, domain[:]...)
	packed = append(packed, seed[:]...)
	packed = append(packed, vrfPubKey[:]...)
	packed = append(packed, proof[:32]...)
	packed = append(packed, proof[32:64]...)
	packed = append(packed, v)
	return crypto.Keccak256Hash(packed)
}

func committeePubKeyForHostTest(secret *big.Int) [4]*big.Int {
	parts := hostSplitBN256Point(new(bn256.G2).ScalarBaseMult(secret).Marshal(), 4)
	return [4]*big.Int{parts[0], parts[1], parts[2], parts[3]}
}

func committeeSignatureForHostTest(messageHash [32]byte, secret *big.Int) [2]*big.Int {
	digest := sha256.Sum256(append(messageHash[:], []byte("committee-sig/mvp/v1")...))
	scalar := new(big.Int).Mod(new(big.Int).SetBytes(digest[:]), bn256.Order)
	if scalar.Sign() == 0 {
		scalar.SetInt64(1)
	}
	hm := new(bn256.G1).ScalarBaseMult(scalar)
	sig := new(bn256.G1).ScalarMult(hm, secret)
	parts := hostSplitBN256Point(sig.Marshal(), 2)
	return [2]*big.Int{parts[0], parts[1]}
}

func hostSplitBN256Point(point []byte, limbs int) []*big.Int {
	out := make([]*big.Int, limbs)
	for i := 0; i < limbs; i++ {
		out[i] = new(big.Int).SetBytes(point[i*32 : (i+1)*32])
	}
	return out
}
