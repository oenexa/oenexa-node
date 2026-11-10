package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/rpc"
)

func TestHandleKeygen(t *testing.T) {
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("crypto.NewWallet failed: %v", err)
	}

	if len(w.Address) != crypto.AddressSize {
		t.Errorf("expected address size %d, got %d", crypto.AddressSize, len(w.Address))
	}
	if len(w.PublicKey) != crypto.PublicKeySize {
		t.Errorf("expected public key size %d, got %d", crypto.PublicKeySize, len(w.PublicKey))
	}
	if len(w.PrivateKey) != crypto.PrivateKeySize {
		t.Errorf("expected private key size %d, got %d", crypto.PrivateKeySize, len(w.PrivateKey))
	}
}

func TestHandleBalance_WithFloatingPointResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"balance_oenexa":   1000000000,
				"balance_nano_oen": 1000000000,
				"balance":          1000000000,
				"balance_oen":      1.0, // Floating point number in JSON!
			},
		})
	}))
	defer ts.Close()

	// Call handleBalance — should decode safely without panic or unmarshal error
	handleBalance("0x1234567890abcdef1234567890abcdef12345678", ts.URL)
}

func TestHandleInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": rpc.ChainInfo{
				ChainID:    1,
				Height:     42,
				TipHash:    "0xabcdef",
				PeerCount:  5,
				MempoolLen: 3,
				Ticker:     "ONXC",
				Version:    "v1.0.0",
			},
		})
	}))
	defer ts.Close()

	handleInfo(ts.URL)
}

func TestHandleTransfer_Flow(t *testing.T) {
	alice, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "oen_getTransactionCount":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]interface{}{"nonce": 0},
			})
		case "oen_gasPrice":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]interface{}{"base_fee": core.InitialBaseFee},
			})
		case "oen_sendRawTransaction":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]string{"tx_hash": "0x1122334455667788"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	privHex := hex.EncodeToString(alice.PrivateKey)
	toHex := crypto.AddressToHex(bob.Address)

	handleTransfer(privHex, toHex, "1.5", ts.URL)
}

func TestGetRPCFlag(t *testing.T) {
	url := getRPCFlag([]string{"--rpc", "http://custom:9999"})
	if url != "http://custom:9999" {
		t.Errorf("expected http://custom:9999, got %s", url)
	}

	defaultURL := getRPCFlag([]string{})
	if defaultURL != defaultRPCURL {
		t.Errorf("expected %s, got %s", defaultRPCURL, defaultURL)
	}
}