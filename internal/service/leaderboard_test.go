package service

import (
	"errors"
	"testing"

	"example/real-time-gaming-leaderboard/internal/store"
)

// --- mocks ---

type mockMySQL struct {
	recordEventFunc      func(username string) error
	getMonthlyScoresFunc func(year, month int) (map[string]int, error)
}

func (m *mockMySQL) RecordEvent(username string) error {
	return m.recordEventFunc(username)
}

func (m *mockMySQL) GetMonthlyScores(year, month int) (map[string]int, error) {
	return m.getMonthlyScoresFunc(year, month)
}

type mockRedis struct {
	topNFunc                func(n int) ([]store.UserRank, error)
	topNPageFunc            func(offset, limit int) ([]store.UserRank, int64, error)
	getUserNeighborhoodFunc func(username string) ([]store.UserRank, error)
	incrementScoreFunc      func(username string) error
	subscribeFunc           func() (<-chan store.ScoreEvent, func(), error)
	isRecoveredFunc         func() bool
	bulkLoadFunc            func(scores map[string]int) error
}

func (m *mockRedis) TopN(n int) ([]store.UserRank, error) {
	return m.topNFunc(n)
}

func (m *mockRedis) TopNPage(offset, limit int) ([]store.UserRank, int64, error) {
	return m.topNPageFunc(offset, limit)
}

func (m *mockRedis) Subscribe() (<-chan store.ScoreEvent, func(), error) {
	return m.subscribeFunc()
}

func (m *mockRedis) GetUserNeighborhood(username string) ([]store.UserRank, error) {
	return m.getUserNeighborhoodFunc(username)
}

func (m *mockRedis) IncrementScore(username string) error {
	return m.incrementScoreFunc(username)
}

func (m *mockRedis) IsRecovered() bool {
	return m.isRecoveredFunc()
}

func (m *mockRedis) BulkLoad(scores map[string]int) error {
	return m.bulkLoadFunc(scores)
}

// --- IncrementScore ---

func TestIncrementScore_BothSucceed(t *testing.T) {
	svc := New(
		&mockMySQL{recordEventFunc: func(username string) error { return nil }},
		&mockRedis{incrementScoreFunc: func(username string) error { return nil }},
	)
	if err := svc.IncrementScore("alice"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestIncrementScore_MySQLFails_RedisNotCalled(t *testing.T) {
	redisCalled := false
	svc := New(
		&mockMySQL{recordEventFunc: func(username string) error {
			return errors.New("db error")
		}},
		&mockRedis{incrementScoreFunc: func(username string) error {
			redisCalled = true
			return nil
		}},
	)

	if err := svc.IncrementScore("alice"); err == nil {
		t.Error("expected error, got nil")
	}
	if redisCalled {
		t.Error("Redis must not be called when MySQL fails")
	}
}

func TestIncrementScore_MySQLSucceeds_RedisFails(t *testing.T) {
	svc := New(
		&mockMySQL{recordEventFunc: func(username string) error { return nil }},
		&mockRedis{incrementScoreFunc: func(username string) error {
			return errors.New("redis down")
		}},
	)

	if err := svc.IncrementScore("alice"); err == nil {
		t.Error("expected error when Redis fails, got nil")
	}
}

// --- Read delegation ---

func TestTopN_DelegatesToRedis(t *testing.T) {
	want := []store.UserRank{{Rank: 1, Username: "alice", Score: 100}}
	svc := New(
		&mockMySQL{},
		&mockRedis{topNFunc: func(n int) ([]store.UserRank, error) { return want, nil }},
	)

	got, err := svc.TopN(10)
	if err != nil || len(got) != 1 || got[0].Username != "alice" {
		t.Errorf("TopN: err=%v, got=%+v", err, got)
	}
}

func TestSubscribe_DelegatesToRedis(t *testing.T) {
	ch := make(chan store.ScoreEvent)
	cleanupCalled := false

	svc := New(
		&mockMySQL{},
		&mockRedis{subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return ch, func() { cleanupCalled = true }, nil
		}},
	)

	gotCh, cleanup, err := svc.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if gotCh != ch {
		t.Error("expected the same channel to be returned")
	}
	cleanup()
	if !cleanupCalled {
		t.Error("expected cleanup to be called")
	}
}

func TestTopNPage_DelegatesToRedis(t *testing.T) {
	want := []store.UserRank{{Rank: 11, Username: "bob", Score: 50}}
	svc := New(
		&mockMySQL{},
		&mockRedis{topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			if offset != 10 || limit != 10 {
				t.Errorf("expected offset=10 limit=10, got offset=%d limit=%d", offset, limit)
			}
			return want, 25, nil
		}},
	)

	got, total, err := svc.TopNPage(10, 10)
	if err != nil || len(got) != 1 || got[0].Username != "bob" || total != 25 {
		t.Errorf("TopNPage: err=%v, got=%+v, total=%d", err, got, total)
	}
}

func TestGetUserNeighborhood_DelegatesToRedis(t *testing.T) {
	want := []store.UserRank{{Rank: 2, Username: "alice", Score: 80}}
	svc := New(
		&mockMySQL{},
		&mockRedis{getUserNeighborhoodFunc: func(username string) ([]store.UserRank, error) {
			return want, nil
		}},
	)

	got, err := svc.GetUserNeighborhood("alice")
	if err != nil || len(got) != 1 {
		t.Errorf("GetUserNeighborhood: err=%v, got=%+v", err, got)
	}
}

// --- RecoverCurrentMonth ---

func TestRecoverCurrentMonth_SkipsIfAlreadyRecovered(t *testing.T) {
	mysqlCalled := false
	svc := New(
		&mockMySQL{getMonthlyScoresFunc: func(year, month int) (map[string]int, error) {
			mysqlCalled = true
			return nil, nil
		}},
		&mockRedis{isRecoveredFunc: func() bool { return true }},
	)

	if err := svc.RecoverCurrentMonth(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if mysqlCalled {
		t.Error("MySQL must not be queried when Redis already has the recovery marker")
	}
}

func TestRecoverCurrentMonth_LoadsFromMySQL(t *testing.T) {
	scores := map[string]int{"alice": 100, "bob": 200}
	var loadedScores map[string]int

	svc := New(
		&mockMySQL{getMonthlyScoresFunc: func(year, month int) (map[string]int, error) {
			return scores, nil
		}},
		&mockRedis{
			isRecoveredFunc: func() bool { return false },
			bulkLoadFunc: func(s map[string]int) error {
				loadedScores = s
				return nil
			},
		},
	)

	if err := svc.RecoverCurrentMonth(); err != nil {
		t.Fatalf("RecoverCurrentMonth: %v", err)
	}
	if len(loadedScores) != 2 || loadedScores["alice"] != 100 {
		t.Errorf("unexpected loaded scores: %+v", loadedScores)
	}
}

func TestRecoverCurrentMonth_EmptyMySQL_SetsMarker(t *testing.T) {
	bulkLoadCalled := false
	svc := New(
		&mockMySQL{getMonthlyScoresFunc: func(year, month int) (map[string]int, error) {
			return map[string]int{}, nil
		}},
		&mockRedis{
			isRecoveredFunc: func() bool { return false },
			bulkLoadFunc: func(s map[string]int) error {
				bulkLoadCalled = true
				return nil
			},
		},
	)

	if err := svc.RecoverCurrentMonth(); err != nil {
		t.Fatalf("RecoverCurrentMonth: %v", err)
	}
	if !bulkLoadCalled {
		t.Error("BulkLoad should be called even for empty scores to set the recovery marker")
	}
}

func TestRecoverCurrentMonth_MySQLError_ReturnsError(t *testing.T) {
	svc := New(
		&mockMySQL{getMonthlyScoresFunc: func(year, month int) (map[string]int, error) {
			return nil, errors.New("db error")
		}},
		&mockRedis{isRecoveredFunc: func() bool { return false }},
	)

	if err := svc.RecoverCurrentMonth(); err == nil {
		t.Error("expected error from MySQL failure, got nil")
	}
}

func TestRecoverCurrentMonth_BulkLoadError_ReturnsError(t *testing.T) {
	svc := New(
		&mockMySQL{getMonthlyScoresFunc: func(year, month int) (map[string]int, error) {
			return map[string]int{"alice": 50}, nil
		}},
		&mockRedis{
			isRecoveredFunc: func() bool { return false },
			bulkLoadFunc:    func(s map[string]int) error { return errors.New("redis down") },
		},
	)

	if err := svc.RecoverCurrentMonth(); err == nil {
		t.Error("expected error from BulkLoad failure, got nil")
	}
}
