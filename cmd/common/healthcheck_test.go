//go:build unit

package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newSilentLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("TInfof", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything).Return()
	l.On("TErrorf", mock.Anything, mock.Anything).Return()
	l.On("TErrorf", mock.Anything).Return()

	return l
}

func TestExecHealthcheck_JSON_Success(t *testing.T) {
	out := &bytes.Buffer{}

	err := execHealthcheck(
		context.Background(), out, true, newSilentLogger(), "",
		func(_ context.Context) error { return nil },
	)

	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, true, got["success"])
	assert.Nil(t, got["error"])
}

func TestExecHealthcheck_JSON_Failure(t *testing.T) {
	out := &bytes.Buffer{}
	checkErr := errors.New("rpc error: PermissionDenied")

	err := execHealthcheck(
		context.Background(), out, true, newSilentLogger(), "",
		func(_ context.Context) error { return checkErr },
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "build cache backend unreachable")

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, false, got["success"])
	assert.Contains(t, got["error"], "build cache backend unreachable")
	assert.Contains(t, got["error"], "PermissionDenied")
}

func TestExecHealthcheck_NoJSON_Failure_NoOutput(t *testing.T) {
	out := &bytes.Buffer{}
	logger := newSilentLogger()
	checkErr := errors.New("connection refused")

	err := execHealthcheck(
		context.Background(), out, false, logger, "",
		func(_ context.Context) error { return checkErr },
	)

	require.Error(t, err)
	assert.Empty(t, out.String(), "non-JSON mode must not write to stdout")
	logger.AssertCalled(t, "TErrorf", mock.Anything, mock.Anything)
}
