---
{
  "title": "Test Suite for Alibaba Cloud OSS Replica Client",
  "summary": "Unit tests for the OSS client covering type identity, bucket validation, idempotent initialization, URL and host parsing, not-found error detection, LTX path formatting, and batch delete error checking.",
  "concepts": [
    "OSS client",
    "bucket validation",
    "idempotent Init",
    "ParseURL",
    "ParseHost",
    "isNotExists",
    "deleteResultError",
    "region detection",
    "URL parsing",
    "batch delete"
  ],
  "categories": [
    "testing",
    "cloud",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "9399b3d566b0f5a6"
  ],
  "backlinks": null,
  "word_count": 248,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file exercises the pure-logic components of the OSS client without requiring real Alibaba Cloud credentials or a live OSS endpoint. All tests use the zero-value or minimally-configured client.

## Initialization Validation

`TestReplicaClient_Init_BucketValidation` covers three cases:
- Empty bucket name returns a specific error `"oss: bucket name is required"` rather than an opaque SDK panic or nil-pointer dereference.
- A valid bucket with an explicit region succeeds.
- A valid bucket with no region succeeds using `DefaultRegion`.

These tests exist because misconfigured clients fail in confusing ways without early validation. The explicit error message helps operators immediately identify the configuration problem.

`TestReplicaClient_Init_Idempotent` calls `Init` twice and verifies neither call returns an error. This guards against a double-initialization bug where the second call overwrites a valid client or re-authenticates unnecessarily.

## URL and Host Parsing

`TestParseURL` covers five URL patterns: simple `oss://bucket/path`, region-embedded hostname, bucket-only URL, and two invalid schemes. `TestParseHost` exercises the regex that extracts bucket, region, and endpoint from OSS hostnames, including the bucket-only case, the bucket-with-region case, and malformed hostnames.

## Error Classification

`TestIsNotExists` verifies that `isNotExists` correctly identifies OSS SDK not-found errors vs. other error types. This prevents litestream from treating a missing LTX file as a fatal error when it should fall back to an earlier generation.

## Batch Delete

`TestDeleteResultError` tests the custom delete result comparison logic with cases where all objects are deleted (no error), some are missing from the result (error), and the result list is empty (error).