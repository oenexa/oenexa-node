package state

import (
	"errors"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/vm"
)

// ApplyBlockParallel executes transactions concurrently if they do not touch
// overlapping accounts. This significantly increases transaction throughput (TPS)
// on multi-core hardware, moving OENEXA towards the 100,000+ TPS target.
//
// Execution runs against an atomic snapshot so failed blocks never leave partial
// state mutations behind. BurnedFees are removed from supply (deflationary).
func ApplyBlockParallel(
	st *DB,
	blk *core.Block,
	validatorAddr [crypto.AddressSize]byte,
	execVM *vm.VM,
) (*BlockResult, error) {
	if st == nil || blk == nil {
		return nil, errors.New("state or block is nil")
	}

	snap := st.Snapshot()
	baseFee := blk.Header.BaseFee
	var gasMu sync.Mutex
	remainingGas := core.BlockGasLimit

	result := &BlockResult{
		TxResults: make([]*TxResult, len(blk.Txs)),
	}

	// ── Phase 1: Static Dependency Analysis (O(N)) ──────────────────────────
	// Group transactions into sequential layers of strictly disjoint sender/recipient pairs.
	var layers [][]int
	lastLayerByAddr := make(map[[crypto.AddressSize]byte]int, len(blk.Txs)*2)
	minLayer := 0

	for i, tx := range blk.Txs {
		if tx == nil {
			return nil, fmt.Errorf("nil transaction at index %d", i)
		}
		// Contract deploys/calls and shielded pool txs can mutate global or arbitrary state
		// (or trigger snapshot rollback on revert), so isolate them in exclusive layers.
		isStatefulBarrier := tx.Type == core.TxDeploy || tx.Type == core.TxCall ||
			tx.Type == core.TxShield || tx.Type == core.TxUnshield || tx.Type == core.TxShieldedTransfer
		if isStatefulBarrier {
			barrierIdx := len(layers)
			layers = append(layers, []int{i})
			minLayer = barrierIdx + 1
			continue
		}

		target := minLayer
		if l, ok := lastLayerByAddr[tx.From]; ok && l+1 > target {
			target = l + 1
		}
		if l, ok := lastLayerByAddr[tx.To]; ok && l+1 > target {
			target = l + 1
		}
		for len(layers) <= target {
			layers = append(layers, nil)
		}
		layers[target] = append(layers[target], i)
		lastLayerByAddr[tx.From] = target
		lastLayerByAddr[tx.To] = target
	}

	// ── Phase 2: Parallel Execution (Bounded Worker Pool) ───────────────────
	var gasUsed, feeCollected, burnedFees, validatorTip uint64
	var globalErr atomic.Value
	maxWorkers := runtime.GOMAXPROCS(0)
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	for _, layer := range layers {
		if len(layer) == 0 {
			continue
		}
		if len(layer) == 1 {
			tx := blk.Txs[layer[0]]
			if tx.GasLimit > remainingGas {
				return nil, fmt.Errorf("block gas limit exceeded")
			}
			txRes, err := ApplyTransaction(snap, tx, tx.GasLimit, execVM, baseFee)
			if err != nil {
				return nil, fmt.Errorf("tx %s failed: %w", crypto.ToHex(tx.Hash), err)
			}
			remainingGas -= txRes.GasUsed
			gasUsed += txRes.GasUsed
			feeCollected += txRes.FeeCollected
			burnedFees += txRes.BurnedFee
			validatorTip += txRes.ValidatorTip
			result.TxResults[layer[0]] = txRes
			continue
		}

		workers := maxWorkers
		if workers > len(layer) {
			workers = len(layer)
		}
		var cursor atomic.Int64
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					pos := int(cursor.Add(1) - 1)
					if pos >= len(layer) || globalErr.Load() != nil {
						return
					}
					idx := layer[pos]
					tx := blk.Txs[idx]

					gasMu.Lock()
					if tx.GasLimit > remainingGas {
						gasMu.Unlock()
						globalErr.Store(fmt.Errorf("block gas limit exceeded"))
						return
					}
					remainingGas -= tx.GasLimit
					gasMu.Unlock()

					txRes, err := ApplyTransaction(snap, tx, tx.GasLimit, execVM, baseFee)
					if err != nil {
						globalErr.Store(fmt.Errorf("tx %s failed: %w", crypto.ToHex(tx.Hash), err))
						return
					}

					if txRes.GasUsed < tx.GasLimit {
						gasMu.Lock()
						remainingGas += tx.GasLimit - txRes.GasUsed
						gasMu.Unlock()
					}

					atomic.AddUint64(&gasUsed, txRes.GasUsed)
					atomic.AddUint64(&feeCollected, txRes.FeeCollected)
					atomic.AddUint64(&burnedFees, txRes.BurnedFee)
					atomic.AddUint64(&validatorTip, txRes.ValidatorTip)

					result.TxResults[idx] = txRes
				}
			}()
		}

		wg.Wait()

		if errVal := globalErr.Load(); errVal != nil {
			return nil, errVal.(error)
		}
	}

	result.GasUsed = gasUsed
	result.FeeCollected = feeCollected
	result.BurnedFees = burnedFees
	result.ValidatorTip = validatorTip

	// ── Phase 3: Credit Validator (Sequential) ───────────────────────────────
	reward := core.BlockReward(blk.Header.Height)
	result.BlockReward = reward

	income, carry := bits.Add64(result.ValidatorTip, reward, 0)
	if carry != 0 {
		return nil, errors.New("validator income overflow")
	}
	if income > 0 {
		validator := snap.GetAccount(validatorAddr)
		newBal, c2 := bits.Add64(validator.Balance, income, 0)
		if c2 != 0 {
			return nil, errors.New("validator balance overflow")
		}
		validator.Balance = newBal
		snap.SetAccount(validatorAddr, validator)
	}

	st.Apply(snap)
	result.ShieldedBalance = st.GetShieldedBalance()
	return result, nil
}
