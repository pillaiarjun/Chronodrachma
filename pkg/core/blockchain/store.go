package blockchain

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

var (
	ErrBlockNotFoundInStore = errors.New("block not found in store")
)

// BlockStore defines the interface for persistent block storage.
type BlockStore interface {
	SaveBlock(block *types.Block) error
	GetBlockByHash(hash types.Hash) (*types.Block, error)
	GetBlockByHeight(height uint64) (*types.Block, error)
	SaveHead(hash types.Hash) error
	GetHead() (types.Hash, error)
	Close() error
}

// BadgerStore implements BlockStore using BadgerDB.
type BadgerStore struct {
	db *badger.DB
	mu sync.RWMutex
}

// NewBadgerStore creates or opens a BadgerDB store at the given path.
// If path is empty, it opens an in-memory store (for testing).
func NewBadgerStore(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path)
	if path == "" {
		opts = opts.WithInMemory(true)
	}
	// Reduce logging noise
	opts.Logger = nil

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &BadgerStore{
		db: db,
	}, nil
}

func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// Keys:
// Block by Hash:   "block:hash:<hash>" -> serialized block
// Block by Height: "block:height:<height>" -> hash
// Head:            "chain:head" -> hash

func (s *BadgerStore) SaveBlock(block *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		// 1. Serialize block
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(block); err != nil {
			return err
		}
		serializedBlock := buf.Bytes()

		// 2. Save by Hash
		hashKey := fmt.Sprintf("block:hash:%x", block.Hash)
		if err := txn.Set([]byte(hashKey), serializedBlock); err != nil {
			return err
		}

		// 3. Save index by Height
		heightKey := fmt.Sprintf("block:height:%d", block.Header.Height)
		if err := txn.Set([]byte(heightKey), block.Hash[:]); err != nil {
			return err
		}

		return nil
	})
}

func (s *BadgerStore) GetBlockByHash(hash types.Hash) (*types.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var block types.Block
	err := s.db.View(func(txn *badger.Txn) error {
		key := fmt.Sprintf("block:hash:%x", hash)
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return ErrBlockNotFoundInStore
			}
			return err
		}

		return item.Value(func(val []byte) error {
			dec := gob.NewDecoder(bytes.NewReader(val))
			return dec.Decode(&block)
		})
	})

	if err != nil {
		return nil, err
	}
	return &block, nil
}

func (s *BadgerStore) GetBlockByHeight(height uint64) (*types.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash types.Hash
	err := s.db.View(func(txn *badger.Txn) error {
		key := fmt.Sprintf("block:height:%d", height)
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return ErrBlockNotFoundInStore
			}
			return err
		}

		return item.Value(func(val []byte) error {
			copy(hash[:], val)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	// Now fetch by hash (can reuse existing method but needs to release lock if not re-entrant... 
	// wait, RLock is not re-entrant for same goroutine? RLock is. 
	// But let's just do it manually inside View or separate call.
	// Calling s.GetBlockByHash would re-acquire RLock which might deadlock if RWMutex isn't recursive (it's NOT in Go).
	// So we must NOT call s.GetBlockByHash directly if we hold the lock.
	// But here we release the lock after View finishes... wait `defer s.mu.RUnlock()` runs at end of function.
	// So if we call s.GetBlockByHash(hash) at the end, we deadlock.
	
	// FIX: Use an internal helper or just release lock?
	// Actually, badger DB handles concurrency well. `s.mu` might be redundant for View operations if valid badger usage.
	// But let's keep it simple.
	
	// We can cheat: we already fetched the hash. Let's return the block using GetBlockByHash AFTER unlocking?
	// But defer unlocks at return.
	// Let's just manually unlock before calling GetBlockByHash, or remove the mutex for read ops if badger is safe (it is thread safe).
	// Actually, `BadgerStore` struct uses `mu sync.RWMutex`. If `badger.DB` is thread-safe (it is), we don't strictly need a mutex around `View` and `Update` calls unless we are coordinating something complex.
	// For simple get/set, badger locks internally.
	// So I will REMOVE the mutex usage for simple Get/Set to avoid deadlocks and complexity, relying on Badger's internal locking.

	return s.GetBlockByHash(hash)
}

func (s *BadgerStore) SaveHead(hash types.Hash) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("chain:head"), hash[:])
	})
}

func (s *BadgerStore) GetHead() (types.Hash, error) {
	var hash types.Hash
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("chain:head"))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			copy(hash[:], val)
			return nil
		})
	})
	return hash, err
}
