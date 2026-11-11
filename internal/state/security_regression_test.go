package state

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mpt"
	"github.com/oenexa/oenexa/internal/vm"
)

func deriveTestContractAddr(from [crypto.AddressSize]byte, nonce uint64) [crypto.AddressSize]byte {
	var nonceBytes [8]byte
	binary.BigEndian.PutUint64(nonceBytes[:], nonce)
	return crypto.HashMany(from[:], nonceBytes[:])
}

func TestSecurity_RevertedTxCallRollsBackValueAndStorage(t *testing.T) {
	ctx := context.Background()
	execVM, err := vm.NewVM(ctx)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	defer execVM.Close(ctx)

	st := NewStateDB()
	sender := [crypto.AddressSize]byte{0x11}

	startBal := uint64(100_000_000)
	st.SetAccount(sender, &Account{Balance: startBal})

	// Deploy CounterContractWASM first.
	deployTx := makeTx(core.TxDeploy, sender, [crypto.AddressSize]byte{}, 0, 0, core.GasDeploy+50_000, core.MinGasPrice, vm.CounterContractWASM)
	dRes, err := ApplyTransaction(st, deployTx, core.BlockGasLimit, execVM, core.MinBaseFee)
	if err != nil || !dRes.Success {
		t.Fatalf("deploy failed: res=%v err=%v", dRes, err)
	}
	contractAddr := deriveTestContractAddr(sender, 0)

	// Call nonexistent function WITH Value=1_000_000 so the VM call fails and must roll back value!
	balBeforeCall := st.GetBalance(sender)
	contractBalBefore := st.GetBalance(contractAddr)
	sendVal := uint64(1_000_000)
	// Wire data format: [nameLen=11][nonexistent]
	badFunc := append([]byte{byte(len("nonexistent"))}, []byte("nonexistent")...)
	callTx := makeTx(core.TxCall, sender, contractAddr, 1, sendVal, core.GasCall+10_000, core.MinGasPrice, badFunc)

	cRes, err := ApplyTransaction(st, callTx, core.BlockGasLimit, execVM, core.MinBaseFee)
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if cRes.Success {
		t.Fatal("expected reverted execution receipt for unknown exported function")
	}

	// Contract must NOT keep sent ONXC value on revert.
	if st.GetBalance(contractAddr) != contractBalBefore {
		t.Fatalf("contract kept reverted value: before=%d after=%d", contractBalBefore, st.GetBalance(contractAddr))
	}
	// Sender nonce must advance, and sender pays only gas used (not sendVal).
	if st.GetNonce(sender) != 2 {
		t.Fatalf("expected sender nonce 2, got %d", st.GetNonce(sender))
	}
	expectedCost := cRes.GasUsed * callTx.GasPrice
	if st.GetBalance(sender) != balBeforeCall-expectedCost {
		t.Fatalf("sender balance mismatch after reverted call: want=%d got=%d", balBeforeCall-expectedCost, st.GetBalance(sender))
	}
}

type memBackend struct {
	data map[string][]byte
}

func newMemBackend() *memBackend {
	return &memBackend{data: make(map[string][]byte)}
}

func (m *memBackend) Get(key []byte) ([]byte, error) {
	v, ok := m.data[string(key)]
	if !ok {
		return nil, nil
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memBackend) Put(key, val []byte) error {
	cp := make([]byte, len(val))
	copy(cp, val)
	m.data[string(key)] = cp
	return nil
}

func (m *memBackend) NewBatch() mpt.Batch {
	return &memBatch{b: m}
}

type memBatch struct {
	b    *memBackend
	ops  []struct{ k, v []byte }
}

func (mb *memBatch) Put(key, val []byte) {
	mb.ops = append(mb.ops, struct{ k, v []byte }{append([]byte(nil), key...), append([]byte(nil), val...)})
}

func (mb *memBatch) Write() error {
	for _, op := range mb.ops {
		mb.b.data[string(op.k)] = op.v
	}
	return nil
}

func TestSecurity_DBCreditHydratesTrie(t *testing.T) {
	backend := newMemBackend()
	trie := mpt.New(backend)
	st1 := NewStateDBWithTrie(trie)

	alice := makeAddr(0xA1)
	st1.SetAccount(alice, &Account{Balance: 5000, Nonce: 7})
	root := st1.CommitRoot()

	// Reopen trie from committed root cleanly with empty dirty map.
	trie2 := mpt.NewWithRoot(backend, root)
	st2 := NewStateDBWithTrie(trie2)

	// Credit without calling GetAccount first must hydrate committed balance 5000.
	st2.Credit(alice, 250)
	acc := st2.GetAccount(alice)
	if acc.Balance != 5250 {
		t.Fatalf("Credit overwrote committed balance: want=5250 got=%d", acc.Balance)
	}
	if acc.Nonce != 7 {
		t.Fatalf("Credit clobbered committed nonce: want=7 got=%d", acc.Nonce)
	}

	// Saturation guard prevents uint64 wraparound.
	st2.Credit(alice, math.MaxUint64-100)
	if st2.GetBalance(alice) != math.MaxUint64 {
		t.Fatalf("expected saturated MaxUint64 balance, got %d", st2.GetBalance(alice))
	}
}

func TestSecurity_TransferSelfNoInfiniteMint(t *testing.T) {
	st := stateWithBalance(addrA, 12345)
	if err := st.Transfer(addrA, addrA, 12345); err != nil {
		t.Fatalf("self-transfer failed: %v", err)
	}
	if got := st.GetBalance(addrA); got != 12345 {
		t.Fatalf("self-transfer mutated balance (infinite mint bug!): want=12345 got=%d", got)
	}

	// Also verify TxTransfer with from == to via ApplyTransaction preserves total supply minus gas.
	tx := makeTx(core.TxTransfer, addrA, addrA, 0, 5000, core.GasTransfer, core.MinGasPrice, nil)
	st.SetAccount(addrA, &Account{Balance: 1_000_000})
	res, err := ApplyTransaction(st, tx, core.BlockGasLimit, nil, core.MinBaseFee)
	if err != nil || !res.Success {
		t.Fatalf("ApplyTransaction self-transfer failed: res=%v err=%v", res, err)
	}
	expected := uint64(1_000_000) - core.GasTransfer*core.MinGasPrice
	if st.GetBalance(addrA) != expected {
		t.Fatalf("ApplyTransaction self-transfer balance mismatch: want=%d got=%d", expected, st.GetBalance(addrA))
	}

	// Overflow guard on recipient balance.
	st.SetAccount(addrA, &Account{Balance: 100})
	st.SetAccount(addrB, &Account{Balance: math.MaxUint64 - 10})
	if err := st.Transfer(addrA, addrB, 50); err == nil {
		t.Fatal("expected recipient balance overflow error")
	}
}

func TestSecurity_ApplyTransactionOverflowPrechecks(t *testing.T) {
	st := stateWithBalance(addrA, 1_000_000)

	// 1. maxCost overflow (GasLimit * GasPrice > MaxUint64)
	tx1 := makeTx(core.TxTransfer, addrA, addrB, 0, 1, math.MaxUint64/2, 4, nil)
	if _, err := ApplyTransaction(st, tx1, math.MaxUint64, nil, core.MinBaseFee); err == nil {
		t.Fatal("expected integer overflow in fee calculation")
	}

	// 2. requiredBalance overflow (Amount + maxCost > MaxUint64) leaves account untouched
	tx2 := makeTx(core.TxTransfer, addrA, addrB, 0, math.MaxUint64-10, core.GasTransfer, core.MinGasPrice, nil)
	if _, err := ApplyTransaction(st, tx2, core.BlockGasLimit, nil, core.MinBaseFee); err == nil {
		t.Fatal("expected integer overflow in required balance")
	}
	if st.GetBalance(addrA) != 1_000_000 || st.GetNonce(addrA) != 0 {
		t.Fatal("failed overflow precheck should not mutate sender state")
	}

	// 3. TxUnshield recipient balance overflow leaves shielded pool and account untouched
	st.CreditShieldedPool(1000)
	st.SetAccount(addrB, &Account{Balance: math.MaxUint64 - 10})
	txUnshield := makeTx(core.TxUnshield, addrA, addrB, 0, 500, core.GasUnshield+1000, core.MinGasPrice, make([]byte, 32))
	uRes, uErr := ApplyTransaction(st, txUnshield, core.BlockGasLimit, nil, core.MinBaseFee)
	if uErr != nil || uRes.Success {
		t.Fatalf("expected TxUnshield overflow revert, got err=%v success=%v", uErr, uRes.Success)
	}
	if st.GetShieldedBalance() != 1000 {
		t.Fatalf("shielded pool mutated after overflow revert: want=1000 got=%d", st.GetShieldedBalance())
	}
}

func TestSecurity_DeployCollisionPrevented(t *testing.T) {
	ctx := context.Background()
	execVM, err := vm.NewVM(ctx)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	defer execVM.Close(ctx)

	st := stateWithBalance(addrA, 1_000_000*core.OneONXC)
	contractAddr := deriveTestContractAddr(addrA, 0)
	// Pre-populate contractAddr with an existing CodeHash
	st.SetAccount(contractAddr, &Account{Balance: 0, CodeHash: crypto.Hash256([]byte("existing"))})

	deployTx := makeTx(core.TxDeploy, addrA, [crypto.AddressSize]byte{}, 0, 0, core.GasDeploy+50_000, core.MinGasPrice, vm.CounterContractWASM)
	res, err := ApplyTransaction(st, deployTx, core.BlockGasLimit, execVM, core.MinBaseFee)
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if res.Success {
		t.Fatal("expected contract collision re-deploy to revert")
	}
}

func TestSecurity_ApplyBlockParallel_MixedRevertsAndTransfers(t *testing.T) {
	ctx := context.Background()
	execVM, err := vm.NewVM(ctx)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	defer execVM.Close(ctx)

	w1, _ := crypto.NewWallet()
	w2, _ := crypto.NewWallet()
	w3, _ := crypto.NewWallet()
	valAddr := [crypto.AddressSize]byte{0xEE}

	initState := func() *DB {
		s := NewStateDB()
		s.SetAccount(w1.Address, &Account{Balance: 100 * core.OneONXC})
		s.SetAccount(w2.Address, &Account{Balance: 100 * core.OneONXC})
		s.SetAccount(w3.Address, &Account{Balance: 100 * core.OneONXC})
		return s
	}

	tx1 := makeTx(core.TxTransfer, w1.Address, addrB, 0, core.OneONXC, core.GasTransfer, core.MinGasPrice, nil)
	tx2 := makeTx(core.TxTransfer, w2.Address, addrA, 0, 2*core.OneONXC, core.GasTransfer, core.MinGasPrice, nil)
	txBadDeploy := makeTx(core.TxDeploy, w3.Address, [crypto.AddressSize]byte{}, 0, 5*core.OneONXC, core.GasDeploy+5_000, core.MinGasPrice, []byte("not-wasm"))
	tx3 := makeTx(core.TxTransfer, w1.Address, w3.Address, 1, core.OneONXC, core.GasTransfer, core.MinGasPrice, nil)

	blk := &core.Block{
		Header: core.BlockHeader{
			Height:        1,
			ValidatorAddr: valAddr,
			GasLimit:      core.BlockGasLimit,
			BaseFee:       core.MinBaseFee,
		},
		Txs: []*core.Transaction{tx1, tx2, txBadDeploy, tx3},
	}

	stSeq := initState()
	seqRes, err := ApplyBlock(stSeq, blk, valAddr, execVM)
	if err != nil {
		t.Fatalf("ApplyBlock sequential: %v", err)
	}
	seqRoot := stSeq.CommitRoot()

	stPar := initState()
	parRes, err := ApplyBlockParallel(stPar, blk, valAddr, execVM)
	if err != nil {
		t.Fatalf("ApplyBlockParallel: %v", err)
	}
	parRoot := stPar.CommitRoot()

	if seqRoot != parRoot {
		t.Fatalf("parallel vs sequential StateRoot mismatch: seq=%x par=%x", seqRoot, parRoot)
	}
	if seqRes.GasUsed != parRes.GasUsed || seqRes.BurnedFees != parRes.BurnedFees {
		t.Fatalf("parallel vs sequential gas/fees mismatch: seq=(%d,%d) par=(%d,%d)",
			seqRes.GasUsed, seqRes.BurnedFees, parRes.GasUsed, parRes.BurnedFees)
	}
	if _, err := ApplyBlockParallel(stPar, nil, valAddr, execVM); err == nil {
		t.Fatal("expected error for nil block")
	}
}
