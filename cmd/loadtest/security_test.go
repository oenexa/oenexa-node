package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadtest_RPCEdges(t *testing.T) {
	// Invalid JSON returned from server
	tsBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer tsBadJSON.Close()

	if _, err := rpcCall(tsBadJSON.URL, "onxc_blockHeight", nil); err == nil {
		t.Fatal("expected JSON decode error")
	}

	// Non-object result fallback wraps as {"value": ...}
	tsScalar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":42}`))
	}))
	defer tsScalar.Close()

	res, err := rpcCall(tsScalar.URL, "onxc_blockHeight", nil)
	if err != nil {
		t.Fatalf("scalar result rpcCall failed: %v", err)
	}
	if res["value"] != "42" {
		t.Fatalf("expected scalar value '42', got %v", res["value"])
	}

	// Missing nonce field defaults cleanly to 0 without error
	if n, err := fetchNonce(tsScalar.URL, "0x00"); err != nil || n != 0 {
		t.Fatalf("expected (0, nil), got (%d, %v)", n, err)
	}
}
