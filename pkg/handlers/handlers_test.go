package handlers

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
)

// mockStatsProvider implements StatsProvider for testing
type mockStatsProvider struct {
	stats *metrics.GlobalStats
	err   error
}

func (m *mockStatsProvider) GetStats() (*metrics.GlobalStats, error) {
	return m.stats, m.err
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	Health(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", body["status"])
	}
}

func TestStatsHandler_JSON_NoCollector(t *testing.T) {
	handler := NewStatsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	handler.JSON(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", resp.StatusCode)
	}
}

func TestStatsHandler_JSON_GetStatsError(t *testing.T) {
	mock := &mockStatsProvider{
		err: errors.New("database error"),
	}
	handler := NewStatsHandlerWithProvider(mock)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	handler.JSON(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", resp.StatusCode)
	}
}

func TestStatsHandler_JSON_WithCollector(t *testing.T) {
	// Create temp db
	tmpFile, err := os.CreateTemp("", "handler_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record some metrics
	collector.Record("TestProvider", "model", 200, 100*time.Millisecond, true, "")
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	handler.JSON(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var stats metrics.GlobalStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode stats: %v", err)
	}

	if stats.TotalRequests < 1 {
		t.Errorf("Expected at least 1 request, got %d", stats.TotalRequests)
	}
}

func TestStatsHandler_Web_NoCollector(t *testing.T) {
	handler := NewStatsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", resp.StatusCode)
	}
}

func TestStatsHandler_Web_GetStatsError(t *testing.T) {
	mock := &mockStatsProvider{
		err: errors.New("database error"),
	}
	handler := NewStatsHandlerWithProvider(mock)

	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Failed to retrieve stats") {
		t.Error("Expected 'Failed to retrieve stats' in response")
	}
}

func TestStatsHandler_Web_WithCollector(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_web_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	handler := NewStatsHandler(collector)

	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Expected Content-Type to contain 'text/html', got '%s'", ct)
	}
}

func TestStatsHandler_Web_WithRefresh(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_refresh_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	handler := NewStatsHandler(collector)

	req := httptest.NewRequest(http.MethodGet, "/stats/web?refresh=5", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// Check that refresh meta tag is present in body
	body := w.Body.String()
	if !strings.Contains(body, `content="5"`) {
		t.Error("Expected refresh meta tag with content='5'")
	}
	if !strings.Contains(body, "Auto-refresh") {
		t.Error("Expected refresh badge in HTML")
	}
}

func TestStatsHandler_Web_SuccessRateColors(t *testing.T) {
	tests := []struct {
		name          string
		successRate   float64
		expectedClass string
	}{
		{"High success rate", 98.0, "success"},
		{"Medium success rate", 90.0, "warning"},
		{"Low success rate", 70.0, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "handler_color_test_*.db")
			if err != nil {
				t.Fatal(err)
			}
			dbPath := tmpFile.Name()
			_ = tmpFile.Close()
			defer func() { _ = os.Remove(dbPath) }()

			store, err := metrics.NewStore(dbPath)
			if err != nil {
				t.Fatalf("Failed to create store: %v", err)
			}

			collector := metrics.NewCollector(store, 100)
			defer func() { _ = collector.Close() }()

			// Record metrics to achieve desired success rate
			successCount := int(tt.successRate)
			failureCount := 100 - successCount
			for i := 0; i < successCount; i++ {
				collector.Record("TestProvider", "model", 200, 100*time.Millisecond, true, "")
			}
			for i := 0; i < failureCount; i++ {
				collector.Record("TestProvider", "model", 500, 100*time.Millisecond, false, "server_error")
			}
			time.Sleep(200 * time.Millisecond)

			handler := NewStatsHandler(collector)
			req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
			w := httptest.NewRecorder()

			handler.Web(w, req)

			body := w.Body.String()
			if !strings.Contains(body, tt.expectedClass) {
				t.Errorf("Expected class '%s' in HTML for success rate %.1f", tt.expectedClass, tt.successRate)
			}
		})
	}
}

func TestStatsHandler_Web_ProviderRows(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_provider_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record metrics with various error types
	collector.Record("TestProvider", "model", 200, 100*time.Millisecond, true, "")
	collector.Record("TestProvider", "model", 429, 50*time.Millisecond, false, "rate_limit")
	collector.Record("TestProvider", "model", 500, 30*time.Millisecond, false, "server_error")
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "TestProvider") {
		t.Error("Expected provider name in HTML")
	}
	if !strings.Contains(body, "badge-warning") {
		t.Error("Expected warning badge for 429 errors")
	}
	if !strings.Contains(body, "badge-error") {
		t.Error("Expected error badge for 5xx errors")
	}
}

func TestStatsHandler_Web_EmptyProviders(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_empty_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// No metrics recorded - empty state
	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "No data available") {
		t.Error("Expected 'No data available' message for empty providers")
	}
}

func TestStatsHandler_Web_FailuresClassError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_failures_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record some failures to trigger failures > 0 branch
	collector.Record("FailProvider", "model", 500, 100*time.Millisecond, false, "server_error")
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	body := w.Body.String()
	// Should have "error" class for failures card
	if !strings.Contains(body, `card-value error`) {
		t.Error("Expected 'error' class for failures > 0")
	}
}

func TestStatsHandler_Web_ProviderBadgeError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_badge_error_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record many failures to get < 80% success rate
	for i := 0; i < 20; i++ {
		collector.Record("BadProvider", "model", 200, 100*time.Millisecond, true, "")
	}
	for i := 0; i < 80; i++ {
		collector.Record("BadProvider", "model", 500, 100*time.Millisecond, false, "server_error")
	}
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	body := w.Body.String()
	// Should have badge-error for provider with < 80% success rate
	if !strings.Contains(body, "badge-error") {
		t.Error("Expected 'badge-error' class for provider with low success rate")
	}
}

func TestStatsHandler_Web_ProviderBadgeWarning(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_badge_warning_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record to get exactly 90% success rate (between 80-95)
	for i := 0; i < 90; i++ {
		collector.Record("WarnProvider", "model", 200, 100*time.Millisecond, true, "")
	}
	for i := 0; i < 10; i++ {
		collector.Record("WarnProvider", "model", 500, 100*time.Millisecond, false, "server_error")
	}
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	body := w.Body.String()
	// Should have badge-warning for provider with 80-95% success rate
	if !strings.Contains(body, "badge-warning") {
		t.Error("Expected 'badge-warning' class for provider with medium success rate")
	}
}

func TestStatsHandler_Web_SuccessClass(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "handler_success_class_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(dbPath) }()

	store, err := metrics.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := metrics.NewCollector(store, 100)
	defer func() { _ = collector.Close() }()

	// Record only successes - failures should be 0 with "success" class
	collector.Record("GoodProvider", "model", 200, 100*time.Millisecond, true, "")
	time.Sleep(200 * time.Millisecond)

	handler := NewStatsHandler(collector)
	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	body := w.Body.String()
	// Should have "success" class for failures card when failures = 0
	if !strings.Contains(body, "badge-success") {
		t.Error("Expected 'badge-success' class for provider with >= 95% success rate")
	}
}

func TestParseDashboardTemplate_Error(t *testing.T) {
	_, err := parseDashboardTemplate(fstest.MapFS{})
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestMustParseDashboardTemplate_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()

	_ = mustParseDashboardTemplate(fstest.MapFS{})
}

func TestStatsHandler_Web_TemplateExecuteError(t *testing.T) {
	oldTemplate := dashboardTemplate
	badTemplate := template.Must(template.New("bad").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("fail") },
	}).Parse("{{fail}}"))
	dashboardTemplate = badTemplate
	t.Cleanup(func() { dashboardTemplate = oldTemplate })

	stats := &metrics.GlobalStats{TotalRequests: 1}
	handler := NewStatsHandlerWithProvider(&mockStatsProvider{stats: stats})

	req := httptest.NewRequest(http.MethodGet, "/stats/web", nil)
	w := httptest.NewRecorder()

	handler.Web(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Failed to render dashboard") {
		t.Fatal("expected render error message")
	}
}
