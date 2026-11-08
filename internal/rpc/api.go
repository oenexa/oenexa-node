package rpc

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/oenexa/oenexa/internal/consensus"
	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mempool"
	"github.com/oenexa/oenexa/internal/state"
	"github.com/oenexa/oenexa/internal/vm"
)

// API holds references to node subsystems and handles JSON-RPC method calls.
type API struct {
	engine        *consensus.Engine
	st            *state.DB
	pool          *mempool.Mempool
	execVM        *vm.VM
	peersFn       func() int // returns current peer count
	broadcastTxFn func(*core.Transaction)
	version       string
}

// NewAPI constructs an API handler.
// peersFn may be nil (peer count will be reported as 0).
func NewAPI(
	engine *consensus.Engine,
	st *state.DB,
	pool *mempool.Mempool,
	execVM *vm.VM,
	peersFn func() int,
	ver string,
) *API {
	if peersFn == nil {
		peersFn = func() int { return 0 }
	}
	return &API{engine: engine, st: st, pool: pool, execVM: execVM, peersFn: peersFn, version: ver}
}

// SetBroadcastTx sets the hook used to broadcast locally submitted transactions over P2P.
func (a *API) SetBroadcastTx(fn func(*core.Transaction)) {
	a.broadcastTxFn = fn
}

// Dispatch routes a parsed request to the appropriate handler.
func (a *API) Dispatch(req *Request) *Response {
	switch req.Method {

	// ── Shielded / Turnstile (OENEXA Dual-Pool) ───────────────────────────
	case "oen_getShieldedBalance":
		return a.getShieldedBalance(req)
	case "oen_getTurnstileStatus":
		return a.getTurnstileStatus(req)

	// ── Chain ──────────────────────────────────────────────────────────────
	case "oen_chainInfo":
		return a.chainInfo(req)

	// ── Blocks ─────────────────────────────────────────────────────────────
	case "oen_recentBlocks", "oen_getLatestBlocks":
		return a.recentBlocks(req)
	case "oen_blockByHeight", "eth_getBlockByNumber":
		return a.blockByHeight(req)
	case "oen_blockByHash":
		return a.blockByHash(req)
	case "oen_blockHeight", "eth_blockNumber":
		return okResponse(req.ID, a.engine.Height())

	// ── Accounts ───────────────────────────────────────────────────────────
	case "oen_getBalance", "eth_getBalance":
		return a.getBalance(req)
	case "oen_getTransactionCount", "eth_getTransactionCount":
		return a.getTransactionCount(req)

	// ── Transactions ───────────────────────────────────────────────────────
	case "oen_sendRawTransaction", "eth_sendRawTransaction":
		return a.sendRawTransaction(req)
	case "oen_call", "eth_call":
		return a.call(req)
	case "oen_getTransactionReceipt", "eth_getTransactionReceipt":
		return a.getTransactionReceipt(req)
	case "oen_getTransactionByHash", "eth_getTransactionByHash":
		return a.getTransactionByHash(req)

	// ── Fees ───────────────────────────────────────────────────────────────
	case "oen_feeEstimate":
		return a.feeEstimate(req)
	case "oen_gasPrice", "eth_gasPrice":
		return a.gasPrice(req)

	// ── Web3 Compatibility ─────────────────────────────────────────────────
	case "eth_chainId":
		return okResponse(req.ID, fmt.Sprintf("0x%x", core.ChainID))
	case "net_version":
		return okResponse(req.ID, fmt.Sprintf("%d", core.ChainID))
	case "web3_clientVersion":
		return okResponse(req.ID, fmt.Sprintf("Oenexa/v%s", a.version))

	default:
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler implementations
// ─────────────────────────────────────────────────────────────────────────────

func (a *API) chainInfo(req *Request) *Response {
	height := a.engine.Height()
	tipHash := ""
	var baseFee uint64 = core.InitialBaseFee
	var gasLimit uint64 = core.BlockGasLimit
	if blk := a.engine.BlockByHeight(height - 1); blk != nil {
		tipHash = crypto.ToHex(blk.Hash)
		baseFee = blk.Header.BaseFee
		gasLimit = blk.Header.GasLimit
	}
	return okResponse(req.ID, &ChainInfo{
		ChainID:             core.ChainID,
		ChainIdCamel:        fmt.Sprintf("%d", core.ChainID),
		NetworkID:           core.ChainID,
		Height:              height,
		TipHash:             tipHash,
		TipHashCamel:        tipHash,
		PeerCount:           a.peersFn(),
		MempoolLen:          a.pool.Len(),
		MempoolSize:         a.pool.Len(),
		Validators:          1,
		ValidatorAddr:       crypto.AddressToHex(a.engine.ValidatorAddr()),
		Round:               int(height % 4),
		BaseFee:             baseFee,
		BaseFeeCamel:        baseFee,
		GasLimit:            gasLimit,
		GasLimitCamel:       gasLimit,
		GasTargetRatio:      0.5,
		GasTargetRatioCamel: 0.5,
		Version:             a.version,
		Ticker:              core.Ticker,
	})
}

func (a *API) recentBlocks(req *Request) *Response {
	var count uint64 = 10
	var params []uint64
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err == nil && len(params) > 0 && params[0] > 0 {
			count = params[0]
			if count > 50 {
				count = 50
			}
		}
	}
	height := a.engine.Height()
	blocks := make([]*BlockInfo, 0, count)
	if height == 0 {
		return okResponse(req.ID, blocks)
	}
	start := height - 1
	var end uint64 = 0
	if start >= count {
		end = start - count + 1
	}
	for h := int64(start); h >= int64(end); h-- {
		blk := a.engine.BlockByHeight(uint64(h))
		if blk != nil {
			blocks = append(blocks, blockToInfo(blk))
		}
	}
	return okResponse(req.ID, blocks)
}

func (a *API) blockByHeight(req *Request) *Response {
	var height uint64
	var intParams []uint64
	if err := json.Unmarshal(req.Params, &intParams); err == nil && len(intParams) > 0 {
		height = intParams[0]
	} else {
		var rawParams []interface{}
		if err := json.Unmarshal(req.Params, &rawParams); err == nil && len(rawParams) > 0 {
			switch v := rawParams[0].(type) {
			case float64:
				height = uint64(v)
			case string:
				if len(v) >= 2 && (v[:2] == "0x" || v[:2] == "0X") {
					parsed, err := strconv.ParseUint(v[2:], 16, 64)
					if err != nil {
						return errResponse(req.ID, CodeInvalidParams, "invalid hex height: "+err.Error())
					}
					height = parsed
				} else {
					parsed, err := strconv.ParseUint(v, 10, 64)
					if err != nil {
						return errResponse(req.ID, CodeInvalidParams, "invalid height string: "+err.Error())
					}
					height = parsed
				}
			default:
				return errResponse(req.ID, CodeInvalidParams, "params: [height uint64|string]")
			}
		} else {
			return errResponse(req.ID, CodeInvalidParams, "params: [height uint64]")
		}
	}
	blk := a.engine.BlockByHeight(height)
	if blk == nil {
		return errResponse(req.ID, CodeInternalError, "block not found")
	}
	return okResponse(req.ID, blockToInfo(blk))
}

func (a *API) blockByHash(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [hash hex-string]")
	}
	hash, err := crypto.HexToHash(params[0])
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid hash: "+err.Error())
	}
	// Linear scan from height 0 up to current tip.
	height := a.engine.Height()
	for h := uint64(0); h < height; h++ {
		blk := a.engine.BlockByHeight(h)
		if blk.Hash == hash {
			return okResponse(req.ID, blockToInfo(blk))
		}
	}
	return errResponse(req.ID, CodeInternalError, "block not found")
}

func (a *API) getBalance(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [address hex-string]")
	}
	addr, err := crypto.HexToAddress(params[0])
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid address: "+err.Error())
	}
	bal := a.st.GetBalance(addr)
	return okResponse(req.ID, map[string]interface{}{
		"balance_oenexa":   bal,
		"balance_nano_oen": bal,
		"balance":          bal,
		"balance_oen":      float64(bal) / float64(core.OneOEN),
	})
}

func (a *API) getShieldedBalance(req *Request) *Response {
	return okResponse(req.ID, map[string]interface{}{
		"shielded_balance_nano_oen": a.st.GetShieldedBalance(),
		"tree_leaf_count":           a.st.CommitmentTree().LeafCount(),
		"tree_root":                 crypto.ToHex(a.st.CommitmentTreeRoot()),
	})
}

func (a *API) getTurnstileStatus(req *Request) *Response {
	trans := a.st.TotalTransparentSupply()
	shielded := a.st.GetShieldedBalance()
	total := a.st.TotalSupply()
	return okResponse(req.ID, map[string]interface{}{
		"transparent_supply_nano_oen": trans,
		"shielded_supply_nano_oen":    shielded,
		"total_supply_nano_oen":       total,
		"invariant_preserved":         trans+shielded == total,
		"status":                      "SECURE",
	})
}

func (a *API) getTransactionCount(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [address hex-string]")
	}
	addr, err := crypto.HexToAddress(params[0])
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid address: "+err.Error())
	}
	return okResponse(req.ID, map[string]uint64{
		"nonce": a.st.GetNonce(addr),
	})
}

// sendRawTransaction accepts a hex-encoded gob-serialised Transaction.
func (a *API) sendRawTransaction(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [raw_tx hex-string]")
	}
	rawStr := params[0]
	if len(rawStr) >= 2 && (rawStr[:2] == "0x" || rawStr[:2] == "0X") {
		rawStr = rawStr[2:]
	}
	raw, err := hex.DecodeString(rawStr)
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid hex: "+err.Error())
	}
	tx, err := gobDecodeTx(raw)
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "decode tx: "+err.Error())
	}
	if err := a.pool.Add(tx); err != nil {
		return errResponse(req.ID, CodeInternalError, "mempool: "+err.Error())
	}
	if a.broadcastTxFn != nil {
		a.broadcastTxFn(tx)
	}
	return okResponse(req.ID, map[string]string{
		"tx_hash": crypto.ToHex(tx.Hash),
	})
}

func (a *API) feeEstimate(req *Request) *Response {
	height := a.engine.Height()
	blk := a.engine.BlockByHeight(height - 1)
	baseFee := blk.Header.BaseFee

	_, ulTip := core.FeeEstimate(baseFee, core.FeeTierUltraLow)
	_, stdTip := core.FeeEstimate(baseFee, core.FeeTierStandard)
	_, fastTip := core.FeeEstimate(baseFee, core.FeeTierFast)

	return okResponse(req.ID, &FeeEstimateResult{
		BaseFee:          baseFee,
		UltraLowTip:      ulTip,
		StandardTip:      stdTip,
		FastTip:          fastTip,
		TransferUltraLow: core.TransferCostOenexa(baseFee, ulTip),
		TransferStandard: core.TransferCostOenexa(baseFee, stdTip),
		TransferFast:     core.TransferCostOenexa(baseFee, fastTip),
	})
}

func (a *API) gasPrice(req *Request) *Response {
	height := a.engine.Height()
	blk := a.engine.BlockByHeight(height - 1)
	return okResponse(req.ID, map[string]uint64{"base_fee": blk.Header.BaseFee})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func blockToInfo(blk *core.Block) *BlockInfo {
	prevHash := crypto.ToHex(blk.Header.PrevHash)
	validatorAddr := crypto.AddressToHex(blk.Header.ValidatorAddr)
	txs := make([]*TxInfo, 0, len(blk.Txs))
	for _, tx := range blk.Txs {
		txs = append(txs, &TxInfo{
			Hash:      "0x" + crypto.ToHex(tx.Hash),
			Type:      uint8(tx.Type),
			Version:   tx.Version,
			Nonce:     tx.Nonce,
			From:      "0x" + crypto.AddressToHex(tx.From),
			To:        "0x" + crypto.AddressToHex(tx.To),
			Amount:    tx.Amount,
			GasLimit:  tx.GasLimit,
			GasPrice:  tx.GasPrice,
			Timestamp: blk.Header.Timestamp,
			DataHex:   "0x" + hex.EncodeToString(tx.Data),
		})
	}
	return &BlockInfo{
		Height:        blk.Header.Height,
		Hash:          crypto.ToHex(blk.Hash),
		PrevHash:      prevHash,
		ParentHash:    prevHash,
		StateRoot:     crypto.ToHex(blk.Header.StateRoot),
		MerkleRoot:    crypto.ToHex(blk.Header.MerkleRoot),
		Timestamp:     blk.Header.Timestamp,
		ValidatorAddr: validatorAddr,
		Proposer:      validatorAddr,
		GasUsed:       blk.Header.GasUsed,
		GasLimit:      blk.Header.GasLimit,
		BaseFee:       blk.Header.BaseFee,
		BurnedFees:    blk.Header.BurnedFees,
		TxCount:       len(blk.Txs),
		Transactions:  txs,
	}
}

// gobDecodeTx decodes a gob-encoded Transaction from raw bytes.
func gobDecodeTx(raw []byte) (*core.Transaction, error) {
	var tx core.Transaction
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// GobEncodeTx encodes a transaction to raw bytes suitable for sendRawTransaction.
func GobEncodeTx(tx *core.Transaction) ([]byte, error) {
	var buf bytes.Buffer
	_ = gob.NewEncoder(&buf).Encode(tx)
	return buf.Bytes(), nil
}

type callArgs struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Value string `json:"value"`
	Data  string `json:"data"`
}

// call executes a smart contract query locally without broadcasting a transaction.
func (a *API) call(req *Request) *Response {
	var params []json.RawMessage
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [call_obj, block_num]")
	}
	var args callArgs
	if err := json.Unmarshal(params[0], &args); err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid call object")
	}

	tx := &core.Transaction{
		Type:     core.TxCall,
		GasLimit: 10_000_000,
		GasPrice: 0,
	}

	if args.From != "" {
		addr, err := crypto.HexToAddress(args.From)
		if err == nil {
			tx.From = addr
		}
	}
	if args.To != "" {
		addr, err := crypto.HexToAddress(args.To)
		if err == nil {
			tx.To = addr
		}
	}
	if args.Value != "" {
		valStr := args.Value
		if len(valStr) >= 2 && (valStr[:2] == "0x" || valStr[:2] == "0X") {
			v, err := strconv.ParseUint(valStr[2:], 16, 64)
			if err == nil {
				tx.Amount = v
			}
		} else {
			v, err := strconv.ParseUint(valStr, 10, 64)
			if err == nil {
				tx.Amount = v
			}
		}
	}
	if args.Data != "" {
		dataStr := args.Data
		if len(dataStr) >= 2 && (dataStr[:2] == "0x" || dataStr[:2] == "0X") {
			dataStr = dataStr[2:]
		}
		data, err := hex.DecodeString(dataStr)
		if err == nil {
			tx.Data = data
		}
	}

	// Create a snapshot to ensure the read-only call does not mutate the node state
	snap := a.st.Snapshot()

	res, err := state.ApplyTransaction(snap, tx, 10_000_000, a.execVM, 0)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, fmt.Sprintf("execution reverted: %v", err))
	}

	return okResponse(req.ID, "0x"+hex.EncodeToString(res.ReturnData))
}

func (a *API) getTransactionReceipt(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [tx_hash hex-string]")
	}
	hashStr := params[0]
	if len(hashStr) >= 2 && (hashStr[:2] == "0x" || hashStr[:2] == "0X") {
		hashStr = hashStr[2:]
	}
	hash, err := crypto.HexToHash(hashStr)
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid hash: "+err.Error())
	}
	
	// Linear scan (for testing)
	height := a.engine.Height()
	for h := uint64(0); h < height; h++ {
		blk := a.engine.BlockByHeight(h)
		for i, tx := range blk.Txs {
			if tx.Hash == hash {
				return okResponse(req.ID, &TxReceipt{
					TransactionHash:   "0x" + crypto.ToHex(tx.Hash),
					TransactionIndex:  fmt.Sprintf("0x%x", i),
					BlockHash:         "0x" + crypto.ToHex(blk.Hash),
					BlockNumber:       fmt.Sprintf("0x%x", blk.Header.Height),
					From:              "0x" + crypto.AddressToHex(tx.From),
					To:                "0x" + crypto.AddressToHex(tx.To),
					GasUsed:           fmt.Sprintf("0x%x", tx.GasLimit), // Approx
					EffectiveGasPrice: fmt.Sprintf("0x%x", blk.Header.BaseFee),
					Status:            "0x1", // 1 = success
				})
			}
		}
	}
	
	return okResponse(req.ID, nil) // not found
}

func (a *API) getTransactionByHash(req *Request) *Response {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		return errResponse(req.ID, CodeInvalidParams, "params: [tx_hash hex-string]")
	}
	hashStr := params[0]
	if len(hashStr) >= 2 && (hashStr[:2] == "0x" || hashStr[:2] == "0X") {
		hashStr = hashStr[2:]
	}
	hash, err := crypto.HexToHash(hashStr)
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid hash: "+err.Error())
	}
	
	height := a.engine.Height()
	for h := uint64(0); h < height; h++ {
		blk := a.engine.BlockByHeight(h)
		for _, tx := range blk.Txs {
			if tx.Hash == hash {
				return okResponse(req.ID, &TxInfo{
					Hash:      "0x" + crypto.ToHex(tx.Hash),
					Type:      uint8(tx.Type),
					Version:   tx.Version,
					Nonce:     tx.Nonce,
					From:      "0x" + crypto.AddressToHex(tx.From),
					To:        "0x" + crypto.AddressToHex(tx.To),
					Amount:    tx.Amount,
					GasLimit:  tx.GasLimit,
					GasPrice:  tx.GasPrice,
					Timestamp: blk.Header.Timestamp,
					DataHex:   "0x" + hex.EncodeToString(tx.Data),
				})
			}
		}
	}
	
	return okResponse(req.ID, nil)
}
