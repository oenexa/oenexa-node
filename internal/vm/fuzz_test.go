package vm

import (
	"context"
	"testing"
	
	"github.com/oenexa/oenexa/internal/crypto"
)

// FuzzExecute tests the VM against random bytecode payloads to ensure
// the Wazero runtime sandbox successfully traps panics and prevents crashes.
func FuzzExecute(f *testing.F) {
	// Seed with a valid WASM binary (an empty module)
	validWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	f.Add(validWasm)
	
	// Seed with some truncated/invalid byte arrays
	f.Add([]byte{0x00, 0x61, 0x73, 0x6d})
	f.Add([]byte{0xff, 0xff, 0xff})
	
	f.Fuzz(func(t *testing.T, bytecode []byte) {
		vm, err := NewVM(context.Background())
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}
		
		ec := &ExecutionContext{
			
			GasLimit:  1_000_000,
		}
		
		addr := [crypto.AddressSize]byte{1}
		// Attempt to deploy random bytes.
		ctx := context.Background(); _ = vm.Deploy(ctx, addr, bytecode)
		
		// If deploy somehow succeeds, try to call it
		codeHash := crypto.Hash256(bytecode)
		_, _ = vm.Call(ctx, ec, codeHash, "call", )
	})
}
