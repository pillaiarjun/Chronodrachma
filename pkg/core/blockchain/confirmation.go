package blockchain

import "github.com/chronodrachma/chrd/pkg/core/types"

// CoinbaseMaturity is the number of blocks that must be mined after a block
// before its outputs become spendable. 24 blocks ~= 24 hours at 1-hour target.
const CoinbaseMaturity uint64 = 24

// IsMature returns true if outputs from the block at outputHeight are spendable
// at the given currentHeight.
// A coinbase mined at height 10 becomes spendable at height 34 (10 + 24).
func IsMature(outputHeight, currentHeight uint64) bool {
	if currentHeight < outputHeight {
		return false
	}
	return (currentHeight - outputHeight) >= CoinbaseMaturity
}

// UTXO represents an unspent transaction output with its originating block height.
type UTXO struct {
	TxID          types.Hash
	OutputIndex   uint32
	BlockHeight   uint64
	Amount        types.Amount
	RecipientAddr types.Hash
}

// SpendableBalance computes the spendable balance at a given chain height
// by summing only outputs that satisfy the maturity requirement.
func SpendableBalance(utxos []UTXO, currentHeight uint64) types.Amount {
	var total types.Amount
	for _, u := range utxos {
		if IsMature(u.BlockHeight, currentHeight) {
			total += u.Amount
		}
	}
	return total
}
