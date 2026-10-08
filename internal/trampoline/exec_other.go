//go:build !darwin

package trampoline

import "errors"

// Exec is a build-tag stub so non-darwin test builds compile. The trampoline
// is never shipped off macOS.
func Exec(_ string, _ []string, _ []string) error {
	return errors.New("trampoline exec not supported on this platform")
}
