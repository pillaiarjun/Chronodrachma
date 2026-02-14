package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chronodrachma/chrd/pkg/config"
	"github.com/chronodrachma/chrd/pkg/core/blockchain"
	"github.com/chronodrachma/chrd/pkg/core/consensus"
	"github.com/chronodrachma/chrd/pkg/core/mempool"
	"github.com/chronodrachma/chrd/pkg/core/types"
	"github.com/chronodrachma/chrd/pkg/miner"
	"github.com/chronodrachma/chrd/pkg/p2p"
	"github.com/chronodrachma/chrd/pkg/rpc"
	"github.com/chronodrachma/chrd/pkg/wallet"
)

func main() {
	// Subcommands
	runCmd := flag.NewFlagSet("run", flag.ExitOnError)
	mineCmd := flag.NewFlagSet("mine", flag.ExitOnError)
	walletCmd := flag.NewFlagSet("wallet", flag.ExitOnError)
	balanceCmd := flag.NewFlagSet("balance", flag.ExitOnError)
	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)

	// Run/Mine Flags
	nodeAddr := runCmd.String("addr", ":9000", "P2P listen address")
	seedNode := runCmd.String("seed", "", "Seed node address to connect to")
	rpcPort := runCmd.String("rpc", ":8080", "RPC server port")

	minerNodeAddr := mineCmd.String("addr", ":9001", "P2P listen address")
	minerSeedNode := mineCmd.String("seed", "", "Seed node address to connect to")
	minerRewardAddr := mineCmd.String("miner-addr", "", "Address to receive mining rewards (hex)")
	minerRpcPort := mineCmd.String("rpc", ":8081", "RPC server port")

	// Wallet Flags
	walletAction := walletCmd.String("action", "new", "Action: new")
	walletFile := walletCmd.String("file", "wallet.dat", "File to save/load key")

	// Balance Flags
	balanceAddr := balanceCmd.String("addr", "", "Address to check balance")
	balanceRpc := balanceCmd.String("rpc", "http://localhost:8080", "RPC server URL")

	// Send Flags
	sendTo := sendCmd.String("to", "", "Recipient address")
	sendAmount := sendCmd.Uint64("amount", 0, "Amount to send")
	sendFee := sendCmd.Uint64("fee", 100, "Transaction fee")
	sendKeyFile := sendCmd.String("key", "wallet.dat", "Private key file")
	sendRpc := sendCmd.String("rpc", "http://localhost:8080", "RPC server URL")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd.Parse(os.Args[2:])
		startNode(*nodeAddr, *seedNode, *rpcPort, false, types.Hash{})
	case "mine":
		mineCmd.Parse(os.Args[2:])
		if *minerRewardAddr == "" {
			fmt.Println("Error: --miner-addr is required for mining")
			os.Exit(1)
		}
		addrHash, err := types.HashFromHex(*minerRewardAddr)
		if err != nil {
			log.Fatalf("Invalid miner address: %v", err)
		}
		startNode(*minerNodeAddr, *minerSeedNode, *minerRpcPort, true, addrHash)
	case "wallet":
		walletCmd.Parse(os.Args[2:])
		handleWallet(*walletAction, *walletFile)
	case "balance":
		balanceCmd.Parse(os.Args[2:])
		if *balanceAddr == "" {
			fmt.Println("Error: --addr is required")
			os.Exit(1)
		}
		handleBalance(*balanceRpc, *balanceAddr)
	case "send":
		sendCmd.Parse(os.Args[2:])
		if *sendTo == "" || *sendAmount == 0 {
			fmt.Println("Error: --to and --amount are required")
			os.Exit(1)
		}
		handleSend(*sendRpc, *sendKeyFile, *sendTo, *sendAmount, *sendFee)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  chrd run [flags]")
	fmt.Println("  chrd mine [flags]")
	fmt.Println("  chrd wallet --action new --file <wallet.dat>")
	fmt.Println("  chrd balance --addr <hex>")
	fmt.Println("  chrd send --to <hex> --amount <uint64> --key <wallet.dat>")
}

func startNode(listenAddr, seedAddr, rpcPort string, isMiner bool, minerAddr types.Hash) {
	log.Printf("Starting Chronodrachma Node (Testnet)...")

	// Initialize Hasher (SHA256 or RandomX based on build tags)
	// Use a fixed seed for prototype. In production, seed comes from block height % N.
	seed := make([]byte, 32)
	hasher, err := consensus.NewHasher(seed, isMiner)
	if err != nil {
		log.Fatalf("Failed to initialize hasher: %v", err)
	}
	defer hasher.Close()

	dbPath := "data"
	if rpcPort == ":8081" {
		dbPath = "data_miner"
	}

	s, err := blockchain.NewBadgerStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer s.Close()

	chain, err := blockchain.NewChain(s, hasher)
	if err != nil {
		log.Fatalf("Failed to load chain: %v", err)
	}

	mp := mempool.NewMempool(chain)

	genesisTime := config.TestnetConfig.GenesisTimestamp
	_, err = chain.InitGenesis(config.GenesisMinerAddress, config.TestnetConfig.InitialDifficulty, genesisTime)
	if err != nil && err != blockchain.ErrChainAlreadyInitialized {
		log.Fatalf("Failed to init genesis: %v", err)
	}

	// P2P
	seeds := []string{}
	if seedAddr != "" {
		seeds = append(seeds, seedAddr)
	}
	p2pConfig := p2p.ServerConfig{
		ListenAddr: listenAddr,
		SeedNodes:  seeds,
	}
	server := p2p.NewServer(p2pConfig, chain, mp)
	// Start P2P in goroutine or non-blocking? Server.Start is blocking?
	// Server.Start loops listener. So we need goroutine.
	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start P2P server: %v", err)
		}
	}()

	// RPC
	rpcServer := rpc.NewServer(chain, mp, server)
	go func() {
		log.Printf("RPC Server listening on %s", rpcPort)
		if err := rpcServer.Start(rpcPort); err != nil {
			log.Printf("RPC Server error: %v", err)
		}
	}()

	if isMiner {
		m := miner.NewMiner(chain, hasher, server, mp, minerAddr)
		m.Start()
		defer m.Stop()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Println("Shutting down...")
}

func handleWallet(action, filename string) {
	if action == "new" {
		pub, priv, err := wallet.GenerateKeyPair()
		if err != nil {
			log.Fatalf("Error generating key: %v", err)
		}
		if err := wallet.SaveKey(filename, priv); err != nil {
			log.Fatalf("Error saving key: %v", err)
		}
		fmt.Printf("Generated new keypair.\n")
		fmt.Printf("Private Key saved to: %s\n", filename)
		fmt.Printf("Address: %s\n", wallet.PubKeyToAddress(pub))
	} else {
		fmt.Println("Unknown wallet action")
	}
}

func handleBalance(rpcUrl, addr string) {
	resp, err := http.Get(fmt.Sprintf("%s/balance?addr=%s", rpcUrl, addr))
	if err != nil {
		log.Fatalf("RPC error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
}

func handleSend(rpcUrl, keyFile, toHex string, amount, fee uint64) {
	// 1. Load Key
	privKey, err := wallet.LoadKey(keyFile)
	if err != nil {
		log.Fatalf("Failed to load key: %v", err)
	}

	// Derive public key (last 32 bytes of priv key in Ed25519)
	// ed25519.PrivateKey is 64 bytes: 32 bytes seed + 32 bytes pubkey.
	if len(privKey) != ed25519.PrivateKeySize {
		log.Fatalf("Invalid key file")
	}
	pubKey := ed25519.PublicKey(privKey[32:])
	fromAddr := wallet.PubKeyToAddress(pubKey)

	// 2. Get Nonce via RPC (using balance endpoint)
	resp, err := http.Get(fmt.Sprintf("%s/balance?addr=%s", rpcUrl, fromAddr))
	if err != nil {
		log.Fatalf("RPC error getting nonce: %v", err)
	}
	defer resp.Body.Close()

	var balanceResp struct {
		Balance types.Amount `json:"balance"`
		Nonce   uint64       `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		log.Fatalf("Failed to decode balance response: %v", err)
	}

	// 3. Construct Tx
	toHash, err := types.HashFromHex(toHex)
	if err != nil {
		log.Fatalf("Invalid recipient: %v", err)
	}
	fromHash, _ := types.HashFromHex(fromAddr) // safe since derived

	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
		Timestamp: time.Now(),
		From:      fromHash,
		To:        toHash,
		Amount:    types.Amount(amount),
		Fee:       types.Amount(fee),
		Nonce:     balanceResp.Nonce, // Use expected next nonce?
		// Note: The nonces returned by GetAccountState is the count of *mined* txs.
		// If there are pending txs, this might collide.
		// For prototype, we assume simple usage.
		// But in `chain.go`, nonce = count of sent txs.
		// So first tx is nonce 0.
		// If I sent 0 txs, nonce is 0.
		// So my next tx should have nonce 0.
		// So `balanceResp.Nonce` IS the next valid nonce. Correct.
	}

	// 4. Sign
	if err := wallet.SignTransaction(tx, privKey); err != nil {
		log.Fatalf("Sign error: %v", err)
	}

	// 5. Submit
	// Prepare JSON payload matching rpc.TxRequest
	req := map[string]interface{}{
		"from":      fromAddr,
		"to":        toHex,
		"amount":    amount,
		"fee":       fee,
		"nonce":     tx.Nonce,
		"signature": hex.EncodeToString(tx.Signature),
		"timestamp": tx.Timestamp.Unix(),
	}

	jsonBody, _ := json.Marshal(req)
	txResp, err := http.Post(fmt.Sprintf("%s/tx", rpcUrl), "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Fatalf("RPC submit error: %v", err)
	}
	defer txResp.Body.Close()
	body, _ := io.ReadAll(txResp.Body)
	fmt.Println(string(body))
}
