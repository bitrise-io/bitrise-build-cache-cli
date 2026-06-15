//go:build unit

package browse

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/browse"
)

// browse_ErrNoOpener returns the internal sentinel so the public-API
// test can drive the warn-path branch without importing the internal
// package in line.
func browse_ErrNoOpener() error { return browse.ErrNoOpener }

//nolint:gochecknoglobals
var errSimulatedExecFailure = errors.New("simulated exec failure")

func newLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&bytes.Buffer{}))
}

// stubOpener records the URL it was asked to open and returns the
// pre-configured error (nil by default). Used to assert that Browser.Open
// hands off to the opener with the expected URL.
type stubOpener struct {
	seenURL string
	err     error
}

func (s *stubOpener) Open(_ context.Context, url string) error {
	s.seenURL = url

	return s.err
}

func TestBrowse_workspaceFromEnv_buildsListURLWithSourceLocal(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		Envs:    map[string]string{"BITRISE_BUILD_CACHE_WORKSPACE_ID": "ws_abc"},
		BaseURL: "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations?source=local", got)
	assert.Equal(t, got, op.seenURL)
}

func TestBrowse_invocationID_deepLinks_noSourceFilter(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID:  "ws_abc",
		InvocationID: "inv_xyz",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations/inv_xyz", got)
	assert.NotContains(t, got, "source=")
}

func TestBrowse_printOnlySkipsOpener(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		PrintOnly:   true,
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.Empty(t, op.seenURL, "PrintOnly must not invoke the opener")
}

func TestBrowse_missingWorkspaceReturnsSentinel(t *testing.T) {
	b := &Browser{Logger: newLogger(), Opener: &stubOpener{}}

	_, err := b.Open(context.Background(), Params{Envs: map[string]string{}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkspaceNotConfigured)
}

func TestBrowse_openerErrNoOpener_emitsNoSupportedLauncherWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	op := &stubOpener{err: browse_ErrNoOpener()}
	b := &Browser{Logger: logger, Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.Contains(t, buf.String(), "No default browser launcher for this OS")
}

func TestBrowse_openerGenericError_emitsCouldNotAutoLaunchWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	op := &stubOpener{err: errSimulatedExecFailure}
	b := &Browser{Logger: logger, Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.Contains(t, buf.String(), "Could not auto-launch the browser")
	// Generic-error message should include the underlying error text so
	// post-mortem isn't blind.
	assert.Contains(t, buf.String(), "simulated exec failure")
}
