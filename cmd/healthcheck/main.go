package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

func validateHTTPURL(raw string) (string, error) {
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid http(s) URL: %q", raw)
	}
	return u.String(), nil
}

func runHealthcheck(rawURL string) error {
	targetURL, err := validateHTTPURL(rawURL)
	if err != nil {
		return err
	}

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"onxc_blockHeight","params":null}`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// #nosec G107,G704 -- targetURL validated for http/https scheme via validateHTTPURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := http.Client{Timeout: 3 * time.Second}
	// #nosec G704 -- operator-configured RPC healthcheck URL validated above
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("node unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func main() {
	// A tiny healthcheck client to run inside scratch docker containers
	// It pings the JSON-RPC endpoint to ensure the node is responsive.
	rawURL := "http://127.0.0.1:8545"
	if env := os.Getenv("OENEXA_RPC_URL"); env != "" {
		rawURL = env
	} else if env := os.Getenv("RPC_URL"); env != "" {
		rawURL = env
	}
	if len(os.Args) > 1 && os.Args[1] != "" {
		rawURL = os.Args[1]
	}

	if err := runHealthcheck(rawURL); err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}