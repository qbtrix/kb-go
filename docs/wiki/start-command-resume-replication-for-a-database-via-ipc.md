---
{
  "title": "Start Command: Resume Replication for a Database via IPC",
  "summary": "Implements the `litestream start` sub-command, which sends a request over the Unix control socket to ask a running daemon to begin replicating a previously-stopped database. Mirrors the `stop` command's IPC pattern.",
  "concepts": [
    "StartCommand",
    "Unix domain socket",
    "HTTP IPC",
    "StartRequest",
    "StartResponse",
    "litestream.Server",
    "database lifecycle",
    "control socket",
    "replication resume"
  ],
  "categories": [
    "cli",
    "litestream",
    "ipc"
  ],
  "source_docs": [
    "a8236128f7ff4ace"
  ],
  "backlinks": null,
  "word_count": 328,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`StartCommand` sends a `litestream.StartRequest` to a running litestream daemon via HTTP over a Unix domain socket. This complements `StopCommand` and `RegisterCommand` in the runtime control surface — together they let operators manage database replication lifecycle without restarting the daemon.

## Use Case

When a database is stopped via `litestream stop`, replication for that specific database is paused while the daemon continues running for other databases. `litestream start` resumes replication for that database. This is useful when performing maintenance (schema migrations, bulk imports) where you want to temporarily halt replication to avoid creating excessive LTX files during writes, then resume cleanly afterward.

## IPC Pattern

The implementation follows the same pattern as `register.go` and `stop.go`:

1. Parse flags (`-timeout`, `-socket`)
2. Validate arguments (exactly one positional database path)
3. Construct an `http.Client` with a custom `Transport` that dials the Unix socket instead of TCP
4. Marshal the request as JSON and POST to `http://localhost/start`
5. Parse the response as `litestream.StartResponse` and pretty-print it

The `http://localhost` host is a placeholder — the transport's `DialContext` ignores the host entirely and always connects to the configured socket path. This lets the code use standard `http.Client` machinery without exposing a real network port.

## Error Handling

On non-200 responses, the command attempts to unmarshal as `litestream.ErrorResponse`. If that fails or the error field is empty, the raw body is displayed. This prevents confusing output like `start failed: {"error":""}` when the daemon returns an unexpected response shape.

## Timeout

The `-timeout` flag sets both the HTTP client timeout and is included in the request body as `StartRequest.Timeout`. The daemon uses the request timeout to limit how long it waits for an in-progress replication to finish before acknowledging the start. Sending the timeout in the request avoids a mismatch where the client gives up before the daemon finishes.

## Known Gaps

No test coverage for `StartCommand` (no `start_test.go` in the batch). Behavior is implicitly tested through the integration tests for the IPC server.