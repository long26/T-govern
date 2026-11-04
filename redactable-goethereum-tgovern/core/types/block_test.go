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
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestBlockEncoding(t *testing.T) {
	tx1 := NewTransaction(0, common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"), big.NewInt(10), 50000, big.NewInt(10), nil)
	tx1, _ = tx1.WithSignature(HomesteadSigner{}, common.Hex2Bytes("9bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094f8a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b100"))
	source := NewBlock(&Header{
		ParentHash: common.HexToHash("83cafc574e1f51ba9dc0568fc617a08ea2429fb384059c972f13b19fa1c8dd55"),
		Coinbase:   common.HexToAddress("8888f1f195afa192cfee860698584c030f4c9db1"),
		Root:       common.HexToHash("ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017"),
		Difficulty: big.NewInt(131072),
		Number:     big.NewInt(1),
		GasLimit:   3141592,
		GasUsed:    21000,
		Time:       1426516743,
		Extra:      common.FromHex("020000"),
		MixDigest:  common.HexToHash("bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff498"),
		Nonce:      EncodeNonce(0xa13a5a8c8f2bb1c4),
	}, []*Transaction{tx1}, nil, nil)
	blockEnc, err := rlp.EncodeToBytes(source)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	var block Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}
	check("Difficulty", block.Difficulty(), big.NewInt(131072))
	check("GasLimit", block.GasLimit(), uint64(3141592))
	check("GasUsed", block.GasUsed(), uint64(21000))
	check("Coinbase", block.Coinbase(), common.HexToAddress("8888f1f195afa192cfee860698584c030f4c9db1"))
	check("MixDigest", block.MixDigest(), common.HexToHash("bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff498"))
	check("Root", block.Root(), common.HexToHash("ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017"))
	check("Hash", block.Hash(), block.Header().Hash())
	check("Nonce", block.Nonce(), uint64(0xa13a5a8c8f2bb1c4))
	check("Time", block.Time(), uint64(1426516743))
	check("Size", block.Size(), common.StorageSize(len(blockEnc)))
	check("len(Transactions)", len(block.Transactions()), 1)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), tx1.Hash())

	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestHeaderHashUsesPlainRLP(t *testing.T) {
	header := &Header{
		ParentHash: common.HexToHash("01"),
		UncleHash:  EmptyUncleHash,
		Root:       common.HexToHash("02"),
		TxHash:     common.HexToHash("03"),
		Extra:      []byte("canonical-extra"),
		Difficulty: big.NewInt(1),
		Number:     big.NewInt(2),
		GasLimit:   1000000,
		Time:       42,
	}
	// Header RLP must remain the canonical Ethereum field sequence. In particular,
	// legacy block-level chameleon metadata must not become an additional field.
	type canonicalHeader struct {
		ParentHash  common.Hash
		UncleHash   common.Hash
		Coinbase    common.Address
		Root        common.Hash
		TxHash      common.Hash
		ReceiptHash common.Hash
		Bloom       Bloom
		Difficulty  *big.Int
		Number      *big.Int
		GasLimit    uint64
		GasUsed     uint64
		Time        uint64
		Extra       []byte
		MixDigest   common.Hash
		Nonce       BlockNonce
	}
	canonical := &canonicalHeader{
		ParentHash:  header.ParentHash,
		UncleHash:   header.UncleHash,
		Coinbase:    header.Coinbase,
		Root:        header.Root,
		TxHash:      header.TxHash,
		ReceiptHash: header.ReceiptHash,
		Bloom:       header.Bloom,
		Difficulty:  header.Difficulty,
		Number:      header.Number,
		GasLimit:    header.GasLimit,
		GasUsed:     header.GasUsed,
		Time:        header.Time,
		Extra:       header.Extra,
		MixDigest:   header.MixDigest,
		Nonce:       header.Nonce,
	}
	if have, want := header.Hash(), rlpHash(canonical); have != want {
		t.Fatalf("header hash must be plain RLP hash: have %x, want %x", have, want)
	}
	modified := CopyHeader(header)
	modified.Extra = []byte("changed-extra")
	if modified.Hash() == header.Hash() {
		t.Fatal("plain header hash must change when header fields change")
	}
}

func TestUncleHash(t *testing.T) {
	uncles := make([]*Header, 0)
	h := CalcUncleHash(uncles)
	exp := common.HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347")
	if h != exp {
		t.Fatalf("empty uncle hash is wrong, got %x != %x", h, exp)
	}
}

func BenchmarkUncleHash(b *testing.B) {
	uncles := make([]*Header, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalcUncleHash(uncles)
	}
}
