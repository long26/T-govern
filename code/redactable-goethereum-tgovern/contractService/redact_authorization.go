package contractService

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

// VerifyRedactAuthorization checks the P2P certificate against trusted contract
// state without submitting a transaction or waiting for one to be mined.
func (s *ContractService) VerifyRedactAuthorization(ctx context.Context, req RedactReqFields, dReq, dPolicy [32]byte, version, expiresAt *big.Int, digest [32]byte, signature []byte) error {
	if req.Version != 1 || req.ReqID == ([32]byte{}) || version == nil || expiresAt == nil || !expiresAt.IsInt64() || expiresAt.Int64() <= time.Now().Unix() || len(signature) != 64 {
		return fmt.Errorf("invalid or expired P2P redaction authorization")
	}
	opts := &bind.CallOpts{Context: ctx}
	params, err := s.contract.GetSystemParams(opts)
	if err != nil {
		return err
	}
	if params.Epoch.Cmp(new(big.Int).SetUint64(req.Epoch)) != 0 || params.CommitteeVersion.Cmp(version) != 0 || params.PkCHHash != req.BodyHashes[6] {
		return fmt.Errorf("P2P redaction authorization has stale system parameters")
	}
	activeHash, err := s.contract.GetActiveSetHash(opts, params.Epoch)
	if err != nil {
		return err
	}
	if activeHash != req.BodyHashes[5] {
		return fmt.Errorf("P2P redaction authorization active set mismatch")
	}
	wantDReq, err := s.contract.ComputeDReq(opts, req)
	if err != nil {
		return err
	}
	if dReq != wantDReq {
		return fmt.Errorf("P2P redaction authorization request digest mismatch")
	}
	wantDigest, err := s.contract.ComputeAuthorizationDigest(opts, dReq, dPolicy, version, expiresAt)
	if err != nil {
		return err
	}
	if digest != wantDigest {
		return fmt.Errorf("P2P redaction authorization signature digest mismatch")
	}
	publicKey := make([]byte, 0, 128)
	for i := int64(0); i < 4; i++ {
		coordinate, err := s.contract.CommitteePubKey(opts, big.NewInt(i))
		if err != nil {
			return err
		}
		publicKey = append(publicKey, common.LeftPadBytes(coordinate.Bytes(), 32)...)
	}
	return verifyRedactCertificate(digest, signature, publicKey)
}

func verifyRedactCertificate(digest [32]byte, signature, publicKey []byte) error {
	if len(signature) != 64 || len(publicKey) != 128 || isZeroBytes(signature) || isZeroBytes(publicKey) {
		return fmt.Errorf("invalid P2P redaction certificate point")
	}
	sig := new(bn256.G1)
	if _, err := sig.Unmarshal(signature); err != nil {
		return fmt.Errorf("invalid redaction signature: %w", err)
	}
	key := new(bn256.G2)
	if _, err := key.Unmarshal(publicKey); err != nil {
		return fmt.Errorf("invalid committee public key: %w", err)
	}
	h := sha256.New()
	h.Write(digest[:])
	h.Write([]byte("committee-sig/mvp/v1"))
	scalar := new(big.Int).SetBytes(h.Sum(nil))
	scalar.Mod(scalar, bn256.Order)
	if scalar.Sign() == 0 {
		scalar.SetInt64(1)
	}
	message := new(bn256.G1).ScalarBaseMult(scalar)
	if !bn256.PairingCheck([]*bn256.G1{sig, new(bn256.G1).Neg(message)}, []*bn256.G2{new(bn256.G2).ScalarBaseMult(big.NewInt(1)), key}) {
		return fmt.Errorf("invalid P2P redaction authorization signature")
	}
	return nil
}
