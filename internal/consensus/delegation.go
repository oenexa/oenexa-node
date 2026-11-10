package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

// ─────────────────────────────────────────────────────────────────────────────
// Delegation & Pooled Staking Architecture
//
// PURPOSE:
//   1. Enables decentralized Proof-of-Stake participation by allowing any sovereign
//      OEN holder to delegate stake to registered network validators.
//   2. Orchestrates proportional reward distribution, validator commission accounting,
//      timelocked unbonding queues, and Byzantine fault slashing propagation.
//   3. Maximizes total Byzantine fault tolerance capital security by aggregating
//      distributed token weight into the active validator consensus set.
//
// WHY:
//   - Requiring full validator hardware and infrastructure excludes standard users,
//     leading to concentrated validator cartels. Pooled delegation democratizes
//     staking rewards and decentralizes voting power.
//   - Unbonding delay prevents "long-range" attacks and rapid capital withdrawal
//     following malicious double-signing attempts.
//   - Proportional slashing propagation ensures delegators conduct due diligence
//     on validator operational reliability, creating market incentives for high uptime.
//
// HOW:
//   - Delegation: Token holders bond OEN (minimum MinDelegationAmount = 0.001 OEN).
//     The staked balance is locked from general transfer and added to the validator's pool.
//   - Reward Accrual: When a validator proposes or votes on a block, gross rewards
//     are collected. The validator's configured commission (e.g. 5% / 500 bps) is
//     allocated to the validator operator, and the net reward is distributed pro-rata
//     to delegators based on their share of the total pool.
//   - Unbonding Queue: Undelegation moves funds into an unbonding queue requiring
//     DefaultUnbondingDelayBlocks before maturation and release to spendable balance.
//   - Slashing: When a validator is slashed for downtime or double-signing, the
//     slashing penalty is applied uniformly across the validator's self-stake and all delegators.
//
// FORMAL MATHEMATICAL PROOFS:
//
//   1. Stake Conservation Invariant (Theorem 1):
//      Let S_v be the total delegated stake for validator v, and s_{d, v} be the stake
//      delegated by delegator d to validator v.
//      Invariant:
//        S_v = \sum_{d \in D_v} s_{d, v}
//      Proof by Structural Induction:
//        - Base Case: Initially, S_v = 0 and D_v = \emptyset. Equation holds (0 = 0).
//        - Delegation Step: When delegator d adds \Delta s > 0, S_v' = S_v + \Delta s
//          and s'_{d, v} = s_{d, v} + \Delta s.
//          S_v' = (\sum_{k \neq d} s_{k, v}) + (s_{d, v} + \Delta s) = \sum s'_{k, v}.
//        - Undelegation Step: When delegator d withdraws \Delta s > 0, S_v' = S_v - \Delta s
//          and s'_{d, v} = s_{d, v} - \Delta s.
//          S_v' = (\sum_{k \neq d} s_{k, v}) + (s_{d, v} - \Delta s) = \sum s'_{k, v}.
//      Therefore, stake conservation holds strictly across all state transitions.
//      Q.E.D.
//
//   2. Reward Solvency & Non-Inflationary Distribution (Theorem 2):
//      Let R_total be the gross block reward allocated to pool v.
//      Let c = CommissionBps / 10,000 (0 <= c <= 1).
//      The validator commission is:
//        R_val = floor( R_total * CommissionBps / 10000 )
//      The net pool reward is:
//        R_net = R_total - R_val
//      For each delegator d with share s_d in pool S_v:
//        R_d = floor( R_net * s_d / S_v )
//      Theorem:
//        R_val + \sum_{d \in D_v} R_d <= R_total
//      Proof:
//        \sum_{d \in D_v} R_d = \sum_{d \in D_v} floor( R_net * s_d / S_v )
//                             <= \sum_{d \in D_v} ( R_net * s_d / S_v )
//                              = R_net * ( \sum s_d ) / S_v = R_net * (S_v / S_v) = R_net.
//        Therefore:
//        R_val + \sum R_d <= R_val + R_net = R_val + (R_total - R_val) = R_total.
//      Any integer truncation remainder (dust) remains unminted or in the reserve,
//      strictly guaranteeing that reward distribution can never cause unexpected inflation.
//      Q.E.D.
//
//   3. Slashing Proportionality Theorem (Theorem 3):
//      Let slash fraction be \alpha \in (0, 1]. For all d \in D_v:
//        s'_{d, v} = s_{d, v} - floor( s_{d, v} * \alpha )
//      The relative ownership share s'_{d, v} / S'_v is preserved within integer rounding bounds:
//        s'_{d, v} / S'_v \approx (s_{d, v} * (1 - \alpha)) / (S_v * (1 - \alpha)) = s_{d, v} / S_v
//      Preventing any advantage or disadvantage among delegators during a slashing event.
//      Q.E.D.
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultUnbondingDelayBlocks uint64 = 100 // blocks until unbonding funds mature
	DefaultCommissionBps        uint64 = 500 // 5% validator commission (500 basis points)
	MinDelegationAmount         uint64 = 1_000_000 // 0.001 OEN
)

var (
	ErrInsufficientDelegation = errors.New("insufficient delegated stake to undelegate")
	ErrValidatorNotActive     = errors.New("validator is not in active set")
	ErrBelowMinDelegation     = errors.New("delegation amount below minimum threshold")
	ErrInsufficientBalance    = errors.New("delegator has insufficient account balance")
)

// Delegation records an active stake bond from a token holder to a validator.
type Delegation struct {
	Delegator       [crypto.AddressSize]byte `json:"delegator"`
	Validator       [crypto.AddressSize]byte `json:"validator"`
	Amount          uint64                   `json:"amount"`
	InitialHeight   uint64                   `json:"initial_height"`
	PendingRewards  uint64                   `json:"pending_rewards"`
}

// UnbondingEntry records funds currently in the cooldown unbonding period.
type UnbondingEntry struct {
	Delegator        [crypto.AddressSize]byte `json:"delegator"`
	Validator        [crypto.AddressSize]byte `json:"validator"`
	Amount           uint64                   `json:"amount"`
	CreationHeight   uint64                   `json:"creation_height"`
	CompletionHeight uint64                   `json:"completion_height"`
}

// ValidatorPool tracks aggregated staking state and delegator shares for a validator.
type ValidatorPool struct {
	ValidatorAddr    [crypto.AddressSize]byte `json:"validator_addr"`
	TotalDelegated   uint64                   `json:"total_delegated"`
	CommissionBps    uint64                   `json:"commission_bps"`
	TotalRewardsPaid uint64                   `json:"total_rewards_paid"`
}

// DelegationManager orchestrates on-chain pooled delegation, reward distribution,
// unbonding queues, and slashing propagation.
type DelegationManager struct {
	mu                  sync.RWMutex
	unbondingDelay      uint64
	validatorSet        *ValidatorSet
	delegations         map[string]map[string]*Delegation // delegatorHex -> validatorHex -> Delegation
	validatorPools      map[string]*ValidatorPool         // validatorHex -> ValidatorPool
	unbondingQueue      []*UnbondingEntry
}

// NewDelegationManager creates a new staking delegation manager.
func NewDelegationManager(vs *ValidatorSet, unbondingDelay uint64) *DelegationManager {
	if unbondingDelay == 0 {
		unbondingDelay = DefaultUnbondingDelayBlocks
	}
	dm := &DelegationManager{
		unbondingDelay: unbondingDelay,
		validatorSet:   vs,
		delegations:    make(map[string]map[string]*Delegation),
		validatorPools: make(map[string]*ValidatorPool),
		unbondingQueue: make([]*UnbondingEntry, 0),
	}

	// Initialize pools for validators in the active set
	if vs != nil {
		for _, val := range vs.All() {
			valHex := crypto.ToHex(val.Address)
			dm.validatorPools[valHex] = &ValidatorPool{
				ValidatorAddr: val.Address,
				CommissionBps: DefaultCommissionBps,
			}
		}
	}
	return dm
}

// Delegate locks funds from delegator state account and allocates stake to the validator.
func (dm *DelegationManager) Delegate(
	st *state.DB,
	delegator [crypto.AddressSize]byte,
	valAddr [crypto.AddressSize]byte,
	amount uint64,
	currentHeight uint64,
) error {
	if amount < MinDelegationAmount {
		return fmt.Errorf("%w: min is %d", ErrBelowMinDelegation, MinDelegationAmount)
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.validatorSet != nil && !dm.validatorSet.Contains(valAddr) {
		return ErrValidatorNotActive
	}

	// Verify and debit delegator account balance
	acc := st.GetAccount(delegator)
	if acc == nil || acc.Balance < amount {
		return ErrInsufficientBalance
	}
	acc.Balance -= amount
	st.SetAccount(delegator, acc)

	delHex := crypto.ToHex(delegator)
	valHex := crypto.ToHex(valAddr)

	if dm.delegations[delHex] == nil {
		dm.delegations[delHex] = make(map[string]*Delegation)
	}

	existing, exists := dm.delegations[delHex][valHex]
	if exists {
		existing.Amount += amount
	} else {
		dm.delegations[delHex][valHex] = &Delegation{
			Delegator:     delegator,
			Validator:     valAddr,
			Amount:        amount,
			InitialHeight: currentHeight,
		}
	}

	pool, exists := dm.validatorPools[valHex]
	if !exists {
		pool = &ValidatorPool{
			ValidatorAddr: valAddr,
			CommissionBps: DefaultCommissionBps,
		}
		dm.validatorPools[valHex] = pool
	}
	pool.TotalDelegated += amount

	return nil
}

// Undelegate unbonds stake from a validator and initiates the unbonding queue period.
func (dm *DelegationManager) Undelegate(
	delegator [crypto.AddressSize]byte,
	valAddr [crypto.AddressSize]byte,
	amount uint64,
	currentHeight uint64,
) (*UnbondingEntry, error) {
	if amount == 0 {
		return nil, errors.New("undelegate amount must be > 0")
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	delHex := crypto.ToHex(delegator)
	valHex := crypto.ToHex(valAddr)

	userDels := dm.delegations[delHex]
	if userDels == nil || userDels[valHex] == nil || userDels[valHex].Amount < amount {
		return nil, ErrInsufficientDelegation
	}

	del := userDels[valHex]
	del.Amount -= amount
	if del.Amount == 0 {
		delete(userDels, valHex)
	}

	if pool, exists := dm.validatorPools[valHex]; exists {
		if pool.TotalDelegated >= amount {
			pool.TotalDelegated -= amount
		} else {
			pool.TotalDelegated = 0
		}
	}

	entry := &UnbondingEntry{
		Delegator:        delegator,
		Validator:        valAddr,
		Amount:           amount,
		CreationHeight:   currentHeight,
		CompletionHeight: currentHeight + dm.unbondingDelay,
	}
	dm.unbondingQueue = append(dm.unbondingQueue, entry)
	return entry, nil
}

// ProcessMaturedUnbondings credits unbonded funds back to delegators once completion height is reached.
func (dm *DelegationManager) ProcessMaturedUnbondings(st *state.DB, currentHeight uint64) ([]*UnbondingEntry, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var matured []*UnbondingEntry
	var remaining []*UnbondingEntry

	for _, entry := range dm.unbondingQueue {
		if currentHeight >= entry.CompletionHeight {
			acc := st.GetAccount(entry.Delegator)
			if acc == nil {
				acc = &state.Account{}
			}
			acc.Balance += entry.Amount
			st.SetAccount(entry.Delegator, acc)
			matured = append(matured, entry)
		} else {
			remaining = append(remaining, entry)
		}
	}

	dm.unbondingQueue = remaining
	return matured, nil
}

// DistributeBlockReward splits block rewards between the validator operator (commission)
// and active delegators.
func (dm *DelegationManager) DistributeBlockReward(valAddr [crypto.AddressSize]byte, totalReward uint64) uint64 {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	valHex := crypto.ToHex(valAddr)
	pool, exists := dm.validatorPools[valHex]
	if !exists || pool.TotalDelegated == 0 {
		return totalReward // all goes to validator operator
	}

	// Commission e.g. 5%
	commission := (totalReward * pool.CommissionBps) / 10000
	delegatorsPool := totalReward - commission

	// Distribute to all delegators proportionally to their stake
	for _, userDels := range dm.delegations {
		if d, ok := userDels[valHex]; ok && d.Amount > 0 {
			share := (delegatorsPool * d.Amount) / pool.TotalDelegated
			d.PendingRewards += share
		}
	}

	pool.TotalRewardsPaid += delegatorsPool
	return commission
}

// ClaimRewards transfers accumulated staking rewards into the delegator's account.
func (dm *DelegationManager) ClaimRewards(st *state.DB, delegator [crypto.AddressSize]byte, valAddr [crypto.AddressSize]byte) (uint64, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	delHex := crypto.ToHex(delegator)
	valHex := crypto.ToHex(valAddr)

	userDels := dm.delegations[delHex]
	if userDels == nil || userDels[valHex] == nil {
		return 0, errors.New("no active delegation found")
	}

	del := userDels[valHex]
	reward := del.PendingRewards
	if reward == 0 {
		return 0, nil
	}

	acc := st.GetAccount(delegator)
	if acc == nil {
		acc = &state.Account{}
	}
	acc.Balance += reward
	st.SetAccount(delegator, acc)

	del.PendingRewards = 0
	return reward, nil
}

// SlashValidatorDelegations cuts delegated stakes by penaltyPercent (e.g. 10%) on equivocation.
func (dm *DelegationManager) SlashValidatorDelegations(valAddr [crypto.AddressSize]byte, penaltyPercent uint64) uint64 {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	valHex := crypto.ToHex(valAddr)
	pool, exists := dm.validatorPools[valHex]
	if !exists || pool.TotalDelegated == 0 {
		return 0
	}

	var totalSlashed uint64
	for _, userDels := range dm.delegations {
		if d, ok := userDels[valHex]; ok && d.Amount > 0 {
			penalty := (d.Amount * penaltyPercent) / 100
			d.Amount -= penalty
			totalSlashed += penalty
		}
	}

	if pool.TotalDelegated >= totalSlashed {
		pool.TotalDelegated -= totalSlashed
	} else {
		pool.TotalDelegated = 0
	}
	return totalSlashed
}

// GetDelegations returns all active delegations for an address.
func (dm *DelegationManager) GetDelegations(delegator [crypto.AddressSize]byte) []*Delegation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	delHex := crypto.ToHex(delegator)
	userDels := dm.delegations[delHex]
	if userDels == nil {
		return nil
	}

	res := make([]*Delegation, 0, len(userDels))
	for _, d := range userDels {
		res = append(res, &Delegation{
			Delegator:      d.Delegator,
			Validator:      d.Validator,
			Amount:         d.Amount,
			InitialHeight:  d.InitialHeight,
			PendingRewards: d.PendingRewards,
		})
	}
	return res
}

// TotalDelegatedStake returns aggregate bonded tokens across all validators.
func (dm *DelegationManager) TotalDelegatedStake() uint64 {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var total uint64
	for _, pool := range dm.validatorPools {
		total += pool.TotalDelegated
	}
	return total
}
