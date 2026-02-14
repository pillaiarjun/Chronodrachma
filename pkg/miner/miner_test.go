package miner

import (
	"testing"
	"time"

	"github.com/chronodrachma/chrd/pkg/core/blockchain"
	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/mempool"
	"github.com/chronodrachma/chrd/pkg/core/types"
	"github.com/chronodrachma/chrd/pkg/p2p"
)

type SlowHasher struct {
	inner consensus.Hasher
	delay time.Duration
}

func (h *SlowHasher) Hash(headerBytes []byte) (types.Hash, error) {
	time.Sleep(h.delay)
	return h.inner.Hash(headerBytes)
}

func (h *SlowHasher) Close() {
	h.inner.Close()
}

func mustNewTestChain(t *testing.T, hasher consensus.Hasher) (*blockchain.Chain, blockchain.BlockStore) {
	store, err := blockchain.NewBadgerStore("") // In-memory
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	chain, err := blockchain.NewChain(store, hasher)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	return chain, store
}

func TestMiner_Mining(t *testing.T) {
	// Use SlowHasher to prevent mining too fast
	hasher := &SlowHasher{inner: consensus.NewSHA256Hasher(), delay: 10 * time.Millisecond}
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	minerAddr := types.Hash{0x01}
	// Use difficulty 0 for tests to ensure genesis passes PoW check (InitGenesis doesn't mine)
	genesis, err := chain.InitGenesis(minerAddr, 0, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("genesis init failed: %v", err)
	}

	mp := mempool.NewMempool(chain)
	p2pServer := p2p.NewServer(p2p.ServerConfig{}, chain, mp)

	// Use Fast hasher for miner? No, miner uses same hasher for PoW.
	// If Miner calls Hash(), it sleeps.
	// This simulates difficulty.
	miner := NewMiner(chain, hasher, p2pServer, mp, minerAddr)

	// Start mining
	miner.Start()

	// Wait for a block
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	found := false
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for block")
		case <-ticker.C:
			if chain.Height() > 0 {
				found = true
			}
		}
		if found {
			break
		}
	}

	miner.Stop()

	tip := chain.Tip()
	if tip.Header.Height < 1 {
		t.Errorf("expected height >= 1, got %d", tip.Header.Height)
	}
	// Check ancestor
	ancestor, _ := chain.GetAncestorAtHeight(tip, 0)
	if ancestor.Hash != genesis.Hash {
		t.Errorf("chain does not originate from genesis")
	}
}

func TestMiner_TipUpdate(t *testing.T) {
	// Slower hasher for tip update test to control pace
	hasher := &SlowHasher{inner: consensus.NewSHA256Hasher(), delay: 50 * time.Millisecond}
	defer hasher.Close()

	chain, store := mustNewTestChain(t, hasher)
	defer store.Close()

	minerAddr := types.Hash{0x01}
	// Diff 0 for Genesis
	_, err := chain.InitGenesis(minerAddr, 0, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("genesis init failed: %v", err)
	}

	mp := mempool.NewMempool(chain)
	p2pServer := p2p.NewServer(p2p.ServerConfig{}, chain, mp)

	miner := NewMiner(chain, hasher, p2pServer, mp, minerAddr)

	miner.Start()
	defer miner.Stop()

	// Expect Miner to find Height 1 eventually.
	// With 50ms delay, and 8 threads, it should find it in < 1s (Diff 0 is trivial, but sleep happens BEFORE checking diff?
	// SlowHasher sleeps then hashes.
	// 50ms per hash. 8 threads -> ~160 hashes/sec.
	// Diff 0 -> Any hash works.
	// So first hash works.
	// So 50ms to find block.

	// Wait for Height 1
	timeout := time.After(2 * time.Second)
	for chain.Height() == 0 {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for block 1")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	tip1 := chain.Tip()

	// Now Miner is working on Height 2.
	// We inject a valid Height 2 block (b2) to force switch to Height 3.
	// We need to build b2 with same hasher (slow).
	// But we can manually build it with fast hasher if we want?
	// Chain validation uses `chain.hasher` (SlowHasher).
	// So validation will be slow (50ms).

	// Construct b2 on top of tip1.
	fastHasher := consensus.NewSHA256Hasher()
	b2 := buildManualBlock(t, fastHasher, tip1, minerAddr)

	// Add b2 to chain.
	if err := chain.AddBlock(b2); err != nil {
		t.Fatalf("failed to add manual block 2: %v", err)
	}

	// Wait for Height 3.
	// Miner should have stopped working on its own Block 2 (which would have parent tip1).
	// It should restart on b2.
	// And mine Block 3 (parent b2).

	timeout = time.After(5 * time.Second)
	found3 := false
Loop:
	for {
		if chain.Height() >= 3 {
			found3 = true
			break Loop
		}
		select {
		case <-timeout:
			break Loop
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	if !found3 {
		t.Fatalf("timed out waiting for block 3. Current height: %d", chain.Height())
	}

	tip3 := chain.Tip()
	// Check if tip3 (or its ancestor at height 2) is b2.
	if tip3.Header.Height < 3 {
		t.Errorf("expected height >= 3")
	}

	b2Canon, _ := chain.GetBlockByHeight(2)
	if b2Canon.Hash != b2.Hash {
		t.Errorf("Miner did not switch to b2. Canon H2: %x, Expected: %x", b2Canon.Hash, b2.Hash)
	}
}

func buildManualBlock(t *testing.T, hasher consensus.Hasher, parent *types.Block, miner types.Hash) *types.Block {
	t.Helper()
	height := parent.Header.Height + 1
	coinbase := &types.Transaction{
		Type: types.TxTypeCoinbase, Timestamp: time.Now(), From: types.ZeroHash, To: miner, Amount: blockchain.BlockReward(height), Nonce: height,
	}
	coinbase.ID = coinbase.ComputeID()

	block := &types.Block{
		Header: types.BlockHeader{
			Version: 1, Height: height, Timestamp: time.Now(), PrevBlockHash: parent.Hash,
			MerkleRoot: types.ComputeMerkleRoot([]*types.Transaction{coinbase}),
			Difficulty: 0, Nonce: 0, // Diff 0 for easy mining
		},
		Transactions: []*types.Transaction{coinbase},
	}

	// Mine it
	for {
		block.Hash = block.ComputeHash()
		pow, _ := hasher.Hash(block.Header.Serialize())
		block.PowHash = pow
		if consensus.MeetsDifficulty(pow, 0) {
			break
		}
		block.Header.Nonce++
	}
	return block
}
