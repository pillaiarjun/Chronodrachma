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

// Chain represents the blockchain state backed by persistent storage.
type Chain struct {
	mu          sync.RWMutex
	store       BlockStore
	tip         *types.Block
	hasher      consensus.Hasher
	genesisTime time.Time
}

// NewChain creates a new chain instance.
// Now it takes a store instead of creating internal maps.
func NewChain(store BlockStore, hasher consensus.Hasher) (*Chain, error) {
	chain := &Chain{
		store:  store,
		hasher: hasher,
	}

	// Try to load tip from store
	headHash, err := store.GetHead()
	if err == nil {
		// Chain exists, load tip block
		tip, err := store.GetBlockByHash(headHash)
		if err != nil {
			return nil, err
		}
		chain.tip = tip
		// Ideally load genesis time too, but for prototype we can fetch block 0?
		// For now, let's just leave genesisTime 0 if not init, or fetch it.
		genesis, err := store.GetBlockByHeight(0)
		if err == nil {
			chain.genesisTime = genesis.Header.Timestamp
		}
	}

	return chain, nil
}

// InitGenesis creates, validates, and adds the genesis block to the chain.
func (c *Chain) InitGenesis(minerAddress types.Hash, difficulty uint64, timestamp time.Time) (*types.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already initialized
	if c.tip != nil {
		return c.tip, ErrChainAlreadyInitialized
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

	// Save to store
	if err := c.store.SaveBlock(block); err != nil {
		return nil, err
	}
	if err := c.store.SaveHead(block.Hash); err != nil {
		return nil, err
	}

	c.tip = block
	c.genesisTime = timestamp

	return block, nil
}

// AddBlock validates and appends a block to the chain.
func (c *Chain) AddBlock(block *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tip == nil {
		return errors.New("chain not initialized: no genesis block")
	}

	parent := c.tip

	// Verify difficulty adjustment (Contextual/Consensus Rule).
	// We use an internal lookup to avoid deadlocks (AddBlock holds the lock, but store is thread safe or we use read methods).
	// However, CalcNextRequiredDifficulty needs a callback.
	getBlockInternal := func(h uint64) (*types.Block, error) {
		// Optimization: if h is current tip height, return tip
		if h == c.tip.Header.Height {
			return c.tip, nil
		}
		// Otherwise fetch from store
		return c.store.GetBlockByHeight(h)
	}

	requiredDiff, err := consensus.CalcNextRequiredDifficulty(parent, getBlockInternal)
	if err != nil {
		return err
	}

	if block.Header.Difficulty != requiredDiff {
		return errors.New("block difficulty does not match required network difficulty")
	}

	if err := ValidateBlock(block, parent, c.hasher); err != nil {
		return err
	}

	// Save to store
	if err := c.store.SaveBlock(block); err != nil {
		return err
	}
	if err := c.store.SaveHead(block.Hash); err != nil {
		return err
	}

	c.tip = block

	return nil
}

// GetBlockByHeight returns the block at the given height.
func (c *Chain) GetBlockByHeight(height uint64) (*types.Block, error) {
	// Optimization: check tip first
	c.mu.RLock()
	tip := c.tip
	c.mu.RUnlock()

	if tip != nil && tip.Header.Height == height {
		return tip, nil
	}
	
	return c.store.GetBlockByHeight(height)
}

// GetBlockByHash returns the block with the given hash.
func (c *Chain) GetBlockByHash(hash types.Hash) (*types.Block, error) {
	return c.store.GetBlockByHash(hash)
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

// GetAccountState calculates the balance and nonce for a given address
// by scanning the entire blockchain history (Prototype: O(N)).
func (c *Chain) GetAccountState(addr types.Hash) (types.Amount, uint64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var balance types.Amount
	var nonce uint64
	currentHeight := uint64(0)
	if c.tip != nil {
		currentHeight = c.tip.Header.Height
	}

	// Iterate from genesis to tip
	for h := uint64(0); h <= currentHeight; h++ {
		// Optimization: We could cache UTXOs/State, but for now we look up.
		// Note: We use GetBlockByHeight which uses the store.
		// Internal method to avoid deadlock if we were calling public method (but we are holding lock).
		// Wait, GetBlockByHeight acquires lock?
		// Chain.GetBlockByHeight:
		// c.mu.RLock() ... defer Match.
		// Recursion on RLock is OK?
		// Go RWMutex: "If a goroutine holds a RWMutex for reading and another goroutine might verify... no."
		// "A RWMutex is NOT recursive."
		// calling c.GetBlockByHeight() inside c.GetAccountState() (which holds RLock) WILL DEADLOCK if GetBlockByHeight tries to RLock.
		// Yes, `GetBlockByHeight` does `c.mu.RLock()`.
		// So we must NOT call `c.GetBlockByHeight`.
		// We should call `c.store.GetBlockByHeight(h)` directly.
		
		block, err := c.store.GetBlockByHeight(h)
		if err != nil {
			// If not found (and h <= currentHeight), something is wrong or tip moved?
			// We held RLock, so structure shouldn't change, but store might?
			// Generally safe to assume it exists.
			if err == ErrBlockNotFound {
				break
			}
			return 0, 0, err
		}

		for _, tx := range block.Transactions {
			// 1. Credits
			if tx.To == addr {
				if tx.Type == types.TxTypeCoinbase {
					// Check Maturity
					if IsMature(block.Header.Height, currentHeight) {
						balance += tx.Amount
					}
				} else {
					// Standard transfer
					balance += tx.Amount
				}
			}

			// 2. Debits (Sent transactions)
			if tx.From == addr {
				// Amount + Fee
				// Check for underflow? logic: balance -= (Amount + Fee)
				totalDebit := tx.Amount + tx.Fee
				if balance < totalDebit {
					// Should not happen in valid chain, but safe to clamp or allow negative?
					// In a valid chain, this tx wouldn't exist if balance was insufficient.
					// However, if we are recalculating state, maybe it goes negative?
					// Or maybe we received funds in same block?
					// Order matters? "In a block, transactions apply in order"?
					// We are iterating txs in order.
					// So it should be fine.
					// balance -= totalDebit
				}
				balance -= totalDebit
				nonce++
			}
		}
	}

	return balance, nonce, nil
}
