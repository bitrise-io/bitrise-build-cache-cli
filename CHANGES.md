# Branch summary: `add-ccache-invocation-info` vs `main`

## 1. Protocol: `SetInvocationID` now carries both parent and child IDs

The `SetInvocationID` IPC message (opcode `0xB1`) previously sent a single invocation ID. It now carries an explicit parent→child pair, with the child UUID generated on the client side before the message is sent.

- **`internal/ccache/protocol/ccache_ipc.go`**: `WriteSetInvocationID(w, parentID, childID)` writes two length-prefixed strings; `ReadSetInvocationID(r)` returns `(parentID, childID, err)`
- **`internal/ccache/ipc_client.go`**: `SendInvocationID(ctx, socketPath, parentID, childID string)` — signature updated to pass both IDs; `SendStop(ctx, socketPath)` added to send a STOP request and block until the server ACKs
- **`cmd/reactnative/run_cmd.go`**: child UUID is generated in `BuildNotifyCcacheHelperFn` before calling `sendInvocationIDFn`, so both IDs are fully controlled by the caller (useful for e2e testing via `set-invocation-id`)

## 2. `onChildInvocation` and `onShutdown` callbacks in the IPC server

Two callbacks decouple the server core from analytics:

- **`onChildInvocation func(prevInvocationID, parentID, childID string, downloadBytes, uploadBytes int64)`** — fired by `handleConnection` after each successful `SetInvocationID`. Receives the invocation ID that was *active before* the new child started (`prevInvocationID`), along with the parent→child IDs from the message and the bytes accumulated during the previous invocation.
- **`onShutdown func(invocationID string, downloadBytes, uploadBytes int64)`** — fired exactly once (via `sync.Once`) when the server stops, whether by STOP request or idle timeout. Receives the last active invocation ID and the remaining byte counts.

`handleSetInvocationID` in `requestProcessor` now only populates `processResult.InvocationParentID/ChildID`; the callback dispatch and byte reset happen in `handleConnection` after `processRequest` returns, keeping the processor stateless with respect to callbacks.

## 3. Per-session byte tracking and atomic reset

Bytes transferred (downloaded on GET, uploaded on PUT) accumulate in `sessionState` and are reset after each `SetInvocationID` or server shutdown report.

- **`internal/ccache/call_stats.go`**: `sessionState.resetAndGet()` atomically swaps both `downloadBytes` and `uploadBytes` to zero using `atomic.Int64.Swap(0)`, returning the previous values. This eliminates the read-then-reset race window that existed with the previous separate `Store(0)` calls.
- **`internal/ccache/ipc_server.go`**: `handleConnection` calls `resetAndGet()` before invoking `onChildInvocation` or `onShutdown`; `SessionBytes() (int64, int64)` exposes the current counters.

## 4. ccache analytics package (`internal/ccache/analytics/`)

Package for sending ccache telemetry to `https://ccache-analytics.services.bitrise.io`.

**Payload types** (`types.go`):
- `Invocation` — run-level data (invocation ID, command, duration, success/error, CI and host metadata)
- `CcacheStats` — all ~50 fields from `ccache --print-stats --format=json`; `CacheHitRate` is derived as `(direct_hit + preprocessed_hit) / total`
- `CcacheInvocation` — ccache stats snapshot linked to a parent invocation, plus `downloadedBytes` and `uploadedBytes` from the IPC proxy session
- `InvocationRelation` — records a parent→child relationship between two invocations

**Client** (`client.go`, `invocations.go`): `PutInvocation`, `PutCcacheInvocation`, and `PutInvocationRelation` — HTTP PUT helpers with retryable HTTP client and bearer token auth.

## 5. `activeInvocationID` tracking for correct stats attribution

The IPC server tracks which invocation ID is currently active so that accumulated bytes and ccache stats are always reported under the invocation that generated them.

- **`internal/ccache/ipc_server.go`**: `IpcServer` gains `activeInvocationID string` (protected by `sync.Mutex`) and is initialized from `initialInvocationID` passed to `NewServer`.
- On each successful `SetInvocationID`: the previous active ID is captured, `activeInvocationID` is updated to the new child ID, and `onChildInvocation` is called with the *previous* ID. This ensures bytes accumulated during invocation N are reported under invocation N, not N+1.
- The update happens unconditionally even when `onChildInvocation` is nil, so `onShutdown` always receives the correct last-active ID.
- Both shutdown paths (STOP in `handleConnection` and idle timeout in `Run`) read `activeInvocationID` under the mutex before calling `onShutdown`. The mutex is needed in all three sites because `handleConnection` goroutines do not stop immediately when the context is cancelled.

## 6. Storage helper wires analytics callbacks (`cmd/ccache/start_storage_helper.go`)

- On startup, reads `BITRISE_INVOCATION_ID`; if present, registers the storage-helper invocation as a child of that parent via `registerInvocationRelation`.
- `onChildInvocation` calls `registerInvocationRelation` for every new child, then `collectAndSendCcacheStats` (stats + bytes in one call) attributed to the *previous* active invocation ID, then `zeroCcacheStats` to reset counters for the next invocation.
- `onShutdown` calls `collectAndSendCcacheStats` under the last active invocation ID.
- `NewServer` now receives `initialInvocationID` so the server can correctly attribute pre-first-child bytes.

## 7. Stop storage-helper command and ccache stat helpers (`cmd/ccache/stop_storage_helper.go`)

- **`ccache storage-helper stop`**: connects to the IPC socket and sends a STOP request; gracefully no-ops if the server is not running. Accepts `--socket` to override the default socket path from config.
- **`collectAndSendCcacheStats`**: runs `ccache --print-stats --format=json`, parses with `analytics.ParseCcacheStats`, then sends a single `PutCcacheInvocation` combining stats and accumulated byte counts. Errors are logged but do not fail the caller.
- **`zeroCcacheStats`**: runs `ccache -z` to reset ccache's internal counters so each invocation's stats window starts clean. No-ops if ccache is not on PATH.

## 8. New and updated CLI commands (`cmd/ccache/`)

- **`ccache register-child-invocation`** (new): standalone command to register a parent→child invocation relationship directly via the analytics API; accepts `--parent-id` and `--child-id` (both required)
- **`ccache storage-helper set-invocation-id`** (updated): `--id` flag replaced with `--parent-id` and `--child-id` (both required), matching the updated protocol
- **`ccache storage-helper stop`** (new): see section 7

## 9. Analytics hooks for `react-native run` (`cmd/reactnative/`)

- **`ccache_analytics_hooks.go`**: `CcacheAnalyticsHooks` struct removed; replaced by `PostRunFn func(invocationID string, args []string, duration time.Duration, execErr error)`. `BuildPostRunFn` sends only the run-level `Invocation` (command metadata, duration, success/error).
- **`run_cmd.go`**: `RunWithInvocationIDFn` takes a separate `preRunFn func()` and `postRunFn PostRunFn`; `zeroCcacheStats` resets ccache counters before each run.

## 10. Supporting changes

- **`internal/consts/consts.go`**: added `CcacheAnalyticsServiceEndpoint = "https://ccache-analytics.services.bitrise.io"`

## 11. Test coverage

- **`internal/ccache/analytics/invocations_test.go`** (new): `ParseCcacheStats` — field mapping, hit rate computation, zero-total guard, all-hits/all-misses, unknown field tolerance, malformed JSON error
- **`internal/ccache/call_stats_test.go`**: `Test_sessionState_resetAndGet` — returns previous values and zeroes counters; safe on already-zero state
- **`internal/ccache/ipc_server_test.go`**: `NewServer` initialises `activeInvocationID` from its parameter; `IpcServer.SessionBytes()` — returns accumulated values, zero on fresh state, reflects reset after `SetInvocationID`
- **`internal/ccache/ipc_server_integration_test.go`** (new): end-to-end tests against a real Unix socket server:
  - `SetInvocationID` fires `onChildInvocation` with correct `prevID`/`parentID`/`childID` and zero bytes
  - `SetInvocationID` reports correctly accumulated bytes from preceding GET operations
  - Sequential `SetInvocationID` calls chain `prevID` correctly (`initial-id` → `child-1` → `child-2`)
  - `SendStop` fires `onShutdown` synchronously (ACK sent only after callback completes) and reports the initial ID when no `SetInvocationID` was called
  - `onShutdown` receives the last active child ID after a `SetInvocationID`
  - Idle timeout fires `onShutdown` with the initial ID
  - STOP then idle timeout: `onShutdown` called exactly once (`sync.Once` guard)
  - `activeInvocationID` is updated even when `onChildInvocation` is nil; uses a single connection for `SetInvocationID` + STOP to guarantee ordering without sleeps
- **`internal/ccache/request_processor_test.go`**: updated for removed `onChildInvocation` parameter; STOP test expects empty response (written by `handleConnection`, not processor); `SET_INVOCATION_ID` test asserts `result.InvocationParentID/ChildID`
- **`cmd/reactnative/run_cmd_test.go`**: updated for new `RunWithInvocationIDFn` signature; `preRunFn` called before execution
- **`cmd/reactnative/notify_ccache_helper_test.go`**: updated for 3-param `sendInvocationIDFn`
