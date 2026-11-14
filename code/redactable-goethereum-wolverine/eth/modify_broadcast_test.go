package eth

import (
	"io"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

type modifyBroadcastRecorder struct {
	mu    sync.Mutex
	codes []uint64
}

func (r *modifyBroadcastRecorder) ReadMsg() (p2p.Msg, error) {
	return p2p.Msg{}, io.EOF
}

func (r *modifyBroadcastRecorder) WriteMsg(msg p2p.Msg) error {
	if err := msg.Discard(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codes = append(r.codes, msg.Code)
	return nil
}

func TestModifyKeyShareBroadcastIncludesNonCollector(t *testing.T) {
	pp, err := chameleon.Setup()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chameleon.SimulatedDistKG(pp, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	share, err := chameleon.PartialAdapt(keys[0], chameleon.Message("old"), chameleon.Message("new"))
	if err != nil {
		t.Fatal(err)
	}
	collector, other := new(modifyBroadcastRecorder), new(modifyBroadcastRecorder)
	pm := &ProtocolManager{peers: newPeerSet(), modifyRelayTTL: 2}
	pm.peers.peers["collector"] = &peer{id: "collector", rw: collector}
	pm.peers.peers["other"] = &peer{id: "other", rw: other}
	msg := types.KeyShareMessage{ProposalID: "p", CollectorID: "collector", Share: share}
	pm.BroadcastKeyShare(msg)
	for name, recorder := range map[string]*modifyBroadcastRecorder{"collector": collector, "other": other} {
		if len(recorder.codes) != 1 || recorder.codes[0] != KeyShareMsg {
			t.Fatalf("peer %s did not receive broadcast share: %v", name, recorder.codes)
		}
	}
	// A relay received from the collector still reaches other peers, never its source.
	msg.Relay.TTL = 2
	if success, failed := pm.relayKeyShare(msg, pm.peers.peers["collector"]); success != 1 || failed != 0 {
		t.Fatalf("relay sent %d messages with %d failures", success, failed)
	}
	if len(collector.codes) != 1 || len(other.codes) != 2 {
		t.Fatalf("relay recipients differ: collector=%d other=%d", len(collector.codes), len(other.codes))
	}
}

func TestModifyMessageContainsOnlyNewBlock(t *testing.T) {
	block := types.NewBlockWithHeader(&types.Header{
		Number: big.NewInt(11), Difficulty: big.NewInt(1),
	})
	msg := types.ModifyMsg{RedactTx: testModifyProposal(types.RelayMetadata{}), NewBlock: block}
	encoded, err := rlp.EncodeToBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Fatalf("modify message has %d fields, want proposal/new block/relay", len(fields))
	}
	var decoded types.ModifyMsg
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.NewBlock.Hash() != block.Hash() {
		t.Fatal("new block changed during round trip")
	}
	// The removed old-block slot must not be accepted as the new wire format.
	legacy, err := rlp.EncodeToBytes([]interface{}{msg.RedactTx, block, block, msg.Relay})
	if err != nil {
		t.Fatal(err)
	}
	if err := rlp.DecodeBytes(legacy, &decoded); err == nil {
		t.Fatal("accepted legacy message with redundant old block")
	}
}

func TestModifyBroadcastSendsOncePerPeer(t *testing.T) {
	first, second := new(modifyBroadcastRecorder), new(modifyBroadcastRecorder)
	pm := &ProtocolManager{
		peers: newPeerSet(), quitSync: make(chan struct{}), modifyRelayTTL: 1,
	}
	defer close(pm.quitSync)
	// No peer broadcast goroutines are needed for the synchronous private messages.
	pm.peers.peers["first"] = &peer{id: "first", rw: first}
	pm.peers.peers["second"] = &peer{id: "second", rw: second}
	block := types.NewBlockWithHeader(&types.Header{
		Number: big.NewInt(11), Difficulty: big.NewInt(1),
	})
	pm.BroadcastModifyMsg(types.ModifyMsg{
		RedactTx: testModifyProposal(types.RelayMetadata{}), NewBlock: block,
	})
	finished := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("broadcast scheduled asynchronous retransmissions")
	}
	for name, recorder := range map[string]*modifyBroadcastRecorder{"first": first, "second": second} {
		recorder.mu.Lock()
		codes := append([]uint64(nil), recorder.codes...)
		recorder.mu.Unlock()
		if len(codes) != 1 || codes[0] != TxModifyMsg {
			t.Fatalf("peer %s: got codes %v, want one TxModifyMsg", name, codes)
		}
	}
}
