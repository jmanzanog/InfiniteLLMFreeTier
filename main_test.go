package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
)

// --- Mocks to force unreachable errors ---

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) { return 0, errors.New("io error total") }
func (e *errorReader) Close() error                     { return nil }

type failResponse struct{}

func (f failResponse) VisitCreateChatCompletionResponse(w http.ResponseWriter) error {
	return errors.New("visit failure")
}

type mockStrictServer struct {
	api.Unimplemented
	resp interface{}
	err  error
}

func (m *mockStrictServer) CreateChatCompletion(ctx context.Context, request api.CreateChatCompletionRequestObject) (api.CreateChatCompletionResponseObject, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.resp.(api.CreateChatCompletionResponseObject), nil
}

func TestGeneratedCode_ExtremeConditions(t *testing.T) {
	// Tests generated code directly to aim for 100% coverage

	t.Run("IO_Read_Failure_During_Decode", func(t *testing.T) {
		h := api.NewStrictHandler(&mockStrictServer{}, nil)
		req := httptest.NewRequest("POST", "/chat", &errorReader{})
		w := httptest.NewRecorder()
		h.CreateChatCompletion(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 on IO failure, got %d", w.Code)
		}
	})

	t.Run("Strict_Server_Returns_Error", func(t *testing.T) {
		h := api.NewStrictHandler(&mockStrictServer{err: errors.New("logic fail")}, nil)
		req := httptest.NewRequest("POST", "/chat", bytes.NewBufferString(`{"model":"test"}`))
		w := httptest.NewRecorder()
		h.CreateChatCompletion(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 on logic fail, got %d", w.Code)
		}
	})

	t.Run("Visit_Method_Fails", func(t *testing.T) {
		h := api.NewStrictHandler(&mockStrictServer{resp: failResponse{}}, nil)
		req := httptest.NewRequest("POST", "/chat", bytes.NewBufferString(`{"model":"test"}`))
		w := httptest.NewRecorder()
		h.CreateChatCompletion(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 on visit fail, got %d", w.Code)
		}
	})

	t.Run("Middleware_Execution", func(t *testing.T) {
		called := false
		mw := func(f api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
				called = true
				return f(ctx, w, r, request)
			}
		}
		// Use a real provider to avoid panic
		p := provider.NewCustomProvider("T", "http://l", "k", "test-model")
		lb := balancer.NewBalancer([]provider.Provider{p})
		h := api.NewStrictHandler(NewServer(lb), []api.StrictMiddlewareFunc{mw})

		req := httptest.NewRequest("POST", "/chat", bytes.NewBufferString(`{"model":"test"}`))
		w := httptest.NewRecorder()
		h.CreateChatCompletion(w, req)
		if !called {
			t.Error("Middleware was not called")
		}
	})
}

// ... (Rest of gateway tests) ...

func TestGateway_E2E(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer mockUpstream.Close()

	r := chi.NewRouter()
	p := provider.NewCustomProvider("Test", mockUpstream.URL, "key", "test-model")
	lb := balancer.NewBalancer([]provider.Provider{p})
	server := NewServer(lb)
	strictHandler := api.NewStrictHandler(server, nil)
	api.HandlerWithOptions(strictHandler, api.ChiServerOptions{
		BaseRouter: r,
		BaseURL:    "/v1",
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	t.Run("Broken_Contract_Invalid_JSON", func(t *testing.T) {
		// Send malformed JSON to trigger generated code decoding error
		resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{ "model": "gpt-4", "messages": "esto-deberia-ser-un-array" }`))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request from generated code, got %d", resp.StatusCode)
		}
	})

	t.Run("Full_HTTP_Integration_Success", func(t *testing.T) {
		jsonBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
		resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(jsonBody))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestRunAndMain(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("GROQ_API_KEY", "test")
	oldListen := listenAndServe
	defer func() { listenAndServe = oldListen }()
	listenAndServe = func(addr string, handler http.Handler) error { return nil }
	_ = run()
}
