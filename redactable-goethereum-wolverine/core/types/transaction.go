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
	"github.com/ethereum/go-ethereum/rlp"
	"io"
	"math/big"
	"sync/atomic"
)

//go:generate gencodec -type txdata -field-override txdataMarshaling -out gen_tx_json.go

var (
	ErrInvalidSig                = errors.New("invalid transaction v, r, s values")
	ErrNITCHKeyNotReady          = errors.New("NITCH system public key is not ready")
	ErrInvalidChameleonHashState = errors.New("invalid NITCH transaction hash context")
	// hashCacheInvalid marks a hash cache entry that must be recomputed.
	// atomic.Value cannot store nil after it has been initialized.
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

	// ChameleonR is the transaction-level NITCH randomizer. It is part of the
	// wire/RLP identity but deliberately excluded from Signer.Hash.
	ChameleonR *big.Int `json:"chameleonR" rlp:"nil"`

	// This is only used when marshaling to JSON.
	Hash *common.Hash `json:"hash" rlp:"-"`
}

// txdataWithoutChameleonR is the canonical message preimage. It contains the
// signed legacy fields, including payload and signature values, but never the
// NITCH randomizer or any cache/display field.
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

type txdataMarshaling struct {
	AccountNonce hexutil.Uint64
	Price        *hexutil.Big
	GasLimit     hexutil.Uint64
	Amount       *hexutil.Big
	Payload      hexutil.Bytes
	V            *hexutil.Big
	R            *hexutil.Big
	S            *hexutil.Big
	ChameleonR   *hexutil.Big
}

func NewTransaction(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return newTransaction(nonce, &to, amount, gasLimit, gasPrice, data)
}
func NewTransactionV2(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte,
	v *big.Int, r *big.Int, s *big.Int, from atomic.Value) *Transaction {
	return newTransactionV2(nonce, &to, amount, gasLimit, gasPrice, data, v, r, s, from)
}
func NewContractCreation(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return newTransaction(nonce, nil, amount, gasLimit, gasPrice, data)
}
func NewContractCreationV2(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte,
	v *big.Int, r *big.Int, s *big.Int, from atomic.Value) *Transaction {
	return newTransactionV2(nonce, nil, amount, gasLimit, gasPrice, data, v, r, s, from)
}
func newTransactionV2(nonce uint64, to *common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte,
	v *big.Int, r *big.Int, s *big.Int, from atomic.Value) *Transaction {
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
	}
	if amount != nil {
		d.Amount.Set(amount)
	}
	if gasPrice != nil {
		d.Price.Set(gasPrice)
	}
	if v != nil {
		d.V.Set(v)
	}
	if r != nil {
		d.R.Set(r)
	}
	if s != nil {
		d.S.Set(s)
	}
	return &Transaction{
		data: d,
		from: from,
	}
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

func (tx *Transaction) withoutChameleonR() txdataWithoutChameleonR {
	return txdataWithoutChameleonR{
		AccountNonce: tx.data.AccountNonce,
		Price:        tx.data.Price,
		GasLimit:     tx.data.GasLimit,
		Recipient:    tx.data.Recipient,
		Amount:       tx.data.Amount,
		Payload:      tx.data.Payload,
		V:            tx.data.V,
		R:            tx.data.R,
		S:            tx.data.S,
	}
}

// EncodeRLP implements rlp.Encoder. An uninitialized local transaction keeps
// the legacy nine-field encoding until the txpool supplies its deterministic
// randomizer; admitted NITCH transactions always encode the tenth field.
func (tx *Transaction) EncodeRLP(w io.Writer) error {
	if tx.data.ChameleonR == nil {
		return rlp.Encode(w, tx.withoutChameleonR())
	}
	return rlp.Encode(w, &tx.data)
}

// DecodeRLP implements rlp.Decoder
func (tx *Transaction) DecodeRLP(s *rlp.Stream) error {
	raw, err := s.Raw()
	if err != nil {
		return err
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(raw, &fields); err != nil {
		return err
	}
	switch len(fields) {
	case 9:
		var legacy txdataWithoutChameleonR
		if err := rlp.DecodeBytes(raw, &legacy); err != nil {
			return err
		}
		tx.data = txdata{
			AccountNonce: legacy.AccountNonce,
			Price:        legacy.Price,
			GasLimit:     legacy.GasLimit,
			Recipient:    legacy.Recipient,
			Amount:       legacy.Amount,
			Payload:      legacy.Payload,
			V:            legacy.V,
			R:            legacy.R,
			S:            legacy.S,
		}
	case 10:
		if err := rlp.DecodeBytes(raw, &tx.data); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid transaction RLP field count %d", len(fields))
	}
	tx.invalidateHash()
	tx.size.Store(common.StorageSize(len(raw)))
	return nil
}

// MarshalJSON encodes the web3 RPC transaction format.
func (tx *Transaction) MarshalJSON() ([]byte, error) {
	hash := tx.Hash()
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
	tx.data.Payload = common.CopyBytes(data)
}
func (tx *Transaction) SetAmount(amount *big.Int) {
	tx.invalidateHash()
	if amount == nil {
		tx.data.Amount = nil
		return
	}
	tx.data.Amount = new(big.Int).Set(amount)
}

// PayloadOnlyEquivalent reports whether two transactions differ only in their
// payload and transaction-level NITCH randomizer. It is used by redaction
// validators to reject changes to value, nonce, recipient, gas or signature.
func (tx *Transaction) PayloadOnlyEquivalent(other *Transaction) bool {
	if tx == nil || other == nil {
		return false
	}
	left := tx.withoutChameleonR()
	right := other.withoutChameleonR()
	left.Payload = nil
	right.Payload = nil
	leftEncoded, leftErr := rlp.EncodeToBytes(left)
	rightEncoded, rightErr := rlp.EncodeToBytes(right)
	return leftErr == nil && rightErr == nil && string(leftEncoded) == string(rightEncoded)
}

// CopyForRedaction returns a deep copy that keeps the source NITCH randomizer.
// The caller must replace it with the threshold-adapted r' after changing the
// payload; no synthetic original-hash extension exists.
func (tx *Transaction) CopyForRedaction() (*Transaction, error) {
	cpy := &Transaction{data: tx.data}
	cpy.data.Price = new(big.Int).Set(tx.data.Price)
	cpy.data.Amount = new(big.Int).Set(tx.data.Amount)
	cpy.data.V = new(big.Int).Set(tx.data.V)
	cpy.data.R = new(big.Int).Set(tx.data.R)
	cpy.data.S = new(big.Int).Set(tx.data.S)
	cpy.data.Payload = common.CopyBytes(tx.data.Payload)
	if tx.data.Recipient != nil {
		recipient := *tx.data.Recipient
		cpy.data.Recipient = &recipient
	}
	if tx.data.ChameleonR != nil {
		cpy.data.ChameleonR = new(big.Int).Set(tx.data.ChameleonR)
	}
	return cpy, nil
}

// ChameleonR returns a copy of the transaction-level randomizer.
func (tx *Transaction) ChameleonR() *big.Int {
	if tx.data.ChameleonR == nil {
		return nil
	}
	return new(big.Int).Set(tx.data.ChameleonR)
}

// SetChameleonR sets a copy of the transaction-level randomizer.
func (tx *Transaction) SetChameleonR(r *big.Int) {
	tx.invalidateHash()
	if r == nil {
		tx.data.ChameleonR = nil
		return
	}
	tx.data.ChameleonR = new(big.Int).Set(r)
}

func (tx *Transaction) invalidateHash() {
	if tx.hash.Load() != nil {
		tx.hash.Store(hashCacheInvalid)
	}
}

func (tx *Transaction) cachedHash() (common.Hash, bool) {
	if cached := tx.hash.Load(); cached != nil {
		if hash := cached.(*common.Hash); hash != hashCacheInvalid {
			return *hash, true
		}
	}
	return common.Hash{}, false
}

func (tx *Transaction) cacheHash(hash common.Hash) common.Hash {
	cached := new(common.Hash)
	*cached = hash
	tx.hash.Store(cached)
	return hash
}

// TransactionHashMessage returns the only message form used by NITCH hash,
// adaptation and receiver verification.
func TransactionHashMessage(tx *Transaction) (chameleon.Message, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", ErrInvalidChameleonHashState)
	}
	encoded, err := rlp.EncodeToBytes(tx.withoutChameleonR())
	if err != nil {
		return nil, fmt.Errorf("%w: encode transaction preimage: %v", ErrInvalidChameleonHashState, err)
	}
	return chameleon.Message(crypto.Keccak256([]byte("NITCH_TX_HASH_V1"), encoded)), nil
}

// EnsureChameleonR fills the deterministic initial randomizer for a locally
// submitted signed transaction. Remote transactions must already carry it.
func (tx *Transaction) EnsureChameleonR(chainID *big.Int) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrInvalidChameleonHashState)
	}
	if tx.data.ChameleonR != nil && tx.data.ChameleonR.Sign() != 0 {
		return tx.ValidateChameleonHashContext()
	}
	key, err := chameleon.GetSystemNITCHKey()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNITCHKeyNotReady, err)
	}
	message, err := TransactionHashMessage(tx)
	if err != nil {
		return err
	}
	r, err := chameleon.ComputeTransactionR(key, chainID, message)
	if err != nil {
		return fmt.Errorf("%w: initialize r: %v", ErrInvalidChameleonHashState, err)
	}
	tx.SetChameleonR(r)
	return tx.ValidateChameleonHashContext()
}

// ValidateChameleonHashContext verifies that a transaction can only be hashed
// with the shared NITCH public key and a valid, non-zero G1 randomizer.
func (tx *Transaction) ValidateChameleonHashContext() error {
	if tx == nil || tx.data.ChameleonR == nil || tx.data.ChameleonR.Sign() == 0 {
		return fmt.Errorf("%w: chameleonR is missing or zero", ErrInvalidChameleonHashState)
	}
	_, err := tx.HashWithError()
	return err
}

// HashWithError computes the NITCH transaction identity. It never falls back
// to an RLP hash for a transaction that carries ChameleonR.
func (tx *Transaction) HashWithError() (common.Hash, error) {
	if tx.data.ChameleonR == nil {
		return common.Hash{}, fmt.Errorf("%w: chameleonR is missing", ErrInvalidChameleonHashState)
	}
	if hash, ok := tx.cachedHash(); ok {
		return hash, nil
	}
	key, err := chameleon.GetSystemNITCHKey()
	if err != nil {
		return common.Hash{}, fmt.Errorf("%w: %v", ErrNITCHKeyNotReady, err)
	}
	message, err := TransactionHashMessage(tx)
	if err != nil {
		return common.Hash{}, err
	}
	output, err := chameleon.Hash(key, message, tx.data.ChameleonR)
	if err != nil || output == nil || output.Hash == nil {
		return common.Hash{}, fmt.Errorf("%w: NITCH hash: %v", ErrInvalidChameleonHashState, err)
	}
	return tx.cacheHash(common.BigToHash(output.Hash)), nil
}

// Hash returns the transaction identity. Incomplete local construction objects
// retain the legacy hash only until txpool admission, where they are either
// initialized or rejected before any consensus path can use this value.
func (tx *Transaction) Hash() common.Hash {
	if hash, ok := tx.cachedHash(); ok {
		return hash
	}
	if tx.data.ChameleonR == nil {
		return tx.cacheHash(rlpHash(tx))
	}
	hash, err := tx.HashWithError()
	if err != nil {
		return common.Hash{}
	}
	return hash
}

// Size returns the true RLP encoded storage size of the transaction, either by
// encoding and returning it, or returning a previsouly cached value.
func (tx *Transaction) Size() common.StorageSize {
	if size := tx.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	tx.EncodeRLP(&c)
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

// Transactions is a Transaction slice type for basic sorting.
type Transactions []*Transaction

// Len returns the length of s.
func (s Transactions) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s.
func (s Transactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements Rlpable. NITCH transactions commit their transaction-level
// chameleon hash in the transaction trie, rather than their mutable wire RLP.
// That makes an accepted payload/r' collision preserve TxRoot while the full
// RLP transaction remains available in the block body and database.
func (s Transactions) GetRlp(i int) []byte {
	if s[i].data.ChameleonR != nil {
		hash, err := s[i].HashWithError()
		if err != nil {
			// DerivableList cannot return an error. Invalid NITCH contexts are
			// rejected before body/root validation; a nil commitment must never
			// be treated as a legacy-RLP fallback.
			return nil
		}
		enc, _ := rlp.EncodeToBytes(hash)
		return enc
	}
	enc, _ := rlp.EncodeToBytes(s[i])
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
