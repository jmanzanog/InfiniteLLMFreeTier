package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	client := New()
	if client == nil {
		t.Fatal("New() returned nil")
	}
	if client.Timeout != DefaultTimeout {
		t.Errorf("Expected timeout %v, got %v", DefaultTimeout, client.Timeout)
	}
}

func TestNewWithTimeout(t *testing.T) {
	customTimeout := 30 * time.Second
	client := NewWithTimeout(customTimeout)
	if client == nil {
		t.Fatal("NewWithTimeout() returned nil")
	}
	if client.Timeout != customTimeout {
		t.Errorf("Expected timeout %v, got %v", customTimeout, client.Timeout)
	}
}

func TestClientTransportSettings(t *testing.T) {
	client := New()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Expected *http.Transport")
	}

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"MaxIdleConns", transport.MaxIdleConns, 100},
		{"MaxIdleConnsPerHost", transport.MaxIdleConnsPerHost, 10},
		{"MaxConnsPerHost", transport.MaxConnsPerHost, 20},
		{"IdleConnTimeout", transport.IdleConnTimeout, 90 * time.Second},
		{"TLSHandshakeTimeout", transport.TLSHandshakeTimeout, 10 * time.Second},
		{"ResponseHeaderTimeout", transport.ResponseHeaderTimeout, 60 * time.Second},
		{"ExpectContinueTimeout", transport.ExpectContinueTimeout, 1 * time.Second},
		{"ForceAttemptHTTP2", transport.ForceAttemptHTTP2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("Expected %v = %v, got %v", tt.name, tt.expected, tt.got)
			}
		})
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout != 120*time.Second {
		t.Errorf("Expected DefaultTimeout to be 120s, got %v", DefaultTimeout)
	}
}
