# Local invocation log

Shared on-disk log of every build-tool invocation the user runs locally — read by `doctor`, `status`, and (future) rebuild tooling, written by the CLI today and by the gradle plugin / RN wrappers in follow-up patches.

## Path layout

```
~/.local/state/bitrise-build-cache/invocations/
  2026-06-23.ndjson
  2026-06-24.ndjson
  2026-06-25.ndjson   <- today, append-only
```

One file per UTC date. Files older than 30 days are deleted by the next `Append` from the canonical Go writer (opportunistic sweep gated by a `.last-sweep` marker file, runs at most once every 24h per process).

## Schema

One JSON object per line. UTF-8, LF-terminated. Each line must be ≤ 4 KiB so a single `O_APPEND` write stays atomic across concurrent writers. POSIX does not formally guarantee regular-file `O_APPEND` atomicity, but Linux and macOS deliver it in practice for writes ≤ PIPE_BUF — sticking to that bound is the simplest portable rule.

| Field | Type | Required | Notes |
|---|---|---|---|
| `invocation_id` | string | yes | Matches the BE record where one exists. |
| `command` | string | yes | The cobra subcommand + key args (free-form, human-readable). |
| `tool` | string | yes | `xcode`, `gradle`, `bazel`, `ccache`, `rn`. |
| `tool_version` | string | no | Xcode version / Gradle version / etc. Omit if unknown. |
| `cli_version` | string | yes | The bitrise-build-cache CLI semver that produced the record. |
| `started_at` | RFC3339 timestamp | yes | When the tool invocation started. |
| `finished_at` | RFC3339 timestamp | no | Equal to `started_at + duration`. The canonical Go writer emits one record per completed invocation, so this is always set today; future streaming writers may omit it for in-flight records. |
| `exit_code` | int | yes | Real exit code where available; 0 = success. |
| `source` | string | yes | `local` or `ci`. CI iff the writer ran under a known CI provider. |

Unknown fields are ignored by readers — additive schema changes are backward compatible.

## Concurrency contract

Writers append from multiple processes:

* CLI subcommands (xcodebuild wrapper, ccache helper, …).
* Gradle plugin (separate JVM process).
* RN wrappers.

Each writer must:

1. `mkdir -p` the invocations dir (`0755`).
2. Open the daily file with `O_APPEND | O_CREATE | O_WRONLY` (`0644`).
3. Encode the record as **a single line** (JSON object + LF).
4. Write **the full line in one syscall**.
5. Close the file.

The 4 KiB cap on the line size matches PIPE_BUF on Linux / macOS, which is the threshold below which `write(2)` on a regular file under `O_APPEND` is observed to be atomic in practice. Records over the cap should be shrunk by truncating their `command` field (the canonical Go writer does this with a `… [truncated]` suffix) before falling back to outright rejection.

## Retention

Daily files older than 30 days are removed by `invocations.Sweep`. The canonical Go writer calls it from `Append` after every successful write, gated by a `.last-sweep` marker file (modtime within the last 24h ⇒ skip) so the cost is amortised. Non-Go writers do not need to implement Sweep themselves — any Go-writer invocation in the same workspace will catch up the cleanup.

## Reference implementation

* Go writer: [`internal/invocations`](../internal/invocations/invocations.go) — `Writer.Append`, `Reader.Recent`, `Sweep`.
* Path resolution: [`internal/paths`](../internal/paths/paths.go) — `Paths.InvocationsDir`, `Paths.InvocationsFile`.

## Kotlin / Java writer sketch (gradle plugin)

```kotlin
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardOpenOption
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

data class InvocationRecord(
    val invocationId: String,
    val command: String,
    val tool: String,
    val cliVersion: String,
    val startedAt: Instant,
    val finishedAt: Instant?,
    val exitCode: Int,
    val source: String,
    val toolVersion: String? = null,
)

private val dayFormat = DateTimeFormatter.ofPattern("yyyy-MM-dd").withZone(ZoneOffset.UTC)

fun appendInvocation(record: InvocationRecord) {
    val home = System.getProperty("user.home")
    val dir = File("$home/.local/state/bitrise-build-cache/invocations")
    dir.mkdirs()

    val day = dayFormat.format(record.startedAt)
    val file = File(dir, "$day.ndjson")

    val line = recordToJson(record) + "\n"
    require(line.toByteArray(Charsets.UTF_8).size <= 4096) {
        "invocation record exceeds atomic-append limit (4096 bytes)"
    }

    Files.newOutputStream(file.toPath(), StandardOpenOption.CREATE, StandardOpenOption.APPEND).use {
        it.write(line.toByteArray(Charsets.UTF_8))
    }
}
```

The JVM serialiser is up to the caller — Jackson, kotlinx.serialization, or hand-rolled — as long as the JSON object stays one line ≤ 4 KiB.
