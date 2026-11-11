package consensus

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

func TestSecurity_ValidatorSetOverflowNilAnd128BitQuorum(t *testing.T) {
	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()

	if _, err := NewValidatorSet([]*Validator{nil}); err == nil {
		t.Fatal("expected error for nil validator")
	}

	if _, err := NewValidatorSet([]*Validator{
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: math.MaxUint64},
		{Address: w2.Address, PublicKey: w2.PublicKey, VotingPower: 2},
	}); err == nil {
		t.Fatal("expected VotingPower uint64 overflow error")
	}

	// Test 128-bit quorum check where 3*power would overflow uint64.
	halfPlus := uint64(1)<<63 - 10
	vs, err := NewValidatorSet([]*Validator{
		{Address: w1.Address, PublicKey: w1.PublicKey, VotingPower: halfPlus},
		{Address: w2.Address, PublicKey: w2.PublicKey, VotingPower: 30},
	})
	if err != nil {
		t.Fatalf("NewValidatorSet: %v", err)
	}
	// w1 alone holds > 2/3 of totalPower (~1<<63), whereas w2 alone does not.
	if !vs.HasQuorum([][crypto.AddressSize]byte{w1.Address}) {
		t.Fatal("expected 128-bit HasQuorum to return true without uint64 overflow")
	}
	if vs.HasQuorum([][crypto.AddressSize]byte{w2.Address}) {
		t.Fatal("expected 128-bit HasQuorum to return false for tiny minority")
	}
}

func TestSecurity_DelegationLargeStakeNoOverflow(t *testing.T) {
	valW, _ := crypto.NewWallet()
	delW, _ := crypto.NewWallet()
	vs, err := NewValidatorSet([]*Validator{
		{Address: valW.Address, PublicKey: valW.PublicKey, VotingPower: 100},
	})
	if err != nil {
		t.Fatalf("NewValidatorSet: %v", err)
	}
	dm := NewDelegationManager(vs, 5)
	// Force CommissionBps > 10000 to verify commission clamping guard.
	dm.validatorPools[crypto.ToHex(valW.Address)].CommissionBps = 15000

	st := state.NewStateDB()
	stake := uint64(1_000_000_000_000_000_000) // 1e18 (multiplying by 1e15 overflows uint64!)
	st.SetAccount(delW.Address, &state.Account{Balance: stake + 1000})

	if err := dm.Delegate(st, delW.Address, valW.Address, stake, 1); err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	reward := uint64(1_000_000_000_000_000) // 1e15
	// Previously (stake * reward) overflowed uint64 silently!
	_ = dm.DistributeBlockReward(valW.Address, reward)

	if _, err := dm.ClaimRewards(st, delW.Address, valW.Address); err != nil {
		t.Fatalf("ClaimRewards: %v", err)
	}

	// Verify >100% penalty clamps to 100% cleanly without overflow.
	slashed := dm.SlashValidatorDelegations(valW.Address, 150)
	if slashed != stake {
		t.Fatalf("expected 100%% clamped slash=%d, got %d", stake, slashed)
	}

	// Undelegate saturates completionHeight cleanly at MaxUint64 without wraparound.
	st.SetAccount(delW.Address, &state.Account{Balance: 2_000_000})
	if err := dm.Delegate(st, delW.Address, valW.Address, 1_000_000, 1); err != nil {
		t.Fatalf("re-delegate: %v", err)
	}
	entry, err := dm.Undelegate(delW.Address, valW.Address, 1_000_000, math.MaxUint64-2)
	if err != nil {
		t.Fatalf("Undelegate: %v", err)
	}
	if entry.CompletionHeight != math.MaxUint64 {
		t.Fatalf("expected saturated MaxUint64 completion height, got %d", entry.CompletionHeight)
	}
}

func TestSecurity_ProcessProposalRejectsTamperedTxAndBaseFee(t *testing.T) {
	e1, e2, val1, _ := setupTestBFT(t)
	genesis := e1.BlockByHeight(0)
	if genesis == nil {
		t.Fatal("missing genesis block")
	}

	// 1. nil block rejected
	if err := e2.ProcessProposal(nil); err == nil {
		t.Fatal("expected nil block rejection")
	}

	// 2. Forged unsigned transaction inside block rejected
	u, _ := crypto.NewWallet()
	forgedTx := core.NewTransfer(u.Address, val1.Address, u.PublicKey, 0, 100, core.MinGasPrice)
	forgedBlk := &core.Block{
		Header: core.BlockHeader{
			Version:       1,
			Height:        1,
			PrevHash:      genesis.Hash,
			Timestamp:     time.Now().UnixNano(),
			ValidatorAddr: val1.Address,
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee,
		},
		Txs: []*core.Transaction{forgedTx},
	}
	forgedBlk.Header.MerkleRoot = core.ComputeMerkleRoot([][crypto.HashSize]byte{forgedTx.Hash})
	if err := forgedBlk.SignHeader(val1.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if err := e2.ProcessProposal(forgedBlk); err == nil {
		t.Fatal("expected ProcessProposal to reject invalid transaction signature")
	}

	// 3. Tampered BaseFee rejected
	wrongFeeBlk := &core.Block{
		Header: core.BlockHeader{
			Version:       1,
			Height:        1,
			PrevHash:      genesis.Hash,
			Timestamp:     time.Now().UnixNano(),
			ValidatorAddr: val1.Address,
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee + 999999,
		},
	}
	wrongFeeBlk.Header.MerkleRoot = core.ComputeMerkleRoot(nil)
	if err := wrongFeeBlk.SignHeader(val1.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if err := e2.ProcessProposal(wrongFeeBlk); err == nil {
		t.Fatal("expected ProcessProposal to reject incorrect BaseFee")
	}
}

func TestSecurity_SignerVoteTypeToggleEquivocationRejected(t *testing.T) {
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatal(err)
	}

	signers := []ValidatorSigner{
		NewLocalSigner(w.Address, w.PublicKey, w.PrivateKey),
		NewKMSSigner(w.Address, w.PublicKey, "kms-key-1", func(payload []byte) ([]byte, error) {
			return crypto.Sign(w.PrivateKey, payload)
		}),
	}

	hashA := crypto.Hash256([]byte("block-A"))
	hashB := crypto.Hash256([]byte("block-B"))

	for idx, s := range signers {
		prevoteA := &Vote{Type: VotePrevote, Height: 10, Round: 1, BlockHash: hashA, Voter: w.Address, PublicKey: w.PublicKey}
		if err := s.SignVote(prevoteA); err != nil {
			t.Fatalf("signer %d initial prevote: %v", idx, err)
		}

		// Signing Precommit at same height/round for hashB must NOT overwrite VotePrevote watermark!
		precommitB := &Vote{Type: VotePrecommit, Height: 10, Round: 1, BlockHash: hashB, Voter: w.Address, PublicKey: w.PublicKey}
		if err := s.SignVote(precommitB); err != nil {
			t.Fatalf("signer %d precommit after prevote: %v", idx, err)
		}

		// Equivocating Prevote with hashB at same height/round must fail!
		equivPrevoteB := &Vote{Type: VotePrevote, Height: 10, Round: 1, BlockHash: hashB, Voter: w.Address, PublicKey: w.PublicKey}
		err := s.SignVote(equivPrevoteB)
		if !errors.Is(err, ErrEquivocationPrevented) {
			t.Fatalf("signer %d allowed vote-type toggle equivocation! err=%v", idx, err)
		}
	}
}
