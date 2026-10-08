//go:build darwin

package trampoline

import "syscall"

// Exec replaces the current process with realPath using syscall.Exec; never
// returns on success. Darwin-only build tag because trampoline is macOS-only.
func Exec(realPath string, argv []string, env []string) error {
	return syscall.Exec(realPath, argv, env) //nolint:wrapcheck // caller logs opaque failure
}
