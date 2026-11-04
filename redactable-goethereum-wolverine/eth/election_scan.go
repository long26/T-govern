package eth

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	// DefaultElectionEpochLength is the default number of blocks in an epoch.
	DefaultElectionEpochLength = uint64(100)

	// DefaultElectionCommitteeSize is the default number of members in an elected committee.
	DefaultElectionCommitteeSize = 4

	// DefaultElectionThreshold is the default threshold for an elected committee.
	DefaultElectionThreshold = 2

	// DefaultElectionInitialBoundary is the default initial boundary block height.
	DefaultElectionInitialBoundary = uint64(0)
)

// BlockReader defines the minimal chain interface needed to scan canonical blocks.
// This is satisfied by *core.BlockChain.
type BlockReader interface {
	GetBlockByNumber(number uint64) *types.Block
}

// DefaultElectionConfig returns an ElectionConfig initialized with default values.
// By default, target is MaxTarget, accepting any valid PoW digest.
func DefaultElectionConfig() ElectionConfig {
	return ElectionConfig{
		EpochLength:     DefaultElectionEpochLength,
		InitialBoundary: DefaultElectionInitialBoundary,
		CommitteeSize:   DefaultElectionCommitteeSize,
		Threshold:       DefaultElectionThreshold,
		Target:          MaxTarget,
		SystemAddress:   DefaultSolutionSystemAddress,
	}
}

// ParseElectionConfigFromEnv parses election configuration from environment variables,
// falling back to defaults for any unset variables.
//
// Environment variables parsed:
//   NITCH_EPOCH_LENGTH         - uint64 block count (must be > 0)
//   NITCH_POW_INITIAL_BOUNDARY - uint64 initial boundary block (default 0)
//   NITCH_POW_COMMITTEE_SIZE   - int member count (must be > 0)
//   NITCH_POW_THRESHOLD        - int threshold (must be >= 0 and < committee_size)
//   NITCH_POW_DIFFICULTY       - decimal or 0x-hex big.Int difficulty (converted to Target)
//   NITCH_POW_TARGET           - hex 32-byte target (takes precedence over difficulty if set)
//   NITCH_POW_SYSTEM_ADDRESS   - hex Ethereum address for solution transactions
//
// If getenv is nil, os.Getenv is used.
func ParseElectionConfigFromEnv(getenv func(string) string) (ElectionConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := DefaultElectionConfig()

	// NITCH_EPOCH_LENGTH
	if raw := strings.TrimSpace(getenv("NITCH_EPOCH_LENGTH")); raw != "" {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || val < 2 {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_EPOCH_LENGTH %q: must be integer >= 2", raw)
		}
		cfg.EpochLength = val
	}

	// NITCH_POW_INITIAL_BOUNDARY
	if raw := strings.TrimSpace(getenv("NITCH_POW_INITIAL_BOUNDARY")); raw != "" {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_POW_INITIAL_BOUNDARY %q", raw)
		}
		cfg.InitialBoundary = val
	}

	// NITCH_POW_COMMITTEE_SIZE
	if raw := strings.TrimSpace(getenv("NITCH_POW_COMMITTEE_SIZE")); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val <= 0 {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_POW_COMMITTEE_SIZE %q: must be positive integer", raw)
		}
		cfg.CommitteeSize = val
		// Adjust default threshold if committee size changed and threshold wasn't explicitly set
		if strings.TrimSpace(getenv("NITCH_POW_THRESHOLD")) == "" {
			if val >= 3 {
				cfg.Threshold = val * 2 / 3
			} else {
				cfg.Threshold = val - 1
			}
		}
	}

	// NITCH_POW_THRESHOLD
	if raw := strings.TrimSpace(getenv("NITCH_POW_THRESHOLD")); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 || val >= cfg.CommitteeSize {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_POW_THRESHOLD %q: must be >= 0 and < %d", raw, cfg.CommitteeSize)
		}
		cfg.Threshold = val
	}

	// NITCH_POW_TARGET / NITCH_POW_DIFFICULTY
	if raw := strings.TrimSpace(getenv("NITCH_POW_TARGET")); raw != "" {
		cfg.Target = common.HexToHash(raw)
	} else if raw := strings.TrimSpace(getenv("NITCH_POW_DIFFICULTY")); raw != "" {
		diff, ok := new(big.Int).SetString(raw, 0)
		if !ok || diff.Sign() <= 0 {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_POW_DIFFICULTY %q: must be positive integer", raw)
		}
		target, err := DifficultyToTarget(diff)
		if err != nil {
			return ElectionConfig{}, fmt.Errorf("converting difficulty %q: %w", raw, err)
		}
		cfg.Target = target
	}

	// NITCH_POW_SYSTEM_ADDRESS
	if raw := strings.TrimSpace(getenv("NITCH_POW_SYSTEM_ADDRESS")); raw != "" {
		if !common.IsHexAddress(raw) {
			return ElectionConfig{}, fmt.Errorf("invalid NITCH_POW_SYSTEM_ADDRESS %q", raw)
		}
		cfg.SystemAddress = common.HexToAddress(raw)
	}

	return cfg, nil
}

// ScanEpochSolutions scans the open interval (previousBoundary, currentBoundary) for valid PoW
// solution transactions matching the expected epoch, tau, target, and system address.
//
// Rules enforced:
// 1. Only blocks in range (previousBoundary, currentBoundary), i.e. [previousBoundary+1, currentBoundary-1]
// 2. Only transactions destined for systemAddress
// 3. Transactions must have valid solution calldata
// 4. Solution.Epoch must match the requested epoch
// 5. Solution.BlockNumber and Solution.TxIndex are set to the block number and index in block
// 6. PoW must be valid: VerifySolution(sol, tau, target) returns true
// 7. If nodeMapper is provided, candidate must successfully map to a NodeID
func ScanEpochSolutions(
	reader BlockReader,
	previousBoundary uint64,
	currentBoundary uint64,
	epoch uint64,
	tau common.Hash,
	target common.Hash,
	systemAddress common.Address,
	nodeMapper func(CandidateID) (NodeID, bool),
) ([]Solution, error) {
	if reader == nil {
		return nil, errors.New("nil block reader")
	}
	fromBlock, toBlock, err := ScanRange(previousBoundary, currentBoundary)
	if err != nil {
		return nil, err
	}

	var validSolutions []Solution
	for num := fromBlock; num <= toBlock; num++ {
		block := reader.GetBlockByNumber(num)
		if block == nil {
			return nil, fmt.Errorf("missing canonical block at number %d", num)
		}

		for txIndex, tx := range block.Transactions() {
			if tx.To() == nil || *tx.To() != systemAddress {
				continue
			}

			sol, err := DecodeSolutionCalldata(tx.Data())
			if err != nil {
				continue // Malformed calldata ignored
			}
			if sol.Epoch != epoch {
				continue // Mismatched epoch ignored
			}

			sol.BlockNumber = block.NumberU64()
			sol.TxIndex = uint64(txIndex)

			if !VerifySolution(sol, tau, target) {
				continue // Invalid PoW digest or does not meet target ignored
			}

			if nodeMapper != nil {
				if _, ok := nodeMapper(sol.CandidateID); !ok {
					continue // Candidate cannot be mapped to known node
				}
			}

			validSolutions = append(validSolutions, *sol)
		}
	}

	return validSolutions, nil
}

// ScanAndElect performs canonical chain scanning for solutions and executes deterministic
// committee election.
func ScanAndElect(
	reader BlockReader,
	previousBoundary uint64,
	currentBoundary uint64,
	epoch uint64,
	tau common.Hash,
	config ElectionConfig,
	activateAt uint64,
	nodeMapper func(CandidateID) (NodeID, bool),
) (*ElectionResult, error) {
	solutions, err := ScanEpochSolutions(
		reader,
		previousBoundary,
		currentBoundary,
		epoch,
		tau,
		config.Target,
		config.SystemAddress,
		nodeMapper,
	)
	if err != nil {
		return nil, err
	}

	return ElectCommittee(solutions, config.CommitteeSize, config.Threshold, epoch, activateAt, nodeMapper)
}
