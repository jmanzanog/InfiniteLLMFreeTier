package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFile_Missing(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	cfg, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
}

func TestLoadConfigFile_InvalidYAML(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(":bad"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadConfigFile(); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestLoadConfigFile_OpenError(t *testing.T) {
	origOpen := openConfigFile
	openConfigFile = func(_ string) (*os.File, error) {
		return nil, errors.New("open fail")
	}
	t.Cleanup(func() { openConfigFile = origOpen })

	if _, err := LoadConfigFile(); err == nil {
		t.Fatal("expected open error")
	}
}

func TestLoadConfigFile_Valid(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	content := []byte("providers:\n  groq:\n    default_model: groq-m\n  gemini:\n    default_model: gem-m\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers.Groq.DefaultModel != "groq-m" {
		t.Fatalf("expected groq-m, got %q", cfg.Providers.Groq.DefaultModel)
	}
	if cfg.Providers.Gemini.DefaultModel != "gem-m" {
		t.Fatalf("expected gem-m, got %q", cfg.Providers.Gemini.DefaultModel)
	}
}

func TestGetProvidersFromEnv_NoKeys(t *testing.T) {
	_ = os.Unsetenv("GROQ_API_KEY")
	_ = os.Unsetenv("CEREBRAS_API_KEY")
	_ = os.Unsetenv("OPENROUTER_API_KEY")
	_ = os.Unsetenv("MISTRAL_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("FIXED_PROVIDER")

	_, err := GetProvidersFromEnv()
	if err == nil {
		t.Fatal("expected error when no providers configured")
	}
}

func TestGetProvidersFromEnv_WithConfigAndFixedProvider(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	content := []byte("providers:\n  groq:\n    default_model: groq-m\n  mistral:\n    default_model: mist-m\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("GROQ_API_KEY", "k1")
	_ = os.Setenv("MISTRAL_API_KEY", "k2")
	_ = os.Setenv("FIXED_PROVIDER", "mistral")
	_ = os.Unsetenv("CEREBRAS_API_KEY")
	_ = os.Unsetenv("OPENROUTER_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("MISTRAL_API_KEY")
		_ = os.Unsetenv("FIXED_PROVIDER")
	})

	providers, err := GetProvidersFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].Name() != "Mistral" {
		t.Fatalf("expected Mistral, got %q", providers[0].Name())
	}
}

func TestGetProvidersFromEnv_AllProviders(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_ = os.Setenv("GROQ_API_KEY", "k1")
	_ = os.Setenv("CEREBRAS_API_KEY", "k2")
	_ = os.Setenv("OPENROUTER_API_KEY", "k3")
	_ = os.Setenv("MISTRAL_API_KEY", "k4")
	_ = os.Setenv("GEMINI_API_KEY", "k5")
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("CEREBRAS_API_KEY")
		_ = os.Unsetenv("OPENROUTER_API_KEY")
		_ = os.Unsetenv("MISTRAL_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")
	})

	providers, err := GetProvidersFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 5 {
		t.Fatalf("expected 5 providers, got %d", len(providers))
	}
}

func TestGetProvidersFromEnv_FixedProviderMissing(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("providers: {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("GROQ_API_KEY", "k1")
	_ = os.Setenv("FIXED_PROVIDER", "unknown")
	t.Cleanup(func() {
		_ = os.Unsetenv("GROQ_API_KEY")
		_ = os.Unsetenv("FIXED_PROVIDER")
	})

	providers, err := GetProvidersFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected 0 providers for missing fixed provider, got %d", len(providers))
	}
}

func TestGetProvidersFromEnv_LoadConfigError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := tempDir(t)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(":bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("GEMINI_API_KEY", "k1")
	t.Cleanup(func() { _ = os.Unsetenv("GEMINI_API_KEY") })

	providers, err := GetProvidersFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].Name() != "Gemini" {
		t.Fatalf("expected Gemini, got %q", providers[0].Name())
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
