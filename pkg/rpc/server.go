package rpc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/blockchain"
	"github.com/chronodrachma/chrd/pkg/core/mempool"
	"github.com/chronodrachma/chrd/pkg/core/types"
	"github.com/chronodrachma/chrd/pkg/p2p"
)

type Server struct {
	chain     *blockchain.Chain
	mempool   *mempool.Mempool
	p2pServer *p2p.Server
}

func NewServer(chain *blockchain.Chain, mp *mempool.Mempool, p2p *p2p.Server) *Server {
	return &Server{
		chain:     chain,
		mempool:   mp,
		p2pServer: p2p,
	}
}

func (s *Server) Start(port string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/tx", s.handleTx)

	// Add a simple status endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Chronodrachma Node Running. Height: %d", s.chain.Height())
	})

	return http.ListenAndServe(port, mux)
}

// GET /balance?addr=<hex>
func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	addrHex := r.URL.Query().Get("addr")
	if addrHex == "" {
		http.Error(w, "missing addr parameter", http.StatusBadRequest)
		return
	}

	addr, err := types.HashFromHex(addrHex)
	if err != nil {
		http.Error(w, "invalid address format", http.StatusBadRequest)
		return
	}

	balance, nonce, err := s.chain.GetAccountState(addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get state: %v", err), http.StatusInternalServerError)
		return
	}

	// Return JSON
	resp := struct {
		Address string       `json:"address"`
		Balance types.Amount `json:"balance"`
		Nonce   uint64       `json:"nonce"`
	}{
		Address: addrHex,
		Balance: balance,
		Nonce:   nonce,
	}

	json.NewEncoder(w).Encode(resp)
}

// POST /tx
// Body: JSON object of transaction fields + signature
type TxRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Amount    uint64 `json:"amount"`
	Fee       uint64 `json:"fee"`
	Nonce     uint64 `json:"nonce"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

func (s *Server) handleTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req TxRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Helper to parse hex
	parseHash := func(h string) (types.Hash, error) {
		return types.HashFromHex(h)
	}

	from, err := parseHash(req.From)
	if err != nil {
		http.Error(w, "invalid from address", http.StatusBadRequest)
		return
	}
	to, err := parseHash(req.To)
	if err != nil {
		http.Error(w, "invalid to address", http.StatusBadRequest)
		return
	}
	
	// Parse signature
	sig, err := hex.DecodeString(req.Signature)
	if err != nil {
		http.Error(w, "invalid signature hex", http.StatusBadRequest)
		return
	}

	// Construct Transaction
	tx := &types.Transaction{
		ID:        types.Hash{}, // will compute
		Type:      types.TxTypeTransfer,
		Timestamp: time.Unix(req.Timestamp, 0),
		From:      from,
		To:        to,
		Amount:    types.Amount(req.Amount),
		Fee:       types.Amount(req.Fee),
		Nonce:     req.Nonce,
		Signature: sig,
	}
	
	// Compute ID
	tx.ID = tx.ComputeID()

	// Add to mempool (validates signature and balance)
	if err := s.mempool.AddTransaction(tx); err != nil {
		http.Error(w, fmt.Sprintf("rejected: %v", err), http.StatusBadRequest)
		return
	}

	// Broadcast via P2P
	s.p2pServer.Broadcast(&p2p.MsgTx{Tx: tx})

	fmt.Fprintf(w, "{\"status\": \"ok\", \"txid\": \"%x\"}", tx.ID)
}
