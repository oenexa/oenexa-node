package crypto

import (
	"bytes"
	"testing"
)

func TestSecurity_WalletRecoveryAndZeroize(t *testing.T) {
	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	privCopy := append([]byte(nil), w.PrivateKey...)
	recovered, err := WalletFromPrivateKey(privCopy)
	if err != nil {
		t.Fatalf("WalletFromPrivateKey: %v", err)
	}
	if recovered.Address != w.Address || !bytes.Equal(recovered.PublicKey, w.PublicKey) {
		t.Fatal("recovered wallet mismatch")
	}

	// Corrupted private key must error
	if _, err := WalletFromPrivateKey([]byte("short-key")); err == nil {
		t.Fatal("expected error for invalid private key length")
	}

	// Zeroize must wipe private key material cleanly
	w.Zeroize()
	for i, b := range w.PrivateKey {
		if b != 0 {
			t.Fatalf("expected byte %d zeroed after Zeroize, got %x", i, b)
		}
	}
}

func TestSecurity_HexDecodingValidation(t *testing.T) {
	if _, err := HexToHash("not-valid-hex"); err == nil {
		t.Fatal("expected error on invalid hex hash")
	}
	if _, err := HexToHash("abcd"); err == nil {
		t.Fatal("expected error on short hash")
	}
	if _, err := HexToAddress("zzzz"); err == nil {
		t.Fatal("expected error on invalid hex address")
	}
	if _, err := HexToAddress("0x1234"); err == nil {
		t.Fatal("expected error on short address")
	}
}
