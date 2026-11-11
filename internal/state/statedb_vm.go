package state

import (
	"encoding/binary"
	"errors"
	"math/bits"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/mpt"
)

// ─────────────────────────────────────────────────────────────────────────────
// VM StateAccessor implementation
// ─────────────────────────────────────────────────────────────────────────────

// GetStorage retrieves a 64-bit value from a contract's storage slot.
// Read path: Dirty cache -> Persistent SMT
func (s *DB) GetStorage(addr [crypto.AddressSize]byte, slot uint32) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Dirty cache hit
	if slots, ok := s.dirtyStorage[addr]; ok {
		if val, ok := slots[slot]; ok {
			return val
		}
	}

	// 2. Persistent trie lookup
	var slotBytes [4]byte
	binary.BigEndian.PutUint32(slotBytes[:], slot)

	storageKey := crypto.HashMany(addr[:], slotBytes[:])
	valHash, err := s.trie.Get(storageKey)
	if err != nil || valHash == crypto.ZeroHash {
		return 0
	}

	// Decode 64-bit big-endian from the last 8 bytes of valHash
	return binary.BigEndian.Uint64(valHash[24:32])
}

// SetStorage sets a 64-bit value in a contract's storage slot.
func (s *DB) SetStorage(addr [crypto.AddressSize]byte, slot uint32, value uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slots, ok := s.dirtyStorage[addr]
	if !ok {
		slots = make(map[uint32]uint64)
		s.dirtyStorage[addr] = slots
	}
	slots[slot] = value
}

func (s *DB) loadAccountLocked(addr [crypto.AddressSize]byte) *Account {
	if a, ok := s.dirty[addr]; ok {
		return a.Clone()
	}
	valHash, err := s.trie.Get(addr)
	if err != nil || valHash == mpt.ZeroHashOf() {
		return &Account{}
	}
	backend := s.trie.BackendRef()
	if backend != nil {
		flatKey := append([]byte("acct:"), valHash[:]...)
		if data, err := backend.Get(flatKey); err == nil && len(data) > 0 {
			return DeserializeAccount(data)
		}
	}
	return &Account{}
}

// Transfer moves ONXC between two accounts atomically during VM execution.
func (s *DB) Transfer(from, to [crypto.AddressSize]byte, amount uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sender := s.loadAccountLocked(from)
	if sender.Balance < amount {
		return errors.New("insufficient balance for transfer")
	}
	if from == to {
		return nil
	}

	receiver := s.loadAccountLocked(to)
	newBal, carry := bits.Add64(receiver.Balance, amount, 0)
	if carry != 0 {
		return errors.New("integer overflow in receiver balance")
	}
	sender.Balance -= amount
	receiver.Balance = newBal

	s.dirty[from] = sender
	s.dirty[to] = receiver
	return nil
}
