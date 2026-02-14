package miner

import (
	"context"
	"log"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/blockchain"
	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/mempool"
	"github.com/chronodrachma/chrd/pkg/core/types"
	"github.com/chronodrachma/chrd/pkg/p2p"
)

type Miner struct {
	chain     *blockchain.Chain
	hasher    consensus.Hasher // Must be initialized for mining (e.g. RandomX dataset)
	p2pServer *p2p.Server
	mempool   *mempool.Mempool
	address   types.Hash // Miner's address for coinbase
	quit      chan struct{}
	wg        sync.WaitGroup
}

func NewMiner(chain *blockchain.Chain, hasher consensus.Hasher, p2pServer *p2p.Server, mp *mempool.Mempool, address types.Hash) *Miner {
	return &Miner{
		chain:     chain,
		hasher:    hasher,
		p2pServer: p2pServer,
		mempool:   mp,
		address:   address,
		quit:      make(chan struct{}),
	}
}

func (m *Miner) Start() {
	log.Println("Miner started. CPU threads:", runtime.NumCPU())
	m.wg.Add(1)
	go m.miningLoop()
}

func (m *Miner) Stop() {
	close(m.quit)
	m.wg.Wait()
	log.Println("Miner stopped")
}

func (m *Miner) miningLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.quit:
			return
		default:
			// 1. Get current tip
			parent := m.chain.Tip()

			// 2. Calculate required difficulty
			getBlockInternal := func(h uint64) (*types.Block, error) {
				return m.chain.GetBlockByHeight(h)
			}
			difficulty, err := consensus.CalcNextRequiredDifficulty(parent, getBlockInternal)
			if err != nil {
				log.Printf("Miner: failed to calc difficulty: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// 3. Construct block template
			block := m.createBlockTemplate(parent, difficulty)

			// 4. Mine (find nonce)
			// For simplicity in prototype, we'll just loop here. 
			// In production, we'd spawn multiple workers.
			if m.solveBlock(block) {
				// Found a block!
				log.Printf("Mined block! Hash: %x, Height: %d", block.Hash, block.Header.Height)
				
				// 5. Add to chain
				if err := m.chain.AddBlock(block); err != nil {
					log.Printf("Miner: failed to add mined block: %v", err)
					continue
				}

				// 6. Broadcast
				m.p2pServer.Broadcast(&p2p.MsgBlock{Block: block})
				
				// 7. Remove included transactions from mempool
				// Note: Ideally this happens via chain event or checking block, 
				// but MINER knows it included them.
				m.mempool.RemoveTransactions(block.Transactions[1:]) // Skip coinbase
			}
		}
	}
}

func (m *Miner) createBlockTemplate(parent *types.Block, difficulty uint64) *types.Block {
	timestamp := time.Now()
	// Ensure timestamp is greater than parent
	if !timestamp.After(parent.Header.Timestamp) {
		timestamp = parent.Header.Timestamp.Add(time.Second)
	}

	// Create coinbase
	coinbase := &types.Transaction{
		Type:      types.TxTypeCoinbase,
		Timestamp: timestamp,
		From:      types.ZeroHash,
		To:        m.address,
		Amount:    blockchain.BlockReward(parent.Header.Height + 1),
		Fee:       0,
		Nonce:     0,
	}
	coinbase.ID = coinbase.ComputeID()

	txs := []*types.Transaction{coinbase}
	
	// Include transactions from mempool
	// Limit to ~1000 for prototype
	pending := m.mempool.GetPendingTransactions(1000)
	txs = append(txs, pending...)

	header := types.BlockHeader{
		Version:       1,
		Height:        parent.Header.Height + 1,
		Timestamp:     timestamp,
		PrevBlockHash: parent.Hash,
		MerkleRoot:    types.ComputeMerkleRoot(txs),
		Difficulty:    difficulty,
		Nonce:         rand.Uint64(), // Start with random nonce
	}

	return &types.Block{
		Header:       header,
		Transactions: txs,
	}
}

func (m *Miner) solveBlock(block *types.Block) bool {
	// Try a batch of nonces
	// Check for quit every so often
	// Since we are single threaded in this simplified loop, we just run for a short duration or N hashes
	
	// Create context with timeout to yield and check for new blocks
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return false // Yield to check for new tip / quit
		case <-m.quit:
			return false
		default:
			headerBytes := block.Header.Serialize()
			hash, err := m.hasher.Hash(headerBytes)
			if err != nil {
				log.Printf("Miner hasher error: %v", err)
				return false
			}

			if consensus.MeetsDifficulty(hash, block.Header.Difficulty) {
				block.Hash = hash
				block.PowHash = hash // For RandomX verification
				return true
			}

			block.Header.Nonce++
		}
	}
}
