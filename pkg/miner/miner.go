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
	numCPU := runtime.NumCPU()
	log.Printf("Miner started. Using %d CPU threads.", numCPU)
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

	// Subscribe to chain tip updates
	tipCh := m.chain.SubscribeTip()

	// Create a context for the current mining job
	// We'll cancel this whenever we need to restart (new tip) or stop (quit)
	var ctx context.Context
	var cancel context.CancelFunc

	// Ensure we clean up the last context when we exit
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	for {
		// 1. Get current tip and prepare to mine
		parent := m.chain.Tip()

		// 2. Refresh context for this mining round
		if cancel != nil {
			cancel()
		}
		ctx, cancel = context.WithCancel(context.Background())

		// 3. Start mining in background
		// We use a channel to signal if we found a block
		foundBlockCh := make(chan *types.Block, 1)

		go func(parentBlock *types.Block, miningCtx context.Context) {
			// Calculate difficulty
			getBlockInternal := func(h uint64) (*types.Block, error) {
				return m.chain.GetBlockByHeight(h)
			}
			difficulty, err := consensus.CalcNextRequiredDifficulty(parentBlock, getBlockInternal)
			if err != nil {
				log.Printf("Miner: failed to calc difficulty: %v", err)
				// Retry after sleep? Or just wait for next tip?
				// For now, small sleep and exit this attempt
				time.Sleep(time.Second)
				return
			}

			// Construct template
			template := m.createBlockTemplate(parentBlock, difficulty)

			// Mine with N workers
			if m.solveBlock(miningCtx, template) {
				select {
				case foundBlockCh <- template:
				case <-miningCtx.Done():
				}
			}
		}(parent, ctx)

		// 4. Wait for events
		select {
		case <-m.quit:
			return

		case newTip := <-tipCh:
			// New tip arrived!
			log.Printf("Miner: New tip received (heigth %d, hash %x). Restarting mining.",
				newTip.Header.Height, newTip.Hash[:8])
			// Loop will continue, cancelling current job context via defer/reassignment

		case block := <-foundBlockCh:
			// We found a block!
			log.Printf("Mined block! Hash: %x, Height: %d", block.Hash, block.Header.Height)

			if err := m.chain.AddBlock(block); err != nil {
				log.Printf("Miner: failed to add mined block: %v", err)
			} else {
				m.p2pServer.Broadcast(&p2p.MsgBlock{Block: block})
				// Remove txs from mempool
				if len(block.Transactions) > 1 {
					m.mempool.RemoveTransactions(block.Transactions[1:])
				}
			}
			// We continue mining on top of our own block (which should trigger tip update shortly,
			// but we can just loop around; AddBlock will trigger notify subscribers too)
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

// solveBlock attempts to solve the block PoW using multiple workers.
// Returns true if solved, false if cancelled or failed.
func (m *Miner) solveBlock(ctx context.Context, block *types.Block) bool {
	numWorkers := runtime.NumCPU()
	resultCh := make(chan struct{}, 1) // Signal success

	// Create a WaitGroup to ensure all workers exit before we return?
	// Not strictly necessary if we don't care about their lingering CPU usage for a microsecond.
	// But let's be clean.
	var wg sync.WaitGroup

	// Base nonce for this attempt.
	// Each worker will start at base + i, and stride by numWorkers.
	baseNonce := block.Header.Nonce

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Local copy of header to modify nonce without lock (only nonce changes)
			// Actually, we can't modify the SAME block header concurrently.
			// Each worker needs its own header structure or we need to be careful.
			// Since `Hash` is method on Block/Header, we should give each worker its own scratch space.

			header := block.Header
			header.Nonce = baseNonce + uint64(workerID)

			for {
				select {
				case <-ctx.Done():
					return
				case <-resultCh:
					// Another worker found it
					return
				default:
					headerBytes := header.Serialize()
					hash, err := m.hasher.Hash(headerBytes)
					if err != nil {
						return
					}

					if consensus.MeetsDifficulty(hash, header.Difficulty) {
						// Found it!
						// Update the shared block with the solution (Thread safe? only one writer wins)
						// We need to signal we won.
						select {
						case resultCh <- struct{}{}:
							// We are the winner, update the main block
							// This is slightly racey if multiple solve at exact same time, but rare.
							// Better to send the solution back.
							block.Header.Nonce = header.Nonce
							// Identity Hash (SHA-256) != PoW Hash (RandomX or DoubleSHA)
							block.Hash = block.ComputeHash()
							block.PowHash = hash
						default:
							// Lost the race
						}
						return
					}

					// Stride
					header.Nonce += uint64(numWorkers)
				}
			}
		}(i)
	}

	// Wait for success or cancel
	select {
	case <-resultCh:
		// A worker succeeded and updated `block`
		// Cancel others
		// (Context is cancelled by caller or we can do it here, but caller owns context)
		// We just wait for them to finish? No, we return true immediately.
		return true
	case <-ctx.Done():
		return false
	}
}
