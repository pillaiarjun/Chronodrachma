package types

import (
	"encoding/binary"
	"time"
)

// TxType distinguishes coinbase transactions from regular transfers.
type TxType uint8

const (
	TxTypeCoinbase TxType = 0
	TxTypeTransfer TxType = 1
)

// Transaction represents a single value transfer on the CHRD chain.
type Transaction struct {
	ID        Hash
	Type      TxType
	Timestamp time.Time
	From      Hash   // ZeroHash for coinbase.
	To        Hash   // Recipient address.
	Amount    Amount // Value in chronos.
	Fee       Amount // Transaction fee (0 for coinbase).
	Nonce     uint64 // Sender's sequential nonce (block height for coinbase).
	Signature []byte // Ed25519 signature (nil for Phase I).
}

// Serialize returns a deterministic byte encoding of the transaction fields
// (excluding ID and Signature) for hashing.
func (tx *Transaction) Serialize() []byte {
	// Type(1) + Timestamp(8) + From(32) + To(32) + Amount(8) + Fee(8) + Nonce(8) = 97 bytes
	buf := make([]byte, 97)
	buf[0] = byte(tx.Type)
	binary.BigEndian.PutUint64(buf[1:9], uint64(tx.Timestamp.Unix()))
	copy(buf[9:41], tx.From[:])
	copy(buf[41:73], tx.To[:])
	binary.BigEndian.PutUint64(buf[73:81], uint64(tx.Amount))
	binary.BigEndian.PutUint64(buf[81:89], uint64(tx.Fee))
	binary.BigEndian.PutUint64(buf[89:97], tx.Nonce)
	return buf
}

// ComputeID computes the SHA-256 hash of the serialized transaction fields.
func (tx *Transaction) ComputeID() Hash {
	return ComputeSHA256(tx.Serialize())
}

// NewCoinbaseTx creates a coinbase transaction paying the block reward to the miner.
func NewCoinbaseTx(minerAddress Hash, blockHeight uint64) *Transaction {
	tx := &Transaction{
		Type:      TxTypeCoinbase,
		Timestamp: time.Now(),
		From:      ZeroHash,
		To:        minerAddress,
		Amount:    BlockReward,
		Fee:       0,
		Nonce:     blockHeight,
	}
	tx.ID = tx.ComputeID()
	return tx
}
