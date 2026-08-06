---
{
  "title": "Azure Blob Storage Replica Client for Litestream",
  "summary": "The `abs` package implements `litestream.ReplicaClient` for Azure Blob Storage, handling LTX file upload, listing, reading, and deletion. It supports three authentication modes (SAS token, shared key, DefaultAzureCredential chain) and exposes a lazy-initialized client with a configurable retry policy.",
  "concepts": [
    "Azure Blob Storage",
    "ReplicaClient",
    "LTX files",
    "SAS token",
    "shared key auth",
    "DefaultAzureCredential",
    "lazy initialization",
    "retry policy",
    "blob listing",
    "Litestream plugin",
    "metadata constraints"
  ],
  "categories": [
    "litestream",
    "Azure",
    "storage",
    "replication"
  ],
  "source_docs": [
    "7597fad31b1ffa17"
  ],
  "backlinks": null,
  "word_count": 522,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Litestream's storage backend is pluggable via the `ReplicaClient` interface. The `abs` package gives teams running on Azure a native blob storage replica without routing traffic through a third-party adapter. It registers itself via `init()` so it is available to any program that blank-imports the package.

## Self-Registration

```go
func init() {
    litestream.RegisterReplicaClientFactory("abs", NewReplicaClientFromURL)
}
```

This pattern means callers never need to reference the `abs` package directly — a URL like `abs://account@container/path` is enough for the factory to construct the right client. The `abs` scheme is also used as the `ReplicaClientType` constant returned by `Type()`, which the Litestream store uses for logging and metrics tagging.

## Lazy Initialization

`Init` is guarded by `sync.Mutex` and a nil check on `c.client`:

```go
if c.client != nil { return nil }
```

This prevents the expensive credential resolution and HTTP client construction from running more than once, even if `Init` is called concurrently from multiple goroutines. The alternative — initializing in `NewReplicaClient` — would fail for URL-constructed clients where credentials are not yet known at construction time.

## Authentication Priority

The client supports three credential modes, checked in this order:

1. **SAS token** — highest priority, typically used for container-scoped temporary access. The token is appended as a query string to the endpoint URL. A warning is logged if both SAS token and account key are set.
2. **Shared key** — uses `AccountName` + `AccountKey` (or `LITESTREAM_AZURE_ACCOUNT_KEY` env var) for permanent machine-to-machine access.
3. **Default credential chain** — falls back to `azidentity.NewDefaultAzureCredential()`, which checks managed identity, environment variables, and developer CLI credentials in sequence. This is the right choice for workloads running in Azure with managed identities.

The priority order prevents a misconfigured environment from silently using weaker credentials than intended.

## Metadata Key Constraint

```go
const MetadataKeyTimestamp = "litestreamtimestamp"
```

Azure Blob metadata keys must be valid C# identifiers — no hyphens allowed. The natural key `litestream-timestamp` would be rejected by the Azure API, causing silent data loss of the timestamp field. The single-word form `litestreamtimestamp` avoids that constraint.

## Retry Policy

The `azblob.ClientOptions` configure 10 retries with a 1s base delay, 30s cap, and 15-minute per-operation timeout. The retry status codes cover the standard transient HTTP failures (429, 500, 502, 503, 504) but also 408 (request timeout), which Azure Storage occasionally returns under load. Without 408 in the list, short-lived connection resets would not be retried.

## LTX File Iteration

`ltxFileIterator` implements a paginated listing of blobs under a given level prefix. It uses Azure's `NewListBlobsFlatPager` which returns pages of up to 5000 items. The iterator loads pages lazily (`loadNextPage`) so memory usage is bounded regardless of how many LTX files exist. The `seek` parameter skips blobs whose names sort before the seek position, enabling efficient resume from a known transaction ID.

## Known Gaps

- `DeleteAll` is a destructive operation that removes all blobs under the configured path. It has no confirmation prompt or dry-run mode, which is a risk in operational contexts.
- The `SASToken` field is stored in plaintext on the struct. Callers that log the client struct with `%+v` may inadvertently leak the token to log aggregators.