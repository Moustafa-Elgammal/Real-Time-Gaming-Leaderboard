# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Start Redis dependency
docker compose up -d

# Run the server
go run main.go

# Build binary
go build -o leaderboard .

# Tidy dependencies
go mod tidy
```

There are no automated tests in this project yet.

## Architecture

Go HTTP service backed by Redis sorted sets. Three-layer layout:

```
main.go                          — loads .env, wires config/store/handler, registers routes
internal/config/config.go        — reads env vars with typed fallbacks
internal/store/redis.go          — Redis client, current-month key logic, data access
internal/handler/leaderboard.go  — Gin HTTP handlers
.env                             — local config (gitignored)
.env.example                     — committed template
```

**Stack:** Gin (HTTP router) + Redis via `go-redis/v9`

**Data model:** Redis sorted sets keyed by `leaderboard-{year}-{month}` (e.g., `leaderboard-2026-6`). The key rotates automatically each month — scores from previous months are not migrated or archived.

**API surface:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/scores` | Top 10 by score (descending via `ZRevRangeWithScores`) |
| GET | `/v1/scores/:username` | User's rank ±4 neighbors via `ZRevRank` + `ZRevRangeWithScores` |
| POST | `/v1/scores/:username` | Increment user score by 1 via `ZIncrBy` |

**Environment variables** (all have defaults, loaded from `.env` via `godotenv`):

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | `` | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `LEADERBOARD_PREFIX` | `leaderboard` | Key prefix for sorted sets |
| `TOP_N` | `10` | Number of entries returned by the top scores endpoint |
| `USER_NEIGHBORHOOD` | `4` | Players shown above and below the queried user |

## Infrastructure

Redis runs via Docker Compose (`docker-compose.yaml`). The app connects to `localhost:6379` with no password and default DB 0.

The `http.http` file contains JetBrains HTTP Client requests for manual testing of all three endpoints.
