---
{
  "title": "S3 Replica Client",
  "summary": "Full-featured S3 replica client that writes, lists, and reads LTX files for Litestream replication. Handles multi-part uploads, provider-specific defaults (R2, Tigris, Backblaze, etc.), SSE-C and SSE-KMS encryption, and backward-compatible v0.3.x generation listing.",
  "concepts": [
    "S3 ReplicaClient",
    "LTX files",
    "multipart upload",
    "Transfer Manager",
    "provider detection",
    "SSE-C",
    "SSE-KMS",
    "fileIterator",
    "metadata caching",
    "Cloudflare R2",
    "Tigris",
    "chunked encoding",
    "GenerationsV3",
    "CompactionLevel",
    "findBucketRegion"
  ],
  "categories": [
    "replication",
    "storage backends",
    "litestream"
  ],
  "source_docs": [
    "97ee28d13eff3111"
  ],
  "backlinks": null,
  "word_count": 424,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`ReplicaClient` implements `litestream.ReplicaClient` using the AWS SDK v2. It manages a lazy-initialized S3 client and multipart uploader, and exposes methods for listing, reading, writing, and deleting LTX files at each compaction level.

## Initialization and Configuration

`Init` builds the AWS config from the struct fields: `AccessKeyID`/`SecretAccessKey` for explicit credentials, `Region` with auto-discovery fallback via `findBucketRegion`, and `Endpoint` for S3-compatible providers. `configureEndpoint` applies provider-specific adjustments:

- **Cloudflare R2**: sets `Concurrency=2` to avoid R2's parallel upload limits.
- **Tigris**: injects a `fly-prefer-regional=true` header for read-your-writes consistency.
- **Custom endpoints**: disables AWS chunked encoding and Content-MD5 checksums that non-AWS providers may reject.

`validateSSEConfig` enforces that SSE-C fields are all provided or all absent — a partial SSE-C config would cause every write to fail with a cryptic AWS error.

## LTX File Layout

LTX files are stored at `{path}/L{level}/{minTXID}-{maxTXID}.ltx`. The `level` component maps directly to the `CompactionLevel` index. `LTXFiles` paginates through S3 using a `fileIterator` that lazily fetches pages and optionally reads object metadata for TXID and size information without downloading the full file.

## Upload Mechanics

`WriteLTXFile` uses the S3 Transfer Manager's multipart uploader. The `PartSize` and `Concurrency` fields control upload chunking. When `RequireContentMD5` is set, a custom middleware (`contentMD5StackKey`) precomputes the MD5 and injects it into the request. The middleware is necessary because the Transfer Manager constructs the request body after the middleware chain has already run, making it impossible to compute a streaming MD5 in the normal SDK request pipeline.

## Metadata Caching

The `fileIterator` batches object metadata reads to reduce API call count. `fetchMetadataBatch` issues `HeadObject` calls concurrently, bounded by `MetadataConcurrency`, and caches results in `metadataCache`. This is an optimization for restore planning, where the client needs TXID ranges without downloading file content.

## Backward Compatibility

`GenerationsV3`, `SnapshotsV3`, and `WALSegmentsV3` list the v0.3.x backup layout (`{path}/generations/{generation}/snapshots/` and `wal/`). These exist so a fresh Litestream instance can discover and restore from backups created by older versions before migrating them to the current LTX layout.

## Encryption

SSE-C (customer-provided key) and SSE-KMS (managed key) are configured via struct fields. The key, algorithm, and MD5 of the key are injected into every write request header. Reads require the same headers to decrypt — `OpenLTXFile` also injects them.

## Known Gaps

- `SignPayload` disables AWS Signature V4 payload signing for providers that do not support it, but chunked encoding is disabled separately; the interaction between these two flags is not explicitly documented.
- Region auto-discovery via `findBucketRegion` makes an additional `GetBucketLocation` call on every `Init`; there is no caching if `Init` is called multiple times.