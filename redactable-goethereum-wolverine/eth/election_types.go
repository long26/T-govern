package eth

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// NodeID represents a stable cryptographic or network identifier for a node
// (e.g. enode URL or public identity string).
type NodeID string

// CandidateID represents an identifier for a candidate submitting a PoW solution.
type CandidateID string

// Solution represents a candidate PoW solution submitted on-chain.
type Solution struct {
	Epoch       uint64      `json:"epoch"`
	CandidateID CandidateID `json:"candidate_id"`
	ElectionPK  []byte      `json:"election_pk"`
	Nonce       uint64      `json:"nonce"`
	PowDigest   common.Hash `json:"pow_digest"`
	BlockNumber uint64      `json:"block_number"`
	TxIndex     uint64      `json:"tx_index"`
}

// Clone returns a deep copy of the Solution.
func (s Solution) Clone() Solution {
	cp := s
	if len(s.ElectionPK) > 0 {
		cp.ElectionPK = make([]byte, len(s.ElectionPK))
		copy(cp.ElectionPK, s.ElectionPK)
	}
	return cp
}

// CommitteeDescriptor represents the deterministic state and membership of a
// committee for a given epoch.
type CommitteeDescriptor struct {
	Epoch       uint64      `json:"epoch"`
	Members     []NodeID    `json:"members"`
	Threshold   int         `json:"threshold"`
	CommitteeID common.Hash `json:"committee_id"`
	ActivateAt  uint64      `json:"activate_at"`
}

// MemberIndex returns the 1-based index (1..len(Members)) for the given NodeID,
// or (0, false) if the NodeID is not in the committee.
func (c *CommitteeDescriptor) MemberIndex(id NodeID) (int, bool) {
	if c == nil {
		return 0, false
	}
	for i, m := range c.Members {
		if m == id {
			return i + 1, true
		}
	}
	return 0, false
}

// IndexToMember returns the NodeID for the given 1-based index,
// or ("", false) if the index is out of bounds.
func (c *CommitteeDescriptor) IndexToMember(idx int) (NodeID, bool) {
	if c == nil || idx < 1 || idx > len(c.Members) {
		return "", false
	}
	return c.Members[idx-1], true
}

// Contains returns true if the committee contains the given NodeID.
func (c *CommitteeDescriptor) Contains(id NodeID) bool {
	_, ok := c.MemberIndex(id)
	return ok
}

// Clone returns a deep copy of the CommitteeDescriptor.
func (c *CommitteeDescriptor) Clone() CommitteeDescriptor {
	if c == nil {
		return CommitteeDescriptor{}
	}
	cp := *c
	if len(c.Members) > 0 {
		cp.Members = make([]NodeID, len(c.Members))
		copy(cp.Members, c.Members)
	}
	return cp
}

// ElectionResult captures the output of a deterministic committee election.
type ElectionResult struct {
	Descriptor    CommitteeDescriptor `json:"descriptor"`
	RankedWinners []Solution          `json:"ranked_winners"` // Top n solutions in election rank order (earliest on-chain first)
	Indices       map[NodeID]int      `json:"indices"`        // 1-based indices (NodeID ascending)
}

// MemberIndex returns the 1-based index of the given NodeID in the elected committee.
func (r *ElectionResult) MemberIndex(id NodeID) (int, bool) {
	if r == nil || r.Indices == nil {
		return 0, false
	}
	idx, ok := r.Indices[id]
	return idx, ok
}

// ElectionConfig captures the parameter configuration for election execution.
type ElectionConfig struct {
	EpochLength     uint64         `json:"epoch_length"`
	InitialBoundary uint64         `json:"initial_boundary"`
	CommitteeSize   int            `json:"committee_size"`
	Threshold       int            `json:"threshold"`
	Target          common.Hash    `json:"target"`
	SystemAddress   common.Address `json:"system_address"`
}

// Validate verifies the consistency and bounds of ElectionConfig parameters.
// EpochLength must be at least 2 so that the open interval (prevBoundary, nextBoundary)
// contains at least one block for solution submissions.
func (c ElectionConfig) Validate() error {
	if c.EpochLength < 2 {
		return errors.New("invalid epoch length: must be >= 2")
	}
	if c.CommitteeSize <= 0 {
		return ErrInvalidCommitteeSize
	}
	if c.Threshold < 0 || c.Threshold >= c.CommitteeSize {
		return ErrInvalidThreshold
	}
	if c.Target == (common.Hash{}) {
		return ErrZeroTarget
	}
	return nil
}

// ActiveCommitteeProvider provides initial active committee member NodeIDs in current DKRS index order.
type ActiveCommitteeProvider interface {
	GetInitialActiveCommittee() (members []NodeID, threshold int, epoch uint64, activateAt uint64, err error)
}

// ActiveCommitteeFunc is a function adapter for ActiveCommitteeProvider.
type ActiveCommitteeFunc func() ([]NodeID, int, uint64, uint64, error)

// GetInitialActiveCommittee calls the underlying function.
func (f ActiveCommitteeFunc) GetInitialActiveCommittee() ([]NodeID, int, uint64, uint64, error) {
	return f()
}

