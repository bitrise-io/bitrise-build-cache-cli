// Package main is the Bitrise Build Cache compiler trampoline. Installed as a
// copy over io.bitrise.cas.xctoolchain/usr/bin/{swiftc,clang,swift}; probes
// the proxy socket, auto-starts it when absent, resolves the real toolchain
// binary and syscall.Execs into it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trampoline"
)

func main() {
	name := filepath.Base(os.Args[0])

	trampoline.EnsureProxy()

	realPath, err := trampoline.ResolveReal(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bitrise trampoline: resolve %s: %v\n", name, err)
		os.Exit(127)
	}

	if err := trampoline.Exec(realPath, os.Args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "bitrise trampoline: exec %s: %v\n", realPath, err)
		os.Exit(127)
	}
}
