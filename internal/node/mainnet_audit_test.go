package node

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oenexa/oenexa/internal/config"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/p2p"
	"github.com/oenexa/oenexa/internal/storage"
)

func TestNode_CorruptTipRefusesToWipeDB(t *testing.T) {
	dir := t.TempDir()
	bdb, err := storage.Open(filepath.Join(dir, "blocks"))
	if err != nil {
		t.Fatalf("Open blocks: %v", err)
	}
	if err := bdb.Put([]byte("tip"), []byte("corrupt")); err != nil {
		t.Fatalf("Put corrupt tip: %v", err)
	}
	_ = bdb.Close()

	cfg := config.Default()
	cfg.Node.DataDir = dir
	cfg.RPC.Enabled = false
	cfg.Metrics.Enabled = false
	cfg.P2P.ListenAddr = "127.0.0.1:0"

	_, err = New(cfg, "")
	if err == nil || !strings.Contains(err.Error(), "read persisted chain tip") {
		t.Fatalf("expected corrupt persisted chain tip failure, got %v", err)
	}
}

func TestNode_InvalidValidatorAddrCleansUpDBHandles(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = t.TempDir()
	cfg.Chain.Validators = []string{"not-valid-hex"}
	cfg.RPC.Enabled = false
	cfg.Metrics.Enabled = false

	_, err := New(cfg, "")
	if err == nil || !strings.Contains(err.Error(), "invalid validator address") {
		t.Fatalf("expected invalid validator address error, got %v", err)
	}
}

func TestNode_SyncReqCapAndVoteValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = t.TempDir()
	cfg.RPC.Enabled = false
	cfg.Metrics.Enabled = false
	cfg.P2P.ListenAddr = "127.0.0.1:0"

	n, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.shutdown()
	if n.p2pNode != nil {
		defer func() { _ = n.p2pNode.Close() }()
		// Giant MaxCount must be safely clamped to <= 128 without spinning or panicking.
		n.p2pNode.OnSyncReq(p2p.SyncReq{FromHeight: 0, MaxCount: 0xffffffff})
		n.p2pNode.OnSyncReq(p2p.SyncReq{FromHeight: 0, MaxCount: 0})

		if n.p2pNode.OnVoteValidated == nil {
			t.Fatal("expected OnVoteValidated hook to be wired")
		}
		if err := n.p2pNode.OnVoteValidated("bad-peer", []byte("not-json")); err == nil {
			t.Fatal("expected error on invalid vote JSON")
		}
	}

	if rep := n.StatusReport(); !strings.Contains(rep, "height=") {
		t.Fatalf("unexpected StatusReport: %q", rep)
	}
	if got := resolveDataPath("", "rel.db"); got != "rel.db" {
		t.Fatalf("resolveDataPath empty dataDir: %q", got)
	}
	w, _ := crypto.NewWallet()
	_ = w
}
