package service

import (
	"fmt"
	"log"
	"time"

	"example/real-time-gaming-leaderboard/internal/store"
)

type EventRecorder interface {
	RecordEvent(username string) error
	GetMonthlyScores(year, month int) (map[string]int, error)
}

type ScoreStore interface {
	TopN(n int) ([]store.UserRank, error)
	TopNPage(offset, limit int) ([]store.UserRank, int64, error)
	GetUserNeighborhood(username string) ([]store.UserRank, error)
	IncrementScore(username string) error
	Subscribe() (<-chan store.ScoreEvent, func(), error)
	IsRecovered() bool
	BulkLoad(scores map[string]int) error
}

type LeaderboardService struct {
	mysql EventRecorder
	redis ScoreStore
}

func New(mysql EventRecorder, redis ScoreStore) *LeaderboardService {
	return &LeaderboardService{mysql: mysql, redis: redis}
}

func (s *LeaderboardService) TopN(n int) ([]store.UserRank, error) {
	return s.redis.TopN(n)
}

func (s *LeaderboardService) TopNPage(offset, limit int) ([]store.UserRank, int64, error) {
	return s.redis.TopNPage(offset, limit)
}

func (s *LeaderboardService) Subscribe() (<-chan store.ScoreEvent, func(), error) {
	return s.redis.Subscribe()
}

func (s *LeaderboardService) GetUserNeighborhood(username string) ([]store.UserRank, error) {
	return s.redis.GetUserNeighborhood(username)
}

// IncrementScore writes to MySQL first; Redis is only updated on success.
func (s *LeaderboardService) IncrementScore(username string) error {
	if err := s.mysql.RecordEvent(username); err != nil {
		return err
	}
	return s.redis.IncrementScore(username)
}

// RecoverCurrentMonth rebuilds the current month's Redis leaderboard from MySQL.
// It is a no-op if the recovery marker already exists, making it safe to call
// on every startup regardless of how many instances are running.
func (s *LeaderboardService) RecoverCurrentMonth() error {
	if s.redis.IsRecovered() {
		log.Println("leaderboard: Redis already populated, skipping recovery")
		return nil
	}

	now := time.Now()
	scores, err := s.mysql.GetMonthlyScores(now.Year(), int(now.Month()))
	if err != nil {
		return fmt.Errorf("recovery: fetch scores from MySQL: %w", err)
	}

	if err := s.redis.BulkLoad(scores); err != nil {
		return fmt.Errorf("recovery: bulk load into Redis: %w", err)
	}

	log.Printf("leaderboard: recovered %d players into Redis", len(scores))
	return nil
}
