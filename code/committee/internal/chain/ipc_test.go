package chain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewIPCGatewayRequiresPath(t *testing.T) {
	_, err := NewIPCGateway(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty ipc path")
	}
}

func TestIPCGatewayIsMrSignerRegisteredPlaceholder(t *testing.T) {
	g := &IPCGateway{}
	registered, err := g.IsMrSignerRegistered(context.Background(), "mrsigner-1")
	if err != nil {
		t.Fatalf("is mrsigner registered failed: %v", err)
	}
	if registered {
		t.Fatalf("placeholder should return false")
	}
}

func TestIPCGatewayGetPendingTEEAndStatus(t *testing.T) {
	ipcPath := startIPCStubServer(t, func(req rpcRequest) rpcResponse {
		switch req.Method {
		case "eth_getPendingTEE":
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"Exists":true,"Deposit":"11","PendingNonce":"12","CreatedAt":"13","ChallengeHash":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
			}
		case "eth_getTEEStatusByOwner":
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"Status":1,"HardwareId":"0x1234"}`),
			}
		case "eth_getActiveSetInfo":
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"Epoch":"1","ActiveTEEs":["0x1111111111111111111111111111111111111111111111111111111111111111"],"ActiveSetHash":"0x2222222222222222222222222222222222222222222222222222222222222222","PkCHHash":"0x3333333333333333333333333333333333333333333333333333333333333333","CommitteePubKeyVersion":"4","ActiveThreshold":"2","ActiveLimit":"3"}`),
			}
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return rpcResponse{}
		}
	})

	g, err := NewIPCGateway(context.Background(), ipcPath)
	if err != nil {
		t.Fatalf("new gateway failed: %v", err)
	}

	pending, err := g.GetPendingTEE(context.Background(), "0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("get pending tee failed: %v", err)
	}
	if !pending.Exists || pending.Nonce.Cmp(big.NewInt(12)) != 0 {
		t.Fatalf("unexpected pending tee: %+v", pending)
	}
	if pending.ChallengeHash == "" {
		t.Fatalf("expected pending challenge hash: %+v", pending)
	}

	status, err := g.GetTEEStatusByOwner(context.Background(), "0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("get tee status failed: %v", err)
	}
	if status.Status != 1 || status.HardwareID != "0x1234" {
		t.Fatalf("unexpected tee status: %+v", status)
	}

	activeSet, err := g.GetActiveSetInfo(context.Background(), big.NewInt(1))
	if err != nil {
		t.Fatalf("get active set info failed: %v", err)
	}
	if activeSet.Epoch.Cmp(big.NewInt(1)) != 0 || len(activeSet.ActiveTEEs) != 1 || activeSet.ActiveThreshold.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("unexpected active set info: %+v", activeSet)
	}
}

func TestIPCGatewaySubmitTEERegistration(t *testing.T) {
	ipcPath := startIPCStubServer(t, func(req rpcRequest) rpcResponse {
		if req.Method != "eth_registerTEE" {
			t.Fatalf("unexpected method: %s", req.Method)
		}
		if len(req.Params) != 1 {
			t.Fatalf("unexpected params: %+v", req.Params)
		}
		payload, ok := req.Params[0].(string)
		if !ok {
			t.Fatalf("expected payload string, got %T", req.Params[0])
		}
		if !strings.Contains(payload, "\"Owner\":\"0x0000000000000000000000000000000000000001\"") {
			t.Fatalf("unexpected payload: %s", payload)
		}
		if !strings.Contains(payload, "\"VrfPubKey\":\"0x55\"") || !strings.Contains(payload, "\"ChallengeHash\":\"0x88\"") {
			t.Fatalf("missing evidence fields: %s", payload)
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`"0xabc123"`),
		}
	})

	g, err := NewIPCGateway(context.Background(), ipcPath)
	if err != nil {
		t.Fatalf("new gateway failed: %v", err)
	}

	txHash, err := g.SubmitTEERegistration(context.Background(), RegistrationInput{
		RequestID:          "r1",
		OwnerAddress:       "0x0000000000000000000000000000000000000001",
		PendingNonce:       big.NewInt(5),
		MREnclave:          "0x11",
		MRSigner:           "0x44",
		HardwareID:         "0x22",
		SigPubKey:          "0x33",
		VrfPubKey:          "0x55",
		EnclavePK:          "0x" + strings.Repeat("66", 64),
		QuoteHash:          []byte{9, 9, 9},
		RegisterCertHash:   "0x77",
		ChallengeHash:      "0x88",
		AggregateSignature: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("submit tee registration failed: %v", err)
	}
	if txHash != "0xabc123" {
		t.Fatalf("unexpected tx hash: %s", txHash)
	}
}

func TestIPCGatewaySubmitTEERevocation(t *testing.T) {
	ipcPath := startIPCStubServer(t, func(req rpcRequest) rpcResponse {
		if req.Method != "eth_revokeTEE" {
			t.Fatalf("unexpected method: %s", req.Method)
		}
		if len(req.Params) != 4 {
			t.Fatalf("unexpected params: %+v", req.Params)
		}
		if req.Params[0] != "0x1111111111111111111111111111111111111111111111111111111111111111" {
			t.Fatalf("unexpected hardware id param: %+v", req.Params)
		}
		if req.Params[2] != "2" {
			t.Fatalf("unexpected slash amount param: %+v", req.Params)
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`"0xrevoked"`),
		}
	})

	g, err := NewIPCGateway(context.Background(), ipcPath)
	if err != nil {
		t.Fatalf("new gateway failed: %v", err)
	}

	txHash, err := g.SubmitTEERevocation(context.Background(), TEERevocationInput{
		HardwareID:         []byte(strings.Repeat("\x11", 32)),
		ReasonHash:         []byte(strings.Repeat("\x22", 32)),
		SlashAmount:        big.NewInt(2),
		AggregateSignature: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("submit tee revocation failed: %v", err)
	}
	if txHash != "0xrevoked" {
		t.Fatalf("unexpected tx hash: %s", txHash)
	}
}

// TestIPCGatewayBroadcastRedactAuthorizationCarriesDReq pins the field that the
// TEE enclave needs to accept a P2P-triggered PartialAdapt: it recomputes d_req
// from the request input and rejects the call when the value it is handed
// disagrees. Without DReq on the broadcast payload every P2P wake-up fails and
// the TEE hosts only start once the authorization transaction is mined.
func TestIPCGatewayBroadcastRedactAuthorizationCarriesDReq(t *testing.T) {
	dReq := strings.Repeat("ab", 32)
	ipcPath := startIPCStubServer(t, func(req rpcRequest) rpcResponse {
		if req.Method != "eth_broadcastRedactAuthorization" {
			t.Fatalf("unexpected method: %s", req.Method)
		}
		payload, ok := req.Params[0].(string)
		if !ok {
			t.Fatalf("expected payload string, got %T", req.Params[0])
		}
		if !strings.Contains(payload, `"DReq":"0x`+dReq+`"`) {
			t.Fatalf("broadcast payload is missing DReq: %s", payload)
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"Status":"broadcasted"}`),
		}
	})

	g, err := NewIPCGateway(context.Background(), ipcPath)
	if err != nil {
		t.Fatalf("new gateway failed: %v", err)
	}

	dReqBytes, err := hex.DecodeString(dReq)
	if err != nil {
		t.Fatalf("decode d_req: %v", err)
	}
	status, err := g.BroadcastRedactAuthorization(context.Background(), RedactAuthorizationInput{
		RequestID:              "req-1",
		Req:                    RedactReqFields{Version: 1, Epoch: 1, ReqID: [32]byte{1}},
		DReq:                   dReqBytes,
		DPolicy:                make([]byte, 32),
		ExpiresAt:              big.NewInt(2000000000),
		CommitteePubKeyVersion: big.NewInt(1),
		AuthorizationDigest:    make([]byte, 32),
		AggregateSignature:     []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("broadcast redact authorization failed: %v", err)
	}
	if status != "broadcasted" {
		t.Fatalf("unexpected status: %s", status)
	}
}

// TestIPCGatewayBroadcastRedactAuthorizationRejectsShortDReq guards the payload
// contract: a truncated d_req cannot be matched against the enclave's
// recomputation, so it must be rejected before reaching the node.
func TestIPCGatewayBroadcastRedactAuthorizationRejectsShortDReq(t *testing.T) {
	ipcPath := startIPCStubServer(t, func(req rpcRequest) rpcResponse {
		t.Fatalf("stub server should not be reached, got method: %s", req.Method)
		return rpcResponse{}
	})
	g, err := NewIPCGateway(context.Background(), ipcPath)
	if err != nil {
		t.Fatalf("new gateway failed: %v", err)
	}
	_, err = g.BroadcastRedactAuthorization(context.Background(), RedactAuthorizationInput{
		RequestID:          "req-1",
		Req:                RedactReqFields{Version: 1, Epoch: 1},
		DReq:               []byte{1, 2, 3},
		DPolicy:            make([]byte, 32),
		ExpiresAt:          big.NewInt(2000000000),
		AggregateSignature: []byte{1, 2, 3},
	})
	if err == nil || !strings.Contains(err.Error(), "d_req must be 32 bytes") {
		t.Fatalf("expected d_req length rejection, got: %v", err)
	}
}

func startIPCStubServer(t *testing.T, handler func(rpcRequest) rpcResponse) string {
	t.Helper()

	ipcPath := filepath.Join(t.TempDir(), "geth.ipc")
	ln, err := net.Listen("unix", ipcPath)
	if err != nil {
		t.Fatalf("listen unix failed: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))

				var req rpcRequest
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				_ = json.NewEncoder(c).Encode(handler(req))
			}(conn)
		}
	}()

	return ipcPath
}
