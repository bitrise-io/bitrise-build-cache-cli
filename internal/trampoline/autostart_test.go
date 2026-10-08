//go:build unit

package trampoline

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func baseAutostartDeps() autostartDeps {
	now := time.Unix(1_700_000_000, 0)

	return autostartDeps{
		socketPath:    func() string { return "/tmp/xcelerate-proxy.sock" },
		lockPath:      func() string { return "/tmp/trampoline-lock" },
		dial:          func(string, time.Duration) error { return errors.New("no proxy") },
		tryLock:       func(string) (func(), bool, error) { return func() {}, true, nil },
		startAutostep: func(string) {},
		sleep:         func(time.Duration) {},
		now:           func() time.Time { return now },
	}
}

func TestEnsureProxy_SocketHitReturnsImmediately(t *testing.T) {
	var startCalls, lockCalls int32
	d := baseAutostartDeps()
	d.dial = func(string, time.Duration) error { return nil }
	d.tryLock = func(string) (func(), bool, error) {
		atomic.AddInt32(&lockCalls, 1)

		return func() {}, true, nil
	}
	d.startAutostep = func(string) { atomic.AddInt32(&startCalls, 1) }

	ensureProxy("swiftc", d)

	assert.Zero(t, atomic.LoadInt32(&lockCalls))
	assert.Zero(t, atomic.LoadInt32(&startCalls))
}

func TestEnsureProxy_StartsWhenSocketMissing(t *testing.T) {
	var startCalls int32
	d := baseAutostartDeps()
	d.startAutostep = func(string) { atomic.AddInt32(&startCalls, 1) }

	ensureProxy("swiftc", d)
	assert.Equal(t, int32(1), atomic.LoadInt32(&startCalls))
}

func TestEnsureProxy_LockBusySleepsAndReprobes(t *testing.T) {
	var startCalls, sleepCalls int32
	tick := time.Unix(1_700_000_000, 0)
	d := baseAutostartDeps()
	d.now = func() time.Time { return tick }
	d.tryLock = func(string) (func(), bool, error) { return nil, false, nil }
	d.dial = func(string, time.Duration) error { return errors.New("no proxy") }
	d.sleep = func(time.Duration) {
		atomic.AddInt32(&sleepCalls, 1)
		tick = tick.Add(startLockRetry)
	}
	d.startAutostep = func(string) { atomic.AddInt32(&startCalls, 1) }

	ensureProxy("swiftc", d)
	assert.Zero(t, atomic.LoadInt32(&startCalls), "lock-busy must not fire autostart")
	assert.Positive(t, atomic.LoadInt32(&sleepCalls), "lock-busy must sleep-and-reprobe at least once")
}

func TestEnsureProxy_LockBusyThenSocketReturns(t *testing.T) {
	var startCalls int32
	tick := time.Unix(1_700_000_000, 0)
	dialErr := errors.New("no proxy")
	d := baseAutostartDeps()
	d.now = func() time.Time { return tick }
	d.tryLock = func(string) (func(), bool, error) { return nil, false, nil }
	d.dial = func(string, time.Duration) error { return dialErr }
	d.sleep = func(time.Duration) {
		tick = tick.Add(startLockRetry)
		dialErr = nil // simulate the other process got the socket up
	}
	d.startAutostep = func(string) { atomic.AddInt32(&startCalls, 1) }

	ensureProxy("swiftc", d)
	assert.Zero(t, atomic.LoadInt32(&startCalls), "a successful re-probe must not fire autostart")
}
