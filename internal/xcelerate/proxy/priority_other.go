//go:build !darwin

package proxy

// ClearBackgroundPriority is a no-op off Darwin: the background band it clears
// is a macOS concept.
func ClearBackgroundPriority() error { return nil }
