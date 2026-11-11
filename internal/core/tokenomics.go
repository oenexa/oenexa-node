package core

import "math"

// ─────────────────────────────────────────────────────────────────────────────
// OENEXA LAYER-1 TOKENOMICS SPECIFICATION
// ─────────────────────────────────────────────────────────────────────────────
//
// PURPOSE:
//   Governs the sovereign emission, disinflation schedule, circulating supply,
//   and annualized staking yield dynamics for the native ONXC cryptocurrency
//   (Open Network for the Next Generation Coin).
//
// WHY (ARCHITECTURAL RATIONALE):
//   1. Hard Supply Cap: An absolute ceiling of 100,000,000 ONXC guarantees that
//      the currency is strictly deflationary over time, eliminating arbitrary
//      monetary debasement.
//   2. Geometric Halving Decay: Predictable step-down emission rewards early network
//      adopters and bootstrap validators while steadily tapering issuance to zero.
//   3. Synergistic Base Fee Burn: In combination with EIP-1559 base fee burning,
//      the net issuance becomes negative (deflationary) as transaction volume scales.
//   4. Institutional Premine Allocation: The 10% genesis allocation (10M ONXC) is
//      transparently allocated between DAO Treasury (5M), Exchange Liquidity (3M),
//      and Validator Staking Bootstrap (2M), with zero VC overhang or private warrants.
//
// HOW (MECHANICAL EXECUTION):
//   1. At height 0 (genesis), no block reward is generated; only the genesis premine exists.
//   2. For blocks [1, HalvingInterval], the coinbase reward is InitialBlockReward (45 ONXC).
//   3. At every multiple of HalvingInterval (1,000,000 blocks), the era increments and the
//      block reward bit-shifts right by 1 (reward >> era).
//   4. After 32 halving eras (era >= 32), the reward becomes zero.
//
// PROOF OF MATHEMATICAL CONVERGENCE:
//   Let R_0 = InitialBlockReward = 45 * 10^9 nano-ONXC.
//   Let H   = HalvingInterval    = 1,000,000 blocks.
//   The total block reward emission across all eras is the sum of geometric series:
//     S_emission = \sum_{k=0}^{\infty} (R_0 / 2^k) * H
//                = R_0 * H * \sum_{k=0}^{\infty} (1/2)^k
//                = R_0 * H * (1 / (1 - 1/2))
//                = R_0 * H * 2
//                = 45 ONXC * 1,000,000 * 2 = 90,000,000 ONXC.
//
//   Adding the Genesis Premine (P = 10,000,000 ONXC):
//     S_total = P + S_emission
//             = 10,000,000 ONXC + 90,000,000 ONXC
//             = 100,000,000 ONXC (Exact Hard Cap).
//
//   Invariant: \forall h \ge 0, CirculatingSupply(h) <= MaxSupply.
// ─────────────────────────────────────────────────────────────────────────────

const (
	InitialBlockReward uint64 = 45 * OneONXC         // 45 ONXC at genesis era
	HalvingInterval    uint64 = 1_000_000            // Halve every 1,000,000 blocks (~23 days at 2s/block)
	GenesisPremine     uint64 = 10_000_000 * OneONXC // 10M ONXC to bootstrap validators and sovereign ecosystem

	// Allocations of the 10M Genesis Premine:
	TreasuryAllocation uint64 = 5_000_000 * OneONXC // 5M ONXC Ecosystem & DAO Treasury (50%)
	LiquidityReserve   uint64 = 3_000_000 * OneONXC // 3M ONXC Institutional Liquidity & Reserves (30%)
	StakingBootstrap   uint64 = 2_000_000 * OneONXC // 2M ONXC Validator Staking Bootstrap (20%)
)

// BlockReward returns the block subsidy (in atomic nano-ONXC units) at the specified height.
//
// PURPOSE:
//   Determines the exact coinbase subsidy granted to the proposing validator of block `height`.
//
// WHY:
//   Provides deterministic, consensus-verifiable minting rules without central authority.
//
// HOW:
//   Computes era = (height - 1) / HalvingInterval.
//   Bitshifts InitialBlockReward >> era.
//   Guards height 0 (genesis has zero subsidy).
//
// PROOF OF BOUNDEDNESS:
//   For era >= 32, (InitialBlockReward >> era) == 0.
//   No integer overflow can occur because era is strictly checked and >> is monotonically non-increasing.
func BlockReward(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	era := (height - 1) / HalvingInterval
	if era >= 32 {
		return 0
	}
	return InitialBlockReward >> era
}

// TotalEmissionAt returns the cumulative block rewards emitted up to and including `height`.
//
// PURPOSE:
//   Calculates total algorithmic issuance for real-time supply audits and dashboard analytics.
//
// HOW:
//   Iterates across halving eras from era 0 to min(current_era, 31), accumulating the product
//   of reward per era and blocks elapsed in that era.
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

// CirculatingSupply returns the aggregate ONXC in existence at `height`.
//
// PROOF OF SUPPLY CONSERVATION:
//   CirculatingSupply(h) = GenesisPremine + TotalEmissionAt(h).
//   Because TotalEmissionAt(h) <= 90,000,000 ONXC and GenesisPremine = 10,000,000 ONXC,
//   CirculatingSupply(h) <= 100,000,000 ONXC holds for all h >= 0.
func CirculatingSupply(height uint64) uint64 {
	return GenesisPremine + TotalEmissionAt(height)
}

// HalvingCountdown returns the remaining blocks until the next halving era boundary.
//
// PURPOSE:
//   Provides real-time countdown metrics for exchanges, explorers, and node telemetry.
//
// HOW:
//   Finds next boundary at (currentEra + 1) * HalvingInterval and computes distance.
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
// given the total bonded ratio of circulating supply.
//
// PURPOSE:
//   Exposes real-time economic yields for validators and delegators.
//
// WHY:
//   Aligns validator incentive with the BFT 2/3 (67%) security quorum threshold.
//
// FORMAL PROOF OF YIELD EQUILIBRIUM:
//   Let B = bondedRatio \in [0.05, 1.0].
//   Let S_circ = CirculatingSupply(height).
//   Let I_annual = BlockReward(height) * BlocksPerYear.
//   Network Inflation Rate: \Pi = I_annual / S_circ.
//   Staker APY: APY = \Pi / B.
//   As staking participation decreases towards 5%, APY automatically escalates,
//   incentivizing holders to bond tokens and restore network security.
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

	return math.Round(apy*100) / 100
}
