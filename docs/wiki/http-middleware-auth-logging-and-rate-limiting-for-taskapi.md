---
{
  "title": "HTTP Middleware: Auth, Logging, and Rate Limiting for taskapi",
  "summary": "Provides three composable HTTP middleware functions for the taskapi package: token-based authentication, request/duration logging, and per-IP sliding-window rate limiting. Each middleware follows the standard Go http.Handler wrapping pattern and is designed to be stacked in the server's handler chain.",
  "concepts": [
    "AuthMiddleware",
    "LoggingMiddleware",
    "RateLimiter",
    "sliding window",
    "rate limiting",
    "http.Handler",
    "middleware",
    "validateToken",
    "sync.Mutex",
    "per-IP",
    "taskapi",
    "Authorization header",
    "health check bypass"
  ],
  "categories": [
    "http",
    "go",
    "middleware",
    "taskapi"
  ],
  "source_docs": [
    "557d913ffba8bde8"
  ],
  "backlinks": null,
  "word_count": 632,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Cross-cutting HTTP concerns — who can call the API, how long requests take, and how often a single client can call — should not live inside individual handler functions. Middleware isolates these concerns into reusable wrappers that the server applies uniformly before dispatching to business logic.

## AuthMiddleware

```go
func AuthMiddleware(next http.Handler) http.Handler
```

`AuthMiddleware` wraps `next` and inspects the `Authorization` header on every request. Two special behaviors:

1. **Health check bypass** — Requests to `/health` skip authentication entirely. This allows load balancers and monitoring tools to probe liveness without needing an API token.
2. **Empty vs. invalid token distinction** — A missing header returns 401 Unauthorized; a present but invalid token returns 403 Forbidden. This distinction matters for clients: 401 means "try authenticating"; 403 means "your credentials are not accepted."

Token validation is delegated to `validateToken(token)`. The current implementation simply checks `len(token) > 10` — a placeholder that accepts any string longer than 10 characters. In production this should be replaced with a constant-time comparison against a stored secret or a JWT verification.

Because the middleware reads the header directly, it depends on the `APIKeyHeader` constant being `"Authorization"`. If the header name were configurable (as the `Config` struct implies), `AuthMiddleware` would need to accept it as a parameter.

## LoggingMiddleware

```go
func LoggingMiddleware(next http.Handler) http.Handler
```

`LoggingMiddleware` records `time.Now()` before dispatching to `next`, then logs method, path, and elapsed duration after `next` returns. The log line is emitted with `log.Printf`, which writes to the global standard logger.

The middleware does not capture the HTTP response status code. Without a response recorder, it cannot log whether the request succeeded or failed, which limits its usefulness for error-rate analysis. Adding a status-capturing `ResponseWriter` wrapper would enable richer logs.

Duration is computed as `time.Since(start)`, which includes time spent in all downstream middleware and the handler. This is the total request latency visible to the server, which is what operators typically want to track.

## RateLimiter

```go
type RateLimiter struct {
    mu       sync.Mutex
    requests map[string][]time.Time
    limit    int
    window   time.Duration
}
```

`RateLimiter` implements a per-IP sliding window counter. For each incoming IP, `Allow` maintains a slice of recent request timestamps. On each call:

1. All timestamps older than `now - window` are discarded.
2. If the remaining count is at or above `limit`, the request is rejected and the slice is updated (without appending).
3. Otherwise, the current time is appended and `true` is returned.

The sliding window is stricter than a fixed-window counter: a fixed-window limiter can allow up to `2 * limit` requests in a burst spanning a window boundary, while the sliding window enforces `limit` requests in any rolling interval.

The `mu sync.Mutex` serializes all `Allow` calls. For high-traffic services this could become a bottleneck if many goroutines call `Allow` simultaneously on different IPs. A sharded map or sync.Map could reduce lock contention.

The `requests` map grows as new IPs are seen and never shrinks — IP entries for clients that stopped making requests accumulate indefinitely. A periodic cleanup goroutine or TTL-based eviction would prevent unbounded memory growth.

`RateLimiter` is constructed by the caller but the middleware integration is not shown in this file — callers must wrap `Allow` in a handler that returns 429 when `Allow` returns false. The rate limiter is a pure data structure, not itself an `http.Handler`.

## Known Gaps

- `validateToken` uses length-only validation — no cryptographic verification. This is explicitly a stub.
- `LoggingMiddleware` does not capture response status code, so error rates cannot be computed from logs alone.
- `RateLimiter.requests` map is never pruned, leading to unbounded memory growth for services with many distinct client IPs.
- `AuthMiddleware` hard-codes the `"Authorization"` header name rather than accepting it from `Config.APIKeyHeader`.
- There is no middleware that integrates `RateLimiter` — callers must wire it manually.