// Copyright 2018 The go-ethereum Authors
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

package core

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type ValidationInfo struct {
	Typ     string `json:"type"`
	Message string `json:"message"`
}
type ValidationMessages struct {
	Messages []ValidationInfo
}

const (
	WARN = "WARNING"
	CRIT = "CRITICAL"
	INFO = "Info"
)

func (vs *ValidationMessages) Crit(msg string) {
	vs.Messages = append(vs.Messages, ValidationInfo{CRIT, msg})
}
func (vs *ValidationMessages) Warn(msg string) {
	vs.Messages = append(vs.Messages, ValidationInfo{WARN, msg})
}
func (vs *ValidationMessages) Info(msg string) {
	vs.Messages = append(vs.Messages, ValidationInfo{INFO, msg})
}

// / getWarnings returns an error with all messages of type WARN of above, or nil if no warnings were present
func (v *ValidationMessages) getWarnings() error {
	var messages []string
	for _, msg := range v.Messages {
		if msg.Typ == WARN || msg.Typ == CRIT {
			messages = append(messages, msg.Message)
		}
	}
	if len(messages) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(messages, ","))
	}
	return nil
}

// SendTxArgs represents the arguments to submit a transaction
type SendTxArgs struct {
	From     common.MixedcaseAddress  `json:"from"`
	To       *common.MixedcaseAddress `json:"to"`
	Gas      hexutil.Uint64           `json:"gas"`
	GasPrice hexutil.Big              `json:"gasPrice"`
	Value    hexutil.Big              `json:"value"`
	Nonce    hexutil.Uint64           `json:"nonce"`
	// We accept "data" and "input" for backwards-compatibility reasons.
	Data  *hexutil.Bytes `json:"data"`
	Input *hexutil.Bytes `json:"input,omitempty"`

	// Redactable extension fields (all optional).
	// SubPublicKey and ChameleonR are accepted for backwards-compatible JSON
	// decoding but are not signed into new transactions here; txpool stamps the
	// canonical pkCH when available. PiHash / PlHash participate in RLP encoding.
	SubPublicKey *hexutil.Big `json:"subPublicKey,omitempty"`
	ChameleonR   *hexutil.Big `json:"chameleonR,omitempty"`
	PiHash       *common.Hash `json:"piHash,omitempty"`
	PlHash       *common.Hash `json:"plHash,omitempty"`
}

func (args SendTxArgs) String() string {
	s, err := json.Marshal(args)
	if err == nil {
		return string(s)
	}
	return err.Error()
}

func (args *SendTxArgs) toTransaction() *types.Transaction {
	var input []byte
	if args.Data != nil {
		input = *args.Data
	} else if args.Input != nil {
		input = *args.Input
	}
	var tx *types.Transaction
	if args.To == nil {
		tx = types.NewContractCreation(uint64(args.Nonce), (*big.Int)(&args.Value), uint64(args.Gas), (*big.Int)(&args.GasPrice), input)
	} else {
		tx = types.NewTransaction(uint64(args.Nonce), args.To.Address(), (*big.Int)(&args.Value), (uint64)(args.Gas), (*big.Int)(&args.GasPrice), input)
	}

	if args.PiHash != nil {
		tx.SetPiHash(*args.PiHash)
	}
	if args.PlHash != nil {
		tx.SetPlHash(*args.PlHash)
	}
	return tx
}
