package ipc

import "encoding/json"

const (
	CmdHello             = "hello"
	CmdStatus            = "status"
	CmdRecentInvocations = "recent_invocations"
	CmdSubscribeHitrate  = "subscribe_hitrate"
	CmdUnsubscribe       = "unsubscribe"
)

// Adding a new error code requires a wire-format version bump.
const (
	CodeProtocolMismatch = "protocol_mismatch"
	CodeUnknownCommand   = "unknown_command"
	CodeInvalidArgs      = "invalid_args"
	CodeNotImplemented   = "not_implemented"
	CodeInternalError    = "internal_error"
	CodeFrameTooLarge    = "frame_too_large"
	CodeRateLimited      = "rate_limited"
)

const (
	ServerXcelerateProxy = "xcelerate-proxy"
	ServerCcacheHelper   = "ccache-helper"
)

// Message.Ok / Event / Error are pointers so absence is distinguishable from an empty body in the golden round-trip.
type Message struct {
	V     int              `json:"v"`
	ID    string           `json:"id,omitempty"`
	Cmd   string           `json:"cmd,omitempty"`
	Args  *json.RawMessage `json:"args,omitempty"`
	Ok    *json.RawMessage `json:"ok,omitempty"`
	Event *json.RawMessage `json:"event,omitempty"`
	Error *ErrorPayload    `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code    string           `json:"code"`
	Message string           `json:"message,omitempty"`
	Details *json.RawMessage `json:"details,omitempty"`
}

type HelloArgs struct {
	Client          string `json:"client"`
	ClientVersion   string `json:"client_version,omitempty"`
	AcceptProtocols []int  `json:"accept_protocols"`
}

type HelloOk struct {
	Server        string `json:"server"`
	ServerVersion string `json:"server_version"`
	Protocol      int    `json:"protocol"`
}

type HelloMismatchError struct {
	Supported []int `json:"supported"`
}

// StatusOk counters are monotonic since the helper started.
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

type RecentInvocationsArgs struct {
	Limit int `json:"limit,omitempty"`
}

// RecentInvocationsOk items are most-recent first.
type RecentInvocationsOk struct {
	Invocations []InvocationSummary `json:"invocations"`
}

type InvocationSummary struct {
	InvocationID string `json:"invocation_id"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at,omitempty"`
	Hits         int64  `json:"hits"`
	Misses       int64  `json:"misses"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
}

// SubscribeHitrateArgs.IntervalMs is clamped server-side to [250, 10000].
type SubscribeHitrateArgs struct {
	IntervalMs int `json:"interval_ms,omitempty"`
}

// SubscribeHitrateOk.IntervalMs echoes the resolved (clamped) interval.
type SubscribeHitrateOk struct {
	Subscribed bool `json:"subscribed"`
	IntervalMs int  `json:"interval_ms"`
}

// HitrateEvent counters are deltas relative to the previous sample, not absolute totals.
type HitrateEvent struct {
	Type     string `json:"type"`
	At       string `json:"at"`
	Hits     int64  `json:"hits"`
	Misses   int64  `json:"misses"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

const HitrateEventType = "hitrate"

type UnsubscribeOk struct {
	Unsubscribed bool `json:"unsubscribed"`
}
