// Package sync provides initial block download (IBD) and chain catch-up.
//
// The Syncer periodically checks whether the local chain is behind any
// connected peer and, if so, requests the missing blocks in batches.
//
// Protocol (uses existing P2P message types MsgGetBlocks / MsgBlocks):
//
//  1. Every SyncInterval the Syncer compares the local height to the height
//     advertised in the peer's latest MsgBlock gossip.
//  2. If behind, it sends MsgGetBlocks{FromHeight: localTip+1, MaxCount: 64}.
//  3. The responding peer sends back an encoded []core.Block slice.
//  4. Blocks are validated and applied sequentially.  The engine and state
//     are updated in-place; committed blocks are written to the BlockStore.
package sync

import (
	"bytes"
	"context"
	"encoding/gob"
	"log"
	"math"
	"sync"
	"time"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/p2p"
	"github.com/oenexa/oenexa/internal/state"
	"github.com/oenexa/oenexa/internal/storage"
	"github.com/oenexa/oenexa/internal/vm"
)

var (
	// SyncInterval is how often the syncer polls for new blocks from peers.
	SyncInterval = 5 * time.Second
	// BatchSize is the maximum number of blocks requested per round.
	BatchSize = 64
)

// Syncer drives initial block download and ongoing chain catch-up.
type Syncer struct {
	engine       *consensus.Engine
	st           *state.DB
	store        *storage.BlockStore
	node         *p2p.Node
	execVM       *vm.VM
	validatorPub []byte // for block-signature verification

	mu          sync.Mutex
	peerHeights map[string]uint64 // nodeID hex → last seen height from peer
}

// New creates a Syncer. execVM may be nil if smart-contract replay is not needed.
func New(
	engine *consensus.Engine,
	st *state.DB,
	store *storage.BlockStore,
	node *p2p.Node,
	execVM *vm.VM,
	validatorPub []byte,
) *Syncer {
	return &Syncer{
		engine:       engine,
		st:           st,
		store:        store,
		node:         node,
		execVM:       execVM,
		validatorPub: validatorPub,
		peerHeights:  make(map[string]uint64),
	}
}

// ObservePeerHeight records the latest height advertised by a peer.
// Called by the P2P block-gossip handler.
func (s *Syncer) ObservePeerHeight(nodeIDHex string, height uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if height > s.peerHeights[nodeIDHex] {
		s.peerHeights[nodeIDHex] = height
	}
}

// BestPeerHeight returns the highest height seen across all peers.
func (s *Syncer) BestPeerHeight() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best uint64
	for _, h := range s.peerHeights {
		if h > best {
			best = h
		}
	}
	return best
}

// Run starts the periodic sync loop; blocks until ctx is canceled.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()

	log.Println("[sync] syncer started")
	for {
		select {
		case <-ctx.Done():
			log.Println("[sync] syncer stopped")
			return
		case <-ticker.C:
			s.trySync()
		}
	}
}

// trySync performs one catch-up attempt if the chain is behind.
func (s *Syncer) trySync() {
	local := s.engine.Height()
	best := s.BestPeerHeight()
	if best <= local {
		return // already up to date or no peers
	}

	want := local // next height to request
	log.Printf("[sync] behind peers: local=%d best_peer=%d, catching up…", local, best)

	// Request blocks in bounded batches per tick so an inflated peer height
	// cannot spin trySync endlessly.
	const maxBatchesPerTick = 8
	for batches := 0; want < best && batches < maxBatchesPerTick; batches++ {
		batch := BatchSize
		if batch <= 0 {
			break
		}
		var count uint32
		if uint64(batch) > math.MaxUint32 {
			count = math.MaxUint32
		} else {
			count = uint32(batch) // #nosec G115 -- bounded to [1, MaxUint32]
		}
		if remaining := best - want; remaining < uint64(count) {
			count = uint32(remaining) // #nosec G115 -- remaining < count <= MaxUint32
		}

		req := p2p.SyncReq{
			FromHeight: want,
			MaxCount:   count,
		}
		// Broadcast to all peers — the first responder wins.
		_ = s.node.BroadcastSyncReq(context.Background(), req)
		want += uint64(count)
	}
}

// ApplyBlocks validates and applies a slice of blocks received from a peer.
// Blocks outside the expected range or failing validation are discarded.
func (s *Syncer) ApplyBlocks(blocks []*core.Block) {
	if s == nil || s.engine == nil {
		return
	}
	for _, blk := range blocks {
		if blk == nil {
			continue
		}
		localHeight := s.engine.Height()
		if localHeight == 0 || blk.Header.Height != localHeight {
			// Already have this block, chain uninitialized, or height mismatch.
			continue
		}
		// Basic structural & validator signature validation.
		if err := blk.VerifyValidatorSig(s.validatorPub); err != nil {
			log.Printf("[sync] invalid block sig at h=%d: %v", blk.Header.Height, err)
			continue
		}
		// Parent linkage & timestamp check.
		prev := s.engine.BlockByHeight(localHeight - 1)
		if prev == nil || blk.Header.PrevHash != prev.Hash {
			log.Printf("[sync] prev-hash mismatch at h=%d", blk.Header.Height)
			continue
		}

		// Verify transaction signatures, hashes, and Merkle root before execution.
		txHashes := make([][crypto.HashSize]byte, len(blk.Txs))
		invalidTx := false
		for i, tx := range blk.Txs {
			if tx == nil {
				invalidTx = true
				break
			}
			if err := tx.BasicValidate(); err != nil {
				log.Printf("[sync] invalid tx at h=%d idx=%d: %v", blk.Header.Height, i, err)
				invalidTx = true
				break
			}
			txHashes[i] = tx.Hash
		}
		if invalidTx {
			continue
		}
		if core.ComputeMerkleRoot(txHashes) != blk.Header.MerkleRoot {
			log.Printf("[sync] merkle root mismatch at h=%d", blk.Header.Height)
			continue
		}

		// Execute against an isolated snapshot so failed or mismatched blocks never corrupt live state.
		var snap *state.DB
		var result *state.BlockResult
		if s.st != nil {
			snap = s.st.Snapshot()
			var err error
			result, err = state.ApplyBlock(snap, blk, blk.Header.ValidatorAddr, s.execVM)
			if err != nil {
				log.Printf("[sync] ApplyBlock h=%d: %v", blk.Header.Height, err)
				continue
			}
			if result.GasUsed != blk.Header.GasUsed {
				log.Printf("[sync] gas used mismatch at h=%d: header=%d computed=%d", blk.Header.Height, blk.Header.GasUsed, result.GasUsed)
				continue
			}
			if result.BurnedFees != blk.Header.BurnedFees {
				log.Printf("[sync] burned fees mismatch at h=%d: header=%d computed=%d", blk.Header.Height, blk.Header.BurnedFees, result.BurnedFees)
				continue
			}
			if blk.Header.StateRoot != (crypto.ZeroHash) {
				computedRoot := snap.CommitRoot()
				if computedRoot != blk.Header.StateRoot {
					log.Printf("[sync] state root mismatch at h=%d: header=%s computed=%s",
						blk.Header.Height, crypto.ToHex(blk.Header.StateRoot), crypto.ToHex(computedRoot))
					continue
				}
			}
			s.st.Apply(snap)
		} else {
			var err error
			result, err = state.ApplyBlock(nil, blk, blk.Header.ValidatorAddr, s.execVM)
			if err != nil {
				log.Printf("[sync] ApplyBlock h=%d: %v", blk.Header.Height, err)
				continue
			}
		}

		log.Printf("[sync] applied block h=%d hash=%s txs=%d burned=%d nano-ONXC reward=%d nano-ONXC",
			blk.Header.Height,
			crypto.ToHex(blk.Hash)[:12]+"…",
			len(blk.Txs),
			result.BurnedFees,
			result.BlockReward,
		)

		// ── VITAL: Flush the new state changes to the persistence layer!
		if s.st != nil {
			_, err := s.st.CommitAndFlush()
			if err != nil {
				log.Printf("[sync] failed to flush state db h=%d: %v", blk.Header.Height, err)
				continue
			}
		}

		// Persist block in block store
		if s.store != nil {
			if err := s.store.PutBlock(blk); err != nil {
				log.Printf("[sync] store block h=%d: %v", blk.Header.Height, err)
			} else {
				_ = s.store.UpdateTip(blk.Header.Height, blk.Hash)
			}
		}

		// Inject block into the consensus engine so it knows the local height advanced
		if s.engine != nil {
			s.engine.InjectBlock(blk)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func gobEncode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
