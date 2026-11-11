package multisig

import (
	"encoding/binary"
	"testing"
)

func TestSecurity_MultisigDuplicateOwnerReplayBlocked(t *testing.T) {
	defer resetMocks()

	store := make(map[string][]byte)
	WriteState = func(key, value []byte) {
		store[string(key)] = append([]byte(nil), value...)
	}
	ReadState = func(key []byte, _ uint32) ([]byte, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, nil
		}
		return v, nil
	}
	// Only signatures matching owner0's pubkey succeed (simulating valid sig by owner0).
	VerifySig = func(pk, _, sig []byte) bool {
		return len(pk) == 1952 && pk[0] == 0xAA && len(sig) == 3309 && sig[0] == 0x01
	}
	transferred := false
	DoTransfer = func(_ []byte, _ uint64) bool {
		transferred = true
		return true
	}

	// Setup 2-of-2 multisig with owner 0 and owner 1.
	owner0 := make([]byte, 1952)
	owner0[0] = 0xAA
	owner1 := make([]byte, 1952)
	owner1[0] = 0xBB

	initPayload := make([]byte, 8+2*1952)
	binary.BigEndian.PutUint32(initPayload[0:4], 2)
	binary.BigEndian.PutUint32(initPayload[4:8], 2)
	copy(initPayload[8:], owner0)
	copy(initPayload[8+1952:], owner1)

	PtrToBytes = func(_, _ uint32) []byte { return initPayload }
	if res := InitMultisig(0, uint32(len(initPayload))); res != 0 {
		t.Fatalf("InitMultisig expected 0, got %d", res)
	}

	// Attacker replays owner0's signature twice in [sig0, sig1] to meet threshold=2.
	execPayload := make([]byte, 44+2*3309)
	binary.BigEndian.PutUint64(execPayload[32:40], 100)
	binary.BigEndian.PutUint32(execPayload[40:44], 2) // numSigs = 2
	execPayload[44] = 0x01
	execPayload[44+3309] = 0x01

	PtrToBytes = func(_, _ uint32) []byte { return execPayload }
	if res := ExecuteTransfer(0, uint32(len(execPayload))); res != -6 {
		t.Fatalf("expected duplicate-owner replay rejection (-6), got %d", res)
	}
	if transferred {
		t.Fatal("duplicate-owner replay executed transfer!")
	}
}

func TestSecurity_MultisigInitGuardsAndDistinctKeys(t *testing.T) {
	defer resetMocks()

	store := make(map[string][]byte)
	WriteState = func(key, value []byte) {
		store[string(key)] = append([]byte(nil), value...)
	}
	ReadState = func(key []byte, _ uint32) ([]byte, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, nil
		}
		return v, nil
	}

	// Verify ownerKey(256) != ownerKey(0) (byte truncation regression check).
	if string(ownerKey(256)) == string(ownerKey(0)) {
		t.Fatal("ownerKey(256) collided with ownerKey(0)")
	}

	badZeroThresh := make([]byte, 8+1952)
	binary.BigEndian.PutUint32(badZeroThresh[0:4], 0)
	binary.BigEndian.PutUint32(badZeroThresh[4:8], 1)
	PtrToBytes = func(_, _ uint32) []byte { return badZeroThresh }
	if res := InitMultisig(0, uint32(len(badZeroThresh))); res != -1 {
		t.Fatalf("expected threshold=0 rejection (-1), got %d", res)
	}

	badHugeCount := make([]byte, 8)
	binary.BigEndian.PutUint32(badHugeCount[0:4], 1)
	binary.BigEndian.PutUint32(badHugeCount[4:8], 2_000_000) // integer overflow trap before fix
	PtrToBytes = func(_, _ uint32) []byte { return badHugeCount }
	if res := InitMultisig(0, 8); res != -2 {
		t.Fatalf("expected ownerCount overflow rejection (-2), got %d", res)
	}

	// Valid init then re-init must be blocked (-3).
	valid := make([]byte, 8+1952)
	binary.BigEndian.PutUint32(valid[0:4], 1)
	binary.BigEndian.PutUint32(valid[4:8], 1)
	PtrToBytes = func(_, _ uint32) []byte { return valid }
	if res := InitMultisig(0, uint32(len(valid))); res != 0 {
		t.Fatalf("first init failed: %d", res)
	}
	if res := InitMultisig(0, uint32(len(valid))); res != -3 {
		t.Fatalf("expected re-init guard (-3), got %d", res)
	}
}
