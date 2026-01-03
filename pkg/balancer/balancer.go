package balancer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

type Balancer struct {
	providers []provider.Provider
	current   uint64
}

func NewBalancer(providers []provider.Provider) *Balancer {
	return &Balancer{
		providers: providers,
	}
}

func (b *Balancer) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	if len(b.providers) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	numProviders := uint64(len(b.providers))

	for i := uint64(0); i < numProviders; i++ {
		idx := (atomic.AddUint64(&b.current, 1) - 1) % numProviders
		p := b.providers[idx]

		slog.Info("Forwarding request", "component", "balancer", "provider", p.Name())
		resp, err := p.Chat(ctx, req)
		if err != nil {
			slog.Warn("Provider transport error", "component", "balancer", "provider", p.Name(), "error", err)
			continue
		}

		// Failover logic: Retry on 429, 5xx, or 400/404 (model not found/invalid request for THIS provider)
		// Do NOT retry on 401/403 (Unauthorized/Forbidden) as those are permanent credential issues.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && resp.StatusCode < 600 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			slog.Warn("Provider error status (failover)", "component", "balancer", "provider", p.Name(), "status", resp.StatusCode, "body", string(bodyBytes))
			_ = resp.Body.Close()
			continue
		}

		if resp.StatusCode == http.StatusOK {
			slog.Info("Provider success", "component", "balancer", "provider", p.Name())
		} else {
			slog.Info("Provider final response", "component", "balancer", "provider", p.Name(), "status", resp.StatusCode)
		}
		return resp, nil
	}

	return nil, fmt.Errorf("all providers failed or rate limited")
}
