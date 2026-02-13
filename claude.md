
# Chronodrachma (CHRD) - Technical Specifications

## 1. Network Overview
**Start Date:** 2024 (Prototype Phase)
**Consensus Mechanism:** Proof-of-Work (RandomX)
**Symbol:** CHRD

## 2. Core Constraints
### Timing & Emission
- **Target Block Time:** 60 minutes (3600 seconds).
- **Emission Schedule:** 1 CHRD per block (Linear emission).
- **Input Supply:** 0 (No pre-mine).
- **Genesis Timestamp:** TBD (On Launch).

### Consensus & Security
- **Algorithm:** RandomX (CPU-optimized PoW).
- **Difficulty Adjustment:** Retargets every block based on moving average of past N blocks to maintain 1-hour intervals.
- **Finality:** 24 confirmations (~24 hours).
    - Coinbase maturity: 24 blocks.
    - Transaction finality: Recommended 24 blocks.

## 3. Architecture (Go)

### Directory Structure
```
/
├── cmd/
│   └── chrd/           # Main executable
├── pkg/
│   ├── blockchain/     # Chain state, Block validation
│   ├── consensus/      # RandomX, Proof verification
│   ├── p2p/            # Peer discovery, Gossipsub
│   └── wallet/         # Key management (Ed25519)
├── go.mod              # Module definition
└── README.md
```

## 4. Implementation Task List

### Phase 1: Foundation (Complete)
- [x] **Project Setup**: Initialize Go module and folder structure.
- [x] **Data Structures**: Define `Block`, `Header`, `Transaction` structs in `pkg/core/types/`.
- [x] **Genesis Block**: Implement Genesis block creation (Height 0) in `pkg/core/blockchain/`.
- [x] **Monetary Policy**: Linear emission (1 CHRD/block) in `pkg/core/blockchain/emission.go`.
- [x] **Confirmation Logic**: 24-block maturity rule in `pkg/core/blockchain/confirmation.go`.
- [x] **Block Validation**: Work (PoW hash), time (1-hour target), integrity (prev hash) in `pkg/core/blockchain/validation.go`.
- [x] **Consensus Interface**: `Hasher` interface + SHA256Hasher (test) in `pkg/core/consensus/`.
- [x] **RandomX CGO Bindings**: Vendored C library + CGO wrappers in `pkg/core/consensus/randomx/`.
- [x] **Tests**: 15 passing tests covering genesis, emission, maturity, and validation.

### Phase 2: Consensus (Proof-of-Work)
- [ ] **Mining Loop**: Implement solver for finding nonces.
- [ ] **Difficulty Controller**: Implement retargeting algorithm for 60m blocks.

### Phase 3: Networking & Persistence
- [ ] **P2P Layer**: Implement basic TCP/QUIC listener and peer handshake.
- [ ] **Block Propagation**: Gossip new blocks to peers.
- [ ] **Storage**: LevelDB/BadgerDB wrapper for block storage.

### Phase 4: Rules & Finality
- [ ] **Fork Choice Rule**: Longest chain with most cumulative difficulty.
