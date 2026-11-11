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

type voteKey struct {
	round    uint32
	voteType uint8
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
	votesAtHeight  map[voteKey][crypto.HashSize]byte
	hasVoted       bool

	lastBlockHeight uint64
	lastBlockHash   [crypto.HashSize]byte
	hasProposed     bool
}

// NewLocalSigner creates a new validator signer with post-quantum keys and anti-slashing protection.
func NewLocalSigner(addr [crypto.AddressSize]byte, pubKey, privKey []byte) *LocalSigner {
	return &LocalSigner{
		address:       addr,
		pubKey:        pubKey,
		privKey:       privKey,
		votesAtHeight: make(map[voteKey][crypto.HashSize]byte),
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
	if len(s.privKey) == 0 {
		return errors.New("no private key available for signing")
	}
	return s.signVoteWithFn(v, func(payload []byte) ([]byte, error) {
		return crypto.Sign(s.privKey, payload)
	})
}

// signVoteWithFn enforces high-water mark anti-slashing invariants across all (round, voteType)
// pairs at the current highest height, preventing vote-type toggling equivocation.
func (s *LocalSigner) signVoteWithFn(v *Vote, signFn func([]byte) ([]byte, error)) error {
	if v == nil {
		return errors.New("nil vote")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := voteKey{round: v.Round, voteType: v.Type}
	if s.hasVoted {
		if v.Height < s.lastVoteHeight {
			return fmt.Errorf("%w: vote height %d < last signed height %d", ErrStaleVote, v.Height, s.lastVoteHeight)
		}
		if v.Height == s.lastVoteHeight {
			if prevHash, exists := s.votesAtHeight[key]; exists && prevHash != v.BlockHash {
				return fmt.Errorf("%w: conflicting hash %x vs %x at height %d round %d",
					ErrEquivocationPrevented, v.BlockHash[:8], prevHash[:8], v.Height, v.Round)
			}
		}
	}

	sig, err := signFn(v.payload())
	if err != nil {
		return err
	}
	v.Signature = sig

	if !s.hasVoted || v.Height > s.lastVoteHeight {
		s.lastVoteHeight = v.Height
		s.votesAtHeight = make(map[voteKey][crypto.HashSize]byte)
	}
	s.votesAtHeight[key] = v.BlockHash
	s.hasVoted = true
	return nil
}

// SignBlock signs a block after checking for proposal equivocation.
func (s *LocalSigner) SignBlock(blk *core.Block) error {
	if len(s.privKey) == 0 {
		return errors.New("no private key available for signing")
	}
	return s.signBlockWithFn(blk, func(payload []byte) ([]byte, error) {
		return crypto.Sign(s.privKey, payload)
	})
}

func (s *LocalSigner) signBlockWithFn(blk *core.Block, signFn func([]byte) ([]byte, error)) error {
	if blk == nil {
		return errors.New("nil block")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	computedHash := blk.ComputeHash()

	if s.hasProposed {
		if blk.Header.Height < s.lastBlockHeight {
			return fmt.Errorf("%w: block height %d < last proposed height %d", ErrStaleVote, blk.Header.Height, s.lastBlockHeight)
		}
		if blk.Header.Height == s.lastBlockHeight && s.lastBlockHash != [crypto.HashSize]byte{} && s.lastBlockHash != computedHash {
			return fmt.Errorf("%w: conflicting block hash at height %d", ErrDuplicateBlockProposal, blk.Header.Height)
		}
	}

	sig, err := signFn(blk.Header.Encode())
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
	return k.guard.signVoteWithFn(v, func(payload []byte) ([]byte, error) {
		sig, err := k.signFn(payload)
		if err != nil {
			return nil, fmt.Errorf("remote kms sign vote: %w", err)
		}
		return sig, nil
	})
}

func (k *KMSSigner) SignBlock(blk *core.Block) error {
	return k.guard.signBlockWithFn(blk, func(payload []byte) ([]byte, error) {
		sig, err := k.signFn(payload)
		if err != nil {
			return nil, fmt.Errorf("remote kms sign block: %w", err)
		}
		return sig, nil
	})
}
