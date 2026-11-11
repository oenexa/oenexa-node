package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

func TestLoadtest_HelpersAndDispatch(t *testing.T) {
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tx_hash":"0x1234"}}`))
	}))
	defer srv.Close()

	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()
	tx := core.NewTransfer(w1.Address, w2.Address, w1.PublicKey, 0, 100, core.InitialBaseFee)
	_ = tx.Sign(w1.PrivateKey)
	raw, err := gobEncodeTx(tx)
	if err != nil || len(raw) == 0 {
		t.Fatalf("gobEncodeTx: %v", err)
	}
	res, err := rpcCall(srv.URL, "onxc_sendRawTransaction", []string{"00"})
	if err != nil || res == nil || sent.Load() != 1 {
		t.Fatalf("rpcCall failed: res=%v err=%v", res, err)
	}
}
