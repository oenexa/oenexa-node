package test

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/oenexa/oenexa/internal/core"
	"github.com/oenexa/oenexa/internal/crypto"
)

// FuzzTransactionGobDecode ensures raw byte inputs to the transaction decoder
// can never trigger memory corruption or unhandled panics.
func FuzzTransactionGobDecode(f *testing.F) {
	// Seed corpus with a valid transaction
	w, err := crypto.NewWallet()
	if err == nil {
		tx := core.NewTransfer(w.Address, w.Address, w.PublicKey, 1, 1000, core.MinGasPrice)
		_ = tx.Sign(w.PrivateKey)
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(tx); err == nil {
			f.Add(buf.Bytes())
		}
	}
	f.Add([]byte{})
	f.Add([]byte("random invalid bytes"))
	f.Add(make([]byte, 1024))

	f.Fuzz(func(t *testing.T, data []byte) {
		var tx core.Transaction
		err := gob.NewDecoder(bytes.NewReader(data)).Decode(&tx)
		if err == nil {
			// If decoding succeeded, BasicValidate must not panic
			_ = tx.BasicValidate()
			_ = tx.ComputeHash()
		}
	})
}

// FuzzHexToAddress validates that hex string address parsing handles arbitrary strings safely.
func FuzzHexToAddress(f *testing.F) {
	f.Add("0000000000000000000000000000000000000000000000000000000000000001")
	f.Add("0x0000000000000000000000000000000000000000000000000000000000000001")
	f.Add("")
	f.Add("xyz")
	f.Add(strings.Repeat("a", 64))
	f.Add(strings.Repeat("f", 128))

	f.Fuzz(func(t *testing.T, s string) {
		addr, err := crypto.HexToAddress(s)
		if err == nil {
			hexStr := crypto.AddressToHex(addr)
			if len(hexStr) != 64 {
				t.Fatalf("unexpected hex length: %d", len(hexStr))
			}
		}
	})
}

// FuzzHexToHash validates hash string parsing safety.
func FuzzHexToHash(f *testing.F) {
	f.Add("4a5aee77954b17a3a970e5ff30e5da48ec42f36f6d0f666f286b24a73229b46e")
	f.Add("0x4a5aee77954b17a3a970e5ff30e5da48ec42f36f6d0f666f286b24a73229b46e")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, s string) {
		h, err := crypto.HexToHash(s)
		if err == nil {
			out := crypto.ToHex(h)
			if len(out) != 64 {
				t.Fatalf("unexpected hash hex length: %d", len(out))
			}
		}
	})
}

// FuzzRosettaPublicKeyDerive verifies that address derivation from raw public keys never crashes.
func FuzzRosettaPublicKeyDerive(f *testing.F) {
	w, err := crypto.NewWallet()
	if err == nil {
		f.Add(hex.EncodeToString(w.PublicKey))
	}
	f.Add("")
	f.Add("00")
	f.Add("deadbeef")

	f.Fuzz(func(t *testing.T, hexStr string) {
		clean := strings.TrimPrefix(hexStr, "0x")
		pubBytes, err := hex.DecodeString(clean)
		if err == nil && len(pubBytes) == crypto.PublicKeySize {
			addr := crypto.DeriveAddress(pubBytes)
			_ = crypto.ToHex(addr)
		}
	})
}
