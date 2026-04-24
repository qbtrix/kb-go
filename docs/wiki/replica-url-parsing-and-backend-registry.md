---
{
  "title": "Replica URL Parsing and Backend Registry",
  "summary": "Implements a pluggable factory registry that maps URL schemes to replica client constructors, along with URL parsing utilities for S3 Access Point ARNs, path cleaning, and S3-compatible provider detection. Provider detection functions allow the system to auto-configure backend-specific defaults without requiring explicit user configuration.",
  "concepts": [
    "ReplicaClientFactory",
    "URL scheme",
    "S3 Access Point",
    "ARN parsing",
    "provider detection",
    "Cloudflare R2",
    "MinIO",
    "Tigris",
    "registry pattern",
    "endpoint normalization",
    "webdav",
    "URL query parameters"
  ],
  "categories": [
    "replication",
    "storage backends",
    "configuration"
  ],
  "source_docs": [
    "1b807004e761ac98"
  ],
  "backlinks": null,
  "word_count": 428,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is the dispatch layer between a human-readable replica URL (`s3://bucket/path`) and the concrete `ReplicaClient` implementation that handles that storage backend. It uses a global registry pattern so backends can self-register via `init()` without import-order coupling.

## Factory Registry

`RegisterReplicaClientFactory` stores a `ReplicaClientFactory` function keyed by URL scheme (`s3`, `gs`, `abs`, `sftp`, `file`, `webdav`, `nats`, `oss`). A read-write mutex guards the map because backends are registered from `init()` functions at program startup, while reads happen on every `NewReplicaClientFromURL` call. `webdavs` is normalized to `webdav` before the lookup so TLS and non-TLS WebDAV share one factory, with the scheme passed through so the factory can still distinguish them.

## URL Parsing

`ParseReplicaURL` handles two URL shapes:

1. **S3 Access Point ARNs** — URLs of the form `s3://arn:aws:s3:...` contain colons that break standard URL parsing. `parseS3AccessPointURL` handles these as a special case before delegating to `net/url`.
2. **Standard URLs** — everything else goes through `net/url.Parse`.

`ParseReplicaURLWithQuery` extends the base parser to also return query parameters and `url.Userinfo`, which some backends use for credentials embedded in the URL.

`CleanReplicaURLPath` strips leading slashes from parsed URL paths. Standard URL parsing leaves a leading `/` on the path component, but storage backends expect a bucket-relative key, not an absolute path.

## S3 Access Point ARN Parsing

`splitS3AccessPointARN` extracts the bucket identifier and key from an ARN string. ARNs use `:` as a delimiter, which collides with the `//user:pass@host` convention in RFC 3986, making a dedicated parser necessary. `RegionFromS3ARN` extracts the region component so the client can be initialized with the correct AWS region without an additional API call.

## Provider Detection

A family of `Is*Endpoint` functions identifies S3-compatible providers by inspecting the endpoint hostname:

- `IsHetznerEndpoint`, `IsTigrisEndpoint`, `IsDigitalOceanEndpoint`, `IsBackblazeEndpoint`, `IsFilebaseEndpoint`, `IsScalewayEndpoint`, `IsCloudflareR2Endpoint`, `IsSupabaseEndpoint` — match known domain suffixes.
- `IsMinIOEndpoint` — catches custom endpoints that resemble MinIO (non-AWS hostnames with a path-style layout).
- `IsLocalEndpoint` — matches `localhost` and RFC 1918 addresses for development environments.

These functions exist because different providers have subtly incompatible behavior: Cloudflare R2 limits upload concurrency, Tigris requires a consistency header, some providers reject AWS chunked encoding. Rather than asking users to set obscure flags, the client auto-detects the provider and applies correct defaults.

## Endpoint Scheme Normalization

`EnsureEndpointScheme` adds `http://` to local endpoints and `https://` to everything else when no scheme is present. Without this, `aws-sdk-go-v2` would reject the endpoint or misroute requests.

## Known Gaps

No TODOs or FIXMEs are present in this file. The provider detection approach relies on hardcoded domain strings; new S3-compatible providers require a code change to receive automatic defaults.