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
	t.Setenv("DB_DSN", "")
	t.Setenv("BATCH_FLUSH_MS", "")
	t.Setenv("BATCH_SIZE", "")

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
	if cfg.DBDSN != "root:password@tcp(localhost:3306)/leaderboard?parseTime=true" {
		t.Errorf("DBDSN: got %q, want default DSN", cfg.DBDSN)
	}
	if cfg.BatchFlushMS != 100 {
		t.Errorf("BatchFlushMS: got %d, want 100", cfg.BatchFlushMS)
	}
	if cfg.BatchSize != 500 {
		t.Errorf("BatchSize: got %d, want 500", cfg.BatchSize)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("REDIS_ADDR", "myredis:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("LEADERBOARD_PREFIX", "game")
	t.Setenv("TOP_N", "25")
	t.Setenv("USER_NEIGHBORHOOD", "2")
	t.Setenv("DB_DSN", "user:pass@tcp(mydb:3306)/myapp?parseTime=true")

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
	if cfg.DBDSN != "user:pass@tcp(mydb:3306)/myapp?parseTime=true" {
		t.Errorf("DBDSN: got %q", cfg.DBDSN)
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
