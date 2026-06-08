---
{
  "title": "Alibaba Cloud OSS Replica Client for Litestream",
  "summary": "Implements the ReplicaClient interface for Alibaba Cloud Object Storage Service (OSS), with concurrent metadata fetching for accurate timestamps, multipart uploads, parallel batch deletes, and a streaming file iterator backed by paginated OSS list calls.",
  "concepts": [
    "Alibaba Cloud OSS",
    "ReplicaClient",
    "metadata concurrency",
    "HeadObject",
    "semaphore",
    "paginated listing",
    "batch delete",
    "deleteResultError",
    "URL parsing",
    "region",
    "timestamp preservation"
  ],
  "categories": [
    "storage",
    "cloud",
    "replication",
    "litestream"
  ],
  "source_docs": [
    "c4aa5689e4bfc933"
  ],
  "backlinks": null,
  "word_count": 335,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This client connects litestream to Alibaba Cloud OSS, which is the dominant object storage service for China-based deployments. It registers under the `"oss"` URL scheme and handles OSS-specific quirks around URL parsing (bucket-embedded region in the hostname), metadata key prefixing, and batch delete error reporting.

## URL and Host Parsing

OSS URLs can embed the bucket and region in the hostname: `bucket.oss-cn-hangzhou.aliyuncs.com`. `ParseHost` uses a regex to extract these components. `ParseURL` validates that the scheme is `oss://` and delegates host parsing. The `DefaultRegion = "cn-hangzhou"` is applied when no region is specified.

## Initialization

`Init` is mutex-guarded and idempotent. It validates that `Bucket` is non-empty (returning a specific error rather than an opaque SDK error), constructs the OSS client with credentials, and sets the endpoint. If `Endpoint` is blank, the standard regional endpoint is used.

## Streaming File Iterator with Metadata Concurrency

`fileIterator` is a lazy streaming iterator backed by OSS's paginated `ListObjects` API. Each page is fetched on demand when the current page is exhausted. 

Because OSS's `ListObjects` response does not include custom metadata (only `LastModified`), accurate timestamps require a separate `HeadObject` call per file. The iterator batches these using a semaphore-controlled goroutine pool (`DefaultMetadataConcurrency = 50` concurrent `HeadObject` calls). This approach avoids the thundering herd that would result from 50,000 unconstrained goroutines on a large replica, while still parallelizing the metadata fetches to reduce wall-clock time.

## Batch Delete Error Handling

`DeleteLTXFiles` issues batch delete requests (up to `MaxKeys = 1000` per request). OSS's SDK returns a result object rather than a simple error, so `deleteResultError` compares the set of requested keys against the set of successfully deleted keys to detect partial failures. This workaround exists because the OSS SDK does not surface per-object delete errors as Go errors.

## Known Gaps

The `MetadataKeyTimestamp` constant documents that the OSS SDK automatically adds the `x-oss-meta-` prefix when setting custom metadata. If a future SDK version changes this behavior, the stored key name would silently change and break timestamp-based restore for existing replicas.