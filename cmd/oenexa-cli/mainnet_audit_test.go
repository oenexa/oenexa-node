package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

func TestOenexaCLI_HandlersAndSecurityGuards(t *testing.T) {
	printUsage()
	if got := getRPCFlag([]string{"--rpc", "http://127.0.0.1:9999"}); got != "http://127.0.0.1:9999" {
		t.Fatalf("getRPCFlag mismatch: %q", got)
	}
	handleKeygen()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req["method"] {
		case "onxc_chainInfo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"chain_id": 1, "version": "v0.9.0", "height": 5, "tip_hash": "0xaa", "peer_count": 2, "mempool_len": 1, "ticker": "ONXC"},
			})
		case "onxc_getBalance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"balance_nano_onxc": float64(1_000_000_000)},
			})
		case "onxc_getTransactionCount":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"nonce": float64(0)},
			})
		case "onxc_gasPrice":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"base_fee": float64(10)},
			})
		case "onxc_sendRawTransaction":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"tx_hash": "0xbeef"},
			})
		}
	}))
	defer srv.Close()

	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()
	privHex := hex.EncodeToString(w1.PrivateKey)
	toHex := crypto.AddressToHex(w2.Address)

	handleInfo(srv.URL)
	handleBalance(toHex, srv.URL)
	handleTransfer(privHex, toHex, "1.5", srv.URL)

	// Adversarial transfer amounts must safely reject without overflow/panics.
	handleTransfer(privHex, toHex, "NaN", srv.URL)
	handleTransfer(privHex, toHex, "Inf", srv.URL)
	handleTransfer(privHex, toHex, "1e20", srv.URL)
	handleTransfer("badhex", toHex, "1", srv.URL)
}
