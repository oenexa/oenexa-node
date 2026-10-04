package p2p

import (
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// BaseReputation is the starting reputation for a new peer
	BaseReputation = 100
	// BanThreshold is the score at which a peer is banned
	BanThreshold = 0

	// Penalties
	PenaltyInvalidSignature  = 50
	PenaltyInvalidBlock      = 100
	PenaltyInvalidTxFormat   = 20
	PenaltySpam              = 10

	// Rewards
	RewardValidBlock = 5
	RewardValidTx    = 1
)

type PeerReputation struct {
	Score     int
	BannedUntil time.Time
}

// ReputationManager tracks and manages peer reputations to protect against
// Sybil and Eclipse attacks.
type ReputationManager struct {
	mu    sync.RWMutex
	peers map[peer.ID]*PeerReputation
	node  *Node
}

// NewReputationManager creates a new reputation manager.
func NewReputationManager(n *Node) *ReputationManager {
	return &ReputationManager{
		peers: make(map[peer.ID]*PeerReputation),
		node:  n,
	}
}

// Penalize reduces a peer's reputation score. If it drops below the BanThreshold,
// the peer is disconnected and banned for 24 hours.
func (rm *ReputationManager) Penalize(p peer.ID, penalty int, reason string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rep, exists := rm.peers[p]
	if !exists {
		rep = &PeerReputation{Score: BaseReputation}
		rm.peers[p] = rep
	}

	rep.Score -= penalty
	log.Printf("[p2p] Penalized peer %s by %d for %s (Score: %d)", p.String(), penalty, reason, rep.Score)

	if rep.Score <= BanThreshold && time.Now().After(rep.BannedUntil) {
		log.Printf("[p2p] Banning peer %s for 24 hours due to low reputation", p.String())
		rep.BannedUntil = time.Now().Add(24 * time.Hour)
		
		// Disconnect from the malicious peer
		if rm.node != nil && rm.node.host != nil {
			_ = rm.node.host.Network().ClosePeer(p)
		}
	}
}

// Reward increases a peer's reputation score, up to a maximum.
func (rm *ReputationManager) Reward(p peer.ID, reward int) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rep, exists := rm.peers[p]
	if !exists {
		rep = &PeerReputation{Score: BaseReputation}
		rm.peers[p] = rep
	}

	rep.Score += reward
	if rep.Score > 200 { // Max cap
		rep.Score = 200
	}
}

// IsBanned checks if a peer is currently serving a ban.
func (rm *ReputationManager) IsBanned(p peer.ID) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	rep, exists := rm.peers[p]
	if !exists {
		return false
	}
	return time.Now().Before(rep.BannedUntil)
}
