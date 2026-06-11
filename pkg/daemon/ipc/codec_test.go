//go:build unit

package ipc

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// golden runs the "decode the file, re-encode, byte-equal" loop that locks
// the wire format. If UPDATE_GOLDEN=1 is set, the encoded bytes are written
// back to the file instead — for the initial seeding and intentional spec
// updates. CI never sets that flag, so unintentional drift fails the test.
func golden(t *testing.T, name string, expected Message) {
	t.Helper()

	path := filepath.Join("testdata", "v1", name)

	// Encode the expected message first — that's what we'll compare against
	// (or write to disk in UPDATE_GOLDEN mode).
	var encoded bytes.Buffer
	enc := NewEncoder(&encoded)
	require.NoError(t, enc.Write(expected))
	require.NoError(t, enc.Flush())

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(path, encoded.Bytes(), 0o644)) //nolint:gosec // test fixtures
		t.Logf("wrote golden %s (%d bytes)", path, encoded.Len())

		return
	}

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "golden file %s missing; run UPDATE_GOLDEN=1 go test to seed", path)

	// Byte-equal check first: catches any drift in field order / escape forms.
	assert.Equal(t, string(raw), encoded.String(), "encoded form differs from golden %s", name)

	// Decode the golden bytes via our Decoder. The decoded Message must round-
	// trip back to the same bytes — proves the decoder doesn't lose detail.
	dec := NewDecoder(bytes.NewReader(raw))
	got, err := dec.Read()
	require.NoError(t, err)

	var redecoded bytes.Buffer
	redecEnc := NewEncoder(&redecoded)
	require.NoError(t, redecEnc.Write(got))
	require.NoError(t, redecEnc.Flush())
	assert.Equal(t, string(raw), redecoded.String(), "decode + re-encode of golden %s diverged", name)
}

func mustArgs(t *testing.T, msg *Message, src any) *Message {
	t.Helper()
	require.NoError(t, MarshalArgs(msg, src))

	return msg
}

func mustOk(t *testing.T, msg *Message, src any) *Message {
	t.Helper()
	require.NoError(t, MarshalOk(msg, src))

	return msg
}

func mustEvent(t *testing.T, msg *Message, src any) *Message {
	t.Helper()
	require.NoError(t, MarshalEvent(msg, src))

	return msg
}

func TestGolden_helloClient(t *testing.T) {
	msg := mustArgs(t, &Message{V: ProtocolV1, Cmd: CmdHello}, HelloArgs{
		Client:          "native-mac",
		ClientVersion:   "0.1.0",
		AcceptProtocols: []int{ProtocolV1},
	})
	golden(t, "hello_client.json", *msg)
}

func TestGolden_helloServer(t *testing.T) {
	msg := mustOk(t, &Message{V: ProtocolV1, Cmd: CmdHello}, HelloOk{
		Server:        ServerXcelerateProxy,
		ServerVersion: "2.8.4",
		Protocol:      ProtocolV1,
	})
	golden(t, "hello_server.json", *msg)
}

func TestGolden_helloMismatchError(t *testing.T) {
	// Mismatch error: typed details body packed via marshalRaw.
	msg := Message{
		V: ProtocolV1,
		Error: &ErrorPayload{
			Code:    CodeProtocolMismatch,
			Message: "client did not accept any protocol the server speaks",
		},
	}
	require.NoError(t, marshalRaw("details", &msg.Error.Details, HelloMismatchError{Supported: []int{ProtocolV1}}))
	golden(t, "hello_mismatch_error.json", msg)
}

func TestGolden_statusRequest(t *testing.T) {
	golden(t, "status_request.json", Message{V: ProtocolV1, ID: "1", Cmd: CmdStatus})
}

func TestGolden_statusResponse(t *testing.T) {
	msg := mustOk(t, &Message{V: ProtocolV1, ID: "1"}, StatusOk{
		Alive:            true,
		UptimeSec:        12345,
		Version:          "2.8.4",
		BuildSHA:         "3fac6b1",
		Hits:             420,
		Misses:           37,
		BytesIn:          8492734,
		BytesOut:         72119,
		LastInvocationID: "0e6f00000000000000000000000000000000000d1e",
		LastInvocationAt: "2026-06-11T15:04:05Z",
	})
	golden(t, "status_response.json", *msg)
}

func TestGolden_recentInvocationsRequest(t *testing.T) {
	msg := mustArgs(t, &Message{V: ProtocolV1, ID: "2", Cmd: CmdRecentInvocations}, RecentInvocationsArgs{Limit: 50})
	golden(t, "recent_invocations_request.json", *msg)
}

func TestGolden_recentInvocationsResponse(t *testing.T) {
	msg := mustOk(t, &Message{V: ProtocolV1, ID: "2"}, RecentInvocationsOk{
		Invocations: []InvocationSummary{
			{
				InvocationID: "0e6f00000000000000000000000000000000000d1e",
				StartedAt:    "2026-06-11T15:03:50Z",
				EndedAt:      "2026-06-11T15:04:05Z",
				Hits:         42,
				Misses:       3,
				BytesIn:      9123,
				BytesOut:     440,
			},
		},
	})
	golden(t, "recent_invocations_response.json", *msg)
}

func TestGolden_subscribeHitrateRequest(t *testing.T) {
	msg := mustArgs(t, &Message{V: ProtocolV1, ID: "3", Cmd: CmdSubscribeHitrate}, SubscribeHitrateArgs{IntervalMs: 1000})
	golden(t, "subscribe_hitrate_request.json", *msg)
}

func TestGolden_subscribeHitrateAck(t *testing.T) {
	msg := mustOk(t, &Message{V: ProtocolV1, ID: "3"}, SubscribeHitrateOk{Subscribed: true, IntervalMs: 1000})
	golden(t, "subscribe_hitrate_ack.json", *msg)
}

func TestGolden_hitrateEvent(t *testing.T) {
	msg := mustEvent(t, &Message{V: ProtocolV1, ID: "3"}, HitrateEvent{
		Type:     HitrateEventType,
		At:       "2026-06-11T15:04:06Z",
		Hits:     3,
		Misses:   0,
		BytesIn:  812,
		BytesOut: 64,
	})
	golden(t, "hitrate_event.json", *msg)
}

func TestGolden_unsubscribeRequest(t *testing.T) {
	golden(t, "unsubscribe_request.json", Message{V: ProtocolV1, ID: "3", Cmd: CmdUnsubscribe})
}

func TestGolden_unsubscribeResponse(t *testing.T) {
	msg := mustOk(t, &Message{V: ProtocolV1, ID: "3"}, UnsubscribeOk{Unsubscribed: true})
	golden(t, "unsubscribe_response.json", *msg)
}

func TestEncoder_rejectsOversizeFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Build a string longer than MaxFrameBytes by stuffing the BuildSHA field.
	huge := strings.Repeat("x", MaxFrameBytes)
	msg := mustOk(t, &Message{V: ProtocolV1, ID: "x"}, StatusOk{BuildSHA: huge})

	err := enc.Write(*msg)
	assert.ErrorIs(t, err, ErrFrameTooLarge)
	assert.Empty(t, buf.Bytes(), "oversize message must not be written even partially")
}

func TestDecoder_rejectsOversizeFrame(t *testing.T) {
	// A single line longer than MaxFrameBytes (no newline) — Decoder must
	// surface ErrFrameTooLarge before allocating arbitrary memory.
	oversize := bytes.Repeat([]byte("a"), MaxFrameBytes+128)
	dec := NewDecoder(bytes.NewReader(oversize))

	_, err := dec.Read()
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}

func TestDecoder_returnsEOFOnCleanClose(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(nil))
	_, err := dec.Read()
	assert.ErrorIs(t, err, io.EOF)
}

func TestDecoder_errorsOnTrailingPartialFrame(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`{"v":1`))
	_, err := dec.Read()
	require.Error(t, err)
	assert.NotErrorIs(t, err, io.EOF, "partial frame at EOF must not be reported as a clean close")
}

func TestDecoder_tolerantOfUnknownFields(t *testing.T) {
	// Forward-compatibility: a server using a future-but-compatible additive
	// field must not break clients running v=1.
	raw := []byte(`{"v":1,"id":"1","ok":{"alive":true,"hits":1,"misses":0,"uptime_sec":1,"version":"x","bytes_in":0,"bytes_out":0,"future_field":42}}` + "\n")
	dec := NewDecoder(bytes.NewReader(raw))

	msg, err := dec.Read()
	require.NoError(t, err)

	var ok StatusOk
	require.NoError(t, UnmarshalOk(msg, &ok))
	assert.True(t, ok.Alive)
	assert.EqualValues(t, 1, ok.Hits)
}
