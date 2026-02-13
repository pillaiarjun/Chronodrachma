//go:build cgo && randomx

package randomx

import (
	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/types"
)

// RandomXHasher implements consensus.Hasher using the RandomX algorithm.
type RandomXHasher struct {
	cache   *Cache
	dataset *Dataset
	vm      *VM
	flags   Flags
}

var _ consensus.Hasher = (*RandomXHasher)(nil)

// NewRandomXHasher initializes the RandomX hasher.
// seedHash seeds the cache. If fullDataset is true, the full ~2GB dataset
// is allocated (for miners). Otherwise only the cache is used (for validators).
func NewRandomXHasher(seedHash []byte, fullDataset bool) (*RandomXHasher, error) {
	flags := GetFlags()

	cache, err := AllocCache(flags)
	if err != nil {
		return nil, err
	}
	cache.Init(seedHash)

	var dataset *Dataset
	if fullDataset {
		flags |= FlagFullMem
		dataset, err = AllocDataset(flags)
		if err != nil {
			cache.Release()
			return nil, err
		}
		dataset.Init(cache, 0, DatasetItemCount())
	}

	vm, err := CreateVM(flags, cache, dataset)
	if err != nil {
		if dataset != nil {
			dataset.Release()
		}
		cache.Release()
		return nil, err
	}

	return &RandomXHasher{
		cache:   cache,
		dataset: dataset,
		vm:      vm,
		flags:   flags,
	}, nil
}

// Hash computes the RandomX hash of the given header bytes.
func (h *RandomXHasher) Hash(headerBytes []byte) (types.Hash, error) {
	result := h.vm.CalculateHash(headerBytes)
	hash, err := types.HashFromBytes(result[:])
	if err != nil {
		return types.Hash{}, err
	}
	return hash, nil
}

// Close releases the VM, dataset, and cache.
func (h *RandomXHasher) Close() {
	if h.vm != nil {
		h.vm.Destroy()
		h.vm = nil
	}
	if h.dataset != nil {
		h.dataset.Release()
		h.dataset = nil
	}
	if h.cache != nil {
		h.cache.Release()
		h.cache = nil
	}
}
