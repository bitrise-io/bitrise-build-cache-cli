package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// On ErrFrameTooLarge servers MUST close the connection — protocol has no recovery mode.
var ErrFrameTooLarge = errors.New("ipc: frame exceeds MaxFrameBytes")

type Encoder struct {
	w *bufio.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriterSize(w, 4096)}
}

// Newline framing is safe only as long as the codec is JSON (RFC 8259 §7 forbids bare LF in strings).
// A future codec swap (CBOR, msgpack) would require length-prefixed framing.
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

func (e *Encoder) Flush() error {
	if err := e.w.Flush(); err != nil {
		return fmt.Errorf("ipc: flush: %w", err)
	}

	return nil
}

type Decoder struct {
	r *bufio.Reader
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReaderSize(r, MaxFrameBytes)}
}

// Read returns ErrFrameTooLarge when a line exceeds MaxFrameBytes; caller must close the connection in that case.
func (d *Decoder) Read() (Message, error) {
	line, err := d.r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return Message{}, ErrFrameTooLarge
		}

		if errors.Is(err, io.EOF) && len(line) > 0 {
			return Message{}, fmt.Errorf("ipc: trailing partial frame at EOF (%d bytes)", len(line))
		}

		if errors.Is(err, io.EOF) {
			return Message{}, io.EOF
		}

		return Message{}, fmt.Errorf("ipc: read frame: %w", err)
	}

	body := bytes.TrimRight(line, "\n")

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return Message{}, fmt.Errorf("ipc: unmarshal frame: %w", err)
	}

	return msg, nil
}

func UnmarshalArgs(msg Message, dst any) error {
	return unmarshalRaw("args", msg.Args, dst)
}

func UnmarshalOk(msg Message, dst any) error {
	return unmarshalRaw("ok", msg.Ok, dst)
}

func UnmarshalEvent(msg Message, dst any) error {
	return unmarshalRaw("event", msg.Event, dst)
}

func MarshalArgs(msg *Message, src any) error {
	return marshalRaw("args", &msg.Args, src)
}

func MarshalOk(msg *Message, src any) error {
	return marshalRaw("ok", &msg.Ok, src)
}

func MarshalEvent(msg *Message, src any) error {
	return marshalRaw("event", &msg.Event, src)
}

func MarshalDetails(err *ErrorPayload, src any) error {
	if err == nil {
		return errors.New("ipc: MarshalDetails called with nil ErrorPayload")
	}

	return marshalRaw("details", &err.Details, src)
}

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
