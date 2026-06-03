# Real-Time Gaming Leaderboard

A lightweight HTTP service that tracks player scores and rankings using Redis sorted sets. Scores are scoped to the current calendar month and reset automatically each month.

---

## Quick Start

```bash
cp .env.example .env
docker compose up --build
```

Both Redis and the app start together. The API is available at `http://localhost:8080`.

### Run locally (without Docker)

Requires Go 1.25+ and a running Redis instance.

```bash
cp .env.example .env
docker compose up -d redis   # or bring your own Redis
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

No running Redis is required — store integration tests use an in-process fake (`miniredis`).

---

## Project Structure

```
main.go                          — entry point: loads config, wires services, registers routes
internal/config/config.go        — reads env vars with typed defaults
internal/store/redis.go          — Redis data access (sorted sets)
internal/handler/leaderboard.go  — Gin HTTP handlers
Dockerfile                       — multi-stage production image
Dockerfile.test                  — test runner image (Go toolchain, no binary)
docker-compose.yaml              — redis + app + test services
.env / .env.example              — local configuration
```

---

## Configuration

Copy `.env.example` to `.env` and adjust as needed. When running via Docker Compose, `REDIS_ADDR` is automatically overridden to `redis:6379`.

| Variable | Default | Description |
|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | *(empty)* | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `LEADERBOARD_PREFIX` | `leaderboard` | Prefix for the Redis sorted set keys |
| `TOP_N` | `10` | Number of players returned by the top scores endpoint |
| `USER_NEIGHBORHOOD` | `4` | Players shown above and below a queried user |

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

---

## Data Model

Scores are stored in Redis sorted sets with keys in the format:

```
{LEADERBOARD_PREFIX}-{year}-{month}
```

Example: `leaderboard-2026-6`

Each month produces a new key. Old keys are not deleted automatically.
