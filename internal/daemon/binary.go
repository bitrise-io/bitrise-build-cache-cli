package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
)

const stableBinName = "bitrise-build-cache"

// StableBinPath returns ~/.bitrise/bin/bitrise-build-cache.
func StableBinPath() (string, error) {
	p, err := paths.Default()
	if err != nil {
		return "", fmt.Errorf("resolve stable bin path: %w", err)
	}

	return p.BitriseBinFile(stableBinName), nil
}

// CopyCLIToStableBin copies src to StableBinPath() with 0o755 perms,
// creating the parent directory if needed. Returns the destination path.
func CopyCLIToStableBin(src string) (string, error) {
	dst, err := StableBinPath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("create stable bin dir: %w", err)
	}

	in, err := os.Open(src) //nolint:gosec // src is the running CLI's own executable path
	if err != nil {
		return "", fmt.Errorf("open source binary %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // executable must be runnable
	if err != nil {
		return "", fmt.Errorf("open destination %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)

		return "", fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(dst)

		return "", fmt.Errorf("close destination %s: %w", dst, err)
	}

	return dst, nil
}
