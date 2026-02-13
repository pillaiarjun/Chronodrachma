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
	Outbound bool      // True if we initiated the connection
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
		// Handle handshake logic here (e.g., sync chain if behind)
	
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
		// Add to mempool (not implemented in Phase I/II yet, just log)
	}
}

// Send sends a message to the peer.
func (p *Peer) Send(msg Message) error {
	return EncodeMessage(p.Conn, msg)
}
