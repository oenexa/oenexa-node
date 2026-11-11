package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oenexa/oenexa/internal/metrics"
	"github.com/oenexa/oenexa/internal/rosetta"
)

const (
	maxRequestBody = 4 << 20 // 4 MiB
	maxBatchSize   = 100
)

type ipBucket struct {
	tokens   float64
	lastSeen time.Time
}

// Server is the JSON-RPC 2.0 and Rosetta HTTP server.
type Server struct {
	api         *API
	http        *http.Server
	mu          sync.Mutex
	rateRPS     float64
	rateBurst   int
	buckets     map[string]*ipBucket
	corsOrigins []string
}

// NewServer creates a JSON-RPC HTTP server that listens on addr.
// readTimeout and writeTimeout default to 30 s if zero.
func NewServer(addr string, api *API, readTimeout, writeTimeout time.Duration) *Server {
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}
	if writeTimeout <= 0 {
		writeTimeout = 30 * time.Second
	}

	mux := http.NewServeMux()
	s := &Server{
		api:       api,
		rateRPS:   10_000,
		rateBurst: 20_000,
		buckets:   make(map[string]*ipBucket),
	}
	mux.HandleFunc("/", s.handleRPC)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/status", s.handleStatus)

	// Mount Coinbase Rosetta v1.4.14 Data & Construction API endpoints
	if api != nil {
		rosettaHandler := rosetta.NewHandler(api.engine, api.st, api.pool, api.peersFn, api.broadcastTxFn)
		rosettaHandler.RegisterRoutes(mux)
	}

	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.withSecurityAndRateLimit(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	return s
}

func (s *Server) withSecurityAndRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path != "/healthz" && !s.allowIP(clientIP(r.RemoteAddr)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetRateLimit configures per-IP token-bucket rate limiting.
// Setting rps <= 0 or burst <= 0 disables rate limiting.
func (s *Server) SetRateLimit(rps float64, burst int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateRPS = rps
	s.rateBurst = burst
	s.buckets = make(map[string]*ipBucket)
}

// SetCORSAllowedOrigins configures allowed CORS origins (empty slice defaults to "*").
func (s *Server) SetCORSAllowedOrigins(origins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.corsOrigins = append([]string(nil), origins...)
}

func (s *Server) allowOriginHeader(origin string) string {
	s.mu.Lock()
	origins := s.corsOrigins
	s.mu.Unlock()
	if len(origins) == 0 {
		return "*"
	}
	for _, allowed := range origins {
		if allowed == "*" {
			return "*"
		}
		if origin != "" && allowed == origin {
			return origin
		}
	}
	return ""
}

func (s *Server) allowIP(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rateRPS <= 0 || s.rateBurst <= 0 {
		return true
	}
	now := time.Now()
	if len(s.buckets) > 10_000 {
		for k, b := range s.buckets {
			if now.Sub(b.lastSeen) > 5*time.Minute {
				delete(s.buckets, k)
			}
		}
	}
	b, ok := s.buckets[ip]
	if !ok {
		b = &ipBucket{tokens: float64(s.rateBurst), lastSeen: now}
		s.buckets[ip] = b
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		b.tokens += elapsed * s.rateRPS
		if maxTokens := float64(s.rateBurst); b.tokens > maxTokens {
			b.tokens = maxTokens
		}
		b.lastSeen = now
	}
	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	return remoteAddr
}

// ServeHTTP implements http.Handler so the Server can be used directly in tests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.http.Handler.ServeHTTP(w, r)
}

// Start begins serving requests in a background goroutine.
// It shuts down gracefully when ctx is canceled.
func (s *Server) Start(ctx context.Context) {
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutCtx)
	}()
	go func() {
		log.Printf("[rpc] JSON-RPC server listening on http://%s", s.http.Addr)
		_ = s.http.ListenAndServe()
	}()
}

// handleRPC handles all inbound JSON-RPC requests (both single and batch).
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if origin := s.allowOriginHeader(r.Header.Get("Origin")); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method isn't allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		writeJSON(w, errResponse(nil, CodeParseError, "read body: "+err.Error()))
		return
	}
	if len(body) > maxRequestBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		writeJSON(w, errResponse(nil, CodeInvalidRequest, "request body too large"))
		return
	}

	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) == 0 {
		writeJSON(w, errResponse(nil, CodeInvalidRequest, "empty request body"))
		return
	}

	// Detect batch vs. single request.
	if trimmed[0] == '[' {
		s.handleBatch(w, body)
		return
	}
	s.handleSingle(w, body)
}

func (s *Server) handleSingle(w http.ResponseWriter, body []byte) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, errResponse(nil, CodeParseError, err.Error()))
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, errResponse(req.ID, CodeInvalidRequest, "jsonrpc must be \"2.0\""))
		return
	}
	metrics.RPCRequests.WithLabelValues(req.Method).Inc()
	resp := s.api.Dispatch(&req)
	if resp.Error != nil {
		metrics.RPCErrors.WithLabelValues(req.Method).Inc()
	}
	writeJSON(w, resp)
}

func (s *Server) handleBatch(w http.ResponseWriter, body []byte) {
	var reqs []json.RawMessage
	if err := json.Unmarshal(body, &reqs); err != nil {
		writeJSON(w, errResponse(nil, CodeParseError, err.Error()))
		return
	}
	if len(reqs) == 0 {
		writeJSON(w, errResponse(nil, CodeInvalidRequest, "empty batch"))
		return
	}
	if len(reqs) > maxBatchSize {
		writeJSON(w, errResponse(nil, CodeInvalidRequest, "batch exceeds maximum allowed size"))
		return
	}

	responses := make([]*Response, 0, len(reqs))
	for _, raw := range reqs {
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			responses = append(responses, errResponse(nil, CodeParseError, err.Error()))
			continue
		}
		if req.JSONRPC != "2.0" {
			responses = append(responses, errResponse(req.ID, CodeInvalidRequest, "jsonrpc must be \"2.0\""))
			continue
		}
		metrics.RPCRequests.WithLabelValues(req.Method).Inc()
		resp := s.api.Dispatch(&req)
		if resp.Error != nil {
			metrics.RPCErrors.WithLabelValues(req.Method).Inc()
		}
		responses = append(responses, resp)
	}
	writeJSON(w, responses)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if origin := s.allowOriginHeader(r.Header.Get("Origin")); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	var height uint64
	var stateRoot string
	var peers int
	var pending int
	ver := "v0.9.0"

	if s.api != nil {
		if s.api.engine != nil {
			height = s.api.engine.Height()
			if height > 0 {
				if blk := s.api.engine.BlockByHeight(height - 1); blk != nil {
					stateRoot = "0x" + hex.EncodeToString(blk.Header.StateRoot[:])
				}
			}
		}
		if s.api.peersFn != nil {
			peers = s.api.peersFn()
		}
		if s.api.pool != nil {
			pending = s.api.pool.Len()
		}
		if s.api.version != "" {
			ver = s.api.version
		}
	}

	resp := map[string]interface{}{
		"version":      ver,
		"network":      "mainnet",
		"chain_height": height,
		"peer_count":   peers,
		"pending_tx":   pending,
		"state_root":   stateRoot,
		"timestamp":    time.Now().UTC(),
		"online":       true,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
