package state

import (
	"errors"
	"fmt"
	"math/bits"
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

	// ── Phase 1: Static Dependency Analysis ───────────────────────────────────
	// Group transactions into sequential layers of strictly disjoint sender/recipient pairs.
	var layers [][]int

	for i, tx := range blk.Txs {
		if tx == nil {
			return nil, fmt.Errorf("nil transaction at index %d", i)
		}
		// Contract deploys/calls and shielded pool txs can mutate global or arbitrary state
		// (or trigger snapshot rollback on revert), so isolate them in exclusive layers.
		isStatefulBarrier := tx.Type == core.TxDeploy || tx.Type == core.TxCall ||
			tx.Type == core.TxShield || tx.Type == core.TxUnshield || tx.Type == core.TxShieldedTransfer
		if isStatefulBarrier {
			layers = append(layers, []int{i})
			continue
		}

		sender := tx.From
		recipient := tx.To

		placed := false
		for layerIdx, layer := range layers {
			conflict := false
			for _, prevIdx := range layer {
				prevTx := blk.Txs[prevIdx]
				if prevTx.Type == core.TxDeploy || prevTx.Type == core.TxCall ||
					prevTx.Type == core.TxShield || prevTx.Type == core.TxUnshield || prevTx.Type == core.TxShieldedTransfer {
					conflict = true
					break
				}
				pSender := prevTx.From

				if sender == pSender || sender == prevTx.To ||
					recipient == pSender || recipient == prevTx.To {
					conflict = true
					break
				}
			}

			if !conflict {
				layers[layerIdx] = append(layers[layerIdx], i)
				placed = true
				break
			}
		}

		if !placed {
			layers = append(layers, []int{i})
		}
	}

	// ── Phase 2: Parallel Execution ──────────────────────────────────────────
	var gasUsed, feeCollected, burnedFees, validatorTip uint64
	var globalErr atomic.Value

	for _, layer := range layers {
		var wg sync.WaitGroup

		for _, txIdx := range layer {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				tx := blk.Txs[idx]

				// Reserve tx.GasLimit atomically using checked uint64 arithmetic.
				gasMu.Lock()
				if tx.GasLimit > remainingGas {
					gasMu.Unlock()
					globalErr.Store(fmt.Errorf("block gas limit exceeded"))
					return
				}
				remainingGas -= tx.GasLimit
				gasMu.Unlock()

				if globalErr.Load() != nil {
					return
				}

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
			}(txIdx)
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
