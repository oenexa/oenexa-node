package crypto

import (
	"crypto/rand"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

// ─────────────────────────────────────────────────────────────────────────────
// NIST POST-QUANTUM CRYPTOGRAPHY SPECIFICATION (FIPS 204 ML-DSA-65)
// ─────────────────────────────────────────────────────────────────────────────
//
// PURPOSE:
//   Provides post-quantum keypair generation, sovereign address derivation,
//   and secure memory zeroization for OENEXA accounts and consensus validators.
//
// WHY (ARCHITECTURAL RATIONALE):
//   1. Shor's Algorithm Immunity: Classical elliptic-curve signatures (secp256k1,
//      ed25519) can be solved in polynomial time O((log N)^3) on a quantum computer.
//   2. FIPS 204 Compliance: NIST standardizes ML-DSA (formerly Dilithium) as the
//      primary primary post-quantum digital signature standard.
//   3. Parameter Set 65: Chosen at NIST Security Category 3 (192-bit security
//      strength, equivalent to AES-192), achieving the optimal balance between
//      computational throughput (77 µs verification) and wire overhead.
//
// HOW (MECHANICAL EXECUTION):
//   1. Keygen: Samples polynomial vectors in the module ring R_q = Z_q[X]/(X^256 + 1)
//      using cryptographically secure entropy from crypto/rand.
//   2. Address Derivation: Computes SHA-3-256(PublicKey) to produce a unique,
//      collision-resistant 32-byte sovereign account address.
//   3. Zeroization: Manually wipes private key memory buffers on shutdown or lifecycle completion.
//
// PROOF OF POST-QUANTUM HARDNESS:
//   Security reduces to the worst-case hardness of the Module Learning With Errors
//   (M-LWE) and Module Short Integer Solution (M-SIS) problems on Euclidean lattices.
//   No known quantum algorithm (including Shor's and Grover's) can solve M-LWE/M-SIS
//   in sub-exponential time; Grover's search provides at most a quadratic speedup
//   which is nullified by the 192-bit security margin of parameter set 65.
// ─────────────────────────────────────────────────────────────────────────────

const (
	PublicKeySize  = 1952 // ML-DSA-65 public key size in bytes
	PrivateKeySize = 4032 // ML-DSA-65 private key size in bytes
	SignatureSize  = 3309 // ML-DSA-65 signature size in bytes
	AddressSize    = 32   // FIPS 202 SHA-3-256 digest of public key in bytes
)

// Wallet holds an ML-DSA-65 keypair and its derived sovereign account address.
type Wallet struct {
	PublicKey  []byte // 1952 bytes (NIST ML-DSA-65)
	PrivateKey []byte // 4032 bytes (NIST ML-DSA-65)
	Address    [AddressSize]byte // 32 bytes (SHA-3-256)
}

// NewWallet generates a fresh ML-DSA-65 keypair using CSPRNG entropy.
func NewWallet() (*Wallet, error) {
	pub, priv, err := mldsa65.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return walletFromKeys(pub, priv)
}

func walletFromKeys(pub *mldsa65.PublicKey, priv *mldsa65.PrivateKey) (*Wallet, error) {
	pubBytes, _ := pub.MarshalBinary()
	privBytes, _ := priv.MarshalBinary()
	addr := DeriveAddress(pubBytes)
	return &Wallet{
		PublicKey:  pubBytes,
		PrivateKey: privBytes,
		Address:    addr,
	}, nil
}

// DeriveAddress computes the sovereign account address: Address = SHA-3-256(pubKeyBytes).
//
// PROOF OF COLLISION RESISTANCE:
//   Under SHA-3-256 (Keccak-f[1600]), finding a collision requires O(2^128) quantum evaluations
//   or O(2^256) classical operations, guaranteeing unique address derivation.
func DeriveAddress(pubKeyBytes []byte) [AddressSize]byte {
	return Hash256(pubKeyBytes)
}

// Zeroize securely overwrites sensitive private key memory buffers with zeros.
func (w *Wallet) Zeroize() {
	for i := range w.PrivateKey {
		w.PrivateKey[i] = 0
	}
}

// WalletFromPrivateKey recovers a Wallet from raw ML-DSA-65 private key bytes.
func WalletFromPrivateKey(privKeyBytes []byte) (*Wallet, error) {
	priv := new(mldsa65.PrivateKey)
	if err := priv.UnmarshalBinary(privKeyBytes); err != nil {
		return nil, err
	}
	pub := priv.Public().(*mldsa65.PublicKey)
	return walletFromKeys(pub, priv)
}
