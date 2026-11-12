// Package download materialises the trampoline binary into
// ~/.bitrise-xcelerate/toolchain/ from the //go:embed bytes baked into the
// CLI at release build time. Honours BITRISE_XCELERATE_TRAMPOLINE_PATH for
// dev-loop overrides.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// EnvOverride lets dev loops point at a local trampoline build instead of the
// embedded bytes.
const EnvOverride = "BITRISE_XCELERATE_TRAMPOLINE_PATH"

// Deps bundles the OS seams; tests swap atoms.
type Deps struct {
	Env      func(key string) string
	MkdirAll func(path string, perm os.FileMode) error
	Stat     func(path string) (os.FileInfo, error)
}

// Default returns a Deps bundle wired to the real filesystem.
func Default() Deps {
	return Deps{
		Env:      os.Getenv,
		MkdirAll: os.MkdirAll,
		Stat:     os.Stat,
	}
}

// EnsureForVersion returns a path to a trampoline binary for the given CLI
// version. The CLI carries the matching per-arch trampoline inline via
// //go:embed; installEmbedded rewrites the cache from those bytes.
// BITRISE_XCELERATE_TRAMPOLINE_PATH wins when set and points at a local
// build. A dev build whose embed is empty and whose override is unset must
// run scripts/build_trampoline_embed.sh before activating.
func EnsureForVersion(_ context.Context, cliVersion string, p paths.Paths) (string, error) {
	return ensure(cliVersion, p, Default())
}

func ensure(cliVersion string, p paths.Paths, d Deps) (string, error) {
	if override := strings.TrimSpace(d.Env(EnvOverride)); override != "" {
		if _, err := d.Stat(override); err != nil {
			return "", fmt.Errorf("trampoline override %q: %w", override, err)
		}

		return override, nil
	}

	cliVersion = strings.TrimPrefix(strings.TrimSpace(cliVersion), "v")
	if cliVersion == "" {
		return "", errors.New("CLI version is empty")
	}

	if len(embeddedTrampoline) == 0 {
		return "", errors.New("trampoline not embedded in this build; run scripts/build_trampoline_embed.sh or set BITRISE_XCELERATE_TRAMPOLINE_PATH")
	}

	target := p.TrampolineBinary(cliVersion)
	if err := d.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("mkdir trampoline cache dir: %w", err)
	}

	return installEmbedded(target, d)
}

func installEmbedded(target string, d Deps) (string, error) {
	wantSum := sha256.Sum256(embeddedTrampoline)
	wantHex := hex.EncodeToString(wantSum[:])

	existing, statErr := d.Stat(target)
	if statErr == nil && !existing.IsDir() && existing.Size() == int64(len(embeddedTrampoline)) {
		if got, err := fileSHA256(target); err == nil && got == wantHex {
			return target, nil
		}
	}

	if err := writeAtomic(target, embeddedTrampoline); err != nil {
		return "", fmt.Errorf("install embedded trampoline: %w", err)
	}

	return target, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from paths.TrampolineBinary
	if err != nil {
		return "", err //nolint:wrapcheck
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err //nolint:wrapcheck
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeAtomic(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil { //nolint:gosec // trampoline must be executable
		return err //nolint:wrapcheck
	}

	return os.Rename(tmp, path) //nolint:wrapcheck
}
