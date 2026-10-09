//go:build unit

package xcelerate

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

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
