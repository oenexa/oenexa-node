package oracle

import (
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
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

func TestSovereignValuationPairs_Known(t *testing.T) {
	cases := []struct {
		pair   string
		wantID string
	}{
		{"OEN", "oenexa"},
		{"OEN-USD", "usd"},
		{"OEN-EUR", "eur"},
		{"OEN-GBP", "gbp"},
		{"OEN-JPY", "jpy"},
		{"OEN-CHF", "chf"},
		{"OEN-USDC", "usd"},
	}
	for _, tc := range cases {
		curr, ok := SovereignValuationPairs[tc.pair]
		if !ok {
			t.Errorf("SovereignValuationPairs[%q]: unexpected miss", tc.pair)
			continue
		}
		if curr != tc.wantID {
			t.Errorf("SovereignValuationPairs[%q]: want %q, got %q", tc.pair, tc.wantID, curr)
		}
	}
}

func TestSovereignValuationPairs_Unknown(t *testing.T) {
	n := NewNode("oracle-1", 500, "http://rpc")
	_, err := n.FetchSovereignPrice("NONEXISTENT_PAIR_XYZ")
	if err == nil {
		t.Error("expected error for unknown sovereign pair, got nil")
	}
}

func TestAggregateReports_ByzantineMedian(t *testing.T) {
	// 5 oracle reports: 3 honest ($1.00 = 1_000_000 micro-units), 2 malicious outliers
	reports := []*OracleReport{
		{Reporter: [crypto.AddressSize]byte{1}, Price: 1_000_000},
		{Reporter: [crypto.AddressSize]byte{2}, Price: 1_020_000},
		{Reporter: [crypto.AddressSize]byte{3}, Price: 980_000},
		{Reporter: [crypto.AddressSize]byte{4}, Price: 100_000_000}, // malicious extreme high
		{Reporter: [crypto.AddressSize]byte{5}, Price: 1},           // malicious extreme low
	}

	median, err := AggregateReports(reports)
	if err != nil {
		t.Fatalf("aggregate reports failed: %v", err)
	}

	// Sorted: [1, 980_000, 1_000_000, 1_020_000, 100_000_000]
	// Expected median is index 2: 1_000_000
	if median != 1_000_000 {
		t.Fatalf("expected median 1,000,000, got %d", median)
	}
}

func TestAggregateReports_EvenCount(t *testing.T) {
	reports := []*OracleReport{
		{Reporter: [crypto.AddressSize]byte{1}, Price: 1_000_000},
		{Reporter: [crypto.AddressSize]byte{2}, Price: 2_000_000},
		{Reporter: [crypto.AddressSize]byte{3}, Price: 3_000_000},
		{Reporter: [crypto.AddressSize]byte{4}, Price: 4_000_000},
	}
	median, err := AggregateReports(reports)
	if err != nil {
		t.Fatalf("aggregate reports failed: %v", err)
	}
	// Middle elements: 2,000,000 and 3,000,000 -> average 2,500,000
	if median != 2_500_000 {
		t.Fatalf("expected median 2,500,000, got %d", median)
	}
}

func TestAggregateReports_Empty(t *testing.T) {
	_, err := AggregateReports(nil)
	if err == nil {
		t.Fatal("expected error on empty reports")
	}
}

func TestFetchESGScore_ReturnsError(t *testing.T) {
	n := NewNode("oracle-2", 100, "http://esg")
	score, err := n.FetchESGScore("company-abc")
	if err == nil {
		t.Error("expected error from FetchESGScore until a certified registry is wired")
	}
	if score != 0 {
		t.Errorf("expected score 0 on error, got %d", score)
	}
}

func TestSubmitTransaction(t *testing.T) {
	n := NewNode("oracle-3", 200, "http://rpc")
	if err := n.SubmitTransaction("0xcontract", 42); err != nil {
		t.Fatalf("SubmitTransaction should not return error, got: %v", err)
	}
	if err := n.SubmitTransaction("", 42); err == nil {
		t.Fatal("expected error for empty contract address")
	}
}
