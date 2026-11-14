package eth

import (
	"errors"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
)

type reshareCountingRW struct {
	writes int
	err    error
}

func (rw *reshareCountingRW) ReadMsg() (p2p.Msg, error) {
	return p2p.Msg{}, io.EOF
}

func (rw *reshareCountingRW) WriteMsg(msg p2p.Msg) error {
	if rw.err != nil {
		return rw.err
	}
	if msg.Payload != nil {
		_, _ = io.Copy(io.Discard, msg.Payload)
	}
	rw.writes++
	return nil
}

func testReshareProtocolManager(peers map[string]*reshareCountingRW, routes map[int]string) *ProtocolManager {
	set := newPeerSet()
	for id, rw := range peers {
		set.peers[id] = &peer{id: id, rw: rw}
	}
	return &ProtocolManager{
		peers:                set,
		committeePeers:       routes,
		committeeDirectPeers: routes,
		reshareSeen:          make(map[common.Hash]time.Time),
		reshareRelayTTL:      7,
	}
}

func testTargetedReshareMessage(target int) types.ReshareMessage {
	return types.ReshareMessage{
		SessionID: "targeted-transport",
		InputRequest: &types.ReshareInputRequest{
			DealerIndex:   big.NewInt(int64(target)),
			ReceiverIndex: big.NewInt(1),
			NeedDeal:      true,
			Nonce:         [32]byte{1},
		},
	}
}

func TestSendReshareToKnownIndexSkipsFallbackFlood(t *testing.T) {
	target := &reshareCountingRW{}
	fallback := &reshareCountingRW{}
	pm := testReshareProtocolManager(
		map[string]*reshareCountingRW{"target": target, "fallback": fallback},
		map[int]string{2: "target"},
	)

	success, failed := pm.SendReshareToIndex(2, testTargetedReshareMessage(2))
	if success != 1 || failed != 0 {
		t.Fatalf("unexpected send counts: success=%d failed=%d", success, failed)
	}
	if target.writes != 1 || fallback.writes != 0 {
		t.Fatalf("known route flooded peers: target=%d fallback=%d", target.writes, fallback.writes)
	}
}

func TestSendReshareFallsBackWhenRouteFails(t *testing.T) {
	target := &reshareCountingRW{err: errors.New("route down")}
	fallback := &reshareCountingRW{}
	pm := testReshareProtocolManager(
		map[string]*reshareCountingRW{"target": target, "fallback": fallback},
		map[int]string{2: "target"},
	)

	success, failed := pm.SendReshareToIndex(2, testTargetedReshareMessage(2))
	if success != 1 || failed != 2 {
		t.Fatalf("unexpected fallback counts: success=%d failed=%d", success, failed)
	}
	if fallback.writes != 1 {
		t.Fatalf("fallback peer writes=%d want 1", fallback.writes)
	}
}

func TestRelayReshareUsesKnownNextHopWithoutFlooding(t *testing.T) {
	target := &reshareCountingRW{}
	fallback := &reshareCountingRW{}
	pm := testReshareProtocolManager(
		map[string]*reshareCountingRW{"target": target, "fallback": fallback},
		map[int]string{2: "target"},
	)
	msg := testTargetedReshareMessage(2)
	msg.Relay = types.RelayMetadata{MessageID: "message", Origin: "origin", TTL: 7, Hop: 1}

	pm.relayReshareMessage(msg, nil, "message")
	if target.writes != 1 || fallback.writes != 0 {
		t.Fatalf("known next hop flooded peers: target=%d fallback=%d", target.writes, fallback.writes)
	}
}
