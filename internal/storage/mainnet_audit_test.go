package storage

import (
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

func TestBlockStore_CorruptTipAndHeightIndexNoPanic(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	bs := NewBlockStore(db)

	// Write a truncated/corrupt tip record and verify GetTip returns error without panic.
	if err := db.Put(keyTip, []byte{1, 2, 3}); err != nil {
		t.Fatalf("Put corrupt tip: %v", err)
	}
	if _, _, err := bs.GetTip(); err == nil {
		t.Fatal("expected error for corrupt tip record")
	}

	// Write an oversized tip record and verify GetTip returns error without panic.
	if err := db.Put(keyTip, make([]byte, 100)); err != nil {
		t.Fatalf("Put oversized tip: %v", err)
	}
	if _, _, err := bs.GetTip(); err == nil {
		t.Fatal("expected error for oversized tip record")
	}

	// Write a corrupt height->hash index entry and verify GetBlockByHeight fails cleanly.
	if err := db.Put(heightKey(99), []byte{0xaa, 0xbb}); err != nil {
		t.Fatalf("Put corrupt height index: %v", err)
	}
	if _, err := bs.GetBlockByHeight(99); err == nil {
		t.Fatal("expected error for corrupt height index")
	}
}

func TestStorage_CrashAndRestartPersistence(t *testing.T) {
	dir := t.TempDir()
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}

	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w.Address}).Build()
	var savedRoot [crypto.HashSize]byte

	{
		bdb, err := Open(dir + "/blocks")
		if err != nil {
			t.Fatalf("Open blocks: %v", err)
		}
		sdb, err := Open(dir + "/state")
		if err != nil {
			t.Fatalf("Open state: %v", err)
		}

		bs := NewBlockStore(bdb)
		ss := NewStateStore(sdb)

		st := state.NewStateDB()
		st.SetAccount(w.Address, &state.Account{Balance: 5_000_000, Nonce: 1})
		if err := ss.SaveState(st); err != nil {
			t.Fatalf("SaveState: %v", err)
		}
		hydrated, err := ss.LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		savedRoot = hydrated.CommitRoot()

		blk1, err := core.NewBlock(1, genesis.Hash, savedRoot, time.Now().UnixNano(), w.Address, nil, 0, core.InitialBaseFee, 0)
		if err != nil {
			t.Fatalf("NewBlock: %v", err)
		}
		if err := bs.PutBlock(genesis); err != nil {
			t.Fatalf("PutBlock genesis: %v", err)
		}
		if err := bs.PutBlock(blk1); err != nil {
			t.Fatalf("PutBlock blk1: %v", err)
		}
		if err := bs.UpdateTip(1, blk1.Hash); err != nil {
			t.Fatalf("UpdateTip: %v", err)
		}

		_ = bdb.Close()
		_ = sdb.Close()
	}

	// Reopen DBs and verify crash recovery state.
	bdb, err := Open(dir + "/blocks")
	if err != nil {
		t.Fatalf("Reopen blocks: %v", err)
	}
	defer func() { _ = bdb.Close() }()
	sdb, err := Open(dir + "/state")
	if err != nil {
		t.Fatalf("Reopen state: %v", err)
	}
	defer func() { _ = sdb.Close() }()

	bs := NewBlockStore(bdb)
	ss := NewStateStore(sdb)

	tipH, _, err := bs.GetTip()
	if err != nil || tipH != 1 {
		t.Fatalf("expected persisted tip height 1, got (%d, %v)", tipH, err)
	}
	reloadedBlk, err := bs.GetBlockByHeight(1)
	if err != nil || reloadedBlk.Header.StateRoot != savedRoot {
		t.Fatalf("reloaded block mismatch: err=%v", err)
	}
	reloadedState, err := ss.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if reloadedState.CommitRoot() != savedRoot {
		t.Fatal("reloaded state SMT root mismatch")
	}
	acc := reloadedState.GetAccount(w.Address)
	if acc.Balance != 5_000_000 || acc.Nonce != 1 {
		t.Fatalf("reloaded account mismatch: %+v", acc)
	}
}
