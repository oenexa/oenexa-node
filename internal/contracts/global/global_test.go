package global

import "testing"

func TestGlobalExecution(t *testing.T) {
	module := NewGlobalModule()
	result := module.Execute()
	expected := "Phase 23: Global Expansion executed successfully"

	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}
	t.Log("Passed: End-to-end cycle completed for Phase 23")
}

func TestGlobalUpdateParamsAndConcurrency(t *testing.T) {
	module := NewGlobalModule()
	if !module.Active || module.Params.ChainID != 1 {
		t.Fatalf("unexpected default global module state: %+v", module)
	}

	updated := DefaultMainnetParams()
	updated.ChainID = 42
	updated.MaxValidators = 150
	updated.ProtocolVersion = "v1.1.0"
	updated.AllowedJurisdictions = []string{"GLOBAL", "CH"}

	module.UpdateParams(updated)
	if module.Params.ChainID != 42 || module.Params.MaxValidators != 150 {
		t.Fatalf("UpdateParams did not update params: %+v", module.Params)
	}
}
