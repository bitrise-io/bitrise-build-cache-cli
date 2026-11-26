//go:build unit

package xcode

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Built from the cobra command names, the argv matched internal/spawn only by
// naming coincidence, and would drift on either rename.
func TestStartProxy_SpawnsTheArgvSpawnDefines(t *testing.T) {
	shortenProxyReadyBudget(t)

	dir := shortTempDir(t)
	t.Setenv("BITRISE_XCELERATE_PROXY_SOCKET_PATH", filepath.Join(dir, "p.sock"))

	rec := &proxySpawnRecorder{}
	_ = startProxy(log.NewLogger(), utils.DefaultOsProxy{}, rec.spawn, nil, filepath.Join(dir, "p.sock"))

	assert.Equal(t, spawn.XcelerateProxy().Args, rec.args)
}

// Without this an upgraded user never leaves the supervised path.
func TestStartProxy_RetiresALegacyLaunchAgent(t *testing.T) {
	if _, err := os.Stat("/bin/launchctl"); err != nil {
		t.Skip("launchd-only")
	}

	shortenProxyReadyBudget(t)

	dir := shortTempDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.LaunchAgentsDir(), 0o755))
	plist := p.PlistPath("io.bitrise.build-cache." + spawn.NameXcelerateProxy)
	require.NoError(t, os.WriteFile(plist, []byte("<plist/>"), 0o644))

	rec := &proxySpawnRecorder{}
	_ = startProxy(log.NewLogger(), utils.DefaultOsProxy{}, rec.spawn, nil, filepath.Join(dir, "p.sock"))

	assert.NoFileExists(t, plist, "the wrapper must retire a leftover launch agent")
}

// These spawn something that never binds, so the wait is pure dead time.
func shortenProxyReadyBudget(t *testing.T) {
	t.Helper()

	prev := proxyReadyBudget
	proxyReadyBudget = 200 * time.Millisecond
	t.Cleanup(func() { proxyReadyBudget = prev })
}

type proxySpawnRecorder struct {
	spawns int
	args   []string
	killed []int
}

// Never the real argv: that is this test binary, which would rerun the suite detached.
func (r *proxySpawnRecorder) spawn(ctx context.Context, _ string, args ...string) utils.Command {
	r.spawns++
	r.args = args

	return utils.DefaultCommandFunc()(ctx, "/usr/bin/true")
}

func (r *proxySpawnRecorder) kill(pid int, _ syscall.Signal) {
	r.killed = append(r.killed, pid)
}
