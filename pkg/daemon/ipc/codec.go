package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrFrameTooLarge is returned by Decoder.Read when a single line exceeds
// MaxFrameBytes before a newline is seen. Servers MUST close the connection
// after surfacing this to their callers; the protocol has no recovery mode.
var ErrFrameTooLarge = errors.New("ipc: frame exceeds MaxFrameBytes")

// Encoder writes IPC messages to w as newline-delimited JSON. Safe for
// concurrent use by a single goroutine; for parallel writers, callers must
// serialise externally (a unix socket is one writer per connection in
// practice).
type Encoder struct {
	w *bufio.Writer
}

// NewEncoder returns an Encoder writing to w. Internally buffered; callers
// must call Flush (or the underlying connection's Close) to ensure the bytes
// hit the wire.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriterSize(w, 4096)}
}

// Write encodes msg and appends '\n'. The resulting bytes are validated
// against MaxFrameBytes; oversize messages return ErrFrameTooLarge and are
// NOT written.
//
// Newline-as-terminator is safe because JSON forbids raw control characters
// (0x00–0x1F) inside string values per RFC 8259 §7 — a literal LF inside a
// string MUST be encoded as `\n`. So `json.Marshal`'s output never contains
// a bare 0x0A, and the framing terminator can never collide with payload
// content. If a future codec swap (e.g. CBOR, msgpack) drops that guarantee
// the framing layer needs to switch to length-prefixed too.
func (e *Encoder) Write(msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ipc: marshal message: %w", err)
	}

	if len(body)+1 > MaxFrameBytes {
		return ErrFrameTooLarge
	}

	if _, err := e.w.Write(body); err != nil {
		return fmt.Errorf("ipc: write body: %w", err)
	}

	if err := e.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("ipc: write newline: %w", err)
	}

	return nil
}

// Flush flushes the buffered writer.
func (e *Encoder) Flush() error {
	if err := e.w.Flush(); err != nil {
		return fmt.Errorf("ipc: flush: %w", err)
	}

	return nil
}

// Decoder reads newline-delimited JSON messages from r. The internal buffer
// is sized so single-frame reads stay zero-alloc once warm.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder returns a Decoder reading from r. Backed by a bufio.Reader with
// MaxFrameBytes capacity so the size-cap can be enforced before allocating
// arbitrary memory.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReaderSize(r, MaxFrameBytes)}
}

// Read returns the next message. Returns io.EOF when the connection closes
// cleanly. Returns ErrFrameTooLarge if a line exceeds MaxFrameBytes; the
// caller must close the connection in that case.
func (d *Decoder) Read() (Message, error) {
	line, err := d.r.ReadSlice('\n')
	if err != nil {
		// bufio.ReadSlice returns ErrBufferFull when the line exceeds the
		// buffer size — that's our frame-too-large signal.
		if errors.Is(err, bufio.ErrBufferFull) {
			return Message{}, ErrFrameTooLarge
		}

		// Partial trailing line without newline (incomplete frame at EOF) is
		// treated as a protocol error; surface EOF only for clean closes.
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return Message{}, fmt.Errorf("ipc: trailing partial frame at EOF (%d bytes)", len(line))
		}

		if errors.Is(err, io.EOF) {
			return Message{}, io.EOF
		}

		return Message{}, fmt.Errorf("ipc: read frame: %w", err)
	}

	// Strip the trailing newline before unmarshal. JSON itself doesn't care,
	// but trimming keeps Marshal+Unmarshal byte-equality clean for tests.
	body := bytes.TrimRight(line, "\n")

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return Message{}, fmt.Errorf("ipc: unmarshal frame: %w", err)
	}

	return msg, nil
}

// ---------------------------------------------------------------------------
// Typed helpers — let callers go straight from Message to the command-specific
// struct without redoing the json.RawMessage indirection at every call site.
// ---------------------------------------------------------------------------

// UnmarshalArgs decodes msg.Args into dst. Returns nil if Args is absent.
func UnmarshalArgs(msg Message, dst any) error {
	return unmarshalRaw("args", msg.Args, dst)
}

// UnmarshalOk decodes msg.Ok into dst. Returns nil if Ok is absent.
func UnmarshalOk(msg Message, dst any) error {
	return unmarshalRaw("ok", msg.Ok, dst)
}

// UnmarshalEvent decodes msg.Event into dst. Returns nil if Event is absent.
func UnmarshalEvent(msg Message, dst any) error {
	return unmarshalRaw("event", msg.Event, dst)
}

// MarshalArgs sets msg.Args from src. Pass nil to clear.
func MarshalArgs(msg *Message, src any) error {
	return marshalRaw("args", &msg.Args, src)
}

// MarshalOk sets msg.Ok from src.
func MarshalOk(msg *Message, src any) error {
	return marshalRaw("ok", &msg.Ok, src)
}

// MarshalEvent sets msg.Event from src.
func MarshalEvent(msg *Message, src any) error {
	return marshalRaw("event", &msg.Event, src)
}

// MarshalDetails sets err.Details from src. Pass nil to clear. Exported so
// external consumers (e.g. the Native Mac app) can attach typed details to
// an ErrorPayload without re-importing encoding/json or reaching into
// internal helpers.
func MarshalDetails(err *ErrorPayload, src any) error {
	if err == nil {
		return errors.New("ipc: MarshalDetails called with nil ErrorPayload")
	}

	return marshalRaw("details", &err.Details, src)
}

// UnmarshalDetails decodes err.Details into dst. Returns nil if Details is
// absent or the ErrorPayload itself is nil — both are valid "no details"
// cases. Use this to read HelloMismatchError / future typed detail bodies.
func UnmarshalDetails(err *ErrorPayload, dst any) error {
	if err == nil {
		return nil
	}

	return unmarshalRaw("details", err.Details, dst)
}

func unmarshalRaw(field string, raw *json.RawMessage, dst any) error {
	if raw == nil {
		return nil
	}

	if err := json.Unmarshal(*raw, dst); err != nil {
		return fmt.Errorf("ipc: unmarshal %s: %w", field, err)
	}

	return nil
}

func marshalRaw(field string, dst **json.RawMessage, src any) error {
	if src == nil {
		*dst = nil

		return nil
	}

	body, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("ipc: marshal %s: %w", field, err)
	}

	raw := json.RawMessage(body)
	*dst = &raw

	return nil
}
