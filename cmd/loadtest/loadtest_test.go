package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

func TestGobEncodeTx(t *testing.T) {
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}

	tx := core.NewTransfer(w.Address, w.Address, w.PublicKey, 1, 1000, 10)
	raw, err := gobEncodeTx(tx)
	if err != nil {
		t.Fatalf("gobEncodeTx failed: %v", err)
	}
	if len(raw) == 0 {
		t.Error("encoded bytes should not be empty")
	}
}

func TestFetchNonce(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]interface{}{"nonce": 5},
		})
	}))
	defer ts.Close()

	nonce, err := fetchNonce(ts.URL, "0x1234567890abcdef1234567890abcdef12345678")
	if err != nil {
		t.Fatalf("fetchNonce failed: %v", err)
	}
	if nonce != 5 {
		t.Errorf("expected nonce 5, got %d", nonce)
	}
}

func TestRPCCall_ErrorHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32600,
				"message": "Invalid Request",
			},
		})
	}))
	defer ts.Close()

	_, err := rpcCall(ts.URL, "test_method", nil)
	if err == nil {
		t.Error("expected RPC error, got nil")
	}
}