package vm_test

import (
	"context"
	"testing"
	"time"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/state"
	"github.com/oenexa/oenexa/internal/vm"
)

var infiniteLoopWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x11, 0x01, 0x0d, 0x69, 0x6e, 0x66, 0x69, 0x6e, 0x69, 0x74, 0x65, 0x5f, 0x6c, 0x6f, 0x6f, 0x70, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b,
}

func TestVMSandbox_InfiniteLoop(t *testing.T) {
	st := state.NewStateDB()
	ctx := context.Background()
	execVM, err := vm.NewVM(ctx)
	if err != nil {
		t.Fatalf("failed to create VM: %v", err)
	}
	defer execVM.Close(ctx)

	contractAddr := crypto.Hash256([]byte("malicious_contract"))
	callerAddr := crypto.Hash256([]byte("caller"))

	st.SetAccount(contractAddr, &state.Account{Balance: 0})
	if err := execVM.Deploy(ctx, contractAddr, infiniteLoopWASM); err != nil {
		t.Fatalf("failed to deploy infinite loop contract: %v", err)
	}
	codeHash := crypto.Hash256(infiniteLoopWASM)

	ec := &vm.ExecutionContext{
		State:        st,
		ContractAddr: contractAddr,
		CallerAddr:   callerAddr,
		GasLimit:     50000,
	}

	// We apply a strict timeout. If the VM is vulnerable to infinite loops, 
	// it won't respect the timeout (unless configured properly), or it won't 
	// deduct gas per instruction.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	execCtx := vm.WithExecCtx(ctx, ec)

	t.Log("Calling infinite_loop...")
	_, err = execVM.Call(execCtx, ec, codeHash, "infinite_loop")
	
	if err == nil {
		t.Fatal("Expected out of gas or timeout error, got nil")
	}
	t.Logf("Contract correctly terminated with: %v", err)
}
