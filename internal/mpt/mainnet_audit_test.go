package mpt

import (
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

func TestLevelDBBackendAndMapBackendPutGet(t *testing.T) {
	mb := newMapBackend()
	if err := mb.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("mapBackend.Put: %v", err)
	}
	got, err := mb.Get([]byte("k1"))
	if err != nil || string(got) != "v1" {
		t.Fatalf("mapBackend.Get mismatch: %q err=%v", got, err)
	}
	if miss, err := mb.Get([]byte("missing")); err != nil || miss != nil {
		t.Fatalf("expected (nil, nil) on missing key, got (%v, %v)", miss, err)
	}

	lb := &LevelDBBackend{
		PutFn: mb.Put,
		GetFn: mb.Get,
		NewBatchFn: func() Batch {
			return mb.NewBatch()
		},
	}
	if err := lb.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("LevelDBBackend.Put: %v", err)
	}
	if got2, err := lb.Get([]byte("k2")); err != nil || string(got2) != "v2" {
		t.Fatalf("LevelDBBackend.Get mismatch: %q err=%v", got2, err)
	}
	bat := lb.NewBatch()
	bat.Put([]byte("k3"), []byte("v3"))
	if err := bat.Write(); err != nil {
		t.Fatalf("LevelDBBackend batch Write: %v", err)
	}
}

func TestTrie_DeleteAndProofAdversarialPaths(t *testing.T) {
	backend := newMapBackend()
	trie := New(backend)

	var a1, a2, a3 [crypto.AddressSize]byte
	a1[0] = 0x00
	a2[0] = 0x80 // differs at bit 0 -> creates branch node
	a3[0] = 0x40

	v1 := crypto.Hash256([]byte("v1"))
	v2 := crypto.Hash256([]byte("v2"))

	if err := trie.Update(a1, v1); err != nil {
		t.Fatalf("Update a1: %v", err)
	}
	if err := trie.Update(a2, v2); err != nil {
		t.Fatalf("Update a2: %v", err)
	}
	if err := trie.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rootBeforeDelete := trie.Root()

	// Deleting a non-existent key should be a safe no-op leaving root intact.
	if err := trie.Delete(a3); err != nil {
		t.Fatalf("Delete missing a3: %v", err)
	}
	if trie.Root() != rootBeforeDelete {
		t.Fatal("deleting absent key mutated root")
	}

	// Prove a1 and verify.
	proof, err := trie.Prove(a1)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !VerifyProof(rootBeforeDelete, a1, v1, proof) {
		t.Fatal("valid proof rejected")
	}
	// Wrong proof length must fail.
	if VerifyProof(rootBeforeDelete, a1, v1, proof[:10]) {
		t.Fatal("short proof accepted")
	}
	// Tampered value hash must fail.
	if VerifyProof(rootBeforeDelete, a1, v2, proof) {
		t.Fatal("wrong value hash accepted")
	}

	// Delete both leaves down both branches -> root must return to zeroHash.
	if err := trie.Delete(a1); err != nil {
		t.Fatalf("Delete a1: %v", err)
	}
	if err := trie.Delete(a2); err != nil {
		t.Fatalf("Delete a2: %v", err)
	}
	if trie.Root() != ZeroHashOf() {
		t.Fatalf("expected zero root after deleting all keys, got %x", trie.Root())
	}

	// Reload from backend via NewWithRoot.
	reloaded := NewWithRoot(backend, rootBeforeDelete)
	gotV1, err := reloaded.Get(a1)
	if err != nil || gotV1 != v1 {
		t.Fatalf("reloaded Get(a1) = %x, err=%v", gotV1, err)
	}
}
