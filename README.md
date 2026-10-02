# Chirpy

A small social feed (think early Twitter) with a Go HTTP API, Postgres, and a plain JavaScript web client. I built the first version in Boot.dev's "Learn HTTP Servers in Go" course, then hardened it the way I would before putting it in front of real users.

![Chirpy web client](docs/screenshot.png)


**Live:** https://chirpy-slpq.onrender.com/app/ (API under `/api`, health at `/api/healthz`). Go server in Docker on Render's free plan, Postgres on a Supabase free project with the goose migrations applied. The free instance sleeps after 15 minutes idle, so the first request can take up to a minute.

## What it does

- Sign up, log in, post 140-character chirps, browse and page through a feed, delete your own chirps.
- Short-lived JWT access tokens (1 hour) plus 60-day refresh tokens that can be revoked.
- A paid "Chirpy Red" tier, switched on by a webhook from the (mock) payment provider Polka.
- The web client at `/app/` uses the same public API and refreshes expired access tokens on its own.

## What I added after the course

| Area | Course version | Now |
|---|---|---|
| Feed queries | Loaded every chirp, filtered by author in Go memory | Author filter and keyset (cursor) pagination in SQL, backed by composite indexes |
| Refresh tokens | Stored raw in the database | Only a SHA-256 hash is stored, so a DB leak does not leak sessions |
| Static files | `http.FileServer(".")` served the whole repo, including `.env` | Only `./static` is served (a test checks `.env` and source files are not reachable) |
| Webhook auth | API key compared with `!=` | Constant-time compare; retried deliveries are idempotent |
| Bad input | Malformed JSON returned 500 | 400 for bad JSON, 413 over 1 MB, email and password validation, 409 on duplicate email |
| Deletes | Read, check owner, then delete (race) | One `DELETE ... WHERE id = $1 AND user_id = $2` |
| Login | No throttling; timing revealed unknown emails | Per-IP rate limit on login and signup (10/min); same bcrypt cost for unknown emails |
| JWT | Any HMAC algorithm accepted | Only HS256 accepted |
| Server | No timeouts, no shutdown handling | Read/write/idle timeouts, graceful shutdown on SIGTERM, JSON request logs, DB-backed health check |
| Tests | Unit tests for the auth package only | Integration tests for every endpoint against real Postgres |
| Delivery | None | GitHub Actions (gofmt, vet, race tests, sqlc drift check, Docker build, gitleaks) and a distroless Docker image that runs migrations on start |
| Chirp length | Counted bytes (emoji got fewer than 140) | Counts characters |

## API

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/api/users` | none | `{email, password}` creates an account |
| PUT | `/api/users` | access token | change email and password |
| POST | `/api/login` | none | returns user, `token`, `refresh_token` |
| POST | `/api/refresh` | refresh token | returns a new access token |
| POST | `/api/revoke` | refresh token | revokes it |
| POST | `/api/chirps` | access token | `{body}` up to 140 characters |
| GET | `/api/chirps` | none | `author_id`, `sort=asc` or `desc`, `limit` (1 to 100), `cursor`; the next page cursor is in the `X-Next-Cursor` header |
| GET | `/api/chirps/{id}` | none | |
| DELETE | `/api/chirps/{id}` | access token | owner only |
| POST | `/api/polka/webhooks` | `ApiKey` header | `user.upgraded` turns on Chirpy Red |
| GET | `/api/healthz` | none | checks the database |

## Run it

```bash
cp .env.example .env          # fill in JWT_SECRET (32+ chars) and POLKA_KEY
go run .                      # AUTO_MIGRATE=true applies sql/schema with goose
open http://localhost:8080/app/
```

Or with Docker: `docker build -t chirpy . && docker run --env-file .env -p 8080:8080 chirpy`

## Test

```bash
createdb chirpy_test
TEST_DB_URL="postgres://postgres:postgres@localhost:5432/chirpy_test?sslmode=disable" go test -race -cover ./...
```

The integration suite resets the test database, runs the real migrations, and drives the full HTTP stack: validation, login, refresh and revoke, ownership, pagination across pages, author filtering, and webhook retries.

## Stack

Go standard library router (Go 1.22 patterns), Postgres, sqlc for typed queries, goose for migrations, bcrypt, golang-jwt.
