package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"moustafa-elgammal/real-time-gaming-leaderboard/internal/config"

	"github.com/redis/go-redis/v9"
)

// UserRank represents a single entry in the leaderboard.
type UserRank struct {
	Rank     int    `json:"rank"     example:"1"`
	Username string `json:"username" example:"alice"`
	Score    int    `json:"score"    example:"980"`
}

// ScoreEvent is published on the score-updates channel after every successful increment.
type ScoreEvent struct {
	Username string `json:"username" example:"alice"`
	Score    int    `json:"score"    example:"981"`
	Rank     int    `json:"rank"     example:"1"`
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

func (s *Store) updatesChannel() string {
	return s.leaderboardPrefix + ":score-updates"
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

// IncrementScore increments the player's score by 1 and publishes a ScoreEvent
// to the score-updates pub/sub channel. The publish is best-effort — a failure
// does not fail the increment.
func (s *Store) IncrementScore(username string) error {
	key := s.currentKey()
	pipe := s.client.Pipeline()
	incrCmd := pipe.ZIncrBy(s.ctx, key, 1, username)
	rankCmd := pipe.ZRevRank(s.ctx, key, username)
	if _, err := pipe.Exec(s.ctx); err != nil {
		return err
	}

	event := ScoreEvent{
		Username: username,
		Score:    int(incrCmd.Val()),
		Rank:     int(rankCmd.Val()) + 1,
	}
	if data, err := json.Marshal(event); err == nil {
		s.client.Publish(s.ctx, s.updatesChannel(), string(data))
	}
	return nil
}

// Subscribe returns a channel of ScoreEvents and a cleanup function.
// The caller must call cleanup when done to close the Redis subscription.
func (s *Store) Subscribe() (<-chan ScoreEvent, func(), error) {
	pubsub := s.client.Subscribe(s.ctx, s.updatesChannel())
	ch := make(chan ScoreEvent, 64)

	go func() {
		defer close(ch)
		for msg := range pubsub.Channel() {
			var event ScoreEvent
			if json.Unmarshal([]byte(msg.Payload), &event) != nil {
				continue
			}
			select {
			case ch <- event:
			default: // drop if consumer is slow
			}
		}
	}()

	var once sync.Once
	return ch, func() { once.Do(func() { pubsub.Close() }) }, nil
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
