package balancer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

type Balancer struct {
	providers []provider.Provider
	current   uint32
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

	numProviders := uint32(len(b.providers))

	for i := uint32(0); i < numProviders; i++ {
		idx := (atomic.AddUint32(&b.current, 1) - 1) % numProviders
		p := b.providers[idx]

		log.Printf("[Balancer] Forwarding request to provider: %s", p.Name())
		resp, err := p.Chat(ctx, req)
		if err != nil {
			log.Printf("[Balancer] Provider %s high-level transport error: %v. Retrying...", p.Name(), err)
			continue
		}

		// Failover logic: Retry on 429, 5xx, or 400/404 (model not found/invalid request for THIS provider)
		// Do NOT retry on 401/403 (Unauthorized/Forbidden) as those are permanent credential issues.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && resp.StatusCode < 600 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("[Balancer] Provider %s returned error status %d. Body: %s. Failover initiated...", p.Name(), resp.StatusCode, string(bodyBytes))
			_ = resp.Body.Close()
			continue
		}

		if resp.StatusCode == http.StatusOK {
			log.Printf("[Balancer] Provider %s responded with status 200 (Success)", p.Name())
		} else {
			log.Printf("[Balancer] Provider %s responded with status %d (Final Response)", p.Name(), resp.StatusCode)
		}
		return resp, nil
	}

	return nil, fmt.Errorf("all providers failed or rate limited")
}
