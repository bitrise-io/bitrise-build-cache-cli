//go:build unit

package proxypid_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/proxypid"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestAcquire_NoPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, func(int) bool { return false })
	require.NoError(t, err)
	require.NotNil(t, release)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content))

	require.NoError(t, release())
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "release should remove pid file")
}

func TestAcquire_StalePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("999999\n"), 0o644))

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, func(int) bool { return false })
	require.NoError(t, err)
	defer func() { _ = release() }()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content), "stale pid file must be overwritten")
}

func TestAcquire_LivePidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("4242\n"), 0o644))

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, func(pid int) bool { return pid == 4242 })
	require.Nil(t, release)
	require.Error(t, err)
	assert.True(t, errors.Is(err, proxypid.ErrAlreadyRunning))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "4242\n", string(content), "existing pid file must not be touched")
}

func TestAcquire_OwnPidNotSelfRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644))

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, func(int) bool { return true })
	require.NoError(t, err)
	defer func() { _ = release() }()
}

func TestAcquire_ReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, nil)
	require.NoError(t, err)

	require.NoError(t, release())
	require.NoError(t, release(), "second release on already-removed pid file must not error")
}

func TestAcquire_MalformedPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("garbage"), 0o644))

	release, err := proxypid.Acquire(utils.DefaultOsProxy{}, path, func(int) bool { return true })
	require.NoError(t, err)
	defer func() { _ = release() }()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(content), "malformed pid file treated as stale")
}

func TestRead_MissingPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")

	pid, alive := proxypid.Read(utils.DefaultOsProxy{}, path, nil)
	assert.Equal(t, 0, pid)
	assert.False(t, alive)
}

func TestRead_LivePid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("4242\n"), 0o644))

	pid, alive := proxypid.Read(utils.DefaultOsProxy{}, path, func(int) bool { return true })
	assert.Equal(t, 4242, pid)
	assert.True(t, alive)
}

func TestRead_StalePid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("4242\n"), 0o644))

	pid, alive := proxypid.Read(utils.DefaultOsProxy{}, path, func(int) bool { return false })
	assert.Equal(t, 4242, pid)
	assert.False(t, alive)
}
