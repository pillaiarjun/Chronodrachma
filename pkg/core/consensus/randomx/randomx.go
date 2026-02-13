//go:build cgo && randomx

package randomx

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../third_party/RandomX/src
#cgo LDFLAGS: -L${SRCDIR}/../../../../third_party/RandomX/build -lrandomx -lstdc++ -lm
#include "randomx.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Flags wraps randomx_flags.
type Flags C.randomx_flags

const (
	FlagDefault    Flags = C.RANDOMX_FLAG_DEFAULT
	FlagJIT        Flags = C.RANDOMX_FLAG_JIT
	FlagSecure     Flags = C.RANDOMX_FLAG_SECURE
	FlagHardAES    Flags = C.RANDOMX_FLAG_HARD_AES
	FlagFullMem    Flags = C.RANDOMX_FLAG_FULL_MEM
	FlagLargePages Flags = C.RANDOMX_FLAG_LARGE_PAGES
)

// GetFlags returns the recommended flags for the current CPU.
func GetFlags() Flags {
	return Flags(C.randomx_get_flags())
}

// Cache wraps a randomx_cache pointer.
type Cache struct {
	ptr *C.randomx_cache
}

// AllocCache allocates a new RandomX cache with the given flags.
func AllocCache(flags Flags) (*Cache, error) {
	ptr := C.randomx_alloc_cache(C.randomx_flags(flags))
	if ptr == nil {
		return nil, errors.New("randomx: failed to allocate cache")
	}
	return &Cache{ptr: ptr}, nil
}

// Init seeds the cache with the given key.
func (c *Cache) Init(key []byte) {
	if len(key) == 0 {
		key = []byte{0}
	}
	C.randomx_init_cache(c.ptr, unsafe.Pointer(&key[0]), C.size_t(len(key)))
}

// Release frees the cache memory.
func (c *Cache) Release() {
	if c.ptr != nil {
		C.randomx_release_cache(c.ptr)
		c.ptr = nil
	}
}

// Dataset wraps a randomx_dataset pointer.
type Dataset struct {
	ptr *C.randomx_dataset
}

// AllocDataset allocates a new RandomX dataset with the given flags.
func AllocDataset(flags Flags) (*Dataset, error) {
	ptr := C.randomx_alloc_dataset(C.randomx_flags(flags))
	if ptr == nil {
		return nil, errors.New("randomx: failed to allocate dataset")
	}
	return &Dataset{ptr: ptr}, nil
}

// DatasetItemCount returns the number of items needed for the full dataset.
func DatasetItemCount() uint64 {
	return uint64(C.randomx_dataset_item_count())
}

// Init populates the dataset from the cache.
func (d *Dataset) Init(cache *Cache, startItem, itemCount uint64) {
	C.randomx_init_dataset(d.ptr, cache.ptr, C.ulong(startItem), C.ulong(itemCount))
}

// Release frees the dataset memory.
func (d *Dataset) Release() {
	if d.ptr != nil {
		C.randomx_release_dataset(d.ptr)
		d.ptr = nil
	}
}

// VM wraps a randomx_vm pointer.
type VM struct {
	ptr *C.randomx_vm
}

// CreateVM creates a new RandomX VM.
func CreateVM(flags Flags, cache *Cache, dataset *Dataset) (*VM, error) {
	var cachePtr *C.randomx_cache
	var datasetPtr *C.randomx_dataset
	if cache != nil {
		cachePtr = cache.ptr
	}
	if dataset != nil {
		datasetPtr = dataset.ptr
	}

	ptr := C.randomx_create_vm(C.randomx_flags(flags), cachePtr, datasetPtr)
	if ptr == nil {
		return nil, errors.New("randomx: failed to create VM")
	}
	return &VM{ptr: ptr}, nil
}

// CalculateHash computes the RandomX hash of the input data.
func (vm *VM) CalculateHash(input []byte) [32]byte {
	var output [32]byte
	if len(input) == 0 {
		input = []byte{0}
	}
	C.randomx_calculate_hash(vm.ptr, unsafe.Pointer(&input[0]), C.size_t(len(input)), unsafe.Pointer(&output[0]))
	return output
}

// Destroy releases the VM resources.
func (vm *VM) Destroy() {
	if vm.ptr != nil {
		C.randomx_destroy_vm(vm.ptr)
		vm.ptr = nil
	}
}
