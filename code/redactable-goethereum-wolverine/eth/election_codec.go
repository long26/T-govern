package eth

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	// MagicSolution is the 4-byte magic header identifying a solution calldata payload ("NPOW").
	MagicSolution = [4]byte{'N', 'P', 'O', 'W'}

	// DefaultSolutionSystemAddress is the well-known system address for solution transactions.
	DefaultSolutionSystemAddress = common.HexToAddress("0x000000000000000000000000000000000000e1ec")

	// ErrNilSolution is returned when attempting to encode a nil Solution.
	ErrNilSolution = errors.New("solution is nil")

	// ErrInvalidCalldataLength is returned when calldata length does not match the expected wire layout.
	ErrInvalidCalldataLength = errors.New("invalid solution calldata length")

	// ErrInvalidMagic is returned when the calldata magic header does not match MagicSolution.
	ErrInvalidMagic = errors.New("invalid solution calldata magic header")

	// ErrUnsupportedVersion is returned when the calldata version is unsupported.
	ErrUnsupportedVersion = errors.New("unsupported solution calldata version")

	// ErrInvalidCandidateID is returned when the CandidateID is empty or exceeds allowed length.
	ErrInvalidCandidateID = errors.New("invalid candidate ID in solution")

	// ErrInvalidElectionPK is returned when the ElectionPK is empty or exceeds allowed length.
	ErrInvalidElectionPK = errors.New("invalid election public key in solution")
)

const (
	// SolutionCalldataVersion1 represents version 1 of the solution calldata protocol.
	SolutionCalldataVersion1 uint16 = 1

	// MaxCandidateIDLength is the maximum allowed byte length for a CandidateID string.
	MaxCandidateIDLength = 512

	// MaxElectionPKLength is the maximum allowed byte length for an ElectionPK.
	MaxElectionPKLength = 2048

	// solutionHeaderLen: magic(4) + version(2) + epoch(8) = 14 bytes
	solutionHeaderLen = 14

	// solutionSuffixLen: nonce(8) + powDigest(32) = 40 bytes
	solutionSuffixLen = 40

	// minSolutionCalldataLen: 14 + 2 (candLen) + 1 (cand) + 2 (pkLen) + 1 (pk) + 40 = 60 bytes
	minSolutionCalldataLen = solutionHeaderLen + 2 + 1 + 2 + 1 + solutionSuffixLen
)

// EncodeSolutionCalldata serializes a Solution into wire calldata bytes according to the
// specification:
//
// Layout:
//   [0..4]    Magic: "NPOW" (4 bytes)
//   [4..6]    Version: uint16 big-endian (2 bytes)
//   [6..14]   Epoch: uint64 big-endian (8 bytes)
//   [14..16]  CandidateID Length: uint16 big-endian (2 bytes)
//   [16..16+C] CandidateID: UTF-8 string bytes (C bytes)
//   [..+2]    ElectionPK Length: uint16 big-endian (2 bytes)
//   [..+K]    ElectionPK: raw public key bytes (K bytes)
//   [..+8]    Nonce: uint64 big-endian (8 bytes)
//   [..+32]   PowDigest: 32 bytes hash
func EncodeSolutionCalldata(sol *Solution) ([]byte, error) {
	if sol == nil {
		return nil, ErrNilSolution
	}
	candLen := len(sol.CandidateID)
	if candLen == 0 || candLen > MaxCandidateIDLength {
		return nil, fmt.Errorf("%w: length %d (allowed 1..%d)", ErrInvalidCandidateID, candLen, MaxCandidateIDLength)
	}
	pkLen := len(sol.ElectionPK)
	if pkLen == 0 || pkLen > MaxElectionPKLength {
		return nil, fmt.Errorf("%w: length %d (allowed 1..%d)", ErrInvalidElectionPK, pkLen, MaxElectionPKLength)
	}

	totalLen := solutionHeaderLen + 2 + candLen + 2 + pkLen + solutionSuffixLen
	buf := make([]byte, totalLen)

	// Magic
	copy(buf[0:4], MagicSolution[:])
	// Version
	binary.BigEndian.PutUint16(buf[4:6], SolutionCalldataVersion1)
	// Epoch
	binary.BigEndian.PutUint64(buf[6:14], sol.Epoch)

	// CandidateID length & bytes
	offset := 14
	binary.BigEndian.PutUint16(buf[offset:offset+2], uint16(candLen))
	offset += 2
	copy(buf[offset:offset+candLen], []byte(sol.CandidateID))
	offset += candLen

	// ElectionPK length & bytes
	binary.BigEndian.PutUint16(buf[offset:offset+2], uint16(pkLen))
	offset += 2
	copy(buf[offset:offset+pkLen], sol.ElectionPK)
	offset += pkLen

	// Nonce
	binary.BigEndian.PutUint64(buf[offset:offset+8], sol.Nonce)
	offset += 8

	// PowDigest
	copy(buf[offset:offset+32], sol.PowDigest[:])

	return buf, nil
}

// DecodeSolutionCalldata parses raw calldata into a Solution struct.
// It strictly validates magic, version, field bounds, and ensures there are no trailing garbage bytes.
// BlockNumber and TxIndex are initialized to 0.
func DecodeSolutionCalldata(data []byte) (*Solution, error) {
	if len(data) < minSolutionCalldataLen {
		return nil, fmt.Errorf("%w: have %d bytes, minimum required %d", ErrInvalidCalldataLength, len(data), minSolutionCalldataLen)
	}
	if !bytes.Equal(data[:4], MagicSolution[:]) {
		return nil, fmt.Errorf("%w: got %x, expected %x", ErrInvalidMagic, data[:4], MagicSolution[:])
	}
	version := binary.BigEndian.Uint16(data[4:6])
	if version != SolutionCalldataVersion1 {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
	}
	epoch := binary.BigEndian.Uint64(data[6:14])

	offset := 14
	candLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if candLen == 0 || candLen > MaxCandidateIDLength {
		return nil, fmt.Errorf("%w: length %d (allowed 1..%d)", ErrInvalidCandidateID, candLen, MaxCandidateIDLength)
	}
	// Check remaining length can at least hold CandidateID + pkLen(2) + 1 + suffix(40)
	if len(data) < offset+candLen+2+1+solutionSuffixLen {
		return nil, fmt.Errorf("%w: payload truncated at candidate ID", ErrInvalidCalldataLength)
	}
	candidateID := CandidateID(string(data[offset : offset+candLen]))
	offset += candLen

	pkLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if pkLen == 0 || pkLen > MaxElectionPKLength {
		return nil, fmt.Errorf("%w: length %d (allowed 1..%d)", ErrInvalidElectionPK, pkLen, MaxElectionPKLength)
	}

	// Exact length check: must match offset + pkLen + 8 (nonce) + 32 (powDigest)
	expectedTotal := offset + pkLen + solutionSuffixLen
	if len(data) != expectedTotal {
		return nil, fmt.Errorf("%w: total bytes %d, expected exactly %d", ErrInvalidCalldataLength, len(data), expectedTotal)
	}

	electionPK := make([]byte, pkLen)
	copy(electionPK, data[offset:offset+pkLen])
	offset += pkLen

	nonce := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	var powDigest common.Hash
	copy(powDigest[:], data[offset:offset+32])

	return &Solution{
		Epoch:       epoch,
		CandidateID: candidateID,
		ElectionPK:  electionPK,
		Nonce:       nonce,
		PowDigest:   powDigest,
		BlockNumber: 0,
		TxIndex:     0,
	}, nil
}

// NewSolutionTransaction constructs an unsigned Ethereum transaction destined for the solution
// system address containing encoded solution calldata.
func NewSolutionTransaction(nonce uint64, to common.Address, gasLimit uint64, gasPrice *big.Int, sol *Solution) (*types.Transaction, error) {
	data, err := EncodeSolutionCalldata(sol)
	if err != nil {
		return nil, err
	}
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	return types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data), nil
}
