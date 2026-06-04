package store

import (
	"context"
	"fmt"
	"time"

	"example/real-time-gaming-leaderboard/internal/config"

	"github.com/redis/go-redis/v9"
)

// UserRank represents a single entry in the leaderboard.
type UserRank struct {
	Rank     int    `json:"rank"     example:"1"`
	Username string `json:"username" example:"alice"`
	Score    int    `json:"score"    example:"980"`
}

type Store struct {
	client            *redis.Client
	ctx               context.Context
	leaderboardPrefix string
	userNeighborhood  int
}

func New(cfg *config.Config) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
			Protocol: 2,
		}),
		ctx:               context.Background(),
		leaderboardPrefix: cfg.LeaderboardPrefix,
		userNeighborhood:  cfg.UserNeighborhood,
	}
}

func (s *Store) currentKey() string {
	now := time.Now()
	return fmt.Sprintf("%s-%d-%d", s.leaderboardPrefix, now.Year(), now.Month())
}

func (s *Store) TopN(n int) ([]UserRank, error) {
	results, err := s.client.ZRevRangeWithScores(s.ctx, s.currentKey(), 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}

	ranks := make([]UserRank, len(results))
	for i, z := range results {
		ranks[i] = UserRank{
			Rank:     i + 1,
			Username: z.Member.(string),
			Score:    int(z.Score),
		}
	}
	return ranks, nil
}

// TopNPage returns a page of players starting at offset (0-based) with the
// given limit, plus the total number of players on the leaderboard.
func (s *Store) TopNPage(offset, limit int) ([]UserRank, int64, error) {
	key := s.currentKey()
	pipe := s.client.Pipeline()
	rangeCmd := pipe.ZRevRangeWithScores(s.ctx, key, int64(offset), int64(offset+limit-1))
	cardCmd := pipe.ZCard(s.ctx, key)
	if _, err := pipe.Exec(s.ctx); err != nil {
		return nil, 0, err
	}

	results := rangeCmd.Val()
	total := cardCmd.Val()

	ranks := make([]UserRank, len(results))
	for i, z := range results {
		ranks[i] = UserRank{
			Rank:     offset + i + 1,
			Username: z.Member.(string),
			Score:    int(z.Score),
		}
	}
	return ranks, total, nil
}

// GetUserNeighborhood returns the user's rank along with the surrounding players.
func (s *Store) GetUserNeighborhood(username string) ([]UserRank, error) {
	pos, err := s.client.ZRevRank(s.ctx, s.currentKey(), username).Result()
	if err != nil {
		return nil, err
	}

	start := pos - int64(s.userNeighborhood)
	if start < 0 {
		start = 0
	}
	end := pos + int64(s.userNeighborhood)

	results, err := s.client.ZRevRangeWithScores(s.ctx, s.currentKey(), start, end).Result()
	if err != nil {
		return nil, err
	}

	ranks := make([]UserRank, len(results))
	for i, z := range results {
		ranks[i] = UserRank{
			Rank:     int(start) + i + 1,
			Username: z.Member.(string),
			Score:    int(z.Score),
		}
	}
	return ranks, nil
}

func (s *Store) IncrementScore(username string) error {
	return s.client.ZIncrBy(s.ctx, s.currentKey(), 1, username).Err()
}

func (s *Store) IsRecovered() bool {
	val, err := s.client.Exists(s.ctx, s.currentKey()+":recovered").Result()
	return err == nil && val > 0
}

// BulkLoad writes all scores into the sorted set and sets a recovery marker
// in a single pipeline. Safe to call with an empty map (marker is still set).
func (s *Store) BulkLoad(scores map[string]int) error {
	pipe := s.client.Pipeline()
	for username, score := range scores {
		pipe.ZAdd(s.ctx, s.currentKey(), redis.Z{Score: float64(score), Member: username})
	}
	pipe.Set(s.ctx, s.currentKey()+":recovered", 1, 0)
	_, err := pipe.Exec(s.ctx)
	return err
}
