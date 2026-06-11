# Bitrise Build Cache — daemon IPC protocol (v1)

This document is the **frozen wire contract** that the Bitrise Build Cache
daemon services expose for status, metrics, and recent-invocation queries.
The future Native Mac app and any other out-of-process consumer build
against this contract; the contract is versioned so we can ship breaking
changes safely.

Locking the protocol now (ACI-5033 / M1) avoids a refactor when the Native
Mac milestone resumes. The implementation in the helper services lands later
— the contract is the authoritative thing in this PR. A golden-file
compatibility test in `pkg/daemon/ipc/` keeps the wire format from drifting.

---

## Transport

Each supervised service (currently `xcelerate-proxy` and `ccache-helper`)
listens on its own per-user unix domain socket. The location is:

| OS    | Path                                                                  |
| ----- | --------------------------------------------------------------------- |
| macOS | `~/Library/Caches/bitrise-build-cache/ctrl/<service>.sock`            |
| Linux | `$XDG_RUNTIME_DIR/bitrise-build-cache/ctrl/<service>.sock` (with fallback to `~/.local/state/bitrise-build-cache/ctrl/<service>.sock` when `XDG_RUNTIME_DIR` is unset) |

`<service>` is the short identifier defined by the daemon supervisor
(`xcelerate-proxy`, `ccache-helper`). The socket is created with permission
`0600` so only the owning user can connect.

**Why one socket per service, not an aggregator?** Aggregation belongs to
the UI layer, not the daemon. Keeping the wire surface aligned with the
supervisor unit boundary means each helper owns its own contract and can be
versioned + restarted independently.

---

## Framing

Newline-delimited JSON, UTF-8, one message per line. No length prefix —
servers and clients MUST treat `\n` (`0x0A`) as the message terminator and
MUST reject embedded `\n` inside the message body (JSON `\n` escape is the
only legal form).

Each message MUST be ≤ 64 KiB. Implementations close the connection if a
line exceeds that bound before a newline is seen. This caps memory use on
hostile / runaway peers.

---

## Message envelope

Every message — request, response, event, and error — uses the same outer
shape:

```json
{
  "v": 1,
  "id": "<correlation id, optional>",
  "cmd": "<command name, optional>",
  "args": { /* command-specific, optional */ },
  "ok":   { /* response payload, optional */ },
  "event":{ /* server-pushed event, optional */ },
  "error":{ /* error detail, optional */ }
}
```

- `v` (int, **required**) — wire-format version. This protocol is `v=1`.
  Future breaking changes bump this number.
- `id` (**JSON string**, optional) — correlation identifier echoed by the
  server in every response / event for that request. Free-form (clients
  typically pick monotonic counters formatted as strings, or UUIDs).
  Omitted on server-initiated messages. **MUST** be encoded as a JSON
  string — never as a number. Swift's `JSONEncoder` defaults `Int` to
  number, which breaks Go's `string` decode; clients in Swift must
  explicitly stringify the id before encoding.
- `cmd` (string) — command name on requests, control verb on
  mid-stream client messages. Absent on response / event / error messages.
- `args` (object) — command parameters. Shape is command-specific.
- `ok` (object) — success response payload.
- `event` (object) — server-pushed event payload (only on subscription
  streams).
- `error` (object) — error payload. Mutually exclusive with `ok` /
  `event`.

Servers MUST reject messages with `v` they don't speak (see Handshake
below) by replying with an `error` envelope of code `protocol_mismatch`,
then closing the connection.

Servers MUST tolerate unknown top-level keys for forward compatibility (so
that adding a new optional field doesn't break older readers). v=1 readers
MAY drop unknown top-level keys on re-encode — round-trip preservation of
forward-compat fields is NOT required at v=1. A future v=2 may strengthen
this if a re-encode use case emerges.

**String escape:** payload bodies serialise via Go's `encoding/json`, which
HTML-safe-escapes `<`, `>`, and `&` to the JSON `<`, `>`,
`&` forms by default. Non-Go consumers MUST accept either the
escaped or raw forms when decoding (JSON itself allows both), and MUST
NOT rely on which form appears on the wire. Non-ASCII codepoints pass
through as raw UTF-8. The golden test
`TestGolden_errorWithSpecialChars` locks the Go-produced form for the
avoidance of doubt.

---

## Handshake

Immediately after `accept()`, the **client** sends a hello as its first
message:

```json
{
  "v": 1,
  "cmd": "hello",
  "args": {
    "client":          "native-mac",
    "client_version":  "0.1.0",
    "accept_protocols": [1]
  }
}
```

Where:

- `client` (string, required) — short identifier for the connecting client
  ("native-mac", "cli", "test-harness", etc.). Servers log this for
  diagnostics.
- `client_version` (string, optional) — semver of the connecting client.
- `accept_protocols` (int[], required) — protocol versions the client can
  speak. The server picks the highest mutually-supported version and
  echoes it in its hello reply.

The **server** replies:

```json
{
  "v": 1,
  "cmd": "hello",
  "ok": {
    "server":         "xcelerate-proxy",
    "server_version": "2.8.4",
    "protocol":        1
  }
}
```

- `server` (string) — the service identifier this socket belongs to.
- `server_version` (string) — semver of the running helper binary.
- `protocol` (int) — the protocol version the server picked from the
  client's `accept_protocols`. Both peers operate under this version for
  the remainder of the connection.

If no protocol overlap exists, the server replies with the error envelope
and closes:

```json
{
  "v": 1,
  "error": {
    "code":      "protocol_mismatch",
    "message":   "client did not accept any protocol the server speaks",
    "supported": [1]
  }
}
```

---

## Commands (request → response)

All commands listed are part of `v=1`. New commands added in `v=1` MUST
keep their existing fields stable; new optional fields MAY be added.
Removing or renaming a field requires bumping `v`.

### `status`

Returns a snapshot of the helper's runtime state.

Request:

```json
{ "v": 1, "id": "1", "cmd": "status" }
```

Response:

```json
{
  "v": 1,
  "id": "1",
  "ok": {
    "alive":       true,
    "uptime_sec":  12345,
    "version":     "2.8.4",
    "build_sha":   "3fac6b1",
    "hits":        420,
    "misses":      37,
    "bytes_in":    8492734,
    "bytes_out":   72119,
    "last_invocation_id": "0e6f...d1e",
    "last_invocation_at": "2026-06-11T15:04:05Z"
  }
}
```

All counters are monotonic since helper start. `last_invocation_*` may be
omitted before the first invocation lands.

### `recent_invocations`

Returns the last N invocations the helper served.

Request:

```json
{ "v": 1, "id": "2", "cmd": "recent_invocations", "args": { "limit": 50 } }
```

- `limit` (int, optional, default 25, max 200) — number of items.

Response:

```json
{
  "v": 1,
  "id": "2",
  "ok": {
    "invocations": [
      {
        "invocation_id": "0e6f...d1e",
        "started_at":    "2026-06-11T15:03:50Z",
        "ended_at":      "2026-06-11T15:04:05Z",
        "hits":          42,
        "misses":        3,
        "bytes_in":      9123,
        "bytes_out":     440
      }
    ]
  }
}
```

Order: most recent first. `ended_at` may be omitted if the invocation is
still in flight.

### `subscribe_hitrate`

Server-pushed stream of hit-rate samples. After acknowledging the
subscription, the server emits one `event` per sample interval until the
client sends `unsubscribe` (referencing the same `id`) or closes the
connection.

Request:

```json
{ "v": 1, "id": "3", "cmd": "subscribe_hitrate", "args": { "interval_ms": 1000 } }
```

- `interval_ms` (int, optional, default 1000, min 250, max 10000) —
  sample cadence.

Initial response (acknowledgement):

```json
{ "v": 1, "id": "3", "ok": { "subscribed": true, "interval_ms": 1000 } }
```

**Ack vs event invariant (MUST).** The subscription acknowledgement and the
final unsubscribe reply MUST carry `ok` only (no `event`). Every stream
sample MUST carry `event` only (no `ok`). Clients dispatch by inspecting
which field is present, not by `id` alone — `id` correlates the whole
subscription, not individual frames within it. Servers that violate this
break clients that can't disambiguate "subscription confirmed" from
"first sample". The golden test `TestSubscribeHitrate_ackVsEventInvariant`
asserts both directions.

Subsequent events (delta-since-last-sample, not absolute counters):

```json
{
  "v": 1,
  "id": "3",
  "event": {
    "type":   "hitrate",
    "at":     "2026-06-11T15:04:06Z",
    "hits":   3,
    "misses": 0,
    "bytes_in":  812,
    "bytes_out": 64
  }
}
```

Unsubscribe (client → server):

```json
{ "v": 1, "id": "3", "cmd": "unsubscribe" }
```

Server final reply:

```json
{ "v": 1, "id": "3", "ok": { "unsubscribed": true } }
```

After this, the server SHOULD stop emitting events for `id=3`. The
connection remains open for other requests.

---

## Errors

```json
{
  "v": 1,
  "id": "1",
  "error": {
    "code":    "invalid_args",
    "message": "limit must be between 1 and 200, got 9001",
    "details": { /* optional command-specific details */ }
  }
}
```

Defined `code` values for v=1:

| Code                 | Meaning                                                        |
| -------------------- | -------------------------------------------------------------- |
| `protocol_mismatch`  | Handshake failed; no shared protocol version.                  |
| `unknown_command`    | `cmd` is not recognised on this protocol version.              |
| `invalid_args`       | `args` failed validation.                                      |
| `not_implemented`    | Command is reserved but not implemented in this server build.  |
| `internal_error`     | Catch-all server failure. `details.cause` may carry detail.    |
| `frame_too_large`    | Inbound message exceeded the 64 KiB frame cap.                 |
| `rate_limited`       | Reserved for future use; not emitted by v=1 servers.           |

Servers MUST NOT introduce new codes inside `v=1`. New codes require a
`v` bump.

---

## What's intentionally out of scope

- **`up` / `down` control**. The OS supervisor (launchd / systemd) is
  already the authoritative start/stop interface, and the CLI surfaces
  `daemon up` / `daemon down` for human use (ACI-5032). Adding parallel
  control verbs over IPC would create two truths and a race window. A
  future v=2 may add a graceful-shutdown verb if the UI requirements
  warrant it.

- **Authentication / authorization**. Sockets live under the user's home
  / runtime dir with mode `0600`; the OS permission check is the only
  trust boundary. Cross-user access is not supported.

- **Async push notifications outside subscriptions**. v=1 has no
  unsolicited server messages (other than subscription events tied to a
  client `id`). Any future "the daemon needs attention" push goes
  through `v=2`.

---

## Versioning policy

- `v` (top-level int) is the **wire-format version**. Bumped on changes
  that older parsers can't safely ignore: removed fields, renamed
  fields, changed value semantics, message shape changes, new required
  fields.

- Within a `v`, the contract is **additive only**: new commands, new
  optional fields, new error codes only after a `v` bump.

- Servers SHOULD support the current `v` and the previous one in
  parallel during a transition window. The handshake picks the highest
  version both peers agree on.

---

## Compatibility test

`pkg/daemon/ipc/codec_test.go` reads canonical byte sequences from
`testdata/v1/*.json`, decodes them into the Go structs, re-encodes, and
asserts the bytes round-trip exactly. Any silent struct change that
breaks the wire format fails the test loudly.

Update the testdata files (and bump `v`) deliberately when changing the
wire contract.
