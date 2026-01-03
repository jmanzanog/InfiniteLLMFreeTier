package balancer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
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

		lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
		lb.Chat(context.Background(), &api.CreateChatCompletionRequest{})
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
