package rosetta

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/state"
)

func setupRosettaEnv(t *testing.T) (*Handler, *http.ServeMux, *crypto.Wallet, *crypto.Wallet, *core.Transaction) {
	t.Helper()
	w1, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet 1: %v", err)
	}
	w2, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet 2: %v", err)
	}
	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{w1.Address}).Build()
	st := state.NewStateDB()
	st.SetAccount(w1.Address, &state.Account{Balance: 100_000_000, Nonce: 0})
	pool := mempool.New(100)
	vs, _ := consensus.NewValidatorSet([]*consensus.Validator{
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: 10},
	})
	eng := consensus.NewEngine(w1.Address, w1.PublicKey, w1.PrivateKey, vs, st, pool, genesis, nil, nil)

	tx := core.NewTransfer(w1.Address, w2.Address, w1.PublicKey, 0, 1_000, core.InitialBaseFee)
	if err := tx.Sign(w1.PrivateKey); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := pool.Add(tx); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	var broadcastCount int
	h := NewHandler(eng, st, pool, func() int { return 3 }, func(_ *core.Transaction) {
		broadcastCount++
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux, w1, w2, tx
}

func postRosetta(t *testing.T, mux *http.ServeMux, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestRosetta_MempoolAndFullConstructionLifecycle(t *testing.T) {
	_, mux, w1, w2, tx := setupRosettaEnv(t)
	netID := NetworkIdentifier{Blockchain: "oenexa", Network: "mainnet"}

	// /mempool
	rrMem := postRosetta(t, mux, "/mempool", map[string]any{"network_identifier": netID})
	if rrMem.Code != http.StatusOK {
		t.Fatalf("/mempool status=%d body=%s", rrMem.Code, rrMem.Body.String())
	}

	// /mempool/transaction found + missing
	rrMemTx := postRosetta(t, mux, "/mempool/transaction", MempoolTransactionRequest{
		NetworkIdentifier:     netID,
		TransactionIdentifier: TransactionIdentifier{Hash: crypto.ToHex(tx.Hash)},
	})
	if rrMemTx.Code != http.StatusOK {
		t.Fatalf("/mempool/transaction status=%d body=%s", rrMemTx.Code, rrMemTx.Body.String())
	}
	rrMemMiss := postRosetta(t, mux, "/mempool/transaction", MempoolTransactionRequest{
		NetworkIdentifier:     netID,
		TransactionIdentifier: TransactionIdentifier{Hash: "0000"},
	})
	if rrMemMiss.Code == http.StatusOK {
		t.Fatal("expected error for missing mempool tx")
	}

	// /construction/derive
	rrDerive := postRosetta(t, mux, "/construction/derive", ConstructionDeriveRequest{
		NetworkIdentifier: netID,
		PublicKey:         PublicKey{HexBytes: hex.EncodeToString(w1.PublicKey), CurveType: "dilithium"},
	})
	if rrDerive.Code != http.StatusOK {
		t.Fatalf("/construction/derive status=%d body=%s", rrDerive.Code, rrDerive.Body.String())
	}

	// /construction/preprocess
	ops := []*Operation{
		{
			OperationIdentifier: OperationIdentifier{Index: 0},
			Type:                "TRANSFER",
			Account:             &AccountIdentifier{Address: crypto.ToHex(w1.Address)},
			Amount:              &Amount{Value: "-500", Currency: Currency{Symbol: CurrencySymbol, Decimals: CurrencyDecimals}},
		},
		{
			OperationIdentifier: OperationIdentifier{Index: 1},
			Type:                "TRANSFER",
			Account:             &AccountIdentifier{Address: crypto.ToHex(w2.Address)},
			Amount:              &Amount{Value: "500", Currency: Currency{Symbol: CurrencySymbol, Decimals: CurrencyDecimals}},
		},
	}
	rrPre := postRosetta(t, mux, "/construction/preprocess", ConstructionPreprocessRequest{
		NetworkIdentifier: netID,
		Operations:        ops,
	})
	if rrPre.Code != http.StatusOK {
		t.Fatalf("/construction/preprocess status=%d", rrPre.Code)
	}

	// /construction/metadata
	rrMeta := postRosetta(t, mux, "/construction/metadata", ConstructionMetadataRequest{
		NetworkIdentifier: netID,
		Options: map[string]any{
			"from":       crypto.ToHex(w1.Address),
			"public_key": hex.EncodeToString(w1.PublicKey),
		},
	})
	if rrMeta.Code != http.StatusOK {
		t.Fatalf("/construction/metadata status=%d", rrMeta.Code)
	}

	// /construction/payloads -> sign -> combine -> hash -> submit (nonce 1 since nonce 0 is in mempool)
	rrPay := postRosetta(t, mux, "/construction/payloads", ConstructionPayloadsRequest{
		NetworkIdentifier: netID,
		Operations:        ops,
		Metadata: map[string]any{
			"nonce":      float64(1),
			"gas_price":  float64(core.InitialBaseFee),
			"public_key": hex.EncodeToString(w1.PublicKey),
		},
	})
	if rrPay.Code != http.StatusOK {
		t.Fatalf("/construction/payloads status=%d body=%s", rrPay.Code, rrPay.Body.String())
	}
	var payResp ConstructionPayloadsResponse
	if err := json.Unmarshal(rrPay.Body.Bytes(), &payResp); err != nil {
		t.Fatalf("Unmarshal payloads: %v", err)
	}
	signingBytes, err := hex.DecodeString(payResp.Payloads[0].HexBytes)
	if err != nil {
		t.Fatalf("DecodeString signingBytes: %v", err)
	}
	sig, err := crypto.Sign(w1.PrivateKey, signingBytes)
	if err != nil {
		t.Fatalf("Sign payload: %v", err)
	}

	rrComb := postRosetta(t, mux, "/construction/combine", ConstructionCombineRequest{
		NetworkIdentifier:   netID,
		UnsignedTransaction: payResp.UnsignedTransaction,
		Signatures: []*Signature{
			{
				SigningPayload: *payResp.Payloads[0],
				PublicKey:      PublicKey{HexBytes: hex.EncodeToString(w1.PublicKey), CurveType: "dilithium"},
				SignatureType:  "dilithium",
				HexBytes:       hex.EncodeToString(sig),
			},
		},
	})
	if rrComb.Code != http.StatusOK {
		t.Fatalf("/construction/combine status=%d body=%s", rrComb.Code, rrComb.Body.String())
	}
	var combResp ConstructionCombineResponse
	_ = json.Unmarshal(rrComb.Body.Bytes(), &combResp)

	rrHash := postRosetta(t, mux, "/construction/hash", ConstructionHashRequest{
		NetworkIdentifier: netID,
		SignedTransaction: combResp.SignedTransaction,
	})
	if rrHash.Code != http.StatusOK {
		t.Fatalf("/construction/hash status=%d body=%s", rrHash.Code, rrHash.Body.String())
	}

	rrSub := postRosetta(t, mux, "/construction/submit", ConstructionSubmitRequest{
		NetworkIdentifier: netID,
		SignedTransaction: combResp.SignedTransaction,
	})
	if rrSub.Code != http.StatusOK {
		t.Fatalf("/construction/submit status=%d body=%s", rrSub.Code, rrSub.Body.String())
	}

	// Height conversion overflow clamp.
	if safeHeightToInt64(math.MaxUint64) != math.MaxInt64 {
		t.Fatal("expected math.MaxInt64 on overflow")
	}
}
