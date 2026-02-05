package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_GeneratesUUID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := GetRequestID(r.Context())
		if reqID == "" {
			t.Error("Expected request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Check response header
	respID := rec.Header().Get("X-Request-ID")
	if respID == "" {
		t.Error("Expected X-Request-ID in response header")
	}
	// UUID format: 8-4-4-4-12 = 36 chars
	if len(respID) != 36 {
		t.Errorf("Expected UUID format (36 chars), got %d chars: %s", len(respID), respID)
	}
}

func TestRequestID_UsesIncomingHeader(t *testing.T) {
	incomingID := "custom-request-id-12345"

	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := GetRequestID(r.Context())
		if reqID != incomingID {
			t.Errorf("Expected %s, got %s", incomingID, reqID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", incomingID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	respID := rec.Header().Get("X-Request-ID")
	if respID != incomingID {
		t.Errorf("Expected response header %s, got %s", incomingID, respID)
	}
}

func TestGetRequestID_EmptyContext(t *testing.T) {
	ctx := context.Background()
	reqID := GetRequestID(ctx)
	if reqID != "" {
		t.Errorf("Expected empty string for context without request ID, got %s", reqID)
	}
}

func TestMaxBodySize_AllowsValidSize(t *testing.T) {
	handler := MaxBodySize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("small body")
	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Length", "10")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}

func TestMaxBodySize_RejectsTooLarge(t *testing.T) {
	// Set a small max for testing
	t.Setenv("MAX_REQUEST_BODY_BYTES", "100")

	handler := MaxBodySize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Length", "1000000") // 1MB
	req.ContentLength = 1000000
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 413, got %d", rec.Code)
	}
}

func TestMaxBodySize_DefaultValue(t *testing.T) {
	// Ensure env is not set (t.Setenv in other tests is scoped to that test)
	expected := int64(10 * 1024 * 1024) // 10MB
	got := getMaxBodyBytes()
	if got != expected {
		t.Errorf("Expected default %d, got %d", expected, got)
	}
}

func TestMaxBodySize_EnvOverride(t *testing.T) {
	t.Setenv("MAX_REQUEST_BODY_BYTES", "5000")

	expected := int64(5000)
	got := getMaxBodyBytes()
	if got != expected {
		t.Errorf("Expected %d, got %d", expected, got)
	}
}

func TestMaxBodySize_InvalidEnv(t *testing.T) {
	t.Setenv("MAX_REQUEST_BODY_BYTES", "invalid")

	expected := int64(10 * 1024 * 1024) // fallback to default
	got := getMaxBodyBytes()
	if got != expected {
		t.Errorf("Expected default %d for invalid env, got %d", expected, got)
	}
}

func TestSanitizeIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:8080", "192.168.1.1:..."},
		{"10.0.0.1:443", "10.0.0.1:..."},
		{"[::1]:8080", "[ipv6]:..."},
		{"", "unknown"},
		{"192.168.1.1", "192.168.1.1"}, // No port
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeIP(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeIP(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}
