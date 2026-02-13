
# Chronodrachma (CHRD)

Chronodrachma is a Layer-1 blockchain prototype featuring:
- **Consensus:** Proof-of-Work (RandomX)
- **Block Time:** 60 minutes
- **Emission:** 1 CHRD/hour (No pre-mine)
- **Finality:** 24-hour rule

## Getting Started

### Prerequisites
- Go 1.21+

### Build
```bash
go build ./cmd/chrd
```

### Directory Structure
- `cmd/chrd/`: Main entry point.
- `pkg/core/`: Consensus and blockchain logic.
- `pkg/p2p/`: Networking.
