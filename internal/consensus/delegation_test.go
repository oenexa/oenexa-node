package consensus_test

import (
	"testing"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

func TestDelegationManager_Lifecycle(t *testing.T) {
	valWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("val wallet: %v", err)
	}
	delWallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("del wallet: %v", err)
	}

	val := &consensus.Validator{
		Address:     valWallet.Address,
		PublicKey:   valWallet.PublicKey,
		VotingPower: 10,
	}
	vs, err := consensus.NewValidatorSet([]*consensus.Validator{val})
	if err != nil {
		t.Fatalf("validator set: %v", err)
	}

	dm := consensus.NewDelegationManager(vs, 10) // 10 blocks unbonding delay

	st := state.NewStateDB()
	initialBalance := uint64(500_000_000) // 500 OEN units
	st.SetAccount(delWallet.Address, &state.Account{Balance: initialBalance})

	// 1. Delegate 100_000_000
	delegateAmt := uint64(100_000_000)
	if err := dm.Delegate(st, delWallet.Address, valWallet.Address, delegateAmt, 1); err != nil {
		t.Fatalf("delegate failed: %v", err)
	}

	// Verify balance decremented
	delAcc := st.GetAccount(delWallet.Address)
	if delAcc.Balance != initialBalance-delegateAmt {
		t.Fatalf("expected balance %d, got %d", initialBalance-delegateAmt, delAcc.Balance)
	}

	// Check active delegations
	dels := dm.GetDelegations(delWallet.Address)
	if len(dels) != 1 || dels[0].Amount != delegateAmt {
		t.Fatalf("expected delegation amount %d, got %v", delegateAmt, dels)
	}

	if dm.TotalDelegatedStake() != delegateAmt {
		t.Fatalf("total delegated mismatch: %d", dm.TotalDelegatedStake())
	}

	// 2. Distribute block reward of 10_000
	blockReward := uint64(10_000)
	commission := dm.DistributeBlockReward(valWallet.Address, blockReward)
	if commission != 500 { // 5% of 10,000 = 500
		t.Fatalf("expected commission 500, got %d", commission)
	}

	// Claim rewards
	claimed, err := dm.ClaimRewards(st, delWallet.Address, valWallet.Address)
	if err != nil {
		t.Fatalf("claim rewards failed: %v", err)
	}
	if claimed != 9500 { // 95% of 10,000 = 9500
		t.Fatalf("expected claimed 9500, got %d", claimed)
	}

	// 3. Undelegate 50_000_000
	unbondAmt := uint64(50_000_000)
	entry, err := dm.Undelegate(delWallet.Address, valWallet.Address, unbondAmt, 5)
	if err != nil {
		t.Fatalf("undelegate failed: %v", err)
	}
	if entry.CompletionHeight != 15 { // 5 + 10 = 15
		t.Fatalf("expected completion height 15, got %d", entry.CompletionHeight)
	}

	// At height 10 (not yet matured)
	matured, err := dm.ProcessMaturedUnbondings(st, 10)
	if err != nil || len(matured) != 0 {
		t.Fatalf("unbondings matured prematurely: %v", matured)
	}

	// At height 15 (matured)
	matured, err = dm.ProcessMaturedUnbondings(st, 15)
	if err != nil || len(matured) != 1 {
		t.Fatalf("expected 1 matured unbonding, got %d", len(matured))
	}

	// 4. Slashing test
	slashed := dm.SlashValidatorDelegations(valWallet.Address, 10) // 10% penalty
	if slashed != 5_000_000 {                                     // 10% of remaining 50M = 5M
		t.Fatalf("expected slashed 5,000,000, got %d", slashed)
	}
}
