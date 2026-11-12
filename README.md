# OENEXA Node (`oenexa-node`)

**OENEXA** is a sovereign, post-quantum, privacy-preserving Layer-1 blockchain. Its native coin is **ONXC** (*Open Network for the Next Generation Coin*).

> **Full Beginner & Student Documentation:**  
> All architectural guides, mathematical proofs, tutorials, and API references live in the companion **`oenexa-docs`** repository (`../oenexa-docs`). This repository strictly contains blockchain node core code, consensus, state, cryptography, networking, RPC, and operational configuration.

---

## Core Specifications

| Property             | Value                                                                    | Source                                                                     |
|:---------------------|:-------------------------------------------------------------------------|:---------------------------------------------------------------------------|
| **Native Coin**      | **ONXC** (`Open Network for the Next Generation Coin`)                   | [`internal/core/types.go`](internal/core/types.go)                         |
| **Atomic Unit**      | `1 ONXC = 1,000,000,000 nano-ONXC` (`9` decimals)                        | [`internal/core/types.go`](internal/core/types.go)                         |
| **Hard Supply Cap**  | `100,000,000 ONXC` (`10M` Genesis Premine + `<90M` Block Rewards)        | [`internal/core/tokenomics.go`](internal/core/tokenomics.go)               |
| **Block Interval**   | `2` seconds (Instant BFT Finality)                                       | [`internal/consensus/engine.go`](internal/consensus/engine.go)             |
| **Halving Schedule** | Initial `45 ONXC/block`, halving every `1,000,000` blocks                | [`internal/core/tokenomics.go`](internal/core/tokenomics.go)               |
| **Cryptography**     | `ML-DSA-65` (FIPS 204), `ML-KEM-768` (FIPS 203), `SHA-3-256` (FIPS 202)  | [`internal/crypto/`](internal/crypto/)                                     |
| **Fee Model**        | Dynamic EIP-1559 (`InitialBaseFee = 10`, `MinBaseFee = 1 nano-ONXC/gas`) | [`internal/core/fee.go`](internal/core/fee.go)                             |
| **Privacy Engine**   | Dual-Pool (Transparent + Shielded) with Turnstile supply invariant       | [`internal/shielded/`](internal/shielded/)                                 |
| **Smart Contracts**  | Pure-Go `wazero` WebAssembly runtime (`OenexaVM`)                        | [`internal/vm/`](internal/vm/)                                             |
| **State Storage**    | `256`-bit Sparse Merkle Trie over LevelDB                                | [`internal/mpt/`](internal/mpt/), [`internal/storage/`](internal/storage/) |
| **APIs**             | JSON-RPC 2.0 (`onxc_*` & `eth_*`) + Rosetta API v1.4.14                  | [`internal/rpc/`](internal/rpc/), [`internal/rosetta/`](internal/rosetta/) |

---

## Quick Start

### 1. Build Binaries
```bash
make build build-cli
```
Or with `go build`:
```bash
go build -trimpath -o bin/oenexa-node ./cmd/node
go build -trimpath -o bin/oenexa-cli ./cmd/oenexa-cli
```

### 2. Run Local Devnet
```bash
# Linux / macOS
./scripts/run-local.sh --reset

# Windows PowerShell
.\scripts\run-local.ps1 -Reset
```

### 3. Run Full Test Suite
```bash
go test -count=1 ./cmd/... ./internal/... ./test/...
go vet ./...
```

---

## Repository Layout

- `cmd/node`: Full node & validator CLI (`oenexa-node`)
- `cmd/oenexa-cli`: Lightweight client CLI (`oenexa-cli`)
- `cmd/healthcheck`, `cmd/loadtest`, `cmd/multisig`: Healthprobe, load generator, and multisig utilities
- `internal/core`: Blocks, transactions, EIP-1559 fees, genesis, and ONXC tokenomics
- `internal/crypto`, `internal/keystore`: `ML-DSA-65`, `SHA-3-256`, and `Argon2id` + `AES-256-GCM` keystores
- `internal/consensus`: BFT consensus engine, validator set, delegation, and 10% equivocation slashing
- `internal/mempool`: Priority-queue mempool (`100,000` capacity, per-sender limits)
- `internal/state`, `internal/mpt`, `internal/storage`: State execution, Sparse Merkle Trie, and LevelDB stores
- `internal/shielded`: Confidential notes, nullifier set, `NoteCommitmentTree`, viewing keys, and Turnstile
- `internal/vm`: `OenexaVM` WASM execution engine
- `internal/p2p`, `internal/sync`, `internal/upgrade`: `libp2p` gossip mesh, block syncer, binary upgrades
- `internal/rpc`, `internal/rosetta`, `internal/metrics`: JSON-RPC 2.0, Rosetta v1.4.14, Prometheus metrics
- `internal/oracle`, `internal/rollup`: Sovereign ONXC price oracles and L2 rollup sequencer
- `configs/`, `deploy/`, `scripts/`: Network TOML configs, Kubernetes/Docker Compose manifests, local launchers
