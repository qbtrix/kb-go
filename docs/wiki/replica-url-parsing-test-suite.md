---
{
  "title": "Replica URL Parsing Test Suite",
  "summary": "Tests every URL scheme, provider endpoint detection, query parameter parsing, and S3 Access Point ARN handling that the replica URL registry supports. Exercises both the basic dispatch path and the provider-specific default injection logic.",
  "concepts": [
    "URL scheme dispatch",
    "backend factory",
    "provider detection",
    "S3 Access Point",
    "ARN",
    "query parameter override",
    "Cloudflare R2",
    "Tigris",
    "endpoint scheme",
    "webdavs normalization",
    "BoolQueryValue"
  ],
  "categories": [
    "testing",
    "replication",
    "storage backends",
    "test"
  ],
  "source_docs": [
    "6407e6ddae8fd336"
  ],
  "backlinks": null,
  "word_count": 380,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file validates `replica_url.go` — the URL dispatch and factory registry layer. Because that file is pure logic with no network calls, tests are synchronous and exhaustive: every supported scheme, every provider detection function, and every edge case in path and ARN parsing gets a dedicated case.

## Backend Dispatch Tests

`TestNewReplicaClientFromURL` creates a client for each registered scheme (`s3`, `gs`, `abs`, `sftp`, `file`, `nats`, `oss`, `webdav`, `webdavs`) and type-asserts to the concrete struct to verify field mapping. This matters because the factory receives parsed URL components; a mistake in how the URL is split (e.g., bucket in path vs. host) would produce a client that silently targets the wrong bucket.

`TestReplicaTypeFromURL` checks that the public type string matches expectations, including that `webdavs` normalizes to `webdav`.

## Provider Detection Tests

Each `Is*Endpoint` function gets a standalone test:

- `TestIsTigrisEndpoint`, `TestIsDigitalOceanEndpoint`, `TestIsBackblazeEndpoint`, `TestIsFilebaseEndpoint`, `TestIsScalewayEndpoint`, `TestIsCloudflareR2Endpoint`, `TestIsSupabaseEndpoint`, `TestIsHetznerEndpoint`, `TestIsMinIOEndpoint`, `TestIsLocalEndpoint` — each test provides known positive hostnames and negative hostnames to guard against overly broad or overly narrow matching. A false positive would apply wrong defaults; a false negative would leave a user with broken uploads.

## S3 Provider Defaults

`TestS3ProviderDefaults` verifies that creating an S3 client from a Cloudflare R2 or Tigris URL automatically sets the correct concurrency limit and consistency header without the user specifying them. `TestS3ProviderDefaults_QueryParamOverrides` verifies the inverse: an explicit query parameter (`?concurrency=5`) takes precedence over the auto-detected default. Without this override mechanism, users with non-standard provider configurations would have no escape hatch.

## ARN and Path Tests

`TestRegionFromS3ARN` checks that region extraction from ARN strings handles all ARN formats including Access Point ARNs with embedded region fields.

`TestParseS3AccessPointURL` exercises the special ARN URL parser with both valid and malformed inputs.

`TestCleanReplicaURLPath` checks that leading slashes are stripped correctly.

## Query and Boolean Helpers

`TestBoolQueryValue` covers the multi-key boolean query helper that lets users write `?path-style=true` or `?force-path-style=true` interchangeably. This prevents silent misconfiguration when users use one alias but the code only checks the other.

## Endpoint Scheme Tests

`TestEnsureEndpointScheme` verifies that `http://` is prepended for localhost and that `https://` is used for public endpoints when no scheme is present.

## Known Gaps

No TODOs. Integration against real provider endpoints is intentionally absent — these tests use only the detection logic, not live network calls.