//go:build unit

package bazelconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderCredHelperShim_substitutesVersion(t *testing.T) {
	body := RenderCredHelperShim("v1.2.3")
	assert.Contains(t, body, `BITRISE_BUILD_CACHE_VERSION="v1.2.3"`)
	assert.NotContains(t, body, credHelperVersionPlaceholder)
}

// The installer URL must resolve to the same tag as the CLI pin, so a shim
// committed at v1 does not silently pick up a breaking installer from main.
func TestRenderCredHelperShim_installerURLDerivedFromPin(t *testing.T) {
	pinned := RenderCredHelperShim("v1.2.3")
	assert.Contains(t, pinned, "installer_ref=\"${BITRISE_BUILD_CACHE_VERSION}\"")
	assert.Contains(t, pinned, `installer_ref="main"`) // fallback branch present
	assert.Contains(t, pinned, "raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/${installer_ref}/install/installer.sh")
	assert.NotContains(t, pinned, "bitrise-build-cache-cli/main/install/installer.sh")
}

func TestCredHelperLineSnippet_expectedFormat(t *testing.T) {
	got := CredHelperLineSnippet("tools/bitrise-build-cache-credhelper.sh")
	assert.Equal(t, "build --credential_helper=*.services.bitrise.io=%workspace%/tools/bitrise-build-cache-credhelper.sh\n", got)
}

// A CLI on PATH must short-circuit the download branch.
func TestCredHelperShim_PATHShortCircuits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim only supported on POSIX platforms")
	}

	workspace := t.TempDir()
	toolsDir := filepath.Join(workspace, "tools")
	require.NoError(t, os.MkdirAll(toolsDir, 0o755))

	shim := filepath.Join(toolsDir, "bitrise-build-cache-credhelper.sh")
	require.NoError(t, os.WriteFile(shim, []byte(RenderCredHelperShim("v1.2.3")), 0o755)) //nolint:gosec

	fakeBin := filepath.Join(workspace, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	// A stand-in `bitrise-build-cache` that just echoes its args so we can prove
	// the shim delegated to it without touching the network.
	fake := filepath.Join(fakeBin, "bitrise-build-cache")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\necho SHIM_DELEGATED $*\n"), 0o755)) //nolint:gosec

	cmd := exec.Command("/bin/sh", shim, "get") //nolint:noctx // fixed argv, deterministic runtime
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+":/usr/bin:/bin",
		"BITRISE_BUILD_CACHE_WORKSPACE_ROOT="+workspace,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "output: %s", string(out))
	assert.Contains(t, string(out), "SHIM_DELEGATED get")
}

// Idempotency: a cached CLI under .bitrise-cache/bin must be re-used instead
// of re-downloaded.
func TestCredHelperShim_ReusesCachedCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim only supported on POSIX platforms")
	}

	workspace := t.TempDir()
	toolsDir := filepath.Join(workspace, "tools")
	require.NoError(t, os.MkdirAll(toolsDir, 0o755))
	shim := filepath.Join(toolsDir, "bitrise-build-cache-credhelper.sh")
	require.NoError(t, os.WriteFile(shim, []byte(RenderCredHelperShim("v1.2.3")), 0o755)) //nolint:gosec

	cacheBin := filepath.Join(workspace, ".bitrise-cache", "bin")
	require.NoError(t, os.MkdirAll(cacheBin, 0o755))
	cached := filepath.Join(cacheBin, "bitrise-build-cache")
	require.NoError(t, os.WriteFile(cached, []byte("#!/bin/sh\necho CACHED_HIT $*\n"), 0o755)) //nolint:gosec

	// Empty PATH (only base utilities) so PATH lookup misses.
	cmd := exec.Command("/bin/sh", shim, "get") //nolint:noctx // fixed argv, deterministic runtime
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"BITRISE_BUILD_CACHE_WORKSPACE_ROOT=" + workspace,
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "output: %s", string(out))
	assert.Contains(t, string(out), "CACHED_HIT get")
}

// Concurrent shim invocations on a cold cache must serialise on the install
// lock — only one fetch happens, and every process ends up exec-ing an intact
// binary.
func TestCredHelperShim_ConcurrentInstallSerialises(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim only supported on POSIX platforms")
	}

	workspace := t.TempDir()
	toolsDir := filepath.Join(workspace, "tools")
	require.NoError(t, os.MkdirAll(toolsDir, 0o755))
	shim := filepath.Join(toolsDir, "bitrise-build-cache-credhelper.sh")
	require.NoError(t, os.WriteFile(shim, []byte(RenderCredHelperShim("v1.2.3")), 0o755)) //nolint:gosec

	// Fake installer that logs each invocation and writes a stub CLI slowly, so
	// racers overlap. We serve it via a fake `curl` that just prints the script.
	callLog := filepath.Join(workspace, "installer-calls.log")
	fakeInstaller := "#!/bin/sh\n" +
		"echo installer-call >>" + callLog + "\n" +
		"# parse -b <dir>\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -b) shift; out_dir=\"$1\"; shift ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"mkdir -p \"$out_dir\"\n" +
		"sleep 0.3\n" +
		"cat >\"$out_dir/bitrise-build-cache\" <<'STUB'\n" +
		"#!/bin/sh\n" +
		"echo INSTALLED_HIT $*\n" +
		"STUB\n" +
		"chmod +x \"$out_dir/bitrise-build-cache\"\n"

	fakeBin := filepath.Join(workspace, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	// Fake curl echoes the installer body regardless of URL.
	curlScript := "#!/bin/sh\ncat <<'INSTALLER'\n" + fakeInstaller + "INSTALLER\n"
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(curlScript), 0o755)) //nolint:gosec

	const workers = 8
	outs := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command("/bin/sh", shim, "get") //nolint:noctx // fixed argv, deterministic runtime
			cmd.Env = []string{
				"PATH=" + fakeBin + ":/usr/bin:/bin",
				"BITRISE_BUILD_CACHE_WORKSPACE_ROOT=" + workspace,
			}
			outs[i], errs[i] = cmd.CombinedOutput()
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i], "worker %d output: %s", i, string(outs[i]))
		assert.Contains(t, string(outs[i]), "INSTALLED_HIT get", "worker %d", i)
	}

	// The installer must have run exactly once (double-check-after-lock).
	logBytes, err := os.ReadFile(callLog)
	require.NoError(t, err)
	// Count non-empty lines.
	calls := 0
	for _, b := range logBytes {
		if b == '\n' {
			calls++
		}
	}
	assert.Equal(t, 1, calls, "installer should have run exactly once, got %d\nlog:\n%s", calls, string(logBytes))
}

// With no CLI on PATH and no network, the shim must fail loudly and never
// return zero — Bazel will surface a non-zero exit.
func TestCredHelperShim_FailsClearlyWhenOfflineAndNoCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim only supported on POSIX platforms")
	}

	workspace := t.TempDir()
	toolsDir := filepath.Join(workspace, "tools")
	require.NoError(t, os.MkdirAll(toolsDir, 0o755))
	shim := filepath.Join(toolsDir, "bitrise-build-cache-credhelper.sh")
	require.NoError(t, os.WriteFile(shim, []byte(RenderCredHelperShim("v1.2.3")), 0o755)) //nolint:gosec

	// A fake `curl` that always fails so we don't hit the network.
	fakeBin := filepath.Join(workspace, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"),
		[]byte("#!/bin/sh\necho >&2 fake-curl failure\nexit 42\n"), 0o755)) //nolint:gosec
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "wget"),
		[]byte("#!/bin/sh\necho >&2 fake-wget failure\nexit 42\n"), 0o755)) //nolint:gosec

	cmd := exec.Command("/bin/sh", shim, "get") //nolint:noctx // fixed argv, deterministic runtime
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+":/usr/bin:/bin",
		"BITRISE_BUILD_CACHE_WORKSPACE_ROOT="+workspace,
	)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "shim must exit non-zero when offline and no CLI available")
	assert.Contains(t, string(out), "installer")
}
