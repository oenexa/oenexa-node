package core

import (
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

func TestSecurity_TokenomicsHalvingAndYieldBounds(t *testing.T) {
	if got := HalvingCountdown(0); got != HalvingInterval {
		t.Fatalf("expected HalvingInterval at genesis, got %d", got)
	}
	if got := HalvingCountdown(HalvingInterval); got != 0 {
		t.Fatalf("expected 0 remaining at exact halving boundary, got %d", got)
	}
	if got := HalvingCountdown(10); got != HalvingInterval-10 {
		t.Fatalf("unexpected countdown at height 10: %d", got)
	}

	// Bonded ratio clamping prevents divide-by-zero or >100% inflation anomalies.
	lowYield := StakingYieldEstimate(100, 0.0)
	floorYield := StakingYieldEstimate(100, 0.05)
	if lowYield != floorYield || lowYield <= 0 {
		t.Fatalf("expected bonded ratio floor at 0.05, low=%v floor=%v", lowYield, floorYield)
	}
	capYield := StakingYieldEstimate(100, 1.5)
	exactOneYield := StakingYieldEstimate(100, 1.0)
	if capYield != exactOneYield || capYield <= 0 {
		t.Fatalf("expected bonded ratio cap at 1.0, cap=%v one=%v", capYield, exactOneYield)
	}
}

func TestSecurity_TransactionSigningPayloadAndValidation(t *testing.T) {
	tx := &Transaction{
		Version:  1,
		Type:     TxTransfer,
		GasLimit: 21000,
		GasPrice: MinGasPrice,
	}
	payload := tx.SigningPayload()
	if len(payload) == 0 {
		t.Fatal("SigningPayload must not be empty")
	}
	// Missing signature must fail BasicValidate
	if err := tx.BasicValidate(); err == nil {
		t.Fatal("expected BasicValidate failure when signature missing")
	}
}

func BenchmarkTransactionComputeHash(b *testing.B) {
	tx := NewTransfer([crypto.AddressSize]byte{1}, [crypto.AddressSize]byte{2}, make([]byte, crypto.PublicKeySize), 1, OneONXC, MinGasPrice)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tx.ComputeHash()
	}
}

