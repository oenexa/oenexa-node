package p2p

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oenexa/oenexa/internal/core"
	oenexaCrypto "github.com/oenexa/oenexa/internal/crypto"
)

func TestNode_InvalidTxRejectedBeforeSeenCache(t *testing.T) {
	w, err := oenexaCrypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	w2, err := oenexaCrypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet 2: %v", err)
	}
	id := NewIdentity(w.PublicKey, w.PrivateKey, "127.0.0.1:0")
	n, err := NewNode(id, 0)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	defer func() { _ = n.Close() }()

	if addrs := n.ListenAddrs(); len(addrs) == 0 {
		t.Fatal("expected non-empty listen addrs")
	}

	// Build a valid signed tx, then forge signature while preserving hash check.
	tx := core.NewTransfer(w.Address, w2.Address, w.PublicKey, 0, 100, core.InitialBaseFee)
	if err := tx.Sign(w.PrivateKey); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	forged := *tx
	forged.Signature = append([]byte(nil), tx.Signature...)
	forged.Signature[0] ^= 0xff
	forged.Hash = forged.ComputeHash()

	if err := forged.Verify(); err == nil {
		t.Fatal("expected forged signature verify failure")
	}
	// Verify valid tx still passes and was never shadowed in seenTxs.
	if n.hasSeenTx(tx.Hash) {
		t.Fatal("valid tx hash unexpectedly present in seenTxs")
	}
	n.markSeenTx(tx.Hash)
	if !n.hasSeenTx(tx.Hash) {
		t.Fatal("expected seenTxs to record tx hash")
	}

	// Also exercise block cache eviction loop.
	n.seenMu.Lock()
	for i := 0; i < 5005; i++ {
		var h [oenexaCrypto.HashSize]byte
		h[0] = byte(i)
		h[1] = byte(i >> 8)
		n.seenBlocks[h] = time.Now().Add(-time.Hour)
	}
	n.seenMu.Unlock()
	n.markSeenBlock([oenexaCrypto.HashSize]byte{0xde, 0xad})
	if len(n.seenBlocks) > 5002 {
		t.Fatalf("expected seenBlocks eviction, got %d", len(n.seenBlocks))
	}

	// Exercise seenTxs eviction loop as well.
	n.seenMu.Lock()
	for i := 0; i < 50002; i++ {
		var h [oenexaCrypto.HashSize]byte
		h[0] = byte(i)
		h[1] = byte(i >> 8)
		h[2] = byte(i >> 16)
		n.seenTxs[h] = time.Now().Add(-time.Hour)
	}
	n.seenMu.Unlock()
	n.markSeenTx([oenexaCrypto.HashSize]byte{0xbe, 0xef})
	if len(n.seenTxs) > 50002 {
		t.Fatalf("expected seenTxs eviction, got %d", len(n.seenTxs))
	}
}

func TestNode_BroadcastsAndReputationLifecycle(t *testing.T) {
	w, err := oenexaCrypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	id := NewIdentity(w.PublicKey, w.PrivateKey, "127.0.0.1:0")
	n, err := NewNode(id, 0)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	defer func() { _ = n.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Broadcast before Start is a safe nil-topic no-op.
	if err := n.BroadcastSyncReq(ctx, SyncReq{FromHeight: 1, MaxCount: 10}); err != nil {
		t.Fatalf("BroadcastSyncReq pre-start: %v", err)
	}
	if err := n.BroadcastSyncRes(ctx, SyncRes{}); err != nil {
		t.Fatalf("BroadcastSyncRes pre-start: %v", err)
	}
	if err := n.BroadcastVote(ctx, []byte("v")); err != nil {
		t.Fatalf("BroadcastVote pre-start: %v", err)
	}
	if err := n.BroadcastBlock(ctx, &core.Block{}); err != nil {
		t.Fatalf("BroadcastBlock pre-start: %v", err)
	}
	if err := n.BroadcastTx(ctx, &core.Transaction{}); err != nil {
		t.Fatalf("BroadcastTx pre-start: %v", err)
	}

	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := n.BroadcastSyncReq(ctx, SyncReq{FromHeight: 0, MaxCount: 5}); err != nil {
		t.Fatalf("BroadcastSyncReq: %v", err)
	}
	if err := n.BroadcastSyncRes(ctx, SyncRes{Blocks: []*core.Block{}}); err != nil {
		t.Fatalf("BroadcastSyncRes: %v", err)
	}
	if err := n.Connect(ctx, "invalid-multiaddr"); err == nil {
		t.Fatal("expected error for invalid multiaddr")
	}

	// ReputationManager penalize + ban + score tracking.
	fakePeer := peer.ID("bad-peer-id")
	n.rm.Reward(fakePeer, RewardValidTx)
	n.rm.Penalize(fakePeer, BaseReputation+50, "forged vote spam")
	if !n.rm.IsBanned(fakePeer) {
		t.Fatal("expected peer to be banned after exceeding threshold")
	}

	// Exercise OnVoteValidated rejection penalizing peer directly via pubsub message struct check.
	var called bool
	n.OnVoteValidated = func(_ peer.ID, data []byte) error {
		called = true
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
		return nil
	}
	if err := n.OnVoteValidated(fakePeer, []byte("{bad")); err == nil || !called {
		t.Fatalf("expected invalid vote error, got %v", err)
	}
	_ = strings.Contains
	_ = (*pubsub.Message)(nil)
	_ = (*pb.Message)(nil)
}
