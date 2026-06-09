//go:build integration

// Run with: go test -tags integration ./internal/store/... -v
// Requires a running MySQL instance. Set TEST_MYSQL_DSN to override the default.
package store

import (
	"os"
	"testing"
	"time"

	"moustafa-elgammal/real-time-gaming-leaderboard/internal/config"
)

func integrationDSN() string {
	if dsn := os.Getenv("TEST_MYSQL_DSN"); dsn != "" {
		return dsn
	}
	return "root:password@tcp(localhost:3306)/leaderboard_test?parseTime=true"
}

func newIntegrationMySQL(t *testing.T) *MySQLStore {
	t.Helper()
	cfg := &config.Config{DBDSN: integrationDSN()}
	s, err := NewMySQL(cfg)
	if err != nil {
		t.Skipf("MySQL unavailable (%v) — skipping integration test", err)
	}
	t.Cleanup(func() {
		s.db.Exec("DROP TABLE IF EXISTS score_events")
		s.db.Close()
	})
	return s
}

func TestIntegration_NewMySQL_MigratesSchema(t *testing.T) {
	s := newIntegrationMySQL(t)

	// Table must exist after NewMySQL succeeds.
	var tableName string
	err := s.db.QueryRow("SHOW TABLES LIKE 'score_events'").Scan(&tableName)
	if err != nil || tableName != "score_events" {
		t.Fatalf("table score_events not found after migrate: %v", err)
	}
}

func TestIntegration_NewMySQL_CreatesMonthPartitions(t *testing.T) {
	s := newIntegrationMySQL(t)

	now := time.Now()
	currentPartition := partitionName(now.Year(), int(now.Month()))
	next := now.AddDate(0, 1, 0)
	nextPartition := partitionName(next.Year(), int(next.Month()))

	for _, name := range []string{currentPartition, nextPartition} {
		var count int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM information_schema.PARTITIONS
			 WHERE TABLE_SCHEMA = DATABASE()
			   AND TABLE_NAME   = 'score_events'
			   AND PARTITION_NAME = ?`, name,
		).Scan(&count)
		if err != nil || count == 0 {
			t.Errorf("expected partition %s to exist, count=%d err=%v", name, count, err)
		}
	}
}

func TestIntegration_RecordEventAndQuery(t *testing.T) {
	s := newIntegrationMySQL(t)

	if err := s.RecordEvent("alice"); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := s.RecordEvent("alice"); err != nil {
		t.Fatalf("RecordEvent second: %v", err)
	}
	if err := s.RecordEvent("bob"); err != nil {
		t.Fatalf("RecordEvent bob: %v", err)
	}

	now := time.Now()
	scores, err := s.GetMonthlyScores(now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if scores["alice"] != 2 {
		t.Errorf("alice: got %d, want 2", scores["alice"])
	}
	if scores["bob"] != 1 {
		t.Errorf("bob: got %d, want 1", scores["bob"])
	}
}

func TestIntegration_RecordBatchAndQuery(t *testing.T) {
	s := newIntegrationMySQL(t)

	batch := map[string]int{"carol": 10, "dave": 5}
	if err := s.RecordBatch(batch); err != nil {
		t.Fatalf("RecordBatch: %v", err)
	}

	now := time.Now()
	scores, err := s.GetMonthlyScores(now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if scores["carol"] != 10 {
		t.Errorf("carol: got %d, want 10", scores["carol"])
	}
	if scores["dave"] != 5 {
		t.Errorf("dave: got %d, want 5", scores["dave"])
	}
}

func TestIntegration_GetMonthlyScores_ExcludesPreviousMonth(t *testing.T) {
	s := newIntegrationMySQL(t)

	// Insert a row backdated to last month directly to bypass the DEFAULT.
	now := time.Now()
	lastMonth := now.AddDate(0, -1, 0)
	_, err := s.db.Exec(
		"INSERT INTO score_events (username, delta, created_at) VALUES (?, ?, ?)",
		"ghost", 99, lastMonth,
	)
	if err != nil {
		t.Fatalf("backdated insert: %v", err)
	}

	scores, err := s.GetMonthlyScores(now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if _, found := scores["ghost"]; found {
		t.Error("GetMonthlyScores returned a score from the previous month")
	}
}

func TestIntegration_EnsureMonthPartition_Idempotent(t *testing.T) {
	s := newIntegrationMySQL(t)

	now := time.Now()
	// First call already happened inside NewMySQL; calling again must not error.
	if err := s.EnsureMonthPartition(now.Year(), int(now.Month())); err != nil {
		t.Errorf("second EnsureMonthPartition call returned error: %v", err)
	}
}
