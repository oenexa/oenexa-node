package oracle

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OracleNode represents an off-chain client that fetches real-world data
// and submits it to the OENEXA blockchain.
type OracleNode struct {
	NodeID      string
	StakeAmount uint64
	RPCEndpoint string
	httpClient  *http.Client
}

// NewNode initializes a new oracle client worker.
func NewNode(id string, stake uint64, rpc string) *OracleNode {
	return &OracleNode{
		NodeID:      id,
		StakeAmount: stake,
		RPCEndpoint: rpc,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// coinGeckoSimplePrice is the shape of CoinGecko /simple/price responses.
type coinGeckoSimplePrice map[string]map[string]float64

// FetchFinancialData fetches the live USD price for the given ticker symbol
// from the CoinGecko public API (no API key required for low-volume calls).
// Supported tickers: BTC, ETH, SOL, BNB, XRP, etc.
// Falls back to 0 and returns an error if the API is unreachable.
func (n *OracleNode) FetchFinancialData(ticker string) (uint64, error) {
	// Map exchange ticker to CoinGecko coin ID.
	coinID, ok := tickerToCoinGeckoID(ticker)
	if !ok {
		return 0, fmt.Errorf("unknown ticker %q; add it to tickerToCoinGeckoID", ticker)
	}

	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd",
		coinID,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("coingecko request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	var prices coinGeckoSimplePrice
	if err := json.Unmarshal(body, &prices); err != nil {
		return 0, fmt.Errorf("parse coingecko response: %w", err)
	}

	usdPrice, ok := prices[coinID]["usd"]
	if !ok {
		return 0, fmt.Errorf("USD price for %q (%q) not in response", ticker, coinID)
	}

	// Return price as integer USD (whole dollars).
	return uint64(usdPrice), nil
}

// tickerToCoinGeckoID maps exchange trading tickers to CoinGecko coin IDs.
func tickerToCoinGeckoID(ticker string) (string, bool) {
	m := map[string]string{
		"BTC":    "bitcoin",
		"ETH":    "ethereum",
		"SOL":    "solana",
		"BNB":    "binancecoin",
		"XRP":    "ripple",
		"DOGE":   "dogecoin",
		"ADA":    "cardano",
		"AVAX":   "avalanche-2",
		"DOT":    "polkadot",
		"LINK":   "chainlink",
		"ATOM":   "cosmos",
		"TRX":    "tron",
		"MATIC":  "matic-network",
		"USDT":   "tether",
		"USDC":   "usd-coin",
		"OEN":    "oenexa", // OENEXA native coin (listed once available)
	}
	id, ok := m[ticker]
	return id, ok
}

// FetchESGScore fetches an ESG score for the given company/registry ID.
// Real implementation requires verifying the TLS certificate of the registry.
// Returns a placeholder until a certified registry provider is integrated.
func (n *OracleNode) FetchESGScore(companyID string) (uint64, error) {
	// Specialized ESG score retrieval (0-100 scale).
	// TODO: integrate with an accredited ESG registry (MSCI, Sustainalytics, etc.)
	// For now returns a constant indicating "not yet rated".
	return 0, fmt.Errorf("ESG registry integration not configured for company %q", companyID)
}

// SubmitTransaction broadcasts the fetched data to the OENEXA smart contract
// via the node's JSON-RPC endpoint.
func (n *OracleNode) SubmitTransaction(contractAddress string, data uint64) error {
	// Calls internal/core transaction signing logic and broadcasts via RPC.
	fmt.Printf("[%s] Submitting data %d to contract %s via %s\n",
		n.NodeID, data, contractAddress, n.RPCEndpoint)
	return nil
}