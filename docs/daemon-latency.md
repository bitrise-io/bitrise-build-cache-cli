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
- **The ccache helper.** Supervision started it too, but its log is 216
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
| D1 | Bound gRPC concurrency (`MaxConcurrentStreams`, `NumStreamWorkers`) | CI, with C1 | 1530ms p50 — 34% better, not a fix |
| B1 | Self-daemonize: double-fork + `setsid`, no launchd | laptop + CI control | **6.3ms p50 — this is the fix** |
| A2 | `LimitLoadToSessionType` (Aqua / Background) | CI | 1809ms p50 — no effect |
| A1 | `LaunchDaemon` in the system domain | | |
| A3 | `NSAppSleepDisabled=1` | CI, with A4 | 1600ms p50 — no effect |
| A4 | `EnablePressuredExit=false`, `LowPriorityIO=false` | CI, with A3 | 1600ms p50 — no effect |
| C1 | `setpriority(PRIO_DARWIN_PROCESS, 0, 0)` at startup | CI, with D1 | folded into D1's 1530ms |

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

Added behind env vars so one binary could test several variants on CI:
`BITRISE_DAEMON_SESSION_TYPE`, `BITRISE_DAEMON_DISABLE_APP_NAP`,
`BITRISE_DAEMON_NO_PRESSURED_EXIT`. Every arm lost to the wrapper, so the knobs
went with the rest of the supervision code; the numbers below are what they
produced.

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

## B1 — self-daemonize, no launchd

The wrapper already spawns the proxy with `Setpgid`, so B1 is less of a change
than it looks. Two things were unknown: whether such a proxy outlives the shell
that started it, and what it costs.

**The cost is already measured.** The `nodaemon` arm *is* a detached shell
child, and it serves at **11.9ms p50** against the launchd proxy's 2199ms. B1
needs no CI latency run; that number is B1's number.

**Persistence, checked on the laptop:** started from a subshell that then
exited, the proxy was reparented to `ppid=1`, stayed running, and still served
(20 ops, 1ms p50).

So a detached proxy satisfies the requirement launchd was chosen for — it
outlives the shell — while keeping the shell's scheduling treatment. What it
does not give is restart-on-crash (`KeepAlive`) or start-at-login, neither of
which CI needs and both of which a small supervisor could provide without
launchd owning the proxy's scheduling.

This makes B1 the leading candidate: the fastest measured configuration, and
the persistence objection does not hold.

## CI verdict

All arms on the reference app, 4-core, same harness, `n=36285` operations each.
Read only the per-invocation `xcelerate-<uuid>.log`: a daemon arm also ships
`xcelerate-proxy.out.log`, which holds the same lines, and counting both
doubles `n` without changing the percentiles.

| arm | p50 | p90 | vs launchd baseline |
|---|---|---|---|
| **nodaemon (wrapper-forked)** | **6.3 ms** | 95.5 ms | **367x faster** |
| launchd baseline | 2314 ms | 3833 ms | — |
| D1 + C1 | 1530 ms | 2810 ms | 34% better |
| A3 + A4 | 1600 ms | 3170 ms | 31% better |
| A2 Aqua | 1809 ms | 3340 ms | 22% better |

None of the launchd-side knobs comes close. The best of them, bounding gRPC
concurrency and clearing the Darwin background band, takes 2314ms to 1530ms —
real, and still **243x** slower than not using launchd at all. A2, A3 and A4
are within noise of each other and of doing nothing.

**The conclusion is that the supervisor is the problem, not its configuration.**
No plist key reaches wrapper latency because the cost is in being a launchd job
at all.

### Harness note

The three variant arms hung in SPM checkout on the first two attempts, 3 for 3
on source-built runs against 0 for 6 on installer runs: `go build` on the VM
interferes with the SPM package checkout that follows. Fixed by running
`dependencies` **before** the CLI install, which also isolates the measurement —
`xcodebuild -resolvePackageDependencies` never needed the cache and was adding
noise to every arm.

## What replaces KeepAlive

`KeepAlive` is the one thing launchd gives that a shell-spawned proxy does not.
Three ways to cover it, measured or read from the code:

**1. The wrapper already does it.** `startProxy` reclaims a wedged proxy before
anything else: if the singleton lock is held but the socket does not answer it
SIGKILLs the holder, waits for a revival, and otherwise spawns its own. So a
crashed proxy is repaired by the next xcodebuild invocation without any
supervisor. What is missing is only repair *during* a build, and a build that
loses its proxy mid-flight degrades to cache misses rather than failing.

**2. A launchd job that only starts the proxy — does not work.** Two laptop
measurements kill it:

| setup | child survives job exit? | child priority |
|---|---|---|
| launchd `Background` job spawns detached child | **no**, killed with the job | — |
| same + `AbandonProcessGroup` | **yes**, reparented to `ppid=1` | **pri 4** |
| shell spawns the same command | yes | **pri 31** |

`AbandonProcessGroup` buys survival and nothing else: the abandoned child still
carries the launchd band. Scheduling treatment is inherited by descendants, so
no arrangement with launchd as an ancestor escapes it. (Note `setsid` does not
exist on macOS; an earlier version of this test silently spawned nothing.)

**3. A supervisor descended from the shell.** The only arrangement that keeps
shell scheduling is one where the shell is the ancestor — a small detached
watcher started at activate time that re-spawns the proxy if it dies. It
inherits the activating shell's treatment and passes it on.

## Why the knobs only got 34%

The CI daemon arm runs `ProcessType Interactive`, which measures **pri 31** —
the same as a shell child. So the proxy is *not* in the Darwin background band
there, C1 had nothing to clear, and the 2314ms → 1530ms improvement is D1
alone.

That leaves the real mechanism, which is **coalition membership** — confirmed
with `taskinfo` as root on an RDE:

| process | RESOURCE coalition |
|---|---|
| launchd agent (`ProcessType Interactive`) | **1967** `io.bitrise.coaltest` — its own |
| the shell | 1980 `com.openssh.sshd...` |
| shell child | **1980** — same as the shell |
| detached grandchild | **1980** — still the same |

A launchd job forms its own coalition, named after its label. A forked child
joins its session leader's, and detaching or reparenting to `ppid=1` does not
move it. Every other policy field is identical between the two: `req role:
TASK_UNSPECIFIED`, `req darwin BG: NO`, no QoS clamp, no App Nap suppression.

macOS applies CPU and I/O resource control **per coalition**. So the proxy is
not merely a low-priority process — it is in a different resource-control group
from the compiler it serves, and when xcodebuild saturates every core from
*its* coalition, the proxy's coalition gets what is left. No plist key changes
coalition membership; it is fixed at `posix_spawn` time.

### Why this looks different from the known launchd-is-slow precedent

GitLab Runner hit a 6x slowdown running as a launchd service (36:42 against
5:48 interactive) and `ProcessType Interactive` **fixed it completely**. Ours
does not, and the difference is instructive: GitLab Runner's *jobs are its own
children*, so they inherit its coalition and Interactive lifts the whole tree.
Our proxy is a service in one coalition serving a CPU-saturating client in
another. Lifting the proxy's own band does nothing about the competition
between two coalitions.

That is why the published advice — "set ProcessType Interactive" — is right for
the common case and insufficient for ours.

### What the ecosystem does

Searched the issue trackers of 18 macOS background-workload projects for
`ProcessType`, `launchd priority`, `launchd slow` and `LaunchAgent
performance`: sccache, bazel, ccache, actions/runner, buildkite/agent,
gitlab-runner, tailscale, syncthing, ollama, lima, colima, restic, rclone,
cloudflared, ZeroTier, mitmproxy, BOINC, xmrig, kopia.

**Two projects hit this and both fixed it with `ProcessType Interactive`:**

- `actions/runner` [#614](https://github.com/actions/runner/pull/614) "MacOS:
  Fix poor performance of process spawned from svc daemon" — `shasum` on a
  10GB file took 17s in a terminal and 24s under the service; the fix saved
  ~30s on a 4-minute workflow.
- `gitlab-runner` [#29089](https://gitlab.com/gitlab-org/gitlab-runner/-/work_items/29089)
  — 36:42 as a service against 5:48 interactive, fixed to 5:48.

Both are the **parent-child** case: the runner spawns the build as its own
child, so it inherits the runner's coalition and lifting the parent's band
lifts everything. Neither is our shape.

**No project in the list reports our shape** — a daemon *serving* a separate
foreground process that saturates the machine. That failure mode appears to be
undocumented, which is consistent with it being invisible to error monitoring:
nothing fails, everything is just slower.

**The closest architectural analogue avoids launchd entirely.** sccache is a
compile cache with a background server, exactly our shape, and its server is
started *by the client on demand*: "The sccache command will spawn a server
process if one is not already running", terminating "after (by default) 10
minutes of inactivity". No launchd, no systemd, no supervision — which is
precisely the model this document recommends, and sccache has no launchd
throttling issues in its tracker.

(Note the Apple docs describe Interactive as priority ~47; measured here it is
31, matching a shell child.)

## What replaces start-at-login

Today: nothing needs replacing, because start-at-login is not currently buying
anything.

`activate xcode` installs shims at `~/.bitrise-xcelerate/bin/{xcodebuild,xcrun}`
and puts that directory on `PATH`. That reaches shells and nothing else — there
is no xcconfig, no build setting, no `defaults` key, nothing Xcode.app reads.
**A GUI Xcode build never goes through the wrapper and never contacts the
proxy.** The epic that introduced the daemon says as much: "Xcode.app
integration → M2 (separate story)", explicitly out of scope.

So the agent starting at login serves only CLI builds, which would get a proxy
lazily on their first `xcodebuild` anyway — one process start, once, against a
367x per-operation penalty for the whole session.

When GUI support does arrive it cannot be solved by a LaunchAgent without
inheriting the same penalty, because descendants inherit the band. The
distinction that matters is not launchd versus not, but *what kind* of launchd
job. Measured with a throwaway `.app` launched via `open`:

| process | pri | ni |
|---|---|---|
| the app bundle itself | 46 (GUI application band, as Finder) | 0 |
| **the app bundle's child** | **31** | 0 |
| shell child, for comparison | 31 | 5 |

So a login item packaged as a real app bundle puts its child on the
application side of the line, where a LaunchAgent cannot.

**But the app bundle does not solve GUI Xcode**, which was the only reason to
want it. Coalitions, measured on an RDE with root `taskinfo`:

| process | pri | RESOURCE coalition |
|---|---|---|
| app bundle | 46 | **1989** `application.io.bitrise.pritest...` — its own |
| the app's child | 31 | **1989** — joins the app |
| shell | 31 | 1983 `com.openssh.sshd...` |
| shell child | 31 | 1983 |

An app bundle forms its *own* application coalition and its children join it.
A proxy spawned from an app-bundle login item therefore lives in our app's
coalition, not in Xcode.app's, and Xcode.app has its own. The cross-coalition
mismatch that causes the slowdown is unchanged — the app bundle only
guarantees the proxy is not in a *throttled* coalition, which is a weaker
claim and is unmeasured for latency.

**The only structurally correct answer for GUI Xcode is for Xcode itself to
spawn the proxy** — a pre-action or "Run Script" build phase would put it in
Xcode's coalition, the same way the wrapper puts it in the CLI build's. Any
arrangement where something else owns the proxy's lifetime reproduces the
problem.

## Decision

On macOS, activation no longer supervises anything: neither `activate xcode`
nor `activate c++` registers a LaunchAgent.

**Nothing is supervised, on any platform.** Each service is started by whoever
needs it — the xcodebuild wrapper for the proxy, `activate c++` for the ccache
helper — which puts it in the build's coalition. The `daemon` subcommands and
the package behind them are gone: keeping a supervision path nobody should take
meant carrying a launchd and a systemd backend to serve it.

Linux is included for consistency and caution rather than measurement.
Coalitions are a macOS concept and no penalty was ever shown for systemd.

A CLI at or below v3.6.9 may have left a launch agent or unit on disk, and it
would keep restarting a supervised service after the upgrade. The wrapper,
activation, `doctor --fix` and `update` retire any they find.

What that changes:

- **Terminal builds**: nothing to do. The wrapper starts the proxy on the first
  build and later builds attach to it, which is what already happened.
- **Xcode.app builds**: the proxy must be running first. `activate` now says so,
  and [xcode-scheme-self-check.md](xcode-scheme-self-check.md) carries a Run Script build phase that starts
  it — spawned by Xcode, so it lands in Xcode's coalition.
- **Crash recovery**: `startProxy` already reclaims a proxy that holds the
  singleton lock but is not serving, so a dead proxy is repaired by the next
  build instead of by `KeepAlive`.
- **`doctor`** still reports a service that is not serving: the check probes the
  socket, so it does not care whether anything is registered. Its remedy is to
  spawn a detached service, for both the proxy and the ccache helper.
- **Reboot**: the proxy does not come back on its own. Terminal builds restart
  it; Xcode.app users need the Run Script or one manual command per session.

### The ccache helper was measured and left alone

The helper has the same shape as the proxy — a background process serving
compilers that saturate the machine — so the expectation was that it would show
the same penalty. It does not.

A/B on a 4-core macOS machine, 240 template-heavy translation units compiled at
full parallelism, 960 helper operations per arm:

| arm | p50 | p90 | p99 | max |
|---|---|---|---|---|
| lazily started | 2.4 ms | 13.5 ms | 126.8 ms | 984.6 ms |
| **supervised** | 3.8 ms | **4.9 ms** | **10.1 ms** | **123.2 ms** |

Both sit in low milliseconds and the supervised arm has the better tails.
Nothing resembling the proxy's 6.3ms against 2314ms. The architecture matched
and the behaviour did not.

The helper is nevertheless unsupervised, as a deliberate choice rather than a
finding: this test ruled out a large effect, not a small one, and running both
services through one lifecycle is simpler than justifying two. If the helper
ever needs a supervisor back, this measurement is the argument for it — and the
deleted code is in this branch's history.

Why the difference is not established. Plausibly the helper does far less work
per operation, and 960 operations over a short compile is a much lighter load
than 36285 over a 4-minute Xcode build — so this rules out a large effect, not
a small one.

D1 (bounded gRPC concurrency) ships alongside on its own merits: 34% faster and
it bounds the thread and fd growth regardless of how the proxy is started.

## Reference numbers

Collected here so the code and the CI scripts can point at one place instead of
carrying measurements in comments that go stale silently.

### Thread and fd growth (why D1 bounds gRPC concurrency)

gRPC serves one goroutine per stream and the compilation plugin opens hundreds
at once, so the runtime grows with service time rather than with request rate —
Little's Law. A CPU-starved proxy held each stream ~185x longer:

| proxy | OS threads | fds |
|---|---|---|
| wrapper-forked | 16 | 33 |
| supervised, CPU-starved | 123 | 1442 |

### Why the CI gate counts timeouts and not percentiles

`e2e-proxy-cache-macos` fails on any `DeadlineExceeded` and ignores the latency
percentiles that `scripts/xcelerate_op_latency.sh` reports. Against the local
fake backend the percentiles do not separate a throttled proxy from a healthy
one — they rank backwards, because loopback latency is dominated by the build:

| arm | p90 | max | timeouts |
|---|---|---|---|
| healthy (`Interactive`) | 483 ms | ~998 ms | **0** |
| throttled (`Background`) | 393 ms | ~998 ms | **38** |

Percentiles only separate the two against the real backend (1931 ms against
5637 ms), which the gate deliberately does not use — no credentials, no
cross-DC variance, no probe blobs in a real workspace. The real backend
produced 128 timeouts where loopback produced 38, so loopback is the weaker
signal but still a decisive one.

Re-run that control before changing the harness: a fake backend that has become
too fast would leave the gate green on a regression.
