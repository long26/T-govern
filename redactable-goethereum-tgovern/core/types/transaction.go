// Copyright 2014 The go-ethereum Authors
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

package types

import (
	"container/heap"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/chameleon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
	"math/big"
	"sync/atomic"
)

//go:generate gencodec -type txdata -field-override txdataMarshaling -out gen_tx_json.go

var (
	ErrInvalidSig = errors.New("invalid transaction v, r, s values")
	// hashCacheInvalid 表示“缓存已失效、需重新计算”。atomic.Value 禁止存 nil，故用此哨兵代替“清除缓存”。
	hashCacheInvalid = new(common.Hash)
)

type Transaction struct {
	data txdata
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type txdata struct {
	AccountNonce uint64          `json:"nonce"    gencodec:"required"`
	Price        *big.Int        `json:"gasPrice" gencodec:"required"`
	GasLimit     uint64          `json:"gas"      gencodec:"required"`
	Recipient    *common.Address `json:"to"       rlp:"nil"`           // nil means contract creation
	Amount       *big.Int        `json:"value"    gencodec:"required"` //edit
	Payload      []byte          `json:"input"    gencodec:"required"` //edit

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

	// ChameleonR is the only redactable extension in the wire/storage identity.
	ChameleonR *big.Int `json:"chameleonR" gencodec:"optional" rlp:"nil"`

	// Legacy local accessors remain available, but these fields are never transmitted.
	SubPublicKey *big.Int    `json:"-" rlp:"-"`
	PiHash       common.Hash `json:"-" rlp:"-"`
	PlHash       common.Hash `json:"-" rlp:"-"`

	// This is only used when marshaling to JSON.
	Hash *common.Hash `json:"hash" rlp:"-"`
}

type txdataWithoutChameleonR struct {
	AccountNonce uint64
	Price        *big.Int
	GasLimit     uint64
	Recipient    *common.Address `rlp:"nil"`
	Amount       *big.Int
	Payload      []byte
	V            *big.Int
	R            *big.Int
	S            *big.Int
}

func (tx *Transaction) withoutChameleonR() txdataWithoutChameleonR {
	return txdataWithoutChameleonR{
		tx.data.AccountNonce, tx.data.Price, tx.data.GasLimit, tx.data.Recipient,
		tx.data.Amount, tx.data.Payload, tx.data.V, tx.data.R, tx.data.S,
	}
}

type txdataMarshaling struct {
	AccountNonce hexutil.Uint64
	Price        *hexutil.Big
	GasLimit     hexutil.Uint64
	Amount       *hexutil.Big
	Payload      hexutil.Bytes
	V            *hexutil.Big
	R            *hexutil.Big
	S            *hexutil.Big
	SubPublicKey *hexutil.Big
	ChameleonR   *hexutil.Big
	PiHash       common.Hash
	PlHash       common.Hash
}

func NewTransaction(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return newTransaction(nonce, &to, amount, gasLimit, gasPrice, data)
}
func NewContractCreation(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return newTransaction(nonce, nil, amount, gasLimit, gasPrice, data)
}
func newTransaction(nonce uint64, to *common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	if len(data) > 0 {
		data = common.CopyBytes(data)
	}
	d := txdata{
		AccountNonce: nonce,
		Recipient:    to,
		Payload:      data,
		Amount:       new(big.Int),
		GasLimit:     gasLimit,
		Price:        new(big.Int),
		V:            new(big.Int),
		R:            new(big.Int),
		S:            new(big.Int),
		SubPublicKey: nil,
		ChameleonR:   nil,
		PiHash:       common.Hash{},
		PlHash:       common.Hash{},
	}
	if amount != nil {
		d.Amount.Set(amount)
	}
	if gasPrice != nil {
		d.Price.Set(gasPrice)
	}

	return &Transaction{data: d}
}

// ChainId returns which chain id this transaction was signed for (if at all)
func (tx *Transaction) ChainId() *big.Int {
	return deriveChainId(tx.data.V)
}

// Protected returns whether the transaction is protected from replay protection.
func (tx *Transaction) Protected() bool {
	return isProtectedV(tx.data.V)
}

func isProtectedV(V *big.Int) bool {
	if V.BitLen() <= 8 {
		v := V.Uint64()
		return v != 27 && v != 28
	}
	// anything not 27 or 28 is considered protected
	return true
}

// EncodeRLP implements rlp.Encoder
func (tx *Transaction) EncodeRLP(w io.Writer) error {
	if tx.data.ChameleonR == nil {
		return rlp.Encode(w, tx.withoutChameleonR())
	}
	return rlp.Encode(w, &tx.data)
}

// DecodeRLP implements rlp.Decoder for the shared nine/ten-field transaction format.
func (tx *Transaction) DecodeRLP(s *rlp.Stream) error {
	raw, err := s.Raw()
	if err != nil {
		return err
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(raw, &fields); err != nil {
		return err
	}
	var data txdata
	switch len(fields) {
	case 9:
		var legacy txdataWithoutChameleonR
		if err := rlp.DecodeBytes(raw, &legacy); err != nil {
			return err
		}
		data = txdata{AccountNonce: legacy.AccountNonce, Price: legacy.Price, GasLimit: legacy.GasLimit,
			Recipient: legacy.Recipient, Amount: legacy.Amount, Payload: legacy.Payload, V: legacy.V, R: legacy.R, S: legacy.S}
	case 10:
		if err := rlp.DecodeBytes(raw, &data); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid transaction RLP field count %d", len(fields))
	}
	tx.data = data
	tx.invalidateHash()
	tx.from = atomic.Value{}
	tx.size.Store(common.StorageSize(len(raw)))
	return nil
}

// MarshalJSON encodes the web3 RPC transaction format.
func (tx *Transaction) MarshalJSON() ([]byte, error) {
	hash, err := tx.HashWithError()
	if err != nil {
		return nil, err
	}
	data := tx.data
	data.Hash = &hash
	return data.MarshalJSON()
}

// UnmarshalJSON decodes the web3 RPC transaction format.
func (tx *Transaction) UnmarshalJSON(input []byte) error {
	var dec txdata
	if err := dec.UnmarshalJSON(input); err != nil {
		return err
	}

	withSignature := dec.V.Sign() != 0 || dec.R.Sign() != 0 || dec.S.Sign() != 0
	if withSignature {
		var V byte
		if isProtectedV(dec.V) {
			chainID := deriveChainId(dec.V).Uint64()
			V = byte(dec.V.Uint64() - 35 - 2*chainID)
		} else {
			V = byte(dec.V.Uint64() - 27)
		}
		if !crypto.ValidateSignatureValues(V, dec.R, dec.S, false) {
			return ErrInvalidSig
		}
	}

	*tx = Transaction{data: dec}
	return nil
}

func (tx *Transaction) Data() []byte       { return common.CopyBytes(tx.data.Payload) }
func (tx *Transaction) Gas() uint64        { return tx.data.GasLimit }
func (tx *Transaction) GasPrice() *big.Int { return new(big.Int).Set(tx.data.Price) }
func (tx *Transaction) Value() *big.Int    { return new(big.Int).Set(tx.data.Amount) }
func (tx *Transaction) Nonce() uint64      { return tx.data.AccountNonce }
func (tx *Transaction) From() atomic.Value { return tx.from }

func (tx *Transaction) CheckNonce() bool { return true }

// To returns the recipient address of the transaction.
// It returns nil if the transaction is a contract creation.
func (tx *Transaction) To() *common.Address {
	if tx.data.Recipient == nil {
		return nil
	}
	to := *tx.data.Recipient
	return &to
}
func (tx *Transaction) SetData(data []byte) {
	tx.invalidateHash()
	tx.data.Payload = data
}
func (tx *Transaction) SetAmount(amount *big.Int) {
	tx.invalidateHash()
	tx.data.Amount = amount
}

// PiHash 返回 PiHash 字段（参与 RLP 编码）。
func (tx *Transaction) PiHash() common.Hash { return tx.data.PiHash }

// SetPiHash 设置 PiHash 字段。
func (tx *Transaction) SetPiHash(h common.Hash) {
	tx.invalidateHash()
	tx.data.PiHash = h
}

// PlHash 返回 PlHash 字段（参与 RLP 编码）。
func (tx *Transaction) PlHash() common.Hash { return tx.data.PlHash }

// SetPlHash 设置 PlHash 字段。
func (tx *Transaction) SetPlHash(h common.Hash) {
	tx.invalidateHash()
	tx.data.PlHash = h
}

func (tx *Transaction) invalidateHash() {
	if tx.size.Load() != nil {
		tx.size.Store(common.StorageSize(0))
	}
	if tx.hash.Load() != nil {
		tx.hash.Store(hashCacheInvalid)
	}
}

// Hash uses the chain's canonical key for transactions carrying ChameleonR.
// Callers admitting transactions must use HashWithError to reject unavailable keys.
// ECDSA signatures continue to use Signer.Hash, never this redactable identity.
func (tx *Transaction) Hash() common.Hash {
	hash, err := tx.HashWithError()
	if err != nil {
		log.Error("Cannot hash redactable transaction", "nonce", tx.Nonce(), "err", err)
		return common.Hash{}
	}
	return hash
}

// HashWithError never caches a legacy or zero hash for an unbound redactable transaction.
func (tx *Transaction) HashWithError() (common.Hash, error) {
	if p := tx.hash.Load(); p != nil {
		if ptr := p.(*common.Hash); ptr != hashCacheInvalid {
			return *ptr, nil
		}
	}
	var hash common.Hash
	if tx.data.ChameleonR != nil && tx.data.ChameleonR.Sign() != 0 {
		key, ready := tx.chameleonPublicKey()
		if !ready {
			return common.Hash{}, ErrChameleonKeyNotReady
		}
		digest, err := TransactionHashMessage(tx)
		if err != nil {
			return common.Hash{}, err
		}
		value := chameleon.Hash(key, new(big.Int).SetBytes(digest), tx.data.ChameleonR)
		if value == nil {
			return common.Hash{}, errors.New("invalid chameleon transaction hash")
		}
		hash = common.BytesToHash(value.Bytes())
	} else {
		hash = rlpHash(tx)
	}
	cached := hash
	tx.hash.Store(&cached)
	return hash, nil
}

// EncodeTxToBytesNoChameleonR 返回交易的 RLP 编码，但不包含 ChameleonR 字段。
func EncodeTxToBytesNoChameleonR(tx *Transaction) ([]byte, error) {
	if tx == nil {
		return nil, errors.New("nil transaction")
	}
	return rlp.EncodeToBytes(tx.withoutChameleonR())
}

// txHashMessageDomain 是变色龙哈希消息的域分离前缀（对齐 nitch 的
// NITCH_TX_HASH_V1）。
var txHashMessageDomain = []byte("NITCH_TX_HASH_V1")

// TransactionHashMessage 返回变色龙哈希唯一使用的消息形态：
// Keccak256("NITCH_TX_HASH_V1" || RLP(tx without ChameleonR))。
// [2026-08-30 迁移 nitch] 恒为 32B：hash-to-G1 成本不随交易体大小增长，
// 且 tx.Hash()、TEE 锻造（PartialAdapt 的 Source/TargetMessage）与接收端
// 校验（validateRedactedBlock）三方统一使用本函数，消息定义单一来源。
func TransactionHashMessage(tx *Transaction) ([]byte, error) {
	if tx == nil {
		return nil, errors.New("nil transaction")
	}
	encoded, err := EncodeTxToBytesNoChameleonR(tx)
	if err != nil {
		return nil, err
	}
	return crypto.Keccak256(txHashMessageDomain, encoded), nil
}

// Size returns the true RLP encoded storage size of the transaction, either by
// encoding and returning it, or returning a previsouly cached value.
func (tx *Transaction) Size() common.StorageSize {
	if size := tx.size.Load(); size != nil && size.(common.StorageSize) != 0 {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, tx)
	tx.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

// AsMessage returns the transaction as a core.Message.
//
// AsMessage requires a signer to derive the sender.
//
// XXX Rename message to something less arbitrary?
func (tx *Transaction) AsMessage(s Signer) (Message, error) {
	msg := Message{
		nonce:      tx.data.AccountNonce,
		gasLimit:   tx.data.GasLimit,
		gasPrice:   new(big.Int).Set(tx.data.Price),
		to:         tx.data.Recipient,
		amount:     tx.data.Amount,
		data:       tx.data.Payload,
		checkNonce: true,
	}

	var err error
	msg.from, err = Sender(s, tx)
	return msg, err
}

// WithSignature returns a new transaction with the given signature.
// This signature needs to be in the [R || S || V] format where V is 0 or 1.
func (tx *Transaction) WithSignature(signer Signer, sig []byte) (*Transaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}
	cpy := &Transaction{data: tx.data}
	cpy.data.R, cpy.data.S, cpy.data.V = r, s, v
	return cpy, nil
}

// Cost returns amount + gasprice * gaslimit.
func (tx *Transaction) Cost() *big.Int {
	total := new(big.Int).Mul(tx.data.Price, new(big.Int).SetUint64(tx.data.GasLimit))
	total.Add(total, tx.data.Amount)
	return total
}

// RawSignatureValues returns the V, R, S signature values of the transaction.
// The return values should not be modified by the caller.
func (tx *Transaction) RawSignatureValues() (v, r, s *big.Int) {
	return tx.data.V, tx.data.R, tx.data.S
}

// SubPublicKey 返回用户的子公钥（变色龙哈希用），可为 nil。
func (tx *Transaction) SubPublicKey() *big.Int {
	key, _ := tx.chameleonPublicKey()
	return key
}

// ChameleonR 返回用户生成的 r（变色龙哈希用），可为 nil。
func (tx *Transaction) ChameleonR() *big.Int { return tx.data.ChameleonR }

// SetChameleonHashFields 设置变色龙哈希相关字段：子公钥与用户生成的 r。设置后 Hash() 将使用变色龙哈希。
// 仅当已有缓存时才写入失效标记（atomic.Value 禁止存 nil，用 hashCacheInvalid 表示失效）。
func (tx *Transaction) SetChameleonHashFields(subPubKey, chameleonR *big.Int) {
	tx.invalidateHash()
	if subPubKey != nil {
		tx.data.SubPublicKey = new(big.Int).Set(subPubKey)
	} else {
		tx.data.SubPublicKey = nil
	}
	if chameleonR != nil {
		tx.data.ChameleonR = new(big.Int).Set(chameleonR)
	} else {
		tx.data.ChameleonR = nil
	}
}

func (tx *Transaction) SetChameleonR(chameleonR *big.Int) {
	tx.invalidateHash()
	if chameleonR != nil {
		tx.data.ChameleonR = new(big.Int).Set(chameleonR)
	} else {
		tx.data.ChameleonR = nil
	}
}

// Transactions is a Transaction slice type for basic sorting.
type Transactions []*Transaction

// Len returns the length of s.
func (s Transactions) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s.
func (s Transactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements DerivableList and returns the i'th transaction's hash in RLP.
// 使用交易哈希（变色龙哈希）而非完整 RLP，这样在锻造新 r 并修改交易字段后，
// 只要 tx.Hash() 不变，默克尔树根（TxHash）即可保持不变。
func (s Transactions) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i].Hash())
	return enc
}

// TxDifference returns a new set which is the difference between a and b.
func TxDifference(a, b Transactions) Transactions {
	keep := make(Transactions, 0, len(a))

	remove := make(map[common.Hash]struct{})
	for _, tx := range b {
		remove[tx.Hash()] = struct{}{}
	}

	for _, tx := range a {
		if _, ok := remove[tx.Hash()]; !ok {
			keep = append(keep, tx)
		}
	}

	return keep
}

// TxByNonce implements the sort interface to allow sorting a list of transactions
// by their nonces. This is usually only useful for sorting transactions from a
// single account, otherwise a nonce comparison doesn't make much sense.
type TxByNonce Transactions

func (s TxByNonce) Len() int           { return len(s) }
func (s TxByNonce) Less(i, j int) bool { return s[i].data.AccountNonce < s[j].data.AccountNonce }
func (s TxByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// TxByPrice implements both the sort and the heap interface, making it useful
// for all at once sorting as well as individually adding and removing elements.
type TxByPrice Transactions

func (s TxByPrice) Len() int           { return len(s) }
func (s TxByPrice) Less(i, j int) bool { return s[i].data.Price.Cmp(s[j].data.Price) > 0 }
func (s TxByPrice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (s *TxByPrice) Push(x interface{}) {
	*s = append(*s, x.(*Transaction))
}

func (s *TxByPrice) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

// TransactionsByPriceAndNonce represents a set of transactions that can return
// transactions in a profit-maximizing sorted order, while supporting removing
// entire batches of transactions for non-executable accounts.
type TransactionsByPriceAndNonce struct {
	txs    map[common.Address]Transactions // Per account nonce-sorted list of transactions
	heads  TxByPrice                       // Next transaction for each unique account (price heap)
	signer Signer                          // Signer for the set of transactions
}

// NewTransactionsByPriceAndNonce creates a transaction set that can retrieve
// price sorted transactions in a nonce-honouring way.
//
// Note, the input map is reowned so the caller should not interact any more with
// if after providing it to the constructor.
func NewTransactionsByPriceAndNonce(signer Signer, txs map[common.Address]Transactions) *TransactionsByPriceAndNonce {
	// Initialize a price based heap with the head transactions
	heads := make(TxByPrice, 0, len(txs))
	for from, accTxs := range txs {
		heads = append(heads, accTxs[0])
		// Ensure the sender address is from the signer
		acc, _ := Sender(signer, accTxs[0])
		txs[acc] = accTxs[1:]
		if from != acc {
			delete(txs, from)
		}
	}
	heap.Init(&heads)

	// Assemble and return the transaction set
	return &TransactionsByPriceAndNonce{
		txs:    txs,
		heads:  heads,
		signer: signer,
	}
}

// Peek returns the next transaction by price.
func (t *TransactionsByPriceAndNonce) Peek() *Transaction {
	if len(t.heads) == 0 {
		return nil
	}
	return t.heads[0]
}

// Shift replaces the current best head with the next one from the same account.
func (t *TransactionsByPriceAndNonce) Shift() {
	acc, _ := Sender(t.signer, t.heads[0])
	if txs, ok := t.txs[acc]; ok && len(txs) > 0 {
		t.heads[0], t.txs[acc] = txs[0], txs[1:]
		heap.Fix(&t.heads, 0)
	} else {
		heap.Pop(&t.heads)
	}
}

// Pop removes the best transaction, *not* replacing it with the next one from
// the same account. This should be used when a transaction cannot be executed
// and hence all subsequent ones should be discarded from the same account.
func (t *TransactionsByPriceAndNonce) Pop() {
	heap.Pop(&t.heads)
}

// Message is a fully derived transaction and implements core.Message
//
// NOTE: In a future PR this will be removed.
type Message struct {
	to         *common.Address
	from       common.Address
	nonce      uint64
	amount     *big.Int
	gasLimit   uint64
	gasPrice   *big.Int
	data       []byte
	checkNonce bool
}

func NewMessage(from common.Address, to *common.Address, nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, checkNonce bool) Message {
	return Message{
		from:       from,
		to:         to,
		nonce:      nonce,
		amount:     amount,
		gasLimit:   gasLimit,
		gasPrice:   gasPrice,
		data:       data,
		checkNonce: checkNonce,
	}
}

func (m Message) From() common.Address { return m.from }
func (m Message) To() *common.Address  { return m.to }
func (m Message) GasPrice() *big.Int   { return m.gasPrice }
func (m Message) Value() *big.Int      { return m.amount }
func (m Message) Gas() uint64          { return m.gasLimit }
func (m Message) Nonce() uint64        { return m.nonce }
func (m Message) Data() []byte         { return m.data }
func (m Message) CheckNonce() bool     { return m.checkNonce }
