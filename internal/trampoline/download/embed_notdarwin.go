//go:build !darwin

package download

// Mach-O binary, macOS only; non-darwin CLI never consumes it.
var embeddedTrampoline []byte
