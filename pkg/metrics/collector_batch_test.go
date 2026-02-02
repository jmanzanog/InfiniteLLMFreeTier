package metrics

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCollector_BatchFlushAtSize(t *testing.T) {
	originalBatchSize := batchSize
	batchSize = 2
	t.Cleanup(func() { batchSize = originalBatchSize })

	mock := &mockStore{}
	collector := NewCollector(mock, 10)
	defer func() { _ = collector.Close() }()

	collector.Record("p", "m", 200, 10*time.Millisecond, true, "")
	collector.Record("p", "m", 200, 10*time.Millisecond, true, "")

	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&mock.saveCount) < 2 {
		t.Errorf("expected at least 2 records saved, got %d", atomic.LoadInt32(&mock.saveCount))
	}
}

func TestCollector_DrainFlushAtSize(t *testing.T) {
	originalBatchSize := batchSize
	batchSize = 2
	t.Cleanup(func() { batchSize = originalBatchSize })

	collector := &Collector{
		recordCh: make(chan RequestRecord, 5),
	}

	collector.recordCh <- RequestRecord{Provider: "p"}
	collector.recordCh <- RequestRecord{Provider: "p"}
	collector.recordCh <- RequestRecord{Provider: "p"}

	batch := []RequestRecord{}
	flushCount := 0
	flush := func() {
		flushCount++
		batch = batch[:0]
	}

	collector.drain(&batch, flush)

	if flushCount < 2 {
		t.Errorf("expected drain to flush at least twice, got %d", flushCount)
	}
}
