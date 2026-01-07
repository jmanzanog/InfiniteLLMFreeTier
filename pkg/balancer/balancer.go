package balancer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

// ChatResult contains the response and metadata about the request
type ChatResult struct {
	Response     *http.Response
	ProviderName string
	ResponseTime time.Duration
}

type Balancer struct {
	providers []provider.Provider
	current   uint64
	collector *metrics.Collector
}

// NewBalancer creates a balancer without metrics collection
func NewBalancer(providers []provider.Provider) *Balancer {
	return &Balancer{
		providers: providers,
	}
}

// NewBalancerWithMetrics creates a balancer with metrics collection enabled
func NewBalancerWithMetrics(providers []provider.Provider, collector *metrics.Collector) *Balancer {
	return &Balancer{
		providers: providers,
		collector: collector,
	}
}

// Chat performs a chat completion request and returns the response with metadata
func (b *Balancer) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	result, err := b.ChatWithResult(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

// ChatWithResult performs a chat completion and returns detailed result with provider info
func (b *Balancer) ChatWithResult(ctx context.Context, req *api.CreateChatCompletionRequest) (*ChatResult, error) {
	if len(b.providers) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	numProviders := uint64(len(b.providers))
	model := req.Model

	for i := uint64(0); i < numProviders; i++ {
		idx := (atomic.AddUint64(&b.current, 1) - 1) % numProviders
		p := b.providers[idx]
		providerName := p.Name()

		slog.Info("Forwarding request", "component", "balancer", "provider", providerName)

		startTime := time.Now()
		resp, err := p.Chat(ctx, req)
		elapsed := time.Since(startTime)

		if err != nil {
			slog.Warn("Provider transport error", "component", "balancer", "provider", providerName, "error", err)
			b.recordMetric(providerName, model, 0, elapsed, false, "transport_error")
			continue
		}

		// Failover logic: Retry on 429, 5xx, or 400/404 (model not found/invalid request for THIS provider)
		// Do NOT retry on 401/403 (Unauthorized/Forbidden) as those are permanent credential issues.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && resp.StatusCode < 600 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			slog.Warn("Provider error status (failover)", "component", "balancer", "provider", providerName, "status", resp.StatusCode, "body", string(bodyBytes))
			_ = resp.Body.Close()

			errorType := classifyError(resp.StatusCode)
			b.recordMetric(providerName, model, resp.StatusCode, elapsed, false, errorType)
			continue
		}

		success := resp.StatusCode == http.StatusOK
		if success {
			slog.Info("Provider success", "component", "balancer", "provider", providerName, "response_time_ms", elapsed.Milliseconds())
		} else {
			slog.Info("Provider final response", "component", "balancer", "provider", providerName, "status", resp.StatusCode)
		}

		b.recordMetric(providerName, model, resp.StatusCode, elapsed, success, "")

		return &ChatResult{
			Response:     resp,
			ProviderName: providerName,
			ResponseTime: elapsed,
		}, nil
	}

	return nil, fmt.Errorf("all providers failed or rate limited")
}

// recordMetric sends metrics to collector asynchronously (non-blocking)
func (b *Balancer) recordMetric(provider, model string, statusCode int, responseTime time.Duration, success bool, errorType string) {
	if b.collector != nil {
		b.collector.Record(provider, model, statusCode, responseTime, success, errorType)
	}
}

// classifyError returns a human-readable error type based on status code
func classifyError(statusCode int) string {
	switch {
	case statusCode == 429:
		return "rate_limit"
	case statusCode >= 500 && statusCode < 600:
		return "server_error"
	case statusCode == 400:
		return "bad_request"
	case statusCode == 404:
		return "not_found"
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	default:
		return "unknown"
	}
}
