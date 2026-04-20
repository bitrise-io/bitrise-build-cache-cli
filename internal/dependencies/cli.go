package dependencies

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	cliBinaryName = "bitrise-build-cache"
	cliModulePath = "github.com/bitrise-io/bitrise-build-cache-cli/v2"
	// InstallDir is the directory where dependency binaries (CLI, ccache) are installed.
	InstallDir = "/usr/local/bin"
)

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
		return installFromGitHubRelease(
			ctx, logger,
			downloadURL(version, runtime.GOOS, runtime.GOARCH),
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

// selfInstall copies the running executable to InstallDir.
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

	destPath := filepath.Join(InstallDir, cliBinaryName)
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

func downloadURL(version, goos, goarch string) string {
	return fmt.Sprintf(
		"https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/v%s/%s_%s_%s_%s.tar.gz",
		version, cliBinaryName, version, goos, goarch,
	)
}

func installFromGitHubRelease(ctx context.Context, logger log.Logger, url, binaryName string) error {
	logger.Debugf("Downloading from %s", url)

	resp, err := downloadFile(ctx, url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Close()

	destPath := filepath.Join(InstallDir, binaryName)
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
