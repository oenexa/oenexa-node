# OENEXA Developer & AI Agent Strict Enforcement Policy 🛡️

> **MANDATORY POLICY FOR ALL HUMAN DEVELOPERS AND AI AGENTS (Antigravity, Cursor, Copilot, Claude, etc.)**  
> **EFFECTIVE DATE: IMMEDIATE — ZERO TOLERANCE FOR UNVERIFIED CODE**

This document defines the non-negotiable rules and quality gates for contributing to this repository. Every developer and AI assistant operating in this codebase is strictly bound by these rules.

---

## 1. The Zero-Bug & 100% Verification Mandate

1. **No Code Without Verification**: Code must be mathematically sound, defensively written, and verified locally before submission.
2. **100% Test Passing Rate**:
   - Zero test failures are tolerated.
   - Zero flaky tests.
   - Any modification must include accompanying unit tests covering happy paths, edge cases, and failure modes.
3. **Zero Compiler / Build Warnings**:
   - Code must compile with 0 warnings and 0 errors.
   - Go code must build with `CGO_ENABLED=0` and pass `go vet`.

---

## 2. Mandatory Security & Fuzzing Verification

Before modifying or adding code that handles external input, cryptography, consensus, or network messaging:

1. **Input Sanitization**: Every RPC, REST, and P2P payload must be bounds-checked and validated against buffer overflows, truncation, and injection.
2. **Consensus Invariants**: State transitions, block proposals, and PBFT votes must guarantee safety, liveness, and immutability.
3. **Security Fuzzing**: Boundary parsers (JSON deserializers, Wazero WASM execution, ML-DSA-65 signature verifiers) must pass fuzz tests without triggering host runtime panics or memory leaks.

---

## 3. Strict Main Branch Gate (Pre-Push Enforcement)

**NO CODE MAY BE PUSHED TO THE `main` BRANCH WITHOUT PASSING THE VERIFICATION CHECKLIST:**

### Pre-Push Verification Commands:
```bash
# 1. Run all unit and integration tests (Must be 100% PASS)
go test ./...

# 2. Verify compilation of daemon binary (Must compile with 0 errors)
go build -o /dev/null ./cmd/node
```

**AI Agent Rule**: If you are an AI assistant executing commands or proposing commits, you **MUST** run the verification suite in your sandbox/terminal and verify a `0` exit code before executing `git push origin main`. Pushing failing code or skipping tests is a critical protocol violation.

---

## 4. Pure Layer-1 Architectural Boundary

* **STRICTLY PURE BLOCKCHAIN ONLY**: `oenexa-node` is the Layer-1 protocol engine.
* **NEVER** introduce consumer business logic, e-commerce logic, food delivery, or external application state into this repository.
* All consumer applications communicate via the decoupled Web3 JSON-RPC 2.0 interface.
