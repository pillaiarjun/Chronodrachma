package consensus

import (
	"math/big"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

const (
	// TargetBlockTime is the expected time between blocks in seconds (60 minutes).
	TargetBlockTime = 3600

	// DifficultyAdjustmentWindow is the number of blocks to look back for calculating average time.
	DifficultyAdjustmentWindow = 24
)

// CalcNextRequiredDifficulty calculates the required difficulty for the next block
// based on the moving average of the past blocks.
//
// The difficulty is the inverse of the target hash (Conceptually).
// Here we use a simplified model where 'Difficulty' is a uint64 representing the specific
// requirements (e.g., number of leading zeros or a target value).
//
// For this prototype, we'll use a standard target-based difficulty where:
// Target = 2^256 / Difficulty
// Higher Difficulty = Lower Target = Harder to find hash < Target.
//
// To adjust: NewDifficulty = OldDifficulty * (ActualTime / TargetTime)
// Wait, if ActualTime > TargetTime (too slow), we want easier difficulty.
// NewDifficulty = OldDifficulty * (TargetTime / ActualTime)
//
// note: types.BlockHeader.Difficulty is uint64.
func CalcNextRequiredDifficulty(
	prevBlock *types.Block,
	getBlockByHeight func(uint64) (*types.Block, error),
) (uint64, error) {

	// 1. Genesis and early blocks have constant difficulty.
	if prevBlock == nil || prevBlock.Header.Height < DifficultyAdjustmentWindow {
		if prevBlock != nil {
			return prevBlock.Header.Difficulty, nil
		}
		// Fallback for genesis creation (caller should handle this usually)
		return 1, nil
	}

	// 2. Get the block at the start of the window.
	firstHeight := prevBlock.Header.Height - DifficultyAdjustmentWindow + 1
	firstBlock, err := getBlockByHeight(firstHeight)
	if err != nil {
		return 0, err
	}

	// 3. Calculate actual time taken for the window.
	// Note: We use timestamps.
	actualTime := prevBlock.Header.Timestamp.Unix() - firstBlock.Header.Timestamp.Unix()
	
	// Avoid division by zero or negative time (sanity check).
	if actualTime <= 0 {
		actualTime = 1
	}

	// 4. Calculate target time for the window.
	targetTime := int64(TargetBlockTime * DifficultyAdjustmentWindow)

	// 5. Adjust difficulty.
	// We use big.Int to avoid overflow during calculation.
	oldDiff := new(big.Int).SetUint64(prevBlock.Header.Difficulty)
	
	// NewDiff = OldDiff * (TargetTime / ActualTime)
	// If ActualTime < TargetTime (too fast), factor > 1 -> Difficulty increases.
	// If ActualTime > TargetTime (too slow), factor < 1 -> Difficulty decreases.
	
	newDiff := new(big.Int).Mul(oldDiff, big.NewInt(targetTime))
	newDiff.Div(newDiff, big.NewInt(actualTime))

	// 6. Clamp difficulty.
	// Min difficulty = 1.
	if newDiff.Cmp(big.NewInt(1)) < 0 {
		return 1, nil
	}
	
	// Cap max adjustment factor to prevent wild swings? (Optional for prototype)
	// Let's stick to simple retargeting for now.

	if !newDiff.IsUint64() {
		// Overflowed uint64, cap at max uint64 (extremely unlikely)
		return ^uint64(0), nil
	}

	return newDiff.Uint64(), nil
}
