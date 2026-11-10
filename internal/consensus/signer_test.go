package consensus_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

func TestLocalSigner_AntiSlashing(t *testing.T) {
	wallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	signer := consensus.NewLocalSigner(wallet.Address, wallet.PublicKey, wallet.PrivateKey)
	if signer.Address() != wallet.Address {
		t.Fatalf("address mismatch")
	}
	if len(signer.PublicKey()) != len(wallet.PublicKey) {
		t.Fatalf("pubkey mismatch")
	}

	blockHashA := crypto.Hash256([]byte("blockA"))
	blockHashB := crypto.Hash256([]byte("blockB"))

	// 1. Initial vote at Height 1 Round 0
	vote1 := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: blockHashA,
		Voter:     wallet.Address,
		PublicKey: wallet.PublicKey,
	}
	if err := signer.SignVote(vote1); err != nil {
		t.Fatalf("initial vote failed: %v", err)
	}

	// 2. Attempt equivocation: same height & round, different block hash!
	voteEquivocation := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: blockHashB,
		Voter:     wallet.Address,
		PublicKey: wallet.PublicKey,
	}
	err = signer.SignVote(voteEquivocation)
	if err == nil {
		t.Fatalf("expected anti-slashing equivocation error, got nil")
	}
	if !errors.Is(err, consensus.ErrEquivocationPrevented) {
		t.Fatalf("expected ErrEquivocationPrevented, got %v", err)
	}

	// 3. Attempt stale vote: height 0 < height 1
	staleVote := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    0,
		Round:     0,
		BlockHash: blockHashA,
		Voter:     wallet.Address,
		PublicKey: wallet.PublicKey,
	}
	err = signer.SignVote(staleVote)
	if err == nil {
		t.Fatalf("expected anti-slashing stale error, got nil")
	}
	if !errors.Is(err, consensus.ErrStaleVote) {
		t.Fatalf("expected ErrStaleVote, got %v", err)
	}

	// 4. Block proposal anti-slashing
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{wallet.Address}).Build()
	blk1, err := core.NewBlock(1, genesis.Hash, [crypto.HashSize]byte{1}, time.Now().UnixNano(), wallet.Address, nil, 0, 10, 0)
	if err != nil {
		t.Fatalf("new block: %v", err)
	}
	if err := signer.SignBlock(blk1); err != nil {
		t.Fatalf("sign block 1: %v", err)
	}

	// Stale block proposal (height 0)
	blkStale, _ := core.NewBlock(0, genesis.Hash, [crypto.HashSize]byte{1}, time.Now().UnixNano(), wallet.Address, nil, 0, 10, 0)
	if err := signer.SignBlock(blkStale); err == nil {
		t.Fatalf("expected stale block proposal error")
	}

	// Conflicting block proposal at same height
	blkConflict, _ := core.NewBlock(1, genesis.Hash, [crypto.HashSize]byte{2}, time.Now().UnixNano(), wallet.Address, nil, 0, 10, 0)
	if err := signer.SignBlock(blkConflict); err == nil {
		t.Fatalf("expected conflicting block proposal error")
	}
}

func TestKMSSigner_RemoteIntegration(t *testing.T) {
	wallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	remoteSignCalls := 0
	remoteSignFn := func(payload []byte) ([]byte, error) {
		remoteSignCalls++
		return crypto.Sign(wallet.PrivateKey, payload)
	}

	kmsSigner := consensus.NewKMSSigner(
		wallet.Address,
		wallet.PublicKey,
		"arn:aws:kms:us-east-1:123456789012:key/oenexa-validator-key-1",
		remoteSignFn,
	)

	if kmsSigner.KeyURI() != "arn:aws:kms:us-east-1:123456789012:key/oenexa-validator-key-1" {
		t.Fatalf("key URI mismatch")
	}

	blockHashA := crypto.Hash256([]byte("kmsBlockA"))
	vote1 := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    10,
		Round:     0,
		BlockHash: blockHashA,
		Voter:     wallet.Address,
		PublicKey: wallet.PublicKey,
	}

	if err := kmsSigner.SignVote(vote1); err != nil {
		t.Fatalf("kms sign vote failed: %v", err)
	}
	if remoteSignCalls != 1 {
		t.Fatalf("expected 1 remote sign call, got %d", remoteSignCalls)
	}

	// Anti-slashing check in KMS signer should prevent dispatch to remote
	blockHashB := crypto.Hash256([]byte("kmsBlockB"))
	voteEquiv := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    10,
		Round:     0,
		BlockHash: blockHashB,
		Voter:     wallet.Address,
		PublicKey: wallet.PublicKey,
	}
	if err := kmsSigner.SignVote(voteEquiv); err == nil {
		t.Fatalf("expected KMS anti-slashing error")
	}
	// Verify remote signer was never called for equivocation attempt!
	if remoteSignCalls != 1 {
		t.Fatalf("remote sign called despite equivocation attempt: calls=%d", remoteSignCalls)
	}
}
