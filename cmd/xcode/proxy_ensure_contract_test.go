//go:build unit

package xcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// The wrapper is the only thing that starts the proxy for a terminal build, so
// the argv it spawns has to be the one internal/spawn defines. Building it from
// the cobra command names instead matched only by naming coincidence, and would
// drift the moment either side was renamed.
func TestStartProxy_SpawnsTheArgvSpawnDefines(t *testing.T) {
	shortenProxyReadyBudget(t)

	dir := shortTempDir(t)
	t.Setenv("BITRISE_XCELERATE_PROXY_SOCKET_PATH", filepath.Join(dir, "p.sock"))

	var gotArgs []string
	recording := func(ctx context.Context, name string, args ...string) utils.Command {
		gotArgs = args

		// Something that exits immediately: the wrapper only needs a process to
		// start, and awaiting the socket is covered elsewhere.
		return utils.DefaultCommandFunc()(ctx, "/usr/bin/true")
	}

	_ = startProxy(log.NewLogger(), utils.DefaultOsProxy{}, recording, nil, filepath.Join(dir, "p.sock"))

	assert.Equal(t, spawn.XcelerateProxy().Args, gotArgs)
}

// A launch agent from a CLI at or below v3.6.9 restarts the proxy under the
// supervisor, which is the slow path this release exists to leave. Ensuring the
// proxy has to retire it, or an upgraded user never actually migrates.
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

	noop := func(ctx context.Context, _ string, _ ...string) utils.Command {
		return utils.DefaultCommandFunc()(ctx, "/usr/bin/true")
	}

	_ = startProxy(log.NewLogger(), utils.DefaultOsProxy{}, noop, nil, filepath.Join(dir, "p.sock"))

	assert.NoFileExists(t, plist, "the wrapper must retire a leftover launch agent")
}

// startProxy waits for the socket after spawning; these tests spawn something
// that never binds, so the wait is pure dead time.
func shortenProxyReadyBudget(t *testing.T) {
	t.Helper()

	prev := proxyReadyBudget
	proxyReadyBudget = 200 * time.Millisecond
	t.Cleanup(func() { proxyReadyBudget = prev })
}
