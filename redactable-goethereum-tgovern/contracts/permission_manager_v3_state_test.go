package contracts

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

func newSimulatedAccount(t *testing.T) (*ecdsa.PrivateKey, *bind.TransactOpts) {
	t.Helper()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key, bind.NewKeyedTransactor(key)
}

func deployPermissionManagerV3ForTest(t *testing.T) (*backends.SimulatedBackend, *Contracts, common.Address, *ecdsa.PrivateKey) {
	return deployPermissionManagerV3ForTestWithParams(t, nil, big.NewInt(1000), big.NewInt(1), big.NewInt(1))
}

func deployPermissionManagerV3ForTestWithParams(t *testing.T, extraKeys []*ecdsa.PrivateKey, epochLength *big.Int, activeThreshold *big.Int, activeLimit *big.Int) (*backends.SimulatedBackend, *Contracts, common.Address, *ecdsa.PrivateKey) {
	return deployPermissionManagerV3ForTestWithEpochs(t, extraKeys, epochLength, epochLength, activeThreshold, activeLimit)
}

func deployPermissionManagerV3ForTestWithEpochs(t *testing.T, extraKeys []*ecdsa.PrivateKey, bootstrapEpochLength *big.Int, steadyEpochLength *big.Int, activeThreshold *big.Int, activeLimit *big.Int) (*backends.SimulatedBackend, *Contracts, common.Address, *ecdsa.PrivateKey) {
	t.Helper()

	key, auth := newSimulatedAccount(t)
	owner := crypto.PubkeyToAddress(key.PublicKey)
	auth.From = owner
	expectedBal := big.NewInt(10000000000)
	alloc := core.GenesisAlloc{
		owner: {Balance: expectedBal},
	}
	for _, extraKey := range extraKeys {
		alloc[crypto.PubkeyToAddress(extraKey.PublicKey)] = core.GenesisAccount{Balance: expectedBal}
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldwd); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	}()
	rawHelper, err := os.ReadFile("helper.json")
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
	backend := backends.NewSimulatedBackend(alloc, 80000000)

	address, _, contract, err := DeployContracts(
		auth,
		backend,
		bootstrapEpochLength,
		steadyEpochLength,
		big.NewInt(1),
		activeThreshold,
		activeLimit,
	)
	if err != nil {
		t.Fatalf("deploy contract: %v", err)
	}
	backend.Commit()
	return backend, contract, address, key
}

func newTransactor(key *ecdsa.PrivateKey, value *big.Int) *bind.TransactOpts {
	auth := bind.NewKeyedTransactor(key)
	auth.Value = value
	auth.Context = context.Background()
	return auth
}

func testHash(label string) [32]byte {
	return crypto.Keccak256Hash([]byte(label))
}

func latestTimestamp(t *testing.T, backend *backends.SimulatedBackend) *big.Int {
	t.Helper()
	header, err := backend.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("latest header: %v", err)
	}
	return new(big.Int).SetUint64(header.Time)
}

func latestBlockNumber(t *testing.T, backend *backends.SimulatedBackend) *big.Int {
	t.Helper()
	header, err := backend.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("latest header: %v", err)
	}
	return new(big.Int).Set(header.Number)
}

func mineUntilBlock(t *testing.T, backend *backends.SimulatedBackend, target *big.Int) {
	t.Helper()
	for latestBlockNumber(t, backend).Cmp(target) < 0 {
		backend.Commit()
	}
}

func advanceToRotationPreparation(t *testing.T, backend *backends.SimulatedBackend, contract *Contracts) {
	t.Helper()
	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	target := new(big.Int).Sub(deadline, big.NewInt(10))
	if target.Sign() < 0 {
		target.SetInt64(0)
	}
	mineUntilBlock(t, backend, target)
}

func triggerRotationAtDeadline(t *testing.T, backend *backends.SimulatedBackend, contract *Contracts, key *ecdsa.PrivateKey) {
	t.Helper()
	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	target := new(big.Int).Sub(deadline, big.NewInt(1))
	if target.Sign() < 0 {
		target.SetInt64(0)
	}
	mineUntilBlock(t, backend, target)
	if _, err := contract.SetTrustedMREnclave(newTransactor(key, nil), testHash("rotation-deadline-trigger"), true); err != nil {
		t.Fatalf("trigger rotation: %v", err)
	}
	backend.Commit()
}

func triggerRotationAtDeadlineWithOwner(t *testing.T, backend *backends.SimulatedBackend, contract *Contracts, ownerKey *ecdsa.PrivateKey, label string) {
	t.Helper()
	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	target := new(big.Int).Sub(deadline, big.NewInt(1))
	if target.Sign() < 0 {
		target.SetInt64(0)
	}
	mineUntilBlock(t, backend, target)
	if _, err := contract.SetTrustedMREnclave(newTransactor(ownerKey, nil), testHash(label), true); err != nil {
		t.Fatalf("trigger rotation as owner: %v", err)
	}
	backend.Commit()
}

func signPkCHForTest(t *testing.T, contract *Contracts, key *ecdsa.PrivateKey, pkCH []byte) []byte {
	t.Helper()
	owner := crypto.PubkeyToAddress(key.PublicKey)
	digest, err := contract.ComputePkCHDigest(nil, owner, pkCH)
	if err != nil {
		t.Fatalf("compute pkCH digest: %v", err)
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatalf("sign pkCH digest: %v", err)
	}
	return signature
}

func splitBN256Point(point []byte, limbs int) []*big.Int {
	out := make([]*big.Int, limbs)
	for i := 0; i < limbs; i++ {
		out[i] = new(big.Int).SetBytes(point[i*32 : (i+1)*32])
	}
	return out
}

func committeePubKeyForSecret(secret *big.Int) [4]*big.Int {
	parts := splitBN256Point(new(bn256.G2).ScalarBaseMult(secret).Marshal(), 4)
	return [4]*big.Int{parts[0], parts[1], parts[2], parts[3]}
}

func committeeSignatureForDigest(messageHash [32]byte, secret *big.Int) [2]*big.Int {
	digest := sha256.Sum256(append(messageHash[:], []byte("committee-sig/mvp/v1")...))
	scalar := new(big.Int).Mod(new(big.Int).SetBytes(digest[:]), bn256.Order)
	if scalar.Sign() == 0 {
		scalar.SetInt64(1)
	}
	hm := new(bn256.G1).ScalarBaseMult(scalar)
	sig := new(bn256.G1).ScalarMult(hm, secret)
	parts := splitBN256Point(sig.Marshal(), 2)
	return [2]*big.Int{parts[0], parts[1]}
}

func abiTypeForTest(t *testing.T, name string) abi.Type {
	t.Helper()
	typ, err := abi.NewType(name, "", nil)
	if err != nil {
		t.Fatalf("new abi type %s: %v", name, err)
	}
	return typ
}

func computeDReqForTest(t *testing.T, contractAddress common.Address, req PermissionManagerV3ReqFields) [32]byte {
	t.Helper()
	args := abi.Arguments{
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "uint256")},
		{Type: abiTypeForTest(t, "address")},
		{Type: abiTypeForTest(t, "uint16")},
		{Type: abiTypeForTest(t, "uint64")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32[7]")},
	}
	encoded, err := args.Pack(
		crypto.Keccak256Hash([]byte("T_GOVERN_REDACT_REQ_V1")),
		big.NewInt(1337),
		contractAddress,
		req.Version,
		req.Epoch,
		req.ReqID,
		req.BodyHashes,
	)
	if err != nil {
		t.Fatalf("pack d_req: %v", err)
	}
	return crypto.Keccak256Hash(encoded)
}

func computeTEERegistrationDigestForTest(t *testing.T, owner common.Address, pendingNonce *big.Int, mrEnclave [32]byte, hardwareID [32]byte, evidence PermissionManagerV3TEERegistrationEvidence) [32]byte {
	t.Helper()
	args := abi.Arguments{
		{Type: abiTypeForTest(t, "address")},
		{Type: abiTypeForTest(t, "uint256")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32[2]")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
	}
	encoded, err := args.Pack(
		owner,
		pendingNonce,
		mrEnclave,
		hardwareID,
		evidence.SigPubKey,
		evidence.VrfPubKey,
		evidence.EnclavePK,
		evidence.MrSigner,
		evidence.QuoteHash,
		evidence.RegisterCertHash,
		evidence.ChallengeHash,
	)
	if err != nil {
		t.Fatalf("pack tee registration digest: %v", err)
	}
	return crypto.Keccak256Hash(encoded)
}

func computeRedactionProofHashForTest(t *testing.T, reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) [32]byte {
	t.Helper()
	args := abi.Arguments{
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes32")},
		{Type: abiTypeForTest(t, "bytes")},
	}
	encoded, err := args.Pack(reqID, dReq, activeSetHash, piReqA)
	if err != nil {
		t.Fatalf("pack redaction proof hash: %v", err)
	}
	return crypto.Keccak256Hash(encoded)
}

func addressAsBytes32(addr common.Address) [32]byte {
	var out [32]byte
	copy(out[12:], addr.Bytes())
	return out
}

func vrfPubKeyForKey(key *ecdsa.PrivateKey) [32]byte {
	return addressAsBytes32(crypto.PubkeyToAddress(key.PublicKey))
}

func sigPubKeyForKey(key *ecdsa.PrivateKey) [32]byte {
	return addressAsBytes32(crypto.PubkeyToAddress(key.PublicKey))
}

func sigPubKeysForKeys(keys []*ecdsa.PrivateKey) [][32]byte {
	out := make([][32]byte, len(keys))
	for i, key := range keys {
		out[i] = sigPubKeyForKey(key)
	}
	return out
}

func deriveVRFResultForTest(seed [32]byte, vrfPubKey [32]byte, proof []byte) [32]byte {
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

func vrfSubmissionForTest(t *testing.T, contract *Contracts, key *ecdsa.PrivateKey, epoch *big.Int) ([32]byte, []byte) {
	t.Helper()
	sigPubKey := sigPubKeyForKey(key)
	vrfPubKey := vrfPubKeyForKey(key)
	seed, err := contract.SeedByEpoch(nil, epoch)
	if err != nil {
		t.Fatalf("get seed for epoch %s: %v", epoch, err)
	}
	if seed == ([32]byte{}) {
		t.Fatalf("seed for epoch %s is zero", epoch)
	}
	digest, err := contract.ComputeVRFDigest(nil, epoch, sigPubKey, vrfPubKey, seed)
	if err != nil {
		t.Fatalf("compute vrf digest: %v", err)
	}
	proof, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatalf("sign vrf digest: %v", err)
	}
	return deriveVRFResultForTest(seed, vrfPubKey, proof), proof
}

type shareProofForTest struct {
	SigPubKey [32]byte
	SI        []byte
	Sigma     []byte
}

func encodePiReqAForTest(t *testing.T, proofs []shareProofForTest) []byte {
	t.Helper()
	shareProofType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "sigPubKey", Type: "bytes32"},
		{Name: "sI", Type: "bytes"},
		{Name: "sigma", Type: "bytes"},
	})
	if err != nil {
		t.Fatalf("create share proof ABI type: %v", err)
	}
	args := abi.Arguments{{Type: shareProofType}}
	encoded, err := args.Pack(proofs)
	if err != nil {
		t.Fatalf("encode pi_req_A: %v", err)
	}
	return encoded
}

func signShareProofForTest(t *testing.T, dReq [32]byte, key *ecdsa.PrivateKey, sI []byte) []byte {
	t.Helper()
	message := make([]byte, 0, len(dReq)+len(sI))
	message = append(message, dReq[:]...)
	message = append(message, sI...)
	digest := crypto.Keccak256Hash(message)
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatalf("sign share proof: %v", err)
	}
	return signature
}

func piReqAForTest(t *testing.T, dReq [32]byte, key *ecdsa.PrivateKey, sI []byte) []byte {
	t.Helper()
	return encodePiReqAForTest(t, []shareProofForTest{{
		SigPubKey: addressAsBytes32(crypto.PubkeyToAddress(key.PublicKey)),
		SI:        sI,
		Sigma:     signShareProofForTest(t, dReq, key, sI),
	}})
}

func submitVRFForTest(t *testing.T, backend *backends.SimulatedBackend, contract *Contracts, key *ecdsa.PrivateKey, epoch *big.Int, hardwareID [32]byte) [32]byte {
	t.Helper()
	sigPubKey := sigPubKeyForKey(key)
	result, proof := vrfSubmissionForTest(t, contract, key, epoch)
	if _, err := contract.SubmitTEESelectionVRF(newTransactor(key, nil), epoch, sigPubKey, result, proof); err != nil {
		t.Fatalf("submit vrf for %x: %v", sigPubKey, err)
	}
	backend.Commit()
	return result
}

func testEvidence(prefix string) PermissionManagerV3TEERegistrationEvidence {
	return PermissionManagerV3TEERegistrationEvidence{
		SigPubKey:        testHash(prefix + "-sig"),
		VrfPubKey:        testHash(prefix + "-vrf"),
		EnclavePK:        testEnclavePKParts(prefix),
		MrSigner:         testHash(prefix + "-mrsigner"),
		QuoteHash:        testHash(prefix + "-quote"),
		RegisterCertHash: testHash(prefix + "-cert"),
	}
}

func registerTEEForTest(t *testing.T, backend *backends.SimulatedBackend, contract *Contracts, key *ecdsa.PrivateKey, committeeSecret *big.Int, hardwareID [32]byte, evidence PermissionManagerV3TEERegistrationEvidence) PermissionManagerV3TEERegistrationEvidence {
	t.Helper()
	owner := crypto.PubkeyToAddress(key.PublicKey)
	evidence.SigPubKey = sigPubKeyForKey(key)
	evidence.VrfPubKey = addressAsBytes32(owner)
	if _, err := contract.StakeTEE(newTransactor(key, big.NewInt(5))); err != nil {
		t.Fatalf("stake tee: %v", err)
	}
	backend.Commit()

	challengeHash, err := contract.GetRegistrationChallenge(nil, owner)
	if err != nil {
		t.Fatalf("get registration challenge: %v", err)
	}
	if challengeHash == ([32]byte{}) {
		t.Fatalf("registration challenge is zero")
	}
	exists, _, pendingNonce, _, _, err := contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee: %v", err)
	}
	if !exists {
		t.Fatalf("pending tee missing")
	}
	evidence.ChallengeHash = challengeHash
	evidence.SigPubKey = addressAsBytes32(owner)
	if evidence.EnclavePK == ([2][32]byte{}) {
		evidence.EnclavePK = testEnclavePKParts(common.Bytes2Hex(hardwareID[:]))
	}

	mrEnclave := testHash("mr-" + common.Bytes2Hex(hardwareID[:]))

	digest := computeTEERegistrationDigestForTest(t, owner, pendingNonce, mrEnclave, hardwareID, evidence)
	signature := committeeSignatureForDigest(digest, committeeSecret)
	if _, err := contract.RegisterTEE(newTransactor(key, nil), owner, pendingNonce, mrEnclave, hardwareID, evidence, signature); err != nil {
		t.Fatalf("register tee: %v", err)
	}
	backend.Commit()
	return evidence
}

func testEnclavePKParts(prefix string) [2][32]byte {
	return [2][32]byte{
		testHash(prefix + "-epk-x"),
		testHash(prefix + "-epk-y"),
	}
}

func TestPermissionManagerV3StakeAndCancelPendingTEE(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	owner := crypto.PubkeyToAddress(key.PublicKey)

	if status, hardwareID, err := contract.GetTEEStatusByOwner(nil, owner); err != nil {
		t.Fatalf("initial getTEEStatusByOwner: %v", err)
	} else {
		if status != 0 {
			t.Fatalf("initial status = %d, want 0", status)
		}
		if hardwareID != ([32]byte{}) {
			t.Fatalf("initial hardware id = %x, want zero", hardwareID)
		}
	}

	stakeTx, err := contract.StakeTEE(newTransactor(key, big.NewInt(5)))
	if err != nil {
		t.Fatalf("stake tee: %v", err)
	}
	backend.Commit()
	if stakeTx == nil {
		t.Fatalf("stake tx is nil")
	}

	exists, deposit, nonce, createdAt, challengeHashFromPending, err := contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee after stake: %v", err)
	}
	if !exists {
		t.Fatalf("pending tee missing after stake")
	}
	if deposit.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("deposit = %s, want 5", deposit)
	}
	if nonce.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("nonce = %s, want 1", nonce)
	}
	if createdAt.Sign() != 0 {
		t.Fatalf("createdAt = %s, want zero because pending timestamps are event-only", createdAt)
	}
	if challengeHashFromPending == ([32]byte{}) {
		t.Fatalf("pending challenge hash is zero after stake")
	}
	challengeHash, err := contract.GetRegistrationChallenge(nil, owner)
	if err != nil {
		t.Fatalf("get registration challenge after stake: %v", err)
	}
	if challengeHash == ([32]byte{}) {
		t.Fatalf("registration challenge is zero after stake")
	}

	status, hardwareID, err := contract.GetTEEStatusByOwner(nil, owner)
	if err != nil {
		t.Fatalf("status after stake: %v", err)
	}
	if status != 1 {
		t.Fatalf("status after stake = %d, want 1", status)
	}
	if hardwareID != ([32]byte{}) {
		t.Fatalf("hardware id after stake = %x, want zero", hardwareID)
	}

	cancelTx, err := contract.CancelPendingTEE(newTransactor(key, nil))
	if err != nil {
		t.Fatalf("cancel pending tee: %v", err)
	}
	backend.Commit()
	if cancelTx == nil {
		t.Fatalf("cancel tx is nil")
	}

	exists, deposit, nonce, createdAt, _, err = contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee after cancel: %v", err)
	}
	if exists {
		t.Fatalf("pending tee still exists after cancel")
	}
	challengeHash, err = contract.GetRegistrationChallenge(nil, owner)
	if err != nil {
		t.Fatalf("get registration challenge after cancel: %v", err)
	}
	if challengeHash != ([32]byte{}) {
		t.Fatalf("challenge after cancel = %x, want zero", challengeHash)
	}
	if deposit.Sign() != 0 || nonce.Sign() != 0 || createdAt.Sign() != 0 {
		t.Fatalf("pending tee after cancel = exists:%t deposit:%s nonce:%s createdAt:%s, want all zero", exists, deposit, nonce, createdAt)
	}

	status, hardwareID, err = contract.GetTEEStatusByOwner(nil, owner)
	if err != nil {
		t.Fatalf("status after cancel: %v", err)
	}
	if status != 0 {
		t.Fatalf("status after cancel = %d, want 0", status)
	}
	if hardwareID != ([32]byte{}) {
		t.Fatalf("hardware id after cancel = %x, want zero", hardwareID)
	}
}

func TestPermissionManagerV3PendingNonceMonotonicAfterCancel(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	owner := crypto.PubkeyToAddress(key.PublicKey)

	if _, err := contract.StakeTEE(newTransactor(key, big.NewInt(1))); err != nil {
		t.Fatalf("first stake tee: %v", err)
	}
	backend.Commit()

	if _, err := contract.CancelPendingTEE(newTransactor(key, nil)); err != nil {
		t.Fatalf("cancel pending tee: %v", err)
	}
	backend.Commit()

	if _, err := contract.StakeTEE(newTransactor(key, big.NewInt(2))); err != nil {
		t.Fatalf("second stake tee: %v", err)
	}
	backend.Commit()

	exists, deposit, nonce, _, _, err := contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee after restake: %v", err)
	}
	if !exists {
		t.Fatalf("pending tee missing after restake")
	}
	if deposit.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("deposit after restake = %s, want 2", deposit)
	}
	if nonce.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("pending nonce after restake = %s, want 2", nonce)
	}
}

func TestPermissionManagerV3TEERegistrationEvidenceAndExit(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(key, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareID := testHash("exit-tee")
	sigPubKey := sigPubKeyForKey(key)
	evidence := testEvidence("exit-tee")
	registerTEEForTest(t, backend, contract, key, committeeSecret, hardwareID, evidence)

	status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil {
		t.Fatalf("get tee status after register: %v", err)
	}
	if status != 3 || registeredSigPubKey != sigPubKey {
		t.Fatalf("registered status = %d/%x, want Active(3)/%x", status, registeredSigPubKey, sigPubKey)
	}

	eraseProofHash := testHash("erase-proof")
	if _, err := contract.SubmitEraseProof(newTransactor(key, nil), sigPubKey, big.NewInt(0), eraseProofHash, []byte("bad")); err == nil {
		t.Fatalf("submit erase proof accepted invalid signature")
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	eraseDigest, err := contract.ComputeEraseProofDigest(nil, sigPubKey, owner, big.NewInt(0), eraseProofHash)
	if err != nil {
		t.Fatalf("compute erase proof digest: %v", err)
	}
	eraseSignature, err := crypto.Sign(eraseDigest[:], key)
	if err != nil {
		t.Fatalf("sign erase proof digest: %v", err)
	}
	if _, err := contract.SubmitEraseProof(newTransactor(key, nil), sigPubKey, big.NewInt(0), eraseProofHash, eraseSignature); err != nil {
		t.Fatalf("submit erase proof: %v", err)
	}
	backend.Commit()

	status, registeredSigPubKey, err = contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil {
		t.Fatalf("get tee status after exit: %v", err)
	}
	if status != 0 || registeredSigPubKey != ([32]byte{}) {
		t.Fatalf("status after finalized exit = %d/%x, want None(0)/zero", status, registeredSigPubKey)
	}
	registered, err := contract.IsHardwareIdRegistered(nil, hardwareID)
	if err != nil {
		t.Fatalf("is hardware registered after exit: %v", err)
	}
	if registered {
		t.Fatalf("hardware id remains registered after finalized exit")
	}
	sigPubKeyRegistered, err := contract.IsSigPubKeyRegistered(nil, sigPubKey)
	if err != nil {
		t.Fatalf("is signature pubkey registered after exit: %v", err)
	}
	if sigPubKeyRegistered {
		t.Fatalf("signature public key remains registered after finalized exit")
	}

	registerTEEForTest(t, backend, contract, key, committeeSecret, hardwareID, testEvidence("exit-tee-restaked"))
	status, registeredSigPubKey, err = contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil {
		t.Fatalf("get tee status after restake/register: %v", err)
	}
	if status != 2 || registeredSigPubKey != sigPubKey {
		t.Fatalf("restaked status = %d/%x, want Candidate(2)/%x", status, registeredSigPubKey, sigPubKey)
	}
}

func TestPermissionManagerV3RotationMarksDepartedTEEExitedUntilEraseProof(t *testing.T) {
	teeKeys := make([]*ecdsa.PrivateKey, 2)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(20), big.NewInt(1), big.NewInt(1))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{
		testHash("rotation-exit-tee-1"),
		testHash("rotation-exit-tee-2"),
	}
	sigPubKeys := [][32]byte{sigPubKeyForKey(teeKeys[0]), sigPubKeyForKey(teeKeys[1])}
	for i, teeKey := range teeKeys {
		registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareIDs[i], testEvidence(fmt.Sprintf("rotation-exit-tee-%d", i+1)))
	}

	advanceToRotationPreparation(t, backend, contract)
	pkCH := []byte("pk-ch-rotation-exit-1")
	if _, err := contract.SetPkCH(newTransactor(teeKeys[0], nil), pkCH, signPkCHForTest(t, contract, teeKeys[0], pkCH)); err != nil {
		t.Fatalf("trigger first rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[0], big.NewInt(1), hardwareIDs[0])
	triggerRotationAtDeadline(t, backend, contract, ownerKey)

	tee1Owner := crypto.PubkeyToAddress(teeKeys[0].PublicKey)
	if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner); err != nil {
		t.Fatalf("get tee 1 status after first rotation: %v", err)
	} else if status != 3 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("tee 1 status = %d/%x, want Active(3)/%x", status, registeredSigPubKey, sigPubKeys[0])
	}

	advanceToRotationPreparation(t, backend, contract)
	if _, err := contract.PrepareRotation(newTransactor(teeKeys[0], nil)); err != nil {
		t.Fatalf("trigger second rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[1], big.NewInt(2), hardwareIDs[1])
	triggerRotationAtDeadlineWithOwner(t, backend, contract, ownerKey, "rotation-exit-second-trigger")

	if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner); err != nil {
		t.Fatalf("get tee 1 status after leaving epoch: %v", err)
	} else if status != 4 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("tee 1 status after leaving epoch = %d/%x, want Exited(4)/%x", status, registeredSigPubKey, sigPubKeys[0])
	}
	if _, err := contract.StakeTEE(newTransactor(teeKeys[0], big.NewInt(5))); err == nil {
		t.Fatalf("stake accepted before exited tee submitted erase proof")
	}

	eraseProofHash := testHash("rotation-exit-erase-proof")
	eraseDigest, err := contract.ComputeEraseProofDigest(nil, sigPubKeys[0], tee1Owner, big.NewInt(1), eraseProofHash)
	if err != nil {
		t.Fatalf("compute erase proof digest: %v", err)
	}
	eraseSignature, err := crypto.Sign(eraseDigest[:], teeKeys[0])
	if err != nil {
		t.Fatalf("sign erase proof digest: %v", err)
	}
	if _, err := contract.SubmitEraseProof(newTransactor(teeKeys[0], nil), sigPubKeys[0], big.NewInt(1), eraseProofHash, eraseSignature); err != nil {
		t.Fatalf("submit erase proof for departed tee: %v", err)
	}
	backend.Commit()

	if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner); err != nil {
		t.Fatalf("get tee 1 status after erase proof: %v", err)
	} else if status != 0 || registeredSigPubKey != ([32]byte{}) {
		t.Fatalf("tee 1 status after erase proof = %d/%x, want None(0)/zero", status, registeredSigPubKey)
	}
	if _, err := contract.StakeTEE(newTransactor(teeKeys[0], big.NewInt(5))); err != nil {
		t.Fatalf("stake after erase proof: %v", err)
	}
	backend.Commit()
}

func TestPermissionManagerV3TEEEnclavePKRoundTrip(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	committeeSecret := big.NewInt(23)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(key, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareID := testHash("enclave-pk-roundtrip")
	evidence := registerTEEForTest(t, backend, contract, key, committeeSecret, hardwareID, testEvidence("enclave-pk-roundtrip"))

	got, err := contract.GetTEEEnclavePK(nil, evidence.SigPubKey)
	if err != nil {
		t.Fatalf("get tee enclave pk: %v", err)
	}
	if got != evidence.EnclavePK {
		t.Fatalf("enclave pk = %x, want %x", got, evidence.EnclavePK)
	}
}

func TestPermissionManagerV3RegisterTEERejectsZeroEnclavePK(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	committeeSecret := big.NewInt(29)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(key, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	owner := crypto.PubkeyToAddress(key.PublicKey)
	if _, err := contract.StakeTEE(newTransactor(key, big.NewInt(5))); err != nil {
		t.Fatalf("stake tee: %v", err)
	}
	backend.Commit()

	challengeHash, err := contract.GetRegistrationChallenge(nil, owner)
	if err != nil {
		t.Fatalf("get registration challenge: %v", err)
	}
	exists, _, pendingNonce, _, _, err := contract.GetPendingTEE(nil, owner)
	if err != nil {
		t.Fatalf("get pending tee: %v", err)
	}
	if !exists {
		t.Fatalf("pending tee missing")
	}

	hardwareID := testHash("zero-enclave-pk")
	mrEnclave := testHash("mr-zero-enclave-pk")
	evidence := testEvidence("zero-enclave-pk")
	evidence.SigPubKey = addressAsBytes32(owner)
	evidence.VrfPubKey = addressAsBytes32(owner)
	evidence.EnclavePK = [2][32]byte{}
	evidence.ChallengeHash = challengeHash

	digest := computeTEERegistrationDigestForTest(t, owner, pendingNonce, mrEnclave, hardwareID, evidence)
	signature := committeeSignatureForDigest(digest, committeeSecret)
	if _, err := contract.RegisterTEE(newTransactor(key, nil), owner, pendingNonce, mrEnclave, hardwareID, evidence, signature); err == nil {
		t.Fatalf("expected zero enclave pk registration to fail")
	}
}

func TestPermissionManagerV3RotationEmitsActiveSetBeforeEraseEvents(t *testing.T) {
	teeKeys := make([]*ecdsa.PrivateKey, 2)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(20), big.NewInt(1), big.NewInt(1))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{testHash("rotation-order-tee-1"), testHash("rotation-order-tee-2")}
	for i, teeKey := range teeKeys {
		registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareIDs[i], testEvidence(fmt.Sprintf("rotation-order-tee-%d", i+1)))
	}

	advanceToRotationPreparation(t, backend, contract)
	pkCH := []byte("pk-ch-rotation-order")
	if _, err := contract.SetPkCH(newTransactor(teeKeys[0], nil), pkCH, signPkCHForTest(t, contract, teeKeys[0], pkCH)); err != nil {
		t.Fatalf("trigger first rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[0], big.NewInt(1), hardwareIDs[0])
	triggerRotationAtDeadline(t, backend, contract, ownerKey)

	advanceToRotationPreparation(t, backend, contract)
	if _, err := contract.PrepareRotation(newTransactor(teeKeys[0], nil)); err != nil {
		t.Fatalf("trigger second rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[1], big.NewInt(2), hardwareIDs[1])

	deadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("rotation deadline: %v", err)
	}
	mineUntilBlock(t, backend, new(big.Int).Sub(deadline, big.NewInt(1)))
	tx, err := contract.SetTrustedMREnclave(newTransactor(ownerKey, nil), testHash("rotation-order-second-trigger"), true)
	if err != nil {
		t.Fatalf("trigger second rotation: %v", err)
	}
	backend.Commit()
	receipt, err := backend.TransactionReceipt(context.Background(), tx.Hash())
	if err != nil {
		t.Fatalf("get second rotation receipt: %v", err)
	}

	activeSetRotatedSig := crypto.Keccak256Hash([]byte("ActiveSetRotated(uint256,bytes32,uint256,uint256,bytes32,bytes32,bytes32[])"))
	exitPendingSig := crypto.Keccak256Hash([]byte("TEEExitPending(bytes32,address,uint256,uint256,uint256)"))
	eraseDeadlineSig := crypto.Keccak256Hash([]byte("TEEEraseProofDeadlineSet(bytes32,address,uint256,uint256)"))
	activeIndex, exitIndex, eraseIndex := -1, -1, -1
	for i, log := range receipt.Logs {
		if len(log.Topics) == 0 {
			continue
		}
		switch log.Topics[0] {
		case activeSetRotatedSig:
			activeIndex = i
		case exitPendingSig:
			exitIndex = i
		case eraseDeadlineSig:
			eraseIndex = i
		}
	}
	if activeIndex < 0 || exitIndex < 0 || eraseIndex < 0 {
		t.Fatalf("missing rotation/erase events: active=%d exit=%d erase=%d", activeIndex, exitIndex, eraseIndex)
	}
	if !(activeIndex < exitIndex && activeIndex < eraseIndex) {
		t.Fatalf("event order active=%d exit=%d erase=%d, want active set before erase events", activeIndex, exitIndex, eraseIndex)
	}
}

func setupDepartedTEEAwaitingEraseProof(t *testing.T) (*backends.SimulatedBackend, *Contracts, *ecdsa.PrivateKey, []*ecdsa.PrivateKey, [][32]byte, *big.Int) {
	t.Helper()

	teeKeys := make([]*ecdsa.PrivateKey, 2)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(20), big.NewInt(1), big.NewInt(1))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{
		testHash("departed-timeout-tee-1"),
		testHash("departed-timeout-tee-2"),
	}
	for i, teeKey := range teeKeys {
		registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareIDs[i], testEvidence(fmt.Sprintf("departed-timeout-tee-%d", i+1)))
	}

	advanceToRotationPreparation(t, backend, contract)
	pkCH := []byte("pk-ch-timeout-1")
	if _, err := contract.SetPkCH(newTransactor(teeKeys[0], nil), pkCH, signPkCHForTest(t, contract, teeKeys[0], pkCH)); err != nil {
		t.Fatalf("trigger first rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[0], big.NewInt(1), hardwareIDs[0])
	triggerRotationAtDeadline(t, backend, contract, ownerKey)

	advanceToRotationPreparation(t, backend, contract)
	if _, err := contract.PrepareRotation(newTransactor(teeKeys[0], nil)); err != nil {
		t.Fatalf("trigger second rotation preparation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKeys[1], big.NewInt(2), hardwareIDs[1])
	triggerRotationAtDeadline(t, backend, contract, ownerKey)

	return backend, contract, ownerKey, teeKeys, hardwareIDs, committeeSecret
}

func TestPermissionManagerV3EraseProofGraceBlocksFixedTo50(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, contract, _, _ := deployPermissionManagerV3ForTestWithParams(t, []*ecdsa.PrivateKey{key}, big.NewInt(20), big.NewInt(1), big.NewInt(1))

	graceBlocks, err := contract.EraseProofGraceBlocks(nil)
	if err != nil {
		t.Fatalf("get erase proof grace blocks: %v", err)
	}
	if graceBlocks.Cmp(big.NewInt(50)) != 0 {
		t.Fatalf("erase proof grace blocks = %s, want 50", graceBlocks)
	}
}

func TestPermissionManagerV3EraseProofTimeoutAutoSlashesOnRotateTrigger(t *testing.T) {
	backend, contract, ownerKey, teeKeys, hardwareIDs, _ := setupDepartedTEEAwaitingEraseProof(t)
	sigPubKeys := sigPubKeysForKeys(teeKeys)
	_ = hardwareIDs

	tee1Owner := crypto.PubkeyToAddress(teeKeys[0].PublicKey)
	if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner); err != nil {
		t.Fatalf("get tee status after rotation: %v", err)
	} else if status != 4 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("departed tee status = %d/%x, want Exited(4)/%x", status, registeredSigPubKey, sigPubKeys[0])
	}

	exitInfo, err := contract.GetTEEExitInfo(nil, sigPubKeys[0])
	if err != nil {
		t.Fatalf("get tee exit info: %v", err)
	}
	if !exitInfo.Pending || exitInfo.TeeOwner != tee1Owner || exitInfo.ExitEpoch.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("unexpected exit info: pending=%v owner=%s epoch=%s", exitInfo.Pending, exitInfo.TeeOwner, exitInfo.ExitEpoch)
	}
	if exitInfo.EraseProofDeadlineBlock.Sign() == 0 {
		t.Fatalf("erase proof deadline not recorded")
	}
	if overdue, err := contract.IsEraseProofOverdue(nil, sigPubKeys[0]); err != nil {
		t.Fatalf("is erase proof overdue: %v", err)
	} else if overdue {
		t.Fatalf("erase proof is overdue before deadline")
	}

	if _, err := contract.SetTrustedMREnclave(newTransactor(ownerKey, nil), testHash("pre-timeout-trigger"), true); err != nil {
		t.Fatalf("pre-timeout rotate trigger: %v", err)
	}
	backend.Commit()
	if status, _, err := contract.GetTEEStatusByOwner(nil, tee1Owner); err != nil {
		t.Fatalf("get tee status after pre-timeout trigger: %v", err)
	} else if status != 4 {
		t.Fatalf("pre-timeout trigger changed status to %d, want Exited(4)", status)
	}

	mineUntilBlock(t, backend, new(big.Int).Add(exitInfo.EraseProofDeadlineBlock, big.NewInt(1)))
	if overdue, err := contract.IsEraseProofOverdue(nil, sigPubKeys[0]); err != nil {
		t.Fatalf("is erase proof overdue after deadline: %v", err)
	} else if !overdue {
		t.Fatalf("erase proof is not overdue after deadline")
	}
	if _, err := contract.SetTrustedMREnclave(newTransactor(ownerKey, nil), testHash("post-timeout-trigger"), true); err != nil {
		t.Fatalf("post-timeout rotate trigger: %v", err)
	}
	backend.Commit()

	status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner)
	if err != nil {
		t.Fatalf("get tee status after timeout slash: %v", err)
	}
	if status != 5 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("timeout slashed status = %d/%x, want Revoked(5)/%x", status, registeredSigPubKey, sigPubKeys[0])
	}
	_, _, _, deposit, active, err := contract.GetTEEInfo(nil, sigPubKeys[0])
	if err != nil {
		t.Fatalf("get tee info after timeout slash: %v", err)
	}
	if deposit.Sign() != 0 || active {
		t.Fatalf("timeout slashed tee deposit/active = %s/%v, want 0/false", deposit, active)
	}
	exitInfo, err = contract.GetTEEExitInfo(nil, sigPubKeys[0])
	if err != nil {
		t.Fatalf("get tee exit info after timeout slash: %v", err)
	}
	if exitInfo.Pending {
		t.Fatalf("exit record remains pending after timeout slash")
	}
}

func TestPermissionManagerV3RevokeExitedTEEAfterEraseDeadline(t *testing.T) {
	backend, contract, ownerKey, teeKeys, hardwareIDs, committeeSecret := setupDepartedTEEAwaitingEraseProof(t)
	sigPubKeys := sigPubKeysForKeys(teeKeys)
	_ = hardwareIDs

	exitInfo, err := contract.GetTEEExitInfo(nil, sigPubKeys[0])
	if err != nil {
		t.Fatalf("get tee exit info: %v", err)
	}
	reasonHash := testHash("erase-timeout-revoke")
	revocationDigest, err := contract.ComputeRevocationDigest(nil, sigPubKeys[0], reasonHash, big.NewInt(1), big.NewInt(1))
	if err != nil {
		t.Fatalf("compute revocation digest: %v", err)
	}
	signature := committeeSignatureForDigest(revocationDigest, committeeSecret)
	if _, err := contract.RevokeTEE(newTransactor(ownerKey, nil), sigPubKeys[0], reasonHash, big.NewInt(1), signature); err == nil {
		t.Fatalf("revoke exited tee accepted before erase proof deadline")
	}

	mineUntilBlock(t, backend, new(big.Int).Add(exitInfo.EraseProofDeadlineBlock, big.NewInt(1)))
	if _, err := contract.RevokeTEE(newTransactor(ownerKey, nil), sigPubKeys[0], reasonHash, big.NewInt(1), signature); err != nil {
		t.Fatalf("revoke exited tee after erase proof deadline: %v", err)
	}
	backend.Commit()

	tee1Owner := crypto.PubkeyToAddress(teeKeys[0].PublicKey)
	status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, tee1Owner)
	if err != nil {
		t.Fatalf("get tee status after revoke: %v", err)
	}
	if status != 5 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("revoked exited tee status = %d/%x, want Revoked(5)/%x", status, registeredSigPubKey, sigPubKeys[0])
	}
	exitInfo, err = contract.GetTEEExitInfo(nil, sigPubKeys[0])
	if err != nil {
		t.Fatalf("get tee exit info after revoke: %v", err)
	}
	if exitInfo.Pending {
		t.Fatalf("exit record remains pending after revoke")
	}
}

func TestPermissionManagerV3TEERevocationKeepsMinimalState(t *testing.T) {
	backend, contract, _, key := deployPermissionManagerV3ForTest(t)
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(key, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareID := testHash("revoke-tee")
	sigPubKey := sigPubKeyForKey(key)
	registerTEEForTest(t, backend, contract, key, committeeSecret, hardwareID, testEvidence("revoke-tee"))

	reasonHash := testHash("bad-quote")
	if _, err := contract.RevokeTEE(newTransactor(key, nil), sigPubKey, reasonHash, big.NewInt(2), [2]*big.Int{big.NewInt(0), big.NewInt(0)}); err == nil {
		t.Fatalf("revoke accepted without committee aggregate signature")
	}

	revocationDigest, err := contract.ComputeRevocationDigest(nil, sigPubKey, reasonHash, big.NewInt(2), big.NewInt(1))
	if err != nil {
		t.Fatalf("compute revocation digest: %v", err)
	}
	signature := committeeSignatureForDigest(revocationDigest, committeeSecret)
	if _, err := contract.RevokeTEE(newTransactor(key, nil), sigPubKey, reasonHash, big.NewInt(2), signature); err != nil {
		t.Fatalf("revoke tee: %v", err)
	}
	backend.Commit()

	status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil {
		t.Fatalf("get tee status after revoke: %v", err)
	}
	if status != 5 || registeredSigPubKey != sigPubKey {
		t.Fatalf("revoked status = %d/%x, want Revoked(5)/%x", status, registeredSigPubKey, sigPubKey)
	}
}

func TestPermissionManagerV3BootstrapsInitialActiveSetOnRegistration(t *testing.T) {
	teeKeys := make([]*ecdsa.PrivateKey, 2)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(1000), big.NewInt(1), big.NewInt(2))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{
		testHash("bootstrap-tee-1"),
		testHash("bootstrap-tee-2"),
	}
	sigPubKeys := sigPubKeysForKeys(teeKeys)
	registerTEEForTest(t, backend, contract, teeKeys[0], committeeSecret, hardwareIDs[0], testEvidence("bootstrap-tee-1"))
	if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(teeKeys[0].PublicKey)); err != nil {
		t.Fatalf("get first tee status: %v", err)
	} else if status != 2 || registeredSigPubKey != sigPubKeys[0] {
		t.Fatalf("first tee status = %d/%x, want Candidate(2)/%x before threshold", status, registeredSigPubKey, sigPubKeys[0])
	}
	activeSet, err := contract.GetActiveSet(nil, big.NewInt(0))
	if err != nil {
		t.Fatalf("get active set before threshold: %v", err)
	}
	if len(activeSet) != 0 {
		t.Fatalf("active set before threshold = %x, want empty", activeSet)
	}

	registerTEEForTest(t, backend, contract, teeKeys[1], committeeSecret, hardwareIDs[1], testEvidence("bootstrap-tee-2"))
	activeSet, err = contract.GetActiveSet(nil, big.NewInt(0))
	if err != nil {
		t.Fatalf("get active set after threshold: %v", err)
	}
	if len(activeSet) != 2 || activeSet[0] != sigPubKeys[0] || activeSet[1] != sigPubKeys[1] {
		t.Fatalf("initial active set = %x, want [%x %x]", activeSet, sigPubKeys[0], sigPubKeys[1])
	}
	for i, teeKey := range teeKeys {
		if status, registeredSigPubKey, err := contract.GetTEEStatusByOwner(nil, crypto.PubkeyToAddress(teeKey.PublicKey)); err != nil {
			t.Fatalf("get tee %d status after bootstrap: %v", i, err)
		} else if status != 3 || registeredSigPubKey != sigPubKeys[i] {
			t.Fatalf("tee %d status after bootstrap = %d/%x, want Active(3)/%x", i, status, registeredSigPubKey, sigPubKeys[i])
		}
	}
	if epoch, err := contract.CurrentEpoch(nil); err != nil {
		t.Fatalf("get current epoch: %v", err)
	} else if epoch.Sign() != 0 {
		t.Fatalf("current epoch after bootstrap = %s, want 0", epoch)
	}
}

func TestPermissionManagerV3UsesSteadyEpochLengthAfterBootstrapRotation(t *testing.T) {
	teeKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate tee key: %v", err)
	}
	bootstrapLength := big.NewInt(50)
	steadyLength := big.NewInt(12)
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithEpochs(t, []*ecdsa.PrivateKey{teeKey}, bootstrapLength, steadyLength, big.NewInt(1), big.NewInt(1))

	if got, err := contract.SteadyEpochLength(nil); err != nil {
		t.Fatalf("steady epoch length: %v", err)
	} else if got.Cmp(steadyLength) != 0 {
		t.Fatalf("steady epoch length = %s, want %s", got, steadyLength)
	}
	lastRotation, err := contract.LastRotationBlock(nil)
	if err != nil {
		t.Fatalf("last rotation block: %v", err)
	}
	firstDeadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("first rotation deadline: %v", err)
	}
	if want := new(big.Int).Add(lastRotation, bootstrapLength); firstDeadline.Cmp(want) != 0 {
		t.Fatalf("first deadline = %s, want %s", firstDeadline, want)
	}
	params, err := contract.GetSystemParams(nil)
	if err != nil {
		t.Fatalf("get bootstrap params: %v", err)
	}
	if params.ConfiguredEpochLength.Cmp(bootstrapLength) != 0 {
		t.Fatalf("bootstrap configured epoch length = %s, want %s", params.ConfiguredEpochLength, bootstrapLength)
	}

	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()
	hardwareID := testHash("steady-epoch-tee")
	registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareID, testEvidence("steady-epoch-tee"))

	advanceToRotationPreparation(t, backend, contract)
	if _, err := contract.PrepareRotation(newTransactor(ownerKey, nil)); err != nil {
		t.Fatalf("prepare rotation: %v", err)
	}
	backend.Commit()
	submitVRFForTest(t, backend, contract, teeKey, big.NewInt(1), hardwareID)
	triggerRotationAtDeadlineWithOwner(t, backend, contract, ownerKey, "steady-epoch-rotation")

	params, err = contract.GetSystemParams(nil)
	if err != nil {
		t.Fatalf("get steady params: %v", err)
	}
	if params.Epoch.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("epoch = %s, want 1", params.Epoch)
	}
	if params.ConfiguredEpochLength.Cmp(steadyLength) != 0 {
		t.Fatalf("steady configured epoch length = %s, want %s", params.ConfiguredEpochLength, steadyLength)
	}
	lastRotation, err = contract.LastRotationBlock(nil)
	if err != nil {
		t.Fatalf("last rotation block after rotation: %v", err)
	}
	secondDeadline, err := contract.RotationDeadlineBlock(nil)
	if err != nil {
		t.Fatalf("second rotation deadline: %v", err)
	}
	if want := new(big.Int).Add(lastRotation, steadyLength); secondDeadline.Cmp(want) != 0 {
		t.Fatalf("second deadline = %s, want %s", secondDeadline, want)
	}
}

func TestPermissionManagerV3VRFRotationSelectsHighestResults(t *testing.T) {
	teeKeys := make([]*ecdsa.PrivateKey, 3)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(20), big.NewInt(1), big.NewInt(2))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{
		testHash("vrf-tee-1"),
		testHash("vrf-tee-2"),
		testHash("vrf-tee-3"),
	}
	sigPubKeys := sigPubKeysForKeys(teeKeys)
	for i, teeKey := range teeKeys {
		registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareIDs[i], testEvidence(fmt.Sprintf("vrf-tee-%d", i+1)))
	}

	advanceToRotationPreparation(t, backend, contract)
	ownerPkCH := []byte("pk-ch-vrf-owner")
	if _, err := contract.SetPkCH(newTransactor(ownerKey, nil), ownerPkCH, signPkCHForTest(t, contract, ownerKey, ownerPkCH)); err == nil {
		t.Fatalf("owner set pkCH accepted even though owner is not a current active TEE")
	}
	pkCH := []byte("pk-ch-vrf")
	if _, err := contract.SetPkCH(newTransactor(teeKeys[0], nil), pkCH, signPkCHForTest(t, contract, teeKeys[0], pkCH)); err != nil {
		t.Fatalf("trigger rotation preparation: %v", err)
	}
	backend.Commit()

	submissions := []struct {
		key       *ecdsa.PrivateKey
		sigPubKey [32]byte
	}{
		{teeKeys[0], sigPubKeys[0]},
		{teeKeys[1], sigPubKeys[1]},
		{teeKeys[2], sigPubKeys[2]},
	}
	type vrfRank struct {
		sigPubKey [32]byte
		result    [32]byte
	}
	ranks := make([]vrfRank, 0, len(submissions))
	for _, submission := range submissions {
		result := submitVRFForTest(t, backend, contract, submission.key, big.NewInt(1), submission.sigPubKey)
		ranks = append(ranks, vrfRank{sigPubKey: submission.sigPubKey, result: result})
	}
	sort.Slice(ranks, func(i, j int) bool {
		if cmp := bytes.Compare(ranks[i].result[:], ranks[j].result[:]); cmp != 0 {
			return cmp > 0
		}
		return bytes.Compare(ranks[i].sigPubKey[:], ranks[j].sigPubKey[:]) > 0
	})

	triggerRotationAtDeadlineWithOwner(t, backend, contract, ownerKey, "vrf-ranking-rotation-trigger")
	activeSet, err := contract.GetActiveSet(nil, big.NewInt(1))
	if err != nil {
		t.Fatalf("get active set: %v", err)
	}
	if len(activeSet) != 2 {
		t.Fatalf("active set length = %d, want 2", len(activeSet))
	}
	if activeSet[0] != ranks[0].sigPubKey || activeSet[1] != ranks[1].sigPubKey {
		t.Fatalf("active set = %x, want descending VRF results [%x %x]", activeSet, ranks[0].sigPubKey, ranks[1].sigPubKey)
	}
}

func TestPermissionManagerV3DefersRotationUntilActiveLimitVRFs(t *testing.T) {
	teeKeys := make([]*ecdsa.PrivateKey, 2)
	for i := range teeKeys {
		var err error
		teeKeys[i], err = crypto.GenerateKey()
		if err != nil {
			t.Fatalf("generate tee key %d: %v", i, err)
		}
	}
	backend, contract, _, ownerKey := deployPermissionManagerV3ForTestWithParams(t, teeKeys, big.NewInt(30), big.NewInt(1), big.NewInt(2))
	committeeSecret := big.NewInt(17)
	if _, err := contract.RegisterCommitteePubKey(newTransactor(ownerKey, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareIDs := [][32]byte{
		testHash("defer-rotation-tee-1"),
		testHash("defer-rotation-tee-2"),
	}
	for i, teeKey := range teeKeys {
		registerTEEForTest(t, backend, contract, teeKey, committeeSecret, hardwareIDs[i], testEvidence(fmt.Sprintf("defer-rotation-tee-%d", i+1)))
	}

	advanceToRotationPreparation(t, backend, contract)
	if _, err := contract.PrepareRotation(newTransactor(ownerKey, nil)); err != nil {
		t.Fatalf("prepare rotation: %v", err)
	}
	backend.Commit()

	submitVRFForTest(t, backend, contract, teeKeys[0], big.NewInt(1), hardwareIDs[0])
	triggerRotationAtDeadlineWithOwner(t, backend, contract, ownerKey, "defer-rotation-trigger")
	if epoch, err := contract.CurrentEpoch(nil); err != nil {
		t.Fatalf("current epoch after partial VRFs: %v", err)
	} else if epoch.Sign() != 0 {
		t.Fatalf("epoch after partial VRFs = %s, want 0", epoch)
	}
	if activeSet, err := contract.GetActiveSet(nil, big.NewInt(0)); err != nil {
		t.Fatalf("get epoch 0 active set: %v", err)
	} else if len(activeSet) != 2 {
		t.Fatalf("epoch 0 active set len after deferred rotation = %d, want 2", len(activeSet))
	}

	submitVRFForTest(t, backend, contract, teeKeys[1], big.NewInt(1), hardwareIDs[1])
	if epoch, err := contract.CurrentEpoch(nil); err != nil {
		t.Fatalf("current epoch after full VRFs: %v", err)
	} else if epoch.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("epoch after full VRFs = %s, want 1", epoch)
	}
	if activeSet, err := contract.GetActiveSet(nil, big.NewInt(1)); err != nil {
		t.Fatalf("get epoch 1 active set: %v", err)
	} else if len(activeSet) != 2 {
		t.Fatalf("epoch 1 active set len = %d, want 2", len(activeSet))
	}
}

func TestPermissionManagerV3RedactionStateMachine(t *testing.T) {
	backend, contract, address, key := deployPermissionManagerV3ForTest(t)
	committeeSecret := big.NewInt(17)

	if _, err := contract.RegisterCommitteePubKey(newTransactor(key, nil), committeePubKeyForSecret(committeeSecret)); err != nil {
		t.Fatalf("register committee pubkey: %v", err)
	}
	backend.Commit()

	hardwareID := testHash("tee-1")
	sigPubKey := sigPubKeyForKey(key)
	registerTEEForTest(t, backend, contract, key, committeeSecret, hardwareID, testEvidence("tee-1"))

	if ready, err := contract.IsSystemReady(nil); err != nil {
		t.Fatalf("is system ready before pkCH/rotation: %v", err)
	} else if ready {
		t.Fatalf("system is ready before pkCH and active set rotation")
	}

	var params struct {
		CommitteeVersion      *big.Int
		PkCHHash              [32]byte
		CurrentPkCHVersion    *big.Int
		ConfiguredEpochLength *big.Int
		Epoch                 *big.Int
		ActiveThreshold       *big.Int
		ActiveLimit           *big.Int
	}
	pkCH := []byte("pk-ch-v1")
	if _, err := contract.SetPkCH(newTransactor(key, nil), pkCH, []byte("bad")); err == nil {
		t.Fatalf("set pkCH accepted invalid signature")
	}
	if _, err := contract.SetPkCH(newTransactor(key, nil), pkCH, signPkCHForTest(t, contract, key, pkCH)); err != nil {
		t.Fatalf("set pkCH: %v", err)
	}
	backend.Commit()
	replacementPkCH := []byte("pk-ch-v2-rejected")
	if _, err := contract.SetPkCH(newTransactor(key, nil), replacementPkCH, signPkCHForTest(t, contract, key, replacementPkCH)); err == nil {
		t.Fatalf("set pkCH accepted second initialization")
	}

	advanceToRotationPreparation(t, backend, contract)
	prepTx, err := contract.PrepareRotation(newTransactor(key, nil))
	if err != nil {
		t.Fatalf("trigger rotation preparation: %v", err)
	}
	backend.Commit()
	receipt, err := backend.TransactionReceipt(context.Background(), prepTx.Hash())
	if err != nil {
		t.Fatalf("get rotation prep receipt: %v", err)
	}
	foundPrepEvent := false
	prepEventSig := crypto.Keccak256Hash([]byte("TEERotationPreparationRequested(uint256,uint256,uint256,uint256,uint256,bytes32,bytes32)"))
	for _, log := range receipt.Logs {
		if len(log.Topics) == 0 || log.Topics[0] != prepEventSig {
			continue
		}
		if event, parseErr := contract.ParseTEERotationPreparationRequested(*log); parseErr == nil {
			foundPrepEvent = true
			if event.NextEpoch.Cmp(big.NewInt(1)) != 0 || event.CandidateCount.Cmp(big.NewInt(1)) != 0 || event.Seed == ([32]byte{}) {
				t.Fatalf("unexpected prep event: nextEpoch=%s candidateCount=%s seed=%x", event.NextEpoch, event.CandidateCount, event.Seed)
			}
		}
	}
	if !foundPrepEvent {
		t.Fatalf("rotation preparation event not emitted")
	}

	submitVRFForTest(t, backend, contract, key, big.NewInt(1), hardwareID)
	triggerRotationAtDeadline(t, backend, contract, key)

	params, err = contract.GetSystemParams(nil)
	if err != nil {
		t.Fatalf("get system params: %v", err)
	}
	if params.CommitteeVersion.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("committee version = %s, want 1", params.CommitteeVersion)
	}
	if params.CurrentPkCHVersion.Sign() != 0 {
		t.Fatalf("pkCH version = %s, want fixed zero", params.CurrentPkCHVersion)
	}
	if params.Epoch.Sign() == 0 {
		t.Fatalf("epoch = %s, want positive after automatic rotation", params.Epoch)
	}
	if params.ActiveThreshold.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("active threshold = %s, want 1", params.ActiveThreshold)
	}
	if ready, err := contract.IsSystemReady(nil); err != nil {
		t.Fatalf("is system ready after rotation: %v", err)
	} else if !ready {
		activeSet, activeErr := contract.GetActiveSet(nil, params.Epoch)
		if activeErr != nil {
			t.Fatalf("system is not ready after pkCH and active set rotation; get active set: %v", activeErr)
		}
		candidates, candidateErr := contract.GetCandidateTEEs(nil)
		if candidateErr != nil {
			t.Fatalf("system is not ready after pkCH and active set rotation; get candidates: %v", candidateErr)
		}
		t.Fatalf("system is not ready after pkCH and active set rotation; epoch=%s active=%d candidates=%d limit=%s threshold=%s", params.Epoch, len(activeSet), len(candidates), params.ActiveLimit, params.ActiveThreshold)
	}

	activeSet, err := contract.GetActiveSet(nil, params.Epoch)
	if err != nil {
		t.Fatalf("get active set: %v", err)
	}
	if len(activeSet) != 1 || activeSet[0] != sigPubKey {
		t.Fatalf("active set = %x, want [%x]", activeSet, sigPubKey)
	}
	activeSetHash, err := contract.GetActiveSetHash(nil, params.Epoch)
	if err != nil {
		t.Fatalf("get active set hash: %v", err)
	}

	req := PermissionManagerV3ReqFields{
		Version: 1,
		Epoch:   uint64(params.Epoch.Uint64()),
		ReqID:   testHash("req-1"),
		BodyHashes: [7][32]byte{
			testHash("tx"),
			testHash("m"),
			testHash("m-prime"),
			testHash("pos"),
			testHash("meta"),
			activeSetHash,
			params.PkCHHash,
		},
	}
	dReq, err := contract.ComputeDReq(nil, req)
	if err != nil {
		t.Fatalf("compute d_req on chain: %v", err)
	}
	if want := computeDReqForTest(t, address, req); dReq != want {
		t.Fatalf("d_req = %x, want %x", dReq, want)
	}

	dPolicy := testHash("policy-v1")
	expiresAt := new(big.Int).Add(latestTimestamp(t, backend), big.NewInt(1000))
	authDigest, err := contract.ComputeAuthorizationDigest(nil, dReq, dPolicy, params.CommitteeVersion, expiresAt)
	if err != nil {
		t.Fatalf("compute auth digest: %v", err)
	}
	signature := committeeSignatureForDigest(authDigest, committeeSecret)

	if _, err := contract.SubmitRedactAuthorization(newTransactor(key, nil), req, dPolicy, expiresAt, signature); err != nil {
		t.Fatalf("submit redact authorization: %v", err)
	}
	backend.Commit()

	record, err := contract.GetRedactRequest(nil, req.ReqID)
	if err != nil {
		t.Fatalf("get redact request: %v", err)
	}
	if record.StoredReqID != req.ReqID || record.DReq != dReq {
		t.Fatalf("stored authorization does not match submitted request")
	}
	if record.DPolicy != ([32]byte{}) {
		t.Fatalf("dPolicy should be event-only, got %x", record.DPolicy)
	}
	if record.Status != 1 {
		t.Fatalf("authorization status = %d, want Authorized(1)", record.Status)
	}
	if record.AuthorizedAt.Sign() != 0 || record.ExpiresAt.Cmp(expiresAt) != 0 {
		t.Fatalf("authorization timing = authorized:%s expires:%s, want zero/%s", record.AuthorizedAt, record.ExpiresAt, expiresAt)
	}
	tampered := req
	tampered.ReqID = testHash("req-2")
	tampered.BodyHashes[1] = testHash("tampered-m")
	if _, err := contract.SubmitRedactAuthorization(newTransactor(key, nil), tampered, dPolicy, expiresAt, signature); err == nil {
		t.Fatalf("tampered request accepted with signature bound to original d_req")
	}

	rejected := req
	rejected.ReqID = testHash("req-rejected")
	rejectedDReq, err := contract.ComputeDReq(nil, rejected)
	if err != nil {
		t.Fatalf("compute rejected d_req: %v", err)
	}
	rejectReason := testHash("policy-reject")
	rejectDigest, err := contract.ComputeRejectionDigest(nil, rejectedDReq, dPolicy, rejectReason, params.CommitteeVersion)
	if err != nil {
		t.Fatalf("compute rejection digest: %v", err)
	}
	rejectSignature := committeeSignatureForDigest(rejectDigest, committeeSecret)
	if _, err := contract.RejectRedactRequest(newTransactor(key, nil), rejected, dPolicy, rejectReason, rejectSignature); err != nil {
		t.Fatalf("reject redact request: %v", err)
	}
	backend.Commit()
	rejectedRecord, err := contract.GetRedactRequest(nil, rejected.ReqID)
	if err != nil {
		t.Fatalf("get rejected redact request: %v", err)
	}
	if rejectedRecord.Status != 3 || rejectedRecord.RejectReasonHash != ([32]byte{}) {
		t.Fatalf("rejected record = status:%d reason:%x, want Rejected(3)/zero audit reason", rejectedRecord.Status, rejectedRecord.RejectReasonHash)
	}
	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		rejected.ReqID,
		testHash("r-prime-rejected"),
		testHash("proof-rejected"),
		testHash("result-rejected"),
		[]byte("pi-rejected"),
	); err == nil {
		t.Fatalf("execute redaction accepted rejected request")
	}

	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		req.ReqID,
		testHash("r-prime-raw-pi"),
		testHash("proof-raw-pi"),
		testHash("result-raw-pi"),
		[]byte("pi-req-a"),
	); err == nil {
		t.Fatalf("execute redaction accepted unstructured pi_req_A")
	}

	inactiveKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate inactive tee key: %v", err)
	}
	inactivePiReqA := piReqAForTest(t, dReq, inactiveKey, []byte("share-from-inactive-tee"))
	inactiveProofHash := computeRedactionProofHashForTest(t, req.ReqID, dReq, activeSetHash, inactivePiReqA)
	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		req.ReqID,
		testHash("r-prime-inactive"),
		inactiveProofHash,
		testHash("result-inactive"),
		inactivePiReqA,
	); err == nil {
		t.Fatalf("execute redaction accepted share proof from inactive sigPubKey")
	}

	badSignaturePiReqA := encodePiReqAForTest(t, []shareProofForTest{{
		SigPubKey: addressAsBytes32(crypto.PubkeyToAddress(key.PublicKey)),
		SI:        []byte("share-from-active-tee"),
		Sigma:     signShareProofForTest(t, testHash("wrong-d-req"), key, []byte("share-from-active-tee")),
	}})
	badSignatureProofHash := computeRedactionProofHashForTest(t, req.ReqID, dReq, activeSetHash, badSignaturePiReqA)
	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		req.ReqID,
		testHash("r-prime-bad-sig"),
		badSignatureProofHash,
		testHash("result-bad-sig"),
		badSignaturePiReqA,
	); err == nil {
		t.Fatalf("execute redaction accepted invalid share proof signature")
	}

	piReqA := piReqAForTest(t, dReq, key, []byte("share-from-active-tee"))
	proofHash := computeRedactionProofHashForTest(t, req.ReqID, dReq, activeSetHash, piReqA)
	onChainProofHash, err := contract.ComputeRedactionProofHash(nil, req.ReqID, dReq, activeSetHash, piReqA)
	if err != nil {
		t.Fatalf("compute redaction proof hash on chain: %v", err)
	}
	if onChainProofHash != proofHash {
		t.Fatalf("proof hash = %x, want %x", onChainProofHash, proofHash)
	}

	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		req.ReqID,
		testHash("r-prime"),
		proofHash,
		testHash("result"),
		piReqA,
	); err != nil {
		t.Fatalf("execute redaction: %v", err)
	}
	backend.Commit()

	record, err = contract.GetRedactRequest(nil, req.ReqID)
	if err != nil {
		t.Fatalf("get executed redact request: %v", err)
	}
	if record.Status != 2 {
		t.Fatalf("executed status = %d, want Executed(2)", record.Status)
	}
	if record.ExecutedAt.Sign() != 0 {
		t.Fatalf("executedAt = %s, want zero because execution timestamps are event-only", record.ExecutedAt)
	}
	executed, err := contract.IsRedactionExecuted(nil, req.ReqID)
	if err != nil {
		t.Fatalf("is redaction executed: %v", err)
	}
	if !executed {
		t.Fatalf("isRedactionExecuted returned false")
	}
	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		req.ReqID,
		testHash("r-prime-2"),
		proofHash,
		testHash("result-2"),
		piReqA,
	); err == nil {
		t.Fatalf("duplicate execution accepted")
	}

	expired := req
	expired.ReqID = testHash("req-expired")
	expiredDReq, err := contract.ComputeDReq(nil, expired)
	if err != nil {
		t.Fatalf("compute expired d_req: %v", err)
	}
	expiredAt := new(big.Int).Add(latestTimestamp(t, backend), big.NewInt(100))
	expiredAuthDigest, err := contract.ComputeAuthorizationDigest(nil, expiredDReq, dPolicy, params.CommitteeVersion, expiredAt)
	if err != nil {
		t.Fatalf("compute expiring auth digest: %v", err)
	}
	expiredSignature := committeeSignatureForDigest(expiredAuthDigest, committeeSecret)
	if _, err := contract.SubmitRedactAuthorization(newTransactor(key, nil), expired, dPolicy, expiredAt, expiredSignature); err != nil {
		t.Fatalf("submit expiring redact authorization: %v", err)
	}
	backend.Commit()
	if err := backend.AdjustTime(101 * time.Second); err != nil {
		t.Fatalf("adjust time past authorization expiry: %v", err)
	}
	backend.Commit()
	expiredProofHash := computeRedactionProofHashForTest(t, expired.ReqID, expiredDReq, activeSetHash, piReqA)
	if _, err := contract.ExecuteRedaction(
		newTransactor(key, nil),
		expired.ReqID,
		testHash("r-prime-expired"),
		expiredProofHash,
		testHash("result-expired"),
		piReqA,
	); err != nil {
		t.Fatalf("execute expired redaction should persist Expired status without reverting: %v", err)
	}
	backend.Commit()
	expiredStatus, err := contract.GetEffectiveRedactStatus(nil, expired.ReqID)
	if err != nil {
		t.Fatalf("get effective expired status: %v", err)
	}
	if expiredStatus != 4 {
		t.Fatalf("expired status = %d, want Expired(4)", expiredStatus)
	}
}
