package ipc

import "encoding/json"

// Command names are constants so callers don't have to recall the exact spelling
// from the spec. Servers also reference these to dispatch incoming requests.
const (
	CmdHello             = "hello"
	CmdStatus            = "status"
	CmdRecentInvocations = "recent_invocations"
	CmdSubscribeHitrate  = "subscribe_hitrate"
	CmdUnsubscribe       = "unsubscribe"
)

// Error codes defined for ProtocolV1. Adding a new code requires a wire-format
// version bump.
const (
	CodeProtocolMismatch = "protocol_mismatch"
	CodeUnknownCommand   = "unknown_command"
	CodeInvalidArgs      = "invalid_args"
	CodeNotImplemented   = "not_implemented"
	CodeInternalError    = "internal_error"
	CodeFrameTooLarge    = "frame_too_large"
	CodeRateLimited      = "rate_limited"
)

// Server name constants — used in HelloOk.Server so the client can distinguish
// which helper it's connected to.
const (
	ServerXcelerateProxy = "xcelerate-proxy"
	ServerCcacheHelper   = "ccache-helper"
)

// Message is the outer envelope every IPC frame uses. Exactly one of Ok /
// Event / Error is set on server-to-client messages; Cmd + Args are set on
// client-to-server requests. See docs/daemon-ipc-protocol.md.
//
// Fields are pointers to RawMessage so absence is distinguishable from
// presence with an empty body, which matters for the golden-file round-trip.
// Callers use the typed Unmarshal* helpers below to access them.
type Message struct {
	V     int              `json:"v"`
	ID    string           `json:"id,omitempty"`
	Cmd   string           `json:"cmd,omitempty"`
	Args  *json.RawMessage `json:"args,omitempty"`
	Ok    *json.RawMessage `json:"ok,omitempty"`
	Event *json.RawMessage `json:"event,omitempty"`
	Error *ErrorPayload    `json:"error,omitempty"`
}

// ErrorPayload is the body of the `error` envelope. Details is free-form
// because error specifics are command-dependent.
type ErrorPayload struct {
	Code    string           `json:"code"`
	Message string           `json:"message,omitempty"`
	Details *json.RawMessage `json:"details,omitempty"`
}

// HelloArgs is the body of `cmd:"hello"` requests sent by the client.
type HelloArgs struct {
	Client          string `json:"client"`
	ClientVersion   string `json:"client_version,omitempty"`
	AcceptProtocols []int  `json:"accept_protocols"`
}

// HelloOk is the body of the server's hello response.
type HelloOk struct {
	Server        string `json:"server"`
	ServerVersion string `json:"server_version"`
	Protocol      int    `json:"protocol"`
}

// HelloMismatchError is the body of the error a server emits when no shared
// protocol version exists. It's a typed view of ErrorPayload.Details so the
// client can react programmatically.
type HelloMismatchError struct {
	Supported []int `json:"supported"`
}

// StatusOk is the body of a status response. Counters are monotonic since the
// helper started. Last invocation fields may be empty before the first
// invocation lands.
type StatusOk struct {
	Alive            bool   `json:"alive"`
	UptimeSec        int64  `json:"uptime_sec"`
	Version          string `json:"version"`
	BuildSHA         string `json:"build_sha,omitempty"`
	Hits             int64  `json:"hits"`
	Misses           int64  `json:"misses"`
	BytesIn          int64  `json:"bytes_in"`
	BytesOut         int64  `json:"bytes_out"`
	LastInvocationID string `json:"last_invocation_id,omitempty"`
	LastInvocationAt string `json:"last_invocation_at,omitempty"`
}

// RecentInvocationsArgs is the body of a recent_invocations request.
type RecentInvocationsArgs struct {
	Limit int `json:"limit,omitempty"`
}

// RecentInvocationsOk is the response. Items are ordered most-recent first.
type RecentInvocationsOk struct {
	Invocations []InvocationSummary `json:"invocations"`
}

// InvocationSummary is one entry in RecentInvocationsOk.Invocations.
type InvocationSummary struct {
	InvocationID string `json:"invocation_id"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at,omitempty"`
	Hits         int64  `json:"hits"`
	Misses       int64  `json:"misses"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
}

// SubscribeHitrateArgs is the body of the subscription request. The server
// clamps interval_ms into [250, 10000].
type SubscribeHitrateArgs struct {
	IntervalMs int `json:"interval_ms,omitempty"`
}

// SubscribeHitrateOk is the initial subscription acknowledgement. The server
// echoes the resolved interval so the client knows the actual cadence after
// clamping.
type SubscribeHitrateOk struct {
	Subscribed bool `json:"subscribed"`
	IntervalMs int  `json:"interval_ms"`
}

// HitrateEvent is the body of one push event on a hitrate subscription.
// Counters are deltas relative to the previous sample for the same
// subscription, not absolute totals.
type HitrateEvent struct {
	Type     string `json:"type"`
	At       string `json:"at"`
	Hits     int64  `json:"hits"`
	Misses   int64  `json:"misses"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

// HitrateEventType is the only legal value of HitrateEvent.Type in v=1.
const HitrateEventType = "hitrate"

// UnsubscribeOk is the body of the server's final reply after a client
// unsubscribes from a stream.
type UnsubscribeOk struct {
	Unsubscribed bool `json:"unsubscribed"`
}
