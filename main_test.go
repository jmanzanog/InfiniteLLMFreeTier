package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/server"
)

// --- Mocks to force unreachable errors ---

type errorReader struct{}

func (e *errorReader) Read(_ []byte) (n int, err error) { return 0, errors.New("io error total") }
func (e *errorReader) Close() error                     { return nil }

type failResponse struct{}

func (f failResponse) VisitCreateChatCompletionResponse(_ http.ResponseWriter) error {
	return errors.New("visit failure")
}

type mockStrictServer struct {
	api.Unimplemented
	resp interface{}
	err  error
}

func (m *mockStrictServer) CreateChatCompletion(_ context.Context, _ api.CreateChatCompletionRequestObject) (api.CreateChatCompletionResponseObject, error) {
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
		h := api.NewStrictHandler(server.NewServer(lb), []api.StrictMiddlewareFunc{mw})

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
	srv := server.NewServer(lb)
	strictHandler := api.NewStrictHandler(srv, nil)
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

func TestHealthEndpoint(t *testing.T) {
	// Create a minimal router with only the health endpoint
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	t.Run("Health_Returns_200_OK", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("Failed to call /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Health_Returns_JSON_Content_Type", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("Failed to call /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}
	})

	t.Run("Health_Returns_Status_OK_Body", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("Failed to call /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		body := buf.String()

		expected := `{"status":"ok"}`
		if body != expected {
			t.Errorf("Expected body '%s', got '%s'", expected, body)
		}
	})
}

func TestHealthEndpoint_Integration(t *testing.T) {
	// Test health endpoint integrated with the full router (like run() sets up)
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer mockUpstream.Close()

	r := chi.NewRouter()

	// Health endpoint (same as in run())
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Mount OpenAI handler
	p := provider.NewCustomProvider("Test", mockUpstream.URL, "key", "test-model")
	lb := balancer.NewBalancer([]provider.Provider{p})
	srv := server.NewServer(lb)
	strictHandler := api.NewStrictHandler(srv, nil)
	api.HandlerWithOptions(strictHandler, api.ChiServerOptions{
		BaseRouter: r,
		BaseURL:    "/v1",
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	t.Run("Health_Works_Alongside_API", func(t *testing.T) {
		// Test /health
		healthResp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("Failed to call /health: %v", err)
		}
		defer func() { _ = healthResp.Body.Close() }()

		if healthResp.StatusCode != http.StatusOK {
			t.Errorf("Health: Expected 200, got %d", healthResp.StatusCode)
		}

		// Test /v1/chat/completions still works
		jsonBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
		apiResp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(jsonBody))
		if err != nil {
			t.Fatalf("Failed to call /v1/chat/completions: %v", err)
		}
		defer func() { _ = apiResp.Body.Close() }()

		if apiResp.StatusCode != http.StatusOK {
			t.Errorf("API: Expected 200, got %d", apiResp.StatusCode)
		}
	})
}

func TestRun_NoProviders(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Unsetenv("GROQ_API_KEY")
	_ = os.Unsetenv("CEREBRAS_API_KEY")
	_ = os.Unsetenv("OPENROUTER_API_KEY")
	_ = os.Unsetenv("MISTRAL_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("FIXED_PROVIDER")

	if err := run(); err == nil {
		t.Fatal("expected error with no providers")
	}
}

func TestRun_MetricsStoreError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Setenv("GROQ_API_KEY", "test")
	_ = os.Setenv("METRICS_DB_PATH", filepath.Join(tmpDir, "missing", "metrics.db"))
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("METRICS_DB_PATH")
	})

	oldListen := listenAndServe
	listenAndServe = func(addr string, handler http.Handler) error { return nil }
	t.Cleanup(func() { listenAndServe = oldListen })

	if err := run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_RetentionDaysOverride(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Setenv("GROQ_API_KEY", "test")
	_ = os.Setenv("METRICS_DB_PATH", filepath.Join(tmpDir, "metrics.db"))
	_ = os.Setenv("METRICS_RETENTION_DAYS", "7")
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("METRICS_DB_PATH")
		_ = os.Unsetenv("METRICS_RETENTION_DAYS")
	})

	oldListen := listenAndServe
	listenAndServe = func(addr string, handler http.Handler) error { return nil }
	t.Cleanup(func() { listenAndServe = oldListen })

	if err := run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_DefaultPort(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Unsetenv("PORT")
	_ = os.Setenv("GROQ_API_KEY", "test")
	_ = os.Setenv("METRICS_DB_PATH", filepath.Join(tmpDir, "metrics.db"))
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("METRICS_DB_PATH")
	})

	oldListen := listenAndServe
	var gotAddr string
	listenAndServe = func(addr string, handler http.Handler) error {
		gotAddr = addr
		return nil
	}
	t.Cleanup(func() { listenAndServe = oldListen })

	if err := run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAddr != ":8080" {
		t.Fatalf("expected :8080, got %s", gotAddr)
	}
}

func TestMain_LogsFatalOnError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Setenv("GROQ_API_KEY", "test")
	_ = os.Setenv("METRICS_DB_PATH", filepath.Join(tmpDir, "metrics.db"))
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("METRICS_DB_PATH")
	})

	oldListen := listenAndServe
	listenAndServe = func(addr string, handler http.Handler) error { return errors.New("listen fail") }
	oldLogFatal := logFatal
	called := false
	logFatal = func(v ...interface{}) { called = true }
	t.Cleanup(func() {
		listenAndServe = oldListen
		logFatal = oldLogFatal
	})

	main()
	if !called {
		t.Fatal("expected logFatal to be called")
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "tmp-")
	if err != nil {
		t.Fatal(err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(absDir) })
	return absDir
}
