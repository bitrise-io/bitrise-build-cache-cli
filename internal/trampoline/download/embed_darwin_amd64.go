//go:build darwin && amd64

package download

import _ "embed"

//go:embed trampoline_darwin_amd64
var embeddedTrampoline []byte
