package vm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	// MaxMemoryPages limits each WASM instance to 256 * 64 KiB = 16 MiB.
	MaxMemoryPages = 256
	// DefaultCallTimeout caps WASM contract execution to prevent infinite-loop DoS.
	DefaultCallTimeout = 5 * time.Second
)

// VM manages WASM contract compilation and execution via wazero.
type VM struct {
	mu      sync.RWMutex
	rt      wazero.Runtime
	modules map[string]wazero.CompiledModule // codeHash hex -> compiled module
	seq     atomic.Uint64                    // unique suffix for module instance names
}

// NewVM creates a new VM and registers the OENEXA host module.
func NewVM(ctx context.Context) (*VM, error) {
	// Enable CloseOnContextDone + memory limit to protect against infinite loops & OOM bombs.
	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(MaxMemoryPages)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	v := &VM{
		rt:      rt,
		modules: make(map[string]wazero.CompiledModule),
	}
	if err := v.registerHostModule(ctx); err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	return v, nil
}

// registerHostModule installs all OENEXA host functions under the "env" module.
func (v *VM) registerHostModule(ctx context.Context) error {
	_, err := v.rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(hostGet(), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_get").
		NewFunctionBuilder().
		WithGoModuleFunction(hostSet(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI64}, []api.ValueType{}).
		Export("onxc_set").
		NewFunctionBuilder().
		WithGoModuleFunction(hostTransfer(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI64}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_transfer").
		NewFunctionBuilder().
		WithGoModuleFunction(hostBalance(), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_balance").
		NewFunctionBuilder().
		WithGoModuleFunction(hostLog(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("onxc_log").
		NewFunctionBuilder().
		WithGoModuleFunction(hostCaller(), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_caller").
		NewFunctionBuilder().
		WithGoModuleFunction(hostBlockHeight(), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_block_height").
		NewFunctionBuilder().
		WithGoModuleFunction(hostValue(), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("onxc_value").
		Instantiate(ctx)
	return err
}

// Deploy compiles and caches a contract's WASM bytecode.
// contractAddr is the on-chain address; code is the raw WASM bytes.
func (v *VM) Deploy(ctx context.Context, contractAddr [crypto.AddressSize]byte, code []byte) error {
	codeHash := crypto.Hash256(code)
	key := crypto.ToHex(codeHash)

	v.mu.RLock()
	_, exists := v.modules[key]
	v.mu.RUnlock()
	if exists {
		return nil // already compiled
	}

	compiled, err := v.rt.CompileModule(ctx, code)
	if err != nil {
		return fmt.Errorf("compile WASM: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if _, exists := v.modules[key]; exists {
		_ = compiled.Close(ctx)
		return nil
	}
	v.modules[key] = compiled
	return nil
}

// Call invokes a named export function of the contract identified by codeHash.
// The ExecutionContext must be set on ctx via WithExecCtx.
// Returns the function's return values as a []uint64 slice.
func (v *VM) Call(
	ctx context.Context,
	ec *ExecutionContext,
	codeHash [crypto.HashSize]byte,
	funcName string,
	params ...uint64,
) ([]uint64, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCallTimeout)
		defer cancel()
	}

	key := crypto.ToHex(codeHash)
	v.mu.RLock()
	compiled, ok := v.modules[key]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("contract isn't deployed: %s", key)
	}

	// Instantiate a fresh module instance for each call (isolation).
	instanceName := fmt.Sprintf("contract_%s_%d", key[:8], v.seq.Add(1))
	cfg := wazero.NewModuleConfig().WithName(instanceName)
	mod, err := v.rt.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("instantiate module: %w", err)
	}
	defer func() {
		_ = mod.Close(context.Background())
	}()

	fn := mod.ExportedFunction(funcName)
	if fn == nil {
		return nil, fmt.Errorf("function %q not exported by contract", funcName)
	}

	result, err := fn.Call(ctx, params...)
	if err != nil {
		return nil, fmt.Errorf("call %q: %w", funcName, err)
	}
	return result, nil
}



// Close releases all wazero resources.
func (v *VM) Close(ctx context.Context) {
	_ = v.rt.Close(ctx)
}
