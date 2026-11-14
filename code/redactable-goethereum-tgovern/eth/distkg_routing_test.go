package eth

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/teehost"
)

type failingDistKGWriter struct{}

func (failingDistKGWriter) ReadMsg() (p2p.Msg, error) {
	return p2p.Msg{}, errors.New("read not supported")
}

func (failingDistKGWriter) WriteMsg(p2p.Msg) error {
	return errors.New("connection closed")
}

func TestSendDistKGShareDirectedAndFallback(t *testing.T) {
	for _, mode := range []string{"direct", "missing-route", "failed-direct", "self"} {
		t.Run(mode, func(t *testing.T) {
			key, own := [32]byte{2}, [32]byte{1}
			host := &fakeDKRSRelayTEEHost{hardwareID: own, distKGRecipients: map[int][32]byte{2: key}}
			targetRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
			otherRW := &recordingMsgRW{msgs: make(chan p2p.Msg, 2)}
			pm := &ProtocolManager{peers: newPeerSet(), teeHost: host, teePeers: make(map[[32]byte]string), distKGRelayTTL: 1}
			pm.peers.peers["target"] = &peer{id: "target", rw: targetRW}
			pm.peers.peers["other"] = &peer{id: "other", rw: otherRW}
			switch mode {
			case "direct":
				pm.registerTEEPeer("target", key)
			case "failed-direct":
				pm.registerTEEPeer("target", key)
				pm.peers.peers["target"].rw = failingDistKGWriter{}
			case "self":
				host.distKGRecipients[2] = own
			}
			msg := teehost.DistKGProtocolMessage{Share: &teehost.DistKGShareMessage{FromIndex: 1, ToIndex: 2, Payload: []byte{0, 1}}}
			if err := pm.SendDistKGShare(context.Background(), 2, msg); err != nil {
				t.Fatal(err)
			}
			wantTarget, wantOther := 0, 0
			switch mode {
			case "direct":
				wantTarget = 1
			case "missing-route":
				wantTarget, wantOther = 1, 1
			case "failed-direct":
				wantOther = 1
			case "self":
				if host.distKGCalls != 1 {
					t.Fatalf("local delivery count = %d", host.distKGCalls)
				}
			}
			if len(targetRW.msgs) != wantTarget || len(otherRW.msgs) != wantOther {
				t.Fatalf("recipients target=%d other=%d, want %d/%d", len(targetRW.msgs), len(otherRW.msgs), wantTarget, wantOther)
			}
			for _, rw := range []*recordingMsgRW{targetRW, otherRW} {
				for len(rw.msgs) > 0 {
					sent := <-rw.msgs
					var decoded teehost.DistKGProtocolMessage
					if sent.Code != TEEDistKGMsg {
						t.Fatal("wrong message code")
					}
					if err := sent.Decode(&decoded); err != nil || decoded.Share == nil || decoded.Share.ToIndex != 2 {
						t.Fatalf("invalid routed share: %v", err)
					}
				}
			}
			if err := pm.SendDistKGShare(context.Background(), 3, msg); err == nil {
				t.Fatal("accepted inconsistent target index")
			}
		})
	}
}
