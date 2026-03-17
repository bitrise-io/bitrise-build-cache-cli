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

	"github.com/bitrise-io/go-utils/v2/log"
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
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

var noOpCaps = func(context.Context) error { return nil }

func defaultConfig() ccacheconfig.Config {
	return ccacheconfig.Config{
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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

		assert.Equal(t, PROCESS_REQUEST_ERROR, result.Outcome)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseErr), resp[0])
	})

	t.Run("capabilities error reported by initCapabilities", func(t *testing.T) {
		client := &ClientMock{}
		conn := &connStub{r: &bytes.Buffer{}, w: &bytes.Buffer{}}

		getCaps := func(context.Context) error { return errors.New("caps error") }

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, getCaps)
		err := proc.initCapabilities(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "caps error")
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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, cfg, configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

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

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		result := proc.processRequest(context.Background())

		assert.Equal(t, PROCESS_REQUEST_SHOULD_STOP, result.Outcome)
		assert.Empty(t, conn.w.Bytes())
	})

	t.Run("SET_INVOCATION_ID", func(t *testing.T) {
		invocationID := "test-invocation-123"
		var capturedLoggerID string

		factoryLogger := &utilsMocks.Logger{}
		for _, method := range []string{"TDebugf", "TInfof", "TErrorf", "Warnf", "Infof", "Debugf"} {
			registerLoggerMethod(factoryLogger, method)
		}

		loggerFactory := LoggerFactory(func(id string) (log.Logger, error) {
			capturedLoggerID = id
			return factoryLogger, nil
		})

		var changeSessionCalls []struct{ invocationID, appSlug, buildSlug, stepID string }
		client := &ClientMock{
			ChangeSessionFunc: func(invocationID, appSlug, buildSlug, stepID string) {
				changeSessionCalls = append(changeSessionCalls, struct {
					invocationID, appSlug, buildSlug, stepID string
				}{invocationID, appSlug, buildSlug, stepID})
			},
		}

		conn := &connStub{
			r: bytes.NewBuffer(buildSetInvocationIDRequest(invocationID)),
			w: &bytes.Buffer{},
		}

		meta := configcommon.CacheConfigMetadata{
			BitriseAppID:           "my-app",
			BitriseBuildID:         "my-build",
			BitriseStepExecutionID: "my-step",
		}

		proc := newRequestProcessor(conn, defaultConfig(), meta, client, mockLogger, loggerFactory, noOpCaps)
		result := proc.processRequest(context.Background())

		assert.Equal(t, PROCESS_REQUEST_OK, result.Outcome)
		assert.Equal(t, invocationID, capturedLoggerID)
		resp := conn.w.Bytes()
		require.NotEmpty(t, resp)
		assert.Equal(t, byte(protocol.ResponseOK), resp[0])

		require.Len(t, changeSessionCalls, 1)
		assert.Equal(t, invocationID, changeSessionCalls[0].invocationID)
		assert.Equal(t, "my-app", changeSessionCalls[0].appSlug)
		assert.Equal(t, "my-build", changeSessionCalls[0].buildSlug)
		assert.Equal(t, "my-step", changeSessionCalls[0].stepID)
	})

	t.Run("context cancellation while waiting for semaphore", func(t *testing.T) {
		client := &ClientMock{}
		conn := &connStub{
			r: bytes.NewBuffer(buildGetRequest([]byte{0x01})),
			w: &bytes.Buffer{},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel before processRequest is called

		proc := newRequestProcessor(conn, defaultConfig(), configcommon.CacheConfigMetadata{}, client, mockLogger, nil, noOpCaps)
		<-proc.ccSemaphore // drain to simulate semaphore held by another goroutine

		result := proc.processRequest(ctx)

		assert.Equal(t, PROCESS_REQUEST_ERROR, result.Outcome)
		assert.ErrorIs(t, result.Err, context.Canceled)
	})
}

func Test_keyToPath(t *testing.T) {
	t.Run("flat layout with empty string", func(t *testing.T) {
		proc := &requestProcessor{config: ccacheconfig.Config{Layout: ""}}
		key := []byte{0xAB, 0xCD}
		assert.Equal(t, "abcd", proc.keyToPath(key))
	})

	t.Run("flat layout explicit", func(t *testing.T) {
		proc := &requestProcessor{config: ccacheconfig.Config{Layout: "flat"}}
		key := []byte{0xAB, 0xCD}
		assert.Equal(t, "abcd", proc.keyToPath(key))
	})

	t.Run("subdirs layout", func(t *testing.T) {
		proc := &requestProcessor{config: ccacheconfig.Config{Layout: "subdirs"}}
		key := []byte{0xAB, 0xCD, 0xEF}
		assert.Equal(t, "ccache/1-ab/cdef", proc.keyToPath(key))
	})

	t.Run("bazel layout with 32-byte key", func(t *testing.T) {
		proc := &requestProcessor{config: ccacheconfig.Config{Layout: "bazel"}}
		key := make([]byte, 32)
		expected := "ac/" + strings.Repeat("0", 64)
		assert.Equal(t, expected, proc.keyToPath(key))
	})
}
