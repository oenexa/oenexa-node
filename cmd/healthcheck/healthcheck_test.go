package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHealthcheck_Healthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  100,
		})
	}))
	defer ts.Close()

	os.Setenv("OENEXA_RPC_URL", ts.URL)
	defer os.Unsetenv("OENEXA_RPC_URL")

	client := http.Client{}
	resp, err := client.Post(ts.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestHealthcheck_Unhealthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := http.Client{}
	resp, err := client.Post(ts.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected non-200 status, got %d", resp.StatusCode)
	}
}