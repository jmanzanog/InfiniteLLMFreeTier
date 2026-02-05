package balancer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
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

// providerState tracks circuit breaker state per provider
type providerState struct {
	mu              sync.RWMutex
	failureCount    int
	cooldownUntil   time.Time
	consecutiveFail int
}

// Balancer with circuit breaker support
type Balancer struct {
	providers        []provider.Provider
	current          uint64
	collector        *metrics.Collector
	providerStates   map[string]*providerState
	statesMu         sync.RWMutex
	cooldownBase     time.Duration
	maxCooldown      time.Duration
	failureThreshold int
}

// NewBalancer creates a balancer without metrics collection
func NewBalancer(providers []provider.Provider) *Balancer {
	return newBalancer(providers, nil)
}

// NewBalancerWithMetrics creates a balancer with metrics collection enabled
func NewBalancerWithMetrics(providers []provider.Provider, collector *metrics.Collector) *Balancer {
	return newBalancer(providers, collector)
}

func newBalancer(providers []provider.Provider, collector *metrics.Collector) *Balancer {
	b := &Balancer{
		providers:        providers,
		collector:        collector,
		providerStates:   make(map[string]*providerState),
		cooldownBase:     getCooldownBase(),
		maxCooldown:      getMaxCooldown(),
		failureThreshold: getFailureThreshold(),
	}

	// Initialize state for each provider
	for _, p := range providers {
		b.providerStates[p.Name()] = &providerState{}
	}

	return b
}

func getCooldownBase() time.Duration {
	if env := os.Getenv("CIRCUIT_COOLDOWN_BASE_SECONDS"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return 30 * time.Second // Default: 30 seconds
}

func getMaxCooldown() time.Duration {
	if env := os.Getenv("CIRCUIT_MAX_COOLDOWN_SECONDS"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return 5 * time.Minute // Default: 5 minutes max
}

func getFailureThreshold() int {
	if env := os.Getenv("CIRCUIT_FAILURE_THRESHOLD"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			return val
		}
	}
	return 3 // Default: 3 consecutive failures to trip
}

// isProviderAvailable checks if provider is not in cooldown
func (b *Balancer) isProviderAvailable(providerName string) bool {
	b.statesMu.RLock()
	state, exists := b.providerStates[providerName]
	b.statesMu.RUnlock()

	if !exists {
		return true
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	return !time.Now().Before(state.cooldownUntil)
}

// recordProviderFailure increments failure count and applies cooldown if threshold reached
func (b *Balancer) recordProviderFailure(providerName string, statusCode int) {
	// Only apply circuit breaker for rate limits (429) and server errors (5xx)
	if statusCode != 429 && (statusCode < 500 || statusCode >= 600) {
		return
	}

	b.statesMu.RLock()
	state, exists := b.providerStates[providerName]
	b.statesMu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.consecutiveFail++
	state.failureCount++

	if state.consecutiveFail >= b.failureThreshold {
		// Exponential backoff: base * 2^(failures - threshold)
		multiplier := 1 << (state.consecutiveFail - b.failureThreshold)
		cooldown := b.cooldownBase * time.Duration(multiplier)
		if cooldown > b.maxCooldown {
			cooldown = b.maxCooldown
		}

		state.cooldownUntil = time.Now().Add(cooldown)
		slog.Warn("Provider circuit breaker tripped",
			"provider", providerName,
			"consecutive_failures", state.consecutiveFail,
			"cooldown_seconds", cooldown.Seconds(),
			"cooldown_until", state.cooldownUntil.Format(time.RFC3339),
		)
	}
}

// recordProviderSuccess resets the consecutive failure counter
func (b *Balancer) recordProviderSuccess(providerName string) {
	b.statesMu.RLock()
	state, exists := b.providerStates[providerName]
	b.statesMu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.consecutiveFail > 0 {
		slog.Info("Provider recovered", "provider", providerName, "previous_failures", state.consecutiveFail)
	}
	state.consecutiveFail = 0
	state.cooldownUntil = time.Time{}
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
	var lastErr error
	skippedCooldown := 0

	for i := uint64(0); i < numProviders; i++ {
		idx := (atomic.AddUint64(&b.current, 1) - 1) % numProviders
		p := b.providers[idx]
		providerName := p.Name()

		// Check circuit breaker
		if !b.isProviderAvailable(providerName) {
			slog.Debug("Provider in cooldown, skipping", "provider", providerName)
			skippedCooldown++
			continue
		}

		slog.Info("Forwarding request", "component", "balancer", "provider", providerName)

		startTime := time.Now()
		resp, err := p.Chat(ctx, req)
		elapsed := time.Since(startTime)

		if err != nil {
			slog.Warn("Provider transport error", "component", "balancer", "provider", providerName, "error", err)
			b.recordMetric(providerName, model, 0, elapsed, false, "transport_error")
			lastErr = fmt.Errorf("provider %s: %w", providerName, err)
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
			b.recordProviderFailure(providerName, resp.StatusCode)
			lastErr = fmt.Errorf("provider %s: status %d", providerName, resp.StatusCode)
			continue
		}

		success := resp.StatusCode == http.StatusOK
		if success {
			slog.Info("Provider success", "component", "balancer", "provider", providerName, "response_time_ms", elapsed.Milliseconds())
			b.recordProviderSuccess(providerName)
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

	if skippedCooldown == len(b.providers) {
		return nil, fmt.Errorf("all providers are in cooldown, try again later")
	}

	// lastErr is always set when providers fail (either transport error or non-OK response)
	return nil, fmt.Errorf("all providers failed: %w", lastErr)
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

// GetProviderStatus returns status info for all providers (for observability endpoints)
func (b *Balancer) GetProviderStatus() map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})

	b.statesMu.RLock()
	defer b.statesMu.RUnlock()

	for name, state := range b.providerStates {
		state.mu.RLock()
		info := map[string]interface{}{
			"available":         time.Now().After(state.cooldownUntil),
			"consecutive_fails": state.consecutiveFail,
			"total_failures":    state.failureCount,
		}
		if !state.cooldownUntil.IsZero() && time.Now().Before(state.cooldownUntil) {
			info["cooldown_until"] = state.cooldownUntil.Format(time.RFC3339)
			info["cooldown_remaining_seconds"] = int(time.Until(state.cooldownUntil).Seconds())
		}
		state.mu.RUnlock()
		result[name] = info
	}

	return result
}
