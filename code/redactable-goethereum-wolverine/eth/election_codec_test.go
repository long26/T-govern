package eth

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestSolutionCalldataRoundTrip(t *testing.T) {
	testCases := []struct {
		name string
		sol  Solution
	}{
		{
			name: "basic solution",
			sol: Solution{
				Epoch:       1,
				CandidateID: "candidate-node-1",
				ElectionPK:  []byte{1, 2, 3, 4, 5, 6, 7, 8},
				Nonce:       123456,
				PowDigest:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			},
		},
		{
			name: "32-byte BLS or Ed25519 PK",
			sol: Solution{
				Epoch:       42,
				CandidateID: "enode://pubkey@127.0.0.1:30303",
				ElectionPK:  bytes.Repeat([]byte{0xaa}, 32),
				Nonce:       0,
				PowDigest:   common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
			},
		},
		{
			name: "48-byte BLS G1 PK",
			sol: Solution{
				Epoch:       1000,
				CandidateID: "node-with-long-id-123456789",
				ElectionPK:  bytes.Repeat([]byte{0xbb}, 48),
				Nonce:       ^uint64(0), // max uint64
				PowDigest:   common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			},
		},
		{
			name: "single byte candidate and PK",
			sol: Solution{
				Epoch:       0,
				CandidateID: "x",
				ElectionPK:  []byte{0xff},
				Nonce:       1,
				PowDigest:   common.Hash{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := EncodeSolutionCalldata(&tc.sol)
			if err != nil {
				t.Fatalf("EncodeSolutionCalldata failed: %v", err)
			}

			decoded, err := DecodeSolutionCalldata(encoded)
			if err != nil {
				t.Fatalf("DecodeSolutionCalldata failed: %v", err)
			}

			if decoded.Epoch != tc.sol.Epoch {
				t.Errorf("Epoch mismatch: got %d, want %d", decoded.Epoch, tc.sol.Epoch)
			}
			if decoded.CandidateID != tc.sol.CandidateID {
				t.Errorf("CandidateID mismatch: got %s, want %s", decoded.CandidateID, tc.sol.CandidateID)
			}
			if !bytes.Equal(decoded.ElectionPK, tc.sol.ElectionPK) {
				t.Errorf("ElectionPK mismatch: got %x, want %x", decoded.ElectionPK, tc.sol.ElectionPK)
			}
			if decoded.Nonce != tc.sol.Nonce {
				t.Errorf("Nonce mismatch: got %d, want %d", decoded.Nonce, tc.sol.Nonce)
			}
			if decoded.PowDigest != tc.sol.PowDigest {
				t.Errorf("PowDigest mismatch: got %s, want %s", decoded.PowDigest.Hex(), tc.sol.PowDigest.Hex())
			}
			if decoded.BlockNumber != 0 || decoded.TxIndex != 0 {
				t.Errorf("BlockNumber and TxIndex should default to 0 on decode, got block=%d, tx=%d",
					decoded.BlockNumber, decoded.TxIndex)
			}
		})
	}
}

func TestSolutionCalldataIllegalInputs(t *testing.T) {
	// 1. Encode nil solution
	if _, err := EncodeSolutionCalldata(nil); err != ErrNilSolution {
		t.Errorf("expected ErrNilSolution, got %v", err)
	}

	// 2. Empty candidate ID
	emptyCand := &Solution{
		Epoch:       1,
		CandidateID: "",
		ElectionPK:  []byte{1, 2, 3},
		Nonce:       10,
	}
	if _, err := EncodeSolutionCalldata(emptyCand); err == nil {
		t.Error("expected error for empty candidate ID, got nil")
	}

	// 3. Empty election PK
	emptyPK := &Solution{
		Epoch:       1,
		CandidateID: "cand1",
		ElectionPK:  nil,
		Nonce:       10,
	}
	if _, err := EncodeSolutionCalldata(emptyPK); err == nil {
		t.Error("expected error for empty election PK, got nil")
	}

	// 4. CandidateID exceeds maximum length
	tooLongCand := &Solution{
		Epoch:       1,
		CandidateID: CandidateID(strings.Repeat("a", MaxCandidateIDLength+1)),
		ElectionPK:  []byte{1},
		Nonce:       10,
	}
	if _, err := EncodeSolutionCalldata(tooLongCand); err == nil {
		t.Error("expected error for candidate ID exceeding max length, got nil")
	}

	// 5. ElectionPK exceeds maximum length
	tooLongPK := &Solution{
		Epoch:       1,
		CandidateID: "cand1",
		ElectionPK:  make([]byte, MaxElectionPKLength+1),
		Nonce:       10,
	}
	if _, err := EncodeSolutionCalldata(tooLongPK); err == nil {
		t.Error("expected error for election PK exceeding max length, got nil")
	}

	// Valid solution for decoding sabotage tests
	validSol := &Solution{
		Epoch:       1,
		CandidateID: "c1",
		ElectionPK:  []byte{0x01, 0x02},
		Nonce:       99,
		PowDigest:   common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
	}
	validBytes, err := EncodeSolutionCalldata(validSol)
	if err != nil {
		t.Fatalf("failed to encode valid solution: %v", err)
	}

	// 6. Data too short
	shortBytes := []byte{0x4e, 0x50, 0x4f, 0x57}
	if _, err := DecodeSolutionCalldata(shortBytes); err == nil {
		t.Error("expected error decoding short calldata, got nil")
	}

	// 7. Invalid magic
	badMagic := make([]byte, len(validBytes))
	copy(badMagic, validBytes)
	copy(badMagic[0:4], []byte("TEST"))
	if _, err := DecodeSolutionCalldata(badMagic); err == nil {
		t.Error("expected error decoding invalid magic, got nil")
	}

	// 8. Unsupported version
	badVersion := make([]byte, len(validBytes))
	copy(badVersion, validBytes)
	badVersion[4] = 0x00
	badVersion[5] = 0x02 // version 2
	if _, err := DecodeSolutionCalldata(badVersion); err == nil {
		t.Error("expected error decoding unsupported version, got nil")
	}

	// 9. Trailing garbage bytes (strictly disallowed)
	withGarbage := append(validBytes, 0xff)
	if _, err := DecodeSolutionCalldata(withGarbage); err == nil {
		t.Error("expected error decoding calldata with trailing garbage, got nil")
	}

	// 10. Truncated payload (e.g. valid length minus 1 byte)
	truncated := validBytes[:len(validBytes)-1]
	if _, err := DecodeSolutionCalldata(truncated); err == nil {
		t.Error("expected error decoding truncated calldata, got nil")
	}
}

func TestNewSolutionTransaction(t *testing.T) {
	sol := &Solution{
		Epoch:       3,
		CandidateID: "cand-test",
		ElectionPK:  []byte{10, 20, 30},
		Nonce:       555,
		PowDigest:   common.HexToHash("0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
	}

	to := DefaultSolutionSystemAddress
	tx, err := NewSolutionTransaction(1, to, 100000, big.NewInt(1000), sol)
	if err != nil {
		t.Fatalf("NewSolutionTransaction failed: %v", err)
	}

	if tx.To() == nil || *tx.To() != to {
		t.Errorf("tx.To() mismatch: got %v, want %v", tx.To(), to)
	}
	if tx.Nonce() != 1 {
		t.Errorf("tx.Nonce() mismatch: got %d, want 1", tx.Nonce())
	}

	// Decode back from tx.Data()
	decoded, err := DecodeSolutionCalldata(tx.Data())
	if err != nil {
		t.Fatalf("failed to decode tx.Data(): %v", err)
	}

	if decoded.Epoch != sol.Epoch || decoded.CandidateID != sol.CandidateID || decoded.Nonce != sol.Nonce {
		t.Errorf("decoded solution from transaction does not match original: got %+v, want %+v", decoded, sol)
	}
}
