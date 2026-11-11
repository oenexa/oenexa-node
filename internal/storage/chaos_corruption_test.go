package storage

import (
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

func TestChaos_StateStoreCorruptedRecordsRecovery(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ss := NewStateStore(db)
	emptyState := state.NewStateDB()
	if err := ss.SaveState(emptyState); err != nil {
		t.Fatalf("SaveState(empty): %v", err)
	}
	loaded, err := ss.LoadState()
	if err != nil || loaded == nil {
		t.Fatalf("LoadState(empty): err=%v loaded=%v", err, loaded)
	}

	// Inject valid account then corrupt flat record bytes to simulate disk corruption.
	var addr [crypto.AddressSize]byte
	addr[0] = 0xAA
	emptyState.SetAccount(addr, &state.Account{Balance: 500, Nonce: 2})
	if err := ss.SaveState(emptyState); err != nil {
		t.Fatalf("SaveState(non-empty): %v", err)
	}

	// Corrupt account record length (not 80 bytes).
	if err := db.Put(accountKey(addr), []byte("corrupted-short-bytes")); err != nil {
		t.Fatalf("db.Put corruption: %v", err)
	}
	if _, err := ss.LoadState(); err == nil {
		t.Fatal("expected LoadState error on corrupted account record length")
	}

	// Restore valid length record then corrupt key prefix length.
	if err := db.Put(accountKey(addr), encodeAccount(&state.Account{Balance: 500, Nonce: 2})); err != nil {
		t.Fatal(err)
	}
	if err := db.Put([]byte("a:short"), make([]byte, 80)); err != nil {
		t.Fatal(err)
	}
	if _, err := ss.LoadState(); err == nil {
		t.Fatal("expected LoadState error on corrupted account key length")
	}
}

func BenchmarkStateStoreSave1kAccounts(b *testing.B) {
	dir := b.TempDir()
	db, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ss := NewStateStore(db)
	st := state.NewStateDB()
	for i := 0; i < 1000; i++ {
		var addr [crypto.AddressSize]byte
		addr[0] = byte(i >> 8)
		addr[1] = byte(i)
		st.SetAccount(addr, &state.Account{Balance: uint64(i+1) * 1_000_000, Nonce: uint64(i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ss.SaveState(st); err != nil {
			b.Fatal(err)
		}
	}
}

