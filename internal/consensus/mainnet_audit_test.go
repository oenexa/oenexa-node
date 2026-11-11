package consensus

import (
	"strings"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/state"
)

func TestEngine_RejectsFutureTimestampDrift(t *testing.T) {
	w0, _ := crypto.NewWallet()
	w1, _ := crypto.NewWallet()

	vs, err := NewValidatorSet([]*Validator{
		{Address: w0.Address, PublicKey: w0.PublicKey, VotingPower: 10},
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: 10},
	})
	if err != nil {
		t.Fatalf("NewValidatorSet: %v", err)
	}

	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w0.Address, w1.Address}).Build()
	st := state.NewStateDB()
	pool := mempool.New(100)

	// Proposer for height 1 is w1 (1 % 2 == 1).
	e0 := NewEngine(w0.Address, w0.PublicKey, w0.PrivateKey, vs, st.Snapshot(), pool, genesis, nil, nil)

	futureTS := time.Now().Add(1 * time.Hour).UnixNano()
	badBlk, err := core.NewBlock(1, genesis.Hash, genesis.Header.StateRoot, futureTS, w1.Address, nil, 0, core.InitialBaseFee, 0)
	if err != nil {
		t.Fatalf("NewBlock: %v", err)
	}
	if err := badBlk.SignHeader(w1.PrivateKey); err != nil {
		t.Fatalf("SignHeader: %v", err)
	}

	err = e0.ProcessProposal(badBlk)
	if err == nil || !strings.Contains(err.Error(), "exceeds max future drift") {
		t.Fatalf("expected future drift rejection, got: %v", err)
	}
}

func TestEngine_ViewChangeRoundTimeoutRotatesProposer(t *testing.T) {
	w0, _ := crypto.NewWallet()
	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()

	vs, err := NewValidatorSet([]*Validator{
		{Address: w0.Address, PublicKey: w0.PublicKey, VotingPower: 10},
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: 10},
		{Address: w2.Address, PublicKey: w2.PublicKey, VotingPower: 10},
	})
	if err != nil {
		t.Fatalf("NewValidatorSet: %v", err)
	}

	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w0.Address, w1.Address, w2.Address}).Build()
	st := state.NewStateDB()
	pool := mempool.New(100)

	// At height 1, round 0 proposer is w1 (1%3=1). Simulate w1 offline -> view change to round 1 -> proposer w2 (2%3=2).
	e0 := NewEngine(w0.Address, w0.PublicKey, w0.PrivateKey, vs, st.Snapshot(), pool, genesis, nil, nil)
	e2 := NewEngine(w2.Address, w2.PublicKey, w2.PrivateKey, vs, st.Snapshot(), pool, genesis, nil, nil)

	if got := vs.ProposerForRound(1, 0).Address; got != w1.Address {
		t.Fatalf("expected w1 as round 0 proposer")
	}
	if got := vs.ProposerForRound(1, 1).Address; got != w2.Address {
		t.Fatalf("expected w2 as round 1 proposer")
	}

	if newRound := e0.AdvanceRound(); newRound != 1 {
		t.Fatalf("expected round 1, got %d", newRound)
	}
	if newRound := e2.AdvanceRound(); newRound != 1 {
		t.Fatalf("expected round 1 on e2, got %d", newRound)
	}
	if err := e2.produceBlock(t.Context()); err != nil {
		t.Fatalf("produceBlock after view change: %v", err)
	}
	if e2.proposal == nil || e2.proposal.Header.ValidatorAddr != w2.Address {
		t.Fatal("expected w2 to propose after view change")
	}
	if err := e0.ProcessProposal(e2.proposal); err != nil {
		t.Fatalf("e0 ProcessProposal after view change: %v", err)
	}
}

func TestEngine_PrecommitQuorumReplaysProposalSafely(t *testing.T) {
	w0, _ := crypto.NewWallet()
	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()

	vs, _ := NewValidatorSet([]*Validator{
		{Address: w0.Address, PublicKey: w0.PublicKey, VotingPower: 10},
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: 10},
		{Address: w2.Address, PublicKey: w2.PublicKey, VotingPower: 10},
	})
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w0.Address, w1.Address, w2.Address}).Build()
	st := state.NewStateDB()
	pool := mempool.New(100)

	e0 := NewEngine(w0.Address, w0.PublicKey, w0.PrivateKey, vs, st.Snapshot(), pool, genesis, nil, nil)
	e1 := NewEngine(w1.Address, w1.PublicKey, w1.PrivateKey, vs, st.Snapshot(), pool, genesis, nil, nil)

	// e1 is proposer for height 1. Build valid proposal via e1.
	if err := e1.produceBlock(t.Context()); err != nil {
		t.Fatalf("produceBlock: %v", err)
	}
	prop := e1.proposal

	// e0 receives proposal, then simulate proposalState clearing before quorum arrives.
	if err := e0.ProcessProposal(prop); err != nil {
		t.Fatalf("ProcessProposal: %v", err)
	}
	e0.mu.Lock()
	e0.proposalState = nil
	e0.mu.Unlock()

	for _, val := range []*crypto.Wallet{w0, w1, w2} {
		v := &Vote{
			Type:      VotePrecommit,
			Height:    1,
			Round:     0,
			BlockHash: prop.Hash,
			Voter:     val.Address,
			PublicKey: val.PublicKey,
		}
		if err := v.Sign(val.PrivateKey); err != nil {
			t.Fatalf("Sign vote: %v", err)
		}
		if err := e0.ProcessVote(v); err != nil {
			t.Fatalf("ProcessVote: %v", err)
		}
	}

	if e0.Height() != 2 {
		t.Fatalf("expected height 2 after replayed precommit commit, got %d", e0.Height())
	}
	if e0.BlockByHeight(1).Header.StateRoot != prop.Header.StateRoot {
		t.Fatal("committed block state root mismatch")
	}
}
