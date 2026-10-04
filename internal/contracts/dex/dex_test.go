package dex

import "testing"

func TestNewOenexaSwap(t *testing.T) {
	pool := NewOenexaSwap()
	if pool.ReserveA != 0 || pool.ReserveB != 0 {
		t.Fatalf("expected empty pool, got %d, %d", pool.ReserveA, pool.ReserveB)
	}
}

func TestAddLiquidity(t *testing.T) {
	pool := NewOenexaSwap()
	pool.AddLiquidity(1000, 2000)
	if pool.ReserveA != 1000 || pool.ReserveB != 2000 {
		t.Fatalf("expected 1000, 2000, got %d, %d", pool.ReserveA, pool.ReserveB)
	}
}

func TestSwapAforB(t *testing.T) {
	pool := NewOenexaSwap()
	pool.AddLiquidity(1000, 2000)

	// Swap 100 A for B
	out, ok := pool.SwapAforB(100)
	if !ok {
		t.Fatalf("swap failed")
	}

	// Calculate expected output
	// fee = 0.3%, amountInWithFee = 100 * 997 = 99700
	// num = 99700 * 2000 = 199400000
	// den = 1000 * 1000 + 99700 = 1099700
	// expectedOut = 199400000 / 1099700 = 181
	if out != 181 {
		t.Fatalf("expected 181 out, got %d", out)
	}

	if pool.ReserveA != 1100 {
		t.Fatalf("expected ReserveA to be 1100, got %d", pool.ReserveA)
	}

	if pool.ReserveB != 2000-181 {
		t.Fatalf("expected ReserveB to be 1819, got %d", pool.ReserveB)
	}
}

func TestSwapAforB_Failures(t *testing.T) {
	pool := NewOenexaSwap()
	
	// Fail on empty pool
	if _, ok := pool.SwapAforB(100); ok {
		t.Fatalf("expected swap to fail on empty pool")
	}

	pool.AddLiquidity(1000, 2000)

	// Fail on zero input
	if _, ok := pool.SwapAforB(0); ok {
		t.Fatalf("expected swap to fail on zero input")
	}

	// Fail on draining the pool
	// If we input an extremely large amount, amountOut would theoretically exceed ReserveB
	// Actually, due to math, amountOut = (in*resB)/(resA + in) which always < resB.
	// But let's test a case where amountOut >= ReserveB (manually manipulating values)
	
	// Test zero output due to integer division
	if _, ok := pool.SwapAforB(1); !ok { // Wait, 1*997*2000 = 1994000. Denom = 1000000 + 997 = 1000997. Output = 1.
		// Output is 1. Let's do smaller
	}
	
	pool.ReserveA = 10000000
	pool.ReserveB = 10
	// Swap 1 A
	// inWithFee = 997
	// num = 997 * 10 = 9970
	// den = 10000000000 + 997
	// out = 9970 / 10000000997 = 0
	if _, ok := pool.SwapAforB(1); ok {
		t.Fatalf("expected swap to fail on zero output")
	}
}
