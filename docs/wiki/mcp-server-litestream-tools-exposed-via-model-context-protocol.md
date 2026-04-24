---
{
  "title": "MCP Server: Litestream Tools Exposed via Model Context Protocol",
  "summary": "Implements an HTTP server that exposes litestream operations as MCP (Model Context Protocol) tools, allowing AI assistants and agent frameworks to query database status, trigger restores, inspect LTX files, and manage replication without direct shell access. Each tool shells out to the litestream CLI to perform the actual operation.",
  "concepts": [
    "MCPServer",
    "Model Context Protocol",
    "mcp-go",
    "Streamable HTTP",
    "litestream tools",
    "exec.CommandContext",
    "ToolResultError",
    "litestream_info",
    "litestream_restore",
    "litestream_databases",
    "litestream_ltx",
    "isReplicaURL",
    "httplog",
    "graceful shutdown"
  ],
  "categories": [
    "cli",
    "mcp",
    "litestream",
    "ai-integration"
  ],
  "source_docs": [
    "8cec3283fc0c78fd"
  ],
  "backlinks": null,
  "word_count": 496,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The `MCPServer` wraps the `mcp-go` library to expose litestream's core operations as MCP tools over Streamable HTTP. This allows LLM-based agents — or any MCP-compatible client — to interact with a running litestream instance by making structured HTTP calls rather than parsing shell output directly.

## Server Lifecycle

`NewMCP()` constructs the server, registers all tools, and wires the MCP handler behind an HTTP request logger (`httplog`). `Start()` launches the HTTP listener in a goroutine. `Close()` performs a graceful shutdown with a 10-second timeout — preventing in-flight tool calls from being cut off mid-execution.

The `ReadHeaderTimeout: 30 * time.Second` on the underlying HTTP server guards against slow-read attacks where a client opens a connection but never sends headers, which would otherwise hold a goroutine indefinitely.

## Tools

### `litestream_databases`
Lists all databases and their replicas from the config file. Accepts an optional `config` parameter to override the default config path. Shells out to `litestream databases -config <path>`.

### `litestream_info`
Aggregates version info, database list, and per-database LTX file listings into a single status report. This is the high-level "what is going on" tool. It calls `litestream version`, `litestream databases`, and `litestream ltx` for each discovered database, assembling the results into a human-readable block. Partial failures (e.g., one database's LTX command fails) are included inline rather than aborting the whole report, so the agent can still act on partial information.

### `litestream_restore`
Restores a database from a replica. Accepts `db_path` (or replica URL), optional `output`, `timestamp`, and `txid` parameters. The `isReplicaURL()` helper determines whether to pass `-config` or not — the CLI rejects combining a replica URL argument with a `-config` flag, and omitting this check would produce a confusing error.

### `litestream_ltx`
Lists LTX files for a specific database, showing transaction ID ranges. Useful for understanding what backup coverage is available.

### `litestream_status`
Displays replication status for databases defined in the config. Maps to `litestream status`.

### `litestream_reset`
Clears local LTX state for a database, forcing a fresh snapshot on next sync. Maps to `litestream reset`.

### `litestream_version`
Returns the installed litestream version string.

## Shell-Out Pattern

Every tool handler uses `exec.CommandContext()` to call the `litestream` binary. This design keeps the MCP layer thin and ensures that tool behavior is identical to CLI behavior — no logic is duplicated. `CombinedOutput()` captures both stdout and stderr, so error messages from the CLI are surfaced to the MCP client rather than silently discarded.

Error responses use `mcp.NewToolResultError()` rather than returning a Go error. MCP protocol distinguishes between transport-level errors (returned as Go errors) and tool-level failures (returned as error result content). Using `ToolResultError` means the LLM sees the failure as a tool output it can reason about rather than as a protocol error.

## Known Gaps

The `litestream_restore` tool accepts a `txid` parameter but the current implementation may not validate that the txid format matches what the CLI expects. No explicit test coverage for the MCP server's HTTP behavior (only unit tests for individual tools exist elsewhere).