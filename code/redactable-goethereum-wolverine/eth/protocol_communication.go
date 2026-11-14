package eth

import (
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

var communicationStatsEnabled = os.Getenv("NITCH_COMM_STATS") == "1"

// sendProtocolMessage preserves p2p.Send's single encoding and write semantics.
// Bytes are the RLP payload before Snappy, not RLPx frames or interface traffic.
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
	case ModifyVerifyMsg:
		return "edit_flow", "modify_proposal"
	case TxModifyMsg:
		return "edit_flow", "modify_result"
	case KeyShareMsg:
		return "edit_flow", "key_share"
	case KeyRequestMsg:
		return "edit_flow", "key_request"
	case DistKGMsg:
		return "system_init", "distkg"
	case ReshareMsg:
		return "rotation_flow", "reshare"
	default:
		return "unknown", "unknown"
	}
}

func communicationEditContext(tx types.RedactTx) []interface{} {
	ctx := []interface{}{"tx_index", tx.Msg.Index}
	if tx.BlockNum != nil && tx.BlockNum.Sign() >= 0 {
		ctx = append(ctx, "block", tx.BlockNum.String(),
			"join_key", redactionJoinKey(tx.BlockNum, tx.Msg.Index))
	}
	if tx.ProposalID != "" {
		ctx = append(ctx, "proposal_id", tx.ProposalID)
	}
	return ctx
}

func (p *peer) logProtocolCommunication(code uint64, data interface{}, attempted uint64, status string) {
	stage, kind := communicationMessageClass(code)
	var sent uint64
	if status == "ok" {
		sent = attempted
	}
	peerID := p.id
	if peerID == "" {
		peerID = "unknown"
	}
	ctx := []interface{}{
		"marker_schema", 1, "stage", stage, "event", "send", "direction", "tx",
		"byte_basis", "rlp_payload_pre_snappy", "msg_code", code, "msg_type", kind,
		"peer", peerID, "payload_bytes", sent, "attempted_payload_bytes", attempted,
		"status", status,
	}
	switch msg := data.(type) {
	case types.RedactTx:
		ctx = append(ctx, communicationEditContext(msg)...)
		ctx = append(ctx, "message_id", msg.Relay.MessageID)
	case types.ModifyMsg:
		ctx = append(ctx, communicationEditContext(msg.RedactTx)...)
		ctx = append(ctx, "message_id", msg.Relay.MessageID)
	case types.KeyShareMessage:
		ctx = append(ctx, "proposal_id", msg.ProposalID, "message_id", msg.Relay.MessageID)
		// ProposalID starts with the same decimal block/index used by edit anchors.
		parts := strings.SplitN(msg.ProposalID, "-", 3)
		if len(parts) == 3 {
			block, blockErr := strconv.ParseUint(parts[0], 10, 64)
			index, indexErr := strconv.ParseUint(parts[1], 10, 64)
			if blockErr == nil && indexErr == nil {
				ctx = append(ctx, "block", block, "tx_index", index,
					"join_key", parts[0]+"-"+parts[1])
			}
		}
	case types.DistKGMessage:
		ctx = append(ctx, "join_key", "dkg-1", "message_id", msg.Relay.MessageID,
			"protocol_kind", distKGMessageKind(msg))
	case types.ReshareMessage:
		ctx = append(ctx, "session", msg.SessionID, "message_id", msg.Relay.MessageID,
			"protocol_kind", reshareMessageKind(msg))
	case []interface{}:
		if code == NewBlockMsg && len(msg) > 0 {
			if block, ok := msg[0].(*types.Block); ok && block != nil {
				ctx = append(ctx, "block", block.NumberU64())
			}
		}
	}
	log.Info("E2E_COMM", ctx...)
}
