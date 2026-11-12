//go:build !darwin

package download

// embeddedTrampoline is intentionally empty on non-darwin builds. The
// trampoline is a Mach-O binary that only runs on macOS; Linux / Windows
// CLI builds never consume it, so bundling it would just bloat the binary.
var embeddedTrampoline []byte
