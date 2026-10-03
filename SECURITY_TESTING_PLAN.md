# Oenexa Node - Security Testing Plan

As a pure blockchain layer, the Oenexa node requires rigorous security validation across consensus, cryptography, networking, and the execution environment. This document outlines the planned security testing processes to be implemented.

## 1. Cryptographic Validation
The node uses Post-Quantum cryptography (ML-DSA-65 / ML-KEM-768). Testing must ensure mathematically sound implementations.
* **Test Focus:**
  * Signature malleability checks.
  * Key generation entropy validation.
  * State root and Merkle tree collision resistance testing.
* **Implementation Plan:** Integrate Go `crypto/rand` fuzzing and external audit scripts against the NIST reference implementations.

## 2. Consensus & Sybil Resistance (P2P Layer)
The node must be protected against network-level attacks.
* **Test Focus:**
  * **Eclipse Attacks:** Attempt to isolate a node by monopolizing its peer connections.
  * **Sybil Attacks:** Spin up hundreds of fake nodes to attempt network takeovers.
  * **Long-Range Attacks:** Submit deep, alternate blockchain histories to test fork resolution.
* **Implementation Plan:** Build a dockerized "Chaos Mesh" network test where random node failures and adversarial nodes are introduced to the Devnet automatically.

## 3. RPC & API Security
The JSON-RPC endpoints are the gateways for wallets and exchanges.
* **Test Focus:**
  * **DDoS & Rate Limiting:** Spam `eth_sendRawTransaction` or `oen_getBalance` to observe memory consumption.
  * **Malformed Payloads:** Send deeply nested JSON or invalid hex strings to trigger panics.
  * **CORS & Authentication:** Ensure `0.0.0.0` bindings securely restrict unauthorized administrative endpoints (like keystore access).
* **Implementation Plan:** Write a Python/Go load-tester specifically designed to mutate JSON payloads and flood the RPC ports.

## 4. Smart Contract VM & State Transition
Since DApps (like the Online Delivery application) will run on top of this pure blockchain, the VM executing their logic must be sandboxed.
* **Test Focus:**
  * **Reentrancy and Sandbox Escapes:** Ensure executed bytecodes cannot read memory outside their allocated heap.
  * **Gas Exhaustion:** Deploy infinite loops and massive array allocations to ensure the `GasLimit` accurately kills the execution before node CPU locks up.
* **Implementation Plan:** Create a suite of "malicious" smart contracts and deploy them into the automated test suite (`go test`).

## 5. Automated Static & Dynamic Analysis
* **Implementation Plan:**
  * Integrate `gosec` (Golang Security Checker) into the GitHub Actions CI/CD pipeline to scan for hardcoded credentials or unsafe pointers.
  * Implement **Fuzz Testing** (`go test -fuzz`) on the transaction decoder to randomly mutate byte arrays.

---
**Status:** Planning Phase.
**Next Actions:** Gradually integrate these vectors into the automated `test/bdd` suite and GitHub Actions workflow.
