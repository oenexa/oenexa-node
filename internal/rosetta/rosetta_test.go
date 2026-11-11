package rosetta_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/rosetta"
	"github.com/oenexa/oenexa/internal/state"
)

func setupRosettaTestServer(t *testing.T) (*httptest.Server, *consensus.Engine, *state.DB, *mempool.Mempool, *crypto.Wallet, *crypto.Wallet) {
	valWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("val wallet: %v", err)
	}
	userWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("user wallet: %v", err)
	}

	val := &consensus.Validator{
		Address:     valWallet.Address,
		PublicKey:   valWallet.PublicKey,
		VotingPower: 10,
	}
	vs, err := consensus.NewValidatorSet([]*consensus.Validator{val})
	if err != nil {
		t.Fatalf("val set: %v", err)
	}

	st := state.NewStateDB()
	st.SetAccount(valWallet.Address, &state.Account{Balance: 1_000_000_000})
	st.SetAccount(userWallet.Address, &state.Account{Balance: 500_000_000, Nonce: 3})

	genesis := core.DefaultGenesisConfig([][crypto.AddressSize]byte{valWallet.Address}).Build()
	pool := mempool.New(1000)

	engine := consensus.NewEngine(
		valWallet.Address,
		valWallet.PublicKey,
		valWallet.PrivateKey,
		vs,
		st,
		pool,
		genesis,
		nil,
		nil,
	)

	broadcastCalled := false
	handler := rosetta.NewHandler(
		engine,
		st,
		pool,
		func() int { return 8 },
		func(tx *core.Transaction) { broadcastCalled = true },
	)

	_ = broadcastCalled

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() { ts.Close() })

	return ts, engine, st, pool, valWallet, userWallet
}

func postJSON(t *testing.T, url string, body interface{}) (*http.Response, []byte) {
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("http post %s: %v", url, err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	_ = resp.Body.Close()
	return resp, buf.Bytes()
}

func TestRosetta_NetworkEndpoints(t *testing.T) {
	ts, _, _, _, _, _ := setupRosettaTestServer(t)

	// 1. /network/list
	resp, data := postJSON(t, ts.URL+"/network/list", map[string]interface{}{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("network/list status: %d", resp.StatusCode)
	}
	var listResp rosetta.NetworkListResponse
	if err := json.Unmarshal(data, &listResp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listResp.NetworkIdentifiers) != 1 || listResp.NetworkIdentifiers[0].Blockchain != "oenexa" {
		t.Fatalf("unexpected network list: %+v", listResp)
	}

	// 2. /network/status
	resp, data = postJSON(t, ts.URL+"/network/status", map[string]interface{}{
		"network_identifier": rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("network/status status: %d", resp.StatusCode)
	}
	var statusResp rosetta.NetworkStatusResponse
	if err := json.Unmarshal(data, &statusResp); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if len(statusResp.Peers) != 8 {
		t.Fatalf("expected 8 peers, got %d", len(statusResp.Peers))
	}

	// 3. /network/options
	resp, data = postJSON(t, ts.URL+"/network/options", map[string]interface{}{
		"network_identifier": rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("network/options status: %d", resp.StatusCode)
	}
	var optsResp rosetta.NetworkOptionsResponse
	if err := json.Unmarshal(data, &optsResp); err != nil {
		t.Fatalf("unmarshal options: %v", err)
	}
	if optsResp.Version.RosettaVersion != "1.4.14" {
		t.Fatalf("expected rosetta 1.4.14, got %s", optsResp.Version.RosettaVersion)
	}
}

func TestRosetta_AccountAndBlock(t *testing.T) {
	ts, engine, _, _, valWallet, userWallet := setupRosettaTestServer(t)

	// 1. /account/balance
	resp, data := postJSON(t, ts.URL+"/account/balance", rosetta.AccountBalanceRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		AccountIdentifier: rosetta.AccountIdentifier{
			Address: crypto.ToHex(userWallet.Address),
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("account/balance status: %d", resp.StatusCode)
	}
	var balResp rosetta.AccountBalanceResponse
	if err := json.Unmarshal(data, &balResp); err != nil {
		t.Fatalf("unmarshal balance: %v", err)
	}
	if len(balResp.Balances) != 1 || balResp.Balances[0].Value != "500000000" {
		t.Fatalf("unexpected balance: %+v", balResp.Balances)
	}

	// 2. /block (Genesis at index 0)
	index0 := int64(0)
	resp, data = postJSON(t, ts.URL+"/block", rosetta.BlockRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		BlockIdentifier: rosetta.PartialBlockIdentifier{
			Index: &index0,
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("block status: %d", resp.StatusCode)
	}
	var blkResp rosetta.BlockResponse
	if err := json.Unmarshal(data, &blkResp); err != nil {
		t.Fatalf("unmarshal block: %v", err)
	}
	if blkResp.Block == nil || blkResp.Block.BlockIdentifier.Index != 0 {
		t.Fatalf("expected genesis block index 0")
	}

	// Produce Block 1 with a transaction
	tx := core.NewTransfer(valWallet.Address, userWallet.Address, valWallet.PublicKey, 0, 10_000_000, core.MinGasPrice)
	_ = tx.Sign(valWallet.PrivateKey)
	
	blk1, snap, err := engine.BuildBlock(1)
	if err != nil {
		t.Fatalf("build block 1: %v", err)
	}
	blk1.Txs = []*core.Transaction{tx}
	_ = engine.Commit(blk1, snap)

	// Query Block 1
	index1 := int64(1)
	resp, data = postJSON(t, ts.URL+"/block", rosetta.BlockRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		BlockIdentifier: rosetta.PartialBlockIdentifier{
			Index: &index1,
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("block 1 status: %d", resp.StatusCode)
	}
	var blk1Resp rosetta.BlockResponse
	_ = json.Unmarshal(data, &blk1Resp)
	if len(blk1Resp.Block.Transactions) != 1 {
		t.Fatalf("expected 1 tx in block 1, got %d", len(blk1Resp.Block.Transactions))
	}

	// Query /block/transaction
	resp, _ = postJSON(t, ts.URL+"/block/transaction", rosetta.BlockTransactionRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		BlockIdentifier: rosetta.BlockIdentifier{
			Index: 1,
			Hash:  crypto.ToHex(blk1.Hash),
		},
		TransactionIdentifier: rosetta.TransactionIdentifier{
			Hash: crypto.ToHex(tx.Hash),
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("block/transaction status: %d", resp.StatusCode)
	}
}

func TestRosetta_ConstructionPipeline(t *testing.T) {
	ts, _, _, pool, _, userWallet := setupRosettaTestServer(t)

	// 1. /construction/derive
	resp, data := postJSON(t, ts.URL+"/construction/derive", rosetta.ConstructionDeriveRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		PublicKey: rosetta.PublicKey{
			HexBytes:  hex.EncodeToString(userWallet.PublicKey),
			CurveType: "dilithium",
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("derive status: %d", resp.StatusCode)
	}
	var deriveResp rosetta.ConstructionDeriveResponse
	_ = json.Unmarshal(data, &deriveResp)
	if deriveResp.AccountIdentifier.Address != crypto.ToHex(userWallet.Address) {
		t.Fatalf("derived address mismatch: %s vs %s", deriveResp.AccountIdentifier.Address, crypto.ToHex(userWallet.Address))
	}

	// 2. /construction/preprocess
	recipientWallet, _ := crypto.NewWallet()
	succ := "SUCCESS"
	ops := []*rosetta.Operation{
		{
			OperationIdentifier: rosetta.OperationIdentifier{Index: 0},
			Type:                "TRANSFER",
			Status:              &succ,
			Account:             &rosetta.AccountIdentifier{Address: crypto.ToHex(userWallet.Address)},
			Amount: &rosetta.Amount{
				Value:    "-1000000",
				Currency: rosetta.Currency{Symbol: "ONXC", Decimals: 9},
			},
		},
		{
			OperationIdentifier: rosetta.OperationIdentifier{Index: 1},
			Type:                "TRANSFER",
			Status:              &succ,
			Account:             &rosetta.AccountIdentifier{Address: crypto.ToHex(recipientWallet.Address)},
			Amount: &rosetta.Amount{
				Value:    "1000000",
				Currency: rosetta.Currency{Symbol: "ONXC", Decimals: 9},
			},
		},
	}

	resp, data = postJSON(t, ts.URL+"/construction/preprocess", rosetta.ConstructionPreprocessRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		Operations: ops,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preprocess status: %d", resp.StatusCode)
	}
	var preResp rosetta.ConstructionPreprocessResponse
	_ = json.Unmarshal(data, &preResp)

	// 3. /construction/metadata
	resp, data = postJSON(t, ts.URL+"/construction/metadata", rosetta.ConstructionMetadataRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		Options: preResp.Options,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status: %d", resp.StatusCode)
	}
	var metaResp rosetta.ConstructionMetadataResponse
	_ = json.Unmarshal(data, &metaResp)
	if metaResp.Metadata["nonce"].(float64) != 3 {
		t.Fatalf("expected nonce 3, got %v", metaResp.Metadata["nonce"])
	}
	metaResp.Metadata["public_key"] = hex.EncodeToString(userWallet.PublicKey)

	// 4. /construction/payloads
	resp, data = postJSON(t, ts.URL+"/construction/payloads", rosetta.ConstructionPayloadsRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		Operations: ops,
		Metadata:   metaResp.Metadata,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("payloads status: %d", resp.StatusCode)
	}
	var payResp rosetta.ConstructionPayloadsResponse
	_ = json.Unmarshal(data, &payResp)
	if len(payResp.Payloads) != 1 {
		t.Fatalf("expected 1 signing payload")
	}

	// Sign payload with user's ML-DSA-65 private key
	payloadBytes, _ := hex.DecodeString(payResp.Payloads[0].HexBytes)
	sig, err := crypto.Sign(userWallet.PrivateKey, payloadBytes)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}

	// 5. /construction/combine
	resp, data = postJSON(t, ts.URL+"/construction/combine", rosetta.ConstructionCombineRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		UnsignedTransaction: payResp.UnsignedTransaction,
		Signatures: []*rosetta.Signature{
			{
				SigningPayload: *payResp.Payloads[0],
				PublicKey: rosetta.PublicKey{
					HexBytes:  hex.EncodeToString(userWallet.PublicKey),
					CurveType: "dilithium",
				},
				SignatureType: "dilithium",
				HexBytes:      hex.EncodeToString(sig),
			},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("combine status: %d", resp.StatusCode)
	}
	var combResp rosetta.ConstructionCombineResponse
	_ = json.Unmarshal(data, &combResp)

	// 6. /construction/hash
	resp, data = postJSON(t, ts.URL+"/construction/hash", rosetta.ConstructionHashRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		SignedTransaction: combResp.SignedTransaction,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hash status: %d", resp.StatusCode)
	}
	var hashResp rosetta.ConstructionHashResponse
	_ = json.Unmarshal(data, &hashResp)
	if len(hashResp.TransactionIdentifier.Hash) != 64 {
		t.Fatalf("invalid tx hash length: %s", hashResp.TransactionIdentifier.Hash)
	}

	// 7. /construction/submit
	resp, _ = postJSON(t, ts.URL+"/construction/submit", rosetta.ConstructionSubmitRequest{
		NetworkIdentifier: rosetta.NetworkIdentifier{
			Blockchain: "oenexa",
			Network:    "mainnet",
		},
		SignedTransaction: combResp.SignedTransaction,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit status: %d", resp.StatusCode)
	}
	if pool.Len() != 1 {
		t.Fatalf("expected tx in mempool, pool len=%d", pool.Len())
	}
}
