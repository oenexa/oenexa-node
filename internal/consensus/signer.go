package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

var (
	ErrEquivocationPrevented  = errors.New("anti-slashing: conflicting vote prevented (equivocation)")
	ErrStaleVote              = errors.New("anti-slashing: vote height below water mark")
	ErrDuplicateBlockProposal = errors.New("anti-slashing: conflicting block proposal prevented")
)

// ValidatorSigner defines the interface for signing consensus votes and block proposals.
// Implementations range from LocalSigner (with memory anti-slashing guards) to remote
// Cloud KMS (AWS KMS, Google Cloud KMS, HashiCorp Vault) or PKCS#11 HSM appliances.
type ValidatorSigner interface {
	Address() [crypto.AddressSize]byte
	PublicKey() []byte
	SignVote(v *Vote) error
	SignBlock(blk *core.Block) error
}

// LocalSigner signs votes and blocks using in-memory ML-DSA-65 keys,
// strictly enforcing high-water mark protection against double-signing.
type LocalSigner struct {
	mu      sync.Mutex
	address [crypto.AddressSize]byte
	pubKey  []byte
	privKey []byte

	// High-water mark state
	lastVoteHeight uint64
	lastVoteRound  uint32
	lastVoteType   uint8
	lastVoteHash   [crypto.HashSize]byte
	hasVoted       bool

	lastBlockHeight uint64
	lastBlockHash   [crypto.HashSize]byte
	hasProposed     bool
}

// NewLocalSigner creates a new validator signer with post-quantum keys and anti-slashing protection.
func NewLocalSigner(addr [crypto.AddressSize]byte, pubKey, privKey []byte) *LocalSigner {
	return &LocalSigner{
		address: addr,
		pubKey:  pubKey,
		privKey: privKey,
	}
}

// Address returns the validator address.
func (s *LocalSigner) Address() [crypto.AddressSize]byte {
	return s.address
}

// PublicKey returns the ML-DSA-65 public key bytes.
func (s *LocalSigner) PublicKey() []byte {
	return s.pubKey
}

// SignVote signs a consensus vote after checking for equivocation.
func (s *LocalSigner) SignVote(v *Vote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.privKey) == 0 {
		return errors.New("no private key available for signing")
	}

	// High-water mark anti-slashing check
	if s.hasVoted {
		if v.Height < s.lastVoteHeight {
			return fmt.Errorf("%w: vote height %d < last signed height %d", ErrStaleVote, v.Height, s.lastVoteHeight)
		}
		if v.Height == s.lastVoteHeight && v.Round == s.lastVoteRound && v.Type == s.lastVoteType {
			if v.BlockHash != s.lastVoteHash {
				return fmt.Errorf("%w: conflicting hash %x vs %x at height %d round %d",
					ErrEquivocationPrevented, v.BlockHash[:8], s.lastVoteHash[:8], v.Height, v.Round)
			}
		}
	}

	if err := v.Sign(s.privKey); err != nil {
		return err
	}

	s.lastVoteHeight = v.Height
	s.lastVoteRound = v.Round
	s.lastVoteType = v.Type
	s.lastVoteHash = v.BlockHash
	s.hasVoted = true
	return nil
}

// SignBlock signs a block after checking for proposal equivocation.
func (s *LocalSigner) SignBlock(blk *core.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.privKey) == 0 {
		return errors.New("no private key available for signing")
	}

	computedHash := blk.ComputeHash()

	if s.hasProposed {
		if blk.Header.Height < s.lastBlockHeight {
			return fmt.Errorf("%w: block height %d < last proposed height %d", ErrStaleVote, blk.Header.Height, s.lastBlockHeight)
		}
		if blk.Header.Height == s.lastBlockHeight && s.lastBlockHash != [crypto.HashSize]byte{} && s.lastBlockHash != computedHash {
			return fmt.Errorf("%w: conflicting block hash at height %d", ErrDuplicateBlockProposal, blk.Header.Height)
		}
	}

	sig, err := crypto.Sign(s.privKey, blk.Header.Encode())
	if err != nil {
		return err
	}
	blk.Signature = sig
	blk.Hash = computedHash

	s.lastBlockHeight = blk.Header.Height
	s.lastBlockHash = computedHash
	s.hasProposed = true
	return nil
}

// KMSSigner integrates remote Cloud KMS / HSM hardware signing while maintaining
// local anti-slashing invariants.
type KMSSigner struct {
	guard  *LocalSigner
	keyURI string
	signFn func(payload []byte) ([]byte, error)
}

// NewKMSSigner wraps a remote signing function with an anti-slashing guard.
func NewKMSSigner(
	addr [crypto.AddressSize]byte,
	pubKey []byte,
	keyURI string,
	remoteSignFn func(payload []byte) ([]byte, error),
) *KMSSigner {
	return &KMSSigner{
		guard:  NewLocalSigner(addr, pubKey, nil),
		keyURI: keyURI,
		signFn: remoteSignFn,
	}
}

func (k *KMSSigner) Address() [crypto.AddressSize]byte {
	return k.guard.Address()
}

func (k *KMSSigner) PublicKey() []byte {
	return k.guard.PublicKey()
}

func (k *KMSSigner) KeyURI() string {
	return k.keyURI
}

func (k *KMSSigner) SignVote(v *Vote) error {
	k.guard.mu.Lock()
	defer k.guard.mu.Unlock()

	if k.guard.hasVoted {
		if v.Height < k.guard.lastVoteHeight {
			return fmt.Errorf("%w: vote height %d < last signed height %d", ErrStaleVote, v.Height, k.guard.lastVoteHeight)
		}
		if v.Height == k.guard.lastVoteHeight && v.Round == k.guard.lastVoteRound && v.Type == k.guard.lastVoteType {
			if v.BlockHash != k.guard.lastVoteHash {
				return fmt.Errorf("%w: conflicting hash at height %d round %d", ErrEquivocationPrevented, v.Height, v.Round)
			}
		}
	}

	sig, err := k.signFn(v.payload())
	if err != nil {
		return fmt.Errorf("remote kms sign vote: %w", err)
	}
	v.Signature = sig

	k.guard.lastVoteHeight = v.Height
	k.guard.lastVoteRound = v.Round
	k.guard.lastVoteType = v.Type
	k.guard.lastVoteHash = v.BlockHash
	k.guard.hasVoted = true
	return nil
}

func (k *KMSSigner) SignBlock(blk *core.Block) error {
	k.guard.mu.Lock()
	defer k.guard.mu.Unlock()

	computedHash := blk.ComputeHash()

	if k.guard.hasProposed {
		if blk.Header.Height < k.guard.lastBlockHeight {
			return fmt.Errorf("%w: block height %d < last proposed height %d", ErrStaleVote, blk.Header.Height, k.guard.lastBlockHeight)
		}
		if blk.Header.Height == k.guard.lastBlockHeight && k.guard.lastBlockHash != [crypto.HashSize]byte{} && k.guard.lastBlockHash != computedHash {
			return fmt.Errorf("%w: conflicting block hash at height %d", ErrDuplicateBlockProposal, blk.Header.Height)
		}
	}

	sig, err := k.signFn(blk.Header.Encode())
	if err != nil {
		return fmt.Errorf("remote kms sign block: %w", err)
	}
	blk.Signature = sig
	blk.Hash = computedHash

	k.guard.lastBlockHeight = blk.Header.Height
	k.guard.lastBlockHash = computedHash
	k.guard.hasProposed = true
	return nil
}
