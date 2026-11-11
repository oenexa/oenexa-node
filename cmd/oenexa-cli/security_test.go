package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurity_CLICallRPCValidation(t *testing.T) {
	if _, err := callRPC("ftp://bad-scheme", "onxc_blockHeight", nil); err == nil {
		t.Fatal("expected error rejecting non-http/https RPC URL")
	}
	if _, err := callRPC("not-a-url", "onxc_blockHeight", nil); err == nil {
		t.Fatal("expected error rejecting malformed RPC URL")
	}

	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`))
	}))
	defer tsErr.Close()

	if _, err := callRPC(tsErr.URL, "onxc_missing", nil); err == nil {
		t.Fatal("expected error on JSON-RPC error response")
	}
}
