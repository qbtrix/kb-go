---
{
  "title": "Google Cloud Storage Replica Client for Litestream",
  "summary": "Implements the ReplicaClient interface backed by Google Cloud Storage (GCS), writing LTX transaction files as GCS objects. Timestamps are stored in custom GCS object metadata so they survive round-trips and can be used for accurate point-in-time restore.",
  "concepts": [
    "Google Cloud Storage",
    "GCS",
    "ReplicaClient",
    "LTX metadata",
    "timestamp preservation",
    "litestream-timestamp",
    "lazy initialization",
    "sync.Mutex",
    "object iterator",
    "point-in-time restore",
    "Application Default Credentials"
  ],
  "categories": [
    "storage",
    "replication",
    "cloud",
    "litestream"
  ],
  "source_docs": [
    "98a8208a2224d14a"
  ],
  "backlinks": null,
  "word_count": 353,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This package connects litestream's replication pipeline to Google Cloud Storage. It implements the `ReplicaClient` interface and registers itself under the `"gs"` URL scheme at package init time, allowing replica URLs like `gs://my-bucket/path`.

## Initialization and Lazy Connection

`Init` creates the `*storage.Client` and acquires a `*storage.BucketHandle` on first call. It is guarded by a `sync.Mutex` and short-circuits with a nil check so repeated calls are no-ops. This pattern prevents redundant credential round-trips when the same client is used across multiple goroutines. The GCS client uses Application Default Credentials, which means it will pick up service account credentials from the environment without explicit configuration.

## Timestamp Metadata

GCS does not let callers control an object's `LastModified` time—that is set by the server on upload. To preserve the original transaction commit time across replication, the client stores the timestamp in a custom metadata field keyed by `MetadataKeyTimestamp = "litestream-timestamp"`. When `useMetadata` is true (timestamp-based restore), `LTXFiles` issues a `HeadObject`-equivalent call to read this field. When false, it falls back to `LastModified` for performance.

## LTX File Iterator

`ltxFileIterator` wraps the GCS object listing API's `*iterator.Iterator`. Each `Next()` call advances the underlying GCS pager and parses the object name into level, minTXID, and maxTXID components. Objects whose names cannot be parsed as LTX filenames are skipped. The iterator holds a reference to the client so it can fetch per-object metadata on demand when `useMetadata` is set.

## Writing

`WriteLTXFile` peeks at the LTX header using a `TeeReader` to extract the commit timestamp, then uploads the full byte stream as a GCS object. The timestamp is written into the object's metadata map. GCS uploads are single-shot (no multipart); large files are handled by the GCS client library's internal streaming.

## Error Normalization

`isNotExists` translates GCS-specific not-found errors into a uniform sentinel so callers do not need to import the GCS SDK just to check for missing files.

## Known Gaps

The GCS client does not implement `ReplicaClientV3` (generation-based WAL segment protocol). Only the v2 level-based LTX interface is present, which means this backend cannot be used with the newer restore path that operates on generation UUIDs.