package dex

// OenexaSwap implements a simple x*y=k Automated Market Maker (AMM) logic.
// It acts as the native Decentralized Exchange (DEX) smart contract.
type OenexaSwap struct {
	ReserveA uint64
	ReserveB uint64
}

// NewOenexaSwap initializes a new empty DEX pool.
func NewOenexaSwap() *OenexaSwap {
	return &OenexaSwap{
		ReserveA: 0,
		ReserveB: 0,
	}
}

// AddLiquidity deposits amountA and amountB into the pool.
// In a full implementation, this would mint LP tokens to the provider.
func (p *OenexaSwap) AddLiquidity(amountA, amountB uint64) {
	p.ReserveA += amountA
	p.ReserveB += amountB
}

// SwapAforB swaps token A for token B using the constant product formula
// x * y = k, with a 0.3% fee applied to the input amount.
func (p *OenexaSwap) SwapAforB(amountA uint64) (out uint64, ok bool) {
	if amountA == 0 {
		return 0, false
	}
	if p.ReserveA == 0 || p.ReserveB == 0 {
		return 0, false
	}

	// 0.3% fee means 99.7% of the input is used for the swap.
	// amountInWithFee = amountIn * 997
	amountInWithFee := amountA * 997
	
	// amountOut = (amountInWithFee * reserveOut) / ((reserveIn * 1000) + amountInWithFee)
	numerator := amountInWithFee * p.ReserveB
	denominator := (p.ReserveA * 1000) + amountInWithFee

	amountOut := numerator / denominator

	// If the output is zero due to integer division/small amounts, fail.
	if amountOut == 0 {
		return 0, false
	}

	// Output cannot exceed reserve.
	if amountOut >= p.ReserveB {
		// Can't drain the pool completely
		return 0, false
	}

	// Update reserves
	p.ReserveA += amountA
	p.ReserveB -= amountOut

	return amountOut, true
}
