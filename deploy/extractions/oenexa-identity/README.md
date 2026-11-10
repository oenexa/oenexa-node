# OENEXA Identity Repository (oenexa-identity)

**Classification:** MOVED FROM OENEXA-NODE  
**Purpose:** W3C Verifiable Credentials (VC), Decentralized Identifiers (DID), Institutional KYC/AML Verifications, Compliance Attestations.  
**Sovereign Native Asset:** ONXC (Open Network for the Next Generation Coin)

---

## Architectural Responsibility

Under the OENEXA enterprise repository governance architecture:
- `oenexa-node` is **blockchain-core only** (consensus, state machine, blocks, cryptography, networking, RPC).
- `oenexa-identity` is the **dedicated enterprise identity domain** (KYC/AML verification, W3C credentials, compliance reporting, biometric / document attestation).

---

## Contents Extracted
- `vc.go`: W3C Verifiable Credential schema definitions, ML-DSA-65 post-quantum signing, and KYC credential issuance.
- `vc_test.go`: Comprehensive test suite for credential verification, signature checks, and tampering detection.
- `identity_extended_test.go`: Edge case testing and multi-jurisdiction attestation tests.

---

## Standalone Repository Initialization

To push this module into a new standalone repository `github.com/oenexa/oenexa-identity`:

```bash
git init oenexa-identity
cd oenexa-identity
cp -r /path/to/oenexa-node/deploy/extractions/oenexa-identity/* .
git add .
git commit -m "feat(identity): initialize sovereign oenexa-identity repository"
git remote add origin git@github.com:oenexa/oenexa-identity.git
git push -u origin main
```
