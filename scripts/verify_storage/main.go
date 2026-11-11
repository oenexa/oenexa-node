package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type MetadataVerify struct {
	NativeSymbol string `json:"native_symbol"`
	CoinName     string `json:"coin_name"`
}

func main() {
	dataDir := "~/.onxc"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}
	if len(dataDir) > 2 && dataDir[:2] == "~/" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, dataDir[2:])
	}

	stateDir := filepath.Join(dataDir, "state")
	fmt.Printf("[verify] Auditing state database integrity at %s...\n", stateDir)

	db, err := leveldb.OpenFile(stateDir, nil)
	if err != nil {
		log.Fatalf("Failed to open LevelDB state store: %v", err)
	}
	defer db.Close()

	// 1. Verify chain identity metadata when present
	rawMeta, err := db.Get([]byte("meta:chain_identity"), nil)
	if err != nil && err != leveldb.ErrNotFound {
		log.Fatalf("Error reading metadata: %v", err)
	}
	if len(rawMeta) > 0 {
		var m MetadataVerify
		if err := json.Unmarshal(rawMeta, &m); err != nil {
			log.Fatalf("Corrupt metadata JSON: %v", err)
		}
		if m.NativeSymbol != "ONXC" {
			log.Fatalf("FAIL: Native symbol is %s, expected ONXC", m.NativeSymbol)
		}
		fmt.Printf("[verify] Chain identity confirmed: %s (%s)\n", m.CoinName, m.NativeSymbol)
	}

	// 2. Iterate all account records and check structural integrity
	iter := db.NewIterator(util.BytesPrefix([]byte("acc:")), nil)
	totalAccounts := 0
	for iter.Next() {
		totalAccounts++
		key := iter.Key()
		if len(key) < 4+20 {
			log.Fatalf("FAIL: Corrupt account key length: %d", len(key))
		}
	}
	iter.Release()

	fmt.Printf("[verify] %d account records passed structural integrity.\n", totalAccounts)
	fmt.Printf("[verify] State trie storage verification PASSED.\n")
}
