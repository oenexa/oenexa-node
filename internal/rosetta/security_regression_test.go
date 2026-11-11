package rosetta_test

import (
	"net/http"
	"testing"

	"github.com/oenexa/oenexa/internal/rosetta"
)

func TestSecurity_RosettaNegativeIndicesAndMalformedConstruction(t *testing.T) {
	ts, _, _, _, _, w := setupRosettaTestServer(t)
	netID := rosetta.NetworkIdentifier{
		Blockchain: "oenexa",
		Network:    "mainnet",
	}

	// 1. POST /block rejects negative block_identifier.index
	neg := int64(-5)
	resp, _ := postJSON(t, ts.URL+"/block", rosetta.BlockRequest{
		NetworkIdentifier: netID,
		BlockIdentifier:   rosetta.PartialBlockIdentifier{Index: &neg},
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 error for negative /block index, got %d", resp.StatusCode)
	}

	// 2. POST /block/transaction rejects negative block_identifier.index
	resp, _ = postJSON(t, ts.URL+"/block/transaction", rosetta.BlockTransactionRequest{
		NetworkIdentifier: netID,
		BlockIdentifier:   rosetta.BlockIdentifier{Index: -1},
		TransactionIdentifier: rosetta.TransactionIdentifier{
			Hash: "0x0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 error for negative /block/transaction index, got %d", resp.StatusCode)
	}

	// 3. POST /construction/combine rejects empty signatures slice
	resp, _ = postJSON(t, ts.URL+"/construction/combine", map[string]interface{}{
		"network_identifier":   netID,
		"unsigned_transaction": "deadbeef",
		"signatures":           []interface{}{},
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 error for empty /construction/combine signatures, got %d", resp.StatusCode)
	}

	// 4. POST /construction/payloads rejects malformed negative metadata values
	_ = w
	resp, _ = postJSON(t, ts.URL+"/construction/payloads", map[string]interface{}{
		"network_identifier": netID,
		"operations": []map[string]interface{}{
			{
				"operation_identifier": map[string]int{"index": 0},
				"type":                 "TRANSFER",
				"account":              map[string]string{"address": "0x01"},
				"amount": map[string]interface{}{
					"value":    "-100",
					"currency": map[string]interface{}{"symbol": "ONXC", "decimals": 9},
				},
			},
		},
		"metadata": map[string]interface{}{
			"nonce":     -1,
			"gas_price": -5,
		},
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 error for malformed negative /construction/payloads metadata, got %d", resp.StatusCode)
	}
}
