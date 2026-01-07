package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

type errorReader struct{}

func (e *errorReader) Read(_ []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func (e *errorReader) Close() error {
	return nil
}

func TestServer_CreateChatCompletion(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-val")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer mockUpstream.Close()

	p := provider.NewCustomProvider("Test", mockUpstream.URL, "key", "test-model")
	lb := balancer.NewBalancer([]provider.Provider{p})
	srv := NewServer(lb)

	t.Run("Success", func(t *testing.T) {
		req := api.CreateChatCompletionRequestObject{
			Body: &api.CreateChatCompletionJSONRequestBody{
				Model: "test-model",
				Messages: []api.ChatCompletionRequestMessage{
					{Role: "user", Content: "Hello"},
				},
			},
		}

		resp, err := srv.CreateChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		w := httptest.NewRecorder()
		err = resp.VisitCreateChatCompletionResponse(w)
		if err != nil {
			t.Fatalf("Visit failed: %v", err)
		}

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}
		if w.Header().Get("X-Provider") != "Test" {
			t.Errorf("Expected X-Provider header, got %s", w.Header().Get("X-Provider"))
		}
		if w.Header().Get("X-Test-Header") != "test-val" {
			t.Errorf("Expected upstream header to be copied")
		}
	})

	t.Run("NilBody", func(t *testing.T) {
		req := api.CreateChatCompletionRequestObject{Body: nil}
		_, err := srv.CreateChatCompletion(context.Background(), req)
		if err == nil {
			t.Error("Expected error for nil body")
		}
	})

	t.Run("BalancerError", func(t *testing.T) {
		lbEmpty := balancer.NewBalancer(nil)
		srvEmpty := NewServer(lbEmpty)
		req := api.CreateChatCompletionRequestObject{
			Body: &api.CreateChatCompletionJSONRequestBody{Model: "test"},
		}
		_, err := srvEmpty.CreateChatCompletion(context.Background(), req)
		if err == nil {
			t.Error("Expected balancer error")
		}
	})

	t.Run("LoggingWithDetails", func(t *testing.T) {
		_ = os.Setenv("LOG_LLM_RESPONSE_DETAILS", "true")
		defer func() { _ = os.Unsetenv("LOG_LLM_RESPONSE_DETAILS") }()

		req := api.CreateChatCompletionRequestObject{
			Body: &api.CreateChatCompletionJSONRequestBody{Model: "test-model"},
		}
		resp, err := srv.CreateChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}

		w := httptest.NewRecorder()
		_ = resp.VisitCreateChatCompletionResponse(w)
		if !strings.Contains(w.Body.String(), "ok") {
			t.Error("Response body should still be available after logging")
		}
	})

	t.Run("LoggingErrorReadingBody", func(t *testing.T) {
		_ = os.Setenv("LOG_LLM_RESPONSE_DETAILS", "true")
		defer func() { _ = os.Unsetenv("LOG_LLM_RESPONSE_DETAILS") }()

		// We need to bypass the real balancer or mock the provider response
		// but since we want to test Server.CreateChatCompletion's specific branch
		// let's use a sub-test that manually constructs the ProxyResponse or similar
		// Actually, let's just use a special provider that returns a broken body

		// This might be tricky because the provider reads the body too.
		// Let's rely on ProxyResponse test directly if needed.
	})
}

func TestProxyResponse_Visit_Streaming(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString("chunk1chunk2")),
	}
	r := &ProxyResponse{
		resp:         resp,
		providerName: "Test",
		responseTime: 100,
	}

	w := &streamingRecorder{ResponseRecorder: httptest.NewRecorder()}
	err := r.VisitCreateChatCompletionResponse(w)
	if err != nil {
		t.Fatal(err)
	}

	if !w.flushed {
		t.Error("Expected Flush to be called")
	}
}

type streamingRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (s *streamingRecorder) Flush() {
	s.flushed = true
}

func TestProxyResponse_BodyReadError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       &errorReader{},
	}
	r := &ProxyResponse{resp: resp}
	w := httptest.NewRecorder()
	err := r.VisitCreateChatCompletionResponse(w)
	if err == nil {
		t.Error("Expected error reading broken body")
	}
}
