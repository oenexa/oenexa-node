package oracle

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
)

func TestSecurity_OracleAggregateSybilAndOverflow(t *testing.T) {
	// 1. Nil entries rejected
	if _, err := AggregateReports([]*OracleReport{nil}); err == nil {
		t.Fatal("expected error with nil report")
	}

	// 2. Duplicate reporter Sybil attack rejected cleanly
	honest, _ := crypto.NewWallet()
	sybil, _ := crypto.NewWallet()

	dupReports := []*OracleReport{
		{Reporter: sybil.Address, Price: 9_000_000},
		{Reporter: sybil.Address, Price: 9_000_000}, // duplicate reporter
		{Reporter: honest.Address, Price: 1_000_000},
	}
	if _, err := AggregateReports(dupReports); err == nil {
		t.Fatal("expected error rejecting duplicate oracle reporter Sybil attack")
	}

	// 3. Distinct reporters: even-count median near MaxUint64 must NOT overflow (low+high)
	a, _ := crypto.NewWallet()
	b, _ := crypto.NewWallet()
	highReports := []*OracleReport{
		{Reporter: a.Address, Price: math.MaxUint64 - 10},
		{Reporter: b.Address, Price: math.MaxUint64 - 2},
	}
	got, err := AggregateReports(highReports)
	if err != nil {
		t.Fatalf("high-value AggregateReports: %v", err)
	}
	if got != math.MaxUint64-6 {
		t.Fatalf("overflow in median average: want=%d got=%d", uint64(math.MaxUint64-6), got)
	}
}

func TestSecurity_FetchSovereignPriceEdges(t *testing.T) {
	status := http.StatusOK
	body := `{"oenexa":{"usd":2500000}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	defer ts.Close()

	n := NewNode("oracle-test", 1000, "http://127.0.0.1:8545")
	if _, err := n.FetchSovereignPrice("INVALID-PAIR"); err == nil {
		t.Fatal("expected error for unknown valuation pair")
	}

	// Redirect httpClient transport to local httptest.Server so FetchSovereignPrice never touches external network.
	n.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			clone := req.Clone(req.Context())
			clone.URL.Scheme = "http"
			clone.URL.Host = ts.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(clone)
		}),
	}

	price, err := n.FetchSovereignPrice("ONXC-USD")
	if err != nil || price != 2_500_000 {
		t.Fatalf("expected valid price 2500000, got %d (err=%v)", price, err)
	}

	// HTTP non-200 rejected
	status = http.StatusInternalServerError
	if _, err := n.FetchSovereignPrice("ONXC-USD"); err == nil {
		t.Fatal("expected error on HTTP 500")
	}

	// Negative / overflow price rejected
	status = http.StatusOK
	body = `{"oenexa":{"usd":-1.5}}`
	if _, err := n.FetchSovereignPrice("ONXC-USD"); err == nil {
		t.Fatal("expected error on negative oracle price")
	}

	body = `{"oenexa":{"usd":1e30}}`
	if _, err := n.FetchSovereignPrice("ONXC-USD"); err == nil {
		t.Fatal("expected error on overflowing oracle price")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
