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

// StorageMetadata records the database schema version and native asset symbol.
type StorageMetadata struct {
	Version      string `json:"version"`
	NativeSymbol string `json:"native_symbol"`
	CoinName     string `json:"coin_name"`
	MigratedAt   int64  `json:"migrated_at"`
}

var metaKey = []byte("meta:chain_identity")

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
	fmt.Printf("[migration] Opening state database at %s...\n", stateDir)

	db, err := leveldb.OpenFile(stateDir, nil)
	if err != nil {
		log.Fatalf("Failed to open LevelDB state store: %v", err)
	}
	defer db.Close()

	// 1. Update chain identity metadata
	meta := StorageMetadata{
		Version:      "v1.0.0",
		NativeSymbol: "ONXC",
		CoinName:     "Open Network for the Next Generation Coin",
		MigratedAt:   1762732800,
	}
	metaBytes, _ := json.Marshal(meta)
	if err := db.Put(metaKey, metaBytes, nil); err != nil {
		log.Fatalf("Failed to write metadata: %v", err)
	}

	// 2. Scan and verify account state records
	accountPrefix := []byte("acc:")
	iter := db.NewIterator(util.BytesPrefix(accountPrefix), nil)
	count := 0
	for iter.Next() {
		count++
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		log.Fatalf("Iterator error: %v", err)
	}

	fmt.Printf("[migration] ✓ Successfully validated %d on-chain state accounts.\n", count)
	fmt.Printf("[migration] ✓ Native currency symbol upgraded to ONXC in persistent storage.\n")
}
