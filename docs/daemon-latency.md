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
| D1 | Bound gRPC concurrency (`MaxConcurrentStreams`, `NumStreamWorkers`) | | |
| B1 | Self-daemonize: double-fork + `setsid`, no launchd | | |
| A2 | `LimitLoadToSessionType` (Aqua / Background) | | |
| A1 | `LaunchDaemon` in the system domain | | |
| A3 | `NSAppSleepDisabled=1` | | |
| A4 | `EnablePressuredExit=false`, `LowPriorityIO=false` | | |
| C1 | `setpriority(PRIO_DARWIN_PROCESS, 0, 0)` at startup | | |

## Test method

A laptop (14-core) and the RDE (6-core) **cannot reproduce the latency gap** —
it needs a small machine under a real compile, and 6-core shows both bands
identical. Local runs are therefore only good for *mechanism* checks: does the
plist load, does the flag apply, do the thread and goroutine counts move. The
verdict on latency comes from CI.
