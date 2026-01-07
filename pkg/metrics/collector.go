package metrics

import (
	"log/slog"
	"sync"
	"time"
)

const (
	batchSize    = 50
	batchTimeout = 100 * time.Millisecond
)

// Collector handles async metric collection without impacting response times
type Collector struct {
	store    Store
	recordCh chan RequestRecord
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewCollector creates a new metrics collector with buffered channel
func NewCollector(store Store, bufferSize int) *Collector {
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	c := &Collector{
		store:    store,
		recordCh: make(chan RequestRecord, bufferSize),
		done:     make(chan struct{}),
	}
	c.wg.Add(1)
	go c.worker()
	return c
}

// StartPurger begins a background goroutine that periodically deletes old metrics
func (c *Collector) StartPurger(days int, interval time.Duration) {
	if days <= 0 {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run once at start
		if err := c.store.PurgeOldMetrics(days); err != nil {
			slog.Error("Failed to purge old metrics", "error", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := c.store.PurgeOldMetrics(days); err != nil {
					slog.Error("Failed to purge old metrics", "error", err)
				}
			case <-c.done:
				return
			}
		}
	}()
}

// worker processes records asynchronously using batching
func (c *Collector) worker() {
	defer c.wg.Done()

	var batch []RequestRecord
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := c.store.SaveRequests(batch); err != nil {
			slog.Error("Failed to save metrics batch", "error", err, "count", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case record := <-c.recordCh:
			batch = append(batch, record)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-c.done:
			// Drain remaining records before closing
			for {
				select {
				case record := <-c.recordCh:
					batch = append(batch, record)
					if len(batch) >= batchSize {
						flush()
					}
				default:
					flush()
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
	c.wg.Wait() // Wait for all workers to finish
	return c.store.Close()
}
