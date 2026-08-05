//go:build unit

package filelock

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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// The goroutine tests all share one pid, so they cannot exercise what actually
// matters across processes: a foreign pid probed with a real signal 0, and a
// holder that dies without releasing. These re-exec the test binary to get
// genuinely separate processes contending for one marker.

const (
	envRole     = "FILELOCK_CHILD_ROLE"
	envLockPath = "FILELOCK_CHILD_LOCK"
	envSentinel = "FILELOCK_CHILD_SENTINEL"
	envLog      = "FILELOCK_CHILD_LOG"
	envHoldMS   = "FILELOCK_CHILD_HOLD_MS"

	roleHold  = "hold"
	roleCrash = "crash"

	exitLockFailed = 4
	exitOverlap    = 5
)

func TestMain(m *testing.M) {
	if role := os.Getenv(envRole); role != "" {
		os.Exit(runChild(role))
	}

	os.Exit(m.Run())
}

// runChild takes the lock, proves it is the only holder, then either releases or
// dies still holding it.
func runChild(role string) int {
	lock := os.Getenv(envLockPath)
	holdMS, _ := strconv.Atoi(os.Getenv(envHoldMS))

	release, err := Acquire(context.Background(), lock, Options{Wait: 30 * time.Second, TTL: 30 * time.Second})
	if err != nil {
		appendLine(fmt.Sprintf("FAILED %d %v", os.Getpid(), err))

		return exitLockFailed
	}

	// O_EXCL on a second file, held for exactly as long as the lock: if another
	// process is in here at the same time this create fails. It asserts exclusion
	// directly, without trusting cross-process clock resolution.
	sentinel := os.Getenv(envSentinel)
	f, sErr := os.OpenFile(sentinel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if sErr != nil {
		appendLine(fmt.Sprintf("OVERLAP %d %v", os.Getpid(), sErr))
		_ = release()

		return exitOverlap
	}
	_ = f.Close()
	appendLine(fmt.Sprintf("ACQUIRED %d", os.Getpid()))

	time.Sleep(time.Duration(holdMS) * time.Millisecond)

	if role == roleCrash {
		// Killed while holding: no release, no deferred cleanup, marker left behind.
		appendLine(fmt.Sprintf("CRASHING %d", os.Getpid()))
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}

	_ = os.Remove(sentinel)
	appendLine(fmt.Sprintf("RELEASED %d", os.Getpid()))
	if err := release(); err != nil {
		appendLine(fmt.Sprintf("RELEASE_ERR %d %v", os.Getpid(), err))

		return exitLockFailed
	}

	return 0
}

func appendLine(line string) {
	f, err := os.OpenFile(os.Getenv(envLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

type childEnv struct {
	lock     string
	sentinel string
	log      string
}

func newChildEnv(t *testing.T) childEnv {
	t.Helper()
	dir := t.TempDir()

	return childEnv{
		lock:     filepath.Join(dir, "contended.lock"),
		sentinel: filepath.Join(dir, "holder.sentinel"),
		log:      filepath.Join(dir, "events.log"),
	}
}

func (e childEnv) start(t *testing.T, role string, holdMS int) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(),
		envRole+"="+role,
		envLockPath+"="+e.lock,
		envSentinel+"="+e.sentinel,
		envLog+"="+e.log,
		envHoldMS+"="+strconv.Itoa(holdMS),
	)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	require.NoError(t, cmd.Start())

	return cmd
}

func (e childEnv) events(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(e.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (e childEnv) countEvents(t *testing.T, kind string) int {
	t.Helper()

	n := 0
	for _, line := range e.events(t) {
		if strings.HasPrefix(line, kind) {
			n++
		}
	}

	return n
}

// The headline case: N real processes, one marker, and every one of them must get
// an exclusive turn.
func TestIntegration_ManyProcessesTakeTurns(t *testing.T) {
	const children = 10
	env := newChildEnv(t)

	cmds := make([]*exec.Cmd, 0, children)
	for range children {
		cmds = append(cmds, env.start(t, roleHold, 30))
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "child %d should acquire, hold and release cleanly:\n%s", i, strings.Join(env.events(t), "\n"))
	}

	assert.Equal(t, children, env.countEvents(t, "ACQUIRED"), "every process should get the lock")
	assert.Equal(t, children, env.countEvents(t, "RELEASED"))
	assert.Zero(t, env.countEvents(t, "OVERLAP"), "two processes were inside the critical section at once")
	assert.Zero(t, env.countEvents(t, "FAILED"))

	assert.NoFileExists(t, env.lock, "the last holder should leave nothing behind")
	assertNoGuards(t, env.lock)
}

// A holder SIGKILLed mid-hold leaves a marker owned by a pid that is genuinely
// gone — the one situation a same-process test cannot produce.
func TestIntegration_MarkerOfACrashedHolderIsBrokenOpen(t *testing.T) {
	env := newChildEnv(t)

	cmd := env.start(t, roleCrash, 0)
	err := cmd.Wait()
	require.Error(t, err, "the child should die from SIGKILL, not exit cleanly")
	require.Equal(t, 1, env.countEvents(t, "CRASHING"))
	require.Zero(t, env.countEvents(t, "RELEASED"), "a crashed holder must not have released")

	deadPID := cmd.Process.Pid
	pid, alive := ReadOwner(utils.DefaultOsProxy{}, env.lock)
	require.Equal(t, deadPID, pid, "the marker should still name the crashed process")
	require.False(t, alive, "and a real signal 0 should report it gone")

	// TTL deliberately long: this must be liveness breaking it open, not age.
	start := time.Now()
	release, err := Acquire(t.Context(), env.lock, Options{Wait: 2 * time.Second, TTL: time.Hour})

	require.NoError(t, err, "a dead holder's marker must not wedge the lock")
	assert.Less(t, time.Since(start), time.Second, "liveness should break it open without waiting out the TTL")
	require.NoError(t, release())
	assertNoGuards(t, env.lock)
}

// The mirror image: a holder that is still running keeps the lock, however old
// its marker gets.
func TestIntegration_LiveHolderInAnotherProcessKeepsTheLock(t *testing.T) {
	env := newChildEnv(t)

	cmd := env.start(t, roleHold, 3000)
	require.Eventually(t, func() bool {
		pid, alive := ReadOwner(utils.DefaultOsProxy{}, env.lock)

		return pid == cmd.Process.Pid && alive
	}, 5*time.Second, 10*time.Millisecond, "waiting for the child to take the lock")

	// Backdate the marker past any TTL, leaving liveness as the only thing that can
	// keep it held.
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(env.lock, old, old))

	_, err := Acquire(t.Context(), env.lock, Options{Wait: 200 * time.Millisecond, TTL: time.Nanosecond})
	require.ErrorIs(t, err, ErrHeld, "a live holder in another process keeps the lock")
	assert.Contains(t, err.Error(), strconv.Itoa(cmd.Process.Pid), "the error should name the holder")

	require.NoError(t, cmd.Wait())
	release, err := Acquire(t.Context(), env.lock, Options{Wait: 2 * time.Second})
	require.NoError(t, err, "once the holder is done the lock is free")
	require.NoError(t, release())
}

// A process that dies holding the marker must not be able to release a successor's.
func TestIntegration_ReleaseAfterAnotherProcessTookOverIsRefused(t *testing.T) {
	env := newChildEnv(t)

	release, err := Acquire(t.Context(), env.lock, Options{})
	require.NoError(t, err)

	// Stand in for a successor: our marker is replaced by a live foreign owner.
	cmd := env.start(t, roleHold, 2000)
	require.NoError(t, os.WriteFile(env.lock, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600))

	require.Error(t, release(), "the marker is no longer ours to remove")
	pid, _ := ReadOwner(utils.DefaultOsProxy{}, env.lock)
	assert.Equal(t, cmd.Process.Pid, pid, "the successor keeps its marker")

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func assertNoGuards(t *testing.T, lock string) {
	t.Helper()

	assert.NoFileExists(t, lock+stealGuardSuffix, "a steal guard outlived its steal")
	leftovers, err := filepath.Glob(lock + ".*")
	require.NoError(t, err)
	assert.Empty(t, leftovers, "the lock directory should be left clean")
}
