package dependencies

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
)

const (
	cliBinaryName = "bitrise-build-cache"
	cliModulePath = "github.com/bitrise-io/bitrise-build-cache-cli/v2"
	// preferredInstallDir is the system-wide location used when the current
	// user can write to it (Bitrise macOS stacks own it and it is already on
	// the default PATH).
	preferredInstallDir = "/usr/local/bin"
)

// InstallDir returns the directory where dependency binaries (CLI, ccache) are
// installed and onto which the React Native activator extends PATH.
//
// It prefers preferredInstallDir (/usr/local/bin) when that directory is
// writable by the current user — the case on Bitrise macOS stacks. When it is
// not writable it falls back to the per-user ~/.bitrise/bin, which is always
// writable. The Linux 2026 stack runs builds as a non-root user that cannot
// write to /usr/local/bin, so without this fallback the self-install fails
// with "permission denied". The fallback is the same stable bin dir the
// daemon installer uses.
func InstallDir() string {
	if dirIsWritable(preferredInstallDir) {
		return preferredInstallDir
	}

	p, err := paths.Default()
	if err != nil {
		// No home dir to fall back to — keep the preferred default and let
		// the eventual write surface a clear permission error.
		return preferredInstallDir
	}

	return p.BitriseBinDir()
}

// dirIsWritable reports whether the current user can create files in dir.
// A non-existent or read-only dir reports false; callers MkdirAll their
// chosen directory before writing.
func dirIsWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".bitrise-build-cache-write-check-*")
	if err != nil {
		return false
	}

	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)

	return true
}

// CLITool returns a Tool that installs the bitrise-build-cache binary
// matching the version embedded in the current binary's module dependencies.
// When the current process IS the CLI binary (e.g. dev builds via `go run`),
// it self-installs by copying the running executable to InstallDir.
func CLITool() (Tool, error) {
	version, err := cliVersion()
	if err != nil {
		return Tool{}, fmt.Errorf("determine CLI version: %w", err)
	}

	install := func(ctx context.Context, logger log.Logger) error {
		return installFromMirrors(
			ctx, logger,
			[]string{
				cliGitHubDownloadURL(version, runtime.GOOS, runtime.GOARCH),
				cliGARDownloadURL(version, runtime.GOOS, runtime.GOARCH),
			},
			cliBinaryName,
		)
	}

	if isMainCLIBinary() {
		install = func(_ context.Context, logger log.Logger) error {
			return selfInstall(logger)
		}
	}

	return Tool{
		Name:    cliBinaryName,
		Version: version,
		Install: install,
	}, nil
}

// isMainCLIBinary reports whether the current process IS the CLI binary
// (as opposed to a step binary that embeds it as a dependency).
func isMainCLIBinary() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}

	return info.Main.Path == cliModulePath
}

// selfInstall copies the running executable into InstallDir.
// Used when the CLI is already running but not on PATH (e.g. `go run` dev builds).
func selfInstall(logger log.Logger) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable symlinks: %w", err)
	}

	installDir := InstallDir()
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("create install dir %s: %w", installDir, err)
	}

	destPath := filepath.Join(installDir, cliBinaryName)
	logger.Debugf("Self-installing: copying %s to %s", exePath, destPath)

	src, err := os.Open(exePath)
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(destPath)

		return fmt.Errorf("copy executable: %w", err)
	}

	if err := os.Chmod(destPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	return nil
}

func cliVersion() (string, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", fmt.Errorf("failed to read build info")
	}

	if info.Main.Path == cliModulePath {
		// We ARE the CLI binary. Use the embedded version if available,
		// otherwise return "" — the caller will see IsInstalled() == true
		// and skip installation.
		return strings.TrimPrefix(info.Main.Version, "v"), nil
	}

	// When embedded as a dependency (e.g. in a step binary), find it in Deps.
	for _, dep := range info.Deps {
		if dep.Path == cliModulePath {
			return strings.TrimPrefix(dep.Version, "v"), nil
		}
	}

	return "", fmt.Errorf("module %s not found in build info (main=%s, version=%s)", cliModulePath, info.Main.Path, info.Main.Version)
}

func cliGitHubDownloadURL(version, goos, goarch string) string {
	return fmt.Sprintf(
		"https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/v%s/%s_%s_%s_%s.tar.gz",
		version, cliBinaryName, version, goos, goarch,
	)
}

func cliGARDownloadURL(version, goos, goarch string) string {
	pkg := fmt.Sprintf("%s_%s_%s.tar.gz", cliBinaryName, goos, goarch)
	filename := fmt.Sprintf("%s_%s_%s_%s.tar.gz", cliBinaryName, version, goos, goarch)

	return garDownloadURL(pkg, version, filename)
}

// installFromMirrors tries each URL in order; returns once one succeeds.
// On total failure the joined error chain is returned.
func installFromMirrors(ctx context.Context, logger log.Logger, urls []string, binaryName string) error {
	if len(urls) == 0 {
		return fmt.Errorf("no download URLs provided for %s", binaryName)
	}

	errs := make([]error, 0, len(urls))

	for i, url := range urls {
		logger.Debugf("Trying mirror %d/%d: %s", i+1, len(urls), url)

		err := downloadAndExtract(ctx, url, binaryName)
		if err == nil {
			if i > 0 {
				logger.Infof("Installed %s from fallback mirror %d/%d", binaryName, i+1, len(urls))
			}

			return nil
		}

		logger.Debugf("Mirror %d/%d failed: %v", i+1, len(urls), err)
		errs = append(errs, fmt.Errorf("mirror %d (%s): %w", i+1, url, err))
	}

	return fmt.Errorf("all %d mirrors failed for %s: %w", len(urls), binaryName, errors.Join(errs...))
}

func downloadAndExtract(ctx context.Context, url, binaryName string) error {
	resp, err := downloadFile(ctx, url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Close()

	installDir := InstallDir()
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("create install dir %s: %w", installDir, err)
	}

	destPath := filepath.Join(installDir, binaryName)
	if err := extractBinaryFromTarGz(resp, binaryName, destPath); err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	if err := os.Chmod(destPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	return nil
}

func extractBinaryFromTarGz(r io.Reader, name, destPath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary %s not found in archive", name)
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		if filepath.Base(header.Name) == name && header.Typeflag == tar.TypeReg {
			f, err := os.Create(destPath)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			defer f.Close()

			if _, err := io.Copy(f, io.LimitReader(tr, header.Size)); err != nil {
				os.Remove(destPath)

				return fmt.Errorf("write binary: %w", err)
			}

			return nil
		}
	}
}
