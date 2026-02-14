//go:build randomx

package consensus

import (
	"log"

	"github.com/chronodrachma/chrd/pkg/core/consensus/randomx"
)

// NewHasher returns the appropriate Hasher implementation based on build tags.
// With 'randomx' tag, this returns the RandomXHasher.
func NewHasher(seed []byte, fullDataset bool) (Hasher, error) {
	log.Println("Initializing RandomX Hasher...")
	if fullDataset {
		log.Println("RandomX: Allocating Full Dataset (Mining Mode). This may take a few seconds...")
	} else {
		log.Println("RandomX: Allocating Cache (Validation Mode).")
	}

	return randomx.NewRandomXHasher(seed, fullDataset)
}
