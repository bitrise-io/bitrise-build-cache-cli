//go:build unit

package oauth

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Goroutines share a pid and a lock table, so they cannot exercise what the
// refresh lock is actually for: separate CLI processes, spawned per Bazel RPC,
// contending for one credential — including one that dies still holding it. These
// re-exec the test binary to get real processes through acquireRefreshLock.

const (
	envRole     = "REFRESHLOCK_CHILD_ROLE"
	envHome     = "REFRESHLOCK_CHILD_HOME"
	envSentinel = "REFRESHLOCK_CHILD_SENTINEL"
	envLog      = "REFRESHLOCK_CHILD_LOG"
	envHoldMS   = "REFRESHLOCK_CHILD_HOLD_MS"
	envEpoch    = "REFRESHLOCK_CHILD_EPOCH"

	roleHold  = "hold"
	roleCrash = "crash"

	exitLockFailed = 4
	exitOverlap    = 5
)

func TestMain(m *testing.M) {
	if role := os.Getenv(envRole); role != "" {
		os.Exit(runLockChild(role))
	}

	os.Exit(m.Run())
}

// runLockChild goes through the production acquireRefreshLock, proves it is the
// only holder, then either releases or dies still holding it.
func runLockChild(role string) int {
	// paths.Default resolves from HOME, so this points the child at the test's dir.
	_ = os.Setenv("HOME", os.Getenv(envHome))
	holdMS, _ := strconv.Atoi(os.Getenv(envHoldMS))

	start := time.Now()
	release, err := acquireRefreshLock(context.Background())
	if err != nil {
		appendEvent(fmt.Sprintf("BLOCKED   pid=%-6d %v", os.Getpid(), err))

		return exitLockFailed
	}
	waited := time.Since(start)

	// O_EXCL on a second file, held exactly as long as the lock: two holders at once
	// are caught directly, without trusting cross-process clock resolution.
	sentinel := os.Getenv(envSentinel)
	f, sErr := os.OpenFile(sentinel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if sErr != nil {
		appendEvent(fmt.Sprintf("OVERLAP   pid=%-6d ANOTHER PROCESS IS ALREADY REFRESHING: %v", os.Getpid(), sErr))
		_ = release()

		return exitOverlap
	}
	_ = f.Close()
	appendEvent(fmt.Sprintf("ACQUIRED  pid=%-6d waited=%s", os.Getpid(), waited.Round(time.Millisecond)))

	held := time.Now()
	time.Sleep(time.Duration(holdMS) * time.Millisecond)

	if role == roleCrash {
		// No release, no unwinding: the kernel has to drop the lock on its own.
		appendEvent(fmt.Sprintf("CRASHING  pid=%-6d SIGKILL while holding the lock", os.Getpid()))
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}

	_ = os.Remove(sentinel)
	if err := release(); err != nil {
		appendEvent(fmt.Sprintf("REL_ERR   pid=%-6d %v", os.Getpid(), err))

		return exitLockFailed
	}
	appendEvent(fmt.Sprintf("RELEASED  pid=%-6d held=%s", os.Getpid(), time.Since(held).Round(time.Millisecond)))

	return 0
}

// One short O_APPEND write per event, so lines from separate processes interleave
// without tearing and the file reads as a single timeline.
func appendEvent(line string) {
	f, err := os.OpenFile(os.Getenv(envLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	epoch, _ := strconv.ParseInt(os.Getenv(envEpoch), 10, 64)
	_, _ = f.WriteString(fmt.Sprintf("t=%-8s %s\n", time.Since(time.Unix(0, epoch)).Round(time.Millisecond), line))
}

type lockEnv struct {
	home     string
	lockFile string
	sentinel string
	log      string
	epoch    time.Time
}

func newLockEnv(t *testing.T) lockEnv {
	t.Helper()
	dir := t.TempDir()

	return lockEnv{
		home:     dir,
		lockFile: paths.FromHome(dir).AuthRefreshLockFile(),
		sentinel: filepath.Join(dir, "refreshing.sentinel"),
		log:      filepath.Join(dir, "events.log"),
		epoch:    time.Now(),
	}
}

func (e lockEnv) start(t *testing.T, role string, holdMS int) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(),
		envRole+"="+role,
		envHome+"="+e.home,
		envSentinel+"="+e.sentinel,
		envLog+"="+e.log,
		envHoldMS+"="+strconv.Itoa(holdMS),
		envEpoch+"="+strconv.FormatInt(e.epoch.UnixNano(), 10),
	)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	require.NoError(t, cmd.Start())

	return cmd
}

func (e lockEnv) events(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(e.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (e lockEnv) count(t *testing.T, kind string) int {
	t.Helper()

	n := 0
	for _, line := range e.events(t) {
		// "t=<elapsed> <KIND> pid=…", so the kind is the second field.
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == kind {
			n++
		}
	}

	return n
}

func (e lockEnv) dump(t *testing.T, title string) {
	t.Helper()
	t.Logf("%s — %s\n\n%s\n", title, e.lockFile, strings.Join(e.events(t), "\n"))
}

// What a Bazel build does: N helpers, one credential, and only one of them may be
// spending the refresh token at a time.
func TestIntegration_RefreshLock_ParallelHelpersTakeTurns(t *testing.T) {
	const helpers = 8
	env := newLockEnv(t)

	cmds := make([]*exec.Cmd, 0, helpers)
	for range helpers {
		cmds = append(cmds, env.start(t, roleHold, 30))
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "helper %d should acquire, hold and release:\n%s", i, strings.Join(env.events(t), "\n"))
	}

	env.dump(t, "eight credential helpers contending for the refresh lock")

	assert.Equal(t, helpers, env.count(t, "ACQUIRED"), "every helper should get its turn")
	assert.Equal(t, helpers, env.count(t, "RELEASED"))
	assert.Zero(t, env.count(t, "OVERLAP"), "two processes were refreshing at once")
	assert.Zero(t, env.count(t, "BLOCKED"), "nobody should have to give up within the wait budget")
}

// The reason for moving to a kernel lock: a helper killed mid-refresh leaves
// nothing to detect or break open.
func TestIntegration_RefreshLock_KilledHolderReleasesImmediately(t *testing.T) {
	env := newLockEnv(t)

	cmd := env.start(t, roleCrash, 0)
	require.Error(t, cmd.Wait(), "the child should die from SIGKILL")
	require.Equal(t, 1, env.count(t, "CRASHING"))
	require.Zero(t, env.count(t, "RELEASED"), "a killed holder cannot have released")

	env.dump(t, "a helper killed while holding the lock")

	require.FileExists(t, env.lockFile, "the lock file stays: unlinking it would let two processes hold it")

	t.Setenv("HOME", env.home)
	start := time.Now()
	release, err := acquireRefreshLock(t.Context())

	require.NoError(t, err, "the kernel should have dropped the dead holder's lock")
	assert.Less(t, time.Since(start), 500*time.Millisecond, "no marker to diagnose, so no waiting")
	require.NoError(t, release())
}

// A live holder in another process keeps it, and the waiter gives up inside its
// budget rather than hanging or stealing.
func TestIntegration_RefreshLock_LiveHolderMakesTheWaiterGiveUp(t *testing.T) {
	env := newLockEnv(t)

	cmd := env.start(t, roleHold, 3000)
	require.Eventually(t, func() bool {
		return env.count(t, "ACQUIRED") == 1
	}, 5*time.Second, 10*time.Millisecond, "waiting for the child to take the lock")

	t.Setenv("HOME", env.home)
	original := refreshLockWait
	refreshLockWait = 300 * time.Millisecond
	defer func() { refreshLockWait = original }()

	start := time.Now()
	_, err := acquireRefreshLock(t.Context())

	require.Error(t, err, "a live holder in another process keeps the lock")
	assert.WithinDuration(t, start.Add(refreshLockWait), time.Now(), 2*time.Second, "it should give up on its budget, not hang")

	require.NoError(t, cmd.Wait())
	release, err := acquireRefreshLock(t.Context())
	require.NoError(t, err, "free once the holder is done")
	require.NoError(t, release())
}
