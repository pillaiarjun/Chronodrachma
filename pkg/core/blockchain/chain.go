package blockchain

import (
	"errors"
	"sync"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

var (
	ErrChainAlreadyInitialized = errors.New("chain is already initialized with genesis")
	ErrBlockNotFound           = errors.New("block not found")
)

// Chain represents the in-memory blockchain state.
type Chain struct {
	mu           sync.RWMutex
	blocks       []*types.Block
	blocksByHash map[types.Hash]*types.Block
	tip          *types.Block
	hasher       consensus.Hasher
	genesisTime  time.Time
}

// NewChain creates a new empty chain with the given hasher.
func NewChain(hasher consensus.Hasher) *Chain {
	return &Chain{
		blocksByHash: make(map[types.Hash]*types.Block),
		hasher:       hasher,
	}
}

// InitGenesis creates, validates, and adds the genesis block to the chain.
func (c *Chain) InitGenesis(minerAddress types.Hash, difficulty uint64, timestamp time.Time) (*types.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.blocks) > 0 {
		return nil, ErrChainAlreadyInitialized
	}

	// Create coinbase transaction.
	coinbase := &types.Transaction{
		Type:      types.TxTypeCoinbase,
		Timestamp: timestamp,
		From:      types.ZeroHash,
		To:        minerAddress,
		Amount:    types.BlockReward,
		Fee:       0,
		Nonce:     0,
	}
	coinbase.ID = coinbase.ComputeID()

	txs := []*types.Transaction{coinbase}

	// Build the genesis block header.
	header := types.BlockHeader{
		Version:       1,
		Height:        0,
		Timestamp:     timestamp,
		PrevBlockHash: types.ZeroHash,
		MerkleRoot:    types.ComputeMerkleRoot(txs),
		Difficulty:    difficulty,
		Nonce:         0,
	}

	block := &types.Block{
		Header:       header,
		Transactions: txs,
	}

	// Compute block identity hash.
	block.Hash = block.ComputeHash()

	// Compute PoW hash.
	headerBytes := header.Serialize()
	powHash, err := c.hasher.Hash(headerBytes)
	if err != nil {
		return nil, err
	}
	block.PowHash = powHash

	// Validate the genesis block.
	if err := ValidateGenesis(block, c.hasher); err != nil {
		return nil, err
	}

	// Add to chain.
	c.blocks = append(c.blocks, block)
	c.blocksByHash[block.Hash] = block
	c.tip = block
	c.genesisTime = timestamp

	return block, nil
}

// AddBlock validates and appends a block to the chain.
func (c *Chain) AddBlock(block *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.blocks) == 0 {
		return errors.New("chain not initialized: no genesis block")
	}

	parent := c.tip

	// Verify difficulty adjustment (Contextual/Consensus Rule).
	// We use an internal lookup to avoid deadlocks (AddBlock holds the lock).
	getBlockInternal := func(h uint64) (*types.Block, error) {
		if h >= uint64(len(c.blocks)) {
			return nil, ErrBlockNotFound
		}
		return c.blocks[h], nil
	}

	requiredDiff, err := consensus.CalcNextRequiredDifficulty(parent, getBlockInternal)
	if err != nil {
		return err
	}

	if block.Header.Difficulty != requiredDiff {
		// return fmt.Errorf("incorrect difficulty: expected %d, got %d", requiredDiff, block.Header.Difficulty)
		// To avoid importing fmt, we can use validation.ErrInvalidDifficulty if we define it, 
		// but for now let's just use errors.New with a static message or just fail. 
		// Actually, let's keep it simple and just return a new error.
		return errors.New("block difficulty does not match required network difficulty")
	}

	if err := ValidateBlock(block, parent, c.hasher); err != nil {
		return err
	}

	c.blocks = append(c.blocks, block)
	c.blocksByHash[block.Hash] = block
	c.tip = block

	return nil
}

// GetBlockByHeight returns the block at the given height.
func (c *Chain) GetBlockByHeight(height uint64) (*types.Block, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if height >= uint64(len(c.blocks)) {
		return nil, ErrBlockNotFound
	}
	return c.blocks[height], nil
}

// GetBlockByHash returns the block with the given hash.
func (c *Chain) GetBlockByHash(hash types.Hash) (*types.Block, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	block, ok := c.blocksByHash[hash]
	if !ok {
		return nil, ErrBlockNotFound
	}
	return block, nil
}

// Tip returns the current chain tip.
func (c *Chain) Tip() *types.Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tip
}

// Height returns the height of the current chain tip. Returns 0 for empty chains.
func (c *Chain) Height() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.tip == nil {
		return 0
	}
	return c.tip.Header.Height
}

// TotalSupply returns the total CHRD emitted up to the current chain tip.
func (c *Chain) TotalSupply() types.Amount {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.tip == nil {
		return 0
	}
	return TotalSupplyAtHeight(c.tip.Header.Height)
}
