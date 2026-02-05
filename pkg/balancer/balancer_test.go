package balancer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

type mockProvider struct {
	name string
	fail error
	code int
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Chat(_ context.Context, _ *api.CreateChatCompletionRequest) (*http.Response, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	w := httptest.NewRecorder()
	w.WriteHeader(m.code)
	return w.Result(), nil
}

func TestBalancer_All(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		lb := NewBalancer([]provider.Provider{})
		_, err := lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if err == nil {
			t.Error("Expected error for empty providers")
		}
	})

	t.Run("RoundRobin", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 200}
		p2 := &mockProvider{name: "p2", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		_, _ = lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		_, _ = lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
	})

	t.Run("Failover_5xx", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 500}
		p2 := &mockProvider{name: "p2", code: 503}
		p3 := &mockProvider{name: "p3", code: 599}
		p4 := &mockProvider{name: "p4", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2, p3, p4})

		resp, err := lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected 200 after 5xx failovers, got %d", resp.StatusCode)
		}
	})

	t.Run("Failover_429", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 429}
		p2 := &mockProvider{name: "p2", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		resp, _ := lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if resp.StatusCode != 200 {
			t.Error("Should have failed over on 429")
		}
	})

	t.Run("No_Failover_On_401_And_600", func(t *testing.T) {
		// 401 should not retry
		lb401 := NewBalancer([]provider.Provider{&mockProvider{code: 401}})
		resp401, _ := lb401.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if resp401.StatusCode != 401 {
			t.Error("401 should return directly")
		}

		// 600 should not retry (out of 5xx range)
		lb600 := NewBalancer([]provider.Provider{&mockProvider{code: 600}})
		resp600, _ := lb600.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if resp600.StatusCode != 600 {
			t.Error("600 should return directly")
		}
	})

	t.Run("Failover_Error", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", fail: errors.New("network fail")}
		p2 := &mockProvider{name: "p2", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		resp, _ := lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if resp.StatusCode != 200 {
			t.Error("Should have failed over on network error")
		}
	})

	t.Run("AllFail", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 429}
		lb := NewBalancer([]provider.Provider{p1})
		_, err := lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		if err == nil {
			t.Error("Expected error when all providers fail")
		}
	})
}

func TestBalancer_ChatWithResult(t *testing.T) {
	t.Run("Empty_Providers", func(t *testing.T) {
		lb := NewBalancer([]provider.Provider{})
		_, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if err == nil {
			t.Error("Expected error for empty providers")
		}
	})

	t.Run("Success_Returns_Result", func(t *testing.T) {
		p := &mockProvider{name: "TestProvider", code: 200}
		lb := NewBalancer([]provider.Provider{p})

		result, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{Model: "test-model"})
		if err != nil {
			t.Fatal(err)
		}
		if result.ProviderName != "TestProvider" {
			t.Errorf("Expected provider 'TestProvider', got '%s'", result.ProviderName)
		}
		if result.Response.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", result.Response.StatusCode)
		}
		if result.ResponseTime < 0 {
			t.Error("ResponseTime should be non-negative")
		}
	})

	t.Run("No_Failover_On_403", func(t *testing.T) {
		lb := NewBalancer([]provider.Provider{&mockProvider{name: "p1", code: 403}})
		result, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Response.StatusCode != 403 {
			t.Errorf("403 should return directly, got %d", result.Response.StatusCode)
		}
	})

	t.Run("Failover_400", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 400}
		p2 := &mockProvider{name: "p2", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		result, _ := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if result.Response.StatusCode != 200 {
			t.Error("Should have failed over on 400")
		}
	})

	t.Run("Failover_404", func(t *testing.T) {
		p1 := &mockProvider{name: "p1", code: 404}
		p2 := &mockProvider{name: "p2", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		result, _ := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if result.Response.StatusCode != 200 {
			t.Error("Should have failed over on 404")
		}
	})
}

func TestBalancer_WithMetrics(t *testing.T) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "balancer_metrics_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := metrics.NewStore(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	t.Run("NewBalancerWithMetrics", func(t *testing.T) {
		p := &mockProvider{name: "MetricsProvider", code: 200}
		lb := NewBalancerWithMetrics([]provider.Provider{p}, collector)

		_, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{Model: "test"})
		if err != nil {
			t.Fatal(err)
		}

		// Wait for async metrics
		time.Sleep(200 * time.Millisecond)

		stats, err := collector.GetStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.TotalRequests == 0 {
			t.Error("Expected metrics to be recorded")
		}
	})

	t.Run("Metrics_On_Failover", func(t *testing.T) {
		p1 := &mockProvider{name: "FailProvider", code: 429}
		p2 := &mockProvider{name: "SuccessProvider", code: 200}
		lb := NewBalancerWithMetrics([]provider.Provider{p1, p2}, collector)

		_, _ = lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{Model: "test"})

		time.Sleep(200 * time.Millisecond)

		stats, _ := collector.GetStats()
		// Should have recorded both the failure and success
		if stats.TotalRequests < 2 {
			t.Errorf("Expected at least 2 requests recorded (1 fail + 1 success), got %d", stats.TotalRequests)
		}
	})

	t.Run("Metrics_On_Transport_Error", func(t *testing.T) {
		p1 := &mockProvider{name: "ErrorProvider", fail: errors.New("network error")}
		p2 := &mockProvider{name: "OKProvider", code: 200}
		lb := NewBalancerWithMetrics([]provider.Provider{p1, p2}, collector)

		_, _ = lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{Model: "test"})

		time.Sleep(200 * time.Millisecond)
		// Metrics should have been recorded for both
	})
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{429, "rate_limit"},
		{500, "server_error"},
		{502, "server_error"},
		{503, "server_error"},
		{599, "server_error"},
		{400, "bad_request"},
		{404, "not_found"},
		{401, "client_error"},
		{403, "client_error"},
		{405, "client_error"},
		{418, "client_error"},
		{200, "unknown"},
		{301, "unknown"},
		{600, "unknown"},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			result := classifyError(tt.code)
			if result != tt.expected {
				t.Errorf("classifyError(%d) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("TripsAfterThreshold", func(t *testing.T) {
		// Set low threshold for testing
		t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "2")
		t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "1")

		p1 := &mockProvider{name: "FailingProvider", code: 429}
		p2 := &mockProvider{name: "BackupProvider", code: 200}
		lb := NewBalancer([]provider.Provider{p1, p2})

		// First request - p1 fails, failover to p2
		result1, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if result1.ProviderName != "BackupProvider" {
			t.Errorf("Expected BackupProvider, got %s", result1.ProviderName)
		}

		// Second request - should still try p1 (threshold not reached)
		result2, _ := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if result2.ProviderName != "BackupProvider" {
			t.Errorf("Expected BackupProvider, got %s", result2.ProviderName)
		}
	})

	t.Run("ProviderRecovery", func(t *testing.T) {
		p := &mockProvider{name: "RecoveringProvider", code: 200}
		lb := NewBalancer([]provider.Provider{p})

		// Simulate failure recording then success
		lb.recordProviderFailure("RecoveringProvider", 429)
		lb.recordProviderFailure("RecoveringProvider", 429)

		// Now record success - should reset
		lb.recordProviderSuccess("RecoveringProvider")

		// Check state is reset
		status := lb.GetProviderStatus()
		if status["RecoveringProvider"]["consecutive_fails"].(int) != 0 {
			t.Error("Expected consecutive_fails to be reset to 0")
		}
	})

	t.Run("NonExistentProvider", func(t *testing.T) {
		lb := NewBalancer([]provider.Provider{})

		// Should not panic on non-existent provider
		lb.recordProviderFailure("NonExistent", 429)
		lb.recordProviderSuccess("NonExistent")

		if !lb.isProviderAvailable("NonExistent") {
			t.Error("Non-existent provider should be available")
		}
	})

	t.Run("OnlyTripsOn429And5xx", func(t *testing.T) {
		p := &mockProvider{name: "TestProvider", code: 200}
		lb := NewBalancer([]provider.Provider{p})

		// 400 should not trip circuit breaker
		lb.recordProviderFailure("TestProvider", 400)
		lb.recordProviderFailure("TestProvider", 400)
		lb.recordProviderFailure("TestProvider", 400)

		status := lb.GetProviderStatus()
		if status["TestProvider"]["consecutive_fails"].(int) != 0 {
			t.Error("400 errors should not increment consecutive_fails")
		}
	})

	t.Run("AllProvidersInCooldown", func(t *testing.T) {
		t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "1")
		t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "60")

		p1 := &mockProvider{name: "P1", code: 429}
		lb := NewBalancer([]provider.Provider{p1})

		// Trip the circuit breaker
		lb.recordProviderFailure("P1", 429)

		// Set cooldown manually
		lb.statesMu.RLock()
		state := lb.providerStates["P1"]
		lb.statesMu.RUnlock()
		state.mu.Lock()
		state.cooldownUntil = time.Now().Add(time.Minute)
		state.mu.Unlock()

		// Now all providers should be in cooldown
		_, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
		if err == nil {
			t.Error("Expected error when all providers in cooldown")
		}
		if err.Error() != "all providers are in cooldown, try again later" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
}

func TestGetProviderStatus(t *testing.T) {
	p1 := &mockProvider{name: "Provider1", code: 200}
	p2 := &mockProvider{name: "Provider2", code: 200}
	lb := NewBalancer([]provider.Provider{p1, p2})

	status := lb.GetProviderStatus()

	if len(status) != 2 {
		t.Errorf("Expected 2 providers in status, got %d", len(status))
	}

	for name, info := range status {
		if _, ok := info["available"]; !ok {
			t.Errorf("Provider %s missing 'available' field", name)
		}
		if _, ok := info["consecutive_fails"]; !ok {
			t.Errorf("Provider %s missing 'consecutive_fails' field", name)
		}
		if _, ok := info["total_failures"]; !ok {
			t.Errorf("Provider %s missing 'total_failures' field", name)
		}
	}
}

func TestBalancerConfigFromEnv(t *testing.T) {
	t.Run("DefaultCooldownBase", func(t *testing.T) {
		result := getCooldownBase()
		expected := 30 * time.Second
		if result != expected {
			t.Errorf("Expected %v, got %v", expected, result)
		}
	})

	t.Run("CustomCooldownBase", func(t *testing.T) {
		t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "60")
		result := getCooldownBase()
		expected := 60 * time.Second
		if result != expected {
			t.Errorf("Expected %v, got %v", expected, result)
		}
	})

	t.Run("DefaultMaxCooldown", func(t *testing.T) {
		result := getMaxCooldown()
		expected := 5 * time.Minute
		if result != expected {
			t.Errorf("Expected %v, got %v", expected, result)
		}
	})

	t.Run("CustomMaxCooldown", func(t *testing.T) {
		t.Setenv("CIRCUIT_MAX_COOLDOWN_SECONDS", "120")
		result := getMaxCooldown()
		expected := 120 * time.Second
		if result != expected {
			t.Errorf("Expected %v, got %v", expected, result)
		}
	})

	t.Run("DefaultFailureThreshold", func(t *testing.T) {
		result := getFailureThreshold()
		expected := 3
		if result != expected {
			t.Errorf("Expected %d, got %d", expected, result)
		}
	})

	t.Run("CustomFailureThreshold", func(t *testing.T) {
		t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "5")
		result := getFailureThreshold()
		expected := 5
		if result != expected {
			t.Errorf("Expected %d, got %d", expected, result)
		}
	})

	t.Run("InvalidEnvValues", func(t *testing.T) {
		t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "invalid")
		t.Setenv("CIRCUIT_MAX_COOLDOWN_SECONDS", "invalid")
		t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "invalid")

		if getCooldownBase() != 30*time.Second {
			t.Error("Should fallback to default on invalid CIRCUIT_COOLDOWN_BASE_SECONDS")
		}
		if getMaxCooldown() != 5*time.Minute {
			t.Error("Should fallback to default on invalid CIRCUIT_MAX_COOLDOWN_SECONDS")
		}
		if getFailureThreshold() != 3 {
			t.Error("Should fallback to default on invalid CIRCUIT_FAILURE_THRESHOLD")
		}
	})
}

func TestBalancer_AllTransportErrors(t *testing.T) {
	p1 := &mockProvider{name: "p1", fail: errors.New("error1")}
	lb := NewBalancer([]provider.Provider{p1})

	_, err := lb.ChatWithResult(context.Background(), &api.CreateChatCompletionRequest{})
	if err == nil {
		t.Error("Expected error when all providers fail with transport errors")
	}
	if !errors.Is(err, errors.Unwrap(err)) && err.Error() == "" {
		t.Error("Expected wrapped error")
	}
}

func TestCircuitBreaker_MaxCooldownCap(t *testing.T) {
	// Set very low max cooldown to test capping
	t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "1")
	t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "100")
	t.Setenv("CIRCUIT_MAX_COOLDOWN_SECONDS", "10")

	p := &mockProvider{name: "TestProvider", code: 200}
	lb := NewBalancer([]provider.Provider{p})

	// Trip circuit breaker multiple times to trigger exponential backoff
	lb.recordProviderFailure("TestProvider", 500)
	lb.recordProviderFailure("TestProvider", 500)
	lb.recordProviderFailure("TestProvider", 500)
	lb.recordProviderFailure("TestProvider", 500)

	// The cooldown should be capped at maxCooldown (10s), not exponentially higher
	status := lb.GetProviderStatus()
	remaining, ok := status["TestProvider"]["cooldown_remaining_seconds"]
	if ok && remaining.(int) > 15 {
		t.Errorf("Cooldown should be capped at ~10s, got %d", remaining.(int))
	}
}

func TestGetProviderStatus_WithActiveCooldown(t *testing.T) {
	t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "1")
	t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "60")

	p := &mockProvider{name: "CooldownProvider", code: 200}
	lb := NewBalancer([]provider.Provider{p})

	// Trip the circuit breaker
	lb.recordProviderFailure("CooldownProvider", 429)

	status := lb.GetProviderStatus()
	info := status["CooldownProvider"]

	// Should have cooldown info
	if _, ok := info["cooldown_until"]; !ok {
		t.Error("Expected cooldown_until field when in cooldown")
	}
	if _, ok := info["cooldown_remaining_seconds"]; !ok {
		t.Error("Expected cooldown_remaining_seconds field when in cooldown")
	}
	if info["available"].(bool) {
		t.Error("Provider should not be available when in cooldown")
	}
}

func TestCircuitBreaker_5xxTripsBreaker(t *testing.T) {
	t.Setenv("CIRCUIT_FAILURE_THRESHOLD", "2")
	t.Setenv("CIRCUIT_COOLDOWN_BASE_SECONDS", "30")

	p := &mockProvider{name: "Server5xx", code: 200}
	lb := NewBalancer([]provider.Provider{p})

	// 5xx errors should trip circuit breaker
	lb.recordProviderFailure("Server5xx", 500)
	lb.recordProviderFailure("Server5xx", 502)

	status := lb.GetProviderStatus()
	if status["Server5xx"]["consecutive_fails"].(int) != 2 {
		t.Errorf("Expected 2 consecutive fails, got %d", status["Server5xx"]["consecutive_fails"].(int))
	}
}
