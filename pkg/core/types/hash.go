package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashSize is the length of all hashes in bytes.
const HashSize = 32

// Hash represents a 32-byte hash (SHA-256 for block ID, RandomX for PoW).
type Hash [HashSize]byte

// ZeroHash is the all-zeroes hash, used as the PrevBlockHash of the genesis block.
var ZeroHash Hash

// HashFromBytes creates a Hash from a byte slice. Returns error if len != 32.
func HashFromBytes(b []byte) (Hash, error) {
	if len(b) != HashSize {
		return Hash{}, fmt.Errorf("hash must be %d bytes, got %d", HashSize, len(b))
	}
	var h Hash
	copy(h[:], b)
	return h, nil
}

// HashFromHex parses a hex-encoded string into a Hash.
func HashFromHex(s string) (Hash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return Hash{}, fmt.Errorf("invalid hex: %w", err)
	}
	return HashFromBytes(b)
}

// Bytes returns the hash as a byte slice.
func (h Hash) Bytes() []byte {
	return h[:]
}

// Hex returns the lowercase hex-encoded string.
func (h Hash) Hex() string {
	return hex.EncodeToString(h[:])
}

// String implements fmt.Stringer.
func (h Hash) String() string {
	return h.Hex()
}

// IsZero returns true if every byte is 0x00.
func (h Hash) IsZero() bool {
	return h == ZeroHash
}

// ComputeSHA256 computes SHA-256 of arbitrary data and returns it as a Hash.
func ComputeSHA256(data []byte) Hash {
	return sha256.Sum256(data)
}
