package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

var (
	listenAndServe = http.ListenAndServe
	logFatal       = log.Fatal
)

// Server implements api.StrictServerInterface

type Server struct {
	api.Unimplemented
	lb *balancer.Balancer
}

func NewServer(lb *balancer.Balancer) *Server {
	return &Server{lb: lb}
}

// CreateChatCompletion implements api.StrictServerInterface
func (s *Server) CreateChatCompletion(ctx context.Context, request api.CreateChatCompletionRequestObject) (api.CreateChatCompletionResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("request body is required")
	}

	slog.Info("Incoming request", "model", request.Body.Model)

	resp, err := s.lb.Chat(ctx, request.Body)
	if err != nil {
		slog.Error("Error forwarding request", "error", err)
		return nil, err
	}

	if os.Getenv("LOG_LLM_RESPONSE_DETAILS") == "true" {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Error reading response body for logging", "error", err)
		}
		// Restore the body so it can be read again
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		slog.Info("Upstream response details",
			"status", resp.Status,
			"status_code", resp.StatusCode,
			"headers", resp.Header,
			"body", string(bodyBytes),
		)
	} else {
		slog.Info("Upstream response received", "status", resp.Status)
	}

	// We return a custom response object to handle both JSON and Streaming
	return &ProxyResponse{resp: resp}, nil
}

// ProxyResponse implements api.CreateChatCompletionResponseObject
type ProxyResponse struct {
	resp *http.Response
}

func (r *ProxyResponse) VisitCreateChatCompletionResponse(w http.ResponseWriter) error {
	defer func() { _ = r.resp.Body.Close() }()

	// Copy headers from upstream
	for k, vv := range r.resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(r.resp.StatusCode)

	// Stream or copy body
	if flusher, ok := w.(http.Flusher); ok {
		_, err := io.Copy(&flushWriter{w, flusher}, r.resp.Body)
		return err
	}

	_, err := io.Copy(w, r.resp.Body)
	return err
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	if n > 0 {
		fw.f.Flush()
	}
	return
}

func run() error {
	// Load .env file if present
	_ = godotenv.Load()

	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	providers := getProvidersFromEnv()
	if len(providers) == 0 {
		return fmt.Errorf("no provider API keys configured")
	}

	lb := balancer.NewBalancer(providers)
	server := NewServer(lb)

	r := chi.NewRouter()

	// NewStrictHandler returns a ServerInterface
	strictHandler := api.NewStrictHandler(server, nil)

	// Mount the generated handler on chi with /v1 prefix
	slog.Info("Server Exposing OpenAI compatible endpoint", "path", "/v1/chat/completions")
	api.HandlerWithOptions(strictHandler, api.ChiServerOptions{
		BaseRouter: r,
		BaseURL:    "/v1",
	})

	slog.Info("Gateway started", "port", port)
	return listenAndServe(":"+port, r)
}

type AppConfig struct {
	Providers struct {
		Groq struct {
			DefaultModel string `yaml:"default_model"`
		} `yaml:"groq"`
		Cerebras struct {
			DefaultModel string `yaml:"default_model"`
		} `yaml:"cerebras"`
		OpenRouter struct {
			DefaultModel string `yaml:"default_model"`
		} `yaml:"openrouter"`
		Mistral struct {
			DefaultModel string `yaml:"default_model"`
		} `yaml:"mistral"`
		Gemini struct {
			DefaultModel string `yaml:"default_model"`
		} `yaml:"gemini"`
	} `yaml:"providers"`
}

func loadConfigFile() (*AppConfig, error) {
	f, err := os.Open("config.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{}, nil // Return empty config if file missing
		}
		return nil, err
	}
	defer f.Close()

	var cfg AppConfig
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func getProvidersFromEnv() []provider.Provider {
	cfg, err := loadConfigFile()
	if err != nil {
		slog.Warn("Error loading config.yaml. Using defaults.", "error", err)
		cfg = &AppConfig{}
	}

	providers := []provider.Provider{}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		slog.Info("Initializing Groq provider", "default_model", cfg.Providers.Groq.DefaultModel)
		providers = append(providers, provider.NewGroqProvider(key, cfg.Providers.Groq.DefaultModel))
	}
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" {
		slog.Info("Initializing Cerebras provider", "default_model", cfg.Providers.Cerebras.DefaultModel)
		providers = append(providers, provider.NewCerebrasProvider(key, cfg.Providers.Cerebras.DefaultModel))
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		slog.Info("Initializing OpenRouter provider", "default_model", cfg.Providers.OpenRouter.DefaultModel)
		providers = append(providers, provider.NewOpenRouterProvider(key, cfg.Providers.OpenRouter.DefaultModel))
	}
	if key := os.Getenv("MISTRAL_API_KEY"); key != "" {
		slog.Info("Initializing Mistral provider", "default_model", cfg.Providers.Mistral.DefaultModel)
		providers = append(providers, provider.NewMistralProvider(key, cfg.Providers.Mistral.DefaultModel))
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		slog.Info("Initializing Gemini provider", "default_model", cfg.Providers.Gemini.DefaultModel)
		providers = append(providers, provider.NewGeminiProvider(key, cfg.Providers.Gemini.DefaultModel))
	}

	// Debug Mode: Fixed Provider
	// If FIXED_PROVIDER is set, ignore others to isolate it.
	if fixed := os.Getenv("FIXED_PROVIDER"); fixed != "" {
		slog.Info("FIXED_PROVIDER mode enabled", "provider", fixed)
		var filtered []provider.Provider
		for _, p := range providers {
			if strings.EqualFold(p.Name(), fixed) {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			slog.Warn("FIXED_PROVIDER requested but not found or not configured", "provider", fixed)
		}
		return filtered
	}

	return providers
}

func main() {
	if err := run(); err != nil {
		logFatal(err)
	}
}
