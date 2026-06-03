package store

import (
	"sync"
	"testing"
	"time"
)

// mockBatchStore records all batches written to it.
type mockBatchStore struct {
	mu      sync.Mutex
	batches []map[string]int
}

func (m *mockBatchStore) RecordBatch(events map[string]int) error {
	cp := make(map[string]int, len(events))
	for k, v := range events {
		cp[k] = v
	}
	m.mu.Lock()
	m.batches = append(m.batches, cp)
	m.mu.Unlock()
	return nil
}

func (m *mockBatchStore) GetMonthlyScores(year, month int) (map[string]int, error) {
	return map[string]int{"alice": 10}, nil
}

func (m *mockBatchStore) batchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func (m *mockBatchStore) allEvents() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	merged := make(map[string]int)
	for _, b := range m.batches {
		for u, d := range b {
			merged[u] += d
		}
	}
	return merged
}

func newTestBatcher(t *testing.T, store *mockBatchStore, flushMS, batchSize int) *EventBatcher {
	t.Helper()
	b := &EventBatcher{
		store:         store,
		buffer:        make(map[string]int),
		flushInterval: time.Duration(flushMS) * time.Millisecond,
		batchSize:     batchSize,
		stop:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	go b.flushLoop()
	t.Cleanup(b.Stop)
	return b
}

func TestBatcher_BuffersWithoutFlush(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 10000, 1000) // large interval and size — no auto-flush

	b.RecordEvent("alice")
	b.RecordEvent("alice")
	b.RecordEvent("bob")

	if mock.batchCount() != 0 {
		t.Errorf("expected 0 flushes before threshold, got %d", mock.batchCount())
	}
}

func TestBatcher_FlushesWhenBatchSizeReached(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 10000, 3) // flush every 3 events

	b.RecordEvent("alice")
	b.RecordEvent("bob")
	b.RecordEvent("carol") // 3rd event triggers flush

	if mock.batchCount() == 0 {
		t.Error("expected flush after batchSize reached, got none")
	}
}

func TestBatcher_AggregatesScoresBeforeFlush(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 10000, 1000)

	for range 5 {
		b.RecordEvent("alice")
	}
	b.Stop()

	events := mock.allEvents()
	if events["alice"] != 5 {
		t.Errorf("expected alice=5, got %d", events["alice"])
	}
}

func TestBatcher_PeriodicFlush(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 20, 10000) // flush every 20ms, size threshold very high

	b.RecordEvent("alice")

	time.Sleep(60 * time.Millisecond) // wait for at least one tick

	if mock.batchCount() == 0 {
		t.Error("expected periodic flush, got none")
	}
}

func TestBatcher_StopFlushesRemaining(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 10000, 10000) // large interval and size — only Stop triggers flush

	b.RecordEvent("alice")
	b.RecordEvent("bob")
	b.Stop() // explicit stop; t.Cleanup stop is a no-op due to sync.Once

	if mock.batchCount() == 0 {
		t.Error("expected Stop to flush remaining events")
	}
	events := mock.allEvents()
	if events["alice"] != 1 || events["bob"] != 1 {
		t.Errorf("unexpected events after Stop: %+v", events)
	}
}

func TestBatcher_GetMonthlyScoresDelegates(t *testing.T) {
	mock := &mockBatchStore{}
	b := newTestBatcher(t, mock, 10000, 1000)

	scores, err := b.GetMonthlyScores(2026, 6)
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if scores["alice"] != 10 {
		t.Errorf("expected alice=10 from mock, got %+v", scores)
	}
}
