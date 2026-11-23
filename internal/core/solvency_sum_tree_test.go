package core_test

import (
	"math/big"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
)

func TestSolvencySumTree_BuildAndAggregateSum(t *testing.T) {
	leaves := []core.ClientLiabilityLeaf{
		{AccountID: "usr_alice", Balance: big.NewInt(1000)},
		{AccountID: "usr_bob", Balance: big.NewInt(2500)},
		{AccountID: "usr_charlie", Balance: big.NewInt(750)},
		{AccountID: "usr_david", Balance: big.NewInt(1750)},
	}

	tree, err := core.NewSolvencySumTree(leaves)
	if err != nil {
		t.Fatalf("unexpected error building solvency sum tree: %v", err)
	}

	expectedSum := big.NewInt(6000)
	if tree.Root.Sum.Cmp(expectedSum) != 0 {
		t.Errorf("expected root sum %v, got %v", expectedSum, tree.Root.Sum)
	}
}

func TestSolvencySumTree_NegativeBalanceStrictlyProhibited(t *testing.T) {
	leaves := []core.ClientLiabilityLeaf{
		{AccountID: "usr_alice", Balance: big.NewInt(1000)},
		{AccountID: "usr_fraudulent_debt", Balance: big.NewInt(-500)}, // Hidden exchange liability debt
	}

	_, err := core.NewSolvencySumTree(leaves)
	if err == nil {
		t.Fatal("expected ErrNegativeBalanceProhibited, got nil")
	}
}

func TestSolvencySumTree_ProofGenerationAndVerification(t *testing.T) {
	leaves := []core.ClientLiabilityLeaf{
		{AccountID: "usr_alice", Balance: big.NewInt(1000)},
		{AccountID: "usr_bob", Balance: big.NewInt(2500)},
		{AccountID: "usr_charlie", Balance: big.NewInt(750)},
	}

	tree, err := core.NewSolvencySumTree(leaves)
	if err != nil {
		t.Fatalf("unexpected error building tree: %v", err)
	}

	// Generate proof for Alice
	proof, err := tree.GenerateProof("usr_alice")
	if err != nil {
		t.Fatalf("failed to generate proof for usr_alice: %v", err)
	}

	valid, err := core.VerifySolvencyProof(tree.Root, proof)
	if err != nil || !valid {
		t.Fatalf("expected valid proof verification, got valid=%v, err=%v", valid, err)
	}

	// Generate proof for Bob
	proofBob, err := tree.GenerateProof("usr_bob")
	if err != nil {
		t.Fatalf("failed to generate proof for usr_bob: %v", err)
	}
	validBob, err := core.VerifySolvencyProof(tree.Root, proofBob)
	if err != nil || !validBob {
		t.Fatalf("expected valid proof for Bob, got valid=%v, err=%v", validBob, err)
	}
}

func TestSolvencySumTree_TamperedProofRejected(t *testing.T) {
	leaves := []core.ClientLiabilityLeaf{
		{AccountID: "usr_alice", Balance: big.NewInt(1000)},
		{AccountID: "usr_bob", Balance: big.NewInt(2500)},
	}

	tree, err := core.NewSolvencySumTree(leaves)
	if err != nil {
		t.Fatalf("unexpected error building tree: %v", err)
	}

	proof, err := tree.GenerateProof("usr_alice")
	if err != nil {
		t.Fatalf("failed to generate proof: %v", err)
	}

	// Tamper with balance
	tamperedProof := *proof
	tamperedProof.Balance = big.NewInt(999999)

	valid, err := core.VerifySolvencyProof(tree.Root, &tamperedProof)
	if valid || err == nil {
		t.Errorf("expected tampered proof to fail validation, got valid=%v, err=%v", valid, err)
	}
}

func TestSolvencySumTree_EmptyLeaves(t *testing.T) {
	tree, err := core.NewSolvencySumTree(nil)
	if err != nil {
		t.Fatalf("unexpected error for empty leaves: %v", err)
	}
	if tree.Root.Sum.Sign() != 0 {
		t.Errorf("expected 0 sum for empty tree, got %v", tree.Root.Sum)
	}
}
