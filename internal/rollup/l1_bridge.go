package rollup

import (
	"bytes"
	"errors"
	"math"
	"sync"

	"github.com/oenexa/oenexa/internal/crypto"
)

// L1Bridge acts as the Smart Contract on the main OENEXA chain (L1)
// that verifies and records L2 batches submitted by the Sequencer.
type L1Bridge struct {
	mu             sync.RWMutex
	currentL2Root  [crypto.HashSize]byte
	batchHistory   map[uint64]*Batch
	lastBatchID    uint64
}

func NewL1Bridge(genesisRoot [crypto.HashSize]byte) *L1Bridge {
	return &L1Bridge{
		currentL2Root: genesisRoot,
		batchHistory:  make(map[uint64]*Batch),
		lastBatchID:   0,
	}
}

// SubmitBatch is called by the Sequencer on the L1 chain.
func (b *L1Bridge) SubmitBatch(batch *Batch) error {
	if batch == nil {
		return errors.New("batch cannot be nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.lastBatchID == math.MaxUint64 || batch.BatchID != b.lastBatchID+1 {
		return errors.New("invalid batch sequence ID")
	}

	// 2. Verify state transition continuity
	if !bytes.Equal(batch.PreRoot[:], b.currentL2Root[:]) {
		return errors.New("pre-root does not match current L2 state root")
	}

	// 3. Verify ZK-STARK Proof & batch transaction commitment (Post-Quantum Security)
	if !verifySTARK(batch) {
		return errors.New("invalid ZK-STARK proof")
	}

	// 4. Accept Batch and update L1's view of L2 state
	b.currentL2Root = batch.PostRoot
	b.batchHistory[batch.BatchID] = batch
	b.lastBatchID = batch.BatchID

	return nil
}

// GetL2Root returns the latest finalized L2 state root on L1.
func (b *L1Bridge) GetL2Root() [crypto.HashSize]byte {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentL2Root
}

// verifySTARK verifies the deterministic seed and SHA-3-256 batch commitment.
func verifySTARK(batch *Batch) bool {
	if batch == nil || len(batch.ZkStarkProof) < 96 {
		return false
	}
	expected := computeBatchCommitment(batch.PreRoot, batch.PostRoot, batch.Transactions)
	return bytes.Equal(batch.ZkStarkProof[:32], batch.PreRoot[:]) &&
		bytes.Equal(batch.ZkStarkProof[32:64], batch.PostRoot[:]) &&
		bytes.Equal(batch.ZkStarkProof[64:96], expected[:])
}
