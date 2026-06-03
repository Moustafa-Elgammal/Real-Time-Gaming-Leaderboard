package config

import (
	"os"
	"strconv"
)

type Config struct {
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	LeaderboardPrefix string
	TopN              int
	UserNeighborhood  int
}

func Load() *Config {
	return &Config{
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnvInt("REDIS_DB", 0),
		LeaderboardPrefix: getEnv("LEADERBOARD_PREFIX", "leaderboard"),
		TopN:              getEnvInt("TOP_N", 10),
		UserNeighborhood:  getEnvInt("USER_NEIGHBORHOOD", 4),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
