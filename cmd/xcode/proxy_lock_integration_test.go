//go:build unit

package xcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// The proxy singleton only means anything across processes: the xcodebuild wrapper
// decides whether to spawn a proxy, and several wrappers can run at once. These
// re-exec the test binary so real processes contend for the real lock.
//
// A helper test rather than a TestMain hook, because the package already has one in
// the external test package and a binary gets only one.

const (
	envProxyRole  = "PROXYLOCK_CHILD_ROLE"
	envProxyHome  = "PROXYLOCK_CHILD_HOME"
	envProxySent  = "PROXYLOCK_CHILD_SENTINEL"
	envProxyLog   = "PROXYLOCK_CHILD_LOG"
	envProxyHold  = "PROXYLOCK_CHILD_HOLD_MS"
	envProxyEpoch = "PROXYLOCK_CHILD_EPOCH"

	proxyRoleStart = "start"
	proxyRoleCrash = "crash"

	proxyExitOverlap = 6
)

// TestHelperProxyLockChild is the child process. It is inert unless the parent
// asked for a role, so a normal run just skips it.
func TestHelperProxyLockChild(t *testing.T) {
	role := os.Getenv(envProxyRole)
	if role == "" {
		t.Skip("not a child process")
	}

	// The package's TestMain repoints HOME at its own temp dir, so claim it back
	// after it has run.
	require.NoError(t, os.Setenv("HOME", os.Getenv(envProxyHome)))
	osProxy := utils.DefaultOsProxy{}
	holdMS, _ := strconv.Atoi(os.Getenv(envProxyHold))

	// The same call start-proxy's RunE makes, so the child exercises production's
	// contention policy — log and exit 0 — not just the lock.
	served := false
	err := withProxySingleton(osProxy, log.NewLogger(), func() error {
		served = true

		sentinel := os.Getenv(envProxySent)
		f, sErr := os.OpenFile(sentinel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if sErr != nil {
			logProxyEvent(fmt.Sprintf("OVERLAP   pid=%-6d A SECOND PROXY IS SERVING: %v", os.Getpid(), sErr))
			os.Exit(proxyExitOverlap)
		}
		_ = f.Close()
		logProxyEvent(fmt.Sprintf("SERVING   pid=%-6d advertised=%d", os.Getpid(), advertisedPID(osProxy)))

		time.Sleep(time.Duration(holdMS) * time.Millisecond)

		if role == proxyRoleCrash {
			logProxyEvent(fmt.Sprintf("CRASHING  pid=%-6d SIGKILL while serving", os.Getpid()))
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}

		_ = os.Remove(sentinel)

		return nil
	})
	if err != nil {
		logProxyEvent(fmt.Sprintf("FAILED    pid=%-6d %v", os.Getpid(), err))
		os.Exit(1)
	}
	if !served {
		logProxyEvent(fmt.Sprintf("SKIPPED   pid=%-6d stood down, another proxy is serving", os.Getpid()))
		os.Exit(0)
	}
	logProxyEvent(fmt.Sprintf("STOPPED   pid=%-6d", os.Getpid()))
	os.Exit(0)
}

func advertisedPID(osProxy utils.OsProxy) int {
	content, _, err := osProxy.ReadFileIfExists(xcelerate.ProxyPidFile(osProxy))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(content))

	return pid
}

func logProxyEvent(line string) {
	f, err := os.OpenFile(os.Getenv(envProxyLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	epoch, _ := strconv.ParseInt(os.Getenv(envProxyEpoch), 10, 64)
	_, _ = f.WriteString(fmt.Sprintf("t=%-8s %s\n", time.Since(time.Unix(0, epoch)).Round(time.Millisecond), line))
}

type proxyEnv struct {
	home     string
	sentinel string
	log      string
	epoch    time.Time
}

func newProxyEnv(t *testing.T) proxyEnv {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	return proxyEnv{
		home:     dir,
		sentinel: filepath.Join(dir, "serving.sentinel"),
		log:      filepath.Join(dir, "events.log"),
		epoch:    time.Now(),
	}
}

func (e proxyEnv) start(t *testing.T, role string, holdMS int) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestHelperProxyLockChild") //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(),
		envProxyRole+"="+role,
		envProxyHome+"="+e.home,
		envProxySent+"="+e.sentinel,
		envProxyLog+"="+e.log,
		envProxyHold+"="+strconv.Itoa(holdMS),
		envProxyEpoch+"="+strconv.FormatInt(e.epoch.UnixNano(), 10),
	)
	// A group leader, so stop-proxy's negative-pid group signal reaches it exactly as
	// it reaches a real detached proxy.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	require.NoError(t, cmd.Start())

	return cmd
}

func (e proxyEnv) events(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(e.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (e proxyEnv) count(t *testing.T, kind string) int {
	t.Helper()

	n := 0
	for _, line := range e.events(t) {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == kind {
			n++
		}
	}

	return n
}

func (e proxyEnv) dump(t *testing.T, title string) {
	t.Helper()
	t.Logf("%s\n\n%s\n", title, strings.Join(e.events(t), "\n"))
}

// Several xcodebuild wrappers starting at once must yield exactly one proxy.
func TestIntegration_ProxyLock_OnlyOneProxyStarts(t *testing.T) {
	const starters = 6
	env := newProxyEnv(t)

	cmds := make([]*exec.Cmd, 0, starters)
	for range starters {
		cmds = append(cmds, env.start(t, proxyRoleStart, 250))
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "starter %d should either serve or skip, never fail:\n%s", i, strings.Join(env.events(t), "\n"))
	}

	env.dump(t, "six xcodebuild wrappers racing to start the proxy")

	assert.Equal(t, 1, env.count(t, "SERVING"), "exactly one proxy may serve")
	assert.Equal(t, starters-1, env.count(t, "SKIPPED"), "the rest should stand down")
	assert.Zero(t, env.count(t, "OVERLAP"), "two proxies were serving at once")
}

// What the wrapper actually runs: with a proxy serving, startProxy must return
// without spawning anything.
func TestIntegration_ProxyLock_WrapperDoesNotSpawnASecondProxy(t *testing.T) {
	env := newProxyEnv(t)
	osProxy := utils.DefaultOsProxy{}

	child := env.start(t, proxyRoleStart, 3000)
	require.Eventually(t, func() bool {
		return env.count(t, "SERVING") == 1
	}, 5*time.Second, 10*time.Millisecond, "waiting for the child to take the singleton")

	pid, running := xcelerate.ProxyOwner(osProxy)
	require.True(t, running, "a serving proxy must be visible to the wrapper")
	require.Equal(t, child.Process.Pid, pid, "and identifiable, so the log names the right process")

	spawns := 0
	spawning := func(ctx context.Context, name string, args ...string) utils.Command {
		spawns++

		return utils.DefaultCommandFunc()(ctx, name, args...)
	}

	require.NoError(t, startProxy(log.NewLogger(), osProxy, spawning, nil,
		xcelerate.ResolveProxySocketPath("", utils.AllEnvs(), osProxy)))
	assert.Zero(t, spawns, "the wrapper must not start a second proxy while one is serving")

	require.NoError(t, child.Wait())
	_, running = xcelerate.ProxyOwner(osProxy)
	assert.False(t, running, "once it stops, the singleton is free")
}

// The old pid-marker version wedged here: a stale pid file (or a recycled pid) made
// the wrapper believe a proxy was running forever, silently disabling caching.
func TestIntegration_ProxyLock_KilledProxyFreesTheSingleton(t *testing.T) {
	env := newProxyEnv(t)
	osProxy := utils.DefaultOsProxy{}

	cmd := env.start(t, proxyRoleCrash, 0)
	require.Error(t, cmd.Wait(), "the child should die from SIGKILL")
	require.Equal(t, 1, env.count(t, "CRASHING"))
	require.Zero(t, env.count(t, "STOPPED"), "a killed proxy cannot have stopped cleanly")

	env.dump(t, "a proxy killed while serving")

	require.FileExists(t, xcelerate.ProxyPidFile(osProxy), "the file stays: it carries the lock")
	assert.Equal(t, cmd.Process.Pid, advertisedPID(osProxy), "and still advertises the dead pid")

	_, running := xcelerate.ProxyOwner(osProxy)
	assert.False(t, running, "the advertised pid is stale, but the lock is free")

	served := false
	require.NoError(t, withProxySingleton(osProxy, log.NewLogger(), func() error {
		served = true

		return nil
	}))
	assert.True(t, served, "a dead proxy must not wedge the singleton — the next one should serve")
}

// Regression: deciding on the pid before the lock reported "not running" whenever
// the advertisement was mid-write, which starts a second proxy.
func TestProxyOwner_HeldWithAnUnreadablePidStillReportsRunning(t *testing.T) {
	newProxyEnv(t)
	osProxy := utils.DefaultOsProxy{}

	// Inspect from inside the critical section, so the lock is genuinely held by the
	// production path while the advertisement is unreadable.
	require.NoError(t, withProxySingleton(osProxy, log.NewLogger(), func() error {
		// What a reader sees between WriteFile's truncate and its write.
		require.NoError(t, os.WriteFile(xcelerate.ProxyPidFile(osProxy), nil, 0o644))

		pid, running := xcelerate.ProxyOwner(osProxy)
		assert.True(t, running, "the lock is held, so a proxy is serving")
		assert.Zero(t, pid, "with the identity simply unknown")

		return nil
	}))
}

// stop-proxy must not unlink the file that carries the lock: a proxy holding the
// old inode and one locking a newly created file would both be the singleton.
func TestIntegration_ProxyLock_StopLeavesTheLockFileInPlace(t *testing.T) {
	env := newProxyEnv(t)
	osProxy := utils.DefaultOsProxy{}

	// Long enough that stop acts on a live proxy — the whole point of the test.
	cmd := env.start(t, proxyRoleStart, 30_000)
	require.Eventually(t, func() bool {
		return env.count(t, "SERVING") == 1
	}, 5*time.Second, 10*time.Millisecond, "waiting for the proxy to serve")

	require.NoError(t, stopXcelerateProxyCommandFn(osProxy, log.NewLogger()))

	_, _ = cmd.Process.Wait()
	env.dump(t, "stop-proxy against a live proxy")

	assert.FileExists(t, xcelerate.ProxyPidFile(osProxy), "stop must not remove the file that carries the lock")

	free, err := flock.New(xcelerate.ProxyPidFile(osProxy)).TryLock()
	require.NoError(t, err)
	assert.True(t, free, "and the lock must be free once the proxy is gone")
}
