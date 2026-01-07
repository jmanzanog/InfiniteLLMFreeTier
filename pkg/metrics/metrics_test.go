package metrics

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// mockStore implements Store for testing error paths
type mockStore struct {
	saveErr       error
	statsErr      error
	saveCount     int32
	closed        bool
	statsToReturn *GlobalStats
}

func (m *mockStore) SaveRequest(record RequestRecord) error {
	return m.SaveRequests([]RequestRecord{record})
}

func (m *mockStore) SaveRequests(records []RequestRecord) error {
	atomic.AddInt32(&m.saveCount, int32(len(records)))
	return m.saveErr
}

func (m *mockStore) GetGlobalStats() (*GlobalStats, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.statsToReturn != nil {
		return m.statsToReturn, nil
	}
	return &GlobalStats{}, nil
}

func (m *mockStore) Close() error {
	m.closed = true
	return nil
}

func (m *mockStore) PurgeOldMetrics(_ int) error {
	return nil
}

func TestStore_SaveAndRetrieve(t *testing.T) {
	// Use temporary database
	tmpFile, err := os.CreateTemp("", "metrics_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Save some records
	records := []RequestRecord{
		{Provider: "Groq", Model: "llama-3", StatusCode: 200, ResponseTime: 100, Success: true, CreatedAt: time.Now()},
		{Provider: "Groq", Model: "llama-3", StatusCode: 200, ResponseTime: 150, Success: true, CreatedAt: time.Now()},
		{Provider: "Groq", Model: "llama-3", StatusCode: 429, ResponseTime: 50, Success: false, ErrorType: "rate_limit", CreatedAt: time.Now()},
		{Provider: "Mistral", Model: "mistral-7b", StatusCode: 200, ResponseTime: 200, Success: true, CreatedAt: time.Now()},
		{Provider: "Mistral", Model: "mistral-7b", StatusCode: 500, ResponseTime: 30, Success: false, ErrorType: "server_error", CreatedAt: time.Now()},
	}

	for _, r := range records {
		if err := store.SaveRequest(r); err != nil {
			t.Fatalf("Failed to save record: %v", err)
		}
	}

	// Get stats
	stats, err := store.GetGlobalStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	// Verify global stats
	if stats.TotalRequests != 5 {
		t.Errorf("Expected 5 total requests, got %d", stats.TotalRequests)
	}
	if stats.TotalSuccess != 3 {
		t.Errorf("Expected 3 successes, got %d", stats.TotalSuccess)
	}
	if stats.TotalFailures != 2 {
		t.Errorf("Expected 2 failures, got %d", stats.TotalFailures)
	}

	// Verify success rate
	expectedRate := 60.0 // 3/5 = 60%
	if stats.OverallSuccessRate != expectedRate {
		t.Errorf("Expected success rate %.1f%%, got %.1f%%", expectedRate, stats.OverallSuccessRate)
	}

	// Verify provider stats
	if len(stats.ProviderStats) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(stats.ProviderStats))
	}

	// Find Groq stats
	var groqStats *ProviderStats
	for i := range stats.ProviderStats {
		if stats.ProviderStats[i].Provider == "Groq" {
			groqStats = &stats.ProviderStats[i]
			break
		}
	}

	if groqStats == nil {
		t.Fatal("Groq stats not found")
	}
	if groqStats.TotalRequests != 3 {
		t.Errorf("Expected 3 Groq requests, got %d", groqStats.TotalRequests)
	}
	if groqStats.Error429Count != 1 {
		t.Errorf("Expected 1 429 error for Groq, got %d", groqStats.Error429Count)
	}
	if groqStats.SuccessRate == 0 {
		t.Error("Expected non-zero success rate for Groq")
	}
	if groqStats.LastRequestAt == "" {
		t.Error("Expected LastRequestAt to be set")
	}

	// Verify Since field
	if stats.Since == "" {
		t.Error("Expected Since field to be set")
	}
	if stats.GeneratedAt == "" {
		t.Error("Expected GeneratedAt field to be set")
	}
}

func TestStore_SaveRequests(t *testing.T) {
	// Use temporary database
	tmpFile, err := os.CreateTemp("", "metrics_batch_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Batch of records
	records := []RequestRecord{
		{Provider: "BatchP", Model: "m1", StatusCode: 200, ResponseTime: 100, Success: true, CreatedAt: time.Now()},
		{Provider: "BatchP", Model: "m2", StatusCode: 200, ResponseTime: 110, Success: true, CreatedAt: time.Now()},
		{Provider: "BatchP", Model: "m3", StatusCode: 200, ResponseTime: 120, Success: true, CreatedAt: time.Now()},
	}

	if err := store.SaveRequests(records); err != nil {
		t.Fatalf("Failed to save batch: %v", err)
	}

	stats, _ := store.GetGlobalStats()
	if stats.TotalRequests != 3 {
		t.Errorf("Expected 3 requests, got %d", stats.TotalRequests)
	}
}

func TestCollector_AsyncRecording(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_collector_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := NewCollector(store, 100)

	// Record some metrics
	collector.Record("TestProvider", "test-model", 200, 100*time.Millisecond, true, "")
	collector.Record("TestProvider", "test-model", 429, 50*time.Millisecond, false, "rate_limit")
	collector.Record("TestProvider", "test-model", 500, 30*time.Millisecond, false, "server_error")

	// Give async worker time to process
	time.Sleep(200 * time.Millisecond)

	stats, err := collector.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.TotalRequests != 3 {
		t.Errorf("Expected 3 requests, got %d", stats.TotalRequests)
	}

	if err := collector.Close(); err != nil {
		t.Errorf("Failed to close collector: %v", err)
	}
}

func TestCollector_BufferFull(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_buffer_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create collector with tiny buffer
	collector := NewCollector(store, 1)

	// Record many metrics quickly (some should be dropped due to full buffer)
	for i := 0; i < 100; i++ {
		collector.Record("TestProvider", "test-model", 200, 100*time.Millisecond, true, "")
	}

	// This should not block or panic
	_ = collector.Close()
}

func TestCollector_DefaultBufferSize(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_default_buffer_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Test with zero buffer size (should use default)
	collector := NewCollector(store, 0)

	// Should work without panic
	collector.Record("TestProvider", "test-model", 200, 100*time.Millisecond, true, "")
	time.Sleep(200 * time.Millisecond)

	_ = collector.Close()
}

func TestCollector_NegativeBufferSize(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_negative_buffer_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Test with negative buffer size (should use default)
	collector := NewCollector(store, -10)

	// Should work without panic
	collector.Record("TestProvider", "test-model", 200, 100*time.Millisecond, true, "")
	time.Sleep(200 * time.Millisecond)

	_ = collector.Close()
}

func TestCollector_DrainOnClose(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_drain_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	collector := NewCollector(store, 100)

	// Record metrics
	for i := 0; i < 10; i++ {
		collector.Record("DrainProvider", "test-model", 200, 100*time.Millisecond, true, "")
	}

	// Wait for worker to process some records before closing
	time.Sleep(100 * time.Millisecond)

	// Close should drain remaining records before store.Close()
	_ = collector.Close()

	// Reopen store to check if records were saved
	store2, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer func() { _ = store2.Close() }()

	stats, err := store2.GetGlobalStats()
	if err != nil {
		t.Fatalf("Failed to get stats after reopen: %v", err)
	}

	// Records should have been saved
	if stats.TotalRequests == 0 {
		t.Error("Expected some records to be saved")
	}
}

func TestStore_EmptyDatabase(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_empty_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	stats, err := store.GetGlobalStats()
	if err != nil {
		t.Fatalf("Failed to get stats from empty db: %v", err)
	}

	if stats.TotalRequests != 0 {
		t.Errorf("Expected 0 requests in empty db, got %d", stats.TotalRequests)
	}
	if len(stats.ProviderStats) != 0 {
		t.Errorf("Expected 0 providers in empty db, got %d", len(stats.ProviderStats))
	}
	if stats.OverallSuccessRate != 0 {
		t.Errorf("Expected 0 success rate in empty db, got %.1f", stats.OverallSuccessRate)
	}
}

func TestStore_ErrorCodes(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "metrics_error_codes_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	store, err := NewStore(tmpPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Save records with different error codes
	records := []RequestRecord{
		{Provider: "P1", Model: "m", StatusCode: 429, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
		{Provider: "P1", Model: "m", StatusCode: 500, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
		{Provider: "P1", Model: "m", StatusCode: 502, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
		{Provider: "P1", Model: "m", StatusCode: 400, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
		{Provider: "P1", Model: "m", StatusCode: 401, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
		{Provider: "P1", Model: "m", StatusCode: 403, ResponseTime: 10, Success: false, CreatedAt: time.Now()},
	}

	for _, r := range records {
		if err := store.SaveRequest(r); err != nil {
			t.Fatalf("Failed to save record: %v", err)
		}
	}

	stats, err := store.GetGlobalStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if len(stats.ProviderStats) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(stats.ProviderStats))
	}

	ps := stats.ProviderStats[0]
	if ps.Error429Count != 1 {
		t.Errorf("Expected 1 429 error, got %d", ps.Error429Count)
	}
	if ps.Error500Count != 2 {
		t.Errorf("Expected 2 5xx errors, got %d", ps.Error500Count)
	}
	if ps.Error400Count != 3 {
		t.Errorf("Expected 3 4xx errors (excluding 429), got %d", ps.Error400Count)
	}
}

func TestCollector_WorkerHandlesSaveError(t *testing.T) {
	mock := &mockStore{
		saveErr: errors.New("database error"),
	}

	collector := NewCollector(mock, 10)

	// Record should still work, but save will fail (logged, not panic)
	collector.Record("TestProvider", "model", 200, 100*time.Millisecond, true, "")

	// Wait for worker to process
	time.Sleep(100 * time.Millisecond)

	// Verify save was attempted
	if atomic.LoadInt32(&mock.saveCount) < 1 {
		t.Error("Expected at least 1 save attempt")
	}

	_ = collector.Close()
}

func TestCollector_DrainHandlesSaveError(t *testing.T) {
	mock := &mockStore{
		saveErr: errors.New("database error on shutdown"),
	}

	collector := NewCollector(mock, 10)

	// Queue multiple records
	for i := 0; i < 5; i++ {
		collector.Record("TestProvider", "model", 200, 100*time.Millisecond, true, "")
	}

	// Close immediately to trigger drain
	_ = collector.Close()

	// Give time for drain to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all saves were attempted despite errors
	if atomic.LoadInt32(&mock.saveCount) < 5 {
		t.Errorf("Expected at least 5 save attempts, got %d", atomic.LoadInt32(&mock.saveCount))
	}

	if !mock.closed {
		t.Error("Expected store to be closed")
	}
}

func TestCollector_GetStatsError(t *testing.T) {
	mock := &mockStore{
		statsErr: errors.New("stats error"),
	}

	collector := NewCollector(mock, 10)
	defer func() { _ = collector.Close() }()

	_, err := collector.GetStats()
	if err == nil {
		t.Error("Expected error from GetStats")
	}
}

func TestCollector_WithMockStore(t *testing.T) {
	mock := &mockStore{
		statsToReturn: &GlobalStats{
			TotalRequests: 42,
			TotalSuccess:  40,
			TotalFailures: 2,
		},
	}

	collector := NewCollector(mock, 10)
	defer func() { _ = collector.Close() }()

	stats, err := collector.GetStats()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if stats.TotalRequests != 42 {
		t.Errorf("Expected 42 requests, got %d", stats.TotalRequests)
	}
}
