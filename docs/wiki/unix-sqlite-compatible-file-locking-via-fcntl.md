---
{
  "title": "Unix SQLite-Compatible File Locking via fcntl",
  "summary": "Implements exclusive file locking that precisely replicates SQLite's locking protocol on POSIX systems using fcntl byte-range locks. This ensures litestream and SQLite cannot hold conflicting locks on the same database file simultaneously.",
  "concepts": [
    "fcntl",
    "byte-range lock",
    "SQLite locking protocol",
    "F_WRLCK",
    "F_SETLKW",
    "pending byte",
    "shared region",
    "exclusive lock",
    "unix.FcntlFlock",
    "rollback on failure"
  ],
  "categories": [
    "platform",
    "concurrency",
    "sqlite",
    "litestream"
  ],
  "source_docs": [
    "9d2a4e0444c4721b"
  ],
  "backlinks": null,
  "word_count": 310,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

SQLite implements its own concurrency control on Unix via `fcntl` byte-range locks over specific regions of the database file. Litestream must acquire the same locks to safely read a consistent snapshot of the database before replicating it. Using a different locking mechanism (such as `flock`) would not block SQLite writers and could allow litestream to read a torn write.

## SQLite Lock Regions

SQLite defines three byte regions in the database file that it uses for locking:

- **Pending byte** (`0x40000000`, 1 byte): A writer acquires this to signal that it is waiting for a write lock. Readers stop acquiring new shared locks once they see a pending lock held.
- **Shared region** (`sqlitePendingByte + 2` to `+ 511`, 510 bytes): Readers hold a shared lock on one byte in this range. Writers must hold write locks on all 510 bytes.

Litestream's `LockFileExclusive` takes a write lock (`F_WRLCK`) on both the pending byte and all 510 shared bytes. This exactly replicates what SQLite does when acquiring a `PENDING` → `EXCLUSIVE` lock upgrade, preventing any SQLite reader or writer from acquiring new locks while litestream holds the exclusive position.

## Atomic Lock Acquisition with Rollback

Acquiring the two regions is not atomic at the OS level. If the pending byte lock succeeds but the shared region lock fails (e.g., because a reader is active), `LockFileExclusive` releases the pending byte lock before returning the error. Without this rollback, the pending byte would remain locked, permanently blocking all new SQLite readers.

## Unlock Order

`UnlockFile` releases both regions and returns the first error encountered. Both unlocks are always attempted, even if the first fails, to avoid leaving any byte range locked.

## `setFcntlLock` Helper

Wraps `unix.FcntlFlock` with `F_SETLKW` (blocking wait) semantics. Using `F_SETLKW` rather than `F_SETLK` means litestream will wait if another process already holds the lock, rather than returning `EAGAIN`.