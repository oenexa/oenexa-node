package core

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/oenexa/oenexa/internal/crypto"
)

// ─────────────────────────────────────────────────────────────────────────────
// Transaction — Quantum-Resistant Atomic State Transition
//
// PURPOSE:
//   1. Represents an indivisible state-transition directive executed on the
//      OENEXA Layer-1 sovereign blockchain.
//   2. Authenticates sovereign ownership, transacts native ONXC value, executes
//      smart contract calls, manages validator staking, and processes zero-knowledge
//      shielded privacy commitments.
//   3. Guarantees deterministic, tamper-proof execution semantics across all
//      participating network nodes.
//
// WHY:
//   - Without strict canonical serialization and unforgeable digital signatures,
//     adversaries could alter transaction amounts, reroute recipient destinations,
//     or replay previously executed transfers.
//   - Standard ECDSA/Ed25519 signatures are vulnerable to Shor's quantum algorithm.
//     OENEXA mandates FIPS 204 ML-DSA-65 post-quantum digital signatures on every
//     single transaction from genesis.
//   - Nonce-based replay protection eliminates duplicate transaction execution
//     across all forks and temporal epochs.
//
// HOW:
//   - Binary canonical payload construction: All fields (Version, Type, Nonce,
//     From, To, Amount, GasLimit, GasPrice, Timestamp, Data, PublicKey) are serialized
//     using strictly defined big-endian byte layouts.
//   - Signature verification: The node derives the public address from PublicKey via
//     SHA-3-256 and validates that it matches From. It then executes ML-DSA-65
//     verification over the canonical signing payload.
//   - Atomic state application: Mempool and block execution engines verify that
//     tx.Nonce == account.Nonce before applying balance deductions and state updates.
//
// FORMAL CRYPTOGRAPHIC PROOF OF INTEGRITY & NON-REPLAYABILITY:
//   Let:
//     tx = (v, \tau, n, from, to, a, g, p, t, D, pk, \sigma)
//     m = signingPayload(tx)
//     H = SHA-3-256
//
//   1. Address Binding Invariant:
//      from = H(pk)
//      Because H is collision-resistant (pre-image and second pre-image resistance
//      bounded by O(2^256) classical / O(2^128) quantum operations), an adversary
//      cannot find pk' != pk such that H(pk') = from. Therefore, a valid transaction
//      cannot be forged using an alternate keypair for an existing address.
//
//   2. Existential Unforgeability under Chosen Message Attacks (EUF-CMA):
//      Under the hardness of the Module Learning With Errors (M-LWE) and Module Short
//      Integer Solution (M-SIS) problems on lattice dimension (6, 5) with ring
//      Z_q[X]/(X^256 + 1) and modulus q = 8,380,417 (FIPS 204 ML-DSA-65):
//      For all PPT adversaries A having access to a signing oracle O_sign:
//        Pr[ Verify(pk, m*, sigma*) = 1  AND  m* not in Queries(O_sign) ] <= negl(lambda)
//      where security parameter lambda = 192 bits (NIST Post-Quantum Category 3).
//      No adversary can alter any field in m (amount, to, nonce, gas) without invalidating sigma*.
//
//   3. Nonce Monotonicity Replay-Prevention Theorem:
//      Let state sigma_h be the account state at height h with account nonce N_A.
//      A transaction tx from account A is valid for inclusion in block h+1 iff:
//        tx.Nonce == N_A
//      Upon successful execution:
//        N_A' = N_A + 1
//      Proof of Non-Replayability:
//        Suppose an adversary attempts to replay tx at any subsequent height h' > h.
//        At height h', account nonce N_A' >= N_A + 1 > tx.Nonce.
//        The state transition rule rejects any tx with tx.Nonce != N_A'.
//        Because nonce increments are strictly monotonic and irreversible,
//        tx can be executed at most once in the entire blockchain history.
//      Q.E.D.
// ─────────────────────────────────────────────────────────────────────────────

// Transaction represents a signed OENEXA transaction.
type Transaction struct {
	Version   uint8
	Type      TxType
	Nonce     uint64
	From      [crypto.AddressSize]byte
	To        [crypto.AddressSize]byte
	Amount    uint64
	GasLimit  uint64
	GasPrice  uint64
	Timestamp int64
	Data      []byte // contract bytecode (Deploy) or call payload (Call)
	PublicKey []byte // ML-DSA-65 public key (1952 bytes)
	Signature []byte // ML-DSA-65 signature (3309 bytes)
	Hash      [crypto.HashSize]byte
}

// signingPayload returns the canonical bytes that are signed.
func (tx *Transaction) signingPayload() []byte {
	buf := make([]byte, 0, 256+len(tx.Data)+len(tx.PublicKey))
	buf = append(buf, tx.Version, uint8(tx.Type))
	b8 := make([]byte, 8)
	binary.BigEndian.PutUint64(b8, tx.Nonce)
	buf = append(buf, b8...)
	buf = append(buf, tx.From[:]...)
	buf = append(buf, tx.To[:]...)
	binary.BigEndian.PutUint64(b8, tx.Amount)
	buf = append(buf, b8...)
	binary.BigEndian.PutUint64(b8, tx.GasLimit)
	buf = append(buf, b8...)
	binary.BigEndian.PutUint64(b8, tx.GasPrice)
	buf = append(buf, b8...)
	binary.BigEndian.PutUint64(b8, uint64(tx.Timestamp))
	buf = append(buf, b8...)
	b4 := make([]byte, 4)
	binary.BigEndian.PutUint32(b4, uint32(len(tx.Data)))
	buf = append(buf, b4...)
	buf = append(buf, tx.Data...)
	buf = append(buf, tx.PublicKey...)
	return buf
}

// SigningPayload returns the canonical bytes that are signed.
func (tx *Transaction) SigningPayload() []byte {
	return tx.signingPayload()
}

// Sign signs the transaction with the given ML-DSA-65 private key bytes.
func (tx *Transaction) Sign(privKeyBytes []byte) error {
	sig, err := crypto.Sign(privKeyBytes, tx.signingPayload())
	if err != nil {
		return err
	}
	tx.Signature = sig
	tx.Hash = tx.ComputeHash()
	return nil
}

// Verify checks the transaction's signature and that From == SHA-3-256(PublicKey).
func (tx *Transaction) Verify() error {
	derivedAddr := crypto.DeriveAddress(tx.PublicKey)
	if derivedAddr != tx.From {
		return errors.New("from address does not match the public key")
	}
	ok, err := crypto.Verify(tx.PublicKey, tx.signingPayload(), tx.Signature)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("invalid transaction signature")
	}
	return nil
}

// ComputeHash returns SHA-3-256 of the signing payload + signature.
func (tx *Transaction) ComputeHash() [crypto.HashSize]byte {
	return crypto.HashMany(tx.signingPayload(), tx.Signature)
}

// BasicValidate performs stateless validation.
func (tx *Transaction) BasicValidate() error {
	if tx.Version != 1 {
		return errors.New("unsupported tx version")
	}
	if tx.GasLimit == 0 {
		return errors.New("gas limit must be > 0")
	}
	if tx.GasPrice < MinGasPrice {
		return errors.New("gas price below a minimum")
	}
	if tx.Timestamp <= 0 {
		return errors.New("invalid timestamp")
	}
	if len(tx.PublicKey) != crypto.PublicKeySize {
		return errors.New("invalid public key size")
	}
	if len(tx.Signature) != crypto.SignatureSize {
		return errors.New("invalid signature size")
	}
	if err := tx.Verify(); err != nil {
		return err
	}
	computedHash := tx.ComputeHash()
	if tx.Hash == [crypto.HashSize]byte{} {
		tx.Hash = computedHash
	} else if tx.Hash != computedHash {
		return errors.New("transaction hash mismatch")
	}
	return nil
}

// NewTransfer creates a transfer transaction (not yet signed).
func NewTransfer(from [crypto.AddressSize]byte, to [crypto.AddressSize]byte,
	pubKey []byte, nonce, amount, gasPrice uint64) *Transaction {
	return &Transaction{
		Version:   1,
		Type:      TxTransfer,
		Nonce:     nonce,
		From:      from,
		To:        to,
		Amount:    amount,
		GasLimit:  GasTransfer,
		GasPrice:  gasPrice,
		Timestamp: time.Now().UnixNano(),
		PublicKey: pubKey,
	}
}

// NewShield creates a shield transaction (deposits transparent ONXC to a shielded note commitment).
func NewShield(from [crypto.AddressSize]byte, pubKey []byte, nonce, amount, gasPrice uint64, noteCommitment [crypto.HashSize]byte, encryptedNote []byte) *Transaction {
	data := make([]byte, 0, crypto.HashSize+len(encryptedNote))
	data = append(data, noteCommitment[:]...)
	data = append(data, encryptedNote...)
	return &Transaction{
		Version:   1,
		Type:      TxShield,
		Nonce:     nonce,
		From:      from,
		To:        [crypto.AddressSize]byte{},
		Amount:    amount,
		GasLimit:  GasShield,
		GasPrice:  gasPrice,
		Timestamp: time.Now().UnixNano(),
		Data:      data,
		PublicKey: pubKey,
	}
}

// NewUnshield creates an unshield transaction (withdraws shielded balance to a transparent address).
func NewUnshield(to [crypto.AddressSize]byte, pubKey []byte, nonce, amount, gasPrice uint64, nullifier [crypto.HashSize]byte, proof []byte) *Transaction {
	data := make([]byte, 0, crypto.HashSize+len(proof))
	data = append(data, nullifier[:]...)
	data = append(data, proof...)
	return &Transaction{
		Version:   1,
		Type:      TxUnshield,
		Nonce:     nonce,
		From:      crypto.DeriveAddress(pubKey),
		To:        to,
		Amount:    amount,
		GasLimit:  GasUnshield,
		GasPrice:  gasPrice,
		Timestamp: time.Now().UnixNano(),
		Data:      data,
		PublicKey: pubKey,
	}
}
