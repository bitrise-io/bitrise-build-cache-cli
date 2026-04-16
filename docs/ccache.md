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
| `storage-helper stop` | `--socket`, `--invocation-id` (parent) | CollectStats → Stop → RegisterInvocationRelation |
| `storage-helper health-check` | `--socket`, `--timeout` (10s), `--poll-interval` (100ms) | Polls until server ready |
| `storage-helper set-invocation-id` | `--parent-id` (req), `--child-id` (req), `--socket` | Sends parent→child pair to running server via IPC |
| `storage-helper collect-stats` | `--invocation-id` (req), `--parent-id`, `--downloaded-bytes`, `--uploaded-bytes` | Reports ccache stats to analytics, zeros counters |

### Other commands

| Command | Flags | What it does |
|---------|-------|-------------|
| `register-invocation` | `--invocation-id` (req), `--build-tool` (default: "multiplatform") | Registers invocation with analytics backend |
| `register-child-invocation` | `--parent-id` (req), `--child-id` (req), `--build-tool` (default: "ccache") | Registers parent→child relation in analytics |

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

### StorageHelper — analytics methods

Both methods read IDs from internal state — callers do not pass IDs.

```go
func (h *StorageHelper) CollectStats(ctx context.Context, params CollectStatsParams) error
func (h *StorageHelper) RegisterInvocationRelation()
```

`CollectStats` checks if server is listening; if so, overrides `DownloadedBytes`/`UploadedBytes` with live session bytes from IPC before reporting.

```go
type CollectStatsParams struct {
    DownloadedBytes int64 // fallback if helper not reachable
    UploadedBytes   int64 // fallback if helper not reachable
}
```

`RegisterInvocationRelation` no-ops if `parentID` is empty.

### Typical stop-command call sequence

```go
helper, _ := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
    ParentInvocationID: parentID, // withDefaults fills BITRISE_INVOCATION_ID if empty
})
// helper.invocationID = fresh UUID
// helper.parentID     = resolved parent

helper.CollectStats(ctx, ccachepkg.CollectStatsParams{})
helper.Stop(ctx)
helper.RegisterInvocationRelation()
```

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
| `RequestGetSessionStats` | `0xB2` | Get download/upload byte counts |
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

`Client` wraps `multiplatform.Client`. Key function:

```go
func CollectAndZero(ctx, client, invocationID, parentID, dlBytes, ulBytes, logger)
```

1. Runs `ccache --print-stats --format=json`
2. Creates `CcacheInvocation` with parsed stats + byte counts
3. Reports to analytics backend
4. On success: runs `ccache -z` to zero local counters

Errors are logged but non-fatal.
