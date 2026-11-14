package eth

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
)

func TestProposalSharesReady(t *testing.T) {
	keys := testReshareKeys(t)
	oldMsg := chameleon.Message([]byte("old message"))
	newMsg := chameleon.Message([]byte("new message"))
	shares := make([]*chameleon.AdaptationShare, len(keys))
	for i, key := range keys {
		var err error
		shares[i], err = chameleon.PartialAdapt(key, oldMsg, newMsg)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, subscribeFirst := range []bool{true, false} {
		name := "subscribe-after-threshold"
		if subscribeFirst {
			name = "subscribe-before-threshold"
		}
		t.Run(name, func(t *testing.T) {
			km := testKeyManagerForReshare(keys[0], 0)
			for _, key := range keys {
				km.RegisterPeerKey(peerKeyAddressForIndex(key.Index), key)
			}
			km.CreateProposal("proposal", oldMsg, newMsg, nil)
			var ready <-chan struct{}
			if subscribeFirst {
				var err error
				ready, err = km.ProposalSharesReady("proposal")
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, index := range []int{0, 0} {
				if err := km.AddShare("proposal", shares[index], oldMsg, newMsg, nil); err != nil {
					t.Fatal(err)
				}
			}
			if subscribeFirst {
				select {
				case <-ready:
					t.Fatal("duplicate shares must not meet the threshold")
				default:
				}
			}
			if err := km.AddShare("proposal", shares[1], oldMsg, newMsg, nil); err != nil {
				t.Fatal(err)
			}
			late, err := km.ProposalSharesReady("proposal")
			if err != nil {
				t.Fatal(err)
			}
			if subscribeFirst && ready != late {
				t.Fatal("waiters must share the same notification")
			}
			select {
			case <-late:
			default:
				t.Fatal("threshold notification must already be closed")
			}
			if err := km.AddShare("proposal", shares[2], oldMsg, newMsg, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProposalSharesReadyWakesWaiter(t *testing.T) {
	km := &KeyManager{threshold: 0, pendingProposals: make(map[string]*ProposalShares)}
	km.CreateProposal("proposal", nil, nil, nil)
	ready, err := km.ProposalSharesReady("proposal")
	if err != nil {
		t.Fatal(err)
	}
	woke := make(chan struct{})
	go func() {
		<-ready
		close(woke)
	}()
	if err := km.AddShare("proposal", &chameleon.AdaptationShare{}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-woke:
	case <-time.After(time.Second):
		t.Fatal("threshold did not wake the waiter")
	}
}

func TestProposalSharesReadyUnknownProposal(t *testing.T) {
	km := &KeyManager{}
	if ready, err := km.ProposalSharesReady("missing"); err == nil || ready != nil {
		t.Fatal("missing proposal must return an error, not an unwakeable channel")
	}
}
