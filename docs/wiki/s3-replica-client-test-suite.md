---
{
  "title": "S3 Replica Client Test Suite",
  "summary": "Tests for the S3 ReplicaClient covering payload signing modes, multipart thresholds, endpoint configuration, SSE-C/SSE-KMS headers, provider-specific defaults, and delete request Content-MD5 generation. Uses in-process HTTP servers to avoid live S3 calls.",
  "concepts": [
    "payload signing",
    "Signature V4",
    "chunked encoding",
    "multipart upload",
    "SSE-C",
    "SSE-KMS",
    "Content-MD5",
    "delete objects",
    "XML marshalling",
    "provider detection",
    "Cloudflare R2",
    "ARN",
    "custom endpoint",
    "httptest"
  ],
  "categories": [
    "testing",
    "storage backends",
    "litestream",
    "test"
  ],
  "source_docs": [
    "e9f34769a6374fda"
  ],
  "backlinks": null,
  "word_count": 373,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file validates the S3 `ReplicaClient` using `net/http/httptest` servers that inspect request headers and bodies. Because the AWS SDK constructs HTTP requests internally, tests must capture them at the transport level to assert that the correct protocol options are being sent.

## Payload Signing Tests

`TestReplicaClientPayloadSigning` and `TestReplicaClient_UnsignedPayload_NoChunkedEncoding` verify that `SignPayload=true` uses Signature V4 body signing, while `SignPayload=false` sends `UNSIGNED-PAYLOAD`. The former is required by AWS S3; the latter is required by several S3-compatible providers that do not implement chunked transfer encoding.

`TestReplicaClient_SignedPayload_CustomEndpoint_NoChunkedEncoding` confirms that even with payload signing enabled, a custom endpoint suppresses `aws-chunked` Transfer-Encoding, because many compatible stores reject that encoding.

## Multipart Upload Threshold

`TestReplicaClient_MultipartUploadThreshold` creates LTX blobs above and below the part-size threshold and verifies that the Transfer Manager switches between single-part PUT and multipart PUT correctly. Getting this wrong either wastes API calls on small files or tries to single-PUT files that exceed the S3 5 GB limit.

## SSE Tests

`TestReplicaClient_SSE_C_Validation` ensures that partial SSE-C configuration (key without algorithm or vice versa) is rejected at `Init` time. `TestReplicaClient_SSE_C_Headers` and `TestReplicaClient_SSE_KMS_Headers` capture upload requests and assert that the correct header names and values are present. `TestReplicaClient_NoSSE_Headers` confirms that no encryption headers leak through when SSE is not configured.

## Provider Default Tests

`TestReplicaClient_R2ConcurrencyDefault` creates a client pointed at a Cloudflare R2 endpoint and asserts `Concurrency == 2` after `Init`. `TestReplicaClient_ProviderEndpointDetection` exercises each `Is*Endpoint` function indirectly through client construction.

`TestReplicaClient_CustomEndpoint_DisablesChecksumFeatures` verifies that `RequireContentMD5` and chunked encoding are both disabled when targeting a custom endpoint — the two features S3-compatible stores most frequently reject.

## Delete Request Tests

`TestReplicaClientDeleteLTXFiles_ContentMD5` asserts that batch delete requests include a valid `Content-MD5` header, which AWS S3 requires for delete-objects operations. `TestReplicaClientDeleteLTXFiles_PreexistingContentMD5` checks that a pre-set header is not overwritten.

`TestMarshalDeleteObjects_EdgeCases` and `TestEncodeObjectIdentifier_AllFields` test the XML marshalling path for delete payloads, covering empty inputs and keys with special characters.

`TestComputeDeleteObjectsContentMD5_Deterministic` asserts that repeated calls with the same input produce identical MD5 values — critical for request retry correctness.

## URL Parsing Edge Cases

`TestParseHost` validates host extraction from S3-compatible provider URLs including path-style vs. virtual-hosted-style. `TestReplicaClient_AccessPointARN` tests ARN-based bucket addressing end-to-end.

## Known Gaps

The test for `TestReplicaClient_TigrisConsistentHeader` verifies the `fly-prefer-regional` header but only on write — read requests are not checked.