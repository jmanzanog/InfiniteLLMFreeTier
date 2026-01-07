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
func (m *mockProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
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
		time.Sleep(100 * time.Millisecond)

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

		time.Sleep(100 * time.Millisecond)

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

		time.Sleep(100 * time.Millisecond)
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
