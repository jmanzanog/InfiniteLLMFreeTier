package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/provider"
	"gopkg.in/yaml.v3"
)

var openConfigFile = os.Open

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

func LoadConfigFile() (*AppConfig, error) {
	f, err := openConfigFile("config.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var cfg AppConfig
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func GetProvidersFromEnv() ([]provider.Provider, error) {
	cfg, err := LoadConfigFile()
	if err != nil {
		slog.Warn("Error loading config.yaml. Using defaults.", "error", err)
		cfg = &AppConfig{}
	}

	var providers []provider.Provider
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

	if len(providers) == 0 {
		return nil, fmt.Errorf("no provider API keys configured")
	}

	// Debug Mode: Fixed Provider
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
		return filtered, nil
	}

	return providers, nil
}
