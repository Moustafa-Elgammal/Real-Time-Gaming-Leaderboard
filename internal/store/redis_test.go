package store

import (
	"testing"

	"example/real-time-gaming-leaderboard/internal/config"

	"github.com/alicebob/miniredis/v2"
)

func newTestStore(t *testing.T, neighborhood int) *Store {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(&config.Config{
		RedisAddr:         mr.Addr(),
		LeaderboardPrefix: "test",
		UserNeighborhood:  neighborhood,
	})
}

// --- IncrementScore ---

func TestIncrementScore_CreatesAndIncrementsPlayer(t *testing.T) {
	s := newTestStore(t, 4)

	if err := s.IncrementScore("alice"); err != nil {
		t.Fatalf("first increment: %v", err)
	}
	if err := s.IncrementScore("alice"); err != nil {
		t.Fatalf("second increment: %v", err)
	}

	ranks, err := s.TopN(10)
	if err != nil {
		t.Fatalf("TopN: %v", err)
	}
	if len(ranks) != 1 || ranks[0].Username != "alice" || ranks[0].Score != 2 {
		t.Errorf("unexpected ranks: %+v", ranks)
	}
}

// --- TopN ---

func TestTopN_ReturnsHighestScoresDescending(t *testing.T) {
	s := newTestStore(t, 4)

	players := map[string]int{"alice": 100, "bob": 300, "carol": 200}
	for name, score := range players {
		for i := 0; i < score; i++ {
			s.IncrementScore(name)
		}
	}

	ranks, err := s.TopN(3)
	if err != nil {
		t.Fatalf("TopN: %v", err)
	}
	if len(ranks) != 3 {
		t.Fatalf("expected 3 results, got %d", len(ranks))
	}
	if ranks[0].Username != "bob" || ranks[1].Username != "carol" || ranks[2].Username != "alice" {
		t.Errorf("wrong order: %+v", ranks)
	}
	if ranks[0].Rank != 1 || ranks[1].Rank != 2 || ranks[2].Rank != 3 {
		t.Errorf("wrong ranks: %+v", ranks)
	}
}

func TestTopN_LimitRespected(t *testing.T) {
	s := newTestStore(t, 4)

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		s.IncrementScore(name)
	}

	ranks, err := s.TopN(3)
	if err != nil {
		t.Fatalf("TopN: %v", err)
	}
	if len(ranks) != 3 {
		t.Errorf("expected 3 results, got %d", len(ranks))
	}
}

func TestTopN_EmptyLeaderboard_ReturnsEmptySlice(t *testing.T) {
	s := newTestStore(t, 4)

	ranks, err := s.TopN(10)
	if err != nil {
		t.Fatalf("TopN: %v", err)
	}
	if len(ranks) != 0 {
		t.Errorf("expected empty, got %+v", ranks)
	}
}

// --- TopNPage ---

func TestTopNPage_FirstPage(t *testing.T) {
	s := newTestStore(t, 4)

	for _, name := range []string{"p1", "p2", "p3", "p4", "p5"} {
		for i := 0; i < 10; i++ {
			s.IncrementScore(name)
		}
	}
	// Give p1 the highest score so ordering is deterministic.
	for i := 0; i < 50; i++ {
		s.IncrementScore("p1")
	}

	ranks, total, err := s.TopNPage(0, 3)
	if err != nil {
		t.Fatalf("TopNPage: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(ranks) != 3 {
		t.Fatalf("expected 3 results, got %d", len(ranks))
	}
	if ranks[0].Rank != 1 || ranks[1].Rank != 2 || ranks[2].Rank != 3 {
		t.Errorf("wrong rank values: %+v", ranks)
	}
}

func TestTopNPage_SecondPage(t *testing.T) {
	s := newTestStore(t, 4)

	scores := map[string]int{"p1": 50, "p2": 40, "p3": 30, "p4": 20, "p5": 10}
	for name, score := range scores {
		for i := 0; i < score; i++ {
			s.IncrementScore(name)
		}
	}

	ranks, total, err := s.TopNPage(3, 3)
	if err != nil {
		t.Fatalf("TopNPage: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(ranks) != 2 {
		t.Fatalf("expected 2 results (page beyond end), got %d", len(ranks))
	}
	if ranks[0].Rank != 4 || ranks[1].Rank != 5 {
		t.Errorf("wrong ranks on second page: %+v", ranks)
	}
}

func TestTopNPage_OffsetBeyondEnd_ReturnsEmpty(t *testing.T) {
	s := newTestStore(t, 4)
	s.IncrementScore("only")

	ranks, total, err := s.TopNPage(10, 5)
	if err != nil {
		t.Fatalf("TopNPage: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(ranks) != 0 {
		t.Errorf("expected empty slice, got %+v", ranks)
	}
}

func TestTopNPage_EmptyLeaderboard(t *testing.T) {
	s := newTestStore(t, 4)

	ranks, total, err := s.TopNPage(0, 10)
	if err != nil {
		t.Fatalf("TopNPage: %v", err)
	}
	if total != 0 || len(ranks) != 0 {
		t.Errorf("expected empty result, got ranks=%+v total=%d", ranks, total)
	}
}

// --- GetUserNeighborhood ---

func TestGetUserNeighborhood_ReturnsCorrectWindow(t *testing.T) {
	s := newTestStore(t, 2)

	// Scores: p1=50 p2=40 p3=30 p4=20 p5=10  (rank 1..5)
	scores := map[string]int{"p1": 50, "p2": 40, "p3": 30, "p4": 20, "p5": 10}
	for name, score := range scores {
		for i := 0; i < score; i++ {
			s.IncrementScore(name)
		}
	}

	// p3 is rank 3 — neighborhood of 2 should return ranks 1..5
	ranks, err := s.GetUserNeighborhood("p3")
	if err != nil {
		t.Fatalf("GetUserNeighborhood: %v", err)
	}
	if len(ranks) != 5 {
		t.Errorf("expected 5 entries, got %d: %+v", len(ranks), ranks)
	}
	if ranks[0].Username != "p1" {
		t.Errorf("expected p1 first, got %q", ranks[0].Username)
	}
}

func TestGetUserNeighborhood_TopPlayer_NoClamping(t *testing.T) {
	s := newTestStore(t, 4)

	for _, name := range []string{"p1", "p2", "p3"} {
		s.IncrementScore(name)
	}
	// Give p1 the highest score
	for i := 0; i < 10; i++ {
		s.IncrementScore("p1")
	}

	// p1 is rank 1 — start should clamp to 0
	ranks, err := s.GetUserNeighborhood("p1")
	if err != nil {
		t.Fatalf("GetUserNeighborhood: %v", err)
	}
	if ranks[0].Rank != 1 {
		t.Errorf("expected rank 1 first, got %d", ranks[0].Rank)
	}
}

func TestGetUserNeighborhood_UnknownUser_ReturnsError(t *testing.T) {
	s := newTestStore(t, 4)

	_, err := s.GetUserNeighborhood("ghost")
	if err == nil {
		t.Error("expected error for unknown user, got nil")
	}
}

// --- BulkLoad and IsRecovered ---

func TestIsRecovered_FalseOnFreshStore(t *testing.T) {
	s := newTestStore(t, 4)
	if s.IsRecovered() {
		t.Error("expected false on a fresh store, got true")
	}
}

func TestBulkLoad_SetsScoresAndMarker(t *testing.T) {
	s := newTestStore(t, 4)

	scores := map[string]int{"alice": 100, "bob": 200}
	if err := s.BulkLoad(scores); err != nil {
		t.Fatalf("BulkLoad: %v", err)
	}

	if !s.IsRecovered() {
		t.Error("expected IsRecovered to be true after BulkLoad")
	}

	ranks, err := s.TopN(10)
	if err != nil {
		t.Fatalf("TopN after BulkLoad: %v", err)
	}
	if len(ranks) != 2 {
		t.Fatalf("expected 2 players, got %d", len(ranks))
	}
	if ranks[0].Username != "bob" || ranks[0].Score != 200 {
		t.Errorf("expected bob first with 200, got %+v", ranks[0])
	}
	if ranks[1].Username != "alice" || ranks[1].Score != 100 {
		t.Errorf("expected alice second with 100, got %+v", ranks[1])
	}
}

func TestBulkLoad_EmptyScores_SetsMarkerOnly(t *testing.T) {
	s := newTestStore(t, 4)

	if err := s.BulkLoad(map[string]int{}); err != nil {
		t.Fatalf("BulkLoad empty: %v", err)
	}
	if !s.IsRecovered() {
		t.Error("expected recovery marker even for empty load")
	}

	ranks, err := s.TopN(10)
	if err != nil {
		t.Fatalf("TopN: %v", err)
	}
	if len(ranks) != 0 {
		t.Errorf("expected empty leaderboard, got %+v", ranks)
	}
}
