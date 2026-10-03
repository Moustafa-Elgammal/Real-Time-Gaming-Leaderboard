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

# Run all unit tests (no real Redis or MySQL required)
go test ./...

# Run a single package's tests
go test ./internal/store/...
go test ./internal/service/...
go test ./internal/config/...

# Run MySQL integration tests (requires a running MySQL with leaderboard_test database)
# Docker MySQL must be up: docker compose up -d mysql
docker exec real-time-gaming-leaderboard-mysql-1 mysql -uroot -ppassword -e "CREATE DATABASE IF NOT EXISTS leaderboard_test;"
go test -tags integration -count=1 -v ./internal/store/... -run TestIntegration
# Override DSN: TEST_MYSQL_DSN="user:pass@tcp(host:3306)/leaderboard_test?parseTime=true"

# Run tests via Docker (matches CI)
docker compose --profile test run --rm test

# Tidy dependencies
go mod tidy

# Regenerate Swagger docs after changing handler annotations or adding endpoints
swag init -g main.go --parseDependency --parseInternal
# Swagger UI available at http://localhost:8080/swagger/index.html when the server is running
```

## Architecture

Internal-only Go HTTP service backed by Redis (live rankings) and MySQL (durable history). It is designed to sit behind a **game service** that handles player authentication, session management, anti-cheat, and per-player rate limiting. The leaderboard service trusts all requests that reach it; the only boundary it enforces is verifying the caller is the game service via a shared secret.

```
main.go                              — loads .env, wires mysql → batcher → redis → service → handler; registers /swagger route
internal/config/config.go            — all env vars with typed defaults
internal/middleware/auth.go          — InternalAuth: checks X-Internal-Token header (no-op when INTERNAL_API_KEY is empty)
internal/store/redis.go              — Redis sorted sets: TopN, TopNPage, GetUserNeighborhood, IncrementScore (pipeline + pub/sub), Subscribe, BulkLoad, IsRecovered
internal/store/mysql.go              — MySQL: migrate, EnsureMonthPartition, RecordBatch, GetMonthlyScores
internal/store/batcher.go            — in-memory EventBatcher: aggregates events, flushes bulk to MySQL
internal/service/leaderboard.go      — coordinator: MySQL-first write order, RecoverCurrentMonth
internal/handler/leaderboard.go      — Gin HTTP handlers (TopN, StreamScoreUpdates, GetUserRank, UpdatePlayerScore) with swaggo annotations
docs/                                — Swagger 2.0 spec (docs.go, swagger.json, swagger.yaml); served at /swagger/index.html
```

**Stack:** Gin · go-redis/v9 · go-sql-driver/mysql · godotenv

### Write path

```
POST /v1/scores/:username
        │
        ├─ EventBatcher.RecordEvent   buffer[username]++
        │       └─ at 500 events or every 100ms → MySQLStore.RecordBatch (multi-value INSERT)
        │
        └─ RedisStore.IncrementScore  pipeline: ZINCRBY + ZREVRANK → PUBLISH score-updates (best-effort)
```

MySQL write is attempted first; Redis is only updated on success. The pub/sub publish is best-effort — a failure does not fail the increment.

### Read path

All reads go directly to Redis sorted sets — no MySQL involved.

### SSE push path

```
GET /v1/scores/stream
        │
        └─ RedisStore.Subscribe → SUBSCRIBE leaderboard:score-updates
                └─ goroutine forwards pub/sub messages to a Go channel
                        └─ handler loop: select { event → SSEvent | ctx.Done → return }
```

Each connected SSE client holds one Redis pub/sub subscription. The subscription is cleaned up on client disconnect via `defer unsubscribe()`.

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
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
| GET | `/v1/scores` | Paginated leaderboard — `?page=1&page_size=TOP_N` (1–100); returns `PagedResponse` |
| GET | `/v1/scores/stream` | SSE stream — pushes `score-update` events (username, score, rank) via Redis pub/sub |
| GET | `/v1/scores/:username` | Player + `USER_NEIGHBORHOOD` above/below |
| POST | `/v1/scores/:username` | Increment score by 1 (MySQL first, then Redis + pub/sub publish) |

### Pagination — `GET /v1/scores`

Query parameters:

| Param | Default | Constraints | Description |
|-------|---------|-------------|-------------|
| `page` | `1` | ≥ 1 | Page number (1-based) |
| `page_size` | `TOP_N` | 1–100 | Entries per page |

Response shape (`PagedResponse`):

```json
{
  "data":      [ { "rank": 1, "username": "alice", "score": 980 }, ... ],
  "page":      1,
  "page_size": 10,
  "total":     150
}
```

`total` is the live count of distinct players on the current month's leaderboard (from Redis `ZCARD`). Out-of-range pages return an empty `data` array with the correct `total`.

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
| `DB_MAX_OPEN_CONNS` | `10` | MySQL pool max open connections |
| `DB_MAX_IDLE_CONNS` | `5` | MySQL pool max idle connections |
| `DB_CONN_MAX_LIFETIME_S` | `300` | MySQL connection max lifetime, seconds |
| `DB_CONN_MAX_IDLE_TIME_S` | `120` | MySQL connection max idle time, seconds |

## Testing

- **Redis tests** (`store/redis_test.go`) — use `miniredis.RunT(t)`, no real Redis. Covers `TopNPage` pagination (first page, second page, offset beyond end, empty leaderboard); `IncrementScore` pub/sub publish (event received, rank reflects current standing); `Subscribe` idempotent unsubscribe.
- **MySQL tests** (`store/mysql_test.go`) — use `go-sqlmock`, no real MySQL.
- **Batcher tests** (`store/batcher_test.go`) — use `mockBatchStore`, no real stores.
- **Service tests** (`service/leaderboard_test.go`) — mock structs with function fields for both stores. Covers `TopNPage` delegation.
- **Handler tests** (`handler/leaderboard_test.go`) — cover default page, explicit page/page_size, invalid params (400), store error (500), empty leaderboard, and max page_size boundary; SSE: single event, multiple events, subscribe error (500), unsubscribe on exit, correct `Content-Type`.
- **Config tests** (`config/config_test.go`) — set env vars via `t.Setenv`, test defaults/overrides/invalid int fallback.
- **Middleware tests** (`middleware/auth_test.go`) — tests disabled (empty key), correct token, wrong token, and missing token cases.
- **MySQL integration tests** (`store/mysql_integration_test.go`, build tag `integration`) — tests `NewMySQL`, schema migration, partition creation, `RecordEvent`, `RecordBatch`, `GetMonthlyScores` cross-month isolation, and `EnsureMonthPartition` idempotency against a real MySQL instance. Uses `leaderboard_test` database; skips gracefully if MySQL is unavailable.

`EventBatcher.Stop()` is idempotent; tests that call `Stop()` explicitly are safe because `t.Cleanup(b.Stop)` is also registered via `newTestBatcher`.

## Infrastructure

Docker Compose (`docker-compose.yaml`) runs four services:

| Service | Notes |
|---------|-------|
| `redis` | `redis:7-alpine`, port 6379 |
| `mysql` | `mysql:8`, port 3306, healthcheck via `mysqladmin ping` |
| `app` | depends on `redis` (started) + `mysql` (healthy), port 8080 |
| `test` | profile `test`, runs `go test -v ./...` against mock stores |

### Kubernetes (`k8s/`, branch `feature/k8s`)

Deploy: `cp k8s/secret.example.yaml k8s/secret.yaml` (git-ignored), edit it, then `make -C k8s secret deploy`. Other targets: `status`, `scale N=..`, `delete`, `port-forward`.

- `redis.yaml` and `mysql.yaml` run single-replica Deployments with PVCs. MySQL `--max-connections=500`: replicas x `DB_MAX_OPEN_CONNS` must stay below it.
- `my-secret` holds all app env vars plus `MYSQL_ROOT_PASSWORD`. `DB_DSN` must use the same password.
- `app-deployment.yaml` runs the app image `elgammalx/imagex:leaderboard` (same tag as the compose `app` service), 3 replicas, with readiness and liveness probes on `GET /healthz`. `/healthz` is registered before `InternalAuth` in `main.go` so probes work when `INTERNAL_API_KEY` is set. `terminationGracePeriodSeconds: 30` lets the batcher flush on SIGTERM.
- `app-service.yaml` is named `app-backend`; the nginx upstream in `nginx-configmap.yaml` points at `app-backend:8080`. Renaming the Service requires updating the ConfigMap.
- Not covered yet: Ingress or LoadBalancer, HPA, PodDisruptionBudget, versioned image tags, `/metrics`.
- nginx fronts the app. Proxy buffering is off and `proxy_read_timeout` is 3600s because the SSE endpoint `GET /v1/scores/stream` must not be buffered or timed out.
- `make -C k8s port-forward` exposes nginx on `localhost:8080`.

### Load testing

`k6/` (scripts, utils, reports) and `grafana/` (provisioned dashboards) back the `load-test` compose profile (influxdb, grafana, k6). See the comments in `docker-compose.yaml` for the run commands.

The `http.http` file contains JetBrains HTTP Client requests for manual endpoint testing.

## Coding Conventions

- All layers communicate via interfaces (handler `Storer`, service `EventRecorder`/`ScoreStore`, batcher `batchStore`) — enables unit testing without real infrastructure.
- Never add `Co-Authored-By` lines to commits.
- Commit identity: Moustafa Elgammal / moustafa_algammal@yahoo.com.
