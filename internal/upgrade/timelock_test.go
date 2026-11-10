package upgrade_test

import (
	"errors"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/upgrade"
)

func TestScheduler_TimelockEnforcement(t *testing.T) {
	wallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}

	scheduler := upgrade.NewScheduler(1, nil)
	// Enforce 100 blocks mandatory activation delay
	scheduler.SetMinActivationDelay(100)

	// Simulate current chain height = 50
	scheduler.OnBlock(50)

	v, _ := upgrade.ParseVersion("2.0.0")

	// 1. Violation: TargetHeight 120 < 50 + 100 (150)
	pPremature := &upgrade.Proposal{
		TargetVersion: v,
		TargetHeight:  120,
		Proposer:      wallet.Address,
		PublicKey:     wallet.PublicKey,
	}
	if err := pPremature.Sign(wallet.PrivateKey); err != nil {
		t.Fatalf("sign premature: %v", err)
	}

	ok, err := scheduler.AddProposal(pPremature)
	if err == nil {
		t.Fatalf("expected timelock violation error, got ok=%v", ok)
	}
	if !errors.Is(err, upgrade.ErrTimelockViolation) {
		t.Fatalf("expected ErrTimelockViolation, got %v", err)
	}
	if scheduler.Scheduled() != nil {
		t.Fatalf("premature proposal should not be scheduled")
	}

	// 2. Compliance: TargetHeight 200 >= 50 + 100
	vSafe, _ := upgrade.ParseVersion("2.1.0")
	pCompliant := &upgrade.Proposal{
		TargetVersion: vSafe,
		TargetHeight:  200,
		Proposer:      wallet.Address,
		PublicKey:     wallet.PublicKey,
	}
	if err := pCompliant.Sign(wallet.PrivateKey); err != nil {
		t.Fatalf("sign compliant: %v", err)
	}

	ok, err = scheduler.AddProposal(pCompliant)
	if err != nil || !ok {
		t.Fatalf("expected compliant proposal to achieve quorum: ok=%v, err=%v", ok, err)
	}
	if scheduler.Scheduled() == nil || scheduler.Scheduled().TargetHeight != 200 {
		t.Fatalf("compliant proposal should be scheduled")
	}
}

func TestScheduler_EmergencySecurityVeto(t *testing.T) {
	wallet, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}

	scheduler := upgrade.NewScheduler(1, nil)
	v, _ := upgrade.ParseVersion("3.0.0")

	p := &upgrade.Proposal{
		TargetVersion: v,
		TargetHeight:  500,
		Proposer:      wallet.Address,
		PublicKey:     wallet.PublicKey,
	}
	if err := p.Sign(wallet.PrivateKey); err != nil {
		t.Fatalf("sign: %v", err)
	}

	ok, err := scheduler.AddProposal(p)
	if err != nil || !ok {
		t.Fatalf("proposal add failed: %v", err)
	}
	if scheduler.Scheduled() == nil {
		t.Fatalf("proposal was not scheduled")
	}

	// Emergency Security Veto by Security Council
	if err := scheduler.Veto("v3.0.0", "Critical vulnerability discovered in EVM bytecode patch"); err != nil {
		t.Fatalf("veto error: %v", err)
	}

	// Verify scheduled proposal was wiped
	if scheduler.Scheduled() != nil {
		t.Fatalf("scheduled proposal should be nil after emergency veto")
	}

	// Attempting to propose the vetoed version again must be rejected!
	ok, err = scheduler.AddProposal(p)
	if err == nil {
		t.Fatalf("expected vetoed proposal rejection")
	}
	if !errors.Is(err, upgrade.ErrProposalVetoed) {
		t.Fatalf("expected ErrProposalVetoed, got %v", err)
	}
}
