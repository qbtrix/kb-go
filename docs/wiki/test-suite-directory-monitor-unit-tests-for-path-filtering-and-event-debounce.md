---
{
  "title": "Test Suite: Directory Monitor Unit Tests for Path Filtering and Event Debounce",
  "summary": "directory_watcher_test.go contains focused unit tests for the three internal behaviors of DirectoryMonitor that are most prone to edge-case bugs: path skip logic for SQLite auxiliary files, glob pattern matching, and pending event deduplication.",
  "concepts": [
    "unit testing",
    "path filtering",
    "shouldSkipPath",
    "matchesPattern",
    "glob pattern",
    "debounce deduplication",
    "WAL file",
    "SHM file",
    "edge cases",
    "DirectoryMonitor"
  ],
  "categories": [
    "testing",
    "litestream",
    "filesystem",
    "unit",
    "test"
  ],
  "source_docs": [
    "dee9cdc96df83047"
  ],
  "backlinks": null,
  "word_count": 294,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The integration and stress tests verify that the monitor works end-to-end, but they cannot easily isolate a failure in path filtering from a failure in the fsnotify integration. These unit tests exercise the filtering logic directly by calling the methods on a `DirectoryMonitor` struct without starting any watches.

## TestDirectoryMonitor_shouldSkipPath

Verifies that `shouldSkipPath` correctly classifies 12 paths:

- **Must skip**: `*.db-wal`, `*.db-shm`, `*.db-journal`, `*.sqlite-wal`, `*.sqlite-shm`, `*.sqlite-journal`
- **Must not skip**: `*.db`, `*.sqlite`, `*.sqlite3`, arbitrary extensions
- **Edge cases**: `withdrawal` (ends with "wal" but not `-wal`), `rhythm` (ends with "shm" but not `-shm`), `myjournal` (ends with "journal" but not `-journal`)

The edge cases prevent the filter from being implemented as a simple `strings.HasSuffix` without the leading dash — a naive implementation would incorrectly skip valid database files whose names happen to end with those substrings.

## TestDirectoryMonitor_matchesPattern

Verifies glob pattern matching across multiple patterns (`*.db`, `*.sqlite`, empty pattern which matches everything). The empty pattern case is important: with no pattern configured, the monitor should replicate all files in the directory, not none.

## TestDirectoryMonitor_pendingEvents

Verifies the debounce queue deduplication logic. When the same path appears multiple times in `pendingEvents`, `flushPendingEvents` should process it once. This is tested by injecting duplicate events and asserting the resulting action count.

Without deduplication, a burst of 100 `Create` events for the same file (which some editors generate on save) would cause 100 attempted `store.AddDB` calls, each acquiring the store lock.

## Known Gaps

- The test constructs `DirectoryMonitor` directly (`&DirectoryMonitor{}`), bypassing `NewDirectoryMonitor`. If `NewDirectoryMonitor` initializes fields that the methods depend on, the unit tests may pass while the real constructor-initialized code fails.
- `TestDirectoryMonitor_pendingEvents` imports `fsnotify` for the event type but does not test the event type filtering (Create vs. Remove vs. Write) — only the deduplication.