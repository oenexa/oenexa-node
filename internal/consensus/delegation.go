package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
)

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
