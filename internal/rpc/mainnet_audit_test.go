package rpc

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServer_SecurityHeadersAndCORSAllowlist(t *testing.T) {
	srv := NewServer("127.0.0.1:0", nil, time.Second, time.Second)
	srv.SetCORSAllowedOrigins([]string{"https://wallet.oenexa.io"})

	// Allowed origin gets echoed.
	reqAllowed := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	reqAllowed.RemoteAddr = "10.0.0.1:1234"
	reqAllowed.Header.Set("Origin", "https://wallet.oenexa.io")
	rrAllowed := httptest.NewRecorder()
	srv.ServeHTTP(rrAllowed, reqAllowed)

	if rrAllowed.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options: nosniff")
	}
	if rrAllowed.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing X-Frame-Options: DENY")
	}
	if rrAllowed.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing Referrer-Policy: no-referrer")
	}
	if rrAllowed.Header().Get("Access-Control-Allow-Origin") != "https://wallet.oenexa.io" {
		t.Fatalf("expected allowed origin echoed, got %q", rrAllowed.Header().Get("Access-Control-Allow-Origin"))
	}

	// Disallowed origin receives no ACAO header.
	reqEvil := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	reqEvil.RemoteAddr = "10.0.0.1:1234"
	reqEvil.Header.Set("Origin", "https://evil.example")
	rrEvil := httptest.NewRecorder()
	srv.ServeHTTP(rrEvil, reqEvil)
	if got := rrEvil.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin unexpectedly allowed: %q", got)
	}
}

func TestServer_RateLimitsAllRoutesExemptsHealthz(t *testing.T) {
	api := NewAPI(nil, nil, nil, nil, nil, "v0.9.0")
	srv := NewServer("127.0.0.1:0", api, time.Second, time.Second)
	srv.SetRateLimit(0.1, 2) // allow 2 requests then 429

	// Request 1 & 2 succeed on Rosetta endpoint (/network/list).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/network/list", bytes.NewReader([]byte(`{}`)))
		req.RemoteAddr = "192.168.1.50:5555"
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d unexpectedly rate-limited", i+1)
		}
	}

	// Request 3 hits 429 Too Many Requests on Rosetta endpoint.
	req3 := httptest.NewRequest(http.MethodPost, "/network/list", bytes.NewReader([]byte(`{}`)))
	req3.RemoteAddr = "192.168.1.50:5555"
	rr3 := httptest.NewRecorder()
	srv.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests on Rosetta endpoint, got %d", rr3.Code)
	}

	// /healthz stays exempt from rate limiting so Kubernetes probes stay healthy.
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqHealth.RemoteAddr = "192.168.1.50:5555"
	rrHealth := httptest.NewRecorder()
	srv.ServeHTTP(rrHealth, reqHealth)
	if rrHealth.Code != http.StatusOK || !strings.Contains(rrHealth.Body.String(), "ok") {
		t.Fatalf("expected /healthz 200 ok, got %d %q", rrHealth.Code, rrHealth.Body.String())
	}
}

func TestServer_ClientIPAndOversizedBodyAndBadBatch(t *testing.T) {
	if got := clientIP("[::1]:8545"); got != "::1" {
		t.Fatalf("clientIP ipv6 mismatch: %q", got)
	}
	if got := clientIP("unbracketed"); got != "unbracketed" {
		t.Fatalf("clientIP fallback mismatch: %q", got)
	}

	srv := NewServer("127.0.0.1:0", NewAPI(nil, nil, nil, nil, nil, "v0.9.0"), time.Second, time.Second)
	oversized := bytes.Repeat([]byte("a"), maxRequestBody+10)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversized))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized RPC body, got %d", rr.Code)
	}
}
