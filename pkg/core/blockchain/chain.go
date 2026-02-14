package blockchain

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

var (
	ErrChainAlreadyInitialized = errors.New("chain is already initialized with genesis")
	ErrBlockNotFound           = errors.New("block not found")
	ErrParentNotFound          = errors.New("parent block not found")
)

// TxPool defines the interface for Mempool interaction.
type TxPool interface {
	AddTransaction(tx *types.Transaction) error
	RemoveTransactions(txs []*types.Transaction)
}

// Chain represents the blockchain state backed by persistent storage.
type Chain struct {
	mu          sync.RWMutex
	store       BlockStore
	tip         *types.Block
	hasher      consensus.Hasher
	genesisTime time.Time
	pool        TxPool

	// Subscription for tip updates (e.g. for miner)
	subscribers []chan *types.Block
	subMu       sync.Mutex
}

// NewChain creates a new chain instance.
func NewChain(store BlockStore, hasher consensus.Hasher) (*Chain, error) {
	chain := &Chain{
		store:       store,
		hasher:      hasher,
		subscribers: make([]chan *types.Block, 0),
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

		genesis, err := store.GetBlockByHeight(0)
		if err == nil {
			chain.genesisTime = genesis.Header.Timestamp
		}
	}

	return chain, nil
}

// SubscribeTip returns a channel that will receive the new tip block whenever it changes.
// The caller should consume from this channel quickly to avoid blocking.
func (c *Chain) SubscribeTip() <-chan *types.Block {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	// Create a buffered channel to avoid immediate blocking,
	// though we'll use non-blocking sends.
	ch := make(chan *types.Block, 1)
	c.subscribers = append(c.subscribers, ch)
	return ch
}

// notifySubscribers sends the new tip to all subscribers.
func (c *Chain) notifySubscribers(newTip *types.Block) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	for _, ch := range c.subscribers {
		select {
		case ch <- newTip:
		default:
			// If channel is full, drop the update (subscriber is too slow)
		}
	}
}

// SetMempool sets the transaction pool for the chain.
func (c *Chain) SetMempool(pool TxPool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pool = pool
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

	// 1. Save Block Data
	if err := c.store.SaveBlock(block); err != nil {
		return nil, err
	}
	// 2. Set as Canonical (Genesis is always canonical initially)
	if err := c.store.SetCanonical(0, block.Hash); err != nil {
		return nil, err
	}
	// 3. Save Cumulative Difficulty
	if err := c.store.SaveCumulativeDifficulty(block.Hash, difficulty); err != nil {
		return nil, err
	}
	// 4. Save Head
	if err := c.store.SaveHead(block.Hash); err != nil {
		return nil, err
	}

	c.tip = block
	c.genesisTime = timestamp

	return block, nil
}

// AddBlock validates and adds a block. It handles forks and chain reorganization.
func (c *Chain) AddBlock(block *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tip == nil {
		return errors.New("chain not initialized: no genesis block")
	}

	// 1. Check if block already exists
	if b, _ := c.store.GetBlockByHash(block.Hash); b != nil {
		return nil // Already processed
	}

	// 2. Find Parent
	parent, err := c.store.GetBlockByHash(block.Header.PrevBlockHash)
	if err != nil {
		// If we support orphans, we would stash it here.
		// For now, we reject if parent is missing.
		return ErrParentNotFound
	}

	// 3. Validate Height
	if block.Header.Height != parent.Header.Height+1 {
		return fmt.Errorf("invalid block height: expected %d, got %d", parent.Header.Height+1, block.Header.Height)
	}

	// 4. Verify Difficulty Adjustment
	// We need to look at the chain *leading up to* this block, effectively walking backwards from parent.
	getBlockForDiff := func(h uint64) (*types.Block, error) {
		// We need to find the ancestor of 'parent' at height 'h'.
		return c.GetAncestorAtHeight(parent, h)
	}

	requiredDiff, err := consensus.CalcNextRequiredDifficulty(parent, getBlockForDiff)
	if err != nil {
		return err
	}

	if block.Header.Difficulty != requiredDiff {
		return fmt.Errorf("block difficulty %d does not match required %d", block.Header.Difficulty, requiredDiff)
	}

	// 5. Validate Block Context
	if err := ValidateBlock(block, parent, c.hasher); err != nil {
		return err
	}

	// 6. Calculate Cumulative Difficulty (CDF)
	parentCDF, err := c.store.GetCumulativeDifficulty(parent.Hash)
	if err != nil {
		// Should verify consistency, assume 0 or error?
		// Genesis must have CDF.
		return fmt.Errorf("failed to get parent cdf: %v", err)
	}

	// Use big.Int to prevent overflow?
	// Prototype uses uint64, but let's be safe if we were using it.
	// We'll stick to uint64 for storage as per interface.
	newCDF := parentCDF + block.Header.Difficulty

	// 7. Save Block and CDF
	if err := c.store.SaveBlock(block); err != nil {
		return err
	}
	if err := c.store.SaveCumulativeDifficulty(block.Hash, newCDF); err != nil {
		return err
	}

	// 8. Fork Choice Rule: Check against current Tip
	tipCDF, err := c.store.GetCumulativeDifficulty(c.tip.Hash)
	if err != nil {
		return fmt.Errorf("failed to get tip cdf: %v", err)
	}

	if newCDF > tipCDF || (newCDF == tipCDF && block.Header.PrevBlockHash == c.tip.Hash) {
		// New Heaviest Chain!
		fmt.Printf("Reorganizing chain: New Tip %d (%x) beats Old Tip %d (%x)\n",
			block.Header.Height, block.Hash[:8], c.tip.Header.Height, c.tip.Hash[:8])

		return c.reorganize(block)
	}

	// Else: It's a side-chain or stale block. We just saved it.
	// Just log it.
	// fmt.Printf("Added side-chain block height=%d hash=%x (CDF: %d vs Tip: %d)\n",
	// 	block.Header.Height, block.Hash[:8], newCDF, tipCDF)

	return nil
}

// reorganize switches the active chain to the newTip.
// It assumes c.mu is locked.
func (c *Chain) reorganize(newTip *types.Block) error {
	// 1. Find Common Ancestor
	ancestor, newChain, oldChain, err := c.findForkPaths(c.tip, newTip)
	if err != nil {
		return err
	}

	_ = ancestor // We mostly used it to build paths

	// 2. Validate New Chain segments fully?
	// We validated each block as we added it (AddBlock logic).
	// We assume they are valid.

	// 3. Update Canonical Index
	// Set new path as canonical
	for _, b := range newChain {
		if err := c.store.SetCanonical(b.Header.Height, b.Hash); err != nil {
			return err
		}
	}

	// 4. Update Tip
	c.tip = newTip
	if err := c.store.SaveHead(newTip.Hash); err != nil {
		return err
	}

	// 5. Update Mempool
	if c.pool != nil {
		// oldChain txs -> return to pool
		var txsToReturn []*types.Transaction
		for _, b := range oldChain {
			txsToReturn = append(txsToReturn, b.Transactions...)
		}

		// newChain txs -> remove from pool
		var txsToRemove []*types.Transaction
		for _, b := range newChain {
			txsToRemove = append(txsToRemove, b.Transactions...)
		}

		// Process removals first? Or adds?
		// Remove txs that are now in the chain.
		c.pool.RemoveTransactions(txsToRemove)

		// Add back txs that were in old chain but NOT in new chain.
		// Note: txsToReturn might contain txs that are ALSO in newChain?
		// Usually we calculate diff.
		// Simple approach: Add all old, then Remove all new.
		// But Add might fail if exists.
		// Robust way:
		// Collect all old txs.
		// Mark txs in new chain.
		// Add only those from old that are NOT in new.

		newTxSet := make(map[types.Hash]bool)
		for _, tx := range txsToRemove {
			newTxSet[tx.ID] = true
		}

		for _, tx := range txsToReturn {
			// Skip coinbase
			if tx.Type == types.TxTypeCoinbase {
				continue
			}
			if !newTxSet[tx.ID] {
				// Try to add back. Ignore errors (e.g. valid checks might fail now)
				_ = c.pool.AddTransaction(tx)
			}
		}
	}

	// 6. Notify Subscribers
	c.notifySubscribers(newTip)

	return nil
}

// findForkPaths finds the common ancestor and the paths from it to the tips.
// Returns ancestor, newChain (ascending from ancestor+1 to newTip), oldChain (ascending from ancestor+1 to oldTip).
func (c *Chain) findForkPaths(oldTip, newTip *types.Block) (*types.Block, []*types.Block, []*types.Block, error) {
	var newChain []*types.Block
	var oldChain []*types.Block

	currNew := newTip
	currOld := oldTip

	// Synchronize heights
	for currNew.Header.Height > currOld.Header.Height {
		newChain = append(newChain, currNew)
		prev, err := c.store.GetBlockByHash(currNew.Header.PrevBlockHash)
		if err != nil {
			return nil, nil, nil, err
		}
		currNew = prev
	}

	for currOld.Header.Height > currNew.Header.Height {
		oldChain = append(oldChain, currOld)
		prev, err := c.store.GetBlockByHash(currOld.Header.PrevBlockHash)
		if err != nil {
			return nil, nil, nil, err
		}
		currOld = prev
	}

	// Walk back together until match
	for currNew.Hash != currOld.Hash {
		newChain = append(newChain, currNew)
		oldChain = append(oldChain, currOld)

		prevNew, err := c.store.GetBlockByHash(currNew.Header.PrevBlockHash)
		if err != nil {
			return nil, nil, nil, err
		}
		prevOld, err := c.store.GetBlockByHash(currOld.Header.PrevBlockHash)
		if err != nil {
			return nil, nil, nil, err
		}
		currNew = prevNew
		currOld = prevOld
	}

	// Reverse slices to be ascending (Ancestor -> Tip)
	reverseBlocks(newChain)
	reverseBlocks(oldChain)

	return currNew, newChain, oldChain, nil
}

func reverseBlocks(blocks []*types.Block) {
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
	}
}

// GetAncestorAtHeight finds the ancestor of 'startBlock' at specific 'height'.
// Used for difficulty calculation on forks.
func (c *Chain) GetAncestorAtHeight(startBlock *types.Block, height uint64) (*types.Block, error) {
	if height > startBlock.Header.Height {
		return nil, errors.New("target height is higher than start block")
	}

	curr := startBlock
	for curr.Header.Height > height {
		prev, err := c.store.GetBlockByHash(curr.Header.PrevBlockHash)
		if err != nil {
			return nil, err
		}
		curr = prev
	}

	if curr.Header.Height != height {
		return nil, fmt.Errorf("failed to navigate to height %d", height)
	}

	return curr, nil
}

// GetBlockByHeight returns the block from the CANONICAL chain.
func (c *Chain) GetBlockByHeight(height uint64) (*types.Block, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. If height > Tip, impossible
	if c.tip == nil || height > c.tip.Header.Height {
		return nil, ErrBlockNotFound
	}

	// 2. Fetch from Store using Canonical Index
	// Optimization: If asking for Tip, return Tip
	if c.tip.Header.Height == height {
		return c.tip, nil
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
// by scanning the entire CANONICAL blockchain history.
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
		// Note: GetBlockByHeight here uses the Store's GetBlockByHeight,
		// which uses the canonical index map.
		// Since we hold the lock, and store calls are safe, we just need to avoid recursive locking
		// if `c.GetBlockByHeight` was called. But we can call `c.store.GetBlockByHeight`.

		block, err := c.store.GetBlockByHeight(h)
		if err != nil {
			if err == ErrBlockNotFoundInStore {
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
				totalDebit := tx.Amount + tx.Fee
				balance -= totalDebit
				nonce++
			}
		}
	}

	return balance, nonce, nil
}
