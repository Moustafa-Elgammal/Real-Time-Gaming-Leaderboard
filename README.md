# Real-Time Gaming Leaderboard

An internal HTTP service that tracks player scores and rankings in real time. It is designed to sit **behind a game service** that handles player authentication and score validation — this service trusts all calls that reach it and focuses solely on recording and ranking scores. Redis drives live leaderboard reads and writes; MySQL persists every scoring event as a durable history log, and the Redis leaderboard is automatically rebuilt from MySQL on startup.

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

### System context

```
 ┌──────────────────────────────────────────────────────┐
 │                    Game Service                      │
 │  • Player authentication (JWT / session)             │
 │  • Score validation (anti-cheat, delta limits)       │
 │  • Rate limiting per player                          │
 └─────────────────────┬────────────────────────────────┘
                       │  internal call
                       │  X-Internal-Token: <secret>
                       ▼
          ┌─────────────────────────┐
          │   Leaderboard Service   │  ← this repo
          └─────────────────────────┘
```

The leaderboard service is **not exposed to game clients directly**. All requests originate from the trusted game service. When `INTERNAL_API_KEY` is set, every request must carry the header `X-Internal-Token: <key>` or receive a `401 Unauthorized`. Leaving the key empty disables the check for local development.

### Write path

```
POST /v1/scores/:username
        │
        ▼  (called by game service after auth + validation)
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
| `INTERNAL_API_KEY` | *(empty)* | Shared secret with the upstream game service. When set, all requests must include `X-Internal-Token: <key>`. Empty = auth disabled (local dev). |

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

---

## Scalability Roadmap

Items below are planned improvements toward a production-grade, fully scalable leaderboard as described in *System Design Interview Vol. II — Chapter: Designing a Real-Time Leaderboard*. They are grouped by concern and ordered roughly by priority within each group.

---

### Infrastructure & Deployment

- [ ] **Load balancer** — Place an L7 load balancer (e.g. AWS ALB, Nginx, or HAProxy) in front of multiple app replicas. The service is stateless (all state lives in Redis and MySQL) so any instance can handle any request. Sticky sessions are not needed.
- [ ] **Kubernetes (K8s)** — Package the app as a K8s `Deployment` with a `HorizontalPodAutoscaler` (HPA) that scales on CPU/RPS metrics. Use `PodDisruptionBudget` to keep at least one replica available during rolling updates.
- [ ] **Helm chart** — Wrap the K8s manifests in a Helm chart for environment-specific overrides (staging vs. production resource limits, replica counts, DSN secrets).
- [ ] **CI/CD pipeline** — GitHub Actions (or equivalent) that runs `go test ./...`, builds the Docker image, pushes to a registry, and triggers a rolling deploy. Add a gate that blocks deploy if test coverage drops below a threshold.
- [ ] **Blue/green or canary deployments** — Route a small percentage of traffic to the new version before full rollout; auto-rollback on error-rate spike.
- [ ] **Multi-region deployment** — Run app + Redis replicas in multiple regions. Use GeoDNS or a global load balancer (e.g. AWS Global Accelerator) to route users to the nearest region. Write to a primary MySQL region; replicate async to secondaries for recovery reads.

---

### Redis Scalability

- [ ] **Redis Cluster** — Shard sorted sets across multiple Redis nodes using consistent hashing. At 50 M users a single sorted set fits in memory (~3–4 GB) but a cluster removes the single-point-of-failure and allows horizontal read scaling.
- [ ] **Redis Sentinel / managed Redis** — Use Redis Sentinel (or AWS ElastiCache with auto-failover) for automatic leader election on primary failure. Update the client to use the Sentinel endpoint instead of a single address.
- [ ] **Redis persistence** — Enable AOF (append-only file) with `appendfsync everysec` on the Redis primary so the sorted set survives a Redis restart without needing a full MySQL replay. Complements but does not replace the existing `:recovered` recovery mechanism.
- [ ] **Key expiry / archiving** — Set a TTL (e.g. 90 days) on old monthly sorted set keys to reclaim Redis memory automatically. Optionally export expired keys to cold storage (S3 + Parquet) before expiry for historical analytics.
- [ ] **Leaderboard segmentation** — Support multiple concurrent leaderboards (per-game, per-region, per-tournament) by parameterizing the key prefix beyond `LEADERBOARD_PREFIX`. Route writes and reads to the correct key without changing the sorted-set data model.

---

### Message Queue / Write Pipeline

- [ ] **Replace in-memory batcher with Kafka (or Redis Streams)** — The current `EventBatcher` is process-local; events are lost if the pod crashes between flush intervals. A durable queue (Kafka topic or Redis Stream) decouples score ingestion from MySQL writes entirely and provides replay-on-failure. Producers write to the queue; a separate consumer group drains to MySQL and Redis.
- [ ] **Dead-letter queue (DLQ)** — Route failed MySQL inserts to a DLQ for inspection and manual replay rather than silently dropping events.
- [ ] **Backpressure signaling** — If the queue depth exceeds a threshold, the HTTP handler should return `429 Too Many Requests` instead of accepting unbounded traffic, preventing queue overflow.

---

### Observability

- [ ] **Structured logging** — Replace `log.Printf` with a structured logger (`zap` or `zerolog`). Emit JSON logs with fields: `level`, `timestamp`, `trace_id`, `username`, `latency_ms`, `error`. Structured logs are indexable in log aggregation systems (Loki, Elasticsearch, CloudWatch).
- [ ] **Distributed tracing** — Instrument with OpenTelemetry (`go.opentelemetry.io/otel`). Propagate trace context across the HTTP handler → service → store chain. Export spans to Jaeger or AWS X-Ray. Critical for diagnosing latency spikes in a multi-service deployment.
- [ ] **Prometheus metrics** — Expose a `/metrics` endpoint. Track: request rate, error rate, handler latency (p50/p99), batcher buffer depth, batcher flush duration, MySQL insert latency, Redis command latency.
- [ ] **Grafana dashboards** — Wire Prometheus to Grafana. Create panels for the four golden signals: latency, traffic, errors, saturation (Redis memory %, MySQL connection pool usage).
- [ ] **Alerting** — Set up Alertmanager (or PagerDuty) rules: error rate > 1 %, p99 latency > 200 ms, Redis memory > 80 %, MySQL replication lag > 30 s, batcher flush failures.
- [ ] **Health endpoints** — Add `/healthz` (liveness: is the process alive?) and `/readyz` (readiness: can it serve traffic — Redis ping + MySQL ping both succeed?). K8s uses these for pod lifecycle management.

---

### Security

> **Trust model:** This service is internal-only. Player authentication, session management, anti-cheat logic, and per-player rate limiting are all responsibilities of the **upstream game service**. The leaderboard service trusts every request that reaches it and only needs to verify that the caller is the game service.

- [x] **Shared-secret internal auth** — `INTERNAL_API_KEY` / `X-Internal-Token` header is already implemented. Set a strong secret in production; rotate it via your secrets manager without redeployment by updating the env var and doing a rolling restart.
- [ ] **Mutual TLS (mTLS)** — For stronger service-to-service identity than a shared secret, issue certificates to both the game service and leaderboard service via cert-manager (or Vault PKI). Each service verifies the other's certificate on every connection. Recommended when operating in a zero-trust network.
- [ ] **Network-level isolation** — Run the leaderboard service in a private subnet / K8s namespace with a `NetworkPolicy` that whitelists inbound traffic only from the game service pod CIDR. The service should have no public ingress.
- [ ] **TLS for transport** — Encrypt all traffic in transit: game service → leaderboard (TLS at the ingress/service mesh layer), app → Redis (`rediss://`), app → MySQL (`tls=true` in DSN). Terminate external TLS at the load balancer with ACM or cert-manager.
- [ ] **Secrets management** — Store `INTERNAL_API_KEY`, `REDIS_PASSWORD`, and `DB_DSN` in a secrets manager (HashiCorp Vault, AWS Secrets Manager, or K8s Secrets with RBAC). Mount them as environment variables at runtime; never bake them into the image or commit them to the repo.
- [ ] **Input validation** — Enforce a maximum username length and character whitelist (alphanumeric + `_`) at the handler layer. The game service should also validate, but defense-in-depth at the leaderboard boundary prevents malformed data from reaching Redis or MySQL.

---

### MySQL Scalability

- [ ] **Read replicas** — Add one or more MySQL read replicas. Direct `GetMonthlyScores` (used during recovery) to a replica to offload the primary.
- [ ] **Connection pooling** — Tune `sql.DB.SetMaxOpenConns`, `SetMaxIdleConns`, and `SetConnMaxLifetime` based on measured concurrency. For very high connection counts consider a connection pooler (ProxySQL or PgBouncer-equivalent).
- [ ] **Automated partition management** — Schedule a monthly cron job (K8s `CronJob`) that calls `EnsureMonthPartition` for the upcoming month, replacing the current at-startup approach which only runs when the app restarts.
- [ ] **Partition archiving** — After a retention window (e.g. 6 months), `ALTER TABLE … DROP PARTITION p{year}_{month}` or export the partition to cold storage (S3) and drop it. This keeps the table size bounded.
- [ ] **MySQL backup & point-in-time recovery** — Enable binary logging and schedule regular `mysqldump` or snapshot backups. Test restore procedures. Use managed MySQL (RDS, Cloud SQL) for automated backups and failover in production.

---

### API & Developer Experience

- [ ] **Pagination** — The top-N endpoint currently returns a fixed slice. Add `limit` and `offset` (or cursor-based) query parameters so clients can page through larger result sets.
- [ ] **WebSocket / Server-Sent Events** — Push live ranking updates to connected clients instead of requiring polling. A Redis pub/sub channel can fan out score-change events to all connected SSE streams.
- [ ] **Historical leaderboards** — Add `GET /v1/scores?year=2026&month=5` to serve past months. Reads from MySQL (`GetMonthlyScores`) since those Redis keys may have expired.
- [ ] **gRPC internal API** — Expose a gRPC interface alongside the REST API for lower-latency service-to-service calls (e.g. from a game backend). Share Protobuf definitions as the contract.
- [ ] **OpenAPI / Swagger spec** — Generate an OpenAPI 3.0 spec from the handler layer (e.g. with `swaggo/swag`) so clients can auto-generate SDKs and the API is self-documented.
