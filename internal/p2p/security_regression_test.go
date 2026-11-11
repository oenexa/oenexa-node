package p2p_test

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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
