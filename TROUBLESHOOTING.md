# Oenexa Developer Troubleshooting & Incident Runbook

This document records the issues encountered during the development and deployment of the Oenexa network. Each entry details the symptoms, root cause, and the exact steps taken to resolve the issue to ensure quick resolution if the same problem occurs in the future.

---

## Issue 1: Docker Port Conflict (wslrelay Zombie Process)

**Symptoms:**
When running `docker compose up -d`, the following error occurs:
`Error response from daemon: ports are not available: exposing port TCP 127.0.0.1:8545 -> 127.0.0.1:0: listen tcp4 127.0.0.1:8545: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.`

**Root Cause:**
A background Windows Subsystem for Linux (WSL) relay process (`wslrelay`) failed to exit properly during a previous Docker shutdown. As a result, the port `8545` remained locked by the ghost process, preventing the new `oenexa-node` container from binding to it.

**Resolution / How to Fix:**
1. **Identify the zombie process:** Run `netstat -ano | findstr :8545` in PowerShell to find the Process ID (PID) holding the port.
   *(Example output will show the PID in the far right column, e.g., 52696)*
2. **Verify the process:** Run `Get-Process -Id <PID>` to confirm it is `wslrelay`.
3. **Kill the process:** Run `Stop-Process -Id <PID> -Force` to forcefully release the port.
4. **Restart Docker Compose:** Run `docker compose down` followed by `docker compose up -d` to cleanly boot the network.

---

## Issue 2: Docker Healthcheck Failing on `scratch` Containers

**Symptoms:**
The `oenexa-node` container is constantly marked as `unhealthy` by Docker, even though the node is running perfectly and serving RPC requests.

**Root Cause:**
The original `docker-compose.yml` healthcheck was using `wget -qO- http://localhost:8545 || exit 1`. However, the `oenexa-node` Docker image is built `FROM scratch` (distroless) for maximum security, meaning it contains literally no operating system utilities—no `sh`, no `bash`, no `wget`, and no `curl`. The healthcheck failed because the command `wget` did not exist inside the container.

**Resolution / How to Fix:**
1. **Create a native Go binary for health checks:** Wrote a tiny, native Go script (`cmd/healthcheck/main.go`) that simply makes an HTTP GET request to the node's local RPC port.
2. **Compile it during the Docker build phase:** Updated the `Dockerfile` to compile the healthcheck binary (`go build -o /out/healthcheck cmd/healthcheck/main.go`).
3. **Embed and Execute:** Copied `/out/healthcheck` into the final `scratch` image.
4. **Update Compose:** Changed the `docker-compose.yml` healthcheck definition to execute the compiled binary directly: `test: ["CMD", "/healthcheck"]`.

---

## Issue 3: Mempool Dropping Concurrent Transactions (Nonce Race Condition)

**Symptoms:**
During high-load TPS (Transactions Per Second) stress testing, the node logs showed transactions being violently rejected with errors like `Nonce mismatch`.

**Root Cause:**
The mempool priority queue (`txHeap`) sorts transactions globally by `GasPrice` (descending) and `Timestamp` (ascending). When the stress test worker blasted 10,000 transactions at once, the system time generated identical timestamps (down to the millisecond) for thousands of transactions. Because the timestamps were identical, the heap sort order became non-deterministic. The node popped transactions out of order, meaning `Nonce 5` might be processed before `Nonce 4`, causing the node's strict sequential nonce check to reject them.

**Resolution / How to Fix:**
1. **Implement an atomic pseudo-timestamp counter:** In the load tester (`cmd/loadtest/main.go`), introduced an atomic counter (`atomic.AddInt64`) applied to a base timestamp.
2. **Guarantee strict chronological ordering:** This ensures that even if 10,000 transactions are generated in the same millisecond, `Nonce 2` will definitively have a timestamp that is precisely 1 unit larger than `Nonce 1`, guaranteeing the heap sorts them in the exact order they must be processed.

---

## Issue 4: Zero Balance Errors & State Wiping (StateDB Bug)

**Symptoms:**
After the first block was minted, all user account balances dropped to 0 and subsequent transactions failed with `Insufficient funds`.

**Root Cause:**
In `internal/state/statedb.go`, the `CommitRoot()` function was aggressively resetting the `s.dirty` map back to an empty state `make(map[string]*Account)` after every block. Since the secondary persistence layer (AccountDB) was not yet fully implemented, the `dirty` map was acting as the sole source of truth in memory. Wiping it deleted the entire state of the blockchain.

**Resolution / How to Fix:**
1. **Remove aggressive reset:** Removed the `s.dirty = make(map[string]*Account)` line from the `CommitRoot()` function.
2. **Result:** The node now successfully persists state across block minting cycles in memory. (Note: A true persistent LevelDB/BadgerDB backend must be implemented prior to mainnet launch).
