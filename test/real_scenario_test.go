package test_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// TestRealScenario_FullBlockchainLifecycle tests a complete real-world scenario:
// 1. Initialize validator, Alice, Bob, and Charlie wallets with ML-DSA-65 post-quantum cryptography.
// 2. Genesis state creation with transparent fund allocations.
// 3. Mempool initialization with capacity and priority queues.
// 4. BFT consensus engine creation with active validator set.
// 5. JSON-RPC server exposure for Web3/Oenexa API endpoints.
// 6. Alice signs and broadcasts a transfer to Bob via JSON-RPC.
// 7. Validator produces and commits block 1.
// 8. Bob receives transparent funds, sender pays base fee + tip.
// 9. Alice deposits transparent OEN into the shielded privacy pool (Shield transaction).
// 10. Validator produces and commits block 2.
// 11. Turnstile invariant verified: Transparent Supply + Shielded Supply == Total Supply.
// 12. Complete Web3 and Oenexa JSON-RPC methods queried and validated.
func TestRealScenario_FullBlockchainLifecycle(t *testing.T) {
	// ── 1. Create Wallets ──────────────────────────────────────────────────
	valWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("failed to create validator wallet: %v", err)
	}
	aliceWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("failed to create Alice wallet: %v", err)
	}
	bobWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("failed to create Bob wallet: %v", err)
	}

	// ── 2. Genesis State Setup ─────────────────────────────────────────────
	st := state.NewStateDB()
	initialAliceBal := 5_000 * core.OneOEN
	st.SetAccount(aliceWallet.Address, &state.Account{
		Balance: initialAliceBal,
		Nonce:   0,
	})
	st.SetAccount(valWallet.Address, &state.Account{
		Balance: 1_000 * core.OneOEN,
		Nonce:   0,
	})

	genesisRoot := st.CommitRoot()
	genesisBlk, err := core.NewBlock(
		0,
		crypto.ZeroHash,
		genesisRoot,
		time.Now().UnixNano(),
		valWallet.Address,
		nil,
		0,
		core.InitialBaseFee,
		0,
	)
	if err != nil {
		t.Fatalf("failed to create genesis block: %v", err)
	}

	// ── 3. Validator Set & Consensus Engine ────────────────────────────────
	vs, err := consensus.NewValidatorSet([]*consensus.Validator{
		{
			Address:     valWallet.Address,
			PublicKey:   valWallet.PublicKey,
			VotingPower: 100,
		},
	})
	if err != nil {
		t.Fatalf("failed to create validator set: %v", err)
	}

	pool := mempool.New(10_000)
	engine := consensus.NewEngine(
		valWallet.Address,
		valWallet.PublicKey,
		valWallet.PrivateKey,
		vs,
		st,
		pool,
		genesisBlk,
		nil,
		nil,
	)

	// ── 4. Setup RPC Server ────────────────────────────────────────────────
	api := rpc.NewAPI(engine, st, pool, nil, func() int { return 3 }, "1.0.0")
	server := rpc.NewServer("127.0.0.1:0", api, 5*time.Second, 5*time.Second)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// ── 5. Verify Initial RPC Queries ──────────────────────────────────────
	// Test eth_chainId
	chainIDRes := callRPC(t, ts.URL, "eth_chainId", nil)
	if chainIDRes != fmt.Sprintf("0x%x", core.ChainID) {
		t.Errorf("eth_chainId: want 0x%x, got %v", core.ChainID, chainIDRes)
	}

	// Test net_version
	netVerRes := callRPC(t, ts.URL, "net_version", nil)
	if netVerRes != fmt.Sprintf("%d", core.ChainID) {
		t.Errorf("net_version: want %d, got %v", core.ChainID, netVerRes)
	}

	// Test web3_clientVersion
	clientVer := callRPC(t, ts.URL, "web3_clientVersion", nil)
	if clientVer != "Oenexa/v1.0.0" {
		t.Errorf("web3_clientVersion: want Oenexa/v1.0.0, got %v", clientVer)
	}

	// Test oen_chainInfo
	chainInfoRaw := callRPC(t, ts.URL, "oen_chainInfo", nil)
	var chainInfo rpc.ChainInfo
	reencodeJSON(chainInfoRaw, &chainInfo)
	if chainInfo.Height != 1 { // Genesis block is height 0, engine length is 1
		t.Errorf("chainInfo.Height: want 1, got %d", chainInfo.Height)
	}
	if chainInfo.PeerCount != 3 {
		t.Errorf("chainInfo.PeerCount: want 3, got %d", chainInfo.PeerCount)
	}

	// Query Alice balance via eth_getBalance
	aliceBalRes := callRPC(t, ts.URL, "eth_getBalance", []string{crypto.AddressToHex(aliceWallet.Address)})
	var balMap map[string]interface{}
	reencodeJSON(aliceBalRes, &balMap)
	if uint64(balMap["balance"].(float64)) != initialAliceBal {
		t.Errorf("Alice balance mismatch: want %d, got %v", initialAliceBal, balMap["balance"])
	}

	// ── 6. Create, Sign & Send Alice -> Bob Transfer ───────────────────────
	transferAmount := 250 * core.OneOEN
	gasPrice := core.InitialBaseFee + 5 // base fee + priority tip
	tx1 := core.NewTransfer(
		aliceWallet.Address,
		bobWallet.Address,
		aliceWallet.PublicKey,
		0,
		transferAmount,
		gasPrice,
	)
	if err := tx1.Sign(aliceWallet.PrivateKey); err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}

	tx1Bytes, err := rpc.GobEncodeTx(tx1)
	if err != nil {
		t.Fatalf("failed to gob-encode tx: %v", err)
	}
	tx1Hex := hex.EncodeToString(tx1Bytes)

	// Broadcast via eth_sendRawTransaction
	sendRes := callRPC(t, ts.URL, "eth_sendRawTransaction", []string{tx1Hex})
	var sendMap map[string]string
	reencodeJSON(sendRes, &sendMap)
	if sendMap["tx_hash"] != crypto.ToHex(tx1.Hash) {
		t.Errorf("tx_hash mismatch: want %s, got %s", crypto.ToHex(tx1.Hash), sendMap["tx_hash"])
	}

	// Mempool should have 1 pending transaction
	if pool.Len() != 1 {
		t.Fatalf("expected 1 tx in mempool, got %d", pool.Len())
	}

	// ── 7. Produce and Commit Block 1 ──────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Propose block
	proposalBlk, pState, err := engine.BuildBlock(1)
	if err != nil {
		t.Fatalf("failed to build block 1: %v", err)
	}
	if len(proposalBlk.Txs) != 1 {
		t.Fatalf("expected 1 tx in block 1, got %d", len(proposalBlk.Txs))
	}

	// Commit block 1
	if err := engine.Commit(proposalBlk, pState); err != nil {
		t.Fatalf("failed to commit block 1: %v", err)
	}

	// Mempool should now be purged
	if pool.Len() != 0 {
		t.Errorf("mempool not purged after commit, len=%d", pool.Len())
	}

	// ── 8. Verify Balances and Receipts ────────────────────────────────────
	bobBalRes := callRPC(t, ts.URL, "oen_getBalance", []string{crypto.AddressToHex(bobWallet.Address)})
	reencodeJSON(bobBalRes, &balMap)
	if uint64(balMap["balance"].(float64)) != transferAmount {
		t.Errorf("Bob balance: want %d, got %v", transferAmount, balMap["balance"])
	}

	// Verify Alice balance deducted transfer amount + gas used
	aliceBalRes = callRPC(t, ts.URL, "eth_getBalance", []string{crypto.AddressToHex(aliceWallet.Address)})
	reencodeJSON(aliceBalRes, &balMap)
	gasCost := core.GasTransfer * gasPrice
	expectedAliceBal := initialAliceBal - transferAmount - gasCost
	if uint64(balMap["balance"].(float64)) != expectedAliceBal {
		t.Errorf("Alice balance: want %d, got %v", expectedAliceBal, balMap["balance"])
	}

	// Query transaction receipt
	receiptRes := callRPC(t, ts.URL, "eth_getTransactionReceipt", []string{"0x" + crypto.ToHex(tx1.Hash)})
	var receipt rpc.TxReceipt
	reencodeJSON(receiptRes, &receipt)
	if receipt.Status != "0x1" {
		t.Errorf("receipt.Status: want 0x1, got %s", receipt.Status)
	}
	if receipt.BlockNumber != "0x1" {
		t.Errorf("receipt.BlockNumber: want 0x1, got %s", receipt.BlockNumber)
	}

	// Query transaction by hash
	txByHashRes := callRPC(t, ts.URL, "eth_getTransactionByHash", []string{"0x" + crypto.ToHex(tx1.Hash)})
	var txInfo rpc.TxInfo
	reencodeJSON(txByHashRes, &txInfo)
	if txInfo.Amount != transferAmount {
		t.Errorf("txInfo.Amount: want %d, got %d", transferAmount, txInfo.Amount)
	}

	// Query block 1 by number
	blkByNumRes := callRPC(t, ts.URL, "eth_getBlockByNumber", []interface{}{1})
	var blkInfo rpc.BlockInfo
	reencodeJSON(blkByNumRes, &blkInfo)
	if blkInfo.Height != 1 {
		t.Errorf("blkInfo.Height: want 1, got %d", blkInfo.Height)
	}
	if blkInfo.TxCount != 1 {
		t.Errorf("blkInfo.TxCount: want 1, got %d", blkInfo.TxCount)
	}

	// ── 9. Shield Operation: Alice Deposits to Shielded Pool ───────────────
	shieldAmount := 100 * core.OneOEN
	var noteCommitment [32]byte
	h := crypto.Hash256([]byte("alice_private_shielded_note_data"))
	copy(noteCommitment[:], h[:])

	txShield := core.NewShield(
		aliceWallet.Address,
		aliceWallet.PublicKey,
		1, // Alice's next nonce
		shieldAmount,
		core.InitialBaseFee,
		noteCommitment,
		[]byte("encrypted_note_memo"),
	)
	if err := txShield.Sign(aliceWallet.PrivateKey); err != nil {
		t.Fatalf("failed to sign shield tx: %v", err)
	}

	if err := pool.Add(txShield); err != nil {
		t.Fatalf("failed to add shield tx to mempool: %v", err)
	}

	// ── 10. Produce and Commit Block 2 ─────────────────────────────────────
	proposalBlk2, pState2, err := engine.BuildBlock(2)
	if err != nil {
		t.Fatalf("failed to build block 2: %v", err)
	}
	if err := engine.Commit(proposalBlk2, pState2); err != nil {
		t.Fatalf("failed to commit block 2: %v", err)
	}

	// ── 11. Verify Shielded Pool Balance & Turnstile Invariant ─────────────
	shieldRes := callRPC(t, ts.URL, "oen_getShieldedBalance", nil)
	var shieldMap map[string]interface{}
	reencodeJSON(shieldRes, &shieldMap)
	if uint64(shieldMap["shielded_balance_nano_oen"].(float64)) != shieldAmount {
		t.Errorf("shielded balance: want %d, got %v", shieldAmount, shieldMap["shielded_balance_nano_oen"])
	}
	if uint64(shieldMap["tree_leaf_count"].(float64)) != 1 {
		t.Errorf("tree leaf count: want 1, got %v", shieldMap["tree_leaf_count"])
	}

	// Query Turnstile Status via RPC
	turnstileRes := callRPC(t, ts.URL, "oen_getTurnstileStatus", nil)
	var turnstileMap map[string]interface{}
	reencodeJSON(turnstileRes, &turnstileMap)
	if turnstileMap["status"] != "SECURE" {
		t.Errorf("turnstile status: want SECURE, got %v", turnstileMap["status"])
	}
	if turnstileMap["invariant_preserved"] != true {
		t.Errorf("turnstile invariant not preserved!")
	}

	// ── 12. Query Recent Blocks and Fee Estimates ──────────────────────────
	feeEstRes := callRPC(t, ts.URL, "oen_feeEstimate", nil)
	var feeEst rpc.FeeEstimateResult
	reencodeJSON(feeEstRes, &feeEst)
	if feeEst.BaseFee < core.MinBaseFee {
		t.Errorf("fee estimate base fee below minimum: %d", feeEst.BaseFee)
	}

	recentRes := callRPC(t, ts.URL, "oen_recentBlocks", []uint64{5})
	var recentBlocks []*rpc.BlockInfo
	reencodeJSON(recentRes, &recentBlocks)
	if len(recentBlocks) != 3 { // genesis, block 1, block 2
		t.Errorf("recent blocks count: want 3, got %d", len(recentBlocks))
	}
	// ── 13. Query Tokenomics & Exchange Supply Endpoints ──────────────────
	circSupplyRes := callRPC(t, ts.URL, "oen_getCirculatingSupply", nil)
	var circMap map[string]interface{}
	reencodeJSON(circSupplyRes, &circMap)
	if circMap["ticker"] != "OEN" {
		t.Errorf("ticker: want OEN, got %v", circMap["ticker"])
	}
	if circMap["circulating_supply_oen"].(float64) <= 0 {
		t.Errorf("circulating supply should be positive")
	}

	tokenomicsRes := callRPC(t, ts.URL, "oen_getTokenomics", nil)
	var tMap map[string]interface{}
	reencodeJSON(tokenomicsRes, &tMap)
	if tMap["max_supply_oen"].(float64) != 100_000_000 {
		t.Errorf("max supply: want 100M, got %v", tMap["max_supply_oen"])
	}

	stakingRes := callRPC(t, ts.URL, "oen_getStakingInfo", nil)
	var sMap map[string]interface{}
	reencodeJSON(stakingRes, &sMap)
	if sMap["target_bonded_ratio"].(float64) != 0.67 {
		t.Errorf("target bonded ratio: want 0.67, got %v", sMap["target_bonded_ratio"])
	}

	_ = ctx
}

func callRPC(t *testing.T, endpoint, method string, params interface{}) interface{} {
	t.Helper()
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("callRPC marshal: %v", err)
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("callRPC POST %s: %v", method, err)
	}
	defer resp.Body.Close()

	var r struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("callRPC decode: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("RPC error in %s: [%d] %s", method, r.Error.Code, r.Error.Message)
	}
	return r.Result
}

func reencodeJSON(in, out interface{}) {
	b, _ := json.Marshal(in)
	_ = json.Unmarshal(b, out)
}
// TestRealScenario_UnshieldAndDoubleSpendPrevention tests:
// 1. Unshielding funds from the shielded pool back to a transparent address.
// 2. Rejecting duplicate unshield attempts with the same nullifier (anti-double-spend).
// 3. Rejecting transactions with invalid/replayed nonces.
// 4. Rejecting transactions with tampered signatures.
func TestRealScenario_UnshieldAndDoubleSpendPrevention(t *testing.T) {
	valWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	userWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}

	st := state.NewStateDB()
	st.SetAccount(userWallet.Address, &state.Account{
		Balance: 1_000 * core.OneOEN,
		Nonce:   0,
	})
	// Fund shielded pool directly with 500 OEN
	st.CreditShieldedPool(500 * core.OneOEN)

	vs, err := consensus.NewValidatorSet([]*consensus.Validator{
		{Address: valWallet.Address, PublicKey: valWallet.PublicKey, VotingPower: 100},
	})
	if err != nil {
		t.Fatal(err)
	}

	genesisBlk, _ := core.NewBlock(0, crypto.ZeroHash, st.CommitRoot(), time.Now().Add(-10*time.Second).UnixNano(), valWallet.Address, nil, 0, core.InitialBaseFee, 0)
	pool := mempool.New(1000)
	engine := consensus.NewEngine(valWallet.Address, valWallet.PublicKey, valWallet.PrivateKey, vs, st, pool, genesisBlk, nil, nil)

	// 1. Prepare valid unshield transaction
	unshieldAmount := 200 * core.OneOEN
	var nullifier [crypto.HashSize]byte
	h := crypto.Hash256([]byte("unique_secret_nullifier_001"))
	copy(nullifier[:], h[:])

	txUnshield := core.NewUnshield(
		userWallet.Address,
		userWallet.PublicKey,
		0,
		unshieldAmount,
		core.InitialBaseFee,
		nullifier,
		[]byte("valid_zk_proof_bytes"),
	)
	if err := txUnshield.Sign(userWallet.PrivateKey); err != nil {
		t.Fatalf("failed to sign unshield tx: %v", err)
	}

	if err := pool.Add(txUnshield); err != nil {
		t.Fatalf("failed to add unshield tx: %v", err)
	}

	// Produce Block 1
	blk1, snap1, err := engine.BuildBlock(1)
	if err != nil {
		t.Fatalf("build block failed: %v", err)
	}
	if err := engine.Commit(blk1, snap1); err != nil {
		t.Fatalf("commit block failed: %v", err)
	}

	// Verify user received unshielded funds
	userAcc := st.GetAccount(userWallet.Address)
	expectedUserBal := 1_000*core.OneOEN + unshieldAmount - (core.GasUnshield * core.InitialBaseFee)
	if userAcc.Balance != expectedUserBal {
		t.Errorf("user balance mismatch after unshield: want %d, got %d", expectedUserBal, userAcc.Balance)
	}

	// Verify shielded pool balance decreased by exactly unshieldAmount
	if st.GetShieldedBalance() != 300*core.OneOEN {
		t.Errorf("shielded pool balance: want %d, got %d", 300*core.OneOEN, st.GetShieldedBalance())
	}

	// 2. Anti-Double-Spend: Attempt to reuse the exact same nullifier in a new tx
	txDoubleSpend := core.NewUnshield(
		userWallet.Address,
		userWallet.PublicKey,
		1, // Next nonce
		unshieldAmount,
		core.InitialBaseFee,
		nullifier, // Same nullifier!
		[]byte("fake_duplicate_zk_proof"),
	)
	_ = txDoubleSpend.Sign(userWallet.PrivateKey)

	// Block builder should skip or reject the double-spend tx
	_ = pool.Add(txDoubleSpend)
	blk2, snap2, err := engine.BuildBlock(2)
	if err != nil {
		t.Fatalf("build block 2 failed: %v", err)
	}
	if len(blk2.Txs) != 0 {
		t.Errorf("double-spend tx was incorrectly included in block 2!")
	}
	_ = engine.Commit(blk2, snap2)
	// Purge the rejected double spend from mempool
	pool.PurgeCommitted([]*core.Transaction{txDoubleSpend})

	// 3. Nonce Replay attack: Submit tx with already used nonce
	txReplay := core.NewTransfer(
		userWallet.Address,
		valWallet.Address,
		userWallet.PublicKey,
		0, // Already used nonce 0!
		10*core.OneOEN,
		core.InitialBaseFee,
	)
	_ = txReplay.Sign(userWallet.PrivateKey)
	if err := pool.Add(txReplay); err == nil {
		// Even if mempool accepted, block builder must skip it
		blk3, snap3, err := engine.BuildBlock(3)
		if err != nil {
			t.Fatalf("build block 3 failed: %v", err)
		}
		if len(blk3.Txs) != 0 {
			t.Errorf("replayed nonce tx was incorrectly included in block!")
		}
		_ = engine.Commit(blk3, snap3)
	}

	// 4. Tampered Signature attack: Tamper with transaction amount
	txTampered := core.NewTransfer(
		userWallet.Address,
		valWallet.Address,
		userWallet.PublicKey,
		1,
		10*core.OneOEN,
		core.InitialBaseFee,
	)
	_ = txTampered.Sign(userWallet.PrivateKey)
	txTampered.Amount = 999_999 * core.OneOEN // Tamper after signing!
	if err := txTampered.Verify(); err == nil {
		t.Error("tampered tx should fail cryptographic signature verification")
	}
}

// TestRealScenario_MultiValidatorQuorumAndSlashing tests:
// 1. 4-validator BFT consensus.
// 2. Successful block commit when > 2/3 (3 of 4) validators vote.
// 3. Equivocation detection when a rogue validator signs two different block hashes at the same round.
// 4. Automatic slashing of the rogue validator's stake and voting power.
func TestRealScenario_MultiValidatorQuorumAndSlashing(t *testing.T) {
	// Create 4 validators
	validators := make([]*crypto.Wallet, 4)
	valSetList := make([]*consensus.Validator, 4)
	for i := 0; i < 4; i++ {
		w, err := crypto.NewWallet()
		if err != nil {
			t.Fatal(err)
		}
		validators[i] = w
		valSetList[i] = &consensus.Validator{
			Address:     w.Address,
			PublicKey:   w.PublicKey,
			VotingPower: 10,
		}
	}

	vs, err := consensus.NewValidatorSet(valSetList)
	if err != nil {
		t.Fatal(err)
	}

	st := state.NewStateDB()
	for _, w := range validators {
		st.SetAccount(w.Address, &state.Account{
			Balance: 10_000 * core.OneOEN,
		})
	}

	genesisBlk, _ := core.NewBlock(0, crypto.ZeroHash, st.CommitRoot(), time.Now().Add(-10*time.Second).UnixNano(), validators[0].Address, nil, 0, core.InitialBaseFee, 0)
	pool := mempool.New(100)

	// Engine runs as validator 1 (since Proposer for height 1 is 1 % 4 = validators[1])
	engine := consensus.NewEngine(
		validators[1].Address,
		validators[1].PublicKey,
		validators[1].PrivateKey,
		vs,
		st,
		pool,
		genesisBlk,
		nil,
		nil,
	)

	// Propose Block 1
	blk1, snap1, err := engine.BuildBlock(1)
	if err != nil {
		t.Fatalf("build block 1 failed: %v", err)
	}

	// Process proposal in engine
	err = engine.ProcessProposal(blk1)
	if err != nil {
		t.Fatalf("process proposal: %v", err)
	}

	// Validator 0 and 2 cast prevotes (Total: Val1 (local) + Val0 + Val2 = 30/40 power = 75% > 66.7% quorum!)
	v0Prevote := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: blk1.Hash,
		Voter:     validators[0].Address,
		PublicKey: validators[0].PublicKey,
	}
	_ = v0Prevote.Sign(validators[0].PrivateKey)
	if err := engine.ProcessVote(v0Prevote); err != nil {
		t.Fatalf("v0 prevote failed: %v", err)
	}

	v2Prevote := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: blk1.Hash,
		Voter:     validators[2].Address,
		PublicKey: validators[2].PublicKey,
	}
	_ = v2Prevote.Sign(validators[2].PrivateKey)
	if err := engine.ProcessVote(v2Prevote); err != nil {
		t.Fatalf("v2 prevote failed: %v", err)
	}

	// Now test Equivocation: Validator 3 votes for a DIFFERENT block hash at the same round
	fakeHash := crypto.Hash256([]byte("fake_conflicting_block"))
	v3VoteA := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: blk1.Hash,
		Voter:     validators[3].Address,
		PublicKey: validators[3].PublicKey,
	}
	_ = v3VoteA.Sign(validators[3].PrivateKey)
	_ = engine.ProcessVote(v3VoteA)

	// Equivocation vote from Validator 3
	v3VoteB := &consensus.Vote{
		Type:      consensus.VotePrevote,
		Height:    1,
		Round:     0,
		BlockHash: fakeHash,
		Voter:     validators[3].Address,
		PublicKey: validators[3].PublicKey,
	}
	_ = v3VoteB.Sign(validators[3].PrivateKey)

	// Processing the conflicting vote MUST detect equivocation and slash Validator 3!
	slashErr := engine.ProcessVote(v3VoteB)
	if slashErr == nil {
		t.Error("expected equivocation error and slashing, got nil")
	}

	// Check that Validator 3 was slashed:
	// Initial balance was 10,000 OEN -> 10% penalty slashed (9,000 OEN remaining)
	val3Acc := st.GetAccount(validators[3].Address)
	expectedVal3Bal := 9_000 * core.OneOEN
	if val3Acc.Balance != expectedVal3Bal {
		t.Errorf("val3 slashed balance: want %d, got %d", expectedVal3Bal, val3Acc.Balance)
	}

	// Check that Validator 3 voting power was slashed in validator set
	v3Entry := vs.Get(validators[3].Address)
	if v3Entry.VotingPower >= 10 {
		t.Errorf("val3 voting power not slashed: %d", v3Entry.VotingPower)
	}
	_ = snap1
}
