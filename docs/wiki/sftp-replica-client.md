---
{
  "title": "SFTP Replica Client",
  "summary": "Implements the Litestream ReplicaClient interface over SFTP, writing LTX files to a remote host via SSH. Manages a persistent SSH+SFTP connection with automatic reconnection on transport errors and supports both password and key-based authentication.",
  "concepts": [
    "SFTP",
    "SSH",
    "ReplicaClient",
    "LTX files",
    "connection pooling",
    "atomic rename",
    "authentication",
    "host key",
    "ConcurrentWrites",
    "DialTimeout",
    "reconnect",
    "init lazy",
    "transport error"
  ],
  "categories": [
    "replication",
    "storage backends",
    "litestream"
  ],
  "source_docs": [
    "ff5764b7557b232a"
  ],
  "backlinks": null,
  "word_count": 392,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`ReplicaClient` in the `sftp` package stores LTX replication data on any host reachable via SSH. It holds a persistent `ssh.Client` and `sftp.Client` rather than reconnecting per-operation, since SFTP connection establishment includes key exchange and authentication which are too expensive for per-file overhead.

## Authentication

The client supports three authentication modes, checked in order:
1. **Key file** — reads a private key from `KeyPath`, defaulting to `~/.ssh/id_rsa`.
2. **Password** — uses `Password` if set.
3. **SSH agent** — falls back to the system SSH agent via the `SSH_AUTH_SOCK` environment variable.

`HostKey` can pin the expected host public key. When unset, host key verification is skipped, which is necessary for automated deployments where the host key is not known in advance but opens the client to MITM on first connection.

## Connection Management

`init` (lowercase) establishes the SSH and SFTP sessions. It is called lazily on the first operation and on reconnect. `resetOnConnError` inspects errors to determine whether they are transport-level failures. When a connection error is detected, both `sshClient` and `sftpClient` are set to nil so the next operation triggers `init` again. The mutex `mu` serializes init calls so two goroutines do not both attempt to reconnect simultaneously.

`DialTimeout` defaults to `DefaultDialTimeout` to prevent indefinitely blocking on an unreachable host.

## LTX File Operations

- `LTXFiles` lists files at `{Path}/L{level}/`, parsing filenames into `litestream.LTXFileInfo` records with TXID ranges extracted from the filename.
- `WriteLTXFile` creates directories if absent, writes the LTX stream to a temp file, then renames it atomically. Atomic rename prevents the listing code from seeing a partially written file.
- `OpenLTXFile` opens the file and, if `offset` or `size` are nonzero, seeks or limits the read via `io.SectionReader`.
- `DeleteLTXFiles` removes the files and then prunes any empty parent directories.

## Concurrent Writes

`ConcurrentWrites` limits how many `WriteLTXFile` calls can proceed simultaneously. SFTP servers often have per-connection concurrency limits, so unbounded parallelism can cause request failures or connection drops.

## URL Registration

`NewReplicaClientFromURL` is called by the main `RegisterReplicaClientFactory` init path. It parses `sftp://user:pass@host/path` into struct fields, including extracting `KeyPath` and `HostKey` from query parameters.

## Known Gaps

- Host key verification is skipped when `HostKey` is empty — there is no TOFU (trust-on-first-use) mechanism to pin the key on first connect.
- `ConcurrentWrites` defaults to 1 (sequential), which may be conservative for servers that support higher concurrency.