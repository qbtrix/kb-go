---
{
  "title": "Test Suite for Restore Command Follow Interval Flag",
  "summary": "A focused unit test for the `-follow-interval` flag on the restore command, verifying that the duration value is parsed correctly and defaults to one second. Catches regressions where a flag refactor could change the default or break duration parsing.",
  "concepts": [
    "RestoreCommand",
    "follow-interval",
    "flag.DurationVar",
    "NewRestoreOptions",
    "duration parsing",
    "default value contract",
    "follow mode",
    "polling interval"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "8a735a51ab761364"
  ],
  "backlinks": null,
  "word_count": 296,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file contains a single test function, `TestRestoreCommand_FollowIntervalFlag`, that exercises the `flag.DurationVar` binding for the `-follow-interval` restore flag. It tests the flag in isolation by constructing the flag set directly rather than calling `RestoreCommand.Run()`, avoiding the need for any network or filesystem resources.

## Why This Test Exists

The `-follow-interval` flag controls how frequently the restore command polls for new LTX files in follow mode. The default of 1 second was chosen to balance responsiveness (seeing new transactions quickly) against polling overhead (hammering the storage backend). A regression that changed the default to `0` would cause a tight loop that could exhaust API rate limits or CPU. A regression that changed it to a very large value would make follow mode useless for real-time replication monitoring.

By testing the default separately from user-supplied values, the test documents the expected default as a contract.

## Test Cases

- **Default** — no flag provided, expects `1 * time.Second`. Confirms `litestream.NewRestoreOptions()` initializes `FollowInterval` to the correct value.
- **CustomValue** — `-follow-interval 500ms`, expects `500 * time.Millisecond`. Confirms sub-second intervals are accepted.
- **LongerInterval** — `-follow-interval 5s`, expects `5 * time.Second`. Confirms the flag is not capped.
- **InvalidDuration** — `-follow-interval notaduration`, expects a parse error. Confirms the flag parser rejects non-duration strings rather than silently using a zero or default value.

## Testing Pattern

The test constructs `litestream.NewRestoreOptions()` and binds `FollowInterval` to a `flag.FlagSet` using `DurationVar`, mirroring exactly how `RestoreCommand.Run()` sets up its flags. This pattern ensures the test reflects the actual production code path rather than a simplified approximation.

## Known Gaps

No test for negative durations or zero duration. `flag.DurationVar` accepts `0` and negative values without error, which could cause a zero-sleep or backward-seeking polling loop. The production code does not guard against this.