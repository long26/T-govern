package types

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"time"
)

// 毕设
// 交易提案
type ProposalTx struct {
	ProposerID *big.Int       `json:"ProposerID"`
	Proposer   common.Address `json:"Proposer"`
	//ProposerSig   []byte   //修改的签名
	R          *big.Int   `json:"R"`
	Msg        ModifyInfo `json:"Msg"`
	CreateTime time.Time  //创建时间戳
}

type ModifyInfo struct {
	BlockNum     *big.Int `json:"BlockNum"`
	RedactReason string   `json:"RedactReason"`
	Index        uint64   `json:"Index"`
	ModifyType   uint64   `json:"ModifyType"` // 1 更新区块的payload 2 删除交易 3 修改交易的extra
	DataBefore   []byte   `json:"DataBefore"`
	DataAfter    []byte   `json:"DataAfter"`
}

type ModifyMsg struct {
	ProposalTx ProposalTx
	NewR       *big.Int
	NewBlock   *Block
	ReqID      [32]byte
	ProofHash  [32]byte
	PiReqA     []byte
	RelayTTL   uint64
}

// TxModifyRequest asks peers to replay a cached TxModifyMsg for a redaction
// that is executed on-chain but not yet applied locally.
type TxModifyRequest struct {
	ReqID    [32]byte
	BlockNum *big.Int
	Index    uint64
	Nonce    uint64
	RelayTTL uint64
}
