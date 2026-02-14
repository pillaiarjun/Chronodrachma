package p2p

import (
	"log"
	"net"
	"sync"
)

// Peer represents a connected remote node.
type Peer struct {
	Conn     net.Conn
	Server   *Server
	Outbound bool // True if we initiated the connection
	wg       sync.WaitGroup
	quit     chan struct{}
}

// NewPeer creates a new peer instance.
func NewPeer(conn net.Conn, server *Server, outbound bool) *Peer {
	return &Peer{
		Conn:     conn,
		Server:   server,
		Outbound: outbound,
		quit:     make(chan struct{}),
	}
}

// Start begins the peer's read/write loops.
func (p *Peer) Start() {
	p.wg.Add(1)
	go p.readLoop()
}

// Stop closes the peer connection.
func (p *Peer) Stop() {
	close(p.quit)
	p.Conn.Close()
	p.wg.Wait()
}

// readLoop continuously reads messages from the connection.
func (p *Peer) readLoop() {
	defer p.wg.Done()
	defer p.Server.RemovePeer(p)

	for {
		select {
		case <-p.quit:
			return
		default:
			msg, err := DecodeMessage(p.Conn)
			if err != nil {
				log.Printf("Read error from %s: %v", p.Conn.RemoteAddr(), err)
				return
			}
			p.handleMessage(msg)
		}
	}
}

func (p *Peer) handleMessage(msg Message) {
	switch m := msg.(type) {
	case *MsgVersion:
		log.Printf("Received Version from %s: v%d, height=%d", p.Conn.RemoteAddr(), m.Version, m.BlockHeight)
		// Check if we are behind
		localHeight := p.Server.Chain.Height()
		if m.BlockHeight > localHeight {
			log.Printf("We are behind peer %s (local=%d, peer=%d). Requesting sync.", p.Conn.RemoteAddr(), localHeight, m.BlockHeight)
			p.Send(&MsgGetBlocks{FromHeight: localHeight + 1})
		}

	case *MsgGetBlocks:
		// Peer wants blocks
		log.Printf("Received GetBlocks from %s starting at %d", p.Conn.RemoteAddr(), m.FromHeight)
		blocks, err := p.Server.Chain.GetBlocksRange(m.FromHeight, 50)
		if err != nil {
			log.Printf("Failed to get blocks for peer: %v", err)
			return
		}
		if len(blocks) > 0 {
			log.Printf("Sending %d blocks to %s", len(blocks), p.Conn.RemoteAddr())
			p.Send(&MsgBlocks{Blocks: blocks})
		}

	case *MsgBlocks:
		// Peer sent us blocks
		log.Printf("Received %d blocks from %s", len(m.Blocks), p.Conn.RemoteAddr())
		count := 0
		for _, block := range m.Blocks {
			if err := p.Server.Chain.AddBlock(block); err != nil {
				log.Printf("Sync: Failed to add block %d (%x): %v", block.Header.Height, block.Hash[:8], err)
				// Stop processing batch on error? Or continue?
				// Usually stop if parent missing.
				break
			}
			count++
		}
		log.Printf("Sync: Added %d/%d blocks.", count, len(m.Blocks))

		// If we processed all successfully, and we are not yet at tip, request more?
		// We can check if the last block added is still behind known peer height?
		// But peer height from Version might be stale.
		// Simple logic: if we received a full batch (50), likely more available.
		if count == len(m.Blocks) && count == 50 {
			lastHeight := m.Blocks[len(m.Blocks)-1].Header.Height
			log.Printf("Sync: Requesting more blocks from %d...", lastHeight+1)
			p.Send(&MsgGetBlocks{FromHeight: lastHeight + 1})
		}

	case *MsgBlock:
		log.Printf("Received Block from %s: %x", p.Conn.RemoteAddr(), m.Block.Hash)
		if err := p.Server.Chain.AddBlock(m.Block); err != nil {
			log.Printf("Failed to add block: %v", err)
		} else {
			log.Printf("Added block %x from peer, broadcasting...", m.Block.Hash)
			p.Server.Broadcast(m) // Gossip
		}

	case *MsgTx:
		log.Printf("Received Tx from %s: %x", p.Conn.RemoteAddr(), m.Tx.ID)
		if err := p.Server.Mempool.AddTransaction(m.Tx); err != nil {
			// If already exists, don't gossip back
			if err.Error() != "transaction already in mempool" {
				log.Printf("Failed to add transaction: %v", err)
			}
		} else {
			log.Printf("Added tx %x to mempool, broadcasting...", m.Tx.ID)
			p.Server.Broadcast(m)
		}
	}
}

// Send sends a message to the peer.
func (p *Peer) Send(msg Message) error {
	return EncodeMessage(p.Conn, msg)
}
