# Operations Runbook: oenexa-node

## Service Overview
- **Service Name:** `oenexa-node`
- **Role:** Layer-1 Post-Quantum Sovereign Blockchain Node (ONXC)
- **Consensus:** NIST ML-DSA-65 / ML-KEM-768 / SHA-3-256 + PoS BFT Engine
- **JSON-RPC Port:** `8545` (Internal / Proxied via Caddy TLS at `/rpc`)
- **P2P Gossip Port:** `8765` (Public TCP)
- **Prometheus Metrics Port:** `9090` (Internal `/metrics`)

## Incident Escalation Matrix
- **Level 1:** SRE / On-Call Node Operator
- **Level 2:** Core Protocol Engineers (`@oenexa/blockchain-core`)
- **Level 3:** Enterprise Architecture & Security Board

## Quick Diagnostic Commands

### 1. Check Container & Process Health
```bash
# Check Docker container status and health state
docker ps --filter name=oenexa-node

# Inspect health check details directly
docker inspect --format='{{json .State.Health}}' oenexa-node | jq .
```

### 2. Live JSON-RPC Node Diagnostics
```bash
# Query Current Block Height
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"onxc_blockHeight","params":[],"id":1}' | jq .

# Query Node Network & Peer Count
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}' | jq .

# Query Node Synchronization Status
curl -s -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"onxc_syncing","params":[],"id":1}' | jq .
```

### 3. Log Stream & Prometheus Metrics
```bash
# Follow live consensus and block production logs
docker logs -f --tail=100 oenexa-node

# Probe Prometheus metrics endpoint
curl -s http://127.0.0.1:9090/metrics | grep -E "oenexa_|consensus_|block_height"
```

## Emergency Procedures

### Node Stalled or Block Sync Lagging
1. Verify network connectivity on P2P port 8765:
   ```bash
   nc -zv 127.0.0.1 8765
   ```
2. Restart the node container cleanly without impacting other services:
   ```bash
   docker compose -f docker-compose.prod.yml up -d --no-deps oenexa-node
   ```
3. Verify resumption of block height progression within 30 seconds.
