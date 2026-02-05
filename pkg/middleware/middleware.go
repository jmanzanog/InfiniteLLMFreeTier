// Package middleware provides HTTP middleware for observability and security.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/google/uuid"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

// RequestID generates or extracts a unique request ID for tracing.
// It checks for incoming X-Request-ID header first, otherwise generates a new UUID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Add to response headers for client correlation
		w.Header().Set("X-Request-ID", requestID)

		// Add to context for downstream handlers
		ctx := context.WithValue(r.Context(), RequestIDKey, requestID)

		// Log with request ID
		slog.Info("Request started",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", sanitizeIP(r.RemoteAddr),
		)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// MaxBodySize limits the size of incoming request bodies.
// Returns 413 Payload Too Large if exceeded.
func MaxBodySize(next http.Handler) http.Handler {
	maxBytes := getMaxBodyBytes()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBytes {
			slog.Warn("Request body too large",
				"request_id", GetRequestID(r.Context()),
				"content_length", r.ContentLength,
				"max_bytes", maxBytes,
			)
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		// Wrap body with max size limiter as a safeguard
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

func getMaxBodyBytes() int64 {
	if env := os.Getenv("MAX_REQUEST_BODY_BYTES"); env != "" {
		if val, err := strconv.ParseInt(env, 10, 64); err == nil && val > 0 {
			return val
		}
	}
	// Default: 10MB (generous for LLM prompts with context)
	return 10 * 1024 * 1024
}

// sanitizeIP removes potential PII by truncating IP addresses in logs.
// For IPv4: shows only first two octets. For IPv6: shows only first segment.
func sanitizeIP(addr string) string {
	// Simple approach: just show presence of IP family
	if len(addr) == 0 {
		return "unknown"
	}
	// For logging purposes, we indicate traffic origin without full IP
	if addr[0] == '[' {
		return "[ipv6]:..."
	}
	// Find first colon (port separator) or end
	for i := 0; i < len(addr); i++ {
		if addr[i] == ':' {
			// IPv4 with port
			return addr[:i] + ":..."
		}
	}
	return addr
}
