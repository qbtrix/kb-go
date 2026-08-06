---
{
  "title": "Windows Platform Helpers for File Ownership and Path Handling",
  "summary": "Provides Windows-specific stubs for Fileinfo (always returns -1/-1 since Windows has no uid/gid) and fixRootDirectory, which handles the Windows extended-length path prefix `\\\\?\\c:` that needs a trailing backslash appended before directory traversal.",
  "concepts": [
    "Windows",
    "build tag",
    "uid gid stub",
    "extended length path",
    "UNC path",
    "fixRootDirectory",
    "MkdirAll",
    "MAX_PATH",
    "cross-platform"
  ],
  "categories": [
    "platform",
    "utilities",
    "litestream"
  ],
  "source_docs": [
    "9c23a2ebd9c5551f"
  ],
  "backlinks": null,
  "word_count": 218,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file pairs with `internal_unix.go` under the `//go:build windows` tag. It satisfies the same two-function contract but with platform-appropriate behavior.

## Fileinfo — No-Op on Windows

Windows does not use POSIX uid/gid ownership. `Fileinfo` always returns `-1, -1`, signaling to callers (`CreateFile`, `MkdirAll`) that they should skip the `Chown` call. This is correct behavior: trying to call `Chown` on Windows would either no-op or error, so bypassing it entirely is cleaner.

## fixRootDirectory — Windows Extended Path Prefix

`fixRootDirectory` is copied from the Go standard library's `os.MkdirAll` implementation. It addresses a Windows-specific path parsing edge case:

Windows extended-length paths use the prefix `\\?\` to bypass the MAX_PATH (260 character) limit. A path like `\\?\c:` refers to the root of drive C but is missing the trailing backslash required to make it a valid directory path. Without the trailing backslash, `os.Mkdir` on `\\?\c:` would fail or behave unexpectedly.

The function checks for this exact 6-character prefix pattern and appends `\\` if matched. All other paths are returned unchanged.

## Why Copy from stdlib?

The standard library's `os.MkdirAll` does this fix internally, but the custom `MkdirAll` in `internal.go` reimplements directory traversal to add uid/gid propagation. To stay correct on Windows, it must replicate this edge case handling. Copying the function rather than reimporting avoids breaking the abstraction boundary.