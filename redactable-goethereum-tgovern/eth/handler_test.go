// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/teehost"
)

func TestSendDKRSUsesTEEIdentityRoute(t *testing.T) {
	app, net := p2p.MsgPipe()
	defer app.Close()
	defer net.Close()

	pm := &ProtocolManager{
		peers:    newPeerSet(),
		teePeers: make(map[[32]byte]string),
	}
	peer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "tee-peer", nil), net, nil)
	if err := pm.peers.Register(peer); err != nil {
		t.Fatalf("register peer: %v", err)
	}
	defer pm.peers.Unregister(peer.id)

	var sigPubKey [32]byte
	sigPubKey[0] = 0x42
	pm.registerTEEPeer(peer.id, sigPubKey)

	sessionID := [32]byte{0x99}
	want := teehost.DKRSProtocolMessage{
		SessionID: sessionID,
		InputRequest: &teehost.DKRSInputRequestMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			NeedShare:      true,
			Attempt:        1,
		},
	}
	errc := make(chan error, 1)
	go func() {
		errc <- pm.SendDKRS(context.Background(), sigPubKey, want)
	}()
	msg, err := app.ReadMsg()
	if err != nil {
		t.Fatalf("read routed DKRS message: %v", err)
	}
	defer msg.Discard()
	if msg.Code != TEEDKRSMsg {
		t.Fatalf("message code = %d, want %d", msg.Code, TEEDKRSMsg)
	}
	var got teehost.DKRSProtocolMessage
	if err := msg.Decode(&got); err != nil {
		t.Fatalf("decode DKRS: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("send DKRS: %v", err)
	}
	if got.SessionID != sessionID || got.InputRequest == nil || got.InputRequest.RequesterIndex != 2 || got.InputRequest.DealerIndex != 1 {
		t.Fatalf("unexpected routed DKRS message: %#v", got)
	}
}

func TestSendDKRSFallbackBroadcastWhenRecipientMissing(t *testing.T) {
	app, net := p2p.MsgPipe()
	defer app.Close()
	defer net.Close()

	pm := &ProtocolManager{
		peers:    newPeerSet(),
		teePeers: make(map[[32]byte]string),
	}
	peer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "tee-peer", nil), net, nil)
	if err := pm.peers.Register(peer); err != nil {
		t.Fatalf("register peer: %v", err)
	}
	defer pm.peers.Unregister(peer.id)

	var missingSigPubKey [32]byte
	missingSigPubKey[0] = 0x77
	sessionID := [32]byte{0x55}
	want := teehost.DKRSProtocolMessage{
		SessionID: sessionID,
		Share: &teehost.DKRSShareMessage{
			FromIndex: 1,
			ToIndex:   2,
			Payload:   []byte("encrypted-share"),
		},
	}
	errc := make(chan error, 1)
	go func() {
		errc <- pm.SendDKRS(context.Background(), missingSigPubKey, want)
	}()
	msg, err := app.ReadMsg()
	if err != nil {
		t.Fatalf("read fallback DKRS broadcast: %v", err)
	}
	defer msg.Discard()
	if msg.Code != TEEDKRSMsg {
		t.Fatalf("message code = %d, want %d", msg.Code, TEEDKRSMsg)
	}
	var got teehost.DKRSProtocolMessage
	if err := msg.Decode(&got); err != nil {
		t.Fatalf("decode DKRS: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("fallback send DKRS: %v", err)
	}
	if got.SessionID != sessionID || got.Share == nil || got.Share.FromIndex != 1 || got.Share.ToIndex != 2 {
		t.Fatalf("unexpected fallback DKRS message: %#v", got)
	}
}

func TestSendDKRSFallbackBroadcastWhenDirectSendFails(t *testing.T) {
	deadApp, deadNet := p2p.MsgPipe()
	deadApp.Close()
	deadNet.Close()

	fallbackRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:            newPeerSet(),
		teePeers:         make(map[[32]byte]string),
		dkrsSeenMessages: make(map[common.Hash]time.Time),
	}
	deadPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "dead-tee-peer", nil), deadNet, nil)
	fallbackPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "fallback-peer", nil), fallbackRW, nil)
	if err := pm.peers.Register(deadPeer); err != nil {
		t.Fatalf("register dead peer: %v", err)
	}
	defer pm.peers.Unregister(deadPeer.id)
	if err := pm.peers.Register(fallbackPeer); err != nil {
		t.Fatalf("register fallback peer: %v", err)
	}
	defer pm.peers.Unregister(fallbackPeer.id)

	var sigPubKey [32]byte
	sigPubKey[0] = 0x42
	pm.registerTEEPeer(deadPeer.id, sigPubKey)

	want := teehost.DKRSProtocolMessage{
		SessionID: [32]byte{0x66},
		InputResponse: &teehost.DKRSInputResponseMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			HasShare:       true,
			Share:          teehost.DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share")},
		},
	}
	if err := pm.SendDKRS(context.Background(), sigPubKey, want); err != nil {
		t.Fatalf("fallback after direct failure: %v", err)
	}
	msg := <-fallbackRW.msgs
	defer msg.Discard()
	if msg.Code != TEEDKRSMsg {
		t.Fatalf("message code = %d, want %d", msg.Code, TEEDKRSMsg)
	}
	var got teehost.DKRSProtocolMessage
	if err := msg.Decode(&got); err != nil {
		t.Fatalf("decode DKRS: %v", err)
	}
	if got.SessionID != want.SessionID || got.InputResponse == nil || got.InputResponse.DealerIndex != 1 {
		t.Fatalf("unexpected fallback DKRS message: %#v", got)
	}
}

type recordingMsgRW struct {
	msgs chan p2p.Msg
}

func (rw *recordingMsgRW) ReadMsg() (p2p.Msg, error) {
	return p2p.Msg{}, fmt.Errorf("recording message reader is write-only")
}

func (rw *recordingMsgRW) WriteMsg(msg p2p.Msg) error {
	rw.msgs <- msg
	return nil
}

type fakeDKRSRelayTEEHost struct {
	distKGRecipients  map[int][32]byte
	hardwareID        [32]byte
	distKGCalls       int
	dkrsCalls         int
	partialAdaptCalls int
	materialCalls     int
	authorizedCalls   int
	// authorizedErr makes HandleRedactAuthorizedMessage fail locally, the way a
	// TEE host does when the enclave rejects the request (e.g. d_req mismatch).
	authorizedErr error
}

func (h *fakeDKRSRelayTEEHost) HandleDistKG(context.Context, teehost.DistKGProtocolMessage, teehost.ProtocolNetwork) error {
	h.distKGCalls++
	return nil
}

func (h *fakeDKRSRelayTEEHost) HandleDKRS(context.Context, teehost.DKRSProtocolMessage, teehost.ProtocolNetwork) error {
	h.dkrsCalls++
	return nil
}

func (h *fakeDKRSRelayTEEHost) HandlePartialAdapt(context.Context, teehost.PartialAdaptProtocolMessage, teehost.ProtocolNetwork) error {
	h.partialAdaptCalls++
	return nil
}

func (h *fakeDKRSRelayTEEHost) HandleRedactMaterial(context.Context, teehost.RedactionMaterial, teehost.ProtocolNetwork) error {
	h.materialCalls++
	return nil
}

func (h *fakeDKRSRelayTEEHost) HandleRedactAuthorizedMessage(context.Context, teehost.RedactAuthorizedMessage, teehost.ProtocolNetwork) error {
	h.authorizedCalls++
	return h.authorizedErr
}

func (h *fakeDKRSRelayTEEHost) HardwareID() [32]byte {
	return h.hardwareID
}

func (h *fakeDKRSRelayTEEHost) DistKGRecipient(index int) ([32]byte, bool) {
	key, ok := h.distKGRecipients[index]
	return key, ok
}

func TestDistKGRelaySeenSetAndTTL(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
	pm := &ProtocolManager{
		peers:              newPeerSet(),
		teePeers:           make(map[[32]byte]string),
		distKGRelayTTL:     1,
		distKGSeenMessages: make(map[common.Hash]time.Time),
		dkrsRelayMaxPeers:  64,
	}
	host := &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0x09}}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.DistKGProtocolMessage{
		RelayTTL: 1,
		Deal: &teehost.DistKGDealMessage{
			DealerIndex: 1,
			Threshold:   2,
			NumParties:  4,
			PHex:        "73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001",
		},
	}
	if err := sendInboundDistKGForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle inbound DistKG: %v", err)
	}
	relayed := <-dstRW.msgs
	defer relayed.Discard()
	if relayed.Code != TEEDistKGMsg {
		t.Fatalf("relayed code = %d, want %d", relayed.Code, TEEDistKGMsg)
	}
	var got teehost.DistKGProtocolMessage
	if err := relayed.Decode(&got); err != nil {
		t.Fatalf("decode relayed DistKG: %v", err)
	}
	if got.RelayTTL != 0 || got.Deal == nil || got.Deal.DealerIndex != 1 {
		t.Fatalf("unexpected relayed DistKG: %#v", got)
	}

	if err := sendInboundDistKGForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle duplicate DistKG: %v", err)
	}
	select {
	case duplicate := <-dstRW.msgs:
		duplicate.Discard()
		t.Fatal("duplicate DistKG message was relayed")
	default:
	}
	if host.distKGCalls != 1 {
		t.Fatalf("HandleDistKG calls = %d, want 1", host.distKGCalls)
	}
}

func sendInboundDistKGForTest(pm *ProtocolManager, srcPeer *peer, srcApp p2p.MsgReadWriter, msg teehost.DistKGProtocolMessage) error {
	errc := make(chan error, 1)
	go func() {
		errc <- pm.handleMsg(srcPeer)
	}()
	if err := p2p.Send(srcApp, TEEDistKGMsg, msg); err != nil {
		return err
	}
	select {
	case err := <-errc:
		return err
	case <-time.After(time.Second):
		return fmt.Errorf("timed out handling inbound DistKG")
	}
}

func TestDKRSRelaySeenSet(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  true,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
	}
	host := &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0x10}}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.DKRSProtocolMessage{
		SessionID: [32]byte{0x88},
		RelayTTL:  1,
		InputRequest: &teehost.DKRSInputRequestMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			NeedDeal:       true,
			NeedShare:      true,
			Attempt:        1,
		},
	}
	if err := sendInboundDKRSForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle inbound DKRS: %v", err)
	}
	relayed := <-dstRW.msgs
	defer relayed.Discard()
	if relayed.Code != TEEDKRSMsg {
		t.Fatalf("relayed code = %d, want %d", relayed.Code, TEEDKRSMsg)
	}
	var got teehost.DKRSProtocolMessage
	if err := relayed.Decode(&got); err != nil {
		t.Fatalf("decode relayed DKRS: %v", err)
	}
	if got.SessionID != msg.SessionID || got.RelayTTL != 0 || got.InputRequest == nil || got.InputRequest.DealerIndex != 1 {
		t.Fatalf("unexpected relayed DKRS: %#v", got)
	}

	if err := sendInboundDKRSForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle duplicate DKRS: %v", err)
	}
	select {
	case duplicate := <-dstRW.msgs:
		duplicate.Discard()
		t.Fatal("duplicate DKRS message was relayed")
	default:
	}
	if host.dkrsCalls != 1 {
		t.Fatalf("HandleDKRS calls = %d, want 1", host.dkrsCalls)
	}
}

func TestDKRSRelayTTLStopsPropagation(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  true,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
	}
	host := &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0x30}}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.DKRSProtocolMessage{
		SessionID: [32]byte{0x45},
		RelayTTL:  0,
		InputRequest: &teehost.DKRSInputRequestMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			NeedShare:      true,
			Attempt:        1,
		},
	}
	if err := sendInboundDKRSForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle inbound DKRS: %v", err)
	}
	select {
	case relayed := <-dstRW.msgs:
		relayed.Discard()
		t.Fatal("DKRS message was relayed after TTL expired")
	default:
	}
	if host.dkrsCalls != 1 {
		t.Fatalf("HandleDKRS calls = %d, want 1", host.dkrsCalls)
	}
}

// TestRedactAuthorizedRelaySurvivesLocalHandlerError pins the regression behind
// the ~5s edit stalls: only the TEE hosts bridged directly onto the sealer are
// woken by the committee's first hop, so every other host depends on the
// overlay relay. A local handler error (e.g. the enclave rejecting the request)
// must not abort the relay, otherwise the remaining hosts stay asleep until the
// authorization transaction is mined and the contract event wakes them.
func TestRedactAuthorizedRelaySurvivesLocalHandlerError(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  true,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
	}
	host := &fakeDKRSRelayTEEHost{
		hardwareID:    [32]byte{0x31},
		authorizedErr: errors.New("tee_partial_adapt failed: -1"),
	}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{0x31}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{0x32}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.RedactAuthorizedMessage{
		ReqID:        [32]byte{0x77},
		Epoch:        1,
		AuthDigest:   [32]byte{0x78},
		AggregateSig: []byte{0x79, 0x7a},
		RelayTTL:     2,
	}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEERedactAuthorizedMsg, msg); err != nil {
		t.Fatalf("handle inbound redact authorized: %v", err)
	}
	if host.authorizedCalls != 1 {
		t.Fatalf("HandleRedactAuthorizedMessage calls = %d, want 1", host.authorizedCalls)
	}
	select {
	case relayed := <-dstRW.msgs:
		if relayed.Code != TEERedactAuthorizedMsg {
			t.Fatalf("relayed code = %d, want %d", relayed.Code, TEERedactAuthorizedMsg)
		}
		var got teehost.RedactAuthorizedMessage
		if err := relayed.Decode(&got); err != nil {
			t.Fatalf("decode relayed redact authorized: %v", err)
		}
		if got.ReqID != msg.ReqID || got.RelayTTL != msg.RelayTTL-1 {
			t.Fatalf("unexpected relayed redact authorized: %#v", got)
		}
	default:
		t.Fatal("redact authorization was not relayed after a local handler error")
	}
}

// TestRedactAuthorizedRelayedByOrdinaryNode covers the relay hop through a node
// that has no TEE host (the sealer itself, observers, and light nodes). The
// committee injects the first hop at the sealer, so if ordinary nodes drop the
// message instead of forwarding it, the TEE overlay is unreachable beyond the
// bridged hosts.
func TestRedactAuthorizedRelayedByOrdinaryNode(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  true,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
		// No SetTEEHost: this node is an ordinary relay in the overlay.
	}

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{0x41}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{0x42}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.RedactAuthorizedMessage{
		ReqID:        [32]byte{0x81},
		Epoch:        1,
		AuthDigest:   [32]byte{0x82},
		AggregateSig: []byte{0x83, 0x84},
		RelayTTL:     2,
	}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEERedactAuthorizedMsg, msg); err != nil {
		t.Fatalf("handle inbound redact authorized: %v", err)
	}
	select {
	case relayed := <-dstRW.msgs:
		if relayed.Code != TEERedactAuthorizedMsg {
			t.Fatalf("relayed code = %d, want %d", relayed.Code, TEERedactAuthorizedMsg)
		}
	default:
		t.Fatal("ordinary node did not relay the redact authorization")
	}
}

func TestDKRSRelayDisabled(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  false,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
	}
	host := &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0x20}}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	msg := teehost.DKRSProtocolMessage{
		SessionID: [32]byte{0x44},
		InputResponse: &teehost.DKRSInputResponseMessage{
			RequesterIndex: 2,
			DealerIndex:    1,
			HasShare:       true,
			Share:          teehost.DKRSShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte("share")},
		},
	}
	if err := sendInboundDKRSForTest(pm, srcPeer, srcApp, msg); err != nil {
		t.Fatalf("handle inbound DKRS: %v", err)
	}
	select {
	case relayed := <-dstRW.msgs:
		relayed.Discard()
		t.Fatal("DKRS message was relayed while relay is disabled")
	default:
	}
	if host.dkrsCalls != 1 {
		t.Fatalf("HandleDKRS calls = %d, want 1", host.dkrsCalls)
	}
}

func sendInboundDKRSForTest(pm *ProtocolManager, srcPeer *peer, srcApp p2p.MsgReadWriter, msg teehost.DKRSProtocolMessage) error {
	errc := make(chan error, 1)
	go func() {
		errc <- pm.handleMsg(srcPeer)
	}()
	if err := p2p.Send(srcApp, TEEDKRSMsg, msg); err != nil {
		return err
	}
	select {
	case err := <-errc:
		return err
	case <-time.After(time.Second):
		return fmt.Errorf("timed out handling inbound DKRS")
	}
}

func TestTEEEditMessagesRelayWithTTLAndDeduplication(t *testing.T) {
	srcApp, srcNet := p2p.MsgPipe()
	defer srcApp.Close()
	defer srcNet.Close()

	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 4)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		teePeers:          make(map[[32]byte]string),
		dkrsRelayEnabled:  true,
		dkrsRelayMaxPeers: 64,
		dkrsSeenMessages:  make(map[common.Hash]time.Time),
	}
	host := &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0x40}}
	pm.SetTEEHost(host)

	srcPeer := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "src-peer", nil), srcNet, nil)
	dstPeer := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "dst-peer", nil), dstRW, nil)
	if err := pm.peers.Register(srcPeer); err != nil {
		t.Fatalf("register src peer: %v", err)
	}
	defer pm.peers.Unregister(srcPeer.id)
	if err := pm.peers.Register(dstPeer); err != nil {
		t.Fatalf("register dst peer: %v", err)
	}
	defer pm.peers.Unregister(dstPeer.id)

	reqID := [32]byte{0x91}
	partial := teehost.PartialAdaptProtocolMessage{
		RelayTTL: 1,
		Contribution: &teehost.PartialAdaptContributionMessage{
			ReqID: reqID,
			Index: 1,
		},
	}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEEPartialAdaptMsg, partial); err != nil {
		t.Fatalf("handle inbound partial adapt: %v", err)
	}
	relayedPartial := <-dstRW.msgs
	if relayedPartial.Code != TEEPartialAdaptMsg {
		t.Fatalf("relayed code = %d, want %d", relayedPartial.Code, TEEPartialAdaptMsg)
	}
	var gotPartial teehost.PartialAdaptProtocolMessage
	if err := relayedPartial.Decode(&gotPartial); err != nil {
		t.Fatalf("decode relayed partial adapt: %v", err)
	}
	relayedPartial.Discard()
	if gotPartial.RelayTTL != 0 || gotPartial.Contribution == nil || gotPartial.Contribution.ReqID != reqID {
		t.Fatalf("unexpected relayed partial adapt: %#v", gotPartial)
	}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEEPartialAdaptMsg, partial); err != nil {
		t.Fatalf("handle duplicate partial adapt: %v", err)
	}
	select {
	case duplicate := <-dstRW.msgs:
		duplicate.Discard()
		t.Fatal("duplicate partial adapt message was relayed")
	default:
	}
	if host.partialAdaptCalls != 1 {
		t.Fatalf("HandlePartialAdapt calls = %d, want 1", host.partialAdaptCalls)
	}

	material := teehost.RedactionMaterial{RelayTTL: 1, ReqID: reqID, Epoch: big.NewInt(1)}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEERedactMaterialMsg, material); err != nil {
		t.Fatalf("handle inbound redact material: %v", err)
	}
	relayedMaterial := <-dstRW.msgs
	if relayedMaterial.Code != TEERedactMaterialMsg {
		t.Fatalf("relayed code = %d, want %d", relayedMaterial.Code, TEERedactMaterialMsg)
	}
	var gotMaterial teehost.RedactionMaterial
	if err := relayedMaterial.Decode(&gotMaterial); err != nil {
		t.Fatalf("decode relayed redact material: %v", err)
	}
	relayedMaterial.Discard()
	if gotMaterial.RelayTTL != 0 || gotMaterial.ReqID != reqID {
		t.Fatalf("unexpected relayed redact material: %#v", gotMaterial)
	}
	if err := sendInboundTEEEditForTest(pm, srcPeer, srcApp, TEERedactMaterialMsg, material); err != nil {
		t.Fatalf("handle duplicate redact material: %v", err)
	}
	select {
	case duplicate := <-dstRW.msgs:
		duplicate.Discard()
		t.Fatal("duplicate redact material was relayed")
	default:
	}
	if host.materialCalls != 1 {
		t.Fatalf("HandleRedactMaterial calls = %d, want 1", host.materialCalls)
	}
}

func TestTEEEditMessagesUseConfiguredRelayTTL(t *testing.T) {
	pm := &ProtocolManager{dkrsRelayTTL: 17}
	partial := pm.preparePartialAdaptOutbound(teehost.PartialAdaptProtocolMessage{})
	if partial.RelayTTL != 17 {
		t.Fatalf("partial adapt RelayTTL = %d, want 17", partial.RelayTTL)
	}
	material := pm.prepareRedactMaterialOutbound(teehost.RedactionMaterial{})
	if material.RelayTTL != 17 {
		t.Fatalf("redact material RelayTTL = %d, want 17", material.RelayTTL)
	}
}

func sendInboundTEEEditForTest(pm *ProtocolManager, srcPeer *peer, srcApp p2p.MsgReadWriter, code uint64, payload interface{}) error {
	errc := make(chan error, 1)
	go func() {
		errc <- pm.handleMsg(srcPeer)
	}()
	if err := p2p.Send(srcApp, code, payload); err != nil {
		return err
	}
	select {
	case err := <-errc:
		return err
	case <-time.After(time.Second):
		return fmt.Errorf("timed out handling inbound TEE edit message %d", code)
	}
}

func TestTxModifyRequestAnsweredFromCache(t *testing.T) {
	originRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
	relayRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
	pm := &ProtocolManager{
		peers:            newPeerSet(),
		txModifyCache:    make(map[[32]byte]txModifyCacheEntry),
		txModifyRequests: make(map[common.Hash]time.Time),
		txModifyRelayTTL: 1,
	}
	origin := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "origin", nil), originRW, nil)
	relay := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "relay", nil), relayRW, nil)
	if err := pm.peers.Register(origin); err != nil {
		t.Fatalf("register origin: %v", err)
	}
	defer pm.peers.Unregister(origin.id)
	if err := pm.peers.Register(relay); err != nil {
		t.Fatalf("register relay: %v", err)
	}
	defer pm.peers.Unregister(relay.id)

	reqID := [32]byte{0x42}
	blockNum := big.NewInt(367)
	modify := types.ModifyMsg{
		ReqID:    reqID,
		NewBlock: types.NewBlockWithHeader(&types.Header{Number: blockNum}),
		ProposalTx: types.ProposalTx{
			Msg: types.ModifyInfo{BlockNum: blockNum, Index: 7},
		},
	}
	pm.cacheTxModifyMsg(modify)

	req := types.TxModifyRequest{ReqID: reqID, BlockNum: blockNum, Index: 7, Nonce: 1, RelayTTL: 1}
	pm.handleTxModifyRequest(req, origin)

	response := <-originRW.msgs
	defer response.Discard()
	if response.Code != TxModifyMsg {
		t.Fatalf("response code = %d, want %d", response.Code, TxModifyMsg)
	}
	var got types.ModifyMsg
	if err := response.Decode(&got); err != nil {
		t.Fatalf("decode cached modify response: %v", err)
	}
	if got.ReqID != reqID || got.ProposalTx.Msg.Index != 7 || got.RelayTTL != 1 {
		t.Fatalf("unexpected cached modify response: %#v", got)
	}

	relayed := <-relayRW.msgs
	defer relayed.Discard()
	if relayed.Code != TxModifyRequestMsg {
		t.Fatalf("relayed code = %d, want %d", relayed.Code, TxModifyRequestMsg)
	}
	var gotReq types.TxModifyRequest
	if err := relayed.Decode(&gotReq); err != nil {
		t.Fatalf("decode relayed modify request: %v", err)
	}
	if gotReq.ReqID != reqID || gotReq.RelayTTL != 0 {
		t.Fatalf("unexpected relayed modify request: %#v", gotReq)
	}
}

func TestTxModifyOutboundUsesConfiguredRelayTTL(t *testing.T) {
	pm := &ProtocolManager{}
	pm.ConfigureTxModifyRelay(17)

	msg := pm.prepareTxModifyOutbound(types.ModifyMsg{ReqID: [32]byte{0x61}})
	if msg.RelayTTL != 17 {
		t.Fatalf("ModifyMsg RelayTTL = %d, want 17", msg.RelayTTL)
	}
	req := pm.prepareTxModifyRequestOutbound(types.TxModifyRequest{ReqID: [32]byte{0x62}})
	if req.RelayTTL != 17 {
		t.Fatalf("TxModifyRequest RelayTTL = %d, want 17", req.RelayTTL)
	}

	pm.ConfigureTxModifyRelay(-1)
	msg = pm.prepareTxModifyOutbound(types.ModifyMsg{ReqID: [32]byte{0x63}})
	if msg.RelayTTL != 0 {
		t.Fatalf("negative configured relay TTL should clamp to zero, got %d", msg.RelayTTL)
	}
}

func TestTxModifyGossipTTLStopsPropagation(t *testing.T) {
	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:             newPeerSet(),
		txModifySeen:      make(map[common.Hash]time.Time),
		appliedRedactions: make(map[[32]byte]struct{}),
		pendingRedactions: make(map[[32]byte]struct{}),
	}
	origin := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "origin", nil), &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}, nil)
	relay := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "relay", nil), dstRW, nil)
	if err := pm.peers.Register(origin); err != nil {
		t.Fatalf("register origin: %v", err)
	}
	defer pm.peers.Unregister(origin.id)
	if err := pm.peers.Register(relay); err != nil {
		t.Fatalf("register relay: %v", err)
	}
	defer pm.peers.Unregister(relay.id)

	pm.handleTxModifyGossip(types.ModifyMsg{ReqID: [32]byte{0x77}, RelayTTL: 0}, origin)

	select {
	case relayed := <-dstRW.msgs:
		relayed.Discard()
		t.Fatal("TxModifyMsg was relayed after TTL expired")
	default:
	}
}

func TestTxModifyRequestTTLStopsPropagation(t *testing.T) {
	dstRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}
	pm := &ProtocolManager{
		peers:            newPeerSet(),
		txModifyRequests: make(map[common.Hash]time.Time),
	}
	origin := newPeer(eth65, p2p.NewPeer(enode.ID{1}, "origin", nil), &recordingMsgRW{msgs: make(chan p2p.Msg, 1)}, nil)
	relay := newPeer(eth65, p2p.NewPeer(enode.ID{2}, "relay", nil), dstRW, nil)
	if err := pm.peers.Register(origin); err != nil {
		t.Fatalf("register origin: %v", err)
	}
	defer pm.peers.Unregister(origin.id)
	if err := pm.peers.Register(relay); err != nil {
		t.Fatalf("register relay: %v", err)
	}
	defer pm.peers.Unregister(relay.id)

	pm.handleTxModifyRequest(types.TxModifyRequest{ReqID: [32]byte{0x78}, Index: 7, RelayTTL: 0}, origin)

	select {
	case relayed := <-dstRW.msgs:
		relayed.Discard()
		t.Fatal("TxModifyRequest was relayed after TTL expired")
	default:
	}
}

func TestTxModifyApplyStateAllowsRetryAfterFailure(t *testing.T) {
	pm := &ProtocolManager{
		appliedRedactions: make(map[[32]byte]struct{}),
		pendingRedactions: make(map[[32]byte]struct{}),
	}
	key := [32]byte{0x99}
	if !pm.beginTxModifyApply(key) {
		t.Fatal("first apply attempt should start")
	}
	if pm.beginTxModifyApply(key) {
		t.Fatal("concurrent apply attempt should be blocked")
	}
	pm.finishTxModifyApply(key, false)
	if !pm.beginTxModifyApply(key) {
		t.Fatal("failed apply should be retryable")
	}
	pm.finishTxModifyApply(key, true)
	if pm.beginTxModifyApply(key) {
		t.Fatal("successful apply should not be retried")
	}
}

func TestTxModifyTimingOnlyCompletesForSuccessfulONUpdate(t *testing.T) {
	pm, _ := newTestProtocolManagerMust(t, downloader.FullSync, 1, nil, nil)
	defer pm.Stop()
	pm.nodeRole = "on"
	pm.port = "18551"
	pm.waitRedactionVisible = func(reqID [32]byte, timeout time.Duration) error { return nil }

	type observation struct {
		event   string
		joinKey string
	}
	events := make(chan observation, 16)
	pm.timingSink = func(stage string, event string, fields ...interface{}) {
		if stage != "ledger_update" {
			return
		}
		obs := observation{event: event}
		for i := 0; i+1 < len(fields); i += 2 {
			if fields[i] == "join_key" {
				obs.joinKey = fmt.Sprint(fields[i+1])
			}
		}
		events <- obs
	}
	waitFor := func(reqID [32]byte, event string) {
		t.Helper()
		wantKey := common.Bytes2Hex(reqID[:])
		deadline := time.After(time.Second)
		for {
			select {
			case obs := <-events:
				if obs.joinKey == wantKey && obs.event == event {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %s/%s", wantKey, event)
			}
		}
	}

	oldBlock := pm.blockchain.GetBlockByNumber(1)
	if oldBlock == nil {
		t.Fatal("test block is missing")
	}
	newBlock := types.NewBlockWithHeader(oldBlock.Header()).WithBody(oldBlock.Transactions(), oldBlock.Uncles())
	reqID := [32]byte{0xa1}
	msg := types.ModifyMsg{
		ReqID:    reqID,
		NewBlock: newBlock,
		ProposalTx: types.ProposalTx{Msg: types.ModifyInfo{
			BlockNum: big.NewInt(1),
			Index:    0,
		}},
	}
	pm.handleTxModifyGossip(msg, nil)
	waitFor(reqID, "tx_modify_received")
	waitFor(reqID, "local_block_updated")

	pm.handleTxModifyGossip(msg, nil)
	waitFor(reqID, "tx_modify_duplicate")

	pm.nodeRole = "light"
	pm.emitLedgerTiming("tx_modify_received", types.ModifyMsg{ReqID: [32]byte{0xb1}})
	select {
	case obs := <-events:
		t.Fatalf("light node emitted ledger timing: %#v", obs)
	default:
	}

	pm.nodeRole = "on"
	pm.teeHost = &fakeDKRSRelayTEEHost{hardwareID: [32]byte{0xc1}}
	pm.emitLedgerTiming("tx_modify_received", types.ModifyMsg{ReqID: [32]byte{0xc2}})
	select {
	case obs := <-events:
		t.Fatalf("TEE host emitted ON ledger timing: %#v", obs)
	default:
	}

	pm.teeHost = nil
	failedReqID := [32]byte{0xd1}
	pm.handleTxModifyGossip(types.ModifyMsg{
		ReqID: failedReqID,
		ProposalTx: types.ProposalTx{Msg: types.ModifyInfo{
			BlockNum: big.NewInt(1),
			Index:    0,
		}},
	}, nil)
	waitFor(failedReqID, "tx_modify_received")
	waitFor(failedReqID, "tx_modify_failed")
	select {
	case obs := <-events:
		if obs.joinKey == common.Bytes2Hex(failedReqID[:]) && obs.event == "local_block_updated" {
			t.Fatal("failed update emitted successful completion")
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// Tests that block headers can be retrieved from a remote chain based on user queries.
func TestGetBlockHeaders63(t *testing.T) { testGetBlockHeaders(t, 63) }
func TestGetBlockHeaders64(t *testing.T) { testGetBlockHeaders(t, 64) }

func testGetBlockHeaders(t *testing.T, protocol int) {
	pm, _ := newTestProtocolManagerMust(t, downloader.FullSync, downloader.MaxHashFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Create a "random" unknown hash for testing
	var unknown common.Hash
	for i := range unknown {
		unknown[i] = byte(i)
	}
	// Create a batch of tests for various scenarios
	limit := uint64(downloader.MaxHeaderFetch)
	tests := []struct {
		query  *getBlockHeadersData // The query to execute for header retrieval
		expect []common.Hash        // The hashes of the block whose headers are expected
	}{
		// A single random block should be retrievable by hash and number too
		{
			&getBlockHeadersData{Origin: hashOrNumber{Hash: pm.blockchain.GetBlockByNumber(limit / 2).Hash()}, Amount: 1},
			[]common.Hash{pm.blockchain.GetBlockByNumber(limit / 2).Hash()},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: limit / 2}, Amount: 1},
			[]common.Hash{pm.blockchain.GetBlockByNumber(limit / 2).Hash()},
		},
		// Multiple headers should be retrievable in both directions
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: limit / 2}, Amount: 3},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(limit / 2).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 + 1).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 + 2).Hash(),
			},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: limit / 2}, Amount: 3, Reverse: true},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(limit / 2).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 - 1).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 - 2).Hash(),
			},
		},
		// Multiple headers with skip lists should be retrievable
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: limit / 2}, Skip: 3, Amount: 3},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(limit / 2).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 + 4).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 + 8).Hash(),
			},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: limit / 2}, Skip: 3, Amount: 3, Reverse: true},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(limit / 2).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 - 4).Hash(),
				pm.blockchain.GetBlockByNumber(limit/2 - 8).Hash(),
			},
		},
		// The chain endpoints should be retrievable
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: 0}, Amount: 1},
			[]common.Hash{pm.blockchain.GetBlockByNumber(0).Hash()},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: pm.blockchain.CurrentBlock().NumberU64()}, Amount: 1},
			[]common.Hash{pm.blockchain.CurrentBlock().Hash()},
		},
		// Ensure protocol limits are honored
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: pm.blockchain.CurrentBlock().NumberU64() - 1}, Amount: limit + 10, Reverse: true},
			pm.blockchain.GetBlockHashesFromHash(pm.blockchain.CurrentBlock().Hash(), limit),
		},
		// Check that requesting more than available is handled gracefully
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: pm.blockchain.CurrentBlock().NumberU64() - 4}, Skip: 3, Amount: 3},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(pm.blockchain.CurrentBlock().NumberU64() - 4).Hash(),
				pm.blockchain.GetBlockByNumber(pm.blockchain.CurrentBlock().NumberU64()).Hash(),
			},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: 4}, Skip: 3, Amount: 3, Reverse: true},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(4).Hash(),
				pm.blockchain.GetBlockByNumber(0).Hash(),
			},
		},
		// Check that requesting more than available is handled gracefully, even if mid skip
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: pm.blockchain.CurrentBlock().NumberU64() - 4}, Skip: 2, Amount: 3},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(pm.blockchain.CurrentBlock().NumberU64() - 4).Hash(),
				pm.blockchain.GetBlockByNumber(pm.blockchain.CurrentBlock().NumberU64() - 1).Hash(),
			},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: 4}, Skip: 2, Amount: 3, Reverse: true},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(4).Hash(),
				pm.blockchain.GetBlockByNumber(1).Hash(),
			},
		},
		// Check a corner case where requesting more can iterate past the endpoints
		{
			&getBlockHeadersData{Origin: hashOrNumber{Number: 2}, Amount: 5, Reverse: true},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(2).Hash(),
				pm.blockchain.GetBlockByNumber(1).Hash(),
				pm.blockchain.GetBlockByNumber(0).Hash(),
			},
		},
		// Check a corner case where skipping overflow loops back into the chain start
		{
			&getBlockHeadersData{Origin: hashOrNumber{Hash: pm.blockchain.GetBlockByNumber(3).Hash()}, Amount: 2, Reverse: false, Skip: math.MaxUint64 - 1},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(3).Hash(),
			},
		},
		// Check a corner case where skipping overflow loops back to the same header
		{
			&getBlockHeadersData{Origin: hashOrNumber{Hash: pm.blockchain.GetBlockByNumber(1).Hash()}, Amount: 2, Reverse: false, Skip: math.MaxUint64},
			[]common.Hash{
				pm.blockchain.GetBlockByNumber(1).Hash(),
			},
		},
		// Check that non existing headers aren't returned
		{
			&getBlockHeadersData{Origin: hashOrNumber{Hash: unknown}, Amount: 1},
			[]common.Hash{},
		}, {
			&getBlockHeadersData{Origin: hashOrNumber{Number: pm.blockchain.CurrentBlock().NumberU64() + 1}, Amount: 1},
			[]common.Hash{},
		},
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Collect the headers to expect in the response
		headers := []*types.Header{}
		for _, hash := range tt.expect {
			headers = append(headers, pm.blockchain.GetBlockByHash(hash).Header())
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x03, tt.query)
		if err := p2p.ExpectMsg(peer.app, 0x04, headers); err != nil {
			t.Errorf("test %d: headers mismatch: %v", i, err)
		}
		// If the test used number origins, repeat with hashes as the too
		if tt.query.Origin.Hash == (common.Hash{}) {
			if origin := pm.blockchain.GetBlockByNumber(tt.query.Origin.Number); origin != nil {
				tt.query.Origin.Hash, tt.query.Origin.Number = origin.Hash(), 0

				p2p.Send(peer.app, 0x03, tt.query)
				if err := p2p.ExpectMsg(peer.app, 0x04, headers); err != nil {
					t.Errorf("test %d: headers mismatch: %v", i, err)
				}
			}
		}
	}
}

// Tests that block contents can be retrieved from a remote chain based on their hashes.
func TestGetBlockBodies63(t *testing.T) { testGetBlockBodies(t, 63) }
func TestGetBlockBodies64(t *testing.T) { testGetBlockBodies(t, 64) }

func testGetBlockBodies(t *testing.T, protocol int) {
	pm, _ := newTestProtocolManagerMust(t, downloader.FullSync, downloader.MaxBlockFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Create a batch of tests for various scenarios
	limit := downloader.MaxBlockFetch
	tests := []struct {
		random    int           // Number of blocks to fetch randomly from the chain
		explicit  []common.Hash // Explicitly requested blocks
		available []bool        // Availability of explicitly requested blocks
		expected  int           // Total number of existing blocks to expect
	}{
		{1, nil, nil, 1},             // A single random block should be retrievable
		{10, nil, nil, 10},           // Multiple random blocks should be retrievable
		{limit, nil, nil, limit},     // The maximum possible blocks should be retrievable
		{limit + 1, nil, nil, limit}, // No more than the possible block count should be returned
		{0, []common.Hash{pm.blockchain.Genesis().Hash()}, []bool{true}, 1},      // The genesis block should be retrievable
		{0, []common.Hash{pm.blockchain.CurrentBlock().Hash()}, []bool{true}, 1}, // The chains head block should be retrievable
		{0, []common.Hash{{}}, []bool{false}, 0},                                 // A non existent block should not be returned

		// Existing and non-existing blocks interleaved should not cause problems
		{0, []common.Hash{
			{},
			pm.blockchain.GetBlockByNumber(1).Hash(),
			{},
			pm.blockchain.GetBlockByNumber(10).Hash(),
			{},
			pm.blockchain.GetBlockByNumber(100).Hash(),
			{},
		}, []bool{false, true, false, true, false, true, false}, 3},
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Collect the hashes to request, and the response to expect
		hashes, seen := []common.Hash{}, make(map[int64]bool)
		bodies := []*blockBody{}

		for j := 0; j < tt.random; j++ {
			for {
				num := rand.Int63n(int64(pm.blockchain.CurrentBlock().NumberU64()))
				if !seen[num] {
					seen[num] = true

					block := pm.blockchain.GetBlockByNumber(uint64(num))
					hashes = append(hashes, block.Hash())
					if len(bodies) < tt.expected {
						bodies = append(bodies, &blockBody{Transactions: block.Transactions(), Uncles: block.Uncles()})
					}
					break
				}
			}
		}
		for j, hash := range tt.explicit {
			hashes = append(hashes, hash)
			if tt.available[j] && len(bodies) < tt.expected {
				block := pm.blockchain.GetBlockByHash(hash)
				bodies = append(bodies, &blockBody{Transactions: block.Transactions(), Uncles: block.Uncles()})
			}
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x05, hashes)
		if err := p2p.ExpectMsg(peer.app, 0x06, bodies); err != nil {
			t.Errorf("test %d: bodies mismatch: %v", i, err)
		}
	}
}

// Tests that the node state database can be retrieved based on hashes.
func TestGetNodeData63(t *testing.T) { testGetNodeData(t, 63) }
func TestGetNodeData64(t *testing.T) { testGetNodeData(t, 64) }

func testGetNodeData(t *testing.T, protocol int) {
	// Define three accounts to simulate transactions with
	acc1Key, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	acc2Key, _ := crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	acc1Addr := crypto.PubkeyToAddress(acc1Key.PublicKey)
	acc2Addr := crypto.PubkeyToAddress(acc2Key.PublicKey)

	signer := types.HomesteadSigner{}
	// Create a chain generator with some simple transactions (blatantly stolen from @fjl/chain_markets_test)
	generator := func(i int, block *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, the test bank sends account #1 some ether.
			tx, _ := types.SignTx(types.NewTransaction(block.TxNonce(testBank), acc1Addr, big.NewInt(10000), params.TxGas, nil, nil), signer, testBankKey)
			block.AddTx(tx)
		case 1:
			// In block 2, the test bank sends some more ether to account #1.
			// acc1Addr passes it on to account #2.
			tx1, _ := types.SignTx(types.NewTransaction(block.TxNonce(testBank), acc1Addr, big.NewInt(1000), params.TxGas, nil, nil), signer, testBankKey)
			tx2, _ := types.SignTx(types.NewTransaction(block.TxNonce(acc1Addr), acc2Addr, big.NewInt(1000), params.TxGas, nil, nil), signer, acc1Key)
			block.AddTx(tx1)
			block.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by account #2.
			block.SetCoinbase(acc2Addr)
			block.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := block.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			block.AddUncle(b2)
			b3 := block.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			block.AddUncle(b3)
		}
	}
	// Assemble the test environment
	pm, db := newTestProtocolManagerMust(t, downloader.FullSync, 4, generator, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Fetch for now the entire chain db
	hashes := []common.Hash{}

	it := db.NewIterator()
	for it.Next() {
		if key := it.Key(); len(key) == common.HashLength {
			hashes = append(hashes, common.BytesToHash(key))
		}
	}
	it.Release()

	p2p.Send(peer.app, 0x0d, hashes)
	msg, err := peer.app.ReadMsg()
	if err != nil {
		t.Fatalf("failed to read node data response: %v", err)
	}
	if msg.Code != 0x0e {
		t.Fatalf("response packet code mismatch: have %x, want %x", msg.Code, 0x0c)
	}
	var data [][]byte
	if err := msg.Decode(&data); err != nil {
		t.Fatalf("failed to decode response node data: %v", err)
	}
	// Verify that all hashes correspond to the requested data, and reconstruct a state tree
	for i, want := range hashes {
		if hash := crypto.Keccak256Hash(data[i]); hash != want {
			t.Errorf("data hash mismatch: have %x, want %x", hash, want)
		}
	}
	statedb := rawdb.NewMemoryDatabase()
	for i := 0; i < len(data); i++ {
		statedb.Put(hashes[i].Bytes(), data[i])
	}
	accounts := []common.Address{testBank, acc1Addr, acc2Addr}
	for i := uint64(0); i <= pm.blockchain.CurrentBlock().NumberU64(); i++ {
		trie, _ := state.New(pm.blockchain.GetBlockByNumber(i).Root(), state.NewDatabase(statedb))

		for j, acc := range accounts {
			state, _ := pm.blockchain.State()
			bw := state.GetBalance(acc)
			bh := trie.GetBalance(acc)

			if (bw != nil && bh == nil) || (bw == nil && bh != nil) {
				t.Errorf("test %d, account %d: balance mismatch: have %v, want %v", i, j, bh, bw)
			}
			if bw != nil && bh != nil && bw.Cmp(bw) != 0 {
				t.Errorf("test %d, account %d: balance mismatch: have %v, want %v", i, j, bh, bw)
			}
		}
	}
}

// Tests that the transaction receipts can be retrieved based on hashes.
func TestGetReceipt63(t *testing.T) { testGetReceipt(t, 63) }
func TestGetReceipt64(t *testing.T) { testGetReceipt(t, 64) }

func testGetReceipt(t *testing.T, protocol int) {
	// Define three accounts to simulate transactions with
	acc1Key, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	acc2Key, _ := crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	acc1Addr := crypto.PubkeyToAddress(acc1Key.PublicKey)
	acc2Addr := crypto.PubkeyToAddress(acc2Key.PublicKey)

	signer := types.HomesteadSigner{}
	// Create a chain generator with some simple transactions (blatantly stolen from @fjl/chain_markets_test)
	generator := func(i int, block *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, the test bank sends account #1 some ether.
			tx, _ := types.SignTx(types.NewTransaction(block.TxNonce(testBank), acc1Addr, big.NewInt(10000), params.TxGas, nil, nil), signer, testBankKey)
			block.AddTx(tx)
		case 1:
			// In block 2, the test bank sends some more ether to account #1.
			// acc1Addr passes it on to account #2.
			tx1, _ := types.SignTx(types.NewTransaction(block.TxNonce(testBank), acc1Addr, big.NewInt(1000), params.TxGas, nil, nil), signer, testBankKey)
			tx2, _ := types.SignTx(types.NewTransaction(block.TxNonce(acc1Addr), acc2Addr, big.NewInt(1000), params.TxGas, nil, nil), signer, acc1Key)
			block.AddTx(tx1)
			block.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by account #2.
			block.SetCoinbase(acc2Addr)
			block.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := block.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			block.AddUncle(b2)
			b3 := block.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			block.AddUncle(b3)
		}
	}
	// Assemble the test environment
	pm, _ := newTestProtocolManagerMust(t, downloader.FullSync, 4, generator, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Collect the hashes to request, and the response to expect
	hashes, receipts := []common.Hash{}, []types.Receipts{}
	for i := uint64(0); i <= pm.blockchain.CurrentBlock().NumberU64(); i++ {
		block := pm.blockchain.GetBlockByNumber(i)

		hashes = append(hashes, block.Hash())
		receipts = append(receipts, pm.blockchain.GetReceiptsByHash(block.Hash()))
	}
	// Send the hash request and verify the response
	p2p.Send(peer.app, 0x0f, hashes)
	if err := p2p.ExpectMsg(peer.app, 0x10, receipts); err != nil {
		t.Errorf("receipts mismatch: %v", err)
	}
}

// Tests that post eth protocol handshake, clients perform a mutual checkpoint
// challenge to validate each other's chains. Hash mismatches, or missing ones
// during a fast sync should lead to the peer getting dropped.
func TestCheckpointChallenge(t *testing.T) {
	tests := []struct {
		syncmode   downloader.SyncMode
		checkpoint bool
		timeout    bool
		empty      bool
		match      bool
		drop       bool
	}{
		// If checkpointing is not enabled locally, don't challenge and don't drop
		{downloader.FullSync, false, false, false, false, false},
		{downloader.FastSync, false, false, false, false, false},

		// If checkpointing is enabled locally and remote response is empty, only drop during fast sync
		{downloader.FullSync, true, false, true, false, false},
		{downloader.FastSync, true, false, true, false, true}, // Special case, fast sync, unsynced peer

		// If checkpointing is enabled locally and remote response mismatches, always drop
		{downloader.FullSync, true, false, false, false, true},
		{downloader.FastSync, true, false, false, false, true},

		// If checkpointing is enabled locally and remote response matches, never drop
		{downloader.FullSync, true, false, false, true, false},
		{downloader.FastSync, true, false, false, true, false},

		// If checkpointing is enabled locally and remote times out, always drop
		{downloader.FullSync, true, true, false, true, true},
		{downloader.FastSync, true, true, false, true, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("sync %v checkpoint %v timeout %v empty %v match %v", tt.syncmode, tt.checkpoint, tt.timeout, tt.empty, tt.match), func(t *testing.T) {
			testCheckpointChallenge(t, tt.syncmode, tt.checkpoint, tt.timeout, tt.empty, tt.match, tt.drop)
		})
	}
}

func testCheckpointChallenge(t *testing.T, syncmode downloader.SyncMode, checkpoint bool, timeout bool, empty bool, match bool, drop bool) {
	// Reduce the checkpoint handshake challenge timeout
	defer func(old time.Duration) { syncChallengeTimeout = old }(syncChallengeTimeout)
	syncChallengeTimeout = 250 * time.Millisecond

	// Initialize a chain and generate a fake CHT if checkpointing is enabled
	var (
		db     = rawdb.NewMemoryDatabase()
		config = new(params.ChainConfig)
	)
	(&core.Genesis{Config: config}).MustCommit(db) // Commit genesis block
	// If checkpointing is enabled, create and inject a fake CHT and the corresponding
	// chllenge response.
	var response *types.Header
	var cht *params.TrustedCheckpoint
	if checkpoint {
		index := uint64(rand.Intn(500))
		number := (index+1)*params.CHTFrequency - 1
		response = &types.Header{Number: big.NewInt(int64(number)), Extra: []byte("valid")}

		cht = &params.TrustedCheckpoint{
			SectionIndex: index,
			SectionHead:  response.Hash(),
		}
	}
	// Create a checkpoint aware protocol manager
	blockchain, err := core.NewBlockChain(db, nil, config, ethash.NewFaker(), vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create new blockchain: %v", err)
	}
	pm, err := NewProtocolManager(config, cht, syncmode, DefaultConfig.NetworkId, new(event.TypeMux), &testTxPool{pool: make(map[common.Hash]*types.Transaction)}, ethash.NewFaker(), blockchain, db, 1, nil, "")
	if err != nil {
		t.Fatalf("failed to start test protocol manager: %v", err)
	}
	pm.Start(1000)
	defer pm.Stop()

	// Connect a new peer and check that we receive the checkpoint challenge
	peer, _ := newTestPeer("peer", eth63, pm, true)
	defer peer.close()

	if checkpoint {
		challenge := &getBlockHeadersData{
			Origin:  hashOrNumber{Number: response.Number.Uint64()},
			Amount:  1,
			Skip:    0,
			Reverse: false,
		}
		if err := p2p.ExpectMsg(peer.app, GetBlockHeadersMsg, challenge); err != nil {
			t.Fatalf("challenge mismatch: %v", err)
		}
		// Create a block to reply to the challenge if no timeout is simulated
		if !timeout {
			if empty {
				if err := p2p.Send(peer.app, BlockHeadersMsg, []*types.Header{}); err != nil {
					t.Fatalf("failed to answer challenge: %v", err)
				}
			} else if match {
				if err := p2p.Send(peer.app, BlockHeadersMsg, []*types.Header{response}); err != nil {
					t.Fatalf("failed to answer challenge: %v", err)
				}
			} else {
				if err := p2p.Send(peer.app, BlockHeadersMsg, []*types.Header{{Number: response.Number}}); err != nil {
					t.Fatalf("failed to answer challenge: %v", err)
				}
			}
		}
	}
	// Wait until the test timeout passes to ensure proper cleanup
	time.Sleep(syncChallengeTimeout + 100*time.Millisecond)

	// Verify that the remote peer is maintained or dropped
	if drop {
		if peers := pm.peers.Len(); peers != 0 {
			t.Fatalf("peer count mismatch: have %d, want %d", peers, 0)
		}
	} else {
		if peers := pm.peers.Len(); peers != 1 {
			t.Fatalf("peer count mismatch: have %d, want %d", peers, 1)
		}
	}
}

func TestBroadcastBlock(t *testing.T) {
	var tests = []struct {
		totalPeers        int
		broadcastExpected int
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{5, 2},
		{9, 3},
		{12, 3},
		{16, 4},
		{26, 5},
		{100, 10},
	}
	for _, test := range tests {
		testBroadcastBlock(t, test.totalPeers, test.broadcastExpected)
	}
}

func testBroadcastBlock(t *testing.T, totalPeers, broadcastExpected int) {
	var (
		evmux   = new(event.TypeMux)
		pow     = ethash.NewFaker()
		db      = rawdb.NewMemoryDatabase()
		config  = &params.ChainConfig{}
		gspec   = &core.Genesis{Config: config}
		genesis = gspec.MustCommit(db)
	)
	blockchain, err := core.NewBlockChain(db, nil, config, pow, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create new blockchain: %v", err)
	}
	pm, err := NewProtocolManager(config, nil, downloader.FullSync, DefaultConfig.NetworkId, evmux, &testTxPool{pool: make(map[common.Hash]*types.Transaction)}, pow, blockchain, db, 1, nil, "")
	if err != nil {
		t.Fatalf("failed to start test protocol manager: %v", err)
	}
	pm.Start(1000)
	defer pm.Stop()
	var peers []*testPeer
	for i := 0; i < totalPeers; i++ {
		peer, _ := newTestPeer(fmt.Sprintf("peer %d", i), eth63, pm, true)
		defer peer.close()

		peers = append(peers, peer)
	}
	chain, _ := core.GenerateChain(gspec.Config, genesis, ethash.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {})
	pm.BroadcastBlock(chain[0], true /*propagate*/)

	errCh := make(chan error, totalPeers)
	doneCh := make(chan struct{}, totalPeers)
	for _, peer := range peers {
		go func(p *testPeer) {
			if err := p2p.ExpectMsg(p.app, NewBlockMsg, &newBlockData{Block: chain[0], TD: big.NewInt(131136)}); err != nil {
				errCh <- err
			} else {
				doneCh <- struct{}{}
			}
		}(peer)
	}
	var received int
	for {
		select {
		case <-doneCh:
			received++

		case <-time.After(100 * time.Millisecond):
			if received != broadcastExpected {
				t.Errorf("broadcast count mismatch: have %d, want %d", received, broadcastExpected)
			}
			return

		case err = <-errCh:
			t.Fatalf("broadcast failed: %v", err)
		}
	}

}

// Tests that a propagated malformed block (uncles or transactions don't match
// with the hashes in the header) gets discarded and not broadcast forward.
func TestBroadcastMalformedBlock(t *testing.T) {
	// Create a live node to test propagation with
	var (
		engine  = ethash.NewFaker()
		db      = rawdb.NewMemoryDatabase()
		config  = &params.ChainConfig{}
		gspec   = &core.Genesis{Config: config}
		genesis = gspec.MustCommit(db)
	)
	blockchain, err := core.NewBlockChain(db, nil, config, engine, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create new blockchain: %v", err)
	}
	pm, err := NewProtocolManager(config, nil, downloader.FullSync, DefaultConfig.NetworkId, new(event.TypeMux), new(testTxPool), engine, blockchain, db, 1, nil, "")
	if err != nil {
		t.Fatalf("failed to start test protocol manager: %v", err)
	}
	pm.Start(2)
	defer pm.Stop()

	// Create two peers, one to send the malformed block with and one to check
	// propagation
	source, _ := newTestPeer("source", eth63, pm, true)
	defer source.close()

	sink, _ := newTestPeer("sink", eth63, pm, true)
	defer sink.close()

	// Create various combinations of malformed blocks
	chain, _ := core.GenerateChain(gspec.Config, genesis, ethash.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {})

	malformedUncles := chain[0].Header()
	malformedUncles.UncleHash[0]++
	malformedTransactions := chain[0].Header()
	malformedTransactions.TxHash[0]++
	malformedEverything := chain[0].Header()
	malformedEverything.UncleHash[0]++
	malformedEverything.TxHash[0]++

	// Keep listening to broadcasts and notify if any arrives
	notify := make(chan struct{}, 1)
	go func() {
		if _, err := sink.app.ReadMsg(); err == nil {
			notify <- struct{}{}
		}
	}()
	// Try to broadcast all malformations and ensure they all get discarded
	for _, header := range []*types.Header{malformedUncles, malformedTransactions, malformedEverything} {
		block := types.NewBlockWithHeader(header).WithBody(chain[0].Transactions(), chain[0].Uncles())
		if err := p2p.Send(source.app, NewBlockMsg, []interface{}{block, big.NewInt(131136)}); err != nil {
			t.Fatalf("failed to broadcast block: %v", err)
		}
		select {
		case <-notify:
			t.Fatalf("malformed block forwarded")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
