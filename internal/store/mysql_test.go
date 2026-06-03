package store

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newMockMySQL(t *testing.T) (*MySQLStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &MySQLStore{db: db}, mock
}

// --- RecordEvent ---

func TestRecordEvent_InsertsRow(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectExec("INSERT INTO score_events").
		WithArgs("alice", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.RecordEvent("alice"); err != nil {
		t.Errorf("RecordEvent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRecordEvent_DBError_ReturnsError(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectExec("INSERT INTO score_events").
		WithArgs("alice", 1).
		WillReturnError(errors.New("connection refused"))

	if err := s.RecordEvent("alice"); err == nil {
		t.Error("expected error, got nil")
	}
}

// --- RecordBatch ---

func TestRecordBatch_InsertsAllRowsInOneQuery(t *testing.T) {
	s, mock := newMockMySQL(t)

	// Usernames are sorted by RecordBatch: alice before bob
	mock.ExpectExec("INSERT INTO score_events").
		WithArgs("alice", 3, "bob", 5).
		WillReturnResult(sqlmock.NewResult(2, 2))

	if err := s.RecordBatch(map[string]int{"bob": 5, "alice": 3}); err != nil {
		t.Errorf("RecordBatch: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRecordBatch_EmptyMap_NoQuery(t *testing.T) {
	s, mock := newMockMySQL(t)
	// No expectations set — any query would fail the test
	if err := s.RecordBatch(map[string]int{}); err != nil {
		t.Errorf("RecordBatch empty: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRecordBatch_DBError_ReturnsError(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectExec("INSERT INTO score_events").
		WillReturnError(errors.New("deadlock"))

	if err := s.RecordBatch(map[string]int{"alice": 1}); err == nil {
		t.Error("expected error, got nil")
	}
}

// --- GetMonthlyScores ---

func TestGetMonthlyScores_UsesDateRange(t *testing.T) {
	s, mock := newMockMySQL(t)

	rows := sqlmock.NewRows([]string{"username", "total"}).
		AddRow("alice", 50).
		AddRow("bob", 30)
	// Query must use >= / < range, not YEAR()/MONTH() functions
	mock.ExpectQuery("WHERE created_at >= . AND created_at <").
		WillReturnRows(rows)

	scores, err := s.GetMonthlyScores(2026, 6)
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if scores["alice"] != 50 || scores["bob"] != 30 {
		t.Errorf("unexpected scores: %+v", scores)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetMonthlyScores_EmptyMonth_ReturnsEmptyMap(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectQuery("WHERE created_at >= . AND created_at <").
		WillReturnRows(sqlmock.NewRows([]string{"username", "total"}))

	scores, err := s.GetMonthlyScores(2026, 1)
	if err != nil {
		t.Fatalf("GetMonthlyScores: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("expected empty map, got %+v", scores)
	}
}

func TestGetMonthlyScores_DBError_ReturnsError(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectQuery("WHERE created_at >= . AND created_at <").
		WillReturnError(errors.New("query failed"))

	if _, err := s.GetMonthlyScores(2026, 6); err == nil {
		t.Error("expected error, got nil")
	}
}

// --- EnsureMonthPartition ---

func TestEnsureMonthPartition_SkipsIfAlreadyExists(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("p2026_06").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// No ALTER TABLE expected

	if err := s.EnsureMonthPartition(2026, 6); err != nil {
		t.Errorf("EnsureMonthPartition: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestEnsureMonthPartition_CreatesPartitionWhenMissing(t *testing.T) {
	s, mock := newMockMySQL(t)

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("p2026_06").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectExec("ALTER TABLE score_events REORGANIZE PARTITION").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := s.EnsureMonthPartition(2026, 6); err != nil {
		t.Errorf("EnsureMonthPartition: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
