//go:build unit

package xcode

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestAcquireProxyPidLock_NoPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")

	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, func(int) bool { return false })
	require.NoError(t, err)
	require.NotNil(t, release)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content))

	require.NoError(t, release())
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "release should remove pid file")
}

func TestAcquireProxyPidLock_StalePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("999999\n"), 0o644))

	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, func(int) bool { return false })
	require.NoError(t, err)
	defer func() { _ = release() }()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content), "stale pid file must be overwritten")
}

func TestAcquireProxyPidLock_LivePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("4242\n"), 0o644))

	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, func(pid int) bool { return pid == 4242 })
	require.Nil(t, release)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errProxyAlreadyRunning))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "4242\n", string(content), "existing pid file must not be touched")
}

func TestAcquireProxyPidLock_OwnPidNotSelfRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644))

	// isAlive returning true would normally reject, but we must not reject our own pid.
	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, func(int) bool { return true })
	require.NoError(t, err)
	defer func() { _ = release() }()
}

func TestAcquireProxyPidLock_ReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")

	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, nil)
	require.NoError(t, err)

	require.NoError(t, release())
	require.NoError(t, release(), "second release on already-removed pid file must not error")
}

func TestAcquireProxyPidLock_MalformedPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("garbage"), 0o644))

	release, err := acquireProxyPidLock(utils.DefaultOsProxy{}, path, func(int) bool { return true })
	require.NoError(t, err)
	defer func() { _ = release() }()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content), "malformed pid file treated as stale")
}
