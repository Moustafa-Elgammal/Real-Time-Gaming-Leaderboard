package store

import (
	"log"
	"sync"
	"time"

	"example/real-time-gaming-leaderboard/internal/config"
)

// batchStore is the subset of MySQLStore the batcher needs.
type batchStore interface {
	RecordBatch(events map[string]int) error
	GetMonthlyScores(year, month int) (map[string]int, error)
}

// EventBatcher buffers score events in memory and flushes them to MySQL in
// bulk on a fixed interval or when the buffer reaches a size threshold.
// This reduces write amplification from 5 000 individual inserts/s to a
// handful of multi-value inserts per second.
//
// Trade-off: Redis is updated immediately (live ranking stays current);
// MySQL receives events with up to BatchFlushMS latency (history).
type EventBatcher struct {
	store         batchStore
	mu            sync.Mutex
	buffer        map[string]int
	flushInterval time.Duration
	batchSize     int
	stop          chan struct{}
	stopped       chan struct{}
	stopOnce      sync.Once
}

func NewEventBatcher(store *MySQLStore, cfg *config.Config) *EventBatcher {
	b := &EventBatcher{
		store:         store,
		buffer:        make(map[string]int),
		flushInterval: time.Duration(cfg.BatchFlushMS) * time.Millisecond,
		batchSize:     cfg.BatchSize,
		stop:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	go b.flushLoop()
	return b
}

// RecordEvent adds one point to the username's buffer entry.
// It triggers a synchronous flush if the buffer hits the size threshold.
func (b *EventBatcher) RecordEvent(username string) error {
	b.mu.Lock()
	b.buffer[username]++
	flush := len(b.buffer) >= b.batchSize
	b.mu.Unlock()

	if flush {
		return b.flush()
	}
	return nil
}

// GetMonthlyScores delegates directly to the underlying store.
func (b *EventBatcher) GetMonthlyScores(year, month int) (map[string]int, error) {
	return b.store.GetMonthlyScores(year, month)
}

// Stop signals the flush loop to exit and waits for the final flush to complete.
// Safe to call multiple times. Call during graceful shutdown to avoid losing buffered events.
func (b *EventBatcher) Stop() {
	b.stopOnce.Do(func() {
		close(b.stop)
		<-b.stopped
	})
}

func (b *EventBatcher) flushLoop() {
	defer close(b.stopped)
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := b.flush(); err != nil {
				log.Printf("batcher: periodic flush error: %v", err)
			}
		case <-b.stop:
			if err := b.flush(); err != nil {
				log.Printf("batcher: final flush error: %v", err)
			}
			return
		}
	}
}

// flush atomically drains the buffer and writes a single batch INSERT.
func (b *EventBatcher) flush() error {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := b.buffer
	b.buffer = make(map[string]int)
	b.mu.Unlock()

	return b.store.RecordBatch(batch)
}
