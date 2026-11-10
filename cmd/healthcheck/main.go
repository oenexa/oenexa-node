package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	// A tiny healthcheck client to run inside scratch docker containers
	// It pings the JSON-RPC endpoint to ensure the node is responsive.

	url := "http://127.0.0.1:8545"
	if env := os.Getenv("OENEXA_RPC_URL"); env != "" {
		url = env
	} else if env := os.Getenv("RPC_URL"); env != "" {
		url = env
	}
	if len(os.Args) > 1 && os.Args[1] != "" {
		url = os.Args[1]
	}

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"oen_blockHeight","params":null}`)

	client := http.Client{
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: request creation failed: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: node unreachable: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: unexpected status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	// If it returns 200 OK, the node is up and healthy.
	os.Exit(0)
}