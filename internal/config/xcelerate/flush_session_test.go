//go:build unit

package xcelerate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

type fakeFlushClient struct {
	resp *session.FlushSessionResponse
	err  error
}

func (f *fakeFlushClient) FlushSession(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*session.FlushSessionResponse, error) {
	return f.resp, f.err
}

func TestPrintEmittedURLs_PrintsOneLinePerID(t *testing.T) {
	buf := &bytes.Buffer{}
	client := &fakeFlushClient{resp: &session.FlushSessionResponse{EmittedInvocationIds: []string{"id-a", "id-b"}}}

	require.NoError(t, printEmittedURLs(context.Background(), nullLogger{}, buf, client))

	out := buf.String()
	assert.Equal(t, 2, strings.Count(out, "Invocation saved. Visit"))
	assert.Contains(t, out, "https://app.bitrise.io/build-cache/invocations/xcode/id-a")
	assert.Contains(t, out, "https://app.bitrise.io/build-cache/invocations/xcode/id-b")
}

func TestPrintEmittedURLs_EmptyResponseIsSilentAndSuccess(t *testing.T) {
	buf := &bytes.Buffer{}
	client := &fakeFlushClient{resp: &session.FlushSessionResponse{}}

	require.NoError(t, printEmittedURLs(context.Background(), nullLogger{}, buf, client))

	assert.Empty(t, buf.String())
}

func TestPrintEmittedURLs_RPCErrorExitsZero(t *testing.T) {
	buf := &bytes.Buffer{}
	client := &fakeFlushClient{err: errors.New("boom")}

	require.NoError(t, printEmittedURLs(context.Background(), nullLogger{}, buf, client))

	assert.Empty(t, buf.String())
}

func TestFlushSession_NoProxyPrintsStdoutNote(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, paths.XcelerateRootRelative), 0o755))
	// Minimal valid xcelerate config so ReadConfig succeeds; no pid file → no proxy.
	cfgPath := filepath.Join(home, paths.XcelerateRootRelative, "config.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{}`), 0o644))

	buf := &bytes.Buffer{}
	err := FlushSession(context.Background(), nullLogger{}, stubOsProxy{DefaultOsProxy: utils.DefaultOsProxy{}, home: home}, buf)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No pending invocations to flush (proxy not running).")
}
