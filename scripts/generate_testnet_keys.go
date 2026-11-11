package main

import (
	"crypto/ed25519"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/keystore"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: go run generate_testnet_keys.go <num_nodes> <out_dir>")
	}
	numNodes, err := strconv.Atoi(os.Args[1])
	if err != nil || numNodes <= 0 {
		log.Fatal("invalid num_nodes: must be a positive integer")
	}
	outDir := filepath.Clean(os.Args[2])

	password := "testnet-password"
	fmt.Printf("Generating %d nodes...\n", numNodes)

	var addresses []string
	var peerIDs []string

	for i := 0; i < numNodes; i++ {
		w, err := crypto.NewWallet()
		if err != nil {
			log.Fatalf("failed to generate wallet: %v", err)
		}
		
		addrHex := crypto.ToHex(w.Address)
		addresses = append(addresses, addrHex)
		
		// Derive Peer ID
		seed := crypto.Hash256(w.PrivateKey)
		edKey := ed25519.NewKeyFromSeed(seed[:32])
		privKey, _ := libp2pCrypto.UnmarshalEd25519PrivateKey(edKey)
		peerID, _ := peer.IDFromPrivateKey(privKey)
		peerIDs = append(peerIDs, peerID.String())

		dir := filepath.Join(outDir, fmt.Sprintf("oenexa-node-%d", i))
		// #nosec G703,G706 -- outDir is a cleaned operator-supplied output directory for key generation
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Fatalf("failed to create directory %q: %v", dir, err) // #nosec G706 -- dir quoted
		}

		err = keystore.Encrypt(filepath.Join(dir, "keystore.json"), password, w)
		if err != nil {
			log.Fatalf("failed to save keystore: %v", err)
		}
		
		fmt.Printf("Node %d: %s (PeerID: %s)\n", i, addrHex, peerID.String())
	}

	fmt.Printf("\n--- config.toml snippets ---\n\n")
	fmt.Printf("validators = [\n")
	for _, addr := range addresses {
		fmt.Printf("  \"%s\",\n", addr)
	}
	fmt.Printf("]\n\n")

	fmt.Printf("bootstrap_peers = [\n")
	for i, pid := range peerIDs {
		fmt.Printf("  \"/dns4/oenexa-node-%d.oenexa-p2p.oenexa-testnet.svc.cluster.local/tcp/8765/p2p/%s\",\n", i, pid)
	}
	fmt.Printf("]\n")
}
