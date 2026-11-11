package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateHTTPURL_Security(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:8545",
		"https://node.oenexa.org/rpc",
	}
	for _, u := range valid {
		if _, err := validateHTTPURL(u); err != nil {
			t.Errorf("expected valid URL %q, got error: %v", u, err)
		}
	}

	invalid := []string{
		"",
		"ftp://127.0.0.1:8545",
		"javascript:alert(1)",
		"http://",
		"/relative/path",
	}
	for _, u := range invalid {
		if _, err := validateHTTPURL(u); err == nil {
			t.Errorf("expected invalid URL rejection for %q", u)
		}
	}
}

func TestRunHealthcheck_Responses(t *testing.T) {
	status := http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer ts.Close()

	if err := runHealthcheck(ts.URL); err != nil {
		t.Fatalf("expected nil error on 200 OK, got %v", err)
	}

	status = http.StatusServiceUnavailable
	if err := runHealthcheck(ts.URL); err == nil {
		t.Fatal("expected error on 503 status")
	}

	if err := runHealthcheck("ftp://invalid-scheme"); err == nil {
		t.Fatal("expected error on invalid scheme")
	}
}
