# ccache: Commands, Public API, and Internals

## Overview

ccache integration enables C++ build caching via a local IPC proxy server. The proxy intercepts ccache secondary-storage requests and forwards them to the Bitrise Build Cache backend over gRPC.

**Three layers:**
- `cmd/ccache/` — cobra commands (thin CLI wrappers)
- `pkg/ccache/` — public API (importable by external Go packages, e.g. steps)
- `internal/ccache/` — IPC server, protocol, session state

---

## cmd/ccache/ — Commands

### `storage-helper` subcommands

| Command | Flags | What it does |
|---------|-------|-------------|
| `storage-helper start` | `--invocation-id` (default: new UUID) | Starts IPC proxy; blocks until ctx done or idle timeout |
| `storage-helper stop` | `--socket`, `--invocation-id` | Shuts down the running storage helper process |
| `storage-helper health-check` | `--socket`, `--timeout` (10s), `--poll-interval` (100ms) | Polls until server ready |
| `storage-helper set-invocation-id` | `--parent-id` (req), `--child-id` (req), `--socket` | Sends parent→child pair to running server via IPC |
| `storage-helper collect-stats` | `--invocation-id`, `--parent-id` | Reports ccache stats to analytics, zeros counters |

---

## Top-level analytics commands

These commands are build-tool-agnostic and registered on the root command (not under `ccache`). Used by Kotlin/Gradle code to register invocation relationships from outside the CLI wrapper.

| Command | Flags | What it does |
|---------|-------|-------------|
| `register-invocation` | `--invocation-id` (req), `--build-tool` (default: "multiplatform") | Registers invocation with analytics backend |
| `register-child-invocation` | `--parent-id` (req), `--child-id` (req), `--build-tool` (default: "ccache") | Registers parent→child relation in analytics |

Both commands read auth credentials from the multiplatform analytics config (`~/.bitrise/analytics/multiplatform/config.json`), written by `activate react-native` or `activate c++`.

---

## pkg/ccache/ — Public API

### StorageHelperParams

```go
type StorageHelperParams struct {
    InvocationID       string            // child ID; withDefaults generates UUID if empty
    ParentInvocationID string            // parent ID; withDefaults falls back to BITRISE_INVOCATION_ID env if empty
    DebugLogging       bool              // enables verbose output for Start()
    Envs               map[string]string // auth/config env vars; nil = current process env
    SocketPath         string            // IPC socket override; empty = read from config file
}
```

`withDefaults` ordering: sets `Envs` first, then `InvocationID` (generate UUID if empty), then `ParentInvocationID` (env fallback if empty). Order matters — parent fallback reads from `Envs`.

### StorageHelper — lifecycle methods

```go
func NewStorageHelper(params StorageHelperParams) (*StorageHelper, error)
func (h *StorageHelper) Start(ctx context.Context) error
func (h *StorageHelper) Stop(ctx context.Context) error
func (h *StorageHelper) HealthCheck(ctx context.Context, params HealthCheckParams) error
```

`Start` blocks. `Stop` is a no-op if the server is not listening.

### StorageHelper — invocation ID state

`StorageHelper` tracks `invocationID` and `parentID` as live fields (protected by `sync.RWMutex`). Initialized from `StorageHelperParams` at construction. Updated by `SetInvocationID` on success.

```go
func (h *StorageHelper) SetInvocationID(ctx context.Context, parentID, childID string) error
```

Sends IPC request, then updates internal state. State only updated on success.

### StorageHelper — analytics method

```go
func (h *StorageHelper) CollectAndSendStats(ctx context.Context, invocationIDOverride, parentIDOverride string)
```

Queries the running storage helper for session byte counts via IPC (`0xB2`), parses `ccache --print-stats`, and if there was any activity (cache hits/misses or transfer bytes > 0):
- Registers the parent→child invocation relation
- Reports the ccache invocation to the analytics backend
- Zeros ccache counters (`ccache -z`)

Pass empty strings for both overrides to use the IDs from internal state (set at construction or via `SetInvocationID`). Override params are for callers that know the correct IDs explicitly (e.g. `collect-stats` CLI command with `--invocation-id`/`--parent-id` flags).

If ccache binary is missing, proceeds with empty stats but still reports transfer bytes. All failures are logged as warnings — the method never propagates errors.

---

## internal/ccache/ — IPC Server & Protocol

### IpcServer

Runs a Unix socket listener. Spawned by `Start()` via `iccache.NewServer(...)`.

```go
func NewServer(config, metadata, client, logger, loggerFactory, initialInvocationID) (*IpcServer, error)
func (s *IpcServer) Run(ctx context.Context) error
func (s *IpcServer) SessionBytes() (downloaded, uploaded int64)
```

Fields relevant to understanding behavior:
- `activeInvocationID` — current session's child ID; updated on `SetInvocationID`
- `sessionState` — atomic byte counters; reset on `SetInvocationID` with a new child ID
- `loggerFactory LoggerFactory` — `func(invocationID string) (log.Logger, error)` — creates per-invocation file loggers

### IPC Protocol request types

| Constant | Byte | Description |
|----------|------|-------------|
| `RequestGet` | `0x00` | Download cache entry |
| `RequestPut` | `0x01` | Upload cache entry |
| `RequestRemove` | `0x02` | Remove cache entry |
| `RequestStop` | `0x03` | Shutdown server |
| `RequestSetInvocationID` | `0xB1` | Set parent→child invocation IDs |
| `RequestGetSessionStats` | `0xB2` | Get session stats: byte counts + active invocation IDs |
| `RequestHealthCheck` | `0xB3` | Health check |

Response bytes: `0x00` OK, `0x01` noop/miss, `0x02` error (followed by message string).

### SetInvocationID reset pattern

When `0xB1` arrives with a new `childID`:
1. `requestProcessor.handleSetInvocationID` reads parent+child, switches KV client session via `client.ChangeSession(childID, ...)`
2. Returns `processResult{InvocationChildID: childID, ...}`
3. `IpcServer.handleSetInvocationIDResult` checks: if childID ≠ `activeInvocationID`, calls `sessionState.resetAndGet()` (zeros byte counters) and updates `activeInvocationID`

Effect: each new invocation starts with clean byte counters, per-invocation log file.

### sessionState

Atomic counters accumulated per session:
```go
type sessionState struct {
    downloadBytes atomic.Int64
    uploadBytes   atomic.Int64
}
func (s *sessionState) resetAndGet() (int64, int64)      // swap to zero, return old values
func (s *sessionState) updateWithResult(r processResult) // accumulate bytes
```

### Key log lines asserted by CI

The `cache-ccache-test` workflow asserts on these exact strings in `~/.local/state/ccache/logs/ccache-<id>.log`. Do not change without updating the workflow.

| Log line pattern | Level | Location | Meaning |
|-----------------|-------|----------|---------|
| `Server listening on` | Info | `ipc_server.go:Run` | Server ready |
| `[SetInvocationID] parent=` | Debug | `request_processor.go:handleSetInvocationID` | Invocation switch (requires `--debug`) |
| `Server shutting down` | Info | `ipc_server.go:Run` | Clean shutdown |
| `[Get - <hex>] OK took` | Debug | `request_processor.go:logCallStats` | Remote cache hit (requires `--debug`) |

### internal/ccache/analytics/

`Client` wraps `multiplatform.Client`. Key functions:

```go
func ParseCcacheStats(data []byte) (CcacheStats, error)
func NewCcacheInvocation(invocationID, parentInvocationID string, invocationDate time.Time, stats CcacheStats, downloadedBytes, uploadedBytes int64) *CcacheInvocation
func (c *Client) PutCcacheInvocation(inv CcacheInvocation) error
```

`CcacheStats.HasActivity()` returns true when `DirectCacheHit + PreprocessedCacheHit + CacheMiss > 0`. Used by `StorageHelper.CollectAndSendStats` to gate analytics.
