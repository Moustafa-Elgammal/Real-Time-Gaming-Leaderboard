package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("LEADERBOARD_PREFIX", "")
	t.Setenv("TOP_N", "")
	t.Setenv("USER_NEIGHBORHOOD", "")

	cfg := Load()

	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("RedisAddr: got %q, want %q", cfg.RedisAddr, "localhost:6379")
	}
	if cfg.RedisPassword != "" {
		t.Errorf("RedisPassword: got %q, want empty", cfg.RedisPassword)
	}
	if cfg.RedisDB != 0 {
		t.Errorf("RedisDB: got %d, want 0", cfg.RedisDB)
	}
	if cfg.LeaderboardPrefix != "leaderboard" {
		t.Errorf("LeaderboardPrefix: got %q, want %q", cfg.LeaderboardPrefix, "leaderboard")
	}
	if cfg.TopN != 10 {
		t.Errorf("TopN: got %d, want 10", cfg.TopN)
	}
	if cfg.UserNeighborhood != 4 {
		t.Errorf("UserNeighborhood: got %d, want 4", cfg.UserNeighborhood)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("REDIS_ADDR", "myredis:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("LEADERBOARD_PREFIX", "game")
	t.Setenv("TOP_N", "25")
	t.Setenv("USER_NEIGHBORHOOD", "2")

	cfg := Load()

	if cfg.RedisAddr != "myredis:6380" {
		t.Errorf("RedisAddr: got %q, want %q", cfg.RedisAddr, "myredis:6380")
	}
	if cfg.RedisPassword != "secret" {
		t.Errorf("RedisPassword: got %q, want %q", cfg.RedisPassword, "secret")
	}
	if cfg.RedisDB != 2 {
		t.Errorf("RedisDB: got %d, want 2", cfg.RedisDB)
	}
	if cfg.LeaderboardPrefix != "game" {
		t.Errorf("LeaderboardPrefix: got %q, want %q", cfg.LeaderboardPrefix, "game")
	}
	if cfg.TopN != 25 {
		t.Errorf("TopN: got %d, want 25", cfg.TopN)
	}
	if cfg.UserNeighborhood != 2 {
		t.Errorf("UserNeighborhood: got %d, want 2", cfg.UserNeighborhood)
	}
}

func TestLoad_InvalidIntFallsBackToDefault(t *testing.T) {
	t.Setenv("TOP_N", "not-a-number")
	t.Setenv("USER_NEIGHBORHOOD", "")
	t.Setenv("REDIS_DB", "")

	cfg := Load()

	if cfg.TopN != 10 {
		t.Errorf("TopN: got %d, want default 10 on invalid input", cfg.TopN)
	}
}
