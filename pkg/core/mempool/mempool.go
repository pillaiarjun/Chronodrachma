package mempool

import (
	"crypto/ed25519"
	"errors"
	"sort"
	"sync"
	"time" // Added missing import for time.Now()

	"github.com/chronodrachma/chrd/pkg/core/blockchain"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

var (
	ErrTxAlreadyInMempool = errors.New("transaction already in mempool")
	ErrInvalidSignature   = errors.New("invalid transaction signature")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidNonce       = errors.New("invalid nonce")
	ErrTxTooOld           = errors.New("transaction timestamp too old")
)

// Mempool manages pending transactions.
type Mempool struct {
	mu    sync.RWMutex
	txs   map[types.Hash]*types.Transaction
	chain *blockchain.Chain
}

// NewMempool creates a new transaction pool.
func NewMempool(chain *blockchain.Chain) *Mempool {
	return &Mempool{
		txs:   make(map[types.Hash]*types.Transaction),
		chain: chain,
	}
}

// Size returns the number of transactions in the pool.
func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.txs)
}

// AddTransaction validates and adds a transaction to the pool.
func (mp *Mempool) AddTransaction(tx *types.Transaction) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// 1. Check existence
	if _, ok := mp.txs[tx.ID]; ok {
		return ErrTxAlreadyInMempool
	}

	// 2. Validate basics
	// (Check basic structure, non-zero amount, etc - omitted for prototype)

	// 3. Verify Signature
	// We assume From address is the Ed25519 public key (or hash of it).
	// For this prototype, let's assume From IS the public key (32 bytes).
	// Address == PubKey.
	// If Address is hash of PubKey, we need PubKey in Tx logic. 
	// The `types.Hash` is 32 bytes. Ed25519 PubKey is 32 bytes.
	// So we treat `tx.From` as the PubKey.
	if !ed25519.Verify(tx.From[:], tx.Serialize(), tx.Signature) {
		return ErrInvalidSignature
	}

	// 4. Validate State (Balance & Nonce)
	balance, currentNonce, err := mp.chain.GetAccountState(tx.From)
	if err != nil {
		return err
	}

	// Nonce Check:
	// For simple prototype, we require tx.Nonce == currentNonce.
	// (Strict ordering, no gaps).
	// NOTE: If we have multiple txs from same sender in mempool, we need to account for them.
	// We should calculate "pending nonce".
	pendingNonce := currentNonce
	pendingDebit := types.Amount(0)

	for _, pending := range mp.txs {
		if pending.From == tx.From {
			if pending.Nonce >= pendingNonce {
				pendingNonce = pending.Nonce + 1
			}
			pendingDebit += pending.Amount + pending.Fee
		}
	}

	if tx.Nonce != pendingNonce {
		// Log/Debug: expected pendingNonce, got tx.Nonce
		return ErrInvalidNonce
	}

	// Balance Check:
	// Balance must cover all pending spends + this one.
	if balance < pendingDebit+tx.Amount+tx.Fee {
		return ErrInsufficientFunds
	}

	mp.txs[tx.ID] = tx
	return nil
}

// GetPendingTransactions returns a list of transactions to mine.
// Simple FIFO or fee-based ordering.
func (mp *Mempool) GetPendingTransactions(maxCount int) []*types.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	result := make([]*types.Transaction, 0, maxCount)
	
	// Convert map to slice for sorting
	allTxs := make([]*types.Transaction, 0, len(mp.txs))
	for _, tx := range mp.txs {
		allTxs = append(allTxs, tx)
	}

	// Sort by Fee (high to low), then Nonce (low to high).
	// For prototype, just sort by Nonce to ensure order?
	// Or just Timestamp.
	sort.Slice(allTxs, func(i, j int) bool {
		// Just FIFO by timestamp for now
		return allTxs[i].Timestamp.Before(allTxs[j].Timestamp)
	})

	for _, tx := range allTxs {
		if len(result) >= maxCount {
			break
		}
		result = append(result, tx)
	}

	return result
}

// RemoveTransactions removes mined transactions from the pool.
func (mp *Mempool) RemoveTransactions(txs []*types.Transaction) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for _, tx := range txs {
		delete(mp.txs, tx.ID)
	}
}
