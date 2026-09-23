//go:build unit

package multiplatform_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
)

func newTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	for _, name := range []string{"Debugf", "Infof", "Warnf", "Errorf", "Printf",
		"TDebugf", "TInfof", "TWarnf", "TErrorf", "TDonef", "TPrintf", "Donef", "Println"} {
		l.On(name, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	return l
}

const bodyReadTimeout = `{"error":"read request body: read tcp 10.70.3.171:3000->127.0.0.6:54769: i/o timeout"}`

func newClient(t *testing.T, handler http.HandlerFunc) (*multiplatform.Client, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := multiplatform.NewClient(srv.URL, "token", newTestLogger())
	require.NoError(t, err)

	return c, srv
}

// The service answers a body-read timeout with 400, which retryablehttp's
// default policy treats as terminal — losing the invocation for good.
func TestPut_RetriesBadRequestFromRequestBodyReadFailure(t *testing.T) {
	var calls atomic.Int32

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(bodyReadTimeout))

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, c.Put("/v1/invocations/x", map[string]string{"a": "b"}))
	assert.Equal(t, int32(2), calls.Load(), "the resend must land after the body-read 400")
}

func TestPut_ExhaustedRetriesKeepStatusAndServerBody(t *testing.T) {
	var calls atomic.Int32

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bodyReadTimeout))
	})

	err := c.Put("/v1/invocations/x", map[string]string{"a": "b"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Contains(t, err.Error(), "read request body", "the server's diagnostic must survive retry exhaustion")
	assert.Equal(t, int32(4), calls.Load(), "one attempt plus maxHTTPClientRetries")
}

// Only the body-read class is retryable: a genuine rejection must fail fast.
func TestPut_DoesNotRetryOrdinaryBadRequest(t *testing.T) {
	var calls atomic.Int32

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid invocation id"}`))
	})

	err := c.Put("/v1/invocations/x", map[string]string{"a": "b"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid invocation id")
	assert.Equal(t, int32(1), calls.Load(), "a real 400 must not be resent")
}

func TestPut_StillRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, c.Put("/v1/invocations/x", map[string]string{"a": "b"}))
	assert.Equal(t, int32(2), calls.Load())
}

func TestPut_SucceedsOnFirstAttempt(t *testing.T) {
	var calls atomic.Int32

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, c.Put("/v1/invocations/x", map[string]string{"a": "b"}))
	assert.Equal(t, int32(1), calls.Load())
}
