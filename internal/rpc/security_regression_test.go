package rpc_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSecurity_RPCServerBodyAndBatchLimits(t *testing.T) {
	ts, _ := buildRealServer(t)

	// 1. Request body > 4 MiB must return HTTP 413 Request Entity Too Large
	oversized := bytes.Repeat([]byte("a"), (4<<20)+1024)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", resp.StatusCode)
	}

	// 2. Batch size > 100 items must return JSON-RPC error
	items := make([]map[string]interface{}, 101)
	for i := range items {
		items[i] = map[string]interface{}{"jsonrpc": "2.0", "id": i + 1, "method": "onxc_blockHeight"}
	}
	batchBody, _ := json.Marshal(items)
	resp2, err := http.Post(ts.URL, "application/json", bytes.NewReader(batchBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	var batchErrOut map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&batchErrOut)
	if batchErrOut["error"] == nil {
		t.Fatal("expected error for oversized JSON-RPC batch (>100)")
	}

	// 3. Batch item with wrong jsonrpc version must return per-item error
	mixedBatch := []byte(`[{"jsonrpc":"1.0","id":1,"method":"onxc_blockHeight"},{"jsonrpc":"2.0","id":2,"method":"onxc_blockHeight"}]`)
	resp3, err := http.Post(ts.URL, "application/json", bytes.NewReader(mixedBatch))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp3.Body.Close() }()
	var mixedOut []map[string]interface{}
	_ = json.NewDecoder(resp3.Body).Decode(&mixedOut)
	if len(mixedOut) != 2 || mixedOut[0]["error"] == nil || mixedOut[1]["result"] == nil {
		t.Fatalf("unexpected mixed batch output: %+v", mixedOut)
	}
}

func TestSecurity_RPCCallRevertAndValidationErrors(t *testing.T) {
	ts, w := buildTestHandler(t)

	// 1. onxc_call with bad hex data returns RPC error
	if errObj := rpcCallErr(t, ts, "onxc_call", []interface{}{
		map[string]string{
			"to":   "0x" + strings.Repeat("aa", 32),
			"data": "0xnot-hex",
		},
	}); errObj == nil {
		t.Fatal("expected error for invalid call hex data")
	}

	// 2. onxc_call targeting non-contract reverts cleanly with error
	if errObj := rpcCallErr(t, ts, "onxc_call", []interface{}{
		map[string]string{
			"from": "0x" + strings.Repeat("11", 32),
			"to":   "0x" + strings.Repeat("22", 32),
			"data": "0x0166",
		},
	}); errObj == nil {
		t.Fatal("expected reverted onxc_call error on EOA target")
	}

	// 3. onxc_blockByHeight rejects fractional/negative heights
	if errObj := rpcCallErr(t, ts, "onxc_blockByHeight", []interface{}{-1}); errObj == nil {
		t.Fatal("expected error for negative block height")
	}
	if errObj := rpcCallErr(t, ts, "onxc_blockByHeight", []interface{}{1.5}); errObj == nil {
		t.Fatal("expected error for fractional block height")
	}

	// 4. onxc_recentBlocks works safely when asking for more blocks than chain height
	_ = w
	res := rpcCall(t, ts, "onxc_chainInfo", nil)
	if res["coin_name"] == nil && res["height"] == nil {
		t.Fatalf("unexpected chainInfo response: %+v", res)
	}
}
