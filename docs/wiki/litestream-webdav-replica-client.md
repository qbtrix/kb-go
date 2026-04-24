---
{
  "title": "Litestream WebDAV Replica Client",
  "summary": "Implements the Litestream ReplicaClient interface over WebDAV, enabling SQLite databases to be continuously replicated to any WebDAV-compatible server. Handles LTX file listing, streaming uploads with explicit Content-Length, range-aware downloads, and directory-safe deletion.",
  "concepts": [
    "ReplicaClient",
    "WebDAV",
    "LTX",
    "PROPFIND",
    "MKCOL",
    "chunked encoding",
    "Content-Length",
    "range request",
    "litestream",
    "replica",
    "gowebdav",
    "lazy init",
    "Prometheus metrics",
    "URL scheme registration"
  ],
  "categories": [
    "storage",
    "litestream",
    "networking",
    "replication"
  ],
  "source_docs": [
    "0663d9d2f104f9e0"
  ],
  "backlinks": null,
  "word_count": 659,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Litestream's replica architecture is plugin-based: any storage backend that satisfies the `ReplicaClient` interface can receive LTX files. This package targets WebDAV — a widely supported HTTP extension present in NAS devices, Nextcloud, ownCloud, and many self-hosted storage solutions — making Litestream accessible to operators who cannot use S3 or GCS.

## Registration

The package's `init()` function registers the client factory under both `"webdav"` and `"webdavs"` URL schemes:

```go
litestream.RegisterReplicaClientFactory("webdav", NewReplicaClientFromURL)
litestream.RegisterReplicaClientFactory("webdavs", NewReplicaClientFromURL)
```

This means users configure replication with `webdav://host/path` or `webdavs://host/path` (TLS), and Litestream's URL parser automatically instantiates this client. `NewReplicaClientFromURL` extracts host, path, and `userinfo` credentials from the parsed URL components, then constructs a `ReplicaClient` with those fields set.

## Lazy Initialization

The internal `gowebdav.Client` is created on first use via the private `init(ctx)` method (distinct from the package-level `init()`). The public `Init(ctx)` delegates to it. This lazy pattern means construction never fails — only the first actual operation can surface connectivity errors. A mutex (`mu`) serializes client creation so concurrent calls to any method before initialization completes don't create multiple clients.

## LTX File Listing

`LTXFiles` calls PROPFIND on the replica path for the given level, parses the returned `FileInfo` list, sorts by `MinTXID`, and filters to entries at or after the `seek` parameter. The sort is essential: Litestream processes LTX files in TXID order, and WebDAV PROPFIND responses are unordered by the protocol. Without sorting, a restore could replay transactions out of order and produce a corrupt database.

When the directory is absent (404/not-found), `LTXFiles` returns an empty iterator rather than an error, treating a missing directory as an empty replica — correct behavior for fresh deployments.

## Uploads: Avoiding Chunked Encoding

`WriteLTXFile` buffers the incoming `io.Reader` into a local temp file before uploading:

```go
// Upload with Content-Length header using seekable temp file.
// WriteStreamWithLength requires both a seekable reader and known size,
// which we now have from the temp file. This avoids chunked encoding
// and ensures reliable uploads across all WebDAV server configurations.
client.WriteStreamWithLength(filename, tmpFile, size, 0644)
```

Many WebDAV server implementations (especially on NAS firmware) reject chunked transfer encoding or do not buffer the body correctly. Buffering to a temp file gives a known size so the upload can set a concrete `Content-Length` header. The trade-off is temporary disk usage equal to one LTX file, which is bounded by the WAL segment size.

Before uploading, the method ensures the parent directory exists via an `MKCOL` call, wrapped to ignore conflicts (directory already exists). Without this, the first file upload into a new level directory would fail.

## Downloads: Range Request Fallback

`OpenLTXFile` has three code paths depending on `offset` and `size`:

1. **Both set** — Issues a `ReadStreamRange` (HTTP Range request) for exactly `[offset, offset+size)`. Most efficient: transfers only the needed bytes.
2. **Offset only** — Issues a full `ReadStream` and discards the first `offset` bytes with `io.CopyN(io.Discard, ...)`. Used when the server does not support Range requests or when size is unknown.
3. **Neither set** — Full stream from the start.

The fallback to full-stream-then-skip exists because some WebDAV servers return 416 or garbled data for range requests. The test suite exercises this fallback explicitly.

## Deletion

`DeleteLTXFiles` iterates the supplied slice and calls `Remove` on each. Not-found errors are suppressed because a file may have already been removed by a parallel compaction run — returning an error in that case would cause the compaction to abort unnecessarily. `DeleteAll` removes the entire configured path, used during database teardown or reset.

## Observability

Every operation increments Prometheus counters via `internal.OperationTotalCounterVec` and `internal.OperationBytesCounterVec` with the `"webdav"` label. This lets operators monitor PUT/GET/DELETE rates and byte volumes per replica type from a unified metrics endpoint.

## Known Gaps

No TODO or FIXME markers. The `DefaultTimeout` of 30 seconds is applied to the HTTP client but not to individual operations, so a very slow upload that stays open may not be bounded by this timeout depending on the `gowebdav` library implementation.