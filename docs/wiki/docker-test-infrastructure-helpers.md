---
{
  "title": "Docker Test Infrastructure Helpers",
  "summary": "Helpers for starting and stopping a MinIO Docker container during integration tests. Provides RequireDocker for clean skip behavior and a random-port MinIO container factory used by S3-compatible storage tests.",
  "concepts": [
    "Docker",
    "MinIO",
    "S3-compatible",
    "ephemeral container",
    "RequireDocker",
    "dynamic port",
    "container lifecycle",
    "test infrastructure",
    "integration",
    "parallel test safety"
  ],
  "categories": [
    "testing",
    "integration",
    "infrastructure",
    "test"
  ],
  "source_docs": [
    "8e918df8d4611668"
  ],
  "backlinks": null,
  "word_count": 289,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

These helpers abstract the Docker lifecycle needed for tests that require an S3-compatible store. Rather than using a shared MinIO instance that could interfere between test runs, each test gets its own ephemeral container with a randomly assigned port.

## RequireDocker

`RequireDocker` runs `docker version` and calls `t.Skip` if Docker is unavailable. This prevents tests from failing with an unhelpful error on environments where Docker is not installed — CI environments without Docker simply skip the test rather than report a false failure.

## StartMinioTestContainer

This function generates a unique container name (`litestream-minio-<nanoseconds>`) to prevent name collisions between parallel test runs. It forcibly removes any container with the same name first (`docker rm -f`) to clean up leaked containers from prior failed runs.

The container is started with:
- Port `0:9000` — Docker assigns an ephemeral host port, avoiding conflicts with other services or parallel tests.
- Standard MinIO credentials (`minioadmin`/`minioadmin`) for test simplicity.

`parseDockerPort` extracts the assigned host port from `docker port` output. The port must be dynamic because using a fixed port (e.g., 9000) would fail when multiple tests run concurrently.

The function returns both the container name (for cleanup) and the endpoint URL.

## StopMinioTestContainer

Calls `docker rm -f` to stop and remove the container. This is typically registered as a `t.Cleanup` callback so cleanup runs even on test failure.

## Known Gaps

- `runDockerCommand` captures output but does not stream it — if the container fails to start, the error is only visible in the returned output bytes, not on the test's log stream.
- There is no wait-for-ready loop after starting the container; callers depend on the `litestream` binary retrying its connection rather than waiting for the MinIO API to become responsive.