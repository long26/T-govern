package eth

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// ErrInsufficientCandidates is returned when fewer valid solutions than required committee size exist.
	ErrInsufficientCandidates = errors.New("insufficient candidate solutions for committee")

	// ErrInvalidCommitteeSize is returned when committee size is not positive.
	ErrInvalidCommitteeSize = errors.New("committee size must be greater than zero")

	// ErrInvalidThreshold is returned when threshold is negative or not less than committee size.
	ErrInvalidThreshold = errors.New("threshold must be non-negative and less than committee size")

	// ErrEmptyScanRange is returned when a requested scan range contains no blocks.
	ErrEmptyScanRange = errors.New("scan range is empty")

	// ErrInvalidScanRange is returned when current boundary is not strictly greater than previous boundary.
	ErrInvalidScanRange = errors.New("current boundary must be strictly greater than previous boundary")

	// ErrZeroDifficulty is returned when a zero or negative difficulty is provided.
	ErrZeroDifficulty = errors.New("difficulty must be greater than zero")

	// ErrZeroTarget is returned when a zero target is converted to difficulty.
	ErrZeroTarget = errors.New("target must be greater than zero")

	// ErrCandidateNotMapped is returned when a candidate cannot be mapped to a known node identity.
	ErrCandidateNotMapped = errors.New("candidate ID could not be mapped to known node ID")

	// ErrDuplicateMappedNodeID is returned when multiple selected candidates map to the same node identity.
	ErrDuplicateMappedNodeID = errors.New("multiple winning candidates mapped to identical node ID")

	// MaxTarget is the maximum possible 256-bit target (2^256 - 1).
	MaxTarget = common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	// two256 is 2^256, used for target/difficulty conversions.
	two256 = new(big.Int).Lsh(big.NewInt(1), 256)
)

// DeriveTau derives the deterministic epoch randomness tau from the boundary block hash
// and the epoch number.
//
// tau = Keccak256(boundaryBlockHash || uint64be(epoch))
func DeriveTau(boundaryBlockHash common.Hash, epoch uint64) common.Hash {
	buf := make([]byte, 40)
	copy(buf[:32], boundaryBlockHash.Bytes())
	binary.BigEndian.PutUint64(buf[32:], epoch)
	return crypto.Keccak256Hash(buf)
}

// ComputePowDigest calculates the deterministic PoW digest for a candidate solution.
//
// digest = Keccak256(tau || uint64be(epoch) || CandidateID || ElectionPK || uint64be(nonce))
func ComputePowDigest(tau common.Hash, epoch uint64, candidateID CandidateID, electionPK []byte, nonce uint64) common.Hash {
	epochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBytes, epoch)

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)

	return crypto.Keccak256Hash(
		tau.Bytes(),
		epochBytes,
		[]byte(candidateID),
		electionPK,
		nonceBytes,
	)
}

// VerifyPowDigest verifies whether the given digest satisfies the difficulty target (digest <= target).
// Target comparison treats both 32-byte hashes as unsigned big-endian 256-bit integers.
func VerifyPowDigest(digest common.Hash, target common.Hash) bool {
	return bytes.Compare(digest[:], target[:]) <= 0
}

// DifficultyToTarget computes the 256-bit target corresponding to the given difficulty.
// Target = 2^256 / difficulty (clamped to MaxTarget if difficulty <= 1).
func DifficultyToTarget(difficulty *big.Int) (common.Hash, error) {
	if difficulty == nil || difficulty.Sign() <= 0 {
		return common.Hash{}, ErrZeroDifficulty
	}
	targetBig := new(big.Int).Div(two256, difficulty)
	maxBig := new(big.Int).Sub(two256, big.NewInt(1))
	if targetBig.Cmp(maxBig) > 0 {
		return MaxTarget, nil
	}
	return common.BigToHash(targetBig), nil
}

// TargetToDifficulty computes the difficulty corresponding to the given target.
// Difficulty = 2^256 / target.
func TargetToDifficulty(target common.Hash) (*big.Int, error) {
	targetBig := new(big.Int).SetBytes(target[:])
	if targetBig.Sign() <= 0 {
		return nil, ErrZeroTarget
	}
	return new(big.Int).Div(two256, targetBig), nil
}

// VerifySolution validates whether a solution is mathematically valid for the given tau and target:
// 1. Recomputes digest = ComputePowDigest(tau, epoch, candidateID, electionPK, nonce)
// 2. Checks that sol.PowDigest strictly matches the recomputed digest
// 3. Verifies that recomputed digest <= target
func VerifySolution(sol *Solution, tau common.Hash, target common.Hash) bool {
	if sol == nil {
		return false
	}
	computed := ComputePowDigest(tau, sol.Epoch, sol.CandidateID, sol.ElectionPK, sol.Nonce)
	if sol.PowDigest != computed {
		return false
	}
	return VerifyPowDigest(computed, target)
}

// FilterValidSolutions returns a slice of solutions that satisfy VerifySolution.
func FilterValidSolutions(solutions []Solution, tau common.Hash, target common.Hash) []Solution {
	var valid []Solution
	for _, s := range solutions {
		if VerifySolution(&s, tau, target) {
			valid = append(valid, s.Clone())
		}
	}
	return valid
}

// CompareSolutionOrder defines a deterministic total order for solutions:
// 1. BlockNumber ascending (earliest block first)
// 2. TxIndex ascending (earliest transaction within block first)
// 3. CandidateID ascending (lexicographical tie-breaker)
// 4. Nonce ascending (tie-breaker)
// 5. PowDigest ascending (tie-breaker)
func CompareSolutionOrder(a, b *Solution) int {
	if a.BlockNumber < b.BlockNumber {
		return -1
	}
	if a.BlockNumber > b.BlockNumber {
		return 1
	}
	if a.TxIndex < b.TxIndex {
		return -1
	}
	if a.TxIndex > b.TxIndex {
		return 1
	}
	if a.CandidateID < b.CandidateID {
		return -1
	}
	if a.CandidateID > b.CandidateID {
		return 1
	}
	if a.Nonce < b.Nonce {
		return -1
	}
	if a.Nonce > b.Nonce {
		return 1
	}
	return bytes.Compare(a.PowDigest[:], b.PowDigest[:])
}

// DeduplicateAndRankSolutions sorts all input solutions deterministically by chain order
// (blockNumber ascending, txIndex ascending) and retains only the earliest valid solution
// for each CandidateID.
//
// The output slice preserves the election rank order (rank 1 at index 0).
func DeduplicateAndRankSolutions(solutions []Solution) []Solution {
	if len(solutions) == 0 {
		return nil
	}

	// Make a defensive copy
	ordered := make([]Solution, len(solutions))
	for i, s := range solutions {
		ordered[i] = s.Clone()
	}

	// Sort deterministically by chain position and tie-breakers
	sort.Slice(ordered, func(i, j int) bool {
		return CompareSolutionOrder(&ordered[i], &ordered[j]) < 0
	})

	// Deduplicate by CandidateID, retaining the first (earliest) one seen
	seen := make(map[CandidateID]bool, len(ordered))
	var unique []Solution
	for _, sol := range ordered {
		if !seen[sol.CandidateID] {
			seen[sol.CandidateID] = true
			unique = append(unique, sol)
		}
	}
	return unique
}

// SelectTopN selects the first n solutions from a ranked slice.
// Returns ErrInvalidCommitteeSize if n <= 0, or ErrInsufficientCandidates if len(solutions) < n.
func SelectTopN(solutions []Solution, n int) ([]Solution, error) {
	if n <= 0 {
		return nil, ErrInvalidCommitteeSize
	}
	if len(solutions) < n {
		return nil, fmt.Errorf("%w: have %d, require %d", ErrInsufficientCandidates, len(solutions), n)
	}
	selected := make([]Solution, n)
	for i := 0; i < n; i++ {
		selected[i] = solutions[i].Clone()
	}
	return selected, nil
}

// GenerateCommitteeIndices generates 1-based indices (1..len(members))
// for the given list of members. Members are sorted in ascending lexicographical order.
func GenerateCommitteeIndices(members []NodeID) map[NodeID]int {
	sortedMembers := make([]NodeID, len(members))
	copy(sortedMembers, members)
	sort.Slice(sortedMembers, func(i, j int) bool {
		return sortedMembers[i] < sortedMembers[j]
	})

	indices := make(map[NodeID]int, len(sortedMembers))
	for i, m := range sortedMembers {
		indices[m] = i + 1
	}
	return indices
}

// ComputeCommitteeID computes a canonical, deterministic Keccak256 hash identifying
// a committee configuration:
//
// Keccak256("nitch-committee-v1:" || uint64be(epoch) || uint64be(threshold) || uint64be(activateAt) || uint64be(len(members)) || members...)
func ComputeCommitteeID(epoch uint64, members []NodeID, threshold int, activateAt uint64) common.Hash {
	// Sort members defensively to guarantee determinism
	sorted := make([]NodeID, len(members))
	copy(sorted, members)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	var b []byte
	b = append(b, []byte("nitch-committee-v1:")...)

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, epoch)
	b = append(b, buf...)

	binary.BigEndian.PutUint64(buf, uint64(threshold))
	b = append(b, buf...)

	binary.BigEndian.PutUint64(buf, activateAt)
	b = append(b, buf...)

	binary.BigEndian.PutUint64(buf, uint64(len(sorted)))
	b = append(b, buf...)

	for _, m := range sorted {
		binary.BigEndian.PutUint64(buf, uint64(len(m)))
		b = append(b, buf...)
		b = append(b, []byte(m)...)
	}

	return crypto.Keccak256Hash(b)
}

// BuildCommittee constructs the CommitteeDescriptor, election rank order, and 1-based
// committee indices from the winning solutions.
//
// nodeMapper converts CandidateID to NodeID. If nil, NodeID(candidateID) is used as default.
func BuildCommittee(
	winners []Solution,
	epoch uint64,
	threshold int,
	activateAt uint64,
	nodeMapper func(CandidateID) (NodeID, bool),
) (*ElectionResult, error) {
	n := len(winners)
	if n == 0 {
		return nil, ErrInvalidCommitteeSize
	}
	if threshold < 0 || threshold >= n {
		return nil, fmt.Errorf("%w: threshold %d, committee size %d", ErrInvalidThreshold, threshold, n)
	}

	members := make([]NodeID, n)
	seenNodes := make(map[NodeID]bool, n)

	for i, w := range winners {
		var nodeID NodeID
		if nodeMapper != nil {
			mapped, ok := nodeMapper(w.CandidateID)
			if !ok {
				return nil, fmt.Errorf("%w: candidate %s", ErrCandidateNotMapped, w.CandidateID)
			}
			nodeID = mapped
		} else {
			nodeID = NodeID(w.CandidateID)
		}

		if seenNodes[nodeID] {
			return nil, fmt.Errorf("%w: node %s", ErrDuplicateMappedNodeID, nodeID)
		}
		seenNodes[nodeID] = true
		members[i] = nodeID
	}

	// Committee members are sorted ascending by NodeID to establish stable 1-based indices
	sort.Slice(members, func(i, j int) bool {
		return members[i] < members[j]
	})

	indices := make(map[NodeID]int, n)
	for i, m := range members {
		indices[m] = i + 1
	}

	committeeID := ComputeCommitteeID(epoch, members, threshold, activateAt)

	descriptor := CommitteeDescriptor{
		Epoch:       epoch,
		Members:     members,
		Threshold:   threshold,
		CommitteeID: committeeID,
		ActivateAt:  activateAt,
	}

	// Make defensive copy of winners
	rankedWinners := make([]Solution, n)
	for i, w := range winners {
		rankedWinners[i] = w.Clone()
	}

	return &ElectionResult{
		Descriptor:    descriptor,
		RankedWinners: rankedWinners,
		Indices:       indices,
	}, nil
}

// ElectCommittee executes deterministic election over valid solutions:
// 1. Sorts and deduplicates candidates (keeping earliest on-chain solution per candidate).
// 2. Selects top n solutions.
// 3. Builds CommitteeDescriptor and 1-based indices sorted by NodeID.
func ElectCommittee(
	validSolutions []Solution,
	n int,
	threshold int,
	epoch uint64,
	activateAt uint64,
	nodeMapper func(CandidateID) (NodeID, bool),
) (*ElectionResult, error) {
	ranked := DeduplicateAndRankSolutions(validSolutions)
	winners, err := SelectTopN(ranked, n)
	if err != nil {
		return nil, err
	}
	return BuildCommittee(winners, epoch, threshold, activateAt, nodeMapper)
}

// CalcNextBoundary returns the next boundary block number given the previous boundary and epoch length.
//
// nextBoundary = previousBoundary + epochLength
func CalcNextBoundary(previousBoundary uint64, epochLength uint64) uint64 {
	return previousBoundary + epochLength
}

// CalcBoundaryBlock returns the boundary block number for a given epoch.
//
// boundaryBlock = initialBoundary + epoch * epochLength
func CalcBoundaryBlock(initialBoundary uint64, epoch uint64, epochLength uint64) uint64 {
	return initialBoundary + epoch*epochLength
}

// IsBoundaryBlock returns true and the finalized epoch number if blockNumber is an epoch boundary block.
//
// An epoch boundary block at initialBoundary + epoch * epochLength (with epoch >= 1)
// finalizes epoch e and starts epoch e+1.
func IsBoundaryBlock(blockNumber uint64, initialBoundary uint64, epochLength uint64) (bool, uint64) {
	if epochLength == 0 || blockNumber <= initialBoundary {
		return false, 0
	}
	diff := blockNumber - initialBoundary
	if diff%epochLength == 0 {
		finalizedEpoch := diff / epochLength
		return true, finalizedEpoch
	}
	return false, 0
}

// CalcEpoch returns the epoch number to which a block belongs.
// Returns error if blockNumber is before initialBoundary or epochLength is 0.
func CalcEpoch(blockNumber uint64, initialBoundary uint64, epochLength uint64) (uint64, error) {
	if epochLength == 0 {
		return 0, errors.New("epochLength must be greater than zero")
	}
	if blockNumber < initialBoundary {
		return 0, fmt.Errorf("block %d is before initial boundary %d", blockNumber, initialBoundary)
	}
	if blockNumber == initialBoundary {
		return 0, nil
	}
	diff := blockNumber - initialBoundary
	// For block in (initialBoundary + (e-1)*epochLength, initialBoundary + e*epochLength]:
	// diff from 1..epochLength is epoch 1.
	epoch := (diff-1)/epochLength + 1
	return epoch, nil
}

// ScanRange returns the inclusive [fromBlock, toBlock] range for the open interval
// (previousBoundary, currentBoundary).
//
// Returns ErrInvalidScanRange if currentBoundary <= previousBoundary.
// Returns ErrEmptyScanRange if currentBoundary <= previousBoundary + 1.
func ScanRange(previousBoundary uint64, currentBoundary uint64) (fromBlock uint64, toBlock uint64, err error) {
	if currentBoundary <= previousBoundary {
		return 0, 0, ErrInvalidScanRange
	}
	if currentBoundary == previousBoundary+1 {
		return 0, 0, ErrEmptyScanRange
	}
	return previousBoundary + 1, currentBoundary - 1, nil
}

// IsInScanRange returns true if blockNumber is strictly within the open interval
// (previousBoundary, currentBoundary).
func IsInScanRange(blockNumber uint64, previousBoundary uint64, currentBoundary uint64) bool {
	return blockNumber > previousBoundary && blockNumber < currentBoundary
}
