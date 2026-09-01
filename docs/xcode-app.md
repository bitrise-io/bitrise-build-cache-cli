# Using the cache from Xcode.app

Terminal builds need nothing extra: `xcodebuild` goes through the wrapper that
`activate xcode` puts on your `PATH`, and the wrapper starts the cache proxy on
the first build and reuses it afterwards.

Builds started from Xcode.app do not go through that wrapper, so the proxy has
to be running before you press Build. This page covers that case.

## Why the proxy is not a background service

It used to be installed as a LaunchAgent, which is the obvious answer and the
wrong one. macOS applies CPU and I/O limits per *resource coalition*, and a
launchd job is placed in a coalition of its own. The proxy then competes with
the compiler it exists to serve, and loses: **2314ms per cache operation
against 6.3ms** for the same binary forked by the wrapper, on the same 4-core
machine. Coalition membership is fixed when the process is spawned, so no plist
setting can fix it — see [daemon-latency.md](daemon-latency.md) for the
measurements.

A proxy started from your shell, or from a build phase, joins the build's
coalition and does not have this problem.

## Start it manually

```sh
bitrise-build-cache xcelerate start-proxy
```

It keeps running until you log out or reboot. It does **not** come back on its
own afterwards, so this has to be repeated at the start of a session. The
command is a no-op if a proxy is already serving, so it is safe to run again.

## Start it from a Run Script build phase

To avoid remembering, add a Run Script phase to the scheme so Xcode starts the
proxy itself. This is also the better option on principle: a proxy spawned by
Xcode lands in Xcode's own coalition, which is where you want it.

1. Select your target, open **Build Phases**.
2. **+ → New Run Script Phase**, and drag it to the top of the list, above
   *Compile Sources*.
3. Name it something like `Start Bitrise cache proxy`.
4. Uncheck **Based on dependency analysis**, so it runs on every build.
5. Paste:

```sh
# Start the Bitrise Build Cache proxy if it is not already serving.
# Backgrounded and detached so it outlives this build phase, and never fails
# the build: a cache that cannot start should slow you down, not stop you.
CLI="$HOME/.bitrise-xcelerate/bin/bitrise-build-cache"
[ -x "$CLI" ] || CLI="$(command -v bitrise-build-cache 2>/dev/null)"

if [ -n "$CLI" ] && [ -x "$CLI" ]; then
  "$CLI" xcelerate start-proxy >/dev/null 2>&1 &
  disown 2>/dev/null || true
fi

exit 0
```

The phase adds well under a second to a build once the proxy is up, because
`start-proxy` exits immediately when one is already serving.

### Why the script never fails the build

Every branch ends in `exit 0`. If the CLI is missing, or the proxy cannot
start, the build continues without the cache rather than failing. That is the
same posture the terminal wrapper takes.

## Checking it is working

```sh
bitrise-build-cache doctor
```

or look for the proxy directly:

```sh
pgrep -fl "xcelerate start-proxy"
```

Cache activity for a build is in `~/.local/state/xcelerate/logs/`.
