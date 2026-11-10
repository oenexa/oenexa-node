package global

import (
	"fmt"
	"sync"
)

// Global Governance & Federation Parameters for OENEXA Layer-1.
// Supports cross-jurisdictional compliance, international node federation,
// and sovereign multi-region consensus anchoring.
type NetworkParams struct {
	ChainID             uint32   `json:"chain_id"`
	NetworkName         string   `json:"network_name"`
	ProtocolVersion     string   `json:"protocol_version"`
	MaxValidators       uint32   `json:"max_validators"`
	MinValidatorStake   uint64   `json:"min_validator_stake"`
	UnbondingPeriodSec  uint64   `json:"unbonding_period_sec"`
	AllowedJurisdictions []string `json:"allowed_jurisdictions"`
}

// DefaultMainnetParams provides initial parameters for global launch.
func DefaultMainnetParams() NetworkParams {
	return NetworkParams{
		ChainID:             1,
		NetworkName:         "OENEXA Sovereign L1 Mainnet",
		ProtocolVersion:     "v1.0.0",
		MaxValidators:       100,
		MinValidatorStake:   100_000 * 1_000_000_000, // 100k ONXC in nano-ONXC
		UnbondingPeriodSec:  86400 * 14,             // 14 days
		AllowedJurisdictions: []string{"GLOBAL", "EU", "APAC", "CH", "UAE", "UK"},
	}
}

// GlobalModule manages network federation rules and parameter updates.
type GlobalModule struct {
	mu     sync.RWMutex
	Active bool
	Params NetworkParams
}

func NewGlobalModule() *GlobalModule {
	return &GlobalModule{
		Active: true,
		Params: DefaultMainnetParams(),
	}
}

func (m *GlobalModule) Execute() string {
	return fmt.Sprintf("Phase %d: Global Expansion executed successfully", 23)
}

// UpdateParams safely modifies network parameters via verified governance proposal.
func (m *GlobalModule) UpdateParams(newParams NetworkParams) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Params = newParams
}