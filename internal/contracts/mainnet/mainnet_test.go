package mainnet

import "testing"

func TestMainnetExecution(t *testing.T) {
	module := NewMainnetModule()
	result := module.Execute()
	expected := "Phase 24: Mainnet Launch executed successfully"

	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
	t.Log("Passed: End-to-end cycle completed for Phase 24")
}

func TestMainnetHardforks(t *testing.T) {
	module := NewMainnetModule()
	if !module.Active || module.Status != StatusActive {
		t.Fatalf("unexpected initial mainnet status: %+v", module)
	}
	if module.IsHardforkActive("UnknownFork", 10_000_000) {
		t.Fatal("unknown hardfork should never be active")
	}
	if module.IsHardforkActive("KyberFIPS203Upgrade", 499_999) {
		t.Fatal("hardfork should not be active before target height")
	}
	if !module.IsHardforkActive("KyberFIPS203Upgrade", 500_000) {
		t.Fatal("hardfork must activate at exact target height")
	}
	if !module.IsHardforkActive("DecentralizedDAO", 2_500_000) {
		t.Fatal("hardfork must remain active above target height")
	}
}
