---
{
  "title": "Compaction Level Model: Multi-Tier LTX File Retention",
  "summary": "Defines the `CompactionLevel` and `CompactionLevels` types that model litestream's multi-tier compaction hierarchy, where LTX transaction files are progressively merged from fine-grained (level 0) to coarse-grained (levels 1+) at increasing time intervals. Level 9 is reserved as the snapshot level for full database snapshots.",
  "concepts": [
    "CompactionLevel",
    "CompactionLevels",
    "SnapshotLevel",
    "LTX compaction",
    "tiered storage",
    "DefaultCompactionLevels",
    "PrevCompactionAt",
    "NextCompactionAt",
    "level validation",
    "time truncation",
    "PrevLevel",
    "NextLevel",
    "MaxLevel"
  ],
  "categories": [
    "litestream",
    "compaction",
    "storage"
  ],
  "source_docs": [
    "744ac7ec8fbcc226"
  ],
  "backlinks": null,
  "word_count": 495,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

Litestream uses a tiered compaction model inspired by log-structured storage systems. Rather than storing every individual WAL transaction forever, LTX files are periodically merged upward into larger composite files covering longer time ranges. This reduces file count (and therefore storage API call costs), while retaining the ability to restore to any point in time covered by the retained levels.

## Level Structure

Each `CompactionLevel` has two fields:

- **Level** — the numeric tier (0 through 8). Must match the struct's index in the `CompactionLevels` slice.
- **Interval** — how often this level is compacted from the level below. Level 0 always has `Interval: 0` (raw LTX files written on every sync; no compaction interval).

`SnapshotLevel = 9` is a constant that represents full database snapshots. It is treated specially throughout the codebase — it cannot be retrieved from `CompactionLevels.Level()`, has no `Interval`, and is handled by the snapshot machinery rather than the incremental compaction logic.

## Default Configuration

```go
DefaultCompactionLevels = CompactionLevels{
    {Level: 0, Interval: 0},         // raw LTX files
    {Level: 1, Interval: 30s},       // 30-second rollups
    {Level: 2, Interval: 5min},      // 5-minute rollups
    {Level: 3, Interval: 1h},        // hourly rollups
}
```

This four-level default provides sub-minute granularity in recent history (level 0 files) while automatically consolidating older history into progressively larger files that require fewer API calls to list or iterate.

## Time-Based Compaction Scheduling

`PrevCompactionAt(now)` truncates `now` to the level's interval, returning when the most recent compaction boundary occurred. `NextCompactionAt(now)` adds one interval to the previous boundary, returning when the next compaction should run. Both use UTC to avoid daylight saving time ambiguities.

This time-truncation approach ensures compaction boundaries are aligned to wall-clock intervals (e.g., every hour on the hour) rather than drifting relative to when the daemon started. Aligned boundaries make retention calculations predictable: an hour-old file is always in the previous hourly compaction window, regardless of when the daemon was last restarted.

## Validation

`CompactionLevels.Validate()` enforces:

1. At least one level must exist
2. Level numbers must be sequential and match their slice index
3. No level can be `SnapshotLevel` or higher (9+)
4. Level 0 must have `Interval: 0`
5. All other levels must have a positive interval

The index-must-match-level rule prevents silently mis-ordered level slices. If levels were out of order, `MaxLTX()` lookups and compaction scheduling would silently operate on the wrong tier.

## Navigation Helpers

- `MaxLevel()` — the highest non-snapshot level index
- `IsValidLevel(level)` — true for 0 through MaxLevel and SnapshotLevel
- `PrevLevel(level)` — one level down; from snapshot returns MaxLevel
- `NextLevel(level)` — one level up; from MaxLevel returns SnapshotLevel; from snapshot returns -1

These navigation methods encapsulate the boundary conditions so callers do not need to know the specific integer value of `SnapshotLevel` or handle the MaxLevel→Snapshot transition manually.

## Known Gaps

No validation that intervals are strictly increasing across levels. A configuration where level 2 has a shorter interval than level 1 would pass validation but produce nonsensical compaction behavior.