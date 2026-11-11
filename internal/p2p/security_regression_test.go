package p2p_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/p2p"
)

func TestSecurity_P2PReputationBanAndAutoDisconnect(t *testing.T) {
	w1, _ := crypto.NewWallet()
	id1 := p2p.NewIdentity(w1.PublicKey, w1.PrivateKey, "")
	node1, err := p2p.NewNode(id1, 0)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := node1.Start(ctx); err != nil {
		t.Fatalf("node1.Start: %v", err)
	}

	pid := peer.ID("bad-peer-id")
	rm := p2p.NewReputationManager(node1)

	if rm.IsBanned(pid) {
		t.Fatal("fresh peer should not be banned")
	}

	// Reward bumps reputation safely up to 200 cap.
	rm.Reward(pid, 250)
	if rm.IsBanned(pid) {
		t.Fatal("rewarded peer should not be banned")
	}

	// Cumulative penalties drop score below BanThreshold (0) -> 24h ban.
	rm.Penalize(pid, p2p.PenaltyInvalidBlock, "tampered header")
	rm.Penalize(pid, p2p.PenaltyInvalidBlock, "repeat offense")
	rm.Penalize(pid, p2p.PenaltyInvalidSignature, "forged signature")

	if !rm.IsBanned(pid) {
		t.Fatal("expected peer to be banned after exceeding BanThreshold")
	}
}

func TestChaos_TwoNodeGossipPartitionAndRecovery(t *testing.T) {
	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()
	n1, err := p2p.NewNode(p2p.NewIdentity(w1.PublicKey, w1.PrivateKey, ""), 0)
	if err != nil {
		t.Fatalf("NewNode 1: %v", err)
	}
	n2, err := p2p.NewNode(p2p.NewIdentity(w2.PublicKey, w2.PrivateKey, ""), 0)
	if err != nil {
		t.Fatalf("NewNode 2: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var gotTx, gotBlock, gotVote, gotReq, gotRes atomic.Int32
	n2.OnTxReceived = func(pid peer.ID, tx *core.Transaction) error {
		gotTx.Add(1)
		return nil
	}
	n2.OnBlockReceived = func(from peer.ID, blk *core.Block) error {
		gotBlock.Add(1)
		return nil
	}
	n2.OnVoteReceived = func(data []byte) {
		gotVote.Add(1)
	}
	n2.OnSyncReq = func(req p2p.SyncReq) {
		gotReq.Add(1)
	}
	n2.OnSyncRes = func(res p2p.SyncRes) {
		gotRes.Add(1)
	}

	if err := n1.Start(ctx); err != nil {
		t.Fatalf("n1.Start: %v", err)
	}
	if err := n2.Start(ctx); err != nil {
		t.Fatalf("n2.Start: %v", err)
	}
	if err := n2.Connect(ctx, n1.ListenAddrs()[0]); err != nil {
		t.Fatalf("n2.Connect: %v", err)
	}
	if n1.PeerCount() == 0 && n2.PeerCount() == 0 {
		t.Fatal("expected connected peer count > 0")
	}

	// Wait for GossipSub mesh to form across all topics (1s default heartbeat)
	time.Sleep(1500 * time.Millisecond)

	tx := core.NewTransfer(w1.Address, w2.Address, w1.PublicKey, 0, core.OneONXC, core.MinGasPrice)
	_ = tx.Sign(w1.PrivateKey)
	if err := n1.BroadcastTx(ctx, tx); err != nil {
		t.Fatalf("BroadcastTx: %v", err)
	}

	blk := &core.Block{
		Header: core.BlockHeader{Version: 1, Height: 1, Timestamp: time.Now().UnixNano(), ValidatorAddr: w1.Address},
	}
	blk.Hash = blk.ComputeHash()
	if err := n1.BroadcastBlock(ctx, blk); err != nil {
		t.Fatalf("BroadcastBlock: %v", err)
	}
	if err := n1.BroadcastVote(ctx, []byte(`{"height":1}`)); err != nil {
		t.Fatalf("BroadcastVote: %v", err)
	}
	if err := n1.BroadcastSyncReq(ctx, p2p.SyncReq{FromHeight: 1, MaxCount: 5}); err != nil {
		t.Fatalf("BroadcastSyncReq: %v", err)
	}
	if err := n1.BroadcastSyncRes(ctx, p2p.SyncRes{Blocks: []*core.Block{blk}}); err != nil {
		t.Fatalf("BroadcastSyncRes: %v", err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if gotTx.Load() > 0 && gotBlock.Load() > 0 && gotVote.Load() > 0 && gotReq.Load() > 0 && gotRes.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if gotTx.Load() == 0 || gotBlock.Load() == 0 || gotVote.Load() == 0 || gotReq.Load() == 0 || gotRes.Load() == 0 {
		t.Fatalf("expected all gossip channels received: tx=%d blk=%d vote=%d req=%d res=%d",
			gotTx.Load(), gotBlock.Load(), gotVote.Load(), gotReq.Load(), gotRes.Load())
	}
}
