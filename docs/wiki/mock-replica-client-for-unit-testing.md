---
{
  "title": "Mock Replica Client for Unit Testing",
  "summary": "A configurable mock implementation of the ReplicaClient interface where every method is backed by an injectable function field. Allows tests to simulate arbitrary backend behavior without a real storage backend.",
  "concepts": [
    "mock",
    "function field pattern",
    "ReplicaClient",
    "test double",
    "compile-time interface check",
    "nil guard",
    "injectable behavior",
    "unit testing"
  ],
  "categories": [
    "testing",
    "mocking",
    "litestream"
  ],
  "source_docs": [
    "e4ae0946c515afc9"
  ],
  "backlinks": null,
  "word_count": 237,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The `mock.ReplicaClient` is a test double that satisfies the `litestream.ReplicaClient` interface. Rather than using a full mocking framework, it uses the function-field pattern: each method delegates to a corresponding `*Func` field, enabling per-test behavior customization without generating mock code.

## Design Pattern

Each interface method has a corresponding `func` field:

```go
type ReplicaClient struct {
    InitFunc           func(ctx context.Context) error
    DeleteAllFunc      func(ctx context.Context) error
    LTXFilesFunc       func(...) (ltx.FileIterator, error)
    OpenLTXFileFunc    func(...) (io.ReadCloser, error)
    WriteLTXFileFunc   func(...) (*ltx.FileInfo, error)
    DeleteLTXFilesFunc func(ctx context.Context, a []*ltx.FileInfo) error
}
```

Methods that have a corresponding `*Func` field call it if non-nil. `Init` has a nil guard (returns nil if `InitFunc == nil`), which makes the zero-value struct usable in tests that only care about other methods. `DeleteAll` and other methods call through unconditionally, which means tests that do not configure the func will panic with a nil function call—a deliberate choice that makes missing setup immediately obvious rather than silently wrong.

## Compile-Time Interface Check

```go
var _ litestream.ReplicaClient = (*ReplicaClient)(nil)
```

This line ensures that if `litestream.ReplicaClient` gains a new method, this file will fail to compile, forcing the mock to be updated before tests can run.

## SetLogger

`SetLogger` is a no-op (`{}`). Mocks rarely need real logging, so this silences the logger without requiring tests to provide one.

## Usage

Typical test usage:

```go
client := &mock.ReplicaClient{
    WriteLTXFileFunc: func(...) (*ltx.FileInfo, error) {
        return &ltx.FileInfo{Size: 100}, nil
    },
}
```