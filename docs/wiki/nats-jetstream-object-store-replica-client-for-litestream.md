---
{
  "title": "NATS JetStream Object Store Replica Client for Litestream",
  "summary": "Implements the ReplicaClient interface using NATS JetStream's object store as the replication backend, supporting multiple authentication methods, TLS, and reconnect configuration. LTX file timestamps are stored in object headers to support point-in-time restore.",
  "concepts": [
    "NATS",
    "JetStream",
    "object store",
    "ReplicaClient",
    "NKey",
    "JWT authentication",
    "TLS",
    "LTX path encoding",
    "timestamp header",
    "point-in-time restore",
    "lazy initialization",
    "reconnect"
  ],
  "categories": [
    "storage",
    "replication",
    "messaging",
    "litestream"
  ],
  "source_docs": [
    "0896e81797b16d36"
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

This client stores LTX files as objects in a NATS JetStream Object Store bucket. NATS is a messaging system with a built-in persistent key/value and object store, making it an unusual but viable litestream backend for deployments that already run NATS infrastructure. The client registers under the `"nats"` URL scheme.

## Authentication Options

NATS supports a wide range of authentication mechanisms, and the client exposes all of them:
- **JWT + Seed**: For NKey-based decentralized auth.
- **Credentials file**: A `.creds` file combining JWT and seed.
- **NKey**: NKey-only without JWT.
- **Username/Password** and **Token**: For simple setups.
- **TLS**: Client certificate and custom root CAs.

This breadth exists because NATS deployments vary widely; the client must be usable in both small self-hosted clusters and large Synadia-managed networks.

## Initialization and Lazy Connection

`Init` acquires a mutex, checks if already connected, and if not, calls `connect` to establish the NATS connection, then `initObjectStore` to locate or create the JetStream object store bucket. The idempotency guard prevents redundant TLS handshakes and credential exchanges on repeated calls.

## Path Encoding

LTX file paths within the bucket follow the format:

```
ltx/<level>/<minTXID>-<maxTXID>.ltx
```

`ltxPath` generates and `parseLTXPath` parses this format. The zero-padded 16-character hexadecimal TXID encoding ensures lexicographic ordering matches chronological order, which is critical for the seek-based iteration used by restore.

## Timestamp Header

NATS object store allows custom headers on objects. The client stores the LTX commit timestamp under `HeaderKeyTimestamp = "Litestream-Timestamp"` as a Unix millisecond string. On listing, if `useMetadata` is true, the header is read to provide accurate timestamps for point-in-time restore.

## File Iterator

`ltxFileIterator` holds a pre-fetched slice of `*ltx.FileInfo` and serves them via `Next()`/`Item()`. This materialized approach (list all, then iterate) differs from the streaming GCS iterator but is appropriate for NATS since object listing returns all objects at once.

## Known Gaps

The reconnect behavior after connection loss (handled by NATS client library's `MaxReconnects` and `ReconnectWait` fields) is configurable, but the client does not expose a circuit breaker or manual reconnect trigger. If the NATS server is unavailable at `Init` time, the entire litestream startup fails.