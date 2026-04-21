package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ProtocolVersion = 0x01
	Cap0            = 0x00 // get/put/remove/stop operations

	RequestGet              = 0x00
	RequestPut              = 0x01
	RequestRemove           = 0x02
	RequestStop             = 0x03
	RequestSetInvocationID  = 0xB1
	RequestGetSessionStats  = 0xB2
	RequestHealthCheck      = 0xB3

	ResponseOK   = 0x00
	ResponseNoop = 0x01
	ResponseErr  = 0x02

	PutFlagOverwrite = 0x01
)

func ReadGreeting(r io.Reader) error {
	version, err := ReadByte(r)
	if err != nil {
		return fmt.Errorf("read protocol version: %w", err)
	}
	if version != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version: 0x%02x", version)
	}

	ncaps, err := ReadByte(r)
	if err != nil {
		return fmt.Errorf("read capabilities count: %w", err)
	}

	for i := 0; i < int(ncaps); i++ {
		if _, err := ReadByte(r); err != nil {
			return fmt.Errorf("read capability: %w", err)
		}
	}

	return nil
}

func WriteGreeting(w io.Writer) error {
	if err := WriteByte(w, ProtocolVersion); err != nil {
		return err
	}

	caps := []byte{Cap0}
	if err := WriteByte(w, uint8(len(caps))); err != nil {
		return err
	}
	for _, cap := range caps {
		if err := WriteByte(w, cap); err != nil {
			return err
		}
	}

	return nil
}

func ReadRequest(r io.Reader) (byte, error) {
	reqType, err := ReadByte(r)
	if err != nil {
		return 0, err
	}
	return reqType, nil
}

func ReadKey(r io.Reader) ([]byte, error) {
	keyLen, err := ReadByte(r)
	if err != nil {
		return nil, err
	}

	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}

	return key, nil
}

func ReadValue(r io.Reader) ([]byte, error) {
	var valueLen uint64
	if err := binary.Read(r, binary.NativeEndian, &valueLen); err != nil {
		return nil, err
	}

	value := make([]byte, valueLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	return value, nil
}

func WriteOK(w io.Writer) error {
	return WriteByte(w, ResponseOK)
}

func WriteNoop(w io.Writer) error {
	return WriteByte(w, ResponseNoop)
}

func WriteErr(w io.Writer, msg string) error {
	if err := WriteByte(w, ResponseErr); err != nil {
		return err
	}
	return WriteMsg(w, msg)
}

func WriteValue(w io.Writer, value []byte) error {
	valueLen := uint64(len(value))
	if err := binary.Write(w, binary.NativeEndian, valueLen); err != nil {
		return err
	}
	_, err := w.Write(value)
	return err
}

func WriteByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}

func ReadByte(r io.Reader) (byte, error) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func WriteMsg(w io.Writer, msg string) error {
	if len(msg) > 255 {
		msg = msg[:255]
	}
	if err := WriteByte(w, uint8(len(msg))); err != nil {
		return err
	}
	_, err := w.Write([]byte(msg))
	return err
}

func ReadMsg(r io.Reader) (string, error) {
	msgLen, err := ReadByte(r)
	if err != nil {
		return "", err
	}
	msg := make([]byte, msgLen)
	if _, err := io.ReadFull(r, msg); err != nil {
		return "", err
	}
	return string(msg), nil
}

func WriteSetInvocationID(w io.Writer, parentID, childID string) error {
	if err := WriteByte(w, RequestSetInvocationID); err != nil {
		return err
	}
	if err := WriteMsg(w, parentID); err != nil {
		return err
	}

	return WriteMsg(w, childID)
}

func WriteSessionStats(w io.Writer, downloadBytes, uploadBytes int64, invocationID, parentID string) error {
	if err := WriteByte(w, ResponseOK); err != nil {
		return err
	}
	if err := binary.Write(w, binary.NativeEndian, downloadBytes); err != nil {
		return err
	}
	if err := binary.Write(w, binary.NativeEndian, uploadBytes); err != nil {
		return err
	}
	if err := WriteMsg(w, invocationID); err != nil {
		return err
	}

	return WriteMsg(w, parentID)
}

func ReadSessionStats(r io.Reader) (downloadBytes, uploadBytes int64, invocationID, parentID string, err error) {
	if err := binary.Read(r, binary.NativeEndian, &downloadBytes); err != nil {
		return 0, 0, "", "", fmt.Errorf("read download bytes: %w", err)
	}
	if err := binary.Read(r, binary.NativeEndian, &uploadBytes); err != nil {
		return 0, 0, "", "", fmt.Errorf("read upload bytes: %w", err)
	}
	invocationID, err = ReadMsg(r)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("read invocation ID: %w", err)
	}
	parentID, err = ReadMsg(r)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("read parent ID: %w", err)
	}

	return downloadBytes, uploadBytes, invocationID, parentID, nil
}

func ReadSetInvocationID(r io.Reader) (parentID, childID string, err error) {
	parentID, err = ReadMsg(r)
	if err != nil {
		return "", "", err
	}

	childID, err = ReadMsg(r)
	if err != nil {
		return "", "", err
	}

	return parentID, childID, nil
}
