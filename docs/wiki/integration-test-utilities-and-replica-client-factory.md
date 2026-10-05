---
{
  "title": "Integration Test Utilities and Replica Client Factory",
  "summary": "Centralized test helpers that provision litestream DB instances, open SQLite connections, and construct replica clients for every supported backend (file, S3, GCS, ABS, SFTP, WebDAV, NATS, OSS, Tigris, R2, B2). Integration tests are skipped unless the -integration flag is set.",
  "concepts": [
    "integration test",
    "test flag gate",
    "replica client factory",
    "MustOpenDB",
    "MustOpenSQLDB",
    "MockSFTPServer",
    "backend provisioning",
    "test helpers",
    "S3",
    "GCS",
    "SFTP",
    "WebDAV",
    "NATS",
    "OSS",
    "Tigris",
    "R2"
  ],
  "categories": [
    "testing",
    "infrastructure",
    "litestream"
  ],
  "source_docs": [
    "05ab44549f9219b4"
  ],
  "backlinks": null,
  "word_count": 347,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This package is the shared scaffolding for all litestream integration tests. It abstracts backend provisioning behind a uniform API so test authors can write a single test function and run it against every backend by iterating `ReplicaClientTypes()`.

## Integration Flag Gate

`Integration()` returns true if the `-integration` command-line flag is set. Most functions in this package call `tb.Skip(...)` when integration mode is off, allowing unit test runs (`go test ./...`) to pass without cloud credentials. CI can then run `go test -integration ./...` with credentials injected as environment variables.

## Database Helpers

- `MustOpenDB` / `MustOpenDBAt`: Create a litestream `DB` in a temp directory and register cleanup with `tb.Cleanup`.
- `MustOpenDBs` / `MustCloseDBs`: Variant that also opens the associated `database/sql.DB` for running SQL statements.
- `MustOpenSQLDB` / `MustCloseSQLDB`: Standalone `database/sql` open/close for cases that do not need the litestream wrapper.

All `Must*` functions call `tb.Fatal` on error, keeping test code clean.

## Replica Client Factory

`NewReplicaClient(tb, typ)` dispatches to a per-backend constructor based on the type string. The full set of supported backends:

| Type | Backend |
|---|---|
| `file` | Local filesystem |
| `s3` | AWS S3 (via env vars) |
| `tigris` | Fly.io Tigris (S3-compatible) |
| `r2` | Cloudflare R2 (S3-compatible) |
| `b2` | Backblaze B2 (S3-compatible) |
| `gs` | Google Cloud Storage |
| `abs` | Azure Blob Storage |
| `sftp` | SFTP |
| `webdav` | WebDAV |
| `nats` | NATS JetStream |
| `oss` | Alibaba Cloud OSS |

Each constructor reads credentials from environment variables and calls `tb.Skip` if the required variables are not set.

## MockSFTPServer

`MockSFTPServer` spins up an in-process SSH server with a SFTP subsystem so SFTP tests can run without an external server. It accepts a host key, starts a TCP listener on a random port, and serves SFTP over SSH using `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`.

## MustDeleteAll

Calls `DeleteAll` on a replica client and registers the call at test start (not cleanup) so any leftover objects from a previous failed run are cleared before the test begins.