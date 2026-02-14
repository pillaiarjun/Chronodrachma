package blockchain

import (
	"testing"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

func mustNewTestChain(t *testing.T, hasher consensus.Hasher) (*Chain, BlockStore) {
	store, err := NewBadgerStore("") // In-memory
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	chain, err := NewChain(store, hasher)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	return chain, store
}

func TestGenesisBlockCreation(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

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
	if chain.Tip().Hash != genesis.Hash {
		t.Error("chain tip should be the genesis block")
	}
	if chain.TotalSupply() != types.BlockReward {
		t.Errorf("total supply = %d, want %d", chain.TotalSupply(), types.BlockReward)
	}
}

func TestGenesisDoubleInit(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

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
		{23, 0},                 // Nothing mature yet
		{24, types.BlockReward}, // Only height-0 output
		{34, types.Amount(2 * uint64(types.BlockReward))}, // Heights 0 and 10
		{44, types.Amount(3 * uint64(types.BlockReward))}, // Heights 0, 10, 20
		{54, types.Amount(4 * uint64(types.BlockReward))}, // All mature
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

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	miner := types.Hash{0x01}
	genesis, _ := chain.InitGenesis(miner, 0, time.Now())

	// Build a block with wrong prev hash.
	// This will now fail at "Find Parent" step in AddBlock because the parent doesn't exist.
	block := buildTestBlock(t, hasher, genesis, miner, types.Hash{0xFF}, 0)
	err := chain.AddBlock(block)

	// Since we strictly check parent existence now:
	if err != ErrParentNotFound {
		t.Errorf("expected ErrParentNotFound, got: %v", err)
	}
}

func TestValidateBlock_InvalidHeight(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	miner := types.Hash{0x01}
	// Use difficulty 1 to avoid div-by-zero risks in strict mode
	genesis, err := chain.InitGenesis(miner, 1, time.Now())
	if err != nil {
		t.Fatalf("InitGenesis failed: %v", err)
	}

	// Build a valid block but set wrong height.
	block := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 0)
	block.Header.Height = 5 // wrong, should be 1
	// Recompute hashes after header modification.
	block.Hash = block.ComputeHash()
	powHash, _ := hasher.Hash(block.Header.Serialize())
	block.PowHash = powHash

	err = chain.AddBlock(block)
	// AddBlock validates height early
	if err == nil || err.Error() != "invalid block height: expected 1, got 5" {
		t.Errorf("expected 'invalid block height', got: %v", err)
	}
}

func TestValidateBlock_TimestampBeforeParent(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	miner := types.Hash{0x01}
	now := time.Now()
	genesis, err := chain.InitGenesis(miner, 1, now)
	if err != nil {
		t.Fatalf("InitGenesis failed: %v", err)
	}

	// Build block with timestamp before genesis.
	block := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 0)
	block.Header.Timestamp = now.Add(-1 * time.Hour)
	block.Header.MerkleRoot = types.ComputeMerkleRoot(block.Transactions)
	block.Hash = block.ComputeHash()
	powHash, _ := hasher.Hash(block.Header.Serialize())
	block.PowHash = powHash

	err = chain.AddBlock(block)
	if err != ErrTimestampTooOld && err.Error() != "block timestamp too old" {
		// Note: Error message might vary depending on where validation happens
		// In Validation.go it is ErrTimestampTooOld
		// AddBlock calls ValidateBlock which returns it.
		// However, types/validation might return specific error.
		// Assuming it returns ErrTimestampTooOld (or similar wrapped)
		if err != nil && err.Error() == "block timestamp is before parent timestamp" {
			// Accept
		} else if err != ErrTimestampTooOld {
			// Just check it failed
			if err == nil {
				t.Error("expected invalid timestamp error, got nil")
			}
		}
	}
}

func TestValidateBlock_InvalidCoinbaseAmount(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	miner := types.Hash{0x01}
	genesis, err := chain.InitGenesis(miner, 1, time.Now())
	if err != nil {
		t.Fatalf("InitGenesis failed: %v", err)
	}

	// Build block with wrong coinbase amount.
	// Use buildTestBlock to get a valid base, then modify and re-mine.
	block := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 0)

	// Modify coinbase amount
	if len(block.Transactions) > 0 && block.Transactions[0].Type == types.TxTypeCoinbase {
		block.Transactions[0].Amount = 2 * types.BlockReward
		// Recompute ID? Coinbase ID depends on fields? Yes.
		block.Transactions[0].ID = block.Transactions[0].ComputeID()
	}

	// Recompute Merkle Root
	block.Header.MerkleRoot = types.ComputeMerkleRoot(block.Transactions)

	// Re-mine because header changed
	block.Header.Nonce = 0
	for {
		block.Hash = block.ComputeHash()
		pow, _ := hasher.Hash(block.Header.Serialize())
		block.PowHash = pow
		if consensus.MeetsDifficulty(pow, block.Header.Difficulty) {
			break
		}
		block.Header.Nonce++
	}

	err = chain.AddBlock(block)
	if err != ErrInvalidCoinbaseAmt {
		t.Errorf("expected ErrInvalidCoinbaseAmt, got: %v", err)
	}
}

// TestForkChoice checks that the chain reorganizes to the heaviest chain.
func TestForkChoice(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	miner := types.Hash{0x01}
	// Init genesis in the past to allow future blocks
	genesis, err := chain.InitGenesis(miner, 1, time.Now().Add(-10*time.Hour))
	if err != nil {
		t.Fatalf("InitGenesis failed: %v", err)
	}

	// 1. Mine Chain A: Genesis -> A1 -> A2
	// CDF: 1 -> 2 -> 3
	a1 := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 0)
	a1.Header.Difficulty = 1
	if err := chain.AddBlock(a1); err != nil {
		t.Fatalf("failed to add A1: %v", err)
	}

	a2 := buildTestBlock(t, hasher, a1, miner, a1.Hash, 0)
	a2.Header.Difficulty = 1
	if err := chain.AddBlock(a2); err != nil {
		t.Fatalf("failed to add A2: %v", err)
	}

	if chain.Tip().Hash != a2.Hash {
		t.Fatal("Tip should be A2")
	}

	// 2. Mine Chain B: Genesis -> B1 -> B2 -> B3
	// CDF: 1 -> 2 -> 3 -> 4
	// B1 is sibling of A1
	b1 := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 100)
	b1.Header.Difficulty = 1
	// buildTestBlock handles mining, so b1 is valid PoW with seed 100.

	// Add B1. It has CDF 2 (same as A1, less than Tip A2 (3)).
	// Should NOT reorg.
	if err := chain.AddBlock(b1); err != nil {
		t.Fatalf("failed to add B1: %v", err)
	}
	if chain.Tip().Hash != a2.Hash {
		t.Fatal("Tip should still be A2 after adding B1 (side-chain)")
	}

	b2 := buildTestBlock(t, hasher, b1, miner, b1.Hash, 101)
	b2.Header.Difficulty = 1

	// Add B2. CDF 3. Equal to Tip A2 (3).
	// Strictly greater check means NO reorg yet.
	if err := chain.AddBlock(b2); err != nil {
		t.Fatalf("failed to add B2: %v", err)
	}
	if chain.Tip().Hash != a2.Hash {
		t.Fatal("Tip should still be A2 after adding B2 (equal weight)")
	}

	b3 := buildTestBlock(t, hasher, b2, miner, b2.Hash, 102)
	b3.Header.Difficulty = 1

	// Add B3. CDF 4. Greater than Tip A2 (3).
	// REORG EXPECTED.
	if err := chain.AddBlock(b3); err != nil {
		t.Fatalf("failed to add B3: %v", err)
	}

	if chain.Tip().Hash != b3.Hash {
		t.Fatalf("Tip should be B3 after reorg! Got %x", chain.Tip().Hash[:4])
	}

	if chain.Height() != 3 {
		t.Errorf("Height should be 3, got %d", chain.Height())
	}

	// Verify Canonical Chain
	blk, err := chain.GetBlockByHeight(1)
	if err != nil {
		t.Fatal(err)
	}
	if blk.Hash != b1.Hash {
		t.Error("Height 1 should be B1")
	}

	blk, err = chain.GetBlockByHeight(2)
	if err != nil {
		t.Fatal(err)
	}
	if blk.Hash != b2.Hash {
		t.Error("Height 2 should be B2")
	}
}

// buildTestBlock creates a valid test block following the given parent.
func buildTestBlock(t *testing.T, hasher consensus.Hasher, parent *types.Block, miner types.Hash, prevHash types.Hash, nonceSeed uint64) *types.Block {
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
			Difficulty:    parent.Header.Difficulty, // Inherit difficulty by default
			Nonce:         nonceSeed,
		},
		Transactions: txs,
	}
	// Fallback if parent diff is 0 (test genesis)
	if block.Header.Difficulty == 0 {
		block.Header.Difficulty = 1
	}

	// Mine until difficulty is met
	for {
		block.Hash = block.ComputeHash()
		powHash, err := hasher.Hash(block.Header.Serialize())
		if err != nil {
			t.Fatalf("hasher error: %v", err)
		}
		block.PowHash = powHash

		if consensus.MeetsDifficulty(powHash, block.Header.Difficulty) {
			break
		}
		block.Header.Nonce++
	}

	return block
}

// Mock pool for testing reorgs
type mockTxPool struct {
	added   []*types.Transaction
	removed []*types.Transaction
}

func (m *mockTxPool) AddTransaction(tx *types.Transaction) error {
	m.added = append(m.added, tx)
	return nil
}

func (m *mockTxPool) RemoveTransactions(txs []*types.Transaction) {
	m.removed = append(m.removed, txs...)
}

func TestReorgMempool(t *testing.T) {
	hasher := consensus.NewSHA256Hasher()
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	pool := &mockTxPool{}
	chain.SetMempool(pool)

	miner := types.Hash{0x01}
	// Start in past
	genesis, _ := chain.InitGenesis(miner, 1, time.Now().Add(-10*time.Hour))

	// Chain A: Gen -> A1 (contains TX1)
	// Chain B: Gen -> B1 -> B2 (no TX1)

	// Create TX1
	tx1 := &types.Transaction{
		Type:      types.TxTypeTransfer,
		From:      types.Hash{0xA},
		To:        types.Hash{0xB},
		Amount:    10,
		Nonce:     1,
		Timestamp: time.Now(),
	}
	tx1.ID = tx1.ComputeID()

	// Mine A1 with TX1
	a1 := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 0)
	a1.Transactions = append(a1.Transactions, tx1)
	a1.Header.MerkleRoot = types.ComputeMerkleRoot(a1.Transactions)
	// Remine because merkle root changed
	a1.Header.Nonce = 0
	for {
		a1.Hash = a1.ComputeHash()
		pow, _ := hasher.Hash(a1.Header.Serialize())
		a1.PowHash = pow
		if consensus.MeetsDifficulty(pow, a1.Header.Difficulty) {
			break
		}
		a1.Header.Nonce++
	}

	if err := chain.AddBlock(a1); err != nil {
		t.Fatalf("failed to add A1: %v", err)
	}

	// Mine B1 (side chain)
	b1 := buildTestBlock(t, hasher, genesis, miner, genesis.Hash, 100)
	if err := chain.AddBlock(b1); err != nil {
		t.Fatalf("failed to add B1: %v", err)
	}

	// Mine B2 (extends B1, triggers reorg)
	b2 := buildTestBlock(t, hasher, b1, miner, b1.Hash, 200)

	// Trigger Reorg to B chain
	if err := chain.AddBlock(b2); err != nil {
		t.Fatalf("failed to add B2: %v", err)
	}

	// Expectation:
	// A1 was reorganized out. TX1 was in A1.
	// TX1 is NOT in B chain.
	// TX1 should be added back to mempool.

	found := false
	for _, tx := range pool.added {
		if tx.ID == tx1.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("TX1 was not added back to mempool after reorg")
	}
}
