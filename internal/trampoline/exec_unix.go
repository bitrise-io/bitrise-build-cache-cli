//go:build darwin

package trampoline

import "syscall"

// Exec replaces the current process with real using syscall.Exec; never
// returns on success. Darwin-only build tag because trampoline is macOS-only.
func Exec(real string, argv []string, env []string) error {
	return syscall.Exec(real, argv, env) //nolint:wrapcheck // caller logs opaque failure
}
