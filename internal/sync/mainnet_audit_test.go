package sync

import (
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/p2p"
	"github.com/oenexa/oenexa/internal/state"
	"github.com/oenexa/oenexa/internal/storage"
)

func TestSyncer_RejectsInvalidSyncedBlocks(t *testing.T) {
	valW, _ := crypto.NewWallet()
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{valW.Address}).Build()

	st := state.NewStateDB()
	pool := mempool.New(100)
	vs, _ := consensus.NewValidatorSet([]*consensus.Validator{
		{Address: valW.Address, PublicKey: valW.PublicKey, VotingPower: 10},
	})
	eng := consensus.NewEngine(valW.Address, valW.PublicKey, valW.PrivateKey, vs, st, pool, genesis, nil, nil)

	bdb, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = bdb.Close() }()
	bs := storage.NewBlockStore(bdb)
	_ = bs.PutBlock(genesis)

	id := p2p.NewIdentity(valW.PublicKey, valW.PrivateKey, "127.0.0.1:0")
	p2pN, err := p2p.NewNode(id, 0)
	if err != nil {
		t.Fatalf("p2p.NewNode: %v", err)
	}
	defer func() { _ = p2pN.Close() }()

	s := New(eng, st, bs, p2pN, nil, valW.PublicKey)

	// Empty slice is a no-op.
	s.ApplyBlocks(nil)
	if eng.Height() != 1 {
		t.Fatalf("expected height 1 after empty ApplyBlocks, got %d", eng.Height())
	}

	// Wrong parent hash must fail.
	badParent, _ := core.NewBlock(1, [crypto.HashSize]byte{0x99}, genesis.Header.StateRoot, time.Now().UnixNano(), valW.Address, nil, 0, core.InitialBaseFee, 0)
	_ = badParent.SignHeader(valW.PrivateKey)
	s.ApplyBlocks([]*core.Block{badParent})
	if eng.Height() != 1 {
		t.Fatal("accepted block with wrong parent hash")
	}

	// Bad validator signature must fail.
	otherW, _ := crypto.NewWallet()
	badSig, _ := core.NewBlock(1, genesis.Hash, genesis.Header.StateRoot, time.Now().UnixNano(), valW.Address, nil, 0, core.InitialBaseFee, 0)
	_ = badSig.SignHeader(otherW.PrivateKey)
	s.ApplyBlocks([]*core.Block{badSig})
	if eng.Height() != 1 {
		t.Fatal("accepted block with invalid validator signature")
	}

	// Wrong state root must fail.
	badRoot, _ := core.NewBlock(1, genesis.Hash, [crypto.HashSize]byte{0xde}, time.Now().UnixNano(), valW.Address, nil, 0, core.InitialBaseFee, 0)
	_ = badRoot.SignHeader(valW.PrivateKey)
	s.ApplyBlocks([]*core.Block{badRoot})
	if eng.Height() != 1 {
		t.Fatal("accepted block with tampered state root")
	}

	// Corrupted Merkle root must fail.
	sender, _ := crypto.NewWallet()
	recv, _ := crypto.NewWallet()
	tx := core.NewTransfer(sender.Address, recv.Address, sender.PublicKey, 0, 10, core.InitialBaseFee)
	_ = tx.Sign(sender.PrivateKey)
	badMerkle, _ := core.NewBlock(1, genesis.Hash, genesis.Header.StateRoot, time.Now().UnixNano(), valW.Address, []*core.Transaction{tx}, 0, core.InitialBaseFee, 0)
	badMerkle.Header.MerkleRoot = [crypto.HashSize]byte{0xaa}
	badMerkle.Hash = badMerkle.ComputeHash()
	_ = badMerkle.SignHeader(valW.PrivateKey)
	s.ApplyBlocks([]*core.Block{badMerkle})
	if eng.Height() != 1 {
		t.Fatal("accepted block with tampered Merkle root")
	}
}
