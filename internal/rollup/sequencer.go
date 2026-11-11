package rollup

import (
	"errors"
	"math"
	"math/bits"
	"sync"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

// Sequencer represents an L2 Rollup node that aggregates user transactions,
// executes them off-chain, and produces batches for L1.
type Sequencer struct {
	mu            sync.RWMutex
	l2State       *state.DB
	mempool       []*core.Transaction
	lastBatchID   uint64
	lastRoot      [crypto.HashSize]byte
	batchInterval time.Duration
	
	// Mock proof generator function
	proofGenerator func(pre, post [crypto.HashSize]byte, txs []*core.Transaction) []byte
}

func NewSequencer() *Sequencer {
	return &Sequencer{
		l2State:       state.NewStateDB(),
		mempool:       make([]*core.Transaction, 0),
		batchInterval: 2 * time.Second,
		proofGenerator: mockSTARKProof,
	}
}

// SetLastRoot sets the initial L2 root (e.g. Genesis)
func (s *Sequencer) SetLastRoot(r [crypto.HashSize]byte) {
	s.lastRoot = r
}

// SubmitTransaction accepts a transaction into the L2 mempool.
// In a real network, this would be an RPC endpoint.
func (s *Sequencer) SubmitTransaction(tx *core.Transaction) error {
	if tx == nil {
		return errors.New("transaction cannot be nil")
	}
	// 1. Verify ML-DSA-65 Signature
	if err := tx.Verify(); err != nil {
		return errors.New("invalid post-quantum signature: " + err.Error())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.mempool = append(s.mempool, tx)
	return nil
}

// BuildBatch takes the current mempool, executes all txs in the L2 StateDB,
// computes the new state root, and generates a ZK-STARK proof.
func (s *Sequencer) BuildBatch() (*Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.mempool) == 0 {
		return nil, errors.New("mempool is empty")
	}
	if s.lastBatchID == math.MaxUint64 {
		return nil, errors.New("batch ID overflow")
	}

	preRoot := s.lastRoot

	txs := make([]*core.Transaction, len(s.mempool))
	copy(txs, s.mempool)
	s.mempool = s.mempool[:0] // Clear mempool

	executed := make([]*core.Transaction, 0, len(txs))
	for _, tx := range txs {
		if tx == nil || tx.Type != core.TxTransfer {
			continue
		}
		sender := s.l2State.GetAccount(tx.From)
		if tx.Nonce < sender.Nonce {
			continue // replay / stale nonce rejected
		}
		required, carry := bits.Add64(tx.Amount, tx.GasLimit, 0)
		if carry != 0 || sender.Balance < required {
			continue
		}

		if tx.From != tx.To {
			receiver := s.l2State.GetAccount(tx.To)
			newRecvBal, c2 := bits.Add64(receiver.Balance, tx.Amount, 0)
			if c2 != 0 {
				continue
			}
			sender.Balance -= tx.Amount
			sender.Nonce = tx.Nonce + 1
			receiver.Balance = newRecvBal
			s.l2State.SetAccount(tx.From, sender)
			s.l2State.SetAccount(tx.To, receiver)
		} else {
			sender.Nonce = tx.Nonce + 1
			s.l2State.SetAccount(tx.From, sender)
		}
		executed = append(executed, tx)
	}

	postRoot := s.l2State.CommitRoot()
	s.lastBatchID++
	s.lastRoot = postRoot

	proof := s.proofGenerator(preRoot, postRoot, executed)

	batch := &Batch{
		BatchID:      s.lastBatchID,
		PreRoot:      preRoot,
		PostRoot:     postRoot,
		Transactions: executed,
		ZkStarkProof: proof,
	}

	return batch, nil
}

// GetL2State returns the current state of the L2.
func (s *Sequencer) GetL2State() *state.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.l2State
}

func computeBatchCommitment(pre, post [crypto.HashSize]byte, txs []*core.Transaction) [crypto.HashSize]byte {
	parts := make([][]byte, 0, 2+len(txs))
	parts = append(parts, pre[:], post[:])
	for _, tx := range txs {
		if tx != nil {
			h := tx.ComputeHash()
			parts = append(parts, h[:])
		}
	}
	return crypto.HashMany(parts...)
}

// mockSTARKProof simulates the generation of a post-quantum ZK-STARK proof.
// Binds the proof commitment to (preRoot || postRoot || txHashes) via SHA-3-256.
func mockSTARKProof(pre, post [crypto.HashSize]byte, txs []*core.Transaction) []byte {
	proof := make([]byte, 4096)
	copy(proof[:32], pre[:])
	copy(proof[32:64], post[:])
	commitment := computeBatchCommitment(pre, post, txs)
	copy(proof[64:96], commitment[:])
	return proof
}
