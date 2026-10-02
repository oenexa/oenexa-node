package crypto

import (
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/sha3"
)

const HashSize = 32

// Hash256 returns the SHA-3-256 digest of data.
func Hash256(data []byte) [HashSize]byte {
	return sha3.Sum256(data)
}

// HashMany hashes a sequence of byte slices concatenated together.
func HashMany(parts ...[]byte) [HashSize]byte {
	h := sha3.New256()
	for _, p := range parts {
		h.Write(p)
	}
	var out [HashSize]byte
	h.Sum(out[:0])
	return out
}

// ToHex encodes a hash to a lowercase hex string.
func ToHex(h [HashSize]byte) string {
	return hex.EncodeToString(h[:])
}

// ZeroHash is the all-zero hash value.
var ZeroHash [HashSize]byte

// HexToHash decodes a 64-character hex string into a [HashSize]byte.
// Accepts both bare hex strings and 0x-prefixed hex strings.
func HexToHash(s string) ([HashSize]byte, error) {
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return [HashSize]byte{}, err
	}
	if len(b) != HashSize {
		return [HashSize]byte{}, fmt.Errorf("hex hash must be %d bytes, got %d", HashSize, len(b))
	}
	var out [HashSize]byte
	copy(out[:], b)
	return out, nil
}

// HexToAddress decodes a hex string into an [AddressSize]byte.
// Accepts bare hex strings, 0x-prefixed, or OEN-prefixed strings,
// supporting both 32-byte native addresses and 20-byte legacy/EVM addresses.
func HexToAddress(s string) ([AddressSize]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "oen") {
		s = s[3:]
	} else if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return [AddressSize]byte{}, err
	}
	var out [AddressSize]byte
	if len(b) == AddressSize {
		copy(out[:], b)
		return out, nil
	}
	if len(b) == 20 {
		// Align 20-byte address to the lower 20 bytes of the 32-byte array
		copy(out[AddressSize-20:], b)
		return out, nil
	}
	return [AddressSize]byte{}, fmt.Errorf("hex address must be 20 or %d bytes, got %d", AddressSize, len(b))
}

// AddressToHex encodes an address to a lowercase hex string.
func AddressToHex(addr [AddressSize]byte) string {
	return hex.EncodeToString(addr[:])
}
