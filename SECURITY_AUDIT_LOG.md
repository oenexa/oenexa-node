# Oenexa Protocol - Security Audit & Hardening Log

This document tracks all security implementations, vulnerability resolutions, and infrastructure hardening applied to the Oenexa network.

---

### SEC-001: Docker Container OS Attack Surface Minimization
* **Date:** 2026-10-02
* **Domain:** Infrastructure / DevOps
* **Vulnerability:** Standard Docker images (like `ubuntu` or `alpine`) contain package managers, shell access (`/bin/sh`), and networking utilities (`wget`, `curl`). If an attacker escapes the node process, they could utilize these tools to download malware or pivot across the network.
* **Resolution:** Re-architected the `Dockerfile` to utilize a multi-stage build. The final production image uses `FROM scratch` (distroless). The image is completely empty except for the compiled Go binary. There is no shell, no OS, and no utilities, practically eliminating the container-level attack surface.

### SEC-002: Docker Healthcheck `scratch` Compatibility
* **Date:** 2026-10-03
* **Domain:** Infrastructure / Availability
* **Vulnerability:** Because of SEC-001 (`FROM scratch`), Docker's built-in `healthcheck` relying on `wget` failed, causing orchestration tools to mark the node as unhealthy and potentially restart it endlessly (causing a self-inflicted Denial of Service).
* **Resolution:** Wrote a tiny native Go HTTP client (`cmd/healthcheck/main.go`) compiled during the Docker build phase. This binary safely pings the RPC port without relying on external OS tools.

### SEC-003: Post-Quantum Cryptography Enforcement (NIST FIPS 204/203)
* **Date:** 2026-10-01
* **Domain:** Core Cryptography
* **Vulnerability:** Shor's algorithm running on sufficiently powerful quantum computers will break standard ECDSA (secp256k1) signatures used by legacy blockchains (Bitcoin/Ethereum).
* **Resolution:** Integrated the final NIST standards for Post-Quantum Cryptography. Replaced ECDSA with **ML-DSA-65** for transaction signatures and **ML-KEM-768** for key encapsulation. The frontend and backend now actively verify quantum-resistant signatures, effectively future-proofing the ledger.

### SEC-004: Frontend RPC Endpoint Verification
* **Date:** 2026-10-03
* **Domain:** Client Security
* **Vulnerability:** The frontend dashboard allowed unrestricted switching of the RPC endpoint without origin verification, potentially allowing a malicious user to phish or misdirect node traffic.
* **Resolution:** Implemented origin detection in `App.tsx` (`initialEndpoint`). It safely defaults to the originating host network rather than blindly falling back to public endpoints, mitigating cross-origin RPC misdirection for node operators.

### SEC-005: Smart Contract VM Gas Exhaustion (Infinite Loop) Mitigation
* **Date:** 2026-10-04
* **Domain:** Smart Contract VM (Wazero)
* **Vulnerability:** The node's WebAssembly execution environment (wazero) was instantiated without CPU bounds or timeout interrupts. A malicious contract containing a `(loop br 0)` instruction could cause the node to lock up indefinitely, executing an infinite loop and resulting in a total Denial of Service for the node.
* **Resolution:** Reconfigured `vm.NewVM` to inject `wazero.NewRuntimeConfig().WithCloseOnContextDone(true)`. Added a context timeout boundary before WebAssembly execution (`Call`). The VM now guarantees execution will be violently aborted via a `context deadline exceeded` panic if the execution breaches the node's block-time gas limits, preserving node availability.
