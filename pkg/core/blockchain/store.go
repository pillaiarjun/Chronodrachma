package blockchain

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"

	"github.com/chronodrachma/chrd/pkg/core/types"
	"github.com/dgraph-io/badger/v4"
)

var (
	ErrBlockNotFoundInStore = errors.New("block not found in store")
)

// BlockStore defines the interface for persistent block storage.
type BlockStore interface {
	// SaveBlock saves the block data but does NOT update the canonical chain index.
	SaveBlock(block *types.Block) error

	GetBlockByHash(hash types.Hash) (*types.Block, error)
	GetBlockByHeight(height uint64) (*types.Block, error)

	// SetCanonical maps a height to a block hash, defining the canonical chain.
	SetCanonical(height uint64, hash types.Hash) error

	SaveHead(hash types.Hash) error
	GetHead() (types.Hash, error)

	// Cumulative Difficulty (CDF) storage
	SaveCumulativeDifficulty(hash types.Hash, cd uint64) error
	GetCumulativeDifficulty(hash types.Hash) (uint64, error)

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
// CDF:             "block:cdf:<hash>" -> uint64

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

		// NOTE: We do NOT save the height index here anymore.
		// That is done explicitly via SetCanonical when part of the main chain.
		return nil
	})
}

func (s *BadgerStore) SetCanonical(height uint64, hash types.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		heightKey := fmt.Sprintf("block:height:%d", height)
		return txn.Set([]byte(heightKey), hash[:])
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
	// Unlocking is handled after fetching hash to call GetBlockByHash
	// But verify recursion safety...
	// We will just read the hash in the transaction, then release lock, then call GetBlockByHash.
	s.mu.RUnlock()

	// Re-acquire lock for reading hash
	s.mu.RLock()
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
	s.mu.RUnlock()

	if err != nil {
		return nil, err
	}

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

func (s *BadgerStore) SaveCumulativeDifficulty(hash types.Hash, cd uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		key := fmt.Sprintf("block:cdf:%x", hash)
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, cd)
		return txn.Set([]byte(key), buf)
	})
}

func (s *BadgerStore) GetCumulativeDifficulty(hash types.Hash) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cd uint64
	err := s.db.View(func(txn *badger.Txn) error {
		key := fmt.Sprintf("block:cdf:%x", hash)
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			if len(val) < 8 {
				return errors.New("invalid cdf value length")
			}
			cd = binary.LittleEndian.Uint64(val)
			return nil
		})
	})
	return cd, err
}
