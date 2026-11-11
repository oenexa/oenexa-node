// Package consensus implements the Byzantine Fault Tolerant (BFT) consensus engine for OENEXA Layer-1.
//
// ─────────────────────────────────────────────────────────────────────────────
// BFT CONSENSUS SAFETY, LIVELINESS & EQUIVOCATION SLASHING SPECIFICATION
// ─────────────────────────────────────────────────────────────────────────────
//
// PURPOSE:
//   Orchestrates round-robin block proposals, two-phase voting (Prevote -> Precommit),
//   instant finality, validator slashing, and post-quantum header validation.
//
// WHY (ARCHITECTURAL RATIONALE):
//   1. Instant Finality: Once a block reaches 2/3+ precommit quorum, it is finalized
//      immediately. Unlike probabilistic Proof-of-Work, OENEXA has zero chain reorganizations.
//   2. High Throughput: 2.0-second block intervals allow sustained high TPS with bounded latency.
//   3. Economic Slashing: Any validator that signs two conflicting votes at the same height/round
//      (equivocation) is penalized: 10% of their staked balance is slashed and voting power is revoked.
//   4. Post-Quantum Cryptography: Block headers and BFT votes are signed using NIST ML-DSA-65.
//
// HOW (MECHANICAL EXECUTION FLOW):
//   1. StepPropose: The designated proposer for height `h` (where proposer = h % |Validators|)
//      assembles transactions from the mempool, computes state transitions, burns the base fee,
//      and signs the block header.
//   2. StepPrevote: Validators verify proposer signature, parent hash linkage, transaction Merkle root,
//      and state root. If valid, each validator signs and broadcasts a Prevote for the block hash.
//   3. StepPrecommit: Upon receiving > 2/3 voting power in Prevotes, the node transitions to
//      Precommit and broadcasts a Precommit vote.
//   4. StepCommit: Upon receiving > 2/3 voting power in Precommits, the block is committed to the
//      canonical chain, state transitions are applied, and mempool transactions are purged.
//
// FORMAL PROOF OF BFT CONSENSUS SAFETY (NO-FORK GUARANTEE):
//   Let N be the total voting power across all active validators.
//   Let f be the maximum Byzantine (malicious or faulty) voting power, such that N >= 3f + 1.
//   The consensus quorum threshold is Q = floor(2N/3) + 1 >= 2f + 1.
//
//   Theorem: Two conflicting blocks B1 != B2 cannot both be committed at the same height h.
//
//   Proof by Contradiction:
//     1. Assume for contradiction that two distinct blocks B1 and B2 are both committed at height h.
//     2. By definition of StepCommit, block B1 received a set of precommits S1 with total power |S1| >= Q.
//     3. Similarly, block B2 received a set of precommits S2 with total power |S2| >= Q.
//     4. The intersection of these two precommit quorums must contain:
//          |S1 \cap S2| = |S1| + |S2| - |S1 \cup S2|
//                       >= 2Q - N
//                       >= 2(2f + 1) - (3f + 1)
//                       = 4f + 2 - 3f - 1
//                       = f + 1.
//     5. Since there are at most f Byzantine validators in the entire network, the intersection
//        S1 \cap S2 must contain at least (f + 1) - f = 1 honest validator.
//     6. For this honest validator to be in S1 \cap S2, it must have signed a precommit for B1
//        AND a precommit for B2 at the same height h and round r.
//     7. However, honest validators strictly enforce the Anti-Slashing High-Water Mark rule
//        (LocalSigner.SignVote), which rejects signing a second vote for a conflicting block hash
//        at the same height and round.
//     8. This contradicts the definition of an honest validator.
//     9. Therefore, two conflicting blocks cannot both attain quorum at the same height. Q.E.D.
// ─────────────────────────────────────────────────────────────────────────────
package consensus

import (
	"context"
	"fmt"
	"log"
	"math/bits"
	"sync"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/state"
	"github.com/oenexa/oenexa/internal/upgrade"
	"github.com/oenexa/oenexa/internal/vm"
)

const (
	BlockInterval = 2 * time.Second
	RoundTimeout  = 4 * time.Second
	MaxTxPerBlock = 10_000 // raised to match the higher block gas limit
)

type BFTStep int

const (
	StepPropose BFTStep = iota
	StepPrevote
	StepPrecommit
	StepCommit
)

// Engine drives BFT block production and validation.
type Engine struct {
	mu sync.Mutex

	validatorAddr [crypto.AddressSize]byte
	validatorPub  []byte
	validatorPriv []byte
	signer        ValidatorSigner

	validatorSet *ValidatorSet
	state        *state.DB
	pool         *mempool.Mempool
	execVM       *vm.VM
	upgradeMgr   *upgrade.Manager

	chain    []*core.Block
	commitCh chan *core.Block

	// BFT State
	step          BFTStep
	round         uint32
	proposal      *core.Block
	proposalState *state.DB
	prevotes      map[[crypto.HashSize]byte]map[string]*Vote // blockHash -> validator -> Vote
	precommits    map[[crypto.HashSize]byte]map[string]*Vote

	// P2P Hooks
	OnVoteBroadcast  func(*Vote)
	OnBlockBroadcast func(*core.Block)
}

// NewEngine creates a consensus engine.
// execVM and upgradeMgr may be nil if those features are not needed.
func NewEngine(
	addr [crypto.AddressSize]byte,
	pubKey, privKey []byte,
	vs *ValidatorSet,
	st *state.DB,
	pool *mempool.Mempool,
	genesis *core.Block,
	execVM *vm.VM,
	upgradeMgr *upgrade.Manager,
) *Engine {
	signer := NewLocalSigner(addr, pubKey, privKey)
	return &Engine{
		validatorAddr: addr,
		validatorPub:  pubKey,
		validatorPriv: privKey,
		signer:        signer,
		validatorSet:  vs,
		state:         st,
		pool:          pool,
		execVM:        execVM,
		upgradeMgr:    upgradeMgr,
		chain:         []*core.Block{genesis},
		commitCh:      make(chan *core.Block, 64),
		prevotes:      make(map[[crypto.HashSize]byte]map[string]*Vote),
		precommits:    make(map[[crypto.HashSize]byte]map[string]*Vote),
	}
}

// NewEngineWithSigner creates a consensus engine using a custom ValidatorSigner (e.g. KMS/HSM).
func NewEngineWithSigner(
	signer ValidatorSigner,
	vs *ValidatorSet,
	st *state.DB,
	pool *mempool.Mempool,
	genesis *core.Block,
	execVM *vm.VM,
	upgradeMgr *upgrade.Manager,
) *Engine {
	addr := signer.Address()
	pub := signer.PublicKey()
	return &Engine{
		validatorAddr: addr,
		validatorPub:  pub,
		signer:        signer,
		validatorSet:  vs,
		state:         st,
		pool:          pool,
		execVM:        execVM,
		upgradeMgr:    upgradeMgr,
		chain:         []*core.Block{genesis},
		commitCh:      make(chan *core.Block, 64),
		prevotes:      make(map[[crypto.HashSize]byte]map[string]*Vote),
		precommits:    make(map[[crypto.HashSize]byte]map[string]*Vote),
	}
}

// SetSigner updates the active validator signer interface.
func (e *Engine) SetSigner(s ValidatorSigner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.signer = s
	if s != nil {
		e.validatorAddr = s.Address()
		e.validatorPub = s.PublicKey()
	}
}

// Signer returns the active validator signer.
func (e *Engine) Signer() ValidatorSigner {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.signer
}

var (
	blockInterval = BlockInterval
	applyTxFunc   = state.ApplyTransaction
	newBlockFunc  = core.NewBlock
	commitFunc    = func(e *Engine, blk *core.Block, snap *state.DB) error {
		return e.commit(blk, snap)
	}
)

// Run starts the block-production loop; it blocks until ctx is canceled.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(blockInterval)
	defer ticker.Stop()
	log.Printf("[consensus] engine started, validator=%s", crypto.ToHex(e.validatorAddr))
	for {
		select {
		case <-ctx.Done():
			log.Println("[consensus] engine stopped")
			return
		case <-ticker.C:
			if err := e.produceBlock(ctx); err != nil {
				log.Printf("[consensus] block production error: %v", err)
			}
		}
	}
}

func (e *Engine) produceBlock(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	height := uint64(len(e.chain))
	proposer := e.validatorSet.Proposer(height)
	if proposer.Address != e.validatorAddr {
		return nil // not our turn
	}

	if e.step != StepPropose {
		return nil // already proposing/voting for this height
	}

	blk, newState, err := e.buildBlock(height)
	if err != nil {
		return fmt.Errorf("buildBlock: %w", err)
	}
	
	e.proposal = blk
	e.proposalState = newState
	e.step = StepPrevote

	log.Printf("[consensus] Proposing block %d (%x)", height, blk.Hash[:4])

	if e.OnBlockBroadcast != nil {
		e.OnBlockBroadcast(blk)
	}

	// Since we proposed it, we also prevote for it
	e.castVoteLocked(VotePrevote, height, blk.Hash)
	return nil
}

// ProcessProposal receives a block proposal from the network.
func (e *Engine) ProcessProposal(blk *core.Block) error {
	if blk == nil {
		return fmt.Errorf("nil block proposal")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	height := uint64(len(e.chain))
	if blk.Header.Height != height {
		return fmt.Errorf("proposal height %d does not match chain height %d", blk.Header.Height, height)
	}

	proposer := e.validatorSet.Proposer(height)
	if proposer == nil || blk.Header.ValidatorAddr != proposer.Address {
		return fmt.Errorf("invalid proposer")
	}

	// 1. Cryptographic validation: Verify proposer's ML-DSA-65 header signature
	if len(proposer.PublicKey) > 0 {
		if err := blk.VerifyValidatorSig(proposer.PublicKey); err != nil {
			return fmt.Errorf("invalid block validator signature: %w", err)
		}
	}

	// 2. Chain linkage validation
	prev := e.chain[height-1]
	if blk.Header.PrevHash != prev.Hash {
		return fmt.Errorf("invalid parent block hash")
	}
	if blk.Header.Timestamp <= prev.Header.Timestamp {
		return fmt.Errorf("block timestamp %d not after parent timestamp %d", blk.Header.Timestamp, prev.Header.Timestamp)
	}

	// 3. Transaction validation & Merkle root verification
	txHashes := make([][crypto.HashSize]byte, len(blk.Txs))
	for i, tx := range blk.Txs {
		if tx == nil {
			return fmt.Errorf("nil transaction at index %d", i)
		}
		if err := tx.BasicValidate(); err != nil {
			return fmt.Errorf("invalid transaction at index %d (%s): %w", i, crypto.ToHex(tx.Hash), err)
		}
		txHashes[i] = tx.Hash
	}
	expectedMerkle := core.ComputeMerkleRoot(txHashes)
	if expectedMerkle != blk.Header.MerkleRoot {
		return fmt.Errorf("invalid transactions merkle root")
	}

	// 4. State transition replay and validation
	snap := e.state.Snapshot()
	baseFee := prev.Header.BaseFee
	var totalGas, totalBurned, totalTip uint64

	for _, tx := range blk.Txs {
		result, err := applyTxFunc(snap, tx, core.BlockGasLimit-totalGas, e.execVM, baseFee)
		if err != nil {
			return fmt.Errorf("transaction execution failed (%s): %w", crypto.ToHex(tx.Hash), err)
		}
		totalGas += result.GasUsed
		totalBurned += result.BurnedFee
		totalTip += result.ValidatorTip
		if totalGas > core.BlockGasLimit {
			return fmt.Errorf("block gas limit exceeded")
		}
	}

	if totalGas != blk.Header.GasUsed {
		return fmt.Errorf("gas used mismatch: header has %d, computed %d", blk.Header.GasUsed, totalGas)
	}
	if totalBurned != blk.Header.BurnedFees {
		return fmt.Errorf("burned fees mismatch: header has %d, computed %d", blk.Header.BurnedFees, totalBurned)
	}
	expectedBaseFee := core.NextBaseFee(baseFee, totalGas)
	if blk.Header.BaseFee != expectedBaseFee {
		return fmt.Errorf("base fee mismatch: header has %d, expected %d", blk.Header.BaseFee, expectedBaseFee)
	}

	// Credit validator: tip + block subsidy (overflow-checked)
	reward := core.BlockReward(height)
	income, carry := bits.Add64(totalTip, reward, 0)
	if carry != 0 {
		return fmt.Errorf("validator income overflow")
	}
	if income > 0 {
		valAcc := snap.GetAccount(proposer.Address)
		newBal, c2 := bits.Add64(valAcc.Balance, income, 0)
		if c2 != 0 {
			return fmt.Errorf("validator balance overflow")
		}
		valAcc.Balance = newBal
		snap.SetAccount(proposer.Address, valAcc)
	}

	computedRoot := snap.CommitRoot()
	if computedRoot != blk.Header.StateRoot {
		return fmt.Errorf("state root mismatch: header has %s, computed %s", crypto.ToHex(blk.Header.StateRoot), crypto.ToHex(computedRoot))
	}

	if e.step > StepPropose {
		return nil // already processing or past propose step
	}

	e.proposal = blk
	e.proposalState = snap
	e.step = StepPrevote

	// Broadcast our prevote
	e.castVoteLocked(VotePrevote, height, blk.Hash)
	return nil
}

// ProcessVote receives a BFT vote from the network.
func (e *Engine) ProcessVote(v *Vote) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := v.Verify(); err != nil {
		return err
	}

	height := uint64(len(e.chain))
	if v.Height != height {
		return nil
	}

	if !e.validatorSet.Contains(v.Voter) {
		return fmt.Errorf("vote from non-validator")
	}

	voterHex := crypto.ToHex(v.Voter)
	hash := v.BlockHash

	// Equivocation detection: check if validator voted for a different hash at the same height & round
	votesMap := e.prevotes
	if v.Type == VotePrecommit {
		votesMap = e.precommits
	}
	for otherHash, voterVotes := range votesMap {
		if otherHash != hash {
			if existingVote, exists := voterVotes[voterHex]; exists && existingVote.Round == v.Round {
				log.Printf("[consensus] SECURITY ALERT: Equivocation detected from validator %s at height %d round %d! Slashing validator.", voterHex, v.Height, v.Round)
				e.slashValidatorLocked(v.Voter)
				return fmt.Errorf("equivocation detected from validator %s", voterHex)
			}
		}
	}

	if v.Type == VotePrevote {
		if e.prevotes[hash] == nil {
			e.prevotes[hash] = make(map[string]*Vote)
		}
		e.prevotes[hash][voterHex] = v
		e.checkPrevoteQuorumLocked(height, hash)
	} else if v.Type == VotePrecommit {
		if e.precommits[hash] == nil {
			e.precommits[hash] = make(map[string]*Vote)
		}
		e.precommits[hash][voterHex] = v
		e.checkPrecommitQuorumLocked(height, hash)
	}
	return nil
}

func (e *Engine) slashValidatorLocked(addr [crypto.AddressSize]byte) {
	// 1. Slash voting power in validator set
	e.validatorSet.Slash(addr, 1)

	// 2. Slash 10% on-chain balance in state if account exists
	if e.state != nil {
		acc := e.state.GetAccount(addr)
		if acc != nil && acc.Balance > 0 {
			penalty := acc.Balance / 10
			acc.Balance -= penalty
			e.state.SetAccount(addr, acc)
			log.Printf("[consensus] Slashed %d ONXC from validator %s", penalty/core.OneONXC, crypto.ToHex(addr))
		}
	}
}

func (e *Engine) castVoteLocked(voteType uint8, height uint64, hash [crypto.HashSize]byte) {
	v := &Vote{
		Type:      voteType,
		Height:    height,
		Round:     e.round,
		BlockHash: hash,
		Voter:     e.validatorAddr,
		PublicKey: e.validatorPub,
	}

	if e.signer != nil {
		if err := e.signer.SignVote(v); err != nil {
			log.Printf("[consensus] signer error: %v", err)
			return
		}
	} else {
		_ = v.Sign(e.validatorPriv)
	}

	if voteType == VotePrevote {
		if e.prevotes[hash] == nil {
			e.prevotes[hash] = make(map[string]*Vote)
		}
		e.prevotes[hash][crypto.ToHex(e.validatorAddr)] = v
	} else if voteType == VotePrecommit {
		if e.precommits[hash] == nil {
			e.precommits[hash] = make(map[string]*Vote)
		}
		e.precommits[hash][crypto.ToHex(e.validatorAddr)] = v
	}

	if e.OnVoteBroadcast != nil {
		e.OnVoteBroadcast(v)
	}

	if voteType == VotePrevote {
		e.checkPrevoteQuorumLocked(height, hash)
	} else {
		e.checkPrecommitQuorumLocked(height, hash)
	}
}

func (e *Engine) checkPrevoteQuorumLocked(height uint64, hash [crypto.HashSize]byte) {
	if e.step >= StepPrecommit {
		return
	}
	var voters [][crypto.AddressSize]byte
	for _, vote := range e.prevotes[hash] {
		voters = append(voters, vote.Voter)
	}
	if e.validatorSet.HasQuorum(voters) {
		e.step = StepPrecommit
		e.castVoteLocked(VotePrecommit, height, hash)
	}
}

func (e *Engine) checkPrecommitQuorumLocked(height uint64, hash [crypto.HashSize]byte) {
	if e.step >= StepCommit {
		return
	}
	var voters [][crypto.AddressSize]byte
	for _, vote := range e.precommits[hash] {
		voters = append(voters, vote.Voter)
	}
	if e.validatorSet.HasQuorum(voters) {
		if e.proposal == nil || e.proposal.Hash != hash {
			return
		}
		e.step = StepCommit
		log.Printf("[consensus] Quorum reached for block %d. Committing.", height)

		stateToCommit := e.proposalState
		if stateToCommit == nil {
			_, stateToCommit, _ = e.buildBlock(height)
		}

		if stateToCommit != nil {
			_ = commitFunc(e, e.proposal, stateToCommit)
		}
		
		// Reset for next height
		e.step = StepPropose
		e.round = 0
		e.proposal = nil
		e.proposalState = nil
		e.prevotes = make(map[[crypto.HashSize]byte]map[string]*Vote)
		e.precommits = make(map[[crypto.HashSize]byte]map[string]*Vote)
	}
}

// buildBlock assembles a new block on a state snapshot.
// Fee model: baseFee is burned; only priorityTip + blockReward go to validator.
func (e *Engine) buildBlock(height uint64) (*core.Block, *state.DB, error) {
	prev := e.chain[height-1]
	prevHash := prev.Hash
	baseFee := prev.Header.BaseFee // inherit parent's base fee
	snap := e.state.Snapshot()

	pending := e.pool.Pending(MaxTxPerBlock)
	var included []*core.Transaction
	var totalGas, totalBurned, totalTip uint64

	for _, tx := range pending {
		txSnap := snap.Snapshot()
		result, err := applyTxFunc(snap, tx, core.BlockGasLimit-totalGas, e.execVM, baseFee)
		if err != nil || (result != nil && !result.Success && tx.Type != core.TxCall) {
			snap.Apply(txSnap)
			var rErr error
			if result != nil {
				rErr = result.Error
			}
			log.Printf("[consensus] buildBlock: skipping invalid/reverted tx %x: err=%v, resErr=%v", tx.Hash[:4], err, rErr)
			continue
		}
		totalGas += result.GasUsed
		totalBurned += result.BurnedFee
		totalTip += result.ValidatorTip
		included = append(included, tx)
		if totalGas >= core.BlockGasLimit {
			break
		}
	}

	// ── Credit validator: tip + block subsidy (NOT burned fees) ─────────────
	reward := core.BlockReward(height)
	income, carry := bits.Add64(totalTip, reward, 0)
	if carry != 0 {
		return nil, nil, fmt.Errorf("validator income overflow")
	}
	if income > 0 {
		validator := snap.GetAccount(e.validatorAddr)
		newBal, c2 := bits.Add64(validator.Balance, income, 0)
		if c2 != 0 {
			return nil, nil, fmt.Errorf("validator balance overflow")
		}
		validator.Balance = newBal
		snap.SetAccount(e.validatorAddr, validator)
	}

	stateRoot := snap.CommitRoot()

	// ── Compute next block's base fee ────────────────────────────────────────
	nextBaseFee := core.NextBaseFee(baseFee, totalGas)

	blk, err := newBlockFunc(
		height, prevHash, stateRoot,
		time.Now().UnixNano(),
		e.validatorAddr, included, totalGas, nextBaseFee, totalBurned,
	)
	if err != nil {
		return nil, nil, err
	}

	if e.signer != nil {
		if err := e.signer.SignBlock(blk); err != nil {
			return nil, nil, err
		}
	} else {
		if err := blk.SignHeader(e.validatorPriv); err != nil {
			return nil, nil, err
		}
	}
	return blk, snap, nil
}

// commit finalizes a block: applies state, purges mempool, appends a chain.
func (e *Engine) commit(blk *core.Block, snap *state.DB) error {
	e.state.Apply(snap)
	e.pool.PurgeCommitted(blk.Txs)
	e.chain = append(e.chain, blk)

	reward := core.BlockReward(blk.Header.Height)
	log.Printf("[consensus] block height=%d hash=%s txs=%d gas=%d baseFee=%d burned=%d nano-ONXC reward=%d ONXC",
		blk.Header.Height,
		crypto.ToHex(blk.Hash)[:16]+"…",
		len(blk.Txs),
		blk.Header.GasUsed,
		blk.Header.BaseFee,
		blk.Header.BurnedFees,
		reward/core.OneONXC,
	)

	select {
	case e.commitCh <- blk:
	default:
	}

	if e.upgradeMgr != nil {
		e.upgradeMgr.OnBlock(blk.Header.Height)
	}
	return nil
}

// CommitCh returns a channel that receives every committed block.
func (e *Engine) CommitCh() <-chan *core.Block { return e.commitCh }

// InjectBlock appends a block that was loaded from persistent storage during
// node startup.  It does NOT re-apply state — the caller must ensure state
// is already consistent with the injected blocks.
func (e *Engine) InjectBlock(blk *core.Block) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Extend the chain slice to the required height, filling gaps with nil.
	for uint64(len(e.chain)) <= blk.Header.Height {
		e.chain = append(e.chain, nil)
	}
	e.chain[blk.Header.Height] = blk
}

// BlockByHeight returns the block at the given height or nil.
func (e *Engine) BlockByHeight(h uint64) *core.Block {
	e.mu.Lock()
	defer e.mu.Unlock()
	if h >= uint64(len(e.chain)) {
		return nil
	}
	return e.chain[h]
}

// BlockByHash returns the block with the matching hash or nil.
func (e *Engine) BlockByHash(hash [crypto.HashSize]byte) *core.Block {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, blk := range e.chain {
		if blk != nil && blk.Hash == hash {
			return blk
		}
	}
	return nil
}

// Height returns the current chain height (number of blocks including genesis).
func (e *Engine) Height() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return uint64(len(e.chain))
}

// AcceptVote processes an incoming vote received from a peer validator.
// Called by the P2P layer when vote messages arrive from other validators.
// Fully processes the vote through the BFT state machine (verify, store,
// check quorum) — identical to ProcessVote but accessible as a named
// API for the P2P integration layer.
func (e *Engine) AcceptVote(v *Vote) error {
	return e.ProcessVote(v)
}

// ValidatorAddr returns the engine's local validator address.
func (e *Engine) ValidatorAddr() [crypto.AddressSize]byte {
	return e.validatorAddr
}

// BuildBlock assembles a new block on a state snapshot. Exported for tests and tooling.
func (e *Engine) BuildBlock(height uint64) (*core.Block, *state.DB, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buildBlock(height)
}

// Commit finalizes a block: applies state, purges mempool, appends to chain. Exported for tests and tooling.
func (e *Engine) Commit(blk *core.Block, snap *state.DB) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.commit(blk, snap)
}
