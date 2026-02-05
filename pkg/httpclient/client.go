// Package httpclient provides a pre-configured HTTP client with sensible defaults
// for timeouts, connection pooling, and keep-alive settings.
package httpclient

import (
	"net"
	"net/http"
	"time"
)

// DefaultTimeout is the overall request timeout for LLM API calls.
// LLMs can take longer to respond, so we use a generous timeout.
const DefaultTimeout = 120 * time.Second

// New creates an HTTP client with optimized transport settings.
// This prevents connection leaks, hung connections, and provides
// consistent behavior across all providers.
func New() *http.Client {
	return NewWithTimeout(DefaultTimeout)
}

// NewWithTimeout creates an HTTP client with custom timeout.
func NewWithTimeout(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		// Connection pooling
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     90 * time.Second,

		// Timeouts for connection establishment
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second, // Connection timeout
			KeepAlive: 30 * time.Second, // Keep-alive probe interval
		}).DialContext,

		// TLS handshake timeout
		TLSHandshakeTimeout: 10 * time.Second,

		// Response header timeout (time to receive headers after request is sent)
		ResponseHeaderTimeout: 60 * time.Second,

		// Expect continue timeout
		ExpectContinueTimeout: 1 * time.Second,

		// Force HTTP/2 where possible but allow fallback
		ForceAttemptHTTP2: true,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}
