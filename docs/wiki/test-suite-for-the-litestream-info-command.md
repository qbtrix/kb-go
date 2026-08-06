---
{
  "title": "Test Suite for the Litestream `info` Command",
  "summary": "info_test.go covers argument validation, connection error handling, timeout behavior, and a successful request/response cycle for the `info` subcommand, using a real Unix socket server to validate the full HTTP transport path.",
  "concepts": [
    "Unix socket testing",
    "atomic counter",
    "unique socket path",
    "timeout validation",
    "connection error",
    "mock HTTP server",
    "argument validation",
    "test cleanup",
    "litestream info"
  ],
  "categories": [
    "testing",
    "litestream",
    "CLI",
    "unit",
    "test"
  ],
  "source_docs": [
    "78f7d01a4883ed65"
  ],
  "backlinks": null,
  "word_count": 297,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The `info` command's correctness depends on both argument parsing and the Unix socket HTTP transport. Testing only argument parsing would miss bugs in the socket connection logic; testing only the happy path would miss validation errors. This test file covers both.

## testSocketPath Helper

```go
var testSocketCounter uint64

func testSocketPath(t *testing.T) string {
    n := atomic.AddUint64(&testSocketCounter, 1)
    path := fmt.Sprintf("/tmp/ls-cmd-test-%d.sock", n)
    t.Cleanup(func() { os.Remove(path) })
    return path
}
```

The atomic counter ensures parallel subtests get unique socket paths and do not collide. `t.Cleanup` removes the socket file after the test, preventing leftover files from interfering with subsequent runs. Without a unique suffix, two parallel tests that both try to bind `ls-cmd-test.sock` would fail.

## Validation Tests

- **TooManyArguments**: passing a positional argument returns "too many arguments".
- **InvalidTimeoutZero**: `--timeout 0` returns the expected error message.
- **InvalidTimeoutNegative**: `--timeout -1` returns the same error.

These guard against the `timeout <= 0` validation being accidentally removed during refactoring.

## Connection Error Tests

- **ConnectionError**: points the command at `/nonexistent/socket.sock` and verifies an error is returned. This confirms the error propagation from `net.DialTimeout` through the HTTP client.
- **CustomTimeout**: same socket path but with `--timeout 1`, verifying the timeout is plumbed correctly.

## Successful Request Test

The successful path creates a real Unix socket server using `net.Listen("unix", socketPath)`, serves a mock `/info` JSON response, and runs the command against it. This tests the full HTTP round-trip including JSON parsing and output formatting without requiring a live Litestream daemon.

## Known Gaps

- The mock server always returns a fixed JSON response regardless of the URL path. If `info.go` changes the endpoint URL, the test would still pass against the wrong path.
- There is no test for the `--json` flag, leaving the raw JSON output path untested.