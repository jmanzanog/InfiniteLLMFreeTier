package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/config"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/handlers"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/middleware"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/server"
	"github.com/joho/godotenv"
)

var (
	// newHTTPServer creates a configured HTTP server with timeouts.
	// Exposed as var to allow overriding in tests if needed, or simply for testability.
	newHTTPServer = func(addr string, handler http.Handler) *http.Server {
		return &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      120 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1MB
		}
	}

	listenAndServe = func(addr string, handler http.Handler) error {
		return newHTTPServer(addr, handler).ListenAndServe()
	}
	logFatal = log.Fatal
)

func run() error {
	// 1. Initial setup
	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	// 2. Load Configuration
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	providers, err := config.GetProvidersFromEnv()
	if err != nil {
		return err
	}

	// 3. Initialize Metrics
	dbPath := os.Getenv("METRICS_DB_PATH")
	if dbPath == "" {
		dbPath = "metrics.db"
	}

	metricsStore, err := metrics.NewStore(dbPath)
	var collector *metrics.Collector
	if err != nil {
		slog.Warn("Failed to initialize metrics store, running without metrics", "error", err)
	} else {
		collector = metrics.NewCollector(metricsStore, 1000)
		defer func() { _ = collector.Close() }()

		// Setup retention policy
		retentionDays := 30
		if retentionStr := os.Getenv("METRICS_RETENTION_DAYS"); retentionStr != "" {
			if days, err := strconv.Atoi(retentionStr); err == nil && days > 0 {
				retentionDays = days
			}
		}
		collector.StartPurger(retentionDays, 24*time.Hour)
		slog.Info("Metrics collection enabled", "db_path", dbPath, "retention_days", retentionDays)
	}

	// 4. Initialize Balancer and Server Logic
	var lb *balancer.Balancer
	if collector != nil {
		lb = balancer.NewBalancerWithMetrics(providers, collector)
	} else {
		lb = balancer.NewBalancer(providers)
	}
	srv := server.NewServer(lb)

	// 5. Setup Router and Handlers with Middleware
	r := chi.NewRouter()

	// Apply middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.MaxBodySize)

	r.Get("/health", handlers.Health)

	statsHandler := handlers.NewStatsHandler(collector)
	r.Get("/stats", statsHandler.JSON)
	r.Get("/stats/web", statsHandler.Web)

	// OpenAI compatible endpoint
	strictHandler := api.NewStrictHandler(srv, nil)
	api.HandlerWithOptions(strictHandler, api.ChiServerOptions{
		BaseRouter: r,
		BaseURL:    "/v1",
	})

	slog.Info("Gateway started",
		"port", port,
		"read_timeout", "60s",
		"write_timeout", "120s",
		"idle_timeout", "120s",
	)
	return listenAndServe(":"+port, r)
}

func main() {
	if err := run(); err != nil {
		logFatal(err)
	}
}
