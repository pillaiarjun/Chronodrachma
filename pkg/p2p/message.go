package p2p

import (
	"encoding/gob"
	"fmt"
	"io"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

// MessageType identifies the type of P2P message.
type MessageType byte

const (
	MsgTypeVersion MessageType = 0x01
	MsgTypeBlock   MessageType = 0x02
	MsgTypeTx      MessageType = 0x03
)

// Message is the generic interface for all P2P messages.
type Message interface {
	Type() MessageType
}

// MsgVersion is the initial handshake message.
type MsgVersion struct {
	Version     uint32
	BlockHeight uint64
	From        string
}

func (m *MsgVersion) Type() MessageType { return MsgTypeVersion }

// MsgBlock broadcasts a new block.
type MsgBlock struct {
	Block *types.Block
}

func (m *MsgBlock) Type() MessageType { return MsgTypeBlock }

// MsgTx broadcasts a new transaction.
type MsgTx struct {
	Tx *types.Transaction
}

func (m *MsgTx) Type() MessageType { return MsgTypeTx }

// EncodeMessage writes a message to the writer using Gob encoding.
// Format: [Type(1)][Payload(Gob)]
func EncodeMessage(w io.Writer, msg Message) error {
	// Write Type
	if _, err := w.Write([]byte{byte(msg.Type())}); err != nil {
		return err
	}

	// Write Payload
	enc := gob.NewEncoder(w)
	return enc.Encode(msg)
}

// DecodeMessage reads a message from the reader.
func DecodeMessage(r io.Reader) (Message, error) {
	// Read Type
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return nil, err
	}

	var msg Message
	switch MessageType(typeBuf[0]) {
	case MsgTypeVersion:
		msg = &MsgVersion{}
	case MsgTypeBlock:
		msg = &MsgBlock{}
	case MsgTypeTx:
		msg = &MsgTx{}
	default:
		return nil, fmt.Errorf("unknown message type: 0x%x", typeBuf[0])
	}

	// Read Payload
	dec := gob.NewDecoder(r)
	if err := dec.Decode(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func init() {
	// Register types for Gob
	gob.Register(&MsgVersion{})
	gob.Register(&MsgBlock{})
	gob.Register(&MsgTx{})
	gob.Register(types.Block{})
	gob.Register(types.Transaction{})
	gob.Register(types.BlockHeader{})
}
