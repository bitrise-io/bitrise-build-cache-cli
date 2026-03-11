//go:build unit

package ccache

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	"github.com/bitrise-io/go-utils/v2/log"
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connStub implements io.ReadWriter using separate read/write buffers.
type connStub struct {
	r *bytes.Buffer
	w *bytes.Buffer
}

func (c *connStub) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *connStub) Write(p []byte) (int, error) {
	return c.w.Write(p)
}

// buildGetRequest builds a GET request: [0x00][keyLen][key...]
func buildGetRequest(key []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestGet)
	buf.WriteByte(byte(len(key)))
	buf.Write(key)
	return buf.Bytes()
}

// buildPutRequest builds a PUT request: [0x01][keyLen][key...][flags][uint64LE size][value...]
func buildPutRequest(key, value []byte, flags byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestPut)
	buf.WriteByte(byte(len(key)))
	buf.Write(key)
	buf.WriteByte(flags)
	sizeBytes := make([]byte, 8)
	binary.NativeEndian.PutUint64(sizeBytes, uint64(len(value)))
	buf.Write(sizeBytes)
	buf.Write(value)
	return buf.Bytes()
}

// buildRemoveRequest builds a REMOVE request: [0x02][keyLen][key...]
func buildRemoveRequest(key []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestRemove)
	buf.WriteByte(byte(len(key)))
	buf.Write(key)
	return buf.Bytes()
}

// buildStopRequest builds a STOP request: [0x03]
func buildStopRequest() []byte {
	return []byte{protocol.RequestStop}
}

// buildSetInvocationIDRequest builds a SetInvocationID request: [0xB1][len][id...]
func buildSetInvocationIDRequest(id string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestSetInvocationID)
	buf.WriteByte(byte(len(id)))
	buf.WriteString(id)
	return buf.Bytes()
}

var noOpCaps = func() error { return nil }

func defaultConfig() Config {
	return Config{
		PushEnabled: true,
		Layout:      "",
	}
}

func Test_requestProcessor_processRequest(t *testing.T) {
	t.Run("GET cache hit", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}
		data := []byte("hello world")

		client := &ClientMock{
			DownloadStreamFunc: func(_ context.Context, w io.Writer, _ string) error {
				_, err := w.Write(data)
				return err
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildGetRequest(key)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_OK, result.Outcome)

		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseOK), resp[0])

		// Read back the value from the response buffer after the 0x00 byte
		respReader := bytes.NewReader(resp[1:])
		got, err := protocol.ReadValue(respReader)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("GET cache miss", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}

		client := &ClientMock{
			DownloadStreamFunc: func(_ context.Context, _ io.Writer, _ string) error {
				return kv.ErrCacheNotFound
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildGetRequest(key)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_MISS, result.Outcome)
		assert.Equal(t, byte(protocol.ResponseNoop), conn.w.Bytes()[0])
	})

	t.Run("GET download error", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}

		client := &ClientMock{
			DownloadStreamFunc: func(_ context.Context, _ io.Writer, _ string) error {
				return errors.New("network failure")
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildGetRequest(key)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_ERROR, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseErr), resp[0])
	})

	t.Run("GET capabilities error", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}

		client := &ClientMock{}

		conn := &connStub{
			r: bytes.NewBuffer(buildGetRequest(key)),
			w: &bytes.Buffer{},
		}

		getCaps := func() error { return errors.New("caps error") }

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, getCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_ERROR, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseErr), resp[0])
	})

	t.Run("PUT push enabled, upload succeeds", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}
		value := []byte("cache content")
		expectedKey := "abcd"

		var capturedKey string
		var capturedData []byte

		client := &ClientMock{
			UploadStreamToBuildCacheFunc: func(_ context.Context, r io.ReadSeeker, k string, _ int64) error {
				capturedKey = k
				var err error
				capturedData, err = io.ReadAll(r)
				return err
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildPutRequest(key, value, 0x00)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_OK, result.Outcome)
		assert.Equal(t, expectedKey, capturedKey)
		assert.Equal(t, value, capturedData)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseOK), resp[0])
	})

	t.Run("PUT push disabled", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}
		value := []byte("cache content")

		client := &ClientMock{}

		conn := &connStub{
			r: bytes.NewBuffer(buildPutRequest(key, value, 0x00)),
			w: &bytes.Buffer{},
		}

		cfg := defaultConfig()
		cfg.PushEnabled = false

		proc := newRequestProcessor(conn, cfg, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_PUSH_DISABLED, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseNoop), resp[0])
	})

	t.Run("PUT upload error", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}
		value := []byte("cache content")

		client := &ClientMock{
			UploadStreamToBuildCacheFunc: func(_ context.Context, _ io.ReadSeeker, _ string, _ int64) error {
				return errors.New("upload failed")
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildPutRequest(key, value, 0x00)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_ERROR, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseErr), resp[0])
	})

	t.Run("REMOVE", func(t *testing.T) {
		key := []byte{0xAB, 0xCD}

		client := &ClientMock{}

		conn := &connStub{
			r: bytes.NewBuffer(buildRemoveRequest(key)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_OK, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseOK), resp[0])
	})

	t.Run("STOP", func(t *testing.T) {
		client := &ClientMock{}

		conn := &connStub{
			r: bytes.NewBuffer(buildStopRequest()),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, nil, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_SHOULD_STOP, result.Outcome)
		assert.Empty(t, conn.w.Bytes())
	})

	t.Run("SET_INVOCATION_ID", func(t *testing.T) {
		invocationID := "test-invocation-123"
		var capturedID string

		factoryLogger := &utilsMocks.Logger{}
		for _, method := range []string{"TDebugf", "TInfof", "TErrorf", "Warnf", "Infof", "Debugf"} {
			registerLoggerMethod(factoryLogger, method)
		}

		loggerFactory := LoggerFactory(func(id string) (log.Logger, error) {
			capturedID = id
			return factoryLogger, nil
		})

		client := &ClientMock{}

		conn := &connStub{
			r: bytes.NewBuffer(buildSetInvocationIDRequest(invocationID)),
			w: &bytes.Buffer{},
		}

		proc := newRequestProcessor(conn, defaultConfig(), client, mockLogger, loggerFactory, noOpCaps)
		result := proc.processRequest()

		assert.Equal(t, PROCESS_REQUEST_OK, result.Outcome)
		assert.Equal(t, invocationID, capturedID)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseOK), resp[0])
	})
}

func Test_keyToPath(t *testing.T) {
	t.Run("flat layout with empty string", func(t *testing.T) {
		proc := &requestProcessor{config: Config{Layout: ""}}
		key := []byte{0xAB, 0xCD}
		assert.Equal(t, "abcd", proc.keyToPath(key))
	})

	t.Run("flat layout explicit", func(t *testing.T) {
		proc := &requestProcessor{config: Config{Layout: "flat"}}
		key := []byte{0xAB, 0xCD}
		assert.Equal(t, "abcd", proc.keyToPath(key))
	})

	t.Run("subdirs layout", func(t *testing.T) {
		proc := &requestProcessor{config: Config{Layout: "subdirs"}}
		key := []byte{0xAB, 0xCD, 0xEF}
		assert.Equal(t, "ccache/1-ab/cdef", proc.keyToPath(key))
	})

	t.Run("bazel layout with 32-byte key", func(t *testing.T) {
		proc := &requestProcessor{config: Config{Layout: "bazel"}}
		key := make([]byte, 32)
		expected := "ac/" + strings.Repeat("0", 64)
		assert.Equal(t, expected, proc.keyToPath(key))
	})
}
