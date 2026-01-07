package metrics

import (
	"log/slog"
	"time"
)

// Collector handles async metric collection without impacting response times
type Collector struct {
	store    *Store
	recordCh chan RequestRecord
	done     chan struct{}
}

// NewCollector creates a new metrics collector with buffered channel
func NewCollector(store *Store, bufferSize int) *Collector {
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	c := &Collector{
		store:    store,
		recordCh: make(chan RequestRecord, bufferSize),
		done:     make(chan struct{}),
	}
	go c.worker()
	return c
}

// worker processes records asynchronously
func (c *Collector) worker() {
	for {
		select {
		case record := <-c.recordCh:
			if err := c.store.SaveRequest(record); err != nil {
				slog.Error("Failed to save metrics", "error", err, "provider", record.Provider)
			}
		case <-c.done:
			// Drain remaining records before closing
			for {
				select {
				case record := <-c.recordCh:
					if err := c.store.SaveRequest(record); err != nil {
						slog.Error("Failed to save metrics on shutdown", "error", err)
					}
				default:
					return
				}
			}
		}
	}
}

// Record submits a request record for async persistence (non-blocking)
func (c *Collector) Record(provider, model string, statusCode int, responseTime time.Duration, success bool, errorType string) {
	record := RequestRecord{
		Provider:     provider,
		Model:        model,
		StatusCode:   statusCode,
		ResponseTime: responseTime.Milliseconds(),
		Success:      success,
		ErrorType:    errorType,
		CreatedAt:    time.Now().UTC(),
	}

	// Non-blocking send - drop if buffer is full to avoid impacting latency
	select {
	case c.recordCh <- record:
		// Successfully queued
	default:
		slog.Warn("Metrics buffer full, dropping record", "provider", provider)
	}
}

// GetStats returns the current statistics
func (c *Collector) GetStats() (*GlobalStats, error) {
	return c.store.GetGlobalStats()
}

// Close gracefully shuts down the collector
func (c *Collector) Close() error {
	close(c.done)
	return c.store.Close()
}
