package contractService

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

type memoryCursorStore struct {
	values map[string]string
}

func (s *memoryCursorStore) Get(key string) string {
	return s.values[key]
}

func (s *memoryCursorStore) Save(key string, value string) error {
	s.values[key] = value
	return nil
}

func TestTEEEventParserTopicsFromABI(t *testing.T) {
	_, contract, _, _, _ := deployWatcherTestContract(t)
	parsers, topics, err := buildTEEEventParsers(contract)
	if err != nil {
		t.Fatalf("build parsers: %v", err)
	}
	expected := expectedTEEWatcherEventNames()
	if len(parsers) != len(expected) || len(topics) != len(expected) {
		t.Fatalf("parser/topic count mismatch: parsers=%d topics=%d", len(parsers), len(topics))
	}
	parsed, err := abi.JSON(strings.NewReader(contracts.ContractsABI))
	if err != nil {
		t.Fatalf("parse abi: %v", err)
	}
	for _, name := range expected {
		eventDef, ok := parsed.Events[name]
		if !ok {
			t.Fatalf("ABI missing expected watcher event %s", name)
		}
		if parsers[eventDef.ID()] == nil {
			t.Fatalf("%s topic parser missing", name)
		}
	}
	if parsers[common.HexToHash("0x1234")] != nil {
		t.Fatalf("unknown topic has parser")
	}
}

func expectedTEEWatcherEventNames() []string {
	return []string{
		"TEERotationPreparationRequested",
		"TEEVRFSubmitted",
		"ActiveSetCheckpointed",
		"ActiveSetRotated",
		"TEEStakePending",
		"PendingTEECancelled",
		"TEERegistered",
		"TEERegistrationEvidenceRecorded",
		"PkCHSet",
		"RedactAuthorized",
		"RedactRequestPayloadRecorded",
		"RedactExecuted",
		"RedactExpired",
		"RedactRejected",
		"TEEExitPending",
		"TEEExited",
		"TEERevoked",
		"TEEEraseProofDeadlineSet",
		"CommitteePubKeyRegistered",
		"TrustedMREnclaveSet",
	}
}

func TestTEEEventWatcherReceivesRotationEvent(t *testing.T) {
	backend, contract, address, key, service := deployWatcherTestContract(t)
	store := &memoryCursorStore{values: make(map[string]string)}
	watcher, err := NewTEEEventWatcher(service, TEEEventWatcherConfig{
		Confirmations: 0,
		BatchSize:     1000,
		CursorStore:   store,
		CursorKey:     "rotation-cursor",
	})
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	ch := make(chan TEEEvent, 4)
	sub := service.SubscribeTEEEvents(ch)
	defer sub.Unsubscribe()

	advanceToRotationPreparationForWatcherTest(t, backend, contract)
	if _, err := contract.PrepareRotation(newWatcherTestTransactor(key, nil)); err != nil {
		t.Fatalf("prepare rotation: %v", err)
	}
	backend.Commit()

	if err := watcher.scanOnce(context.Background()); err != nil {
		t.Fatalf("scan once: %v", err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind != TEEEventRotationPreparationRequested {
				continue
			}
			if ev.RotationRequested == nil {
				t.Fatalf("rotation event missing payload: %#v", ev)
			}
			if ev.RotationRequested.NextEpoch.Cmp(big.NewInt(1)) != 0 {
				t.Fatalf("next epoch = %s, want 1", ev.RotationRequested.NextEpoch)
			}
			goto foundRotation
		case <-deadline:
			t.Fatalf("timed out waiting for rotation event from %s", address.Hex())
		}
	}
foundRotation:
	if store.values["rotation-cursor"] == "" {
		t.Fatalf("cursor was not saved")
	}
}

func TestTEEEventWatcherDispatchesMultipleTopics(t *testing.T) {
	backend, contract, _, key, service := deployWatcherTestContract(t)
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newWatcherTestTransactor(key, nil), committeePubKeyForWatcherTest(committeeSecret)); err != nil {
		t.Fatalf("register committee key: %v", err)
	}
	backend.Commit()
	hardwareID := watcherTestHash("multi-topic-tee")
	registerTEEForWatcherTest(t, backend, contract, key, committeeSecret, hardwareID)

	store := &memoryCursorStore{values: make(map[string]string)}
	watcher, err := NewTEEEventWatcher(service, TEEEventWatcherConfig{
		Confirmations: 0,
		BatchSize:     1000,
		CursorStore:   store,
		CursorKey:     "multi-topic-cursor",
	})
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	ch := make(chan TEEEvent, 8)
	sub := service.SubscribeTEEEvents(ch)
	defer sub.Unsubscribe()

	if err := watcher.scanOnce(context.Background()); err != nil {
		t.Fatalf("scan once: %v", err)
	}
	seenRegistered := false
	seenEvidence := false
	deadline := time.After(time.Second)
	for !seenRegistered || !seenEvidence {
		select {
		case ev := <-ch:
			if ev.Kind == TEEEventRegistered && ev.Registered != nil {
				seenRegistered = true
			}
			if ev.Kind == TEEEventRegistrationEvidenceRecorded && ev.RegistrationEvidenceRecorded != nil {
				seenEvidence = true
			}
		case <-deadline:
			t.Fatalf("missing expected events: registered=%v evidence=%v", seenRegistered, seenEvidence)
		}
	}
}

func TestContractServiceGetTEEEnclavePK(t *testing.T) {
	backend, contract, _, key, service := deployWatcherTestContract(t)
	committeeSecret := big.NewInt(19)
	if _, err := contract.RegisterCommitteePubKey(newWatcherTestTransactor(key, nil), committeePubKeyForWatcherTest(committeeSecret)); err != nil {
		t.Fatalf("register committee key: %v", err)
	}
	backend.Commit()

	evidence := registerTEEForWatcherTest(t, backend, contract, key, committeeSecret, watcherTestHash("contract-service-epk"))
	got, err := service.GetTEEEnclavePK(evidence.SigPubKey)
	if err != nil {
		t.Fatalf("get tee enclave pk: %v", err)
	}
	want := make([]byte, 0, 64)
	want = append(want, evidence.EnclavePK[0][:]...)
	want = append(want, evidence.EnclavePK[1][:]...)
	if !bytes.Equal(got, want) {
		t.Fatalf("enclave pk = %x, want %x", got, want)
	}
}

func TestTEEEventWatcherConfirmationDelayDoesNotAdvanceCursor(t *testing.T) {
	_, _, _, _, service := deployWatcherTestContract(t)
	store := &memoryCursorStore{values: make(map[string]string)}
	watcher, err := NewTEEEventWatcher(service, TEEEventWatcherConfig{
		Confirmations: 100,
		BatchSize:     1000,
		CursorStore:   store,
		CursorKey:     "confirm-cursor",
	})
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	if err := watcher.scanOnce(context.Background()); err != nil {
		t.Fatalf("scan once: %v", err)
	}
	if got := store.values["confirm-cursor"]; got != "" {
		t.Fatalf("cursor advanced despite confirmations: %q", got)
	}
}

func deployWatcherTestContract(t *testing.T) (*backends.SimulatedBackend, *contracts.Contracts, common.Address, *ecdsa.PrivateKey, *ContractService) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	backend := backends.NewSimulatedBackend(core.GenesisAlloc{
		owner: {Balance: big.NewInt(10000000000)},
	}, 80000000)
	loadWatcherTestChameleonParams(t)

	auth := newWatcherTestTransactor(key, nil)
	auth.From = owner
	address, _, contract, err := contracts.DeployContracts(auth, backend, big.NewInt(50), big.NewInt(100), big.NewInt(1), big.NewInt(1), big.NewInt(1))
	if err != nil {
		t.Fatalf("deploy contract: %v", err)
	}
	backend.Commit()
	service, err := NewContractServiceWithBackend(backend, address, key)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return backend, contract, address, key, service
}

func loadWatcherTestChameleonParams(t *testing.T) {
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

func advanceToRotationPreparationForWatcherTest(t *testing.T, backend *backends.SimulatedBackend, contract *contracts.Contracts) {
	t.Helper()
	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	target := new(big.Int).Sub(deadline, big.NewInt(10))
	if target.Sign() < 0 {
		target.SetInt64(0)
	}
	for latestWatcherTestBlock(t, backend).Cmp(target) < 0 {
		backend.Commit()
	}
}

func latestWatcherTestBlock(t *testing.T, backend *backends.SimulatedBackend) *big.Int {
	t.Helper()
	header, err := backend.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("latest block: %v", err)
	}
	return new(big.Int).Set(header.Number)
}

func newWatcherTestTransactor(key *ecdsa.PrivateKey, value *big.Int) *bind.TransactOpts {
	auth := bind.NewKeyedTransactor(key)
	auth.Value = value
	auth.Context = context.Background()
	return auth
}

func watcherTestHash(label string) [32]byte {
	return crypto.Keccak256Hash([]byte(label))
}

func watcherAddressAsBytes32(addr common.Address) [32]byte {
	var out [32]byte
	copy(out[12:], addr.Bytes())
	return out
}

func registerTEEForWatcherTest(t *testing.T, backend *backends.SimulatedBackend, contract *contracts.Contracts, key *ecdsa.PrivateKey, committeeSecret *big.Int, hardwareID [32]byte) contracts.PermissionManagerV3TEERegistrationEvidence {
	t.Helper()
	owner := crypto.PubkeyToAddress(key.PublicKey)
	if _, err := contract.StakeTEE(newWatcherTestTransactor(key, big.NewInt(5))); err != nil {
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
		SigPubKey:        watcherAddressAsBytes32(owner),
		VrfPubKey:        watcherAddressAsBytes32(owner),
		EnclavePK:        watcherTestEnclavePKParts("watcher"),
		MrSigner:         watcherTestHash("mrsigner"),
		QuoteHash:        watcherTestHash("quote"),
		RegisterCertHash: watcherTestHash("cert"),
		ChallengeHash:    challengeHash,
	}
	mrEnclave := watcherTestHash("mr")
	digest := computeTEERegistrationDigestForWatcherTest(t, owner, pendingNonce, mrEnclave, hardwareID, evidence)
	signature := committeeSignatureForWatcherTest(digest, committeeSecret)
	if _, err := contract.RegisterTEE(newWatcherTestTransactor(key, nil), owner, pendingNonce, mrEnclave, hardwareID, evidence, signature); err != nil {
		t.Fatalf("register tee: %v", err)
	}
	backend.Commit()
	return evidence
}

func computeTEERegistrationDigestForWatcherTest(t *testing.T, owner common.Address, pendingNonce *big.Int, mrEnclave [32]byte, hardwareID [32]byte, evidence contracts.PermissionManagerV3TEERegistrationEvidence) [32]byte {
	t.Helper()
	args := abi.Arguments{
		{Type: watcherABIType(t, "address")},
		{Type: watcherABIType(t, "uint256")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32[2]")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
		{Type: watcherABIType(t, "bytes32")},
	}
	encoded, err := args.Pack(owner, pendingNonce, mrEnclave, hardwareID, evidence.SigPubKey, evidence.VrfPubKey, evidence.EnclavePK, evidence.MrSigner, evidence.QuoteHash, evidence.RegisterCertHash, evidence.ChallengeHash)
	if err != nil {
		t.Fatalf("pack registration digest: %v", err)
	}
	return crypto.Keccak256Hash(encoded)
}

func watcherTestEnclavePKParts(prefix string) [2][32]byte {
	return [2][32]byte{
		watcherTestHash(prefix + "-epk-x"),
		watcherTestHash(prefix + "-epk-y"),
	}
}

func watcherABIType(t *testing.T, name string) abi.Type {
	t.Helper()
	typ, err := abi.NewType(name, "", nil)
	if err != nil {
		t.Fatalf("new abi type %s: %v", name, err)
	}
	return typ
}

func committeePubKeyForWatcherTest(secret *big.Int) [4]*big.Int {
	parts := watcherSplitBN256Point(new(bn256.G2).ScalarBaseMult(secret).Marshal(), 4)
	return [4]*big.Int{parts[0], parts[1], parts[2], parts[3]}
}

func committeeSignatureForWatcherTest(messageHash [32]byte, secret *big.Int) [2]*big.Int {
	digest := sha256.Sum256(append(messageHash[:], []byte("committee-sig/mvp/v1")...))
	scalar := new(big.Int).Mod(new(big.Int).SetBytes(digest[:]), bn256.Order)
	if scalar.Sign() == 0 {
		scalar.SetInt64(1)
	}
	hm := new(bn256.G1).ScalarBaseMult(scalar)
	sig := new(bn256.G1).ScalarMult(hm, secret)
	parts := watcherSplitBN256Point(sig.Marshal(), 2)
	return [2]*big.Int{parts[0], parts[1]}
}

func watcherSplitBN256Point(point []byte, limbs int) []*big.Int {
	out := make([]*big.Int, limbs)
	for i := 0; i < limbs; i++ {
		out[i] = new(big.Int).SetBytes(point[i*32 : (i+1)*32])
	}
	return out
}
