package rollup_test

import (
	"math"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/rollup"
	"github.com/oenexa/oenexa/internal/state"
)

func TestSecurity_RollupProofAndReplayAndOverflow(t *testing.T) {
	seq := rollup.NewSequencer()
	l2State := seq.GetL2State()

	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()
	l2State.SetAccount(w1.Address, &state.Account{Balance: 10_000, Nonce: 0})
	l2State.SetAccount(w2.Address, &state.Account{Balance: math.MaxUint64 - 50, Nonce: 0})

	genRoot := l2State.CommitRoot()
	seq.SetLastRoot(genRoot)
	l2State.SetAccount(w1.Address, &state.Account{Balance: 10_000, Nonce: 0})
	l2State.SetAccount(w2.Address, &state.Account{Balance: math.MaxUint64 - 50, Nonce: 0})

	bridge := rollup.NewL1Bridge(genRoot)

	// nil tx rejected cleanly
	if err := seq.SubmitTransaction(nil); err == nil {
		t.Fatal("expected nil tx rejection")
	}

	// 1. Self-transfer does NOT inflate balance (from == to guard)
	selfTx := &core.Transaction{
		Version: 1, Type: core.TxTransfer, Nonce: 0,
		From: w1.Address, To: w1.Address, Amount: 5000,
		Timestamp: time.Now().Unix(), PublicKey: w1.PublicKey,
	}
	if err := selfTx.Sign(w1.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if err := seq.SubmitTransaction(selfTx); err != nil {
		t.Fatalf("submit selfTx: %v", err)
	}

	// 2. Stale/replayed nonce (Nonce=0 again after Nonce=0 advances to 1) & overflow recipient excluded from batch.
	replayTx := &core.Transaction{
		Version: 1, Type: core.TxTransfer, Nonce: 0,
		From: w1.Address, To: w2.Address, Amount: 10,
		Timestamp: time.Now().Unix() + 1, PublicKey: w1.PublicKey,
	}
	_ = replayTx.Sign(w1.PrivateKey)
	_ = seq.SubmitTransaction(replayTx)

	overflowTx := &core.Transaction{
		Version: 1, Type: core.TxTransfer, Nonce: 1,
		From: w1.Address, To: w2.Address, Amount: 100, // overflows w2 balance (MaxUint64 - 50)
		Timestamp: time.Now().Unix() + 2, PublicKey: w1.PublicKey,
	}
	_ = overflowTx.Sign(w1.PrivateKey)
	_ = seq.SubmitTransaction(overflowTx)

	batch, err := seq.BuildBatch()
	if err != nil {
		t.Fatalf("BuildBatch: %v", err)
	}
	// Only selfTx should have executed and been included in batch.Transactions.
	if len(batch.Transactions) != 1 {
		t.Fatalf("expected 1 executed tx in batch (skipped replay+overflow), got %d", len(batch.Transactions))
	}
	if bal := l2State.GetBalance(w1.Address); bal != 10_000 {
		t.Fatalf("self-transfer mutated L2 balance: want=10000 got=%d", bal)
	}

	// 3. Forged / tampered ZK-STARK proof commitment must be rejected by L1Bridge!
	tampered := *batch
	tampered.ZkStarkProof = append([]byte(nil), batch.ZkStarkProof...)
	tampered.ZkStarkProof[70] ^= 0xff
	if err := bridge.SubmitBatch(&tampered); err == nil {
		t.Fatal("expected L1Bridge to reject tampered ZK-STARK proof commitment")
	}

	// Nil batch rejected
	if err := bridge.SubmitBatch(nil); err == nil {
		t.Fatal("expected L1Bridge to reject nil batch")
	}

	// Valid batch accepted and advances root
	if err := bridge.SubmitBatch(batch); err != nil {
		t.Fatalf("L1Bridge rejected valid batch: %v", err)
	}
	if bridge.GetL2Root() != batch.PostRoot {
		t.Fatal("L1Bridge root did not advance to batch.PostRoot")
	}
}
