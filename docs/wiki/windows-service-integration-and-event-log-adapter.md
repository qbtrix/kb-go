---
{
  "title": "Windows Service Integration and Event Log Adapter",
  "summary": "Provides Windows-specific implementations of the platform abstraction functions, enabling litestream to run as a native Windows Service managed by the Service Control Manager. Includes an eventlog writer adapter that bridges Go's slog to the Windows Event Log.",
  "concepts": [
    "Windows Service",
    "Service Control Manager",
    "svc.Handler",
    "eventlog.Log",
    "eventlogWriter",
    "isWindowsService",
    "svc.Run",
    "io.Writer",
    "build tags",
    "slog",
    "windows/svc",
    "compile-time interface assertion"
  ],
  "categories": [
    "cli",
    "platform",
    "litestream",
    "windows"
  ],
  "source_docs": [
    "ccb05b719cf84493"
  ],
  "backlinks": null,
  "word_count": 431,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is compiled only on Windows (via the `//go:build windows` tag). It implements the same three functions as `main_notwindows.go` but with full Windows Service support: detecting SCM execution, running under the SCM, and listening for the Windows interrupt signal.

## Windows Service Detection

`isWindowsService()` delegates to `svc.IsWindowsService()` from `golang.org/x/sys/windows/svc`. This function inspects the process's parent and session context to determine if it was started by the Windows Service Control Manager rather than interactively. The result controls whether the main program redirects logging to the Event Log and enters the SCM control loop.

## Running Under the SCM

`runWindowsService()` performs the full Windows Service startup sequence:

1. **Event Log registration** — calls `eventlog.InstallAsEventCreate()` to register the service name as a log source. This call is allowed to fail silently because the source may already be registered from a previous installation, and there is no place to log the error until the Event Log is opened.
2. **Event Log connection** — opens the log and sets `slog` to write through `eventlogWriter` for the duration of service execution. The deferred reset restores stderr logging after the service stops, so any post-exit cleanup messages are visible.
3. **SCM control loop** — `svc.Run()` blocks until the SCM sends a stop request. Inside `windowsService.Execute()`, the service loads the config, starts replication, and then enters a select loop waiting for SCM change requests.

## windowsService.Execute()

This method implements the `svc.Handler` interface. The state machine:

- Sends `svc.StartPending` to let the SCM know startup is in progress
- Loads config from the default Windows path (`C:\Litestream\litestream.yml`)
- Runs `ReplicateCommand.Run()` inside the service context
- Sends `svc.Running` with `AcceptStop` once replication is active
- On `svc.Stop`: calls `c.Close()`, sends `StopPending`, and returns cleanly
- On `svc.Interrogate`: echoes the current status back, required by the SCM protocol

The two error exit codes (1 for config failure, 2 for replication failure) follow Windows Service error code conventions, allowing tools like `sc query` to distinguish configuration problems from runtime failures.

## eventlogWriter

`eventlogWriter` is a type alias over `eventlog.Log` that implements `io.Writer`. It converts each `Write()` call into an `Info` event with event ID 1. The `var _ io.Writer = (*eventlogWriter)(nil)` compile-time assertion ensures the interface is satisfied — if the `Write` signature ever drifts, the build fails immediately rather than silently dropping log output.

## Signal Handling

On Windows, `signalChan()` listens for `os.Interrupt` only (mapped to Ctrl+C). SIGTERM does not exist on Windows; graceful shutdown from the SCM is handled through the `svc.Stop` command in the Execute loop, not through signals.

## Known Gaps

No TODOs or incomplete implementations.