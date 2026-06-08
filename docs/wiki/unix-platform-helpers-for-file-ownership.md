---
{
  "title": "Unix Platform Helpers for File Ownership",
  "summary": "Provides the Unix-specific implementations of Fileinfo (uid/gid extraction via syscall.Stat_t) and a pass-through fixRootDirectory, guarded by a build tag that covers all POSIX-like targets.",
  "concepts": [
    "build tag",
    "syscall.Stat_t",
    "uid",
    "gid",
    "file ownership",
    "Fileinfo",
    "POSIX",
    "cross-platform",
    "os.FileInfo",
    "type assertion",
    "MkdirAll"
  ],
  "categories": [
    "platform",
    "utilities",
    "litestream"
  ],
  "source_docs": [
    "72259583af8afd07"
  ],
  "backlinks": null,
  "word_count": 267,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is one half of a platform split: the build tag covers aix, darwin, dragonfly, freebsd, linux, netbsd, openbsd, and solaris. The Windows counterpart is in `internal_windows.go`. Together they provide a consistent API surface that callers in `internal.go` use without knowing the target OS.

## Fileinfo — Extracting uid/gid

`Fileinfo` takes an `os.FileInfo` (the result of `os.Stat`, `os.Lstat`, or a directory listing) and returns the numeric uid and gid of the owning user and group.

The extraction relies on type-asserting `fi.Sys()` to `*syscall.Stat_t`. On all POSIX targets, Go's OS abstraction stores the raw syscall stat structure behind this interface. If the assertion fails (e.g., on a virtual filesystem that returns a different underlying type), the function returns `-1, -1` as a sentinel meaning "ownership unknown, do not attempt to Chown".

This is used by `CreateFile` and `MkdirAll` in `internal.go` to preserve the uid/gid of an original database file when litestream writes replicated or restored files. Without this, files restored by a root-running litestream process would be owned by root rather than the application user.

## fixRootDirectory

On Unix the root-directory fix is a no-op pass-through—the path `"/"` does not need adjustment. The function exists solely to satisfy the shared call site in `MkdirAll`'s implementation, which calls it before traversing path segments. The Windows version of this function handles a Windows-specific UNC path quirk.

## Build Tag Coverage

The tag `//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris` covers every Unix-like platform Go officially targets. Adding a new Unix-like target to Go would require updating this tag.