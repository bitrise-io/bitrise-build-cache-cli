# React Native: Commands, Public API, and ccache Dependency

## Overview

React Native support wraps build/run commands with pre/post hooks that activate multi-platform build caching (Gradle, Xcode, C++/ccache) and report analytics. The C++ side is tightly coupled to the ccache storage helper — see `docs/ccache.md` for the ccache internals.

**Layers:**
- `cmd/reactnative/` — cobra commands
- `pkg/reactnative/` — public API (Activator, Runner)
- `pkg/reactnative/post_run_deps.go` — post-run analytics, ccache stats collection

---

## cmd/reactnative/ — Commands

| Command | Flags | What it does |
|---------|-------|-------------|
| `activate react-native` | `--gradle` (bool, default: true), `--xcode` (bool, default: true), `--cpp` (bool, default: true) | Installs deps, activates all enabled cache backends, writes config |
| `react-native run` | (none — `DisableFlagParsing`, all args forwarded) | Wraps command with ccache pre/post hooks, reports analytics |

---

## pkg/reactnative/ — Public API

### Activator

```go
type ActivatorParams struct {
    GradleEnabled bool
    XcodeEnabled  bool
    CppEnabled    bool
    DebugLogging  bool
    Logger        log.Logger // nil = production logger
}

func NewActivator(params ActivatorParams) *Activator
func (a *Activator) Activate(ctx context.Context) error
```

**Activate flow:**
1. Install CLI binary (if not present) and ccache (if `CppEnabled`)
2. Export install dir to PATH via envman
3. Activate Gradle (if enabled) — writes init script, updates `gradle.properties`
4. Activate Xcode (if enabled) — via `xcelerate.Activate()`
5. Activate C++ (if enabled):
   - `ccachepkg.Activator.Activate()` — exports env vars for ccache
   - Starts storage helper as detached process (survives activation step)
6. Save multiplatform analytics config to disk

**ccache env vars exported by Activate:**
- `CCACHE_BASEDIR` — working directory (normalizes paths in cache keys)
- `CCACHE_NOHASHDIR=true`
- `CCACHE_REMOTE_ONLY=true`
- `CCACHE_REMOTE_STORAGE=<CRS HTTP endpoint>`
- `CMAKE_CXX_COMPILER_LAUNCHER=ccache`
- `CMAKE_C_COMPILER_LAUNCHER=ccache`

### Runner

```go
type RunnerParams struct {
    ExecFn             ExecFunc          // func(environ []string, name string, args ...string) error
    CcacheInvocationID string            // unique ID for C++ invocation; generated if empty
    Logger             log.Logger
    OsProxy            utils.OsProxy
    DecoderFactory     utils.DecoderFactory
}

func NewRunner(params RunnerParams) *Runner
func (r *Runner) Run(args []string, invocationID string, environ []string) error
```

**Run flow:**
1. Strip leading `"--"` from args (cobra `DisableFlagParsing` artifact)
2. If ccache socket available:
   - Start storage helper if not already listening; await ready (5s timeout)
   - Health check
   - `socket.SetInvocationID(invocationID, ccacheInvocationID)` — links RN invocation to ccache session
   - `ccache -z` — zero local ccache stats
3. Execute command with `BITRISE_INVOCATION_ID=invocationID` injected into environ
4. Post-run: `postRunDeps.run(invocationID, args, duration, execErr, ccacheInvocationID)`

---

## ccacheSocket Interface (internal to runner.go)

```go
type ccacheSocket interface {
    IsListening() bool
    Start() error
    AwaitReady() bool
    HealthCheck(ctx context.Context) error
    SetInvocationID(ctx context.Context, parentID, childID string) error
}
```

Implemented by `internal/ccache.Socket` in production. This interface talks to the already-running storage helper via IPC — it does NOT use `pkg/ccache.StorageHelper`. The socket's `SetInvocationID` only sends the IPC request; it does not hold state.

---

## Post-Run Analytics (post_run_deps.go)

**postRunDeps** handles all analytics after command execution. Created by `newPostRunDeps(logger, osProxy, decoderFactory)` which reads the multiplatform analytics config and creates a `ccacheanalytics.Client`.

The analytics client receives its own `clientLogger` created with `log.WithDebugLog(config.DebugLogging)`. This is separate from the runner's logger — `HTTP PUT:` debug lines appear when `activate react-native --debug` was used, without relying on cobra's `IsDebugLogMode` (which cobra never sets for `DisableFlagParsing` commands).

**postRunDeps.run call sequence:**

```
invocationID      = BITRISE_INVOCATION_ID (RN parent)
ccacheInvocationID = runner-generated UUID (C++ child)

1. sendInvocation — reports React Native invocation (duration, command, success/error)
2. collectStats   — reports C++ cache stats (see below)
3. sendRelation   — registers parent→child: invocationID → ccacheInvocationID
```

**collectStats — ccache dependency:**

```go
func (d *postRunDeps) collectStats(ctx context.Context, ccacheInvocationID, parentID string) {
    helper, _ := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
        InvocationID:       ccacheInvocationID,
        ParentInvocationID: parentID,
    })
    helper.CollectStats(ctx, ccachepkg.CollectStatsParams{})
}
```

`CollectStats` queries the running storage helper for session bytes (download/upload) via IPC, reports to analytics backend via `ccacheanalytics.CollectAndZero`, then zeros ccache counters.

**sendRelation** uses `postRunDeps.client.PutInvocationRelation` directly (not via `StorageHelper.RegisterInvocationRelation`) because `postRunDeps` has its own analytics client.

---

## Full Analytics Data Flow

```
Bitrise build
└── BITRISE_INVOCATION_ID = <parent>
    │
    └── react-native run [args]
        │
        ├── [pre-run] socket.SetInvocationID(parent, ccacheInvocationID)
        │       → IPC 0xB1 → server resets session byte counters
        │       → ccache -z (zeros local ccache stats)
        │
        ├── [exec] command runs, C++ files compiled via ccache
        │       → ccache GET/PUT requests proxy through storage helper
        │       → session byte counters accumulate
        │
        └── [post-run]
            ├── sendInvocation(RN invocation, build tool="react-native")
            ├── collectStats(ccacheInvocationID, parent)
            │       → StorageHelper.CollectStats()
            │       → IPC 0xB2 → get session bytes
            │       → ccacheanalytics.CollectAndZero: report + ccache -z
            └── sendRelation(parent → ccacheInvocationID, build tool="ccache")
```

### Command parsing

`parseCommand(args)` extracts a normalized command name for analytics:

| Input | Output |
|-------|--------|
| `["npm", "run", "build"]` | `"npm run build"` |
| `["npx", "react-native", "run-android"]` | `"npx react-native run-android"` |
| `["yarn", "ios"]` | `"yarn ios"` |
| `["expo", "start"]` | `"expo start"` |
| `["fastlane", "beta"]` | `"fastlane beta"` |
| `["./gradlew"]` | `"./gradlew"` |

Known package managers: `yarn`, `npm`, `npx`, `expo`, `pnpm`, `fastlane`.
Known three-token prefixes: `npm run`, `npx react-native`.
