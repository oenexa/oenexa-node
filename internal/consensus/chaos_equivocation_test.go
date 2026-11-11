package consensus

import (
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/state"
)

func TestChaos_ThreeValidatorPartitionEquivocationAndRecovery(t *testing.T) {
	// 3 validators with powers [40, 40, 20] total=100 -> >2/3 quorum strictly requires 67+.
	// {v0,v1}=80 holds quorum, while {v2}=20 minority partition cannot finalize alone.
	w0, _ := crypto.NewWallet()
	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()

	vs, err := NewValidatorSet([]*Validator{
		{Address: w0.Address, PublicKey: w0.PublicKey, VotingPower: 40},
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: 40},
		{Address: w2.Address, PublicKey: w2.PublicKey, VotingPower: 20},
	})
	if err != nil {
		t.Fatalf("NewValidatorSet: %v", err)
	}
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w0.Address, w1.Address, w2.Address}).Build()

	st0 := state.NewStateDB()
	pool0 := mempool.New(0)
	eng0 := NewEngine(w0.Address, w0.PublicKey, w0.PrivateKey, vs, st0, pool0, genesis, nil, nil)

	// Build block at height 1
	blk1, snap, err := eng0.BuildBlock(1)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	_ = snap

	// Minority validator v2 cannot achieve quorum alone -> height remains 1
	vMinority := &Vote{Type: VotePrecommit, Height: 1, Round: 0, BlockHash: blk1.Hash, Voter: w2.Address, PublicKey: w2.PublicKey}
	vMinority.Signature, _ = crypto.Sign(w2.PrivateKey, vMinority.payload())
	if err := eng0.ProcessVote(vMinority); err != nil {
		t.Fatalf("ProcessVote minority: %v", err)
	}
	if vs.HasQuorum([][crypto.AddressSize]byte{w2.Address}) {
		t.Fatal("minority partition must not reach 2/3 BFT quorum")
	}

	// Simulate Byzantine equivocation by w2 signing conflicting block hash at same height/round/type
	badHash := crypto.Hash256([]byte("forked-block"))
	vEquiv := &Vote{Type: VotePrecommit, Height: 1, Round: 0, BlockHash: badHash, Voter: w2.Address, PublicKey: w2.PublicKey}
	vEquiv.Signature, _ = crypto.Sign(w2.PrivateKey, vEquiv.payload())
	if err := eng0.ProcessVote(vEquiv); err == nil {
		t.Fatal("expected ProcessVote to reject Byzantine double-vote equivocation")
	}

	// Partition heals: v0 + v1 form >2/3 supermajority quorum (80/100) and commit advances height
	if !vs.HasQuorum([][crypto.AddressSize]byte{w0.Address, w1.Address}) {
		t.Fatal("majority partition {v0,v1} should reach quorum")
	}
	if err := eng0.Commit(blk1, snap); err != nil {
		t.Fatalf("Commit after partition heal: %v", err)
	}
	if eng0.Height() != 2 || eng0.BlockByHash(blk1.Hash) == nil {
		t.Fatalf("expected committed height 2 and retrievable block by hash")
	}
	if eng0.ValidatorAddr() != w0.Address {
		t.Fatal("ValidatorAddr mismatch")
	}

	// Also exercise NewEngineWithSigner + SetSigner + Signer + KMSSigner SignBlock
	signer := NewKMSSigner(w0.Address, w0.PublicKey, "kms-test", func(p []byte) ([]byte, error) {
		return crypto.Sign(w0.PrivateKey, p)
	})
	engSigner := NewEngineWithSigner(signer, vs, state.NewStateDB(), mempool.New(0), genesis, nil, nil)
	if engSigner.Signer() == nil || engSigner.Signer().Address() != w0.Address || len(engSigner.Signer().PublicKey()) == 0 {
		t.Fatal("expected active KMS signer")
	}
	blkSigTest := &core.Block{Header: core.BlockHeader{Height: 2, PrevHash: blk1.Hash, Timestamp: time.Now().UnixNano(), ValidatorAddr: w0.Address}}
	if err := signer.SignBlock(blkSigTest); err != nil {
		t.Fatalf("KMSSigner.SignBlock failed: %v", err)
	}
	engSigner.SetSigner(NewLocalSigner(w0.Address, w0.PublicKey, w0.PrivateKey))
}
