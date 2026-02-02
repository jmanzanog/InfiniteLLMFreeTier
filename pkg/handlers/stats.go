package handlers

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/metrics"
)

//go:embed templates/*.html
var templateFS embed.FS

var dashboardTemplate *template.Template

func init() {
	dashboardTemplate = mustParseDashboardTemplate(templateFS)
}

func parseDashboardTemplate(fsys fs.FS) (*template.Template, error) {
	return template.ParseFS(fsys, "templates/dashboard.html")
}

func mustParseDashboardTemplate(fsys fs.FS) *template.Template {
	tmpl, err := parseDashboardTemplate(fsys)
	if err != nil {
		panic("failed to parse dashboard template: " + err.Error())
	}
	return tmpl
}

// StatsProvider abstracts stats retrieval for testing
type StatsProvider interface {
	GetStats() (*metrics.GlobalStats, error)
}

// StatsHandler handles metrics-related endpoints
type StatsHandler struct {
	provider StatsProvider
}

// NewStatsHandler creates a new stats handler
func NewStatsHandler(collector *metrics.Collector) *StatsHandler {
	if collector == nil {
		return &StatsHandler{provider: nil}
	}
	return &StatsHandler{provider: collector}
}

// NewStatsHandlerWithProvider creates a handler with a custom stats provider (for testing)
func NewStatsHandlerWithProvider(provider StatsProvider) *StatsHandler {
	return &StatsHandler{provider: provider}
}

// JSON returns metrics as JSON
func (h *StatsHandler) JSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.provider == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"metrics collection not enabled"}`))
		return
	}

	stats, err := h.provider.GetStats()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to retrieve stats"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

// DashboardData holds the data for the dashboard template
type DashboardData struct {
	Stats                     *metrics.GlobalStats
	RefreshInterval           string
	SuccessRateClass          string
	FailuresClass             string
	Providers                 []ProviderData
	OverallSuccessRatePercent string // Pre-formatted percentage for CSS/HTML
}

// ProviderData holds provider stats with computed badge class
type ProviderData struct {
	metrics.ProviderStats
	BadgeClass string
}

// Web returns an HTML dashboard with metrics
func (h *StatsHandler) Web(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if h.provider == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<h1>Metrics collection not enabled</h1>`))
		return
	}

	stats, err := h.provider.GetStats()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<h1>Failed to retrieve stats</h1>`))
		return
	}

	// Build template data
	data := DashboardData{
		Stats:           stats,
		RefreshInterval: r.URL.Query().Get("refresh"),
	}

	// Compute success rate class and style
	if stats.OverallSuccessRate >= 95 {
		data.SuccessRateClass = "success"
	} else if stats.OverallSuccessRate >= 80 {
		data.SuccessRateClass = "warning"
	} else {
		data.SuccessRateClass = "error"
	}
	data.OverallSuccessRatePercent = fmt.Sprintf("%.1f", stats.OverallSuccessRate)

	// Compute failures class
	if stats.TotalFailures > 0 {
		data.FailuresClass = "error"
	} else {
		data.FailuresClass = "success"
	}

	// Build provider data with badge classes
	for _, p := range stats.ProviderStats {
		pd := ProviderData{ProviderStats: p}
		if p.SuccessRate >= 95 {
			pd.BadgeClass = "badge-success"
		} else if p.SuccessRate >= 80 {
			pd.BadgeClass = "badge-warning"
		} else {
			pd.BadgeClass = "badge-error"
		}
		data.Providers = append(data.Providers, pd)
	}

	// Render template
	var buf bytes.Buffer
	if err := dashboardTemplate.Execute(&buf, data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<h1>Failed to render dashboard</h1>`))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
