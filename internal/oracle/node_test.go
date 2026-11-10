package oracle

import (
	"testing"
)

func TestNewNode(t *testing.T) {
	n := NewNode("node-1", 1000, "http://localhost:8545")
	if n.NodeID != "node-1" {
		t.Errorf("NodeID: want node-1, got %s", n.NodeID)
	}
	if n.StakeAmount != 1000 {
		t.Errorf("StakeAmount: want 1000, got %d", n.StakeAmount)
	}
	if n.RPCEndpoint != "http://localhost:8545" {
		t.Errorf("RPCEndpoint: want http://localhost:8545, got %s", n.RPCEndpoint)
	}
	if n.httpClient == nil {
		t.Error("httpClient must not be nil")
	}
}

func TestTickerToCoinGeckoID_Known(t *testing.T) {
	cases := []struct {
		ticker string
		wantID string
	}{
		{"BTC", "bitcoin"},
		{"ETH", "ethereum"},
		{"SOL", "solana"},
		{"BNB", "binancecoin"},
		{"USDT", "tether"},
		{"OEN", "oenexa"},
	}
	for _, tc := range cases {
		id, ok := tickerToCoinGeckoID(tc.ticker)
		if !ok {
			t.Errorf("tickerToCoinGeckoID(%q): unexpected miss", tc.ticker)
			continue
		}
		if id != tc.wantID {
			t.Errorf("tickerToCoinGeckoID(%q): want %q, got %q", tc.ticker, tc.wantID, id)
		}
	}
}

func TestTickerToCoinGeckoID_Unknown(t *testing.T) {
	_, ok := tickerToCoinGeckoID("NONEXISTENT_TICKER_XYZ")
	if ok {
		t.Error("expected miss for unknown ticker, got hit")
	}
}

// TestFetchFinancialData_UnknownTicker verifies that an unknown ticker
// returns an error rather than a stale hardcoded value.
func TestFetchFinancialData_UnknownTicker(t *testing.T) {
	n := NewNode("oracle-1", 500, "http://rpc")
	_, err := n.FetchFinancialData("NONEXISTENT_TICKER_XYZ")
	if err == nil {
		t.Error("expected error for unknown ticker, got nil")
	}
}

// TestFetchESGScore_ReturnsError confirms ESG score returns an error until
// a real registry is configured (zero-mock policy compliance).
func TestFetchESGScore_ReturnsError(t *testing.T) {
	n := NewNode("oracle-2", 100, "http://esg")
	score, err := n.FetchESGScore("company-abc")
	if err == nil {
		t.Error("expected error from FetchESGScore until a real registry is wired")
	}
	if score != 0 {
		t.Errorf("expected score 0 on error, got %d", score)
	}
}

func TestSubmitTransaction(t *testing.T) {
	n := NewNode("oracle-3", 200, "http://rpc")
	err := n.SubmitTransaction("0xcontract", 42)
	if err != nil {
		t.Fatalf("SubmitTransaction should not return error, got: %v", err)
	}
}