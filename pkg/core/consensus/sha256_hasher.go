package consensus

import (
	"crypto/sha256"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

// SHA256Hasher implements Hasher using double-SHA256.
// Used in tests to avoid requiring the RandomX CGO build.
type SHA256Hasher struct{}

var _ Hasher = (*SHA256Hasher)(nil)

// NewSHA256Hasher returns a new SHA256Hasher.
func NewSHA256Hasher() *SHA256Hasher {
	return &SHA256Hasher{}
}

// Hash computes SHA256(SHA256(headerBytes)).
func (h *SHA256Hasher) Hash(headerBytes []byte) (types.Hash, error) {
	first := sha256.Sum256(headerBytes)
	second := sha256.Sum256(first[:])
	return second, nil
}

// Close is a no-op for SHA256Hasher.
func (h *SHA256Hasher) Close() {}
