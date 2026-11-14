package eth

import (
	"encoding/hex"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/teehost"
)

var communicationStatsEnabled = os.Getenv("TGOVERN_COMM_STATS") == "1"

// sendProtocolMessage counts the same RLP payload that p2p.Send writes, before
// Snappy/RLPx. A successful write is not an acknowledgement by the remote app.
func (p *peer) sendProtocolMessage(code uint64, data interface{}) error {
	if !communicationStatsEnabled {
		return p2p.Send(p.rw, code, data)
	}
	size, payload, err := rlp.EncodeToReader(data)
	if err != nil {
		p.logProtocolCommunication(code, data, 0, "encode_failed")
		return err
	}
	err = p.rw.WriteMsg(p2p.Msg{Code: code, Size: uint32(size), Payload: payload})
	status := "ok"
	if err != nil {
		status = "write_failed"
	}
	p.logProtocolCommunication(code, data, uint64(size), status)
	return err
}

func communicationMessageClass(code uint64) (string, string) {
	switch code {
	case StatusMsg:
		return "handshake", "status"
	case TransactionMsg:
		return "transaction_propagation", "transactions"
	case NewPooledTransactionHashesMsg:
		return "transaction_propagation", "pooled_transaction_hashes"
	case GetPooledTransactionsMsg:
		return "transaction_propagation", "get_pooled_transactions"
	case PooledTransactionsMsg:
		return "transaction_propagation", "pooled_transactions"
	case NewBlockHashesMsg:
		return "block_propagation", "new_block_hashes"
	case NewBlockMsg:
		return "block_propagation", "new_block"
	case GetBlockHeadersMsg:
		return "chain_sync", "get_block_headers"
	case BlockHeadersMsg:
		return "chain_sync", "block_headers"
	case GetBlockBodiesMsg:
		return "chain_sync", "get_block_bodies"
	case BlockBodiesMsg:
		return "chain_sync", "block_bodies"
	case GetNodeDataMsg:
		return "chain_sync", "get_node_data"
	case NodeDataMsg:
		return "chain_sync", "node_data"
	case GetReceiptsMsg:
		return "chain_sync", "get_receipts"
	case ReceiptsMsg:
		return "chain_sync", "receipts"
	case TEEDistKGMsg:
		return "system_init", "tee_distkg"
	case TEEDKRSMsg:
		return "rotation_flow", "tee_dkrs"
	case TEEIdentityMsg:
		return "tee_identity", "tee_identity"
	case TEEPartialAdaptMsg:
		return "edit_flow", "tee_partial_adapt"
	case TEERedactMaterialMsg:
		return "edit_flow", "redact_material"
	case TEERedactAuthorizedMsg:
		return "edit_flow", "redact_authorized"
	case ModifyVerifyMsg:
		return "edit_flow", "modify_proposal"
	case TxModifyMsg:
		return "ledger_update_e2e", "result_delivery_only"
	case TxModifyRequestMsg:
		return "ledger_update_e2e", "result_request"
	case PublicKeyMsg:
		return "unknown", "legacy_public_key"
	case PrivateKeyMsg:
		return "unknown", "legacy_private_key"
	default:
		return "unknown", "unknown"
	}
}

func communicationID(id [32]byte) string { return hex.EncodeToString(id[:]) }

func communicationEditContext(block *big.Int, index uint64) []interface{} {
	if block == nil {
		return nil
	}
	return []interface{}{"block", block.String(), "tx_index", index,
		"join_key", block.String() + "-" + strconv.FormatUint(index, 10)}
}

func (p *peer) logProtocolCommunication(code uint64, data interface{}, attempted uint64, status string) {
	stage, component := communicationMessageClass(code)
	var sent uint64
	if status == "ok" {
		sent = attempted
	}
	ctx := []interface{}{"marker_schema", 2, "event", "send", "transport", "geth_eth",
		"observation", "sender_write", "stage", stage, "component", component,
		"byte_basis", "rlp_payload_pre_snappy", "msg_code", code, "msg_type", component,
		"peer", p.id, "payload_bytes", sent, "attempted_payload_bytes", attempted,
		"status", status, "node", os.Getenv("TGOVERN_COMM_NODE")}
	switch msg := data.(type) {
	case types.ProposalTx:
		ctx = append(ctx, communicationEditContext(msg.Msg.BlockNum, msg.Msg.Index)...)
	case types.ModifyMsg:
		ctx = append(ctx, communicationEditContext(msg.ProposalTx.Msg.BlockNum, msg.ProposalTx.Msg.Index)...)
		ctx = append(ctx, "request_id", communicationID(msg.ReqID))
	case types.TxModifyRequest:
		ctx = append(ctx, communicationEditContext(msg.BlockNum, msg.Index)...)
		ctx = append(ctx, "request_id", communicationID(msg.ReqID))
	case teehost.DistKGProtocolMessage:
		kind := "empty"
		switch {
		case msg.Deal != nil:
			kind = "deal"
		case msg.Share != nil:
			kind = "share"
		case msg.Complaint != nil:
			kind = "complaint"
		case msg.VerifyKey != nil:
			kind = "verify_key"
		}
		ctx = append(ctx, "protocol_kind", kind, "session", "tee-distkg")
	case teehost.DKRSProtocolMessage:
		kind := "empty"
		switch {
		case msg.Deal != nil:
			kind = "deal"
		case msg.Share != nil:
			kind = "share"
		case msg.Complaint != nil:
			kind = "complaint"
		case msg.VerifyKey != nil:
			kind = "verify_key"
		case msg.InputRequest != nil:
			kind = "input_request"
		case msg.InputResponse != nil:
			kind = "input_response"
		}
		ctx = append(ctx, "session", communicationID(msg.SessionID), "protocol_kind", kind)
	case teehost.PartialAdaptProtocolMessage:
		if msg.Contribution != nil {
			ctx = append(ctx, "request_id", communicationID(msg.Contribution.ReqID), "protocol_kind", "contribution")
		}
		if msg.Aggregate != nil {
			ctx = append(ctx, "request_id", communicationID(msg.Aggregate.ReqID), "protocol_kind", "aggregate")
		}
	case teehost.RedactAuthorizedMessage:
		ctx = append(ctx, "request_id", communicationID(msg.ReqID), "epoch", msg.Epoch)
	case teehost.RedactionMaterial:
		ctx = append(ctx, communicationEditContext(msg.BlockNum, msg.Index)...)
		ctx = append(ctx, "request_id", communicationID(msg.ReqID))
		if msg.Epoch != nil {
			ctx = append(ctx, "epoch", msg.Epoch.String())
		}
	case []interface{}:
		if code == NewBlockMsg && len(msg) > 0 {
			if b, ok := msg[0].(*types.Block); ok && b != nil {
				ctx = append(ctx, "block", b.NumberU64())
			}
		}
	}
	log.Info("E2E_COMM", ctx...)
}
