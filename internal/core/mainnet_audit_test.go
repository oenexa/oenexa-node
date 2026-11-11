package core

import (
	"math"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

func TestNextBaseFee_OverflowSafe(t *testing.T) {
	// Near uint64 max: must saturate cleanly at math.MaxUint64 instead of wrapping to tiny values.
	maxNear := uint64(math.MaxUint64 - 10)
	got := NextBaseFee(maxNear, BlockGasLimit)
	if got != math.MaxUint64 {
		t.Fatalf("expected saturation at math.MaxUint64, got %d", got)
	}

	// Half of uint64 max at full block: must increase by <= 12.5% without wrapping around.
	halfMax := uint64(math.MaxUint64 / 2)
	gotHalf := NextBaseFee(halfMax, BlockGasLimit)
	if gotHalf <= halfMax {
		t.Fatalf("expected base fee increase without wraparound: before=%d after=%d", halfMax, gotHalf)
	}

	// Empty block at very high base fee must decrease without underflow.
	gotDec := NextBaseFee(math.MaxUint64, 0)
	if gotDec == math.MaxUint64 || gotDec < MinBaseFee {
		t.Fatalf("expected valid decrease from MaxUint64, got %d", gotDec)
	}

	// Exact target keeps base fee constant.
	if same := NextBaseFee(12345, TargetBlockGas()); same != 12345 {
		t.Fatalf("expected unchanged base fee at target, got %d", same)
	}
}

func TestTransferCostNanoONXC_OverflowSafe(t *testing.T) {
	if cost := TransferCostNanoONXC(math.MaxUint64, 1); cost != math.MaxUint64 {
		t.Fatalf("expected math.MaxUint64 on addition overflow, got %d", cost)
	}
	if cost := TransferCostNanoONXC(math.MaxUint64/2, 0); cost != math.MaxUint64 {
		t.Fatalf("expected math.MaxUint64 on multiplication overflow, got %d", cost)
	}
	if cost := TransferCostNanoONXC(InitialBaseFee, 5); cost != GasTransfer*(InitialBaseFee+5) {
		t.Fatalf("unexpected normal cost: got %d", cost)
	}
}

func TestDefaultGenesisConfig_SupplyConservationAcrossMultiValidators(t *testing.T) {
	for _, count := range []int{1, 3, 4, 7, 13} {
		vals := make([][crypto.AddressSize]byte, count)
		for i := range vals {
			w, err := crypto.NewWallet()
			if err != nil {
				t.Fatalf("NewWallet: %v", err)
			}
			vals[i] = w.Address
		}
		cfg := DefaultGenesisConfig(vals)
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate failed for %d validators: %v", count, err)
		}
		total, err := cfg.TotalAllocation()
		if err != nil {
			t.Fatalf("TotalAllocation: %v", err)
		}
		if total != GenesisPremine {
			t.Fatalf("count=%d: want exact GenesisPremine (%d), got %d", count, GenesisPremine, total)
		}
	}
}

func TestGenesisConfig_ValidateErrors(t *testing.T) {
	var nilCfg *GenesisConfig
	if err := nilCfg.Validate(); err == nil {
		t.Fatal("expected error for nil config")
	}

	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()

	overflowCfg := &GenesisConfig{
		Timestamp: 100,
		Allocations: map[[crypto.AddressSize]byte]uint64{
			w1.Address: math.MaxUint64,
			w2.Address: 1,
		},
	}
	if err := overflowCfg.Validate(); err == nil {
		t.Fatal("expected overflow error")
	}

	exceedsCap := &GenesisConfig{
		Timestamp: 100,
		Allocations: map[[crypto.AddressSize]byte]uint64{
			w1.Address: MaxSupply + 1,
		},
	}
	if err := exceedsCap.Validate(); err == nil {
		t.Fatal("expected MaxSupply exceeded error")
	}

	badTS := &GenesisConfig{
		Timestamp:   0,
		Allocations: map[[crypto.AddressSize]byte]uint64{w1.Address: 1},
	}
	if err := badTS.Validate(); err == nil {
		t.Fatal("expected invalid timestamp error")
	}
}

func TestCirculatingSupply_HardCapInvariant(t *testing.T) {
	heights := []uint64{0, 1, 100, HalvingInterval, HalvingInterval + 1, 10 * HalvingInterval, 32 * HalvingInterval, 100 * HalvingInterval}
	var prev uint64
	for _, h := range heights {
		sup := CirculatingSupply(h)
		if sup > MaxSupply {
			t.Fatalf("height %d: CirculatingSupply %d exceeds MaxSupply %d", h, sup, MaxSupply)
		}
		if sup < prev {
			t.Fatalf("height %d: CirculatingSupply %d decreased below previous %d", h, sup, prev)
		}
		prev = sup
	}
}

func FuzzNextBaseFeeOverflowBounds(f *testing.F) {
	f.Add(uint64(10), uint64(0))
	f.Add(uint64(math.MaxUint64), uint64(BlockGasLimit))
	f.Add(uint64(InitialBaseFee), uint64(BlockGasLimit))
	f.Fuzz(func(t *testing.T, current, gasUsed uint64) {
		if gasUsed > BlockGasLimit {
			gasUsed = gasUsed % (BlockGasLimit + 1)
		}
		if current < MinBaseFee {
			current = MinBaseFee
		}
		next := NextBaseFee(current, gasUsed)
		if next < MinBaseFee {
			t.Fatalf("NextBaseFee(%d, %d) = %d < MinBaseFee", current, gasUsed, next)
		}
	})
}
