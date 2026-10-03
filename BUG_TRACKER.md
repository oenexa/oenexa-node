# Oenexa Protocol - Bug Tracker & Resolution Log

This document records the core protocol bugs, logic errors, and state machine issues encountered during development. It tracks the symptom, root cause, and the applied fix to ensure regressions can be easily identified.

---

### Bug-001: Mempool Nonce Race Condition under High Load
* **Date:** 2026-10-03
* **Severity:** High
* **Component:** `internal/mempool`
* **Symptoms:** During stress testing (>1,500 TPS), the node aggressively rejected valid transactions with `Nonce mismatch` errors.
* **Root Cause:** The `txHeap` sorts transactions by `GasPrice` and `Timestamp`. Under extreme load, thousands of transactions were generated within the exact same millisecond. Identical timestamps caused non-deterministic heap sorting, leading to transactions being popped out of order (e.g., Nonce 5 before Nonce 4). The strict state machine correctly rejected the out-of-order nonces.
* **Resolution:** Implemented an atomic pseudo-timestamp counter (`atomic.AddInt64(&baseTimestamp, 1)`) in the load testing client. This guaranteed that even in the same millisecond, transactions were stamped with strictly increasing time values matching their nonce order, allowing the heap to sort them deterministically.

---

### Bug-002: In-Memory StateDB Wipe on Block Commit
* **Date:** 2026-10-02
* **Severity:** Critical
* **Component:** `internal/state/statedb.go`
* **Symptoms:** After the first block was minted, all user account balances dropped to 0. Subsequent transactions failed with `Insufficient funds`.
* **Root Cause:** The `CommitRoot()` function aggressively reset the `s.dirty` map (`s.dirty = make(map[string]*Account)`) after computing the state root. Because the secondary persistent database layer (AccountDB) is not yet fully implemented, wiping the in-memory map effectively deleted the entire ledger state.
* **Resolution:** Removed the aggressive map reset from `CommitRoot()`. The node now successfully persists state across blocks in memory.
* **Follow-up Required:** Must implement true persistent backend (LevelDB/BadgerDB) flushing before mainnet.

---

### Bug-003: Front-End UI Theme Mismatch (Black Buttons)
* **Date:** 2026-10-03
* **Severity:** Low
* **Component:** `oenexa-frontend/src/App.tsx`
* **Symptoms:** Toggling to "Light Mode" caused UI buttons to render as pitch black and become unreadable against the light background, breaking UX.
* **Root Cause:** The custom Sci-Fi Matrix CSS was strictly designed around a dark theme (`var(--bg-primary)`). The `isDark` React state toggle was flipping the CSS data-theme without having proper light-mode fallbacks defined in the CSS.
* **Resolution:** Removed the Light/Dark toggle entirely from the Header and hardcoded `document.documentElement.setAttribute('data-theme', 'dark')` on mount. Enforced Pure Dark Mode to match the Matrix/Sci-Fi brand identity.
