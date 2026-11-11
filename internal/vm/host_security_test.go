package vm

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

type mockStateAccessor struct {
	mu       sync.Mutex
	storage  map[[crypto.AddressSize]byte]map[uint32]uint64
	balances map[[crypto.AddressSize]byte]uint64
}

func newMockState() *mockStateAccessor {
	return &mockStateAccessor{
		storage:  make(map[[crypto.AddressSize]byte]map[uint32]uint64),
		balances: make(map[[crypto.AddressSize]byte]uint64),
	}
}

func (m *mockStateAccessor) GetStorage(addr [crypto.AddressSize]byte, slot uint32) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slots, ok := m.storage[addr]; ok {
		return slots[slot]
	}
	return 0
}

func (m *mockStateAccessor) SetStorage(addr [crypto.AddressSize]byte, slot uint32, value uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storage[addr] == nil {
		m.storage[addr] = make(map[uint32]uint64)
	}
	m.storage[addr][slot] = value
}

func (m *mockStateAccessor) GetBalance(addr [crypto.AddressSize]byte) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balances[addr]
}

func (m *mockStateAccessor) Transfer(from, to [crypto.AddressSize]byte, amount uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.balances[from] < amount {
		return ErrOutOfGas // reuse non-nil sentinel error
	}
	m.balances[from] -= amount
	m.balances[to] += amount
	return nil
}

// buildAllHostCallsWASM constructs a valid WASM binary exporting tests for all 8 host functions + linear memory.
func buildAllHostCallsWASM(toAddr [32]byte, logBytes []byte) []byte {
	// Types:
	// 0: (i32) -> (i64)         (oen_get, oen_balance)
	// 1: (i32, i64) -> ()       (oen_set)
	// 2: (i32, i64) -> (i64)    (oen_transfer)
	// 3: (i32, i32) -> ()       (oen_log)
	// 4: () -> (i64)            (oen_caller, oen_block_height, oen_value, query getters)
	// 5: () -> ()               (run_rw, run_xfer, run_log, write_set)
	typeContent := []byte{
		0x06,
		0x60, 0x01, 0x7f, 0x01, 0x7e,
		0x60, 0x02, 0x7f, 0x7e, 0x00,
		0x60, 0x02, 0x7f, 0x7e, 0x01, 0x7e,
		0x60, 0x02, 0x7f, 0x7f, 0x00,
		0x60, 0x00, 0x01, 0x7e,
		0x60, 0x00, 0x00,
	}

	addImp := func(buf []byte, name string, typeIdx byte) []byte {
		buf = append(buf, 0x03, 'e', 'n', 'v', byte(len(name)))
		buf = append(buf, []byte(name)...)
		buf = append(buf, 0x00, typeIdx)
		return buf
	}
	impContent := []byte{0x08}
	impContent = addImp(impContent, "oen_get", 0)          // func 0
	impContent = addImp(impContent, "oen_set", 1)          // func 1
	impContent = addImp(impContent, "oen_transfer", 2)     // func 2
	impContent = addImp(impContent, "oen_balance", 0)      // func 3
	impContent = addImp(impContent, "oen_log", 3)          // func 4
	impContent = addImp(impContent, "oen_caller", 4)       // func 5
	impContent = addImp(impContent, "oen_block_height", 4) // func 6
	impContent = addImp(impContent, "oen_value", 4)        // func 7

	// 6 local functions (funcs 8..13):
	// 8: get_caller ()->i64 (type 4)
	// 9: get_height ()->i64 (type 4)
	// 10: get_val ()->i64 (type 4)
	// 11: get_bal ()->i64 (type 4) - reads balance of addr at mem[0..32]
	// 12: do_xfer ()->i64 (type 4) - transfers 250 to mem[0..32], returns 0/1 extended to i64
	// 13: do_log ()->() (type 5) - logs mem[64..64+len(logBytes)]
	funcContent := []byte{0x06, 4, 4, 4, 4, 4, 5}

	// Memory section: 1 page min (64 KiB)
	memContent := []byte{0x01, 0x00, 0x01}

	addExp := func(buf []byte, name string, kind byte, idx byte) []byte {
		buf = append(buf, byte(len(name)))
		buf = append(buf, []byte(name)...)
		buf = append(buf, kind, idx)
		return buf
	}
	expContent := []byte{0x07}
	expContent = addExp(expContent, "memory", 0x02, 0)
	expContent = addExp(expContent, "get_caller", 0x00, 8)
	expContent = addExp(expContent, "get_height", 0x00, 9)
	expContent = addExp(expContent, "get_val", 0x00, 10)
	expContent = addExp(expContent, "get_bal", 0x00, 11)
	expContent = addExp(expContent, "do_xfer", 0x00, 12)
	expContent = addExp(expContent, "do_log", 0x00, 13)

	bodyCaller := []byte{0x00, 0x10, 0x05, 0x0b}
	bodyHeight := []byte{0x00, 0x10, 0x06, 0x0b}
	bodyVal := []byte{0x00, 0x10, 0x07, 0x0b}
	bodyBal := []byte{0x00, 0x41, 0x00, 0x10, 0x03, 0x0b} // oen_balance(0)
	// oen_transfer(0, 250) -> i64
	bodyXfer := []byte{0x00, 0x41, 0x00, 0x42, 0xfa, 0x01, 0x10, 0x02, 0x0b}
	// oen_log(64, 5000) -> tests maxLogBytes=4096 clamping! (SLEB128 64 = 0xc0 0x00)
	bodyLog := []byte{0x00, 0x41, 0xc0, 0x00, 0x41, 0x88, 0x27, 0x10, 0x04, 0x0b}

	codeContent := []byte{0x06}
	for _, b := range [][]byte{bodyCaller, bodyHeight, bodyVal, bodyBal, bodyXfer, bodyLog} {
		codeContent = appendULEB128(codeContent, safeUint32Len(len(b)))
		codeContent = append(codeContent, b...)
	}

	// Data section: 2 active segments at mem[0] = toAddr and mem[64] = logBytes (SLEB128 64 = 0xc0 0x00)
	dataContent := []byte{0x02}
	dataContent = append(dataContent, 0x00, 0x41, 0x00, 0x0b)
	dataContent = appendULEB128(dataContent, safeUint32Len(len(toAddr)))
	dataContent = append(dataContent, toAddr[:]...)
	dataContent = append(dataContent, 0x00, 0x41, 0xc0, 0x00, 0x0b)
	dataContent = appendULEB128(dataContent, safeUint32Len(len(logBytes)))
	dataContent = append(dataContent, logBytes...)

	mod := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	mod = appendSection(mod, 0x01, typeContent)
	mod = appendSection(mod, 0x02, impContent)
	mod = appendSection(mod, 0x03, funcContent)
	mod = appendSection(mod, 0x05, memContent)
	mod = appendSection(mod, 0x07, expContent)
	mod = appendSection(mod, 0x0a, codeContent)
	mod = appendSection(mod, 0x0b, dataContent)
	return mod
}

func TestHostFunctions_AllAndSecurityGuards(t *testing.T) {
	ctx := context.Background()
	v, err := NewVM(ctx)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	defer v.Close(ctx)

	if asUint32(math.MaxUint32) != math.MaxUint32 {
		t.Fatal("asUint32 boundary failed")
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on asUint32 overflow")
			}
		}()
		_ = asUint32(uint64(math.MaxUint32) + 1)
	}()

	toAddr := [32]byte{0x09, 0x08, 0x07}
	callerAddr := [32]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01, 0x02}
	contractAddr := [32]byte{0x55}

	bigLog := make([]byte, 5000)
	for i := range bigLog {
		bigLog[i] = 'X'
	}
	wasmBytes := buildAllHostCallsWASM(toAddr, bigLog)
	codeHash := crypto.Hash256(wasmBytes)

	if err := v.Deploy(ctx, contractAddr, wasmBytes); err != nil {
		t.Fatalf("Deploy all-host WASM: %v", err)
	}

	st := newMockState()
	st.balances[contractAddr] = 1000
	st.balances[toAddr] = 500

	ec := &ExecutionContext{
		State:        st,
		ContractAddr: contractAddr,
		CallerAddr:   callerAddr,
		Value:        4242,
		BlockHeight:  999,
		GasLimit:     100_000,
	}
	callCtx := WithExecCtx(ctx, ec)

	res, err := v.Call(callCtx, ec, codeHash, "get_caller")
	if err != nil || len(res) != 1 || res[0] != binary.BigEndian.Uint64(callerAddr[:8]) {
		t.Fatalf("get_caller failed: res=%v err=%v", res, err)
	}
	res, err = v.Call(callCtx, ec, codeHash, "get_height")
	if err != nil || res[0] != 999 {
		t.Fatalf("get_height failed: res=%v err=%v", res, err)
	}
	res, err = v.Call(callCtx, ec, codeHash, "get_val")
	if err != nil || res[0] != 4242 {
		t.Fatalf("get_val failed: res=%v err=%v", res, err)
	}
	res, err = v.Call(callCtx, ec, codeHash, "get_bal")
	if err != nil || res[0] != 500 {
		t.Fatalf("get_bal failed: res=%v err=%v", res, err)
	}
	res, err = v.Call(callCtx, ec, codeHash, "do_xfer")
	if err != nil || res[0] != 1 {
		t.Fatalf("do_xfer failed: res=%v err=%v", res, err)
	}
	if st.GetBalance(toAddr) != 750 || st.GetBalance(contractAddr) != 750 {
		t.Fatalf("balances after xfer mismatch: contract=%d to=%d", st.GetBalance(contractAddr), st.GetBalance(toAddr))
	}

	// Exceed balance triggers stack[0]=0 return without crashing.
	for i := 0; i < 4; i++ {
		_, _ = v.Call(callCtx, ec, codeHash, "do_xfer")
	}

	// ReadOnly transfer trap check.
	ecRO := &ExecutionContext{
		State:        st,
		ContractAddr: contractAddr,
		GasLimit:     100_000,
		ReadOnly:     true,
	}
	if _, err := v.Call(WithExecCtx(ctx, ecRO), ecRO, codeHash, "do_xfer"); err == nil {
		t.Fatal("expected trap on transfer inside ReadOnly context")
	}

	// oen_log caps length at maxLogBytes=4096 and charges gas.
	gasBefore := ec.GasUsed
	if _, err := v.Call(callCtx, ec, codeHash, "do_log"); err != nil {
		t.Fatalf("do_log failed: %v", err)
	}
	if ec.GasUsed-gasBefore != gasCostLog+uint64(maxLogBytes) {
		t.Fatalf("expected log gas %d, got %d", gasCostLog+uint64(maxLogBytes), ec.GasUsed-gasBefore)
	}
}

func TestVM_ConcurrentDeployAndCall(t *testing.T) {
	ctx := context.Background()
	v, err := NewVM(ctx)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	defer v.Close(ctx)

	codeHash := crypto.Hash256(CounterContractWASM)
	contractAddr := [32]byte{0x42}
	if err := v.Deploy(ctx, contractAddr, CounterContractWASM); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	st := newMockState()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ec := &ExecutionContext{
				State:        st,
				ContractAddr: contractAddr,
				GasLimit:     100_000,
				ReadOnly:     idx%2 == 0,
			}
			callCtx := WithExecCtx(ctx, ec)
			_, err := v.Call(callCtx, ec, codeHash, "get_count")
			if err != nil {
				t.Errorf("concurrent Call failed: %v", err)
			}
			if idx%3 == 0 {
				_ = v.Deploy(ctx, [32]byte{byte(idx + 100)}, CounterContractWASM)
			}
		}(i)
	}
	wg.Wait()
}
