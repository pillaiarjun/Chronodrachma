package p2p

import (
	"log"
	"net"
	"sync"

	"github.com/chronodrachma/chrd/pkg/core/blockchain"
)

// Server manages the P2P network.
type Server struct {
	Config     ServerConfig
	Chain      *blockchain.Chain
	peers      map[string]*Peer
	peerMu     sync.RWMutex
	listener   net.Listener
	quit       chan struct{}
}

type ServerConfig struct {
	ListenAddr string
	SeedNodes  []string
}

func NewServer(config ServerConfig, chain *blockchain.Chain) *Server {
	return &Server{
		Config: config,
		Chain:  chain,
		peers:  make(map[string]*Peer),
		quit:   make(chan struct{}),
	}
}

func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.Config.ListenAddr)
	if err != nil {
		return err
	}
	s.listener = l
	log.Printf("P2P server listening on %s", s.Config.ListenAddr)

	// Connect to seeds
	for _, seed := range s.Config.SeedNodes {
		go s.Connect(seed)
	}

	go s.acceptLoop()
	return nil
}

func (s *Server) Connect(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Printf("Failed to connect to seed %s: %v", addr, err)
		return
	}
	s.addPeer(conn, true)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}
		s.addPeer(conn, false)
	}
}

func (s *Server) addPeer(conn net.Conn, outbound bool) {
	s.peerMu.Lock()
	defer s.peerMu.Unlock()

	addr := conn.RemoteAddr().String()
	if _, ok := s.peers[addr]; ok {
		conn.Close()
		return
	}

	p := NewPeer(conn, s, outbound)
	s.peers[addr] = p
	p.Start()

	// Send handshake
	p.Send(&MsgVersion{
		Version:     1,
		BlockHeight: s.Chain.Height(),
		From:        s.Config.ListenAddr,
	})
	
	log.Printf("Peer connected: %s (outbound=%v)", addr, outbound)
}

func (s *Server) RemovePeer(p *Peer) {
	s.peerMu.Lock()
	defer s.peerMu.Unlock()
	
	addr := p.Conn.RemoteAddr().String()
	delete(s.peers, addr)
	p.Stop()
	log.Printf("Peer disconnected: %s", addr)
}

func (s *Server) Broadcast(msg Message) {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	for _, p := range s.peers {
		go p.Send(msg)
	}
}
