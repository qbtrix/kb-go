---
{
  "title": "Environment-Based Application Configuration for taskapi",
  "summary": "Provides structured application configuration for the taskapi package, loaded from environment variables with safe defaults and explicit validation. Uses sentinel ConfigError values to give callers machine-readable field-level failure information.",
  "concepts": [
    "Config",
    "LoadConfig",
    "DefaultConfig",
    "ConfigError",
    "environment variables",
    "validation",
    "sentinel errors",
    "port",
    "MaxPageSize",
    "CORSOrigins",
    "taskapi",
    "os.Getenv"
  ],
  "categories": [
    "configuration",
    "go",
    "http",
    "taskapi"
  ],
  "source_docs": [
    "c72e598bb6e7dcfc"
  ],
  "backlinks": null,
  "word_count": 587,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Configuration scattered across `os.Getenv` calls in handler or server code is hard to audit, test, or document. `config.go` centralizes all runtime settings in a single `Config` struct with a clear loading path: defaults first, then environment overrides, then explicit validation before the server starts.

## Default Values

`DefaultConfig` returns a fully populated `Config` with safe defaults:

- **Port 8080** — A common non-privileged HTTP port that avoids requiring root.
- **`sqlite://tasks.db`** — A local SQLite database path, appropriate for development.
- **`Authorization`** — The conventional HTTP header for API keys.
- **Log level `info`** — Verbose enough to be useful, quiet enough to avoid noise.
- **MaxPageSize 100** — Limits the number of records returned per list request, preventing unintentional full-table scans from unbounded queries.
- **CORSOrigins `["*"]`** — Open CORS for development; operators are expected to override this in production.

Returning a fully-populated default rather than a zero-value struct means a caller that skips `LoadConfig` entirely still gets a workable configuration, which is useful in tests.

## Environment Loading

`LoadConfig` calls `DefaultConfig` and then selectively overrides fields from environment variables:

```go
if port := os.Getenv("PORT"); port != "" {
    if p, err := strconv.Atoi(port); err == nil {
        cfg.Port = p
    }
}
```

The `if err == nil` guard silently ignores unparseable values (e.g., `PORT=abc`) and retains the default. This is a deliberate choice: an invalid env value failing silently rather than crashing may be preferable in some deployment contexts, though it can make misconfiguration harder to diagnose. A `MaxPageSize` override additionally requires `m > 0`, preventing a zero or negative page size that would reject all list requests.

After loading, a `log.Printf` logs the resolved port, database URL, and log level. Logging the database URL here is a trade-off: it aids debugging but risks exposing credentials if the URL contains a password in the query string.

## Validation

`Validate` checks two mandatory invariants after loading:

- **Port range** — Must be 1–65535. Port 0 would cause the OS to assign an ephemeral port, making the server address unpredictable. Ports above 65535 are invalid at the TCP level.
- **DatabaseURL presence** — An empty URL would cause the database driver to fail at the first query rather than at startup, making the failure harder to attribute.

Validation returns a `*ConfigError` rather than a plain string error. `ConfigError` carries the field name alongside the message:

```go
type ConfigError struct {
    Field   string
    Message string
}
```

This lets callers inspect `err.(*ConfigError).Field` to surface field-specific feedback (e.g., in a startup health check or structured log line) without parsing the error string.

The two sentinel errors `ErrInvalidPort` and `ErrMissingDatabase` are package-level variables. Using `errors.Is` against these is not possible because they are pointer values and each call to `Validate` returns the same pointers — callers can use `==` comparison or type-assert to `*ConfigError`.

## APIKeyHeader and CORSOrigins

`APIKeyHeader` is not currently loaded from the environment (no `os.Getenv("API_KEY_HEADER")` call exists). The field is present in the struct but only settable via `DefaultConfig`. This means it cannot be overridden at runtime without code changes.

Similarly, `CORSOrigins` has no corresponding environment-loading logic; the default wildcard `["*"]` is always used. In production deployments this would need to be restricted.

## Known Gaps

- `APIKeyHeader` and `CORSOrigins` are not loaded from environment variables despite being fields in `Config`, making them runtime-immutable without code changes.
- Silent fallback on invalid `PORT` or `MAX_PAGE_SIZE` values can hide misconfiguration.
- The startup log line may expose database credentials if the URL contains embedded passwords.