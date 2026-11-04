package types

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// RedactTx 编辑提案（论文：Redaction Instruction RI）。随机数属于被编辑的
// 交易，不属于区块头或提案外层。
type RedactTx struct {
	//ProposerID    enode.ID
	BlockNum *big.Int // 需要修改的交易所在的区块号
	//ProposerSig   []byte   //修改的签名
	CreateTime  time.Time //修改时间戳
	ProposalID  string    //提案者生成的全网一致份额收集ID
	CollectorID string    //提案者 p2p 短节点 ID，用于份额定向回传
	OldMessage  chameleon.Message
	NewMessage  chameleon.Message
	HashOutput  *chameleon.HashOutput
	Relay       RelayMetadata

	Msg ModifiedMessage
}

// ModifiedMessage 编辑指令内容（论文：RI = (txid, i, x*) 的扩展）
// 当前协议只允许 ModifyType=1 的 payload 编辑。
type ModifiedMessage struct {
	BlockNum     string `json:"BlockNum"`
	RedactReason string `json:"RedactReason"`
	Index        uint64 `json:"Index"`
	ModifyType   uint64 `json:"ModifyType"` // 1 修改交易 payload
	DataBefore   string `json:"DataBefore"`
	DataAfter    string `json:"DataAfter"`
}

// RedactLog 记录每次修改的详细信息
type RedactLog struct {
	Timestamp   uint64   //修改时间戳
	ProposerID  enode.ID //提议者ID
	OldValue    *big.Int //修改前的值
	NewValue    *big.Int //修改后的值
	Reason      string   //修改原因
	BlockNumber *big.Int //修改时的区块号
}

// ModifyMsg 编辑消息（论文 Protocol 2: 广播的编辑结果）。r' 已编码在
// NewBlock 的目标交易中。
type ModifyMsg struct {
	RedactTx RedactTx
	NewBlock *Block
	Relay    RelayMetadata
}

// KeyShareMessage 密钥份额消息
// 用于节点之间交换适应份额
type KeyShareMessage struct {
	ProposalID  string
	CollectorID string
	Share       *chameleon.AdaptationShare
	OldMessage  chameleon.Message
	NewMessage  chameleon.Message
	HashOutput  *chameleon.HashOutput
	Relay       RelayMetadata
}

// RelayMetadata keeps local bounded-gossip bookkeeping. Only TTL is RLP-encoded;
// message fingerprints and peer identities are recomputed or resolved locally.
type RelayMetadata struct {
	MessageID string
	Origin    string
	TTL       uint64
	Hop       uint64
}

// DistKGMessage 分布式密钥生成协议消息（NITCH Protocol 1）
// 用于委员会成员间交换 Deal、Share、Complaint、Response、VerifyKey
type DistKGMessage struct {
	Deal          *chameleon.DistKGDeal      `rlp:"nil"` // 步骤1 广播的承诺，仅 Deal 非空时有效
	Share         *chameleon.DistKGShare     `rlp:"nil"` // 步骤1 秘密份额，仅 Share 非空时有效
	Complaint     *chameleon.DistKGComplaint `rlp:"nil"` // 步骤2/3 投诉，仅 Complaint 非空时有效
	Response      *chameleon.DistKGResponse  `rlp:"nil"` // 步骤3 被投诉方回应，仅 Response 非空时有效
	VerifyKey     *chameleon.DistKGVerifyKey `rlp:"nil"` // 步骤4完成后广播验证密钥vk_j
	InputRequest  *DistKGInputRequest        `rlp:"nil"`
	InputResponse *DistKGInputResponse       `rlp:"nil"`
	Relay         RelayMetadata
}

// DistKGInputRequest asks an original dealer to replay missing inputs. Deal is
// public and is useful to every observer; Share is only applied by RequesterIndex.
type DistKGInputRequest struct {
	DealerIndex    *big.Int
	RequesterIndex *big.Int
	NeedDeal       bool
	NeedShare      bool
	Nonce          uint64
	Attempt        uint64
}

// DistKGInputResponse wraps the dealer's original, independently verifiable
// input. Receivers feed Deal/Share through the normal DistKG handlers.
type DistKGInputResponse struct {
	DealerIndex    *big.Int
	RequesterIndex *big.Int
	Deal           *chameleon.DistKGDeal  `rlp:"nil"`
	Share          *chameleon.DistKGShare `rlp:"nil"`
	Nonce          uint64
}

// ReshareMessage 是 DKRS 重分享协议消息。
// SessionID 用于区分不同重分享会话，防止旧消息污染当前流程。
type ReshareMessage struct {
	SessionID     string                   // 会话标识
	Deal          *chameleon.DKRSDeal      `rlp:"nil"`
	Share         *chameleon.DKRSShare     `rlp:"nil"`
	Complaint     *chameleon.DKRSComplaint `rlp:"nil"`
	Response      *chameleon.DKRSResponse  `rlp:"nil"`
	VerifyKey     *chameleon.DKRSVerifyKey `rlp:"nil"`
	InputRequest  *ReshareInputRequest     `rlp:"nil"`
	InputResponse *ReshareInputResponse    `rlp:"nil"`
	RelayTTL      uint64                   // Deprecated wire compatibility mirror of Relay.TTL.
	Relay         RelayMetadata
}

// ReshareInputRequest asks a dealer to replay the cached DKRS inputs for a
// receiver in the same reshare session.
type ReshareInputRequest struct {
	DealerIndex   *big.Int
	ReceiverIndex *big.Int
	NeedDeal      bool
	NeedShare     bool
	Nonce         [32]byte
	Attempt       uint64
}

// ReshareInputResponse carries a dealer's cached DKRS deal/share back through
// the normal ReshareMsg path.
type ReshareInputResponse struct {
	DealerIndex   *big.Int
	ReceiverIndex *big.Int
	Nonce         [32]byte
	HasDeal       bool
	Deal          *chameleon.DKRSDeal `rlp:"nil"`
	HasShare      bool
	Share         *chameleon.DKRSShare `rlp:"nil"`
}
