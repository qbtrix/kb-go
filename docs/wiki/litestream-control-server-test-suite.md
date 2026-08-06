---
{
  "title": "Litestream Control Server Test Suite",
  "summary": "Tests every HTTP endpoint on the Litestream Unix socket control server, exercising request parsing, response encoding, and server lifecycle. Uses real Unix sockets and an in-process HTTP client to validate the full request path.",
  "concepts": [
    "Unix socket",
    "HTTP client",
    "testify",
    "control server",
    "HandleInfo",
    "HandleList",
    "HandleStart",
    "HandleStop",
    "HandleRegister",
    "HandleSync",
    "atomic counter",
    "DialContext",
    "socket path",
    "io.Reader"
  ],
  "categories": [
    "testing",
    "server",
    "litestream",
    "test"
  ],
  "source_docs": [
    "55bffccb8156d32c"
  ],
  "backlinks": null,
  "word_count": 359,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test suite creates a live `Server` bound to a unique Unix socket per test, issues real HTTP requests over that socket using an `http.Transport` configured with a Unix dialer, and asserts JSON responses. Using real sockets rather than a mock avoids testing a layer that does not exist in production — the Unix socket binding and file permission setup are part of the behavior being tested.

## Socket Uniqueness

`testSocketPath` generates a unique socket path for each test using an atomic counter (`testSocketCounter`). Without uniqueness, parallel tests would collide on the same socket path and fail with `EADDRINUSE`.

## Endpoint Tests

`TestServer_HandleInfo` starts a server with a known `Version` string and asserts it appears in the `/info` JSON response.

`TestServer_HandleList` registers databases with the store and asserts they appear in `/list`. This verifies both the registration path and the serialization of `DatabaseSummary`.

`TestServer_HandleStart` and `TestServer_HandleStop` send `StartRequest` / `StopRequest` payloads and assert the `Status` field in the response. These tests call real `store.EnableDB` / `store.DisableDB` via the HTTP handler, confirming the handler correctly bridges HTTP and store operations.

`TestServer_HandleRegister` and `TestServer_HandleUnregister` add and remove a database at runtime via HTTP and assert the store's database list reflects the change.

`TestServer_HandleSync` triggers a sync and asserts the response includes a non-zero `TXID`.

## HTTP over Unix Socket

`newSocketClient` creates an `http.Client` with a custom `DialContext` that connects to a Unix socket path rather than a hostname. This is the same mechanism a real CLI tool would use to communicate with the daemon. Without it, test requests would go to a real TCP address and never reach the test server.

## `stringReader` Helper

`stringReader` wraps a string as an `io.Reader` via the `stringReaderType` struct. This exists to provide request bodies without using `bytes.NewBufferString`, likely for clarity in test setup.

## Known Gaps

- Tests do not verify that the Unix socket file is cleaned up after `Server.Close()`. A leaked socket file would prevent the next daemon start on the same path.
- Permission checking (the `SocketPerms` field) is not tested — a caller with wrong filesystem permissions would silently fail to connect rather than get a clear error.