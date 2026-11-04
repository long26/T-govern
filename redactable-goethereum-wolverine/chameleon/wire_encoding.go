package chameleon

import (
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/rlp"
)

// ScalarWireSize matches the existing enclave's scalar serialization, not Fr's bit size.
const ScalarWireSize = 48

var scalarWireOrder, _ = new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)

type scalarWire [ScalarWireSize]byte

func scalarForWire(value *big.Int) (scalarWire, error) {
	var encoded scalarWire
	if value == nil || value.Sign() < 0 || value.Cmp(scalarWireOrder) >= 0 {
		return encoded, fmt.Errorf("invalid wire scalar: expected 0 <= scalar < Fr order")
	}
	value.FillBytes(encoded[:])
	return encoded, nil
}

func scalarFromWire(encoded scalarWire) (*big.Int, error) {
	value := new(big.Int).SetBytes(encoded[:])
	if value.Cmp(scalarWireOrder) >= 0 {
		return nil, fmt.Errorf("invalid wire scalar: value is not in Fr")
	}
	return value, nil
}

// MarshalScalar48 returns the canonical fixed-width representation of an Fr value.
func MarshalScalar48(value *big.Int) ([]byte, error) {
	encoded, err := scalarForWire(value)
	if err != nil {
		return nil, err
	}
	return encoded[:], nil
}

// UnmarshalScalar48 rejects noncanonical widths and values rather than reducing them.
func UnmarshalScalar48(encoded []byte) (*big.Int, error) {
	if len(encoded) != ScalarWireSize {
		return nil, fmt.Errorf("invalid wire scalar length %d", len(encoded))
	}
	var wire scalarWire
	copy(wire[:], encoded)
	return scalarFromWire(wire)
}

type shareWire struct {
	FromIndex *big.Int
	ToIndex   *big.Int
	Share     scalarWire
}

func encodeShare(w io.Writer, from, to, scalar *big.Int) error {
	encoded, err := scalarForWire(scalar)
	if err != nil {
		return err
	}
	return rlp.Encode(w, shareWire{from, to, encoded})
}

func decodeShare(s *rlp.Stream) (shareWire, *big.Int, error) {
	var wire shareWire
	if err := s.Decode(&wire); err != nil {
		return wire, nil, err
	}
	scalar, err := scalarFromWire(wire.Share)
	return wire, scalar, err
}

func (s DistKGShare) EncodeRLP(w io.Writer) error {
	return encodeShare(w, s.FromIndex, s.ToIndex, s.Share)
}

func (s *DistKGShare) DecodeRLP(stream *rlp.Stream) error {
	wire, scalar, err := decodeShare(stream)
	if err != nil {
		return err
	}
	*s = DistKGShare{wire.FromIndex, wire.ToIndex, scalar}
	return nil
}

func (s DKRSShare) EncodeRLP(w io.Writer) error {
	return encodeShare(w, s.FromIndex, s.ToIndex, s.Share)
}

func (s *DKRSShare) DecodeRLP(stream *rlp.Stream) error {
	wire, scalar, err := decodeShare(stream)
	if err != nil {
		return err
	}
	*s = DKRSShare{wire.FromIndex, wire.ToIndex, scalar}
	return nil
}

func (s DistKGResponse) EncodeRLP(w io.Writer) error {
	return encodeShare(w, s.DealerIndex, s.ToIndex, s.Share)
}

func (s *DistKGResponse) DecodeRLP(stream *rlp.Stream) error {
	wire, scalar, err := decodeShare(stream)
	if err != nil {
		return err
	}
	*s = DistKGResponse{wire.FromIndex, wire.ToIndex, scalar}
	return nil
}

func (s DKRSResponse) EncodeRLP(w io.Writer) error {
	return encodeShare(w, s.DealerIndex, s.ToIndex, s.Share)
}

func (s *DKRSResponse) DecodeRLP(stream *rlp.Stream) error {
	wire, scalar, err := decodeShare(stream)
	if err != nil {
		return err
	}
	*s = DKRSResponse{wire.FromIndex, wire.ToIndex, scalar}
	return nil
}

type proofWire struct {
	Omega scalarWire
	Z     scalarWire
}

func (p NIZKProof) EncodeRLP(w io.Writer) error {
	omega, err := scalarForWire(p.Omega)
	if err != nil {
		return err
	}
	z, err := scalarForWire(p.Z)
	if err != nil {
		return err
	}
	return rlp.Encode(w, proofWire{omega, z})
}

func (p *NIZKProof) DecodeRLP(stream *rlp.Stream) error {
	var wire proofWire
	if err := stream.Decode(&wire); err != nil {
		return err
	}
	omega, err := scalarFromWire(wire.Omega)
	if err != nil {
		return err
	}
	z, err := scalarFromWire(wire.Z)
	if err != nil {
		return err
	}
	*p = NIZKProof{Omega: omega, Z: z}
	return nil
}

type adaptationShareWire struct {
	Index *big.Int
	Share [g1PointByteSize]byte
	Proof NIZKProof
}

func (s AdaptationShare) EncodeRLP(w io.Writer) error {
	if s.Share == nil || s.Proof == nil {
		return fmt.Errorf("invalid adaptation share: missing point or proof")
	}
	var wire adaptationShareWire
	wire.Index, wire.Proof = s.Index, *s.Proof
	pairingMu.Lock()
	point := *s.Share
	if !pairing.G1.IsOnCurve(&point) || !pairing.G1.InCorrectSubgroup(&point) {
		pairingMu.Unlock()
		return fmt.Errorf("invalid adaptation share: point not in G1")
	}
	copy(wire.Share[:], pairing.G1.ToBytes(&point))
	pairingMu.Unlock()
	return rlp.Encode(w, wire)
}

func (s *AdaptationShare) DecodeRLP(stream *rlp.Stream) error {
	var wire adaptationShareWire
	if err := stream.Decode(&wire); err != nil {
		return err
	}
	pairingMu.Lock()
	point, err := pairing.G1.FromBytes(wire.Share[:])
	if err == nil && !pairing.G1.InCorrectSubgroup(point) {
		err = fmt.Errorf("invalid adaptation share: point not in G1 subgroup")
	}
	pairingMu.Unlock()
	if err != nil {
		return err
	}
	*s = AdaptationShare{Index: wire.Index, Share: point, Proof: &wire.Proof}
	return nil
}
