package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
	"time"

	"github.com/oenexa/oenexa/internal/crypto"
)

// GenesisConfig holds the parameters used to build the genesis block.
type GenesisConfig struct {
	ChainID       int
	Timestamp     int64
	ValidatorAddr [crypto.AddressSize]byte
	Allocations   map[[crypto.AddressSize]byte]uint64
}

// DefaultGenesisConfig returns a sensible genesis config for a network.
// When multiple genesis validators are supplied, GenesisPremine (10M ONXC) is
// split among them so aggregate genesis supply never exceeds GenesisPremine.
func DefaultGenesisConfig(validators [][crypto.AddressSize]byte) *GenesisConfig {
	alloc := make(map[[crypto.AddressSize]byte]uint64)
	if len(validators) == 1 {
		alloc[validators[0]] = GenesisPremine
	} else if len(validators) > 1 {
		share := GenesisPremine / uint64(len(validators))
		rem := GenesisPremine % uint64(len(validators))
		for i, v := range validators {
			amt := share
			if i == 0 {
				amt += rem
			}
			alloc[v] += amt
		}
	}
	// The first validator acts as the nominal proposer of the genesis block
	proposer := [crypto.AddressSize]byte{}
	if len(validators) > 0 {
		proposer = validators[0]
	}
	return &GenesisConfig{
		ChainID:       ChainID,
		Timestamp:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
		ValidatorAddr: proposer,
		Allocations:   alloc,
	}
}

// TotalAllocation returns the sum of all genesis allocations with overflow protection.
func (cfg *GenesisConfig) TotalAllocation() (uint64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("nil genesis config")
	}
	var total uint64
	for _, bal := range cfg.Allocations {
		sum, carry := bits.Add64(total, bal, 0)
		if carry != 0 {
			return 0, fmt.Errorf("genesis allocation sum overflows uint64")
		}
		total = sum
	}
	return total, nil
}

// Validate checks that genesis allocations fit within MaxSupply and non-zero parameters hold.
func (cfg *GenesisConfig) Validate() error {
	total, err := cfg.TotalAllocation()
	if err != nil {
		return err
	}
	if total > MaxSupply {
		return fmt.Errorf("genesis allocation %d exceeds MaxSupply %d", total, MaxSupply)
	}
	if cfg.Timestamp <= 0 {
		return fmt.Errorf("genesis timestamp must be positive")
	}
	return nil
}

// Build creates and returns the genesis block (height 0, no transactions).
func (cfg *GenesisConfig) Build() *Block {
	var prevHash [crypto.HashSize]byte
	stateRoot := genesisStateRoot(cfg.Allocations)

	hdr := BlockHeader{
		Version:       ProtocolVersion,
		Height:        0,
		PrevHash:      prevHash,
		MerkleRoot:    EmptyMerkleRoot(),
		StateRoot:     stateRoot,
		Timestamp:     cfg.Timestamp,
		ValidatorAddr: cfg.ValidatorAddr,
		GasUsed:       0,
		GasLimit:      BlockGasLimit,
		BaseFee:       InitialBaseFee, // seed the EIP-1559 fee market
		BurnedFees:    0,
	}
	blk := &Block{Header: hdr, Txs: nil}
	blk.Hash = blk.ComputeHash()
	return blk
}

// genesisStateRoot produces a deterministic hash from the allocation map.
func genesisStateRoot(allocs map[[crypto.AddressSize]byte]uint64) [crypto.HashSize]byte {
	keys := make([][crypto.AddressSize]byte, 0, len(allocs))
	for k := range allocs {
		keys = append(keys, k)
	}
	// sort.Slice with bytes.Compare for deterministic ordering.
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i][:], keys[j][:]) < 0
	})

	h := make([]byte, 0, len(allocs)*(crypto.AddressSize+8))
	b8 := make([]byte, 8)
	for _, k := range keys {
		h = append(h, k[:]...)
		binary.BigEndian.PutUint64(b8, allocs[k])
		h = append(h, b8...)
	}
	return crypto.Hash256(h)
}
