//go:build darwin && arm64

package download

import _ "embed"

//go:embed trampoline_darwin_arm64
var embeddedTrampoline []byte
