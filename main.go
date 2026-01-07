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
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/server"
	"github.com/joho/godotenv"
)

var (
	listenAndServe = http.ListenAndServe
	logFatal       = log.Fatal
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

	// 5. Setup Router and Handlers
	r := chi.NewRouter()
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

	slog.Info("Gateway started", "port", port)
	return listenAndServe(":"+port, r)
}

func main() {
	if err := run(); err != nil {
		logFatal(err)
	}
}
