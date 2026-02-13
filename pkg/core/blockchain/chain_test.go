package blockchain

import (
	"testing"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

func TestGenesisBlockCreation(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}
	now := time.Now()

	genesis, err := chain.InitGenesis(miner, 0, now)
	if err != nil {
		t.Fatalf("InitGenesis failed: %v", err)
	}

	// Height 0.
	if genesis.Header.Height != 0 {
		t.Errorf("genesis height = %d, want 0", genesis.Header.Height)
	}

	// PrevBlockHash is zero.
	if genesis.Header.PrevBlockHash != types.ZeroHash {
		t.Error("genesis PrevBlockHash should be ZeroHash")
	}

	// Exactly one coinbase transaction.
	if len(genesis.Transactions) != 1 {
		t.Fatalf("genesis has %d transactions, want 1", len(genesis.Transactions))
	}
	coinbase := genesis.Transactions[0]
	if coinbase.Type != types.TxTypeCoinbase {
		t.Errorf("genesis tx type = %d, want TxTypeCoinbase", coinbase.Type)
	}
	if coinbase.Amount != types.BlockReward {
		t.Errorf("coinbase amount = %d, want %d", coinbase.Amount, types.BlockReward)
	}
	if coinbase.To != miner {
		t.Error("coinbase recipient should be the miner address")
	}

	// Block hash is non-zero.
	if genesis.Hash.IsZero() {
		t.Error("genesis block hash should not be zero")
	}

	// PoW hash is non-zero.
	if genesis.PowHash.IsZero() {
		t.Error("genesis PoW hash should not be zero")
	}

	// Chain state.
	if chain.Height() != 0 {
		t.Errorf("chain height = %d, want 0", chain.Height())
	}
	if chain.Tip() != genesis {
		t.Error("chain tip should be the genesis block")
	}
	if chain.TotalSupply() != types.BlockReward {
		t.Errorf("total supply = %d, want %d", chain.TotalSupply(), types.BlockReward)
	}
}

func TestGenesisDoubleInit(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}

	_, err := chain.InitGenesis(miner, 0, time.Now())
	if err != nil {
		t.Fatalf("first InitGenesis failed: %v", err)
	}

	_, err = chain.InitGenesis(miner, 0, time.Now())
	if err != ErrChainAlreadyInitialized {
		t.Errorf("second InitGenesis error = %v, want ErrChainAlreadyInitialized", err)
	}
}

func TestBlockRewardConstant(t *testing.T) {
	heights := []uint64{0, 1, 100, 1_000_000, 8_760} // 8760 = hours in a year
	for _, h := range heights {
		reward := BlockReward(h)
		if reward != types.BlockReward {
			t.Errorf("BlockReward(%d) = %d, want %d", h, reward, types.BlockReward)
		}
	}
}

func TestTotalSupplyAtHeight(t *testing.T) {
	tests := []struct {
		height uint64
		want   types.Amount
	}{
		{0, types.BlockReward},                             // 1 CHRD after genesis
		{23, types.Amount(24 * uint64(types.BlockReward))}, // 24 CHRD after 24 blocks
		{8759, types.Amount(8760 * uint64(types.BlockReward))},
	}
	for _, tt := range tests {
		got := TotalSupplyAtHeight(tt.height)
		if got != tt.want {
			t.Errorf("TotalSupplyAtHeight(%d) = %d, want %d", tt.height, got, tt.want)
		}
	}
}

func TestCoinbaseMaturity(t *testing.T) {
	tests := []struct {
		outputHeight  uint64
		currentHeight uint64
		want          bool
	}{
		{0, 0, false},
		{0, 23, false},
		{0, 24, true},
		{0, 25, true},
		{10, 33, false},
		{10, 34, true},
		{100, 123, false},
		{100, 124, true},
	}
	for _, tt := range tests {
		got := IsMature(tt.outputHeight, tt.currentHeight)
		if got != tt.want {
			t.Errorf("IsMature(%d, %d) = %v, want %v", tt.outputHeight, tt.currentHeight, got, tt.want)
		}
	}
}

func TestSpendableBalance(t *testing.T) {
	utxos := []UTXO{
		{BlockHeight: 0, Amount: types.BlockReward},
		{BlockHeight: 10, Amount: types.BlockReward},
		{BlockHeight: 20, Amount: types.BlockReward},
		{BlockHeight: 30, Amount: types.BlockReward},
	}

	tests := []struct {
		currentHeight uint64
		want          types.Amount
	}{
		{23, 0},                                              // Nothing mature yet
		{24, types.BlockReward},                              // Only height-0 output
		{34, types.Amount(2 * uint64(types.BlockReward))},    // Heights 0 and 10
		{44, types.Amount(3 * uint64(types.BlockReward))},    // Heights 0, 10, 20
		{54, types.Amount(4 * uint64(types.BlockReward))},    // All mature
	}
	for _, tt := range tests {
		got := SpendableBalance(utxos, tt.currentHeight)
		if got != tt.want {
			t.Errorf("SpendableBalance at height %d = %d, want %d", tt.currentHeight, got, tt.want)
		}
	}
}

func TestValidateBlock_InvalidPrevHash(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}
	genesis, _ := chain.InitGenesis(miner, 0, time.Now())

	// Build a block with wrong prev hash.
	block := buildTestBlock(t, hasher, genesis, miner, types.Hash{0xFF})
	err := chain.AddBlock(block)
	if err != ErrInvalidPrevHash {
		t.Errorf("expected ErrInvalidPrevHash, got: %v", err)
	}
}

func TestValidateBlock_InvalidHeight(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}
	genesis, _ := chain.InitGenesis(miner, 0, time.Now())

	// Build a valid block but set wrong height.
	block := buildTestBlock(t, hasher, genesis, miner, genesis.Hash)
	block.Header.Height = 5 // wrong, should be 1
	// Recompute hashes after header modification.
	block.Hash = block.ComputeHash()
	powHash, _ := hasher.Hash(block.Header.Serialize())
	block.PowHash = powHash

	err := chain.AddBlock(block)
	if err != ErrInvalidHeight {
		t.Errorf("expected ErrInvalidHeight, got: %v", err)
	}
}

func TestValidateBlock_TimestampBeforeParent(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}
	now := time.Now()
	genesis, _ := chain.InitGenesis(miner, 0, now)

	// Build block with timestamp before genesis.
	block := buildTestBlock(t, hasher, genesis, miner, genesis.Hash)
	block.Header.Timestamp = now.Add(-1 * time.Hour)
	block.Header.MerkleRoot = types.ComputeMerkleRoot(block.Transactions)
	block.Hash = block.ComputeHash()
	powHash, _ := hasher.Hash(block.Header.Serialize())
	block.PowHash = powHash

	err := chain.AddBlock(block)
	if err != ErrTimestampTooOld {
		t.Errorf("expected ErrTimestampTooOld, got: %v", err)
	}
}

func TestValidateBlock_InvalidCoinbaseAmount(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain := NewChain(hasher)
	miner := types.Hash{0x01}
	genesis, _ := chain.InitGenesis(miner, 0, time.Now())

	// Build block with wrong coinbase amount.
	coinbase := &types.Transaction{
		Type:      types.TxTypeCoinbase,
		Timestamp: time.Now(),
		From:      types.ZeroHash,
		To:        miner,
		Amount:    types.Amount(2 * uint64(types.BlockReward)), // 2 CHRD instead of 1
		Nonce:     1,
	}
	coinbase.ID = coinbase.ComputeID()

	block := &types.Block{
		Header: types.BlockHeader{
			Version:       1,
			Height:        1,
			Timestamp:     time.Now().Add(1 * time.Hour),
			PrevBlockHash: genesis.Hash,
			MerkleRoot:    types.ComputeMerkleRoot([]*types.Transaction{coinbase}),
			Difficulty:    0,
			Nonce:         0,
		},
		Transactions: []*types.Transaction{coinbase},
	}
	block.Hash = block.ComputeHash()
	powHash, _ := hasher.Hash(block.Header.Serialize())
	block.PowHash = powHash

	err := chain.AddBlock(block)
	if err != ErrInvalidCoinbaseAmt {
		t.Errorf("expected ErrInvalidCoinbaseAmt, got: %v", err)
	}
}

// buildTestBlock creates a valid test block following the given parent.
func buildTestBlock(t *testing.T, hasher consensus.Hasher, parent *types.Block, miner types.Hash, prevHash types.Hash) *types.Block {
	t.Helper()
	height := parent.Header.Height + 1

	coinbase := &types.Transaction{
		Type:      types.TxTypeCoinbase,
		Timestamp: time.Now(),
		From:      types.ZeroHash,
		To:        miner,
		Amount:    types.BlockReward,
		Nonce:     height,
	}
	coinbase.ID = coinbase.ComputeID()

	txs := []*types.Transaction{coinbase}

	block := &types.Block{
		Header: types.BlockHeader{
			Version:       1,
			Height:        height,
			Timestamp:     parent.Header.Timestamp.Add(1 * time.Hour),
			PrevBlockHash: prevHash,
			MerkleRoot:    types.ComputeMerkleRoot(txs),
			Difficulty:    0,
			Nonce:         0,
		},
		Transactions: txs,
	}
	block.Hash = block.ComputeHash()
	powHash, err := hasher.Hash(block.Header.Serialize())
	if err != nil {
		t.Fatalf("hasher error: %v", err)
	}
	block.PowHash = powHash
	return block
}
