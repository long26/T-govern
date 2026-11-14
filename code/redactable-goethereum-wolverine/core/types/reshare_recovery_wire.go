package types

import (
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/rlp"
)

type reshareInputRequestWire struct {
	RequesterIndex uint64
	DealerIndex    uint64
	NeedDeal       bool
	NeedShare      bool
	Nonce          [32]byte
	Attempt        uint64
}

type reshareDealWire struct {
	DealerIndex   uint64
	OldThreshold  uint64
	OldNumParties uint64
	NewThreshold  uint64
	NewNumParties uint64
	A             []byte
	B0            []byte
}

type reshareShareWire struct {
	FromIndex uint64
	ToIndex   uint64
	Payload   []byte
}

type reshareInputResponseWire struct {
	RequesterIndex uint64
	DealerIndex    uint64
	Nonce          [32]byte
	HasDeal        bool
	Deal           reshareDealWire
	HasShare       bool
	Share          reshareShareWire
}

func reshareWireIndex(index *big.Int) (uint64, error) {
	if index == nil {
		return 0, nil
	}
	if !index.IsUint64() {
		return 0, fmt.Errorf("reshare index is not uint64: %v", index)
	}
	return index.Uint64(), nil
}

func (m ReshareInputRequest) EncodeRLP(w io.Writer) error {
	requester, err := reshareWireIndex(m.ReceiverIndex)
	if err != nil {
		return err
	}
	dealer, err := reshareWireIndex(m.DealerIndex)
	if err != nil {
		return err
	}
	return rlp.Encode(w, reshareInputRequestWire{requester, dealer, m.NeedDeal, m.NeedShare, m.Nonce, m.Attempt})
}

func (m *ReshareInputRequest) DecodeRLP(s *rlp.Stream) error {
	var wire reshareInputRequestWire
	if err := s.Decode(&wire); err != nil {
		return err
	}
	*m = ReshareInputRequest{
		DealerIndex: new(big.Int).SetUint64(wire.DealerIndex), ReceiverIndex: new(big.Int).SetUint64(wire.RequesterIndex),
		NeedDeal: wire.NeedDeal, NeedShare: wire.NeedShare, Nonce: wire.Nonce, Attempt: wire.Attempt,
	}
	return nil
}

func recoveryDealForWire(deal *chameleon.DKRSDeal) (reshareDealWire, error) {
	var wire reshareDealWire
	if deal == nil {
		return wire, nil
	}
	for _, field := range []struct {
		value *big.Int
		out   *uint64
	}{
		{deal.DealerIndex, &wire.DealerIndex}, {deal.OldThreshold, &wire.OldThreshold},
		{deal.OldNumParties, &wire.OldNumParties}, {deal.NewThreshold, &wire.NewThreshold},
		{deal.NewNumParties, &wire.NewNumParties},
	} {
		value, err := reshareWireIndex(field.value)
		if err != nil {
			return wire, err
		}
		*field.out = value
	}
	wire.A = deal.A
	wire.B0 = deal.B0
	return wire, nil
}

func recoveryDealFromWire(wire reshareDealWire) *chameleon.DKRSDeal {
	return &chameleon.DKRSDeal{
		DealerIndex: new(big.Int).SetUint64(wire.DealerIndex), OldThreshold: new(big.Int).SetUint64(wire.OldThreshold),
		OldNumParties: new(big.Int).SetUint64(wire.OldNumParties), NewThreshold: new(big.Int).SetUint64(wire.NewThreshold),
		NewNumParties: new(big.Int).SetUint64(wire.NewNumParties), A: wire.A, B0: wire.B0,
	}
}

func (m ReshareInputResponse) EncodeRLP(w io.Writer) error {
	if m.HasDeal != (m.Deal != nil) || m.HasShare != (m.Share != nil) {
		return fmt.Errorf("inconsistent reshare recovery presence flags")
	}
	requester, err := reshareWireIndex(m.ReceiverIndex)
	if err != nil {
		return err
	}
	dealer, err := reshareWireIndex(m.DealerIndex)
	if err != nil {
		return err
	}
	wire := reshareInputResponseWire{RequesterIndex: requester, DealerIndex: dealer, Nonce: m.Nonce, HasDeal: m.HasDeal, HasShare: m.HasShare}
	if wire.Deal, err = recoveryDealForWire(m.Deal); err != nil {
		return err
	}
	if m.Share != nil {
		if wire.Share.FromIndex, err = reshareWireIndex(m.Share.FromIndex); err != nil {
			return err
		}
		if wire.Share.ToIndex, err = reshareWireIndex(m.Share.ToIndex); err != nil {
			return err
		}
		if wire.Share.Payload, err = chameleon.MarshalScalar48(m.Share.Share); err != nil {
			return err
		}
	}
	return rlp.Encode(w, wire)
}

func (m *ReshareInputResponse) DecodeRLP(s *rlp.Stream) error {
	var wire reshareInputResponseWire
	if err := s.Decode(&wire); err != nil {
		return err
	}
	out := ReshareInputResponse{
		ReceiverIndex: new(big.Int).SetUint64(wire.RequesterIndex), DealerIndex: new(big.Int).SetUint64(wire.DealerIndex),
		Nonce: wire.Nonce, HasDeal: wire.HasDeal, HasShare: wire.HasShare,
	}
	if wire.HasDeal {
		out.Deal = recoveryDealFromWire(wire.Deal)
	} else if wire.Deal.DealerIndex != 0 || wire.Deal.OldThreshold != 0 || wire.Deal.OldNumParties != 0 ||
		wire.Deal.NewThreshold != 0 || wire.Deal.NewNumParties != 0 || len(wire.Deal.A) != 0 || len(wire.Deal.B0) != 0 {
		return fmt.Errorf("unexpected deal without presence flag")
	}
	if wire.HasShare {
		scalar, err := chameleon.UnmarshalScalar48(wire.Share.Payload)
		if err != nil {
			return err
		}
		out.Share = &chameleon.DKRSShare{
			FromIndex: new(big.Int).SetUint64(wire.Share.FromIndex), ToIndex: new(big.Int).SetUint64(wire.Share.ToIndex), Share: scalar,
		}
	} else if wire.Share.FromIndex != 0 || wire.Share.ToIndex != 0 || len(wire.Share.Payload) != 0 {
		return fmt.Errorf("unexpected share without presence flag")
	}
	*m = out
	return nil
}
