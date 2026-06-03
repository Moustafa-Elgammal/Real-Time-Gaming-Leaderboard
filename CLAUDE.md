# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Start all dependencies (Redis + MySQL)
docker compose up -d redis mysql

# Run the server locally
go run .

# Build binary
go build -o leaderboard .

# Run all tests (no real Redis or MySQL required)
go test ./...

# Run a single package's tests
go test ./internal/store/...
go test ./internal/service/...
go test ./internal/config/...

# Run tests via Docker (matches CI)
docker compose --profile test run --rm test

# Tidy dependencies
go mod tidy
```

## Architecture

Internal-only Go HTTP service backed by Redis (live rankings) and MySQL (durable history). It is designed to sit behind a **game service** that handles player authentication, session management, anti-cheat, and per-player rate limiting. The leaderboard service trusts all requests that reach it; the only boundary it enforces is verifying the caller is the game service via a shared secret.

```
main.go                              — loads .env, wires mysql → batcher → redis → service → handler
internal/config/config.go            — all env vars with typed defaults
internal/middleware/auth.go          — InternalAuth: checks X-Internal-Token header (no-op when INTERNAL_API_KEY is empty)
internal/store/redis.go              — Redis sorted sets: TopN, GetUserNeighborhood, IncrementScore, BulkLoad, IsRecovered
internal/store/mysql.go              — MySQL: migrate, EnsureMonthPartition, RecordBatch, GetMonthlyScores
internal/store/batcher.go            — in-memory EventBatcher: aggregates events, flushes bulk to MySQL
internal/service/leaderboard.go      — coordinator: MySQL-first write order, RecoverCurrentMonth
internal/handler/leaderboard.go      — Gin HTTP handlers (TopN, GetUserRank, UpdatePlayerScore)
```

**Stack:** Gin · go-redis/v9 · go-sql-driver/mysql · godotenv

### Write path

```
POST /v1/scores/:username
        │
        ├─ EventBatcher.RecordEvent   buffer[username]++
        │       └─ at 500 events or every 100ms → MySQLStore.RecordBatch (multi-value INSERT)
        │
        └─ RedisStore.IncrementScore  ZINCRBY (immediate, live ranking)
```

MySQL write is attempted first; Redis is only updated on success.

### Read path

All reads go directly to Redis sorted sets — no MySQL involved.

### Startup recovery

On boot, `service.RecoverCurrentMonth()` checks for a `{leaderboard-key}:recovered` marker in Redis. If absent it queries `MySQLStore.GetMonthlyScores` for the current month and calls `RedisStore.BulkLoad` (pipeline ZADD + set marker). This rebuilds the Redis leaderboard from MySQL history after a restart without re-scanning on subsequent boots.

### Graceful shutdown

`main.go` listens for `SIGINT`/`SIGTERM`, calls `batcher.Stop()` (flushes remaining buffer to MySQL), then `http.Server.Shutdown`. `EventBatcher.Stop()` is idempotent via `sync.Once`.

## Data Model

### Redis

Sorted sets keyed by `{LEADERBOARD_PREFIX}-{year}-{month}` (e.g. `leaderboard-2026-6`). Key rotates each month; old keys are not deleted. Recovery marker: `{leaderboard-key}:recovered`.

### MySQL — `score_events`

```sql
CREATE TABLE score_events (
  id         BIGINT NOT NULL AUTO_INCREMENT,
  username   VARCHAR(255) NOT NULL,
  delta      INT NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id, created_at),
  INDEX idx_created_at (created_at)
)
PARTITION BY RANGE COLUMNS(created_at) (
  PARTITION p_future VALUES LESS THAN (MAXVALUE)
);
```

- No `idx_username` — random I/O cost is prohibitive at scale.
- `created_at` in PK satisfies InnoDB's partition column requirement.
- Monthly partitions (e.g. `p2026_06`) are carved from `p_future` via `REORGANIZE PARTITION` on startup for current and next month.
- `GetMonthlyScores` uses `created_at >= ? AND created_at < ?` (date range, not `YEAR()`/`MONTH()` functions) so the index and partition pruning both apply.
- `RecordBatch` sorts usernames before building the INSERT for deterministic queries.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/scores` | Top `TOP_N` players, score descending |
| GET | `/v1/scores/:username` | Player + `USER_NEIGHBORHOOD` above/below |
| POST | `/v1/scores/:username` | Increment score by 1 (MySQL first, then Redis) |

## Environment Variables

All have defaults. Loaded from `.env` via `godotenv`; Docker Compose overrides `REDIS_ADDR` and `DB_DSN` automatically.

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | `` | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `LEADERBOARD_PREFIX` | `leaderboard` | Key prefix for sorted sets |
| `TOP_N` | `10` | Entries returned by top scores endpoint |
| `USER_NEIGHBORHOOD` | `4` | Players shown above and below queried user |
| `DB_DSN` | `root:password@tcp(localhost:3306)/leaderboard?parseTime=true` | MySQL DSN |
| `BATCH_FLUSH_MS` | `100` | Batcher flush interval in milliseconds |
| `BATCH_SIZE` | `500` | Batcher buffer size that triggers immediate flush |
| `INTERNAL_API_KEY` | `` | Shared secret with the game service; empty = auth disabled |

## Testing

- **Redis tests** (`store/redis_test.go`) — use `miniredis.RunT(t)`, no real Redis.
- **MySQL tests** (`store/mysql_test.go`) — use `go-sqlmock`, no real MySQL.
- **Batcher tests** (`store/batcher_test.go`) — use `mockBatchStore`, no real stores.
- **Service tests** (`service/leaderboard_test.go`) — mock structs with function fields for both stores.
- **Config tests** (`config/config_test.go`) — set env vars via `t.Setenv`, test defaults/overrides/invalid int fallback.
- **Middleware tests** (`middleware/auth_test.go`) — tests disabled (empty key), correct token, wrong token, and missing token cases.

`EventBatcher.Stop()` is idempotent; tests that call `Stop()` explicitly are safe because `t.Cleanup(b.Stop)` is also registered via `newTestBatcher`.

## Infrastructure

Docker Compose (`docker-compose.yaml`) runs four services:

| Service | Notes |
|---------|-------|
| `redis` | `redis:7-alpine`, port 6379 |
| `mysql` | `mysql:8`, port 3306, healthcheck via `mysqladmin ping` |
| `app` | depends on `redis` (started) + `mysql` (healthy), port 8080 |
| `test` | profile `test`, runs `go test -v ./...` against mock stores |

The `http.http` file contains JetBrains HTTP Client requests for manual endpoint testing.

## Coding Conventions

- All layers communicate via interfaces (handler `Storer`, service `EventRecorder`/`ScoreStore`, batcher `batchStore`) — enables unit testing without real infrastructure.
- Never add `Co-Authored-By` lines to commits.
- Commit identity: Moustafa Elgammal / moustafa_algammal@yahoo.com.
