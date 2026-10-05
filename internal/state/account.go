package state

import (
	"encoding/binary"

	"github.com/oenexa/oenexa/internal/crypto"
)

// Account holds the on-chain state for a single address.
type Account struct {
	Nonce       uint64
	Balance     uint64
	CodeHash    [crypto.HashSize]byte // zero for EOA
	StorageRoot [crypto.HashSize]byte // zero for EOA
}

// IsContract returns true if this account has associated code.
func (a *Account) IsContract() bool {
	return a.CodeHash != crypto.ZeroHash
}

// Clone returns a deep copy of the account.
func (a *Account) Clone() *Account {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

// Serialize converts the account into a flat byte slice for disk storage.
func (a *Account) Serialize() []byte {
	buf := make([]byte, 8+8+32+32)
	binary.BigEndian.PutUint64(buf[0:8], a.Nonce)
	binary.BigEndian.PutUint64(buf[8:16], a.Balance)
	copy(buf[16:48], a.CodeHash[:])
	copy(buf[48:80], a.StorageRoot[:])
	return buf
}

// DeserializeAccount parses a byte slice into an Account.
func DeserializeAccount(data []byte) *Account {
	if len(data) < 80 {
		return &Account{}
	}
	acc := &Account{}
	acc.Nonce = binary.BigEndian.Uint64(data[0:8])
	acc.Balance = binary.BigEndian.Uint64(data[8:16])
	copy(acc.CodeHash[:], data[16:48])
	copy(acc.StorageRoot[:], data[48:80])
	return acc
}

