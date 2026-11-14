package contractService

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

type fakePkCHReader struct {
	pkCH []byte
	hash [32]byte
	err  error

	pkCHCalls int
	hashCalls int
}

func (f *fakePkCHReader) GetPkCH() ([]byte, error) {
	f.pkCHCalls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.pkCH...), nil
}

func (f *fakePkCHReader) GetPkCHHash() ([32]byte, error) {
	f.hashCalls++
	if f.err != nil {
		return [32]byte{}, f.err
	}
	return f.hash, nil
}

func TestPkCHProviderNotReadyThenReady(t *testing.T) {
	reader := &fakePkCHReader{}
	provider := NewPkCHProvider(reader)

	if pkCH, ok := provider.CurrentPkCH(); ok || pkCH != nil {
		t.Fatalf("empty pkCH ready = %v pkCH = %v, want not ready", ok, pkCH)
	}
	if reader.pkCHCalls != 1 || reader.hashCalls != 0 {
		t.Fatalf("calls after empty pkCH = (%d,%d), want (1,0)", reader.pkCHCalls, reader.hashCalls)
	}

	reader.pkCH = []byte{0x01, 0x02, 0x03}
	reader.hash = crypto.Keccak256Hash(reader.pkCH)
	pkCH, ok := provider.CurrentPkCH()
	if !ok || pkCH == nil || pkCH.Uint64() != 0x010203 {
		t.Fatalf("pkCH ready = %v pkCH = %v, want 0x010203", ok, pkCH)
	}
	if reader.pkCHCalls != 2 || reader.hashCalls != 1 {
		t.Fatalf("calls after ready pkCH = (%d,%d), want (2,1)", reader.pkCHCalls, reader.hashCalls)
	}

	reader.pkCH = []byte{0xff}
	pkCH, ok = provider.CurrentPkCH()
	if !ok || pkCH == nil || pkCH.Uint64() != 0x010203 {
		t.Fatalf("cached pkCH = %v ready=%v, want original 0x010203", pkCH, ok)
	}
	if reader.pkCHCalls != 2 || reader.hashCalls != 1 {
		t.Fatalf("READY cache was not used, calls = (%d,%d)", reader.pkCHCalls, reader.hashCalls)
	}
}

func TestPkCHProviderHashMismatch(t *testing.T) {
	reader := &fakePkCHReader{
		pkCH: []byte{0x01},
		hash: crypto.Keccak256Hash([]byte{0x02}),
	}
	provider := NewPkCHProvider(reader)
	if pkCH, ok := provider.CurrentPkCH(); ok || pkCH != nil {
		t.Fatalf("hash mismatch ready = %v pkCH = %v, want not ready", ok, pkCH)
	}
}

func TestPkCHProviderActivationBlock(t *testing.T) {
	raw := []byte{0x04}
	reader := &fakePkCHReader{pkCH: raw, hash: crypto.Keccak256Hash(raw)}
	provider := NewPkCHProvider(reader)
	if pkCH, enforce := provider.PolicyPkCHForBlock(10); enforce || pkCH != nil {
		t.Fatalf("policy before activation = %v %v, want disabled", pkCH, enforce)
	}
	if err := provider.ObservePkCHAtBlock(10); err != nil {
		t.Fatalf("observe pkCH: %v", err)
	}
	if pkCH, enforce := provider.PolicyPkCHForBlock(10); enforce || pkCH != nil {
		t.Fatalf("policy at activation block = %v %v, want disabled", pkCH, enforce)
	}
	if pkCH, enforce := provider.PolicyPkCHForBlock(11); !enforce || pkCH == nil || pkCH.Uint64() != 4 {
		t.Fatalf("policy after activation = %v %v, want pkCH", pkCH, enforce)
	}
	if provider.activationBlock == math.MaxUint64 {
		t.Fatalf("activation block was not recorded")
	}
}

func TestPkCHProviderRestoresOnlyMatchingKey(t *testing.T) {
	raw := []byte{4}
	provider := NewPkCHProvider(&fakePkCHReader{pkCH: raw, hash: crypto.Keccak256Hash(raw)})
	if err := provider.RestoreActivationBlock(10, big.NewInt(5)); err == nil {
		t.Fatal("restored metadata for an unrelated public key")
	}
	if _, ready := provider.ActivationBlock(); ready {
		t.Fatal("invalid restore activated policy")
	}
	if err := provider.RestoreActivationBlock(10, big.NewInt(4)); err != nil {
		t.Fatal(err)
	}
	if err := provider.ObservePkCHAtBlock(100); err != nil {
		t.Fatal(err)
	}
	if number, ready := provider.ActivationBlock(); !ready || number != 10 {
		t.Fatalf("lost historical activation: number=%d ready=%v", number, ready)
	}
	if key, ready := provider.PolicyPkCHForBlock(11); !ready || key.Cmp(big.NewInt(4)) != 0 {
		t.Fatal("historical policy key unavailable after restore")
	}
}

func TestPkCHProviderPropagatesReadError(t *testing.T) {
	want := errors.New("read failed")
	provider := NewPkCHProvider(&fakePkCHReader{err: want})
	if err := provider.ObservePkCHAtBlock(1); !errors.Is(err, want) {
		t.Fatalf("observe error = %v, want %v", err, want)
	}
}
