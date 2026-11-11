package state_test

import (
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

func BenchmarkTPS_ApplyBlock_500Tx(b *testing.B) {
	benchmarkApplyBlockTPS(b, 500, false)
}

func BenchmarkTPS_ApplyBlockParallel_500Tx(b *testing.B) {
	benchmarkApplyBlockTPS(b, 500, true)
}

func benchmarkApplyBlockTPS(b *testing.B, batchSize int, parallel bool) {
	valW, _ := crypto.NewWallet()
	baseSt := state.NewStateDB()
	txs := make([]*core.Transaction, batchSize)
	for i := 0; i < batchSize; i++ {
		sw, _ := crypto.NewWallet()
		rw, _ := crypto.NewWallet()
		baseSt.SetAccount(sw.Address, &state.Account{Balance: 100 * core.OneONXC, Nonce: 0})

		tx := core.NewTransfer(sw.Address, rw.Address, sw.PublicKey, 0, core.OneONXC, core.InitialBaseFee)
		if err := tx.Sign(sw.PrivateKey); err != nil {
			b.Fatalf("Sign: %v", err)
		}
		txs[i] = tx
	}

	blk, err := core.NewBlock(1, [crypto.HashSize]byte{1}, baseSt.CommitRoot(), time.Now().UnixNano(), valW.Address, txs, uint64(batchSize)*core.GasTransfer, core.InitialBaseFee, 0)
	if err != nil {
		b.Fatalf("NewBlock: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := baseSt.Snapshot()
		if parallel {
			if _, err := state.ApplyBlockParallel(snap, blk, valW.Address, nil); err != nil {
				b.Fatalf("ApplyBlockParallel: %v", err)
			}
		} else {
			if _, err := state.ApplyBlock(snap, blk, valW.Address, nil); err != nil {
				b.Fatalf("ApplyBlock: %v", err)
			}
		}
	}
	b.StopTimer()
	secs := b.Elapsed().Seconds()
	if secs > 0 {
		b.ReportMetric(float64(b.N*batchSize)/secs, "tx/s")
	}
}
