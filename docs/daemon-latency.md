# Why the supervised proxy is slow, and what fixes it

The launchd-supervised xcelerate proxy serves cache operations far slower than
the same binary forked by the xcodebuild wrapper. This file records what was
measured and what each candidate fix did, so the next person does not repeat
the dead ends.

## The measurement

Reference app: WordPress-iOS (`4ee4b595-430f-4ba6-91c1-3157004c27c3`), pinned
commit `91c8c2d9`, `g2.mac.medium` (4-core / 6GB), warm cache, both arms run in
parallel. Workflows `diag-v368-daemon` and `diag-v368-nodaemon` differ only in
whether the daemon is installed.

Baseline, CLI v3.6.9 (`ProcessType Interactive`, i.e. the supposedly fixed
band):

| | daemon (launchd) | nodaemon (wrapper-forked) |
|---|---|---|
| cache op p50 | **2199 ms** | **11.9 ms** |
| cache op p90 | 3598 ms | 241 ms |
| `sched_p99` under load | 5.2 – 21.0 ms | 229 – 328 µs |
| OS threads (peak) | 123 | 16 |
| open fds (peak) | 1442 | 33 |
| goroutines | 460 – 537 | 95 – 253 |
| wall time | 13.1 min | 10.0 min |

Both arms served an **identical** workload: 12618 GetValue, 3860 hits, 23667
Load, 36285 timed operations. Same binary, same machine class, same commit.

## What that means

`sched_p99` is how long a goroutine sat runnable before it got a thread. The
launchd proxy waits 30-90x longer for CPU. Everything else follows from it:
each operation takes ~185x longer, so by Little's Law far more operations are
in flight at once, each holding a file descriptor and a goroutine, and
goroutines blocked in syscalls make the Go runtime spawn OS threads. The thread
count climbs 9 → 30 → 109 → 118 → 123 and never falls back, which is the
signature of the runtime growing its M pool rather than of extra work.

So threads and fds are symptoms. The cause is CPU scheduling.

Two things this rules out:

- **Pool saturation.** The launchd proxy's kv channel pool was *emptier*
  (0-10 of 16, mostly 0) than the wrapper's (5-12 of 16). It is not queueing on
  channels.
- **The ccache helper.** `daemon install` starts it too, but its log is 216
  bytes of "Helper idle".

## Why the e2e gate does not catch this

`e2e-daemon-cache-macos` fails on any `DeadlineExceeded`. This configuration
produces **none** — it is purely slower. The gate would pass every variant
below. Judge candidates on **per-operation p50**, never on the gate.

`ProcessType Interactive` fixed the timeouts and did not fix the latency. The
earlier "Interactive ≈ 16ms/op" measurement came from that e2e, which runs 620
operations against a small Swift project; WordPress runs 36285 and the gap
returns.

## Candidates

Tracked in the table below; each row links to the commit that tested it.

| # | Candidate | Where tested | Result |
|---|---|---|---|
| D1 | Bound gRPC concurrency (`MaxConcurrentStreams`, `NumStreamWorkers`) | laptop: inconclusive | needs CI |
| B1 | Self-daemonize: double-fork + `setsid`, no launchd | | |
| A2 | `LimitLoadToSessionType` (Aqua / Background) | laptop: loads (Aqua only) | needs CI |
| A1 | `LaunchDaemon` in the system domain | | |
| A3 | `NSAppSleepDisabled=1` | laptop: loads | needs CI |
| A4 | `EnablePressuredExit=false`, `LowPriorityIO=false` | laptop: loads | needs CI |
| C1 | `setpriority(PRIO_DARWIN_PROCESS, 0, 0)` at startup | laptop: verified working | needs CI |

## Test method

A laptop (14-core) and the RDE (6-core) **cannot reproduce the latency gap** —
it needs a small machine under a real compile, and 6-core shows both bands
identical. Local runs are therefore only good for *mechanism* checks: does the
plist load, does the flag apply, do the thread and goroutine counts move. The
verdict on latency comes from CI.

## D1 — bound gRPC concurrency

`grpc.NewServer()` was created with neither `MaxConcurrentStreams` nor
`NumStreamWorkers`, so gRPC spawns one goroutine per stream with no ceiling.
Set to 64 streams and `min(2*NumCPU, 32)` workers.

**Laptop result: inconclusive, and the reason matters.** 400 operations at 200
concurrent against a fake backend delayed 1.5s moved the proxy from 19 to 21
threads — no signal either way:

| | threads | fds | up_p50 |
|---|---|---|---|
| unbounded | 19 | 19 | 25ms |
| bounded | 21 | 19 | 21ms |

Go's netpoller parks goroutines blocked on *network* I/O without holding an OS
thread, so a probe that only moves bytes over gRPC cannot grow the thread pool.
The 123 threads seen on CI must come from blocking *file* syscalls against CAS
blobs, which this probe never touches. Any local test of D1 is therefore
meaningless; it has to be judged on the reference app.

A byproduct: the probe used here (`latency_probe_test.go`) had been deleted as
"temporary" in an earlier PR and was not recoverable from git, so it is
rewritten and kept. `FAKE_BACKEND_DELAY` is new, for simulating a proxy that
cannot keep up.

## A2/A3/A4 — launchd plist knobs

Added behind env vars so one binary can test several variants on CI:
`BITRISE_DAEMON_SESSION_TYPE`, `BITRISE_DAEMON_DISABLE_APP_NAP`,
`BITRISE_DAEMON_NO_PRESSURED_EXIT`.

Laptop mechanism check — does the job still load?

| variant | keys in plist | launchd state |
|---|---|---|
| baseline | — | running |
| A2 `Aqua` | `LimitLoadToSessionType` | running |
| A2 `Background` | `LimitLoadToSessionType` | **not loaded** |
| A3 | `NSAppSleepDisabled` | running |
| A4 | `EnablePressuredExit`, `LowPriorityIO` | running |

`Background` is not loadable in the `gui/<uid>` domain at all, so that half of
A2 is only reachable via a LaunchDaemon in `system/` (A1). Everything else
loads, so all are safe to try on CI — the laptop cannot say whether any of them
changes latency.

## C1 — clear the Darwin background band

`setpriority(PRIO_DARWIN_PROCESS, 0, 0)` at proxy startup, applied to every
thread in the process. Unlike the plist knobs this needs no cooperation from
launchd, so it also covers a job that was placed in the band for reasons the
plist does not express.

**Laptop: verified working**, the one candidate whose mechanism can be proven
here. Forcing the process into the background band and clearing it again:

| step | `getpriority(PRIO_DARWIN_PROCESS)` |
|---|---|
| start | 0 |
| after `PRIO_DARWIN_BG` | 1 |
| after `ClearBackgroundPriority()` | 0 |

Whether the supervised proxy is *in* that band on CI is the open question; if
it is, this is a one-line fix, and if it is not, the call is a harmless no-op
(it returns an error, which is logged at debug and ignored).
