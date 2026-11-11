package main

import "testing"

func TestMultisigEntrypoints(t *testing.T) {
	main()
	// Invalid zero-length payload should safely return negative error code from exported wrappers.
	if code := init_multisig(0, 0); code >= 0 {
		t.Fatalf("expected negative error code from init_multisig(0,0), got %d", code)
	}
	if code := execute_transfer(0, 0); code >= 0 {
		t.Fatalf("expected negative error code from execute_transfer(0,0), got %d", code)
	}
}
