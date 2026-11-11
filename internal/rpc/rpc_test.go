package rpc_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/rpc"
	"github.com/oenexa/oenexa/internal/state"
)

func buildTestHandler(t *testing.T) (*httptest.Server, *crypto.Wallet) {
	t.Helper()
	w, _ := crypto.NewWallet()
	genCfg := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w.Address})
	genesis := genCfg.Build()
	st := state.NewStateDB()
	for addr, bal := range genCfg.Allocations {
		st.SetAccount(addr, &state.Account{Balance: bal})
	}
	pool := mempool.New(0)
	validator := &consensus.Validator{Address: w.Address, PublicKey: w.PublicKey, VotingPower: 1}
	vs, _ := consensus.NewValidatorSet([]*consensus.Validator{validator})
	engine := consensus.NewEngine(w.Address, w.PublicKey, w.PrivateKey, vs, st, pool, genesis, nil, nil)
	api := rpc.NewAPI(engine, st, pool, nil, nil, "test")
	srv := rpc.NewServer("127.0.0.1:0", api, 5*time.Second, 5*time.Second)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, w
}

func rpcCall(t *testing.T, ts *httptest.Server, method string, params interface{}) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("WARN: could not close HTTP response body: %v\n", err)
		}
	}(resp.Body)
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Error != nil {
		t.Fatalf("RPC error %d: %s", out.Error.Code, out.Error.Message)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out.Result, &result)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

func TestRPC_BlockHeight(t *testing.T) {
	ts, _ := buildTestHandler(t)
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "onxc_blockHeight", "params": nil,
	})
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("WARN: could not close HTTP response body: %v\n", err)
		}
	}(resp.Body)
	var out struct {
		Result float64 `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	// Genesis engine starts at height 1 (genesis at 0, chain length = 1).
	if out.Result < 1 {
		t.Errorf("expected height >= 1, got %v", out.Result)
	}
}

func TestRPC_GetBalance(t *testing.T) {
	ts, w := buildTestHandler(t)
	result := rpcCall(t, ts, "onxc_getBalance", []string{crypto.AddressToHex(w.Address)})
	bal, ok := result["balance_nano_onxc"].(float64)
	if !ok {
		t.Fatalf("missing balance_nano_onxc in %v", result)
	}
	if bal <= 0 {
		t.Errorf("expected positive balance, got %v", bal)
	}
}

func TestRPC_GetTransactionCount(t *testing.T) {
	ts, w := buildTestHandler(t)
	result := rpcCall(t, ts, "onxc_getTransactionCount", []string{crypto.AddressToHex(w.Address)})
	_, ok := result["nonce"]
	if !ok {
		t.Fatalf("missing nonce in %v", result)
	}
}

func TestRPC_ChainInfo(t *testing.T) {
	ts, _ := buildTestHandler(t)
	result := rpcCall(t, ts, "onxc_chainInfo", nil)
	if _, ok := result["height"]; !ok {
		t.Errorf("missing height in chain info: %v", result)
	}
	if _, ok := result["chain_id"]; !ok {
		t.Errorf("missing chain_id in chain info: %v", result)
	}
}

func TestRPC_BlockByHeight(t *testing.T) {
	ts, _ := buildTestHandler(t)
	result := rpcCall(t, ts, "onxc_blockByHeight", []uint64{0})
	if _, ok := result["hash"]; !ok {
		t.Errorf("missing hash in block: %v", result)
	}
}

func TestRPC_GasPrice(t *testing.T) {
	ts, _ := buildTestHandler(t)
	result := rpcCall(t, ts, "onxc_gasPrice", nil)
	if _, ok := result["base_fee"]; !ok {
		t.Errorf("missing base_fee in gas price: %v", result)
	}
}

func TestRPC_MethodNotFound(t *testing.T) {
	ts, _ := buildTestHandler(t)
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "onxc_nonExistent", "params": nil,
	})
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("WARN: could not close HTTP response body: %v\n", err)
		}
	}(resp.Body)
	var out struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Error == nil || out.Error.Code != -32601 {
		t.Errorf("expected -32601 MethodNotFound, got %+v", out.Error)
	}
}

func TestRPC_ShieldedAndTurnstile(t *testing.T) {
	ts, _ := buildTestHandler(t)
	resShielded := rpcCall(t, ts, "onxc_getShieldedBalance", nil)
	if _, ok := resShielded["shielded_balance_nano_onxc"]; !ok {
		t.Fatalf("missing shielded_balance_nano_onxc in %v", resShielded)
	}
	if _, ok := resShielded["tree_leaf_count"]; !ok {
		t.Fatalf("missing tree_leaf_count in %v", resShielded)
	}

	resTurnstile := rpcCall(t, ts, "onxc_getTurnstileStatus", nil)
	if _, ok := resTurnstile["transparent_supply_nano_onxc"]; !ok {
		t.Fatalf("missing transparent_supply_nano_onxc in %v", resTurnstile)
	}
	if val, ok := resTurnstile["invariant_preserved"].(bool); !ok || !val {
		t.Fatalf("expected invariant_preserved to be true, got %v", resTurnstile)
	}
}

func TestRPC_ONXCNamespace(t *testing.T) {
	ts, w := buildTestHandler(t)
	// Test canonical ONXC RPC methods and zero-alias strictness
	resChainONXC := rpcCall(t, ts, "onxc_chainInfo", nil)
	if ticker, ok := resChainONXC["ticker"].(string); !ok || ticker != "ONXC" {
		t.Errorf("expected ticker ONXC in onxc_chainInfo, got %v", ticker)
	}

	resBal := rpcCall(t, ts, "onxc_getBalance", []string{crypto.AddressToHex(w.Address)})
	if _, ok := resBal["balance_nano_onxc"]; !ok {
		t.Fatalf("missing balance_nano_onxc in %v", resBal)
	}
	if _, ok := resBal["balance_onxc"]; !ok {
		t.Fatalf("missing balance_onxc in %v", resBal)
	}
}

func TestRPC_Web3Compatibility(t *testing.T) {
	ts, w := buildTestHandler(t)

	// eth_chainId
	resChainId := rpcCallString(t, ts, "eth_chainId", nil)
	if resChainId == "" {
		t.Errorf("expected chainId string, got empty")
	}

	// net_version
	resNet := rpcCallString(t, ts, "net_version", nil)
	if resNet == "" {
		t.Errorf("expected net_version string, got empty")
	}

	// web3_clientVersion
	resVer := rpcCallString(t, ts, "web3_clientVersion", nil)
	if resVer == "" {
		t.Errorf("expected web3_clientVersion string, got empty")
	}

	// eth_getBalance with 0x prefix
	resBal := rpcCall(t, ts, "eth_getBalance", []string{"0x" + crypto.AddressToHex(w.Address)})
	if _, ok := resBal["balance_nano_onxc"]; !ok {
		t.Fatalf("missing balance_nano_onxc in %v", resBal)
	}

	// eth_getTransactionCount
	resNonce := rpcCall(t, ts, "eth_getTransactionCount", []string{crypto.AddressToHex(w.Address)})
	if _, ok := resNonce["nonce"]; !ok {
		t.Fatalf("missing nonce in %v", resNonce)
	}
}

func rpcCallString(t *testing.T, ts *httptest.Server, method string, params interface{}) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", method, err)
	}
	defer resp.Body.Close()

	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("rpc error on %s: code=%d msg=%s", method, out.Error.Code, out.Error.Message)
	}
	var res string
	_ = json.Unmarshal(out.Result, &res)
	return res
}

func TestRPC_DelegationEndpoints(t *testing.T) {
	w, _ := crypto.NewWallet()
	genCfg := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w.Address})
	genesis := genCfg.Build()
	st := state.NewStateDB()
	pool := mempool.New(0)
	validator := &consensus.Validator{Address: w.Address, PublicKey: w.PublicKey, VotingPower: 1}
	vs, _ := consensus.NewValidatorSet([]*consensus.Validator{validator})
	engine := consensus.NewEngine(w.Address, w.PublicKey, w.PrivateKey, vs, st, pool, genesis, nil, nil)
	api := rpc.NewAPI(engine, st, pool, nil, nil, "test")
	srv := rpc.NewServer("127.0.0.1:0", api, 5*time.Second, 5*time.Second)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// Without delegation manager attached
	res := rpcCall(t, ts, "onxc_getTotalDelegated", []interface{}{})
	if res["total_delegated"] != float64(0) {
		t.Fatalf("expected 0 total delegated, got %v", res["total_delegated"])
	}

	// Attached delegation manager
	dm := consensus.NewDelegationManager(vs, 100)
	api.SetDelegationManager(dm)

	res = rpcCall(t, ts, "onxc_getTotalDelegated", []interface{}{})
	if res["total_delegated"] != float64(0) {
		t.Fatalf("expected 0 total delegated, got %v", res["total_delegated"])
	}

	// Test valid query with no delegations
	addrHex := crypto.ToHex([crypto.AddressSize]byte{2})
	delsResp := rpcCall(t, ts, "onxc_getDelegations", []string{addrHex})
	_ = delsResp
}

