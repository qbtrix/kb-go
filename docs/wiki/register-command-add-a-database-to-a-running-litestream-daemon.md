---
{
  "title": "Register Command: Add a Database to a Running Litestream Daemon",
  "summary": "Implements the `litestream register` sub-command, which sends a JSON request over a Unix domain socket to a running litestream daemon asking it to begin replicating a new SQLite database. Registration is idempotent — registering an already-tracked database returns success without creating a duplicate.",
  "concepts": [
    "RegisterCommand",
    "Unix domain socket",
    "HTTP over Unix socket",
    "RegisterDatabaseRequest",
    "RegisterDatabaseResponse",
    "idempotent registration",
    "dynamic database",
    "control socket",
    "JSON API",
    "litestream daemon IPC"
  ],
  "categories": [
    "cli",
    "litestream",
    "ipc"
  ],
  "source_docs": [
    "77287b12bd8f14ea"
  ],
  "backlinks": null,
  "word_count": 408,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`RegisterCommand` lets operators add a SQLite database to a running litestream daemon without restarting it. This is useful in environments where databases are created dynamically — for example, a multi-tenant SaaS where each tenant gets their own SQLite file.

## Communication Pattern

The command communicates with the daemon via HTTP over a Unix domain socket. This is an unusual pattern — HTTP semantics (methods, status codes, JSON bodies) over a Unix socket rather than TCP. The approach provides:

- **Structured request/response** — JSON encoding means the request is self-describing and easy to extend
- **Process-local security** — a Unix socket at `/var/run/litestream.sock` is only accessible to processes with filesystem permission to that path, avoiding the need for authentication tokens
- **Standard error handling** — HTTP status codes give the daemon a way to signal both success and typed failures

The `http.Transport` is customized to dial the Unix socket path instead of resolving DNS, while the URL host is set to `localhost` as a placeholder the transport ignores. This reuses the standard HTTP client machinery without requiring a real network interface.

## Argument Validation

Before making any network call, the command validates:

- Exactly one positional argument (the database path)
- The `-replica` flag is required — there is no default replica destination
- Timeout must be positive

These checks happen before any I/O, so the user gets an immediate clear error rather than a connection timeout for a malformed invocation.

## Request and Response

The request body is a `litestream.RegisterDatabaseRequest` struct containing the database path and replica URL. On success, the daemon returns a `litestream.RegisterDatabaseResponse` that the command pretty-prints as JSON to stdout, giving the operator a confirmation of what was registered.

Error responses are unmarshalled as `litestream.ErrorResponse` first. If that fails (or if the error field is empty), the raw body is used. This two-level fallback prevents confusing output when the daemon returns an unexpected error format.

## Idempotency

From the test suite, registering a database that is already tracked by the daemon returns success and does not create a duplicate entry in the store. The daemon-side implementation handles deduplication; the CLI does not need to check beforehand. This makes `register` safe to call from provisioning scripts that may run more than once.

## Known Gaps

No timeout flag validation for the socket path existence — a missing or mis-spelled socket path produces a generic connection error rather than a targeted "socket not found" message.