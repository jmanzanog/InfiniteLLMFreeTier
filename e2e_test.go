package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/handlers"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/server"
)

// E2E tests simulate production-like behavior with real SQLite, real collector,
// real balancer, and real HTTP server. Only LLM providers are mocked.

func setupE2EServer(t *testing.T, providers []provider.Provider) (*httptest.Server, *metrics.Collector, string) {
	t.Helper()

	// Create temp SQLite database
	tmpFile, err := os.CreateTemp("", "e2e_metrics_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()

	// Initialize real metrics store
	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create metrics store: %v", err)
	}

	// Initialize real collector with larger buffer for E2E
	collector := metrics.NewCollector(store, 1000)

	// Initialize real balancer with metrics
	lb := balancer.NewBalancerWithMetrics(providers, collector)
	srv := server.NewServer(lb)

	// Create real chi router
	r := chi.NewRouter()

	// Use handlers package (same as production)
	r.Get("/health", handlers.Health)
	statsHandler := handlers.NewStatsHandler(collector)
	r.Get("/stats", statsHandler.JSON)
	r.Get("/stats/web", statsHandler.Web)

	// Mount OpenAI-compatible handler
	strictHandler := api.NewStrictHandler(srv, nil)
	api.HandlerWithOptions(strictHandler, api.ChiServerOptions{
		BaseRouter: r,
		BaseURL:    "/v1",
	})

	ts := httptest.NewServer(r)
	return ts, collector, dbPath
}

func cleanupE2E(t *testing.T, ts *httptest.Server, collector *metrics.Collector, dbPath string) {
	t.Helper()
	ts.Close()
	_ = collector.Close()
	_ = os.Remove(dbPath)
}

// TestE2E_FullFlow tests the complete request flow with metrics collection
func TestE2E_FullFlow(t *testing.T) {
	// Mock LLM provider that returns success
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // Simulate latency
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-mock",
			"object": "chat.completion",
			"created": 1234567890,
			"model": "mock-model",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello from mock!"},
				"finish_reason": "stop"
			}]
		}`))
	}))
	defer mockProvider.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("MockProvider", mockProvider.URL, "test-key", "mock-model"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	t.Run("Health_Endpoint", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("Health request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Chat_Completion_With_Headers", func(t *testing.T) {
		body := `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`
		resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("Chat request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify X-Provider header
		xProvider := resp.Header.Get("X-Provider")
		if xProvider != "MockProvider" {
			t.Errorf("Expected X-Provider 'MockProvider', got '%s'", xProvider)
		}

		// Verify X-Response-Time-Ms header exists and is reasonable
		xResponseTime := resp.Header.Get("X-Response-Time-Ms")
		if xResponseTime == "" {
			t.Error("X-Response-Time-Ms header missing")
		}
	})

	t.Run("Stats_After_Requests", func(t *testing.T) {
		// Make a few more requests
		for i := 0; i < 3; i++ {
			body := `{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}`
			resp, _ := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
			if resp != nil {
				_ = resp.Body.Close()
			}
		}

		// Wait for async metrics to be processed
		time.Sleep(200 * time.Millisecond)

		resp, err := http.Get(ts.URL + "/stats")
		if err != nil {
			t.Fatalf("Stats request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		var stats metrics.GlobalStats
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode stats: %v", err)
		}

		// We made 4 requests total (1 in previous test + 3 here)
		if stats.TotalRequests < 4 {
			t.Errorf("Expected at least 4 requests, got %d", stats.TotalRequests)
		}

		if stats.TotalSuccess < 4 {
			t.Errorf("Expected at least 4 successes, got %d", stats.TotalSuccess)
		}

		// Verify provider stats
		if len(stats.ProviderStats) == 0 {
			t.Error("Expected provider stats")
		}

		// Verify avg response time is reasonable (> 50ms due to mock delay)
		if stats.AvgResponseMs < 50 {
			t.Errorf("Expected avg response time >= 50ms, got %.2f", stats.AvgResponseMs)
		}
	})

	t.Run("Stats_Web_Dashboard", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/stats/web")
		if err != nil {
			t.Fatalf("Stats web request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify Content-Type is HTML
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Expected Content-Type 'text/html', got '%s'", contentType)
		}

		// Read full body
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}
		bodyStr := string(bodyBytes)

		// Check for dashboard title
		if !strings.Contains(bodyStr, "InfiniteLLM Gateway") {
			t.Error("Expected 'InfiniteLLM Gateway' in dashboard HTML")
		}

		// Check for stats elements
		if !strings.Contains(bodyStr, "Total Requests") {
			t.Error("Expected 'Total Requests' in dashboard HTML")
		}
	})

	t.Run("Stats_Web_With_Refresh", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/stats/web?refresh=5")
		if err != nil {
			t.Fatalf("Stats web request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}
		bodyStr := string(bodyBytes)

		// Check for refresh meta tag
		if !strings.Contains(bodyStr, `content="5"`) {
			t.Error("Expected refresh meta tag with content='5'")
		}

		// Check for refresh badge
		if !strings.Contains(bodyStr, "Auto-refresh") {
			t.Error("Expected 'Auto-refresh' badge in HTML")
		}
	})
}

// TestE2E_Failover tests failover behavior with real metrics collection
func TestE2E_Failover(t *testing.T) {
	var callCount int32

	// First provider returns 429 (rate limit)
	mockProvider1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer mockProvider1.Close()

	// Second provider returns success
	mockProvider2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"success"}}]}`))
	}))
	defer mockProvider2.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("RateLimitedProvider", mockProvider1.URL, "key1", "model1"),
		provider.NewCustomProvider("SuccessProvider", mockProvider2.URL, "key2", "model2"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	t.Run("Failover_On_429", func(t *testing.T) {
		body := `{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}`
		resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Should succeed via failover
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 after failover, got %d", resp.StatusCode)
		}

		// Should have hit SuccessProvider after failover
		xProvider := resp.Header.Get("X-Provider")
		if xProvider != "SuccessProvider" {
			t.Errorf("Expected X-Provider 'SuccessProvider', got '%s'", xProvider)
		}
	})

	t.Run("Stats_Shows_Failover", func(t *testing.T) {
		time.Sleep(200 * time.Millisecond) // Wait for async metrics

		resp, err := http.Get(ts.URL + "/stats")
		if err != nil {
			t.Fatalf("Stats request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var stats metrics.GlobalStats
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode stats: %v", err)
		}

		// Find rate limited provider stats
		var rateLimitedStats *metrics.ProviderStats
		for i := range stats.ProviderStats {
			if stats.ProviderStats[i].Provider == "RateLimitedProvider" {
				rateLimitedStats = &stats.ProviderStats[i]
				break
			}
		}

		if rateLimitedStats == nil {
			t.Fatal("RateLimitedProvider stats not found")
		}

		if rateLimitedStats.Error429Count == 0 {
			t.Error("Expected at least 1 rate limit error recorded")
		}

		if rateLimitedStats.FailureCount == 0 {
			t.Error("Expected at least 1 failure recorded")
		}
	})
}

// TestE2E_MultipleProviders_RoundRobin tests round-robin distribution with metrics
func TestE2E_MultipleProviders_RoundRobin(t *testing.T) {
	var provider1Calls, provider2Calls int32

	mockProvider1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&provider1Calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"p1","choices":[{"message":{"content":"from p1"}}]}`))
	}))
	defer mockProvider1.Close()

	mockProvider2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&provider2Calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"p2","choices":[{"message":{"content":"from p2"}}]}`))
	}))
	defer mockProvider2.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("Provider1", mockProvider1.URL, "key1", "model1"),
		provider.NewCustomProvider("Provider2", mockProvider2.URL, "key2", "model2"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	// Make 10 requests
	for i := 0; i < 10; i++ {
		body := `{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}`
		resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	// Both providers should have been called (round-robin)
	p1Calls := atomic.LoadInt32(&provider1Calls)
	p2Calls := atomic.LoadInt32(&provider2Calls)

	if p1Calls == 0 {
		t.Error("Provider1 was never called")
	}
	if p2Calls == 0 {
		t.Error("Provider2 was never called")
	}

	// Distribution should be roughly equal (5 each for 10 requests)
	total := p1Calls + p2Calls
	if total != 10 {
		t.Errorf("Expected 10 total calls, got %d", total)
	}

	// Wait for metrics and verify
	time.Sleep(200 * time.Millisecond)

	resp, _ := http.Get(ts.URL + "/stats")
	defer func() { _ = resp.Body.Close() }()

	var stats metrics.GlobalStats
	_ = json.NewDecoder(resp.Body).Decode(&stats)

	if stats.TotalRequests != 10 {
		t.Errorf("Expected 10 requests in stats, got %d", stats.TotalRequests)
	}

	if len(stats.ProviderStats) != 2 {
		t.Errorf("Expected 2 providers in stats, got %d", len(stats.ProviderStats))
	}
}

// TestE2E_ServerErrors tests handling of 5xx errors with metrics
func TestE2E_ServerErrors(t *testing.T) {
	// Provider that returns 500
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer mockProvider.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("ErrorProvider", mockProvider.URL, "key", "model"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should return 500 since all providers failed
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", resp.StatusCode)
	}

	// Wait and verify stats
	time.Sleep(200 * time.Millisecond)

	statsResp, _ := http.Get(ts.URL + "/stats")
	defer func() { _ = statsResp.Body.Close() }()

	var stats metrics.GlobalStats
	_ = json.NewDecoder(statsResp.Body).Decode(&stats)

	// Find error provider stats
	for _, ps := range stats.ProviderStats {
		if ps.Provider == "ErrorProvider" {
			if ps.Error500Count == 0 {
				t.Error("Expected 5xx error to be recorded")
			}
			if ps.FailureCount == 0 {
				t.Error("Expected failure to be recorded")
			}
		}
	}
}

// TestE2E_ResponseTimeTracking tests accurate response time measurement
func TestE2E_ResponseTimeTracking(t *testing.T) {
	const mockDelay = 100 * time.Millisecond

	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(mockDelay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"delayed"}}]}`))
	}))
	defer mockProvider.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("SlowProvider", mockProvider.URL, "key", "model"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	// Make request and time it
	start := time.Now()
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(start)

	// Response should have taken at least mockDelay
	if elapsed < mockDelay {
		t.Errorf("Expected response time >= %v, got %v", mockDelay, elapsed)
	}

	// X-Response-Time-Ms should reflect the delay
	xResponseTime := resp.Header.Get("X-Response-Time-Ms")
	if xResponseTime == "" {
		t.Fatal("X-Response-Time-Ms header missing")
	}

	// Wait and check stats
	time.Sleep(200 * time.Millisecond)

	statsResp, _ := http.Get(ts.URL + "/stats")
	defer func() { _ = statsResp.Body.Close() }()

	var stats metrics.GlobalStats
	_ = json.NewDecoder(statsResp.Body).Decode(&stats)

	// Avg response time should be at least mockDelay
	if stats.AvgResponseMs < float64(mockDelay.Milliseconds()) {
		t.Errorf("Expected avg response time >= %dms, got %.2fms", mockDelay.Milliseconds(), stats.AvgResponseMs)
	}
}

// TestE2E_ConcurrentRequests tests thread safety under concurrent load
func TestE2E_ConcurrentRequests(t *testing.T) {
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer mockProvider.Close()

	providers := []provider.Provider{
		provider.NewCustomProvider("ConcurrentProvider", mockProvider.URL, "key", "model"),
	}

	ts, collector, dbPath := setupE2EServer(t, providers)
	defer cleanupE2E(t, ts, collector, dbPath)

	const numRequests = 50
	done := make(chan bool, numRequests)

	// Make concurrent requests
	for i := 0; i < numRequests; i++ {
		go func() {
			body := `{"model":"gpt-4","messages":[{"role":"user","content":"Concurrent test"}]}`
			resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
			if err == nil {
				_ = resp.Body.Close()
			}
			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		<-done
	}

	// Wait for async metrics (SQLite writes are sequential, need time for 50 requests)
	time.Sleep(1 * time.Second)

	resp, _ := http.Get(ts.URL + "/stats")
	defer func() { _ = resp.Body.Close() }()

	var stats metrics.GlobalStats
	_ = json.NewDecoder(resp.Body).Decode(&stats)

	if stats.TotalRequests != numRequests {
		t.Errorf("Expected %d requests in stats, got %d", numRequests, stats.TotalRequests)
	}

	if stats.TotalSuccess != numRequests {
		t.Errorf("Expected %d successes in stats, got %d", numRequests, stats.TotalSuccess)
	}
}
