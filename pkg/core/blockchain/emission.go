package blockchain

import "github.com/chronodrachma/chrd/pkg/core/types"

// BlockReward returns the mining reward for a given block height.
// Always exactly 1 CHRD — no halving schedule.
func BlockReward(height uint64) types.Amount {
	return types.BlockReward
}

// TotalSupplyAtHeight returns the total CHRD emitted after the given block height.
// Each block emits 1 CHRD, so: TotalSupply = (height + 1) * 1 CHRD.
func TotalSupplyAtHeight(height uint64) types.Amount {
	return types.Amount((height + 1) * uint64(types.BlockReward))
}

// ExpectedSupplyAtTime returns the expected total supply based on elapsed time
// since genesis. With a 1-hour target: ExpectedSupply = HoursElapsed * 1 CHRD.
func ExpectedSupplyAtTime(genesisTimestamp, currentTimestamp int64) types.Amount {
	if currentTimestamp <= genesisTimestamp {
		return 0
	}
	elapsedSeconds := currentTimestamp - genesisTimestamp
	elapsedHours := uint64(elapsedSeconds) / 3600
	return types.Amount(elapsedHours * uint64(types.BlockReward))
}
