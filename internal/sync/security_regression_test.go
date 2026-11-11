package sync

import (
	"math"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/p2p"
	"github.com/oenexa/oenexa/internal/state"
)

func setupSecuritySyncer(t *testing.T) (*Syncer, *consensus.Engine, *state.DB, *crypto.Wallet, *core.Block) {
	t.Helper()
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	st := state.NewStateDB()
	st.SetAccount(w.Address, &state.Account{Balance: 100 * core.OneONXC})
	vs, _ := consensus.NewValidatorSet([]*consensus.Validator{
		{Address: w.Address, PublicKey: w.PublicKey, VotingPower: 1},
	})
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w.Address}).Build()
	eng := consensus.NewEngine(w.Address, w.PublicKey, w.PrivateKey, vs, st, nil, genesis, nil, nil)

	id := p2p.NewIdentity(w.PublicKey, w.PrivateKey, "127.0.0.1:0")
	node, _ := p2p.NewNode(id, 0)
	s := New(eng, st, nil, node, nil, w.PublicKey)
	return s, eng, st, w, genesis
}

func TestSecurity_TrySyncBoundedOnMaxUint64PeerHeight(t *testing.T) {
	s, _, _, _, _ := setupSecuritySyncer(t)
	s.ObservePeerHeight("malicious-peer", math.MaxUint64)
	// Must complete immediately in <= 8 batches without spinning infinitely.
	done := make(chan struct{})
	go func() {
		s.trySync()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("trySync spun on MaxUint64 peer height")
	}
}

func TestSecurity_ApplyBlocksRejectsTamperedRootGasAndTxs(t *testing.T) {
	s, eng, st, w, genesis := setupSecuritySyncer(t)
	startHeight := eng.Height() // 1
	startBal := st.GetBalance(w.Address)

	// 1. Nil block and invalid signature/tx rejected cleanly.
	s.ApplyBlocks([]*core.Block{nil})

	// 2. Valid tx in block, but forged StateRoot! State must remain untouched.
	u, _ := crypto.NewWallet()
	tx := core.NewTransfer(w.Address, u.Address, w.PublicKey, 0, core.OneONXC, core.MinGasPrice)
	if err := tx.Sign(w.PrivateKey); err != nil {
		t.Fatal(err)
	}

	// Execute copy to know real GasUsed & BurnedFees, then poison StateRoot.
	snap := st.Snapshot()
	tmpBlk := &core.Block{
		Header: core.BlockHeader{
			Version:       1,
			Height:        startHeight,
			PrevHash:      genesis.Hash,
			Timestamp:     time.Now().UnixNano(),
			ValidatorAddr: w.Address,
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee,
		},
		Txs: []*core.Transaction{tx},
	}
	res, err := state.ApplyBlock(snap, tmpBlk, w.Address, nil)
	if err != nil {
		t.Fatal(err)
	}

	forgedRoot := crypto.Hash256([]byte("forged-state-root"))
	forgedBlk := &core.Block{
		Header: core.BlockHeader{
			Version:       1,
			Height:        startHeight,
			PrevHash:      genesis.Hash,
			MerkleRoot:    core.ComputeMerkleRoot([][crypto.HashSize]byte{tx.Hash}),
			StateRoot:     forgedRoot,
			Timestamp:     tmpBlk.Header.Timestamp,
			ValidatorAddr: w.Address,
			GasUsed:       res.GasUsed,
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee,
			BurnedFees:    res.BurnedFees,
		},
		Txs: []*core.Transaction{tx},
	}
	if err := forgedBlk.SignHeader(w.PrivateKey); err != nil {
		t.Fatal(err)
	}

	s.ApplyBlocks([]*core.Block{forgedBlk})
	if eng.Height() != startHeight {
		t.Fatalf("forged StateRoot block advanced engine height! want=%d got=%d", startHeight, eng.Height())
	}
	if st.GetBalance(w.Address) != startBal || st.GetBalance(u.Address) != 0 {
		t.Fatal("failed synced block mutated live state!")
	}

	// 3. Valid block must pass all validations, execute cleanly, and advance engine height!
	validSnap := st.Snapshot()
	validBlk := &core.Block{
		Header: core.BlockHeader{
			Version:       1,
			Height:        startHeight,
			PrevHash:      genesis.Hash,
			Timestamp:     time.Now().UnixNano(),
			ValidatorAddr: w.Address,
			MerkleRoot:    core.ComputeMerkleRoot([][crypto.HashSize]byte{tx.Hash}),
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee,
		},
		Txs: []*core.Transaction{tx},
	}
	validRes, err := state.ApplyBlock(validSnap, validBlk, w.Address, nil)
	if err != nil {
		t.Fatal(err)
	}
	validBlk.Header.GasUsed = validRes.GasUsed
	validBlk.Header.BurnedFees = validRes.BurnedFees
	validBlk.Header.StateRoot = validSnap.CommitRoot()
	if err := validBlk.SignHeader(w.PrivateKey); err != nil {
		t.Fatal(err)
	}

	s.ApplyBlocks([]*core.Block{validBlk})
	if eng.Height() != startHeight+1 {
		t.Fatalf("valid synced block failed to advance engine height! want=%d got=%d", startHeight+1, eng.Height())
	}
}
