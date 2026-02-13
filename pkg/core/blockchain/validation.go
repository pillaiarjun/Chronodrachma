package blockchain

import (
	"errors"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

var (
	ErrInvalidPrevHash    = errors.New("block previous hash does not match parent")
	ErrInvalidHeight      = errors.New("block height is not parent height + 1")
	ErrInvalidTimestamp    = errors.New("block timestamp is invalid")
	ErrTimestampTooOld    = errors.New("block timestamp is before parent timestamp")
	ErrTimestampTooFar    = errors.New("block timestamp is too far in the future")
	ErrInvalidPoW         = errors.New("block PoW hash does not meet difficulty target")
	ErrInvalidBlockHash   = errors.New("block hash does not match header")
	ErrInvalidMerkleRoot  = errors.New("merkle root does not match transactions")
	ErrNoCoinbaseTx       = errors.New("block must contain exactly one coinbase transaction")
	ErrInvalidCoinbaseAmt = errors.New("coinbase amount does not match block reward")
	ErrInvalidCoinbasePos = errors.New("coinbase transaction must be first in block")
	ErrPowHashMismatch    = errors.New("block PoW hash does not match re-execution")
)

// MaxFutureBlockTime is how far ahead of local time a block's timestamp can be.
const MaxFutureBlockTime = 2 * time.Hour

// ValidateBlock performs full validation of a block against its parent.
func ValidateBlock(block *types.Block, parent *types.Block, hasher consensus.Hasher) error {
	// 1. Height continuity.
	if block.Header.Height != parent.Header.Height+1 {
		return ErrInvalidHeight
	}

	// 2. Previous block hash integrity.
	if block.Header.PrevBlockHash != parent.Hash {
		return ErrInvalidPrevHash
	}

	// 3. Timestamp must be after parent.
	if !block.Header.Timestamp.After(parent.Header.Timestamp) {
		return ErrTimestampTooOld
	}

	// 4. Timestamp must not be too far in the future.
	if block.Header.Timestamp.After(time.Now().Add(MaxFutureBlockTime)) {
		return ErrTimestampTooFar
	}

	return validateBlockInternal(block, hasher)
}

// ValidateGenesis checks that the genesis block is well-formed.
func ValidateGenesis(genesis *types.Block, hasher consensus.Hasher) error {
	if genesis.Header.Height != 0 {
		return ErrInvalidHeight
	}
	if genesis.Header.PrevBlockHash != types.ZeroHash {
		return ErrInvalidPrevHash
	}
	return validateBlockInternal(genesis, hasher)
}

// validateBlockInternal checks merkle root, block hash, PoW, and coinbase.
func validateBlockInternal(block *types.Block, hasher consensus.Hasher) error {
	// 5. Merkle root.
	expectedMerkle := types.ComputeMerkleRoot(block.Transactions)
	if block.Header.MerkleRoot != expectedMerkle {
		return ErrInvalidMerkleRoot
	}

	// 6. Block hash (SHA-256 of header).
	expectedHash := block.ComputeHash()
	if block.Hash != expectedHash {
		return ErrInvalidBlockHash
	}

	// 7. Re-execute PoW hash.
	headerBytes := block.Header.Serialize()
	computedPow, err := hasher.Hash(headerBytes)
	if err != nil {
		return err
	}
	if block.PowHash != computedPow {
		return ErrPowHashMismatch
	}

	// 8. PoW meets difficulty.
	// Note: We check if the hash meets the claimed difficulty. 
	// The check for whether the claimed difficulty is *correct* relative to the chain
	// must be done by the caller (Contextual Validation).
	if !consensus.MeetsDifficulty(block.PowHash, block.Header.Difficulty) {
		return ErrInvalidPoW
	}

	// 9. Coinbase validation: exactly one coinbase TX at position 0.
	coinbaseCount := 0
	for i, tx := range block.Transactions {
		if tx.Type == types.TxTypeCoinbase {
			if i != 0 {
				return ErrInvalidCoinbasePos
			}
			coinbaseCount++
		}
	}
	if coinbaseCount != 1 {
		return ErrNoCoinbaseTx
	}

	// 10. Coinbase amount must equal block reward.
	if block.Transactions[0].Amount != BlockReward(block.Header.Height) {
		return ErrInvalidCoinbaseAmt
	}

	return nil
}
