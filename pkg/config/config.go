package config

import (
	"time"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

// NetworkConfig holds the network-wide parameters.
type NetworkConfig struct {
	Name             string
	GenesisTimestamp time.Time
	InitialDifficulty uint64
	SeedNodes        []string
}

// TestnetConfig defines the parameters for the Phase II testnet.
var TestnetConfig = NetworkConfig{
	Name:             "chrd-testnet-v1",
	GenesisTimestamp: time.Now(), // Will be overridden at runtime or fixed for shared genesis
	InitialDifficulty: 1000,      // Low difficulty for CPU mining test
	SeedNodes:        []string{}, // To be populated via CLI or discovery
}

// GenesisMinerAddress is a hardcoded address for the genesis coinbase.
// In a real launch, this would be a burn address or specific premine addr (if any).
var GenesisMinerAddress = types.Hash{} // Zero hash for now
