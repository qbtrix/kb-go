---
{
  "title": "Litestream `info` Command: Daemon Status via Unix Socket",
  "summary": "`info.go` implements the `litestream info` subcommand, which queries the running Litestream daemon's `/info` HTTP endpoint via a Unix domain socket and displays version, uptime, and database count. It serves as a lightweight health check for operators.",
  "concepts": [
    "Unix socket",
    "HTTP client",
    "daemon health check",
    "litestream info",
    "control socket",
    "JSON output",
    "timeout validation",
    "operator tooling",
    "runtime status"
  ],
  "categories": [
    "litestream",
    "CLI",
    "monitoring",
    "tooling"
  ],
  "source_docs": [
    "3ccf0125e4880a4b"
  ],
  "backlinks": null,
  "word_count": 349,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The `databases` command reads the config file to show what Litestream is configured to manage. The `info` command shows what the daemon is actually doing at runtime. The distinction matters when the daemon has started but has not yet picked up all configured databases, or when environment variable substitution in the config produces a different path than expected.

## Unix Socket Transport

The daemon exposes a control API over a Unix domain socket rather than a TCP port. This design choice prevents port conflicts, avoids firewall rules, and limits access to users with filesystem permission to the socket path (typically `root` or the `litestream` service account).

The HTTP client is configured with a custom `DialContext` that routes all connections through the Unix socket:

```go
Transport: &http.Transport{
    DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
        return net.DialTimeout("unix", *socketPath, clientTimeout)
    },
}
```

The HTTP host header (`http://localhost/info`) is ignored by the daemon, which listens only on the socket. The URL scheme and host are required by the `http.Client` API but have no routing significance.

## Output Format

With `--json`, the raw JSON response body is written to stdout. Without it, the JSON is parsed and formatted as human-readable key-value pairs. This dual mode lets the command serve both operators inspecting the daemon manually and scripts polling daemon health.

## Timeout Validation

```go
if *timeout <= 0 {
    return fmt.Errorf("timeout must be greater than 0")
}
```

This guard prevents a zero or negative timeout from silently creating an HTTP client that either never times out (zero means no timeout in Go's `http.Client`) or times out immediately. Both would be surprising to the operator.

## Known Gaps

- The default socket path (`/var/run/litestream.sock`) is hardcoded and not read from the config file. If the daemon is configured to use a different socket path, the operator must always pass `--socket`.
- The command connects to the HTTP endpoint but does not validate that the response is from a Litestream daemon (no version header check), so pointing it at an arbitrary HTTP server on the socket would produce confusing output.