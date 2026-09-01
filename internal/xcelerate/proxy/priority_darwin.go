//go:build darwin

package proxy

import (
	"golang.org/x/sys/unix"
)

// PRIO_DARWIN_PROCESS applies to every thread in the process; level 0 clears
// the background designation that throttles CPU and I/O. A launchd job can be
// placed in that band without asking, which is what makes the supervised proxy
// serve cache operations far slower than a wrapper-forked one.
const prioDarwinProcess = 4

// ClearBackgroundPriority takes the process out of Darwin's background band.
// Best-effort: a process that was never in it returns an error, which is not a
// failure.
func ClearBackgroundPriority() error {
	//nolint:wrapcheck // the caller logs it and carries on
	return unix.Setpriority(prioDarwinProcess, 0, 0)
}
