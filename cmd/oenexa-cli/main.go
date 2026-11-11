package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/rpc"
)

var defaultRPCURL = "http://127.0.0.1:8545"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]

	switch command {
	case "keygen":
		handleKeygen()
	case "balance":
		if len(os.Args) < 3 {
			fmt.Println("Usage: oenexa-cli balance <address> [--rpc <url>]")
			return
		}
		rpcURL := getRPCFlag(os.Args[3:])
		handleBalance(os.Args[2], rpcURL)
	case "transfer":
		if len(os.Args) < 5 {
			fmt.Println("Usage: oenexa-cli transfer <private_key_hex> <to_address> <amount_in_onxc> [--rpc <url>]")
			return
		}
		rpcURL := getRPCFlag(os.Args[5:])
		handleTransfer(os.Args[2], os.Args[3], os.Args[4], rpcURL)
	case "info":
		rpcURL := getRPCFlag(os.Args[2:])
		handleInfo(rpcURL)
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Println("OENEXA Post-Quantum Wallet & Node CLI (Native Coin: ONXC)")
	fmt.Println("Usage:")
	fmt.Println("  oenexa-cli keygen                                         - Generate a new ML-DSA-65 post-quantum wallet")
	fmt.Println("  oenexa-cli balance <address> [--rpc <url>]                - Check ONXC balance of an address")
	fmt.Println("  oenexa-cli transfer <priv_key> <to> <amount> [--rpc url]  - Sign and broadcast a transfer in ONXC")
	fmt.Println("  oenexa-cli info [--rpc <url>]                             - Query node sync and chain information")
}

func getRPCFlag(args []string) string {
	fs := flag.NewFlagSet("cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rpcURL := fs.String("rpc", defaultRPCURL, "JSON-RPC endpoint URL")
	_ = fs.Parse(args)
	return *rpcURL
}

func handleKeygen() {
	wallet, err := crypto.NewWallet()
	if err != nil {
		fmt.Printf("Error: Failed to generate wallet: %v\n", err)
		return
	}

	fmt.Println("================================================================================")
	fmt.Println("              OENEXA Post-Quantum Wallet Generated (ML-DSA-65)                  ")
	fmt.Println("================================================================================")
	fmt.Printf("Address     : %s\n", crypto.AddressToHex(wallet.Address))
	fmt.Printf("Public Key  : %s\n", hex.EncodeToString(wallet.PublicKey))
	fmt.Printf("Private Key : %s\n", hex.EncodeToString(wallet.PrivateKey))
	fmt.Println("================================================================================")
	fmt.Println("CRITICAL: Keep your private key confidential. It is required to sign transactions.")
}

func handleInfo(rpcURL string) {
	res, err := callRPC(rpcURL, "onxc_chainInfo", nil)
	if err != nil {
		fmt.Printf("Error querying node: %v\n", err)
		return
	}

	var info rpc.ChainInfo
	if err := json.Unmarshal(res, &info); err != nil {
		fmt.Printf("Error parsing chain info: %v\n", err)
		return
	}

	fmt.Println("=== OENEXA Node Status ===")
	fmt.Printf("Network     : OENEXA Mainnet (Chain ID %d)\n", info.ChainID)
	fmt.Printf("Node Version: %s\n", info.Version)
	fmt.Printf("Chain Height: %d blocks\n", info.Height)
	fmt.Printf("Tip Hash    : %s\n", info.TipHash)
	fmt.Printf("Peer Count  : %d peers\n", info.PeerCount)
	fmt.Printf("Mempool     : %d pending transactions\n", info.MempoolLen)
	fmt.Printf("Ticker      : %s\n", info.Ticker)
}

func handleBalance(addrStr, rpcURL string) {
	addrStr = strings.TrimPrefix(addrStr, "0x")
	res, err := callRPC(rpcURL, "onxc_getBalance", []string{addrStr})
	if err != nil {
		fmt.Printf("Error querying balance: %v\n", err)
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(res, &data); err != nil {
		fmt.Printf("Error decoding balance: %v\n", err)
		return
	}

	var balNano uint64
	if v, ok := data["balance_nano_onxc"]; ok {
		switch n := v.(type) {
		case float64:
			balNano = uint64(n)
		case uint64:
			balNano = n
		}
	}
	balONXC := float64(balNano) / float64(core.OneONXC)

	fmt.Println("=== OENEXA Balance ===")
	fmt.Printf("Address : %s\n", addrStr)
	fmt.Printf("Balance : %.9f ONXC (%d nano-ONXC)\n", balONXC, balNano)
}

func handleTransfer(privKeyHex, toAddrHex, amountStr, rpcURL string) {
	privKeyHex = strings.TrimPrefix(privKeyHex, "0x")
	privKey, err := hex.DecodeString(privKeyHex)
	if len(privKey) > 0 {
		defer clear(privKey)
	}
	if err != nil || len(privKey) != crypto.PrivateKeySize {
		fmt.Printf("Error: Invalid ML-DSA-65 private key (expected %d bytes hex)\n", crypto.PrivateKeySize)
		return
	}

	wallet, err := crypto.WalletFromPrivateKey(privKey)
	if err != nil {
		fmt.Printf("Error recovering wallet from private key: %v\n", err)
		return
	}
	defer wallet.Zeroize()

	toAddr, err := crypto.HexToAddress(toAddrHex)
	if err != nil {
		fmt.Printf("Error: Invalid recipient address: %v\n", err)
		return
	}

	amountONXC, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || math.IsNaN(amountONXC) || math.IsInf(amountONXC, 0) || amountONXC <= 0 {
		fmt.Printf("Error: Invalid transfer amount: %s\n", amountStr)
		return
	}
	scaled := amountONXC * float64(core.OneONXC)
	if scaled >= float64(math.MaxUint64) || scaled < 1 {
		fmt.Printf("Error: Transfer amount out of range: %s\n", amountStr)
		return
	}
	amountNano := uint64(scaled)

	// 1. Fetch current account nonce from node
	nonceRes, err := callRPC(rpcURL, "onxc_getTransactionCount", []string{crypto.AddressToHex(wallet.Address)})
	if err != nil {
		fmt.Printf("Error querying account nonce: %v\n", err)
		return
	}
	var nonceData map[string]interface{}
	_ = json.Unmarshal(nonceRes, &nonceData)
	var nonce uint64
	if v, ok := nonceData["nonce"]; ok {
		switch n := v.(type) {
		case float64:
			nonce = uint64(n)
		case uint64:
			nonce = n
		}
	}

	// 2. Fetch current gas price / base fee from node
	gasRes, err := callRPC(rpcURL, "onxc_gasPrice", nil)
	gasPrice := uint64(core.MinGasPrice + 10)
	if err == nil {
		var gasData map[string]interface{}
		_ = json.Unmarshal(gasRes, &gasData)
		if v, ok := gasData["base_fee"]; ok {
			var bf uint64
			switch n := v.(type) {
			case float64:
				bf = uint64(n)
			case uint64:
				bf = n
			}
			if bf >= core.MinGasPrice {
				gasPrice = bf + 5 // base fee + priority tip
			}
		}
	}

	// 3. Construct and sign transaction
	tx := core.NewTransfer(wallet.Address, toAddr, wallet.PublicKey, nonce, amountNano, gasPrice)
	if err := tx.Sign(wallet.PrivateKey); err != nil {
		fmt.Printf("Error signing transaction with ML-DSA-65: %v\n", err)
		return
	}

	// 4. Gob encode transaction
	encoded, err := rpc.GobEncodeTx(tx)
	if err != nil {
		fmt.Printf("Error encoding transaction: %v\n", err)
		return
	}

	// 5. Submit via onxc_sendRawTransaction
	sendRes, err := callRPC(rpcURL, "onxc_sendRawTransaction", []string{hex.EncodeToString(encoded)})
	if err != nil {
		fmt.Printf("Transaction rejected by network: %v\n", err)
		return
	}

	var sendData map[string]string
	_ = json.Unmarshal(sendRes, &sendData)

	fmt.Println("================================================================================")
	fmt.Println("                  OENEXA Transaction Broadcast Succeeded!                       ")
	fmt.Println("================================================================================")
	fmt.Printf("Transaction Hash: %s\n", sendData["tx_hash"])
	fmt.Printf("From            : %s\n", crypto.AddressToHex(wallet.Address))
	fmt.Printf("To              : %s\n", crypto.AddressToHex(toAddr))
	fmt.Printf("Amount          : %.9f ONXC (%d nano-ONXC)\n", amountONXC, amountNano)
	fmt.Printf("Nonce           : %d\n", nonce)
	fmt.Printf("Gas Price       : %d nano-ONXC/gas\n", gasPrice)
	fmt.Println("================================================================================")
	fmt.Println("Transaction is now in the mempool and gossiping across P2P validator mesh.")
}

func callRPC(rawURL, method string, params interface{}) (json.RawMessage, error) {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid RPC URL %q", rawURL)
	}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})

	client := &http.Client{Timeout: 15 * time.Second}
	// #nosec G107 -- CLI operator-specified RPC URL validated for http/https scheme
	resp, err := client.Post(u.String(), "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("node unreachable at %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("invalid response from node: %w", err)
	}

	if r.Error != nil {
		return nil, fmt.Errorf("[%d] %s", r.Error.Code, r.Error.Message)
	}

	return r.Result, nil
}
