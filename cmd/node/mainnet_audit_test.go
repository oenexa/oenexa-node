package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

func TestCLI_WalletAndQueryAndVersionCommands(t *testing.T) {
	dir := t.TempDir()
	flagDataDir = dir
	flagKeystore = "ks.json"
	flagPassword = "StrongPassword123!"
	defer func() {
		flagDataDir = ""
		flagKeystore = ""
		flagPassword = ""
		flagRPCAddr = "http://127.0.0.1:8545"
		flagTxTo = ""
		flagTxAmount = 0
		flagQueryAddr = ""
		flagQueryHeight = 0
	}()

	// versionCmd
	versionCmd.Run(versionCmd, nil)
	printBanner()

	// wallet new -> wallet show -> wallet export-viewing-key
	if err := walletNewCmd.RunE(walletNewCmd, nil); err != nil {
		t.Fatalf("walletNewCmd: %v", err)
	}
	if err := walletShowCmd.RunE(walletShowCmd, nil); err != nil {
		t.Fatalf("walletShowCmd: %v", err)
	}
	if err := walletExportVKCmd.RunE(walletExportVKCmd, nil); err != nil {
		t.Fatalf("walletExportVKCmd: %v", err)
	}

	// wallet shield & unshield validation (before RPC server)
	flagShieldAmount = 0
	if err := walletShieldCmd.RunE(walletShieldCmd, nil); err == nil {
		t.Fatal("expected error for zero shield amount")
	}
	flagUnshieldTo = ""
	if err := walletUnshieldCmd.RunE(walletUnshieldCmd, nil); err == nil {
		t.Fatal("expected error for missing --to")
	}

	// Mock JSON-RPC server for query, shield, unshield & tx commands.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		res := map[string]any{"balance_nano_onxc": float64(500), "base_fee": float64(10), "nonce": float64(0), "tx_hash": "0xabcd", "height": float64(1)}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": res})
	}))
	defer srv.Close()
	flagRPCAddr = srv.URL

	// Real signed wallet shield & unshield broadcast via mock RPC
	w2, _ := crypto.NewWallet()
	flagShieldAmount = 100
	if err := walletShieldCmd.RunE(walletShieldCmd, nil); err != nil {
		t.Fatalf("walletShieldCmd: %v", err)
	}
	flagUnshieldTo = crypto.AddressToHex(w2.Address)
	if err := walletUnshieldCmd.RunE(walletUnshieldCmd, nil); err != nil {
		t.Fatalf("walletUnshieldCmd: %v", err)
	}

	// query commands
	flagQueryAddr = crypto.AddressToHex(w2.Address)
	for _, fn := range []func() error{
		func() error { return queryBalanceCmd.RunE(queryBalanceCmd, nil) },
		func() error { return queryChainCmd.RunE(queryChainCmd, nil) },
		func() error { return queryFeeCmd.RunE(queryFeeCmd, nil) },
		func() error { return queryTurnstileCmd.RunE(queryTurnstileCmd, nil) },
	} {
		if err := fn(); err != nil {
			t.Fatalf("query command error: %v", err)
		}
	}

	// query block error without flag vs passing --height=0 via cobra FlagSet
	if err := queryBlockCmd.RunE(queryBlockCmd, nil); err == nil {
		t.Fatal("expected --height required error")
	}
	_ = queryBlockCmd.Flags().Set("height", "0")
	if err := queryBlockCmd.RunE(queryBlockCmd, nil); err != nil {
		t.Fatalf("queryBlockCmd with --height=0 failed: %v", err)
	}

	// tx send command
	flagTxTo = crypto.AddressToHex(w2.Address)
	flagTxAmount = 100
	flagTxNonce = 0
	if err := txSendCmd.RunE(txSendCmd, nil); err != nil {
		t.Fatalf("txSendCmd: %v", err)
	}

	// loadConfig + resolvePassword + helpers
	flagTestnet = true
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("loadConfig testnet: %v", err)
	}
	flagTestnet = false

	_ = os.Setenv("ONXC_PASSWORD", "envpw")
	flagPassword = ""
	if resolvePassword() != "envpw" {
		t.Fatal("expected env password")
	}
	_ = os.Unsetenv("ONXC_PASSWORD")

	if extractString(map[string]any{"k": "v"}, "k") != "v" || extractString(map[string]any{}, "k") != "" {
		t.Fatal("extractString mismatch")
	}
	if extractUint64(map[string]any{"n": uint64(7)}, "n") != 7 || extractUint64(map[string]any{}, "n") != 0 {
		t.Fatal("extractUint64 mismatch")
	}
	rawTx, err := gobEncodeTx(core.NewTransfer(w2.Address, w2.Address, w2.PublicKey, 0, 1, 10))
	if err != nil || len(rawTx) == 0 {
		t.Fatal("gobEncodeTx failed")
	}
	_ = filepath.Join(dir, "ks.json")
}
