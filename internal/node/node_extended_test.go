package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/config"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/keystore"
	"github.com/oenexa/oenexa/internal/state"
)

func TestNodeStartupShutdown(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "127.0.0.1:0"
	cfg.Node.DataDir = t.TempDir()
	cfg.Node.MinerEnabled = true
	cfg.RPC.Enabled = true
	cfg.RPC.ListenAddr = "127.0.0.1:0"
	cfg.Metrics.Enabled = true
	cfg.Metrics.ListenAddr = "127.0.0.1:0"

	n, err := New(cfg, "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	defer n.shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		n.Start(ctx)
		close(done)
	}()
	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Call P2P handlers for coverage
	if n.p2pNode != nil {
		_ = n.p2pNode.OnTxReceived("", &core.Transaction{})
		_ = n.p2pNode.OnBlockReceived("", &core.Block{})
	}

	_ = n.StatusReport()
	cancel()
	<-done
}

func TestNodeStartupErrors(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "127.0.0.1:-1"
	cfg.Node.DataDir = t.TempDir()

	// 1. Existing invalid keystore with password
	ksFile := filepath.Join(cfg.Node.DataDir, "keystore.json")
	if err := os.WriteFile(ksFile, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := New(cfg, "wrongpass")
	if err == nil {
		t.Fatal("expected error on invalid keystore decryption")
	}

	// 2. Existing valid keystore with password
	os.Remove(ksFile)
	w, _ := crypto.NewWallet()
	_ = keystore.Encrypt(ksFile, "pass123", w)

	// Missing password when keystore exists
	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error on existing keystore without password")
	}

	// Valid password
	nValid, err := New(cfg, "pass123")
	if err != nil {
		t.Fatal(err)
	}
	nValid.shutdown()

	// Create a new wallet and save it when password is provided
	os.Remove(ksFile)
	nNew, err := New(cfg, "pass123")
	if err != nil {
		t.Fatal(err)
	}
	nNew.shutdown()

	// 2.5. Keystore save failure
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "uncreatable_ks")
	f_ks, _ := os.Create(cfg.Node.DataDir)
	f_ks.Close()
	_, err = New(cfg, "pass123")
	if err == nil {
		t.Fatal("expected error saving keystore")
	}

	// 3. Mkdir blocks failure
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "uncreatable")
	f, _ := os.Create(cfg.Node.DataDir)
	f.Close()
	cfg.Storage.BlocksDir = "blocks"
	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error on mkdir blocks")
	}
	os.Remove(cfg.Node.DataDir)

	// 4. Mkdir state failure
	cfg.Node.DataDir = t.TempDir()
	cfg.Storage.StateDir = "state"
	statePath := filepath.Join(cfg.Node.DataDir, "state")
	f2, _ := os.Create(statePath)
	f2.Close()
	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error on mkdir state")
	}
	os.Remove(statePath)

	// 5. storage.Open block db failure
	cfg.Node.DataDir = t.TempDir()
	cfg.Storage.BlocksDir = "chaindata"
	blocksFile := filepath.Join(cfg.Node.DataDir, "chaindata")
	if err := os.WriteFile(blocksFile, []byte("file-not-dir"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error opening block db if it's a file")
	}
	_ = os.Remove(blocksFile)

	// 6. storage.Open state db failure
	cfg.Node.DataDir = t.TempDir()
	cfg.Storage.StateDir = "state"
	stateFile := filepath.Join(cfg.Node.DataDir, "state")
	if err := os.WriteFile(stateFile, []byte("file-not-dir"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error opening state db if it's a file")
	}
	os.Remove(stateFile)
}

func TestNodeLoadChain(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "127.0.0.1:0"
	cfg.Node.DataDir = t.TempDir()
	n, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	defer n.shutdown()

	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{{}}).Build()
	chain, err := n.loadChain(genesis, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected length 1, got %d", len(chain))
	}

	// Insert block 1 and test loadChain up to 2 (missing 2)
	b1 := genesis
	b1.Header.Height = 1
	if err := n.blockStore.PutBlock(b1); err != nil {
		t.Fatal(err)
	}
	chain, err = n.loadChain(genesis, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 2 { // should stop at 1
		t.Fatalf("expected partial chain of length 2, got %d", len(chain))
	}
}

func TestResolveDataPath(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "127.0.0.1:0"

	abs := filepath.Join(t.TempDir(), "abs")
	res := resolveDataPath(cfg.Node.DataDir, abs)
	if res != abs {
		t.Fatal("expected absolute path to resolve to itself")
	}

	res2 := resolveDataPath("", "rel")
	if res2 != "rel" {
		t.Fatalf("expected rel, got %s", res2)
	}

	res3 := resolveDataPath("~/", "rel")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "rel")
	if res3 != expected {
		t.Fatalf("expected %s, got %s", expected, res3)
	}
}

func TestNodeDefaultListenAddr(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "" // Trigger the "" default branch
	cfg.Node.DataDir = t.TempDir()
	n, err := New(cfg, "")
	if err == nil {
		n.shutdown()
	}
}

func TestStateLoadErrorInNew(t *testing.T) {
	cfg := config.Default()
	cfg.P2P.ListenAddr = "127.0.0.1:0"
	cfg.Node.DataDir = t.TempDir()

	n, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	blk := &core.Block{Header: core.BlockHeader{Height: 1}}
	_ = n.blockStore.PutBlock(blk)
	_ = n.blockStore.UpdateTip(1, blk.Hash)
	_ = n.stateDB.Put([]byte("a:badhex"), []byte("val"))
	n.shutdown()

	_, err = New(cfg, "")
	if err == nil {
		t.Fatal("expected error loading state")
	}

	cfg.Node.DataDir = t.TempDir()
	n2, _ := New(cfg, "")
	blk2 := &core.Block{Header: core.BlockHeader{Height: 1}}
	_ = n2.blockStore.PutBlock(blk2)
	_ = n2.blockStore.UpdateTip(1, blk2.Hash)
	st := state.NewStateDB()
	_ = n2.stateStore.SaveState(st)
	n2.shutdown()

	nOk, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	nOk.shutdown()
}
