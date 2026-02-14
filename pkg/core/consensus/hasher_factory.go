//go:build !randomx

package consensus

import (
	"log"
)

// NewHasher returns the appropriate Hasher implementation based on build tags.
// Without 'randomx' tag, this returns the SHA256Hasher (fast, for testing/prototype).
func NewHasher(seed []byte, fullDataset bool) (Hasher, error) {
	log.Println("WARNING: RandomX build tag not found. Using SHA256 Hasher (Prototype Mode).")
	log.Println("To enable RandomX, build with: go build -tags randomx")
	return NewSHA256Hasher(), nil
}
