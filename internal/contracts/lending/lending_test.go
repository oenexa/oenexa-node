package lending

import "testing"

func TestNewOenexaLend(t *testing.T) {
	lend := NewOenexaLend()
	if lend.oraclePrice != 100 {
		t.Fatalf("expected 100, got %d", lend.oraclePrice)
	}
}

func TestUpdateOraclePrice(t *testing.T) {
	lend := NewOenexaLend()
	lend.UpdateOraclePrice(200)
	if lend.oraclePrice != 200 {
		t.Fatalf("expected 200, got %d", lend.oraclePrice)
	}
}

func TestDepositCollateral(t *testing.T) {
	lend := NewOenexaLend()
	lend.DepositCollateral("alice", 100)
	
	if lend.Positions["alice"].CollateralOEN != 100 {
		t.Fatalf("expected 100, got %d", lend.Positions["alice"].CollateralOEN)
	}
}

func TestBorrowQUSD(t *testing.T) {
	lend := NewOenexaLend()
	lend.DepositCollateral("alice", 100)
	// Collateral = 100, price = 100. Value = 10000
	// Max debt = 10000 / 1.5 = 6666.66
	
	if ok := lend.BorrowQUSD("alice", 5000); !ok {
		t.Fatalf("expected to borrow 5000 QUSD")
	}
	
	if ok := lend.BorrowQUSD("alice", 2000); ok {
		t.Fatalf("expected borrow to fail")
	}
	
	if lend.Positions["alice"].DebtQUSD != 5000 {
		t.Fatalf("expected debt to be 5000, got %d", lend.Positions["alice"].DebtQUSD)
	}
	
	if ok := lend.BorrowQUSD("bob", 100); ok {
		t.Fatalf("expected borrow to fail for non-existent user")
	}
}

func TestLiquidate(t *testing.T) {
	lend := NewOenexaLend()
	lend.DepositCollateral("alice", 100)
	lend.BorrowQUSD("alice", 5000)
	
	if ok := lend.Liquidate("alice"); ok {
		t.Fatalf("expected liquidate to fail on healthy position")
	}
	
	// Drop oracle price to 50
	// Value = 100 * 50 = 5000
	// Debt = 5000. Ratio = 100%. Should be liquidated!
	lend.UpdateOraclePrice(50)
	
	if ok := lend.Liquidate("alice"); !ok {
		t.Fatalf("expected liquidate to succeed")
	}
	
	if lend.Positions["alice"].CollateralOEN != 0 || lend.Positions["alice"].DebtQUSD != 0 {
		t.Fatalf("expected position to be cleared")
	}
	
	// Test edge cases
	if ok := lend.Liquidate("alice"); ok {
		t.Fatalf("expected liquidate to fail on empty position")
	}
	
	if ok := lend.Liquidate("bob"); ok {
		t.Fatalf("expected liquidate to fail on unknown user")
	}
}
