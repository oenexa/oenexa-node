package core

import "math"

// Emission schedule constants.
//
// Total max supply: 100M OEN
//   - 10M OEN pre-mined at genesis (10%)
//       * 5M OEN: Ecosystem & Developer Grant Treasury (50% of premine)
//       * 3M OEN: Institutional Liquidity & Exchange Reserves (30% of premine)
//       * 2M OEN: Validator Bootstrap Staking Pool (20% of premine)
//   - 90M OEN emitted as block rewards via halving schedule
//
// Reward halves every HalvingInterval blocks.
// Sum of the infinite geometric series:
//
//	InitialBlockReward * HalvingInterval * 2 = 45 OEN * 1,000,000 * 2 = 90M OEN ✓
const (
	InitialBlockReward uint64 = 45 * OneOEN         // 45 OEN at genesis era
	HalvingInterval    uint64 = 1_000_000           // halve every 1M blocks (~23 days at 2s/block)
	GenesisPremine     uint64 = 10_000_000 * OneOEN // 10M OEN to bootstrap the validator and ecosystem

	// Allocations of the 10M Genesis Premine:
	TreasuryAllocation uint64 = 5_000_000 * OneOEN // 5M OEN Ecosystem & DAO Treasury
	LiquidityReserve   uint64 = 3_000_000 * OneOEN // 3M OEN Exchange Liquidity Reserve
	StakingBootstrap   uint64 = 2_000_000 * OneOEN // 2M OEN Validator Staking Bootstrap
)

// BlockReward returns the block subsidy (in oenexa) at the given block height.
//
// Height 0 (genesis) always returns 0 — genesis has no coinbase.
// After 32 halvings, the reward rounds down to 0 and stays there.
//
// Era 0: heights 1 – 1,000,000 → 45 OEN
// Era 1: heights 1,000,001 – 2,000,000 → 22.5 OEN (integer: 22_500_000_000)
// Era 2: heights 2,000,001 – 3,000,000 → 11.25 OEN …
func BlockReward(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	era := (height - 1) / HalvingInterval
	if era >= 32 { // after 32 halvings reward is negligible / zero
		return 0
	}
	return InitialBlockReward >> era
}

// TotalEmissionAt returns the cumulative block reward emission up to (and
// including) the given height — useful for supply tracking / dashboards.
func TotalEmissionAt(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	var total uint64
	for era := uint64(0); era < 32; era++ {
		start := era*HalvingInterval + 1
		end := start + HalvingInterval - 1
		if start > height {
			break
		}
		if end > height {
			end = height
		}
		reward := InitialBlockReward >> era
		total += reward * (end - start + 1)
	}
	return total
}

// CirculatingSupply returns the total OEN in existence at the given height:
// genesis pre-mine + block reward emission so far.
func CirculatingSupply(height uint64) uint64 {
	return GenesisPremine + TotalEmissionAt(height)
}

// HalvingCountdown returns the number of blocks remaining until the next halving era.
func HalvingCountdown(height uint64) uint64 {
	if height == 0 {
		return HalvingInterval
	}
	currentEra := (height - 1) / HalvingInterval
	nextHalvingHeight := (currentEra + 1) * HalvingInterval
	if nextHalvingHeight > height {
		return nextHalvingHeight - height
	}
	return 0
}

// StakingYieldEstimate computes the annualized staking yield (APY percentage)
// given the total bonded ratio (e.g. 0.60 for 60% of circulating supply staked).
// Target staking participation is 67% (2/3 BFT security threshold).
func StakingYieldEstimate(height uint64, bondedRatio float64) float64 {
	if bondedRatio <= 0.05 {
		bondedRatio = 0.05 // Floor at 5% to avoid divide-by-zero
	}
	if bondedRatio > 1.0 {
		bondedRatio = 1.0
	}

	currentReward := BlockReward(height)
	blocksPerYear := float64(365 * 24 * 3600 / 2) // 2-second blocks = 15,768,000 blocks/year
	annualIssuance := float64(currentReward) * blocksPerYear
	circSupply := float64(CirculatingSupply(height))

	inflationRate := annualIssuance / circSupply
	apy := (inflationRate / bondedRatio) * 100.0

	// Round to two decimal places
	return math.Round(apy*100) / 100
}