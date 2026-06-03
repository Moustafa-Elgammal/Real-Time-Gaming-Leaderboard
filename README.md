# Real-Time Gaming Leaderboard

An HTTP service that tracks player scores and rankings in real time. Redis drives live leaderboard reads and writes; MySQL persists every scoring event as a durable history log, and the Redis leaderboard is automatically rebuilt from MySQL on startup.

---

## Core Features

- **Live rankings** — Redis sorted sets serve top-N and player-neighborhood queries with sub-millisecond latency.
- **Durable history** — Every score increment is persisted to MySQL as a `score_events` record before Redis is updated. If MySQL fails, Redis is not touched.
- **Startup recovery** — On boot the service checks whether the current month's Redis key has already been populated. If not, it replays `score_events` from MySQL and rebuilds the sorted set. A recovery marker key prevents duplicate replays across multiple restarts or instances.
- **High-throughput write batching** — Instead of one INSERT per request, an in-memory `EventBatcher` aggregates events and flushes them to MySQL in a single multi-value `INSERT` every 100 ms (or when the buffer reaches 500 events). At 5 000 writes/second this reduces MySQL insert rate from 5 000/s to roughly 10/s.
- **Graceful shutdown** — On `SIGINT`/`SIGTERM` the batcher flushes any buffered events before the HTTP server closes, so no scored events are silently dropped.
- **Monthly partitioning** — The `score_events` table is range-partitioned by `created_at`. Each month gets its own partition; queries for monthly scores scan only the relevant partition instead of the full table. Partitions for the current and next month are created automatically on startup.

---

## Quick Start

```bash
cp .env.example .env
docker compose up --build
```

Redis, MySQL, and the app all start together. The API is available at `http://localhost:8080`.

### Run locally (without Docker)

Requires Go 1.25+, a running Redis instance, and a running MySQL instance.

```bash
cp .env.example .env
docker compose up -d redis mysql
go run .
```

---

## Testing

### In Docker

```bash
docker compose --profile test run --rm test
```

### Locally

```bash
go test ./...
```

No external services are required — Redis tests use `miniredis` (in-process) and MySQL tests use `go-sqlmock` (no real DB needed).

---

## Project Structure

```
main.go                              — entry point: loads config, wires stores/batcher/service/handler
internal/config/config.go            — reads env vars with typed defaults
internal/store/redis.go              — Redis data access: sorted sets, bulk load, recovery marker
internal/store/mysql.go              — MySQL data access: migrate, partitions, batch inserts, history queries
internal/store/batcher.go            — in-memory event batcher: buffers writes, flushes to MySQL in bulk
internal/service/leaderboard.go      — coordinator: MySQL-first write order, startup recovery
internal/handler/leaderboard.go      — Gin HTTP handlers
Dockerfile                           — multi-stage production image (Go builder → alpine final)
Dockerfile.test                      — test runner image (Go toolchain, no binary)
docker-compose.yaml                  — redis + mysql + app + test services
.env / .env.example                  — local configuration
```

---

## Architecture

### Write path

```
POST /v1/scores/:username
        │
        ▼
  LeaderboardService.IncrementScore
        │
        ├─ 1. EventBatcher.RecordEvent(username)   buffer++
        │         │
        │         │  every 100 ms or 500 events
        │         ▼
        │      MySQLStore.RecordBatch              single multi-value INSERT
        │         ↳ MySQL error → logged, no Redis update
        │
        └─ 2. RedisStore.IncrementScore            ZINCRBY (live ranking)
```

Redis is updated immediately so live rankings are always current. MySQL receives events with up to `BATCH_FLUSH_MS` latency via the batcher.

### Read path

```
GET /v1/scores            → Redis ZREVRANGEWITHSCORES (top N)
GET /v1/scores/:username  → Redis ZREVRANK + ZREVRANGEWITHSCORES (neighborhood)
```

All leaderboard reads go directly to Redis.

### Startup recovery

```
app start
  └─ RecoverCurrentMonth()
        │
        ├─ Redis key exists AND :recovered marker set → skip (already populated)
        │
        └─ marker absent → MySQLStore.GetMonthlyScores(year, month)
                               │
                               ├─ empty result → set :recovered marker, done
                               └─ scores found → RedisStore.BulkLoad (pipeline ZADD)
                                                  └─ set :recovered marker
```

---

## Configuration

Copy `.env.example` to `.env` and adjust as needed. When running via Docker Compose, `REDIS_ADDR` and `DB_DSN` are automatically overridden to use the internal service hostnames.

| Variable | Default | Description |
|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | *(empty)* | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `LEADERBOARD_PREFIX` | `leaderboard` | Prefix for the Redis sorted set keys |
| `TOP_N` | `10` | Number of players returned by the top scores endpoint |
| `USER_NEIGHBORHOOD` | `4` | Players shown above and below a queried user |
| `DB_DSN` | `root:password@tcp(localhost:3306)/leaderboard?parseTime=true` | MySQL DSN |
| `BATCH_FLUSH_MS` | `100` | How often (ms) the batcher flushes buffered events to MySQL |
| `BATCH_SIZE` | `500` | Buffer size that triggers an immediate flush regardless of the timer |

---

## API

Base URL: `http://localhost:8080`

### Get top scores

Returns the top `TOP_N` players for the current month, ordered by score descending.

```
GET /v1/scores
```

**Response `200 OK`**
```json
[
  { "rank": 1, "username": "alice", "score": 980 },
  { "rank": 2, "username": "bob",   "score": 850 }
]
```

---

### Get a player's rank and neighbors

Returns the player along with up to `USER_NEIGHBORHOOD` players ranked above and below them.

```
GET /v1/scores/:username
```

**Response `200 OK`**
```json
[
  { "rank": 3, "username": "carol", "score": 820 },
  { "rank": 4, "username": "dave",  "score": 800 },
  { "rank": 5, "username": "alice", "score": 780 },
  { "rank": 6, "username": "eve",   "score": 760 },
  { "rank": 7, "username": "frank", "score": 740 }
]
```

**Response `404 Not Found`** — username not on the leaderboard.

---

### Increment a player's score

Adds 1 point to the player's score for the current month. Creates the player entry if it does not exist.

```
POST /v1/scores/:username
```

**Response `200 OK`**
```json
{ "message": "saved" }
```

**Response `500 Internal Server Error`** — returned if the MySQL write fails; Redis is not updated.

---

## Data Model

### Redis — live rankings

Sorted sets keyed by `{LEADERBOARD_PREFIX}-{year}-{month}` (e.g. `leaderboard-2026-6`). Each month produces a new key automatically. Old keys are not deleted or archived.

A companion key `{leaderboard-key}:recovered` is set after a successful startup recovery to prevent repeated replays.

### MySQL — score history

```sql
CREATE TABLE score_events (
  id         BIGINT NOT NULL AUTO_INCREMENT,
  username   VARCHAR(255) NOT NULL,
  delta      INT NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id, created_at),   -- created_at required by InnoDB partition constraint
  INDEX idx_created_at (created_at)
)
PARTITION BY RANGE COLUMNS(created_at) (
  PARTITION p_future VALUES LESS THAN (MAXVALUE)
);
```

- No `idx_username` — at 50 M users the random I/O cost of maintaining that index during bulk inserts is prohibitive.
- Monthly partitions (e.g. `p2026_06`) are carved out of `p_future` on startup via `ALTER TABLE … REORGANIZE PARTITION`. `GetMonthlyScores` uses a `created_at >= ? AND created_at < ?` date range so the optimizer can prune to the single relevant partition.
- The batcher writes aggregated deltas (one row per unique username per flush window) rather than one row per event, which keeps the table from growing at the raw event rate.
