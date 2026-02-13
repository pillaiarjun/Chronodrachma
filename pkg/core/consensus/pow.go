package consensus

import "github.com/chronodrachma/chrd/pkg/core/types"

// Hasher computes Proof-of-Work hashes. Implementations include RandomXHasher
// (production, CGO) and SHA256Hasher (testing, pure Go).
type Hasher interface {
	// Hash computes the PoW hash of the given block header bytes.
	Hash(headerBytes []byte) (types.Hash, error)

	// Close releases any resources held by the hasher.
	Close()
}

// MeetsDifficulty checks whether a PoW hash satisfies the given difficulty target.
// Difficulty is the number of leading zero bits required.
// difficulty=0 means any hash passes; difficulty=8 means first byte must be 0x00.
func MeetsDifficulty(powHash types.Hash, difficulty uint64) bool {
	if difficulty == 0 {
		return true
	}
	if difficulty > 256 {
		return false
	}

	fullBytes := difficulty / 8
	remainBits := difficulty % 8

	// Check full zero bytes.
	for i := uint64(0); i < fullBytes; i++ {
		if powHash[i] != 0 {
			return false
		}
	}

	// Check remaining bits in the next byte.
	if remainBits > 0 && fullBytes < 32 {
		mask := byte(0xFF << (8 - remainBits))
		if powHash[fullBytes]&mask != 0 {
			return false
		}
	}

	return true
}
