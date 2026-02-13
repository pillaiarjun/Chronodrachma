package types

import (
	"encoding/binary"
	"time"
)

// BlockHeader contains all metadata for a block.
type BlockHeader struct {
	Version       uint32
	Height        uint64
	Timestamp     time.Time
	PrevBlockHash Hash
	MerkleRoot    Hash
	Difficulty    uint64
	Nonce         uint64
}

// Serialize returns a deterministic 100-byte encoding of the header.
// Field order: Version(4) || Height(8) || Timestamp(8) || PrevBlockHash(32) ||
//
//	MerkleRoot(32) || Difficulty(8) || Nonce(8)
func (h *BlockHeader) Serialize() []byte {
	buf := make([]byte, 100)
	binary.BigEndian.PutUint32(buf[0:4], h.Version)
	binary.BigEndian.PutUint64(buf[4:12], h.Height)
	binary.BigEndian.PutUint64(buf[12:20], uint64(h.Timestamp.Unix()))
	copy(buf[20:52], h.PrevBlockHash[:])
	copy(buf[52:84], h.MerkleRoot[:])
	binary.BigEndian.PutUint64(buf[84:92], h.Difficulty)
	binary.BigEndian.PutUint64(buf[92:100], h.Nonce)
	return buf
}

// Block is a complete block: header + body (transactions).
type Block struct {
	Header       BlockHeader
	Transactions []*Transaction
	Hash         Hash // SHA-256 of the serialized header (block identity).
	PowHash      Hash // PoW hash of the serialized header (proves work).
}

// ComputeHash computes the SHA-256 of the serialized header.
func (b *Block) ComputeHash() Hash {
	return ComputeSHA256(b.Header.Serialize())
}

// ComputeMerkleRoot computes the SHA-256 Merkle tree root of the transaction IDs.
func ComputeMerkleRoot(txs []*Transaction) Hash {
	if len(txs) == 0 {
		return ZeroHash
	}

	hashes := make([]Hash, len(txs))
	for i, tx := range txs {
		hashes[i] = tx.ID
	}

	for len(hashes) > 1 {
		var next []Hash
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := append(hashes[i].Bytes(), hashes[i+1].Bytes()...)
				next = append(next, ComputeSHA256(combined))
			} else {
				// Odd element: duplicate it.
				combined := append(hashes[i].Bytes(), hashes[i].Bytes()...)
				next = append(next, ComputeSHA256(combined))
			}
		}
		hashes = next
	}

	return hashes[0]
}
