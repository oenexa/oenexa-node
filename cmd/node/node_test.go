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
	"github.com/oenexa/oenexa/internal/keystore"
)

func TestExtractString(t *testing.T) {
	m := map[string]interface{}{
		"tx_hash": "0x1234abcd",
		"number":  42,
	}
	if extractString(m, "tx_hash") != "0x1234abcd" {
		t.Errorf("expected 0x1234abcd, got %s", extractString(m, "tx_hash"))
	}
	if extractString(m, "nonexistent") != "" {
		t.Errorf("expected empty string, got %s", extractString(m, "nonexistent"))
	}
}

func TestExtractUint64(t *testing.T) {
	m := map[string]interface{}{
		"float_val": float64(100),
		"uint_val":  uint64(200),
		"str_val":   "invalid",
	}
	if extractUint64(m, "float_val") != 100 {
		t.Errorf("expected 100, got %d", extractUint64(m, "float_val"))
	}
	if extractUint64(m, "uint_val") != 200 {
		t.Errorf("expected 200, got %d", extractUint64(m, "uint_val"))
	}
	if extractUint64(m, "str_val") != 0 {
		t.Errorf("expected 0, got %d", extractUint64(m, "str_val"))
	}
}

func TestResolvePassword(t *testing.T) {
	flagPassword = "flag_pass"
	if resolvePassword() != "flag_pass" {
		t.Errorf("expected flag_pass, got %s", resolvePassword())
	}
	flagPassword = ""

	os.Setenv("ONXC_PASSWORD", "env_pass")
	defer os.Unsetenv("ONXC_PASSWORD")
	if resolvePassword() != "env_pass" {
		t.Errorf("expected env_pass, got %s", resolvePassword())
	}
}

func TestResolvePath(t *testing.T) {
	p1 := resolvePath("/data", "wallet.json")
	if p1 != "/data/wallet.json" {
		t.Errorf("expected /data/wallet.json, got %s", p1)
	}

	p2 := resolvePath("/data", "/absolute/path")
	if p2 != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %s", p2)
	}
}

func TestFetchBaseFee(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]interface{}{"base_fee": core.InitialBaseFee},
		})
	}))
	defer ts.Close()

	bf, err := fetchBaseFee(ts.URL)
	if err != nil {
		t.Fatalf("fetchBaseFee failed: %v", err)
	}
	if bf != core.InitialBaseFee {
		t.Errorf("expected %d, got %d", core.InitialBaseFee, bf)
	}
}

func TestWalletLifecycle_EncryptedKeystore(t *testing.T) {
	dir := t.TempDir()
	ksPath := filepath.Join(dir, "keystore.json")
	password := "TestPassphrase123!"

	// 1. Generate and encrypt wallet
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	if err := keystore.Encrypt(ksPath, password, w); err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// 2. Peek address without password
	peekedAddr, err := keystore.PeekAddress(ksPath)
	if err != nil {
		t.Fatalf("PeekAddress failed: %v", err)
	}
	if peekedAddr != crypto.AddressToHex(w.Address) {
		t.Errorf("peeked address %s != actual %s", peekedAddr, crypto.AddressToHex(w.Address))
	}

	// 3. Decrypt and verify
	decrypted, err := keystore.Decrypt(ksPath, password)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted.Address != w.Address {
		t.Error("decrypted address does not match original")
	}
}