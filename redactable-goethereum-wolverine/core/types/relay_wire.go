package types

import (
	"bytes"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/rlp"
)

// Relay metadata is local bookkeeping. Only the remaining TTL travels on the wire.
func (m RelayMetadata) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, m.TTL)
}

func (m *RelayMetadata) DecodeRLP(s *rlp.Stream) error {
	ttl, err := s.Uint()
	if err != nil {
		return err
	}
	*m = RelayMetadata{TTL: ttl}
	return nil
}

type reshareComplaintWire struct {
	ComplainantIndex uint64
	DealerIndex      uint64
}

type reshareVerifyKeyWire struct {
	Index           uint64
	VerificationKey []byte
}

// The common DKRS envelope follows TEE's tagged layout, including B0 inside deals.
type reshareMessageWire struct {
	SessionID     [32]byte
	RelayTTL      uint64
	Kind          uint64
	Deal          reshareDealWire
	Share         reshareShareWire
	Complaint     reshareComplaintWire
	VerifyKey     reshareVerifyKeyWire
	InputRequest  reshareInputRequestWire
	InputResponse reshareInputResponseWire
}

func convertResharePayload(value, target interface{}) error {
	encoded, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}
	return rlp.DecodeBytes(encoded, target)
}

func (m ReshareMessage) EncodeRLP(w io.Writer) error {
	ttl := m.Relay.TTL
	if ttl == 0 {
		ttl = m.RelayTTL
	}
	wire := reshareMessageWire{SessionID: ReshareWireSessionID(m.SessionID), RelayTTL: ttl}
	count := 0
	for _, present := range []bool{m.Deal != nil, m.Share != nil, m.Complaint != nil, m.Response != nil,
		m.VerifyKey != nil, m.InputRequest != nil, m.InputResponse != nil} {
		if present {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("multiple reshare payloads")
	}
	var err error
	switch {
	case m.Deal != nil:
		wire.Kind = 1
		wire.Deal, err = recoveryDealForWire(m.Deal)
	case m.Share != nil:
		wire.Kind = 2
		err = convertResharePayload(m.Share, &wire.Share)
	case m.Complaint != nil:
		wire.Kind = 3
		err = convertResharePayload(m.Complaint, &wire.Complaint)
	case m.VerifyKey != nil:
		wire.Kind = 4
		err = convertResharePayload(m.VerifyKey, &wire.VerifyKey)
	case m.InputRequest != nil:
		wire.Kind = 5
		err = convertResharePayload(m.InputRequest, &wire.InputRequest)
	case m.InputResponse != nil:
		wire.Kind = 6
		err = convertResharePayload(m.InputResponse, &wire.InputResponse)
	case m.Response != nil:
		// Legacy nitch-only response type; no live sender exists in the rotation driver.
		wire.Kind = 7
		err = convertResharePayload(m.Response, &wire.Share)
	}
	if err != nil {
		return err
	}
	return rlp.Encode(w, wire)
}

func (m *ReshareMessage) DecodeRLP(s *rlp.Stream) error {
	raw, err := s.Raw()
	if err != nil {
		return err
	}
	var wire reshareMessageWire
	if err := rlp.DecodeBytes(raw, &wire); err != nil {
		return err
	}
	out := ReshareMessage{SessionID: reshareWireSessionName(wire.SessionID), RelayTTL: wire.RelayTTL, Relay: RelayMetadata{TTL: wire.RelayTTL}}
	switch wire.Kind {
	case 0:
	case 1:
		out.Deal = recoveryDealFromWire(wire.Deal)
	case 2:
		out.Share = new(chameleon.DKRSShare)
		err = convertResharePayload(wire.Share, out.Share)
	case 3:
		out.Complaint = &chameleon.DKRSComplaint{ComplainantIndex: new(big.Int).SetUint64(wire.Complaint.ComplainantIndex), DealerIndex: new(big.Int).SetUint64(wire.Complaint.DealerIndex)}
	case 4:
		out.VerifyKey = &chameleon.DKRSVerifyKey{Index: new(big.Int).SetUint64(wire.VerifyKey.Index), VerificationKey: wire.VerifyKey.VerificationKey}
	case 5:
		out.InputRequest = new(ReshareInputRequest)
		err = convertResharePayload(wire.InputRequest, out.InputRequest)
	case 6:
		out.InputResponse = new(ReshareInputResponse)
		err = convertResharePayload(wire.InputResponse, out.InputResponse)
	case 7:
		out.Response = new(chameleon.DKRSResponse)
		err = convertResharePayload(wire.Share, out.Response)
	default:
		return fmt.Errorf("unknown reshare message kind %d", wire.Kind)
	}
	if err != nil {
		return err
	}
	canonical, err := rlp.EncodeToBytes(out)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, canonical) {
		return fmt.Errorf("noncanonical unused reshare payload fields")
	}
	*m = out
	return nil
}
