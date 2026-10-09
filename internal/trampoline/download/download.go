// Package download fetches the universal trampoline binary from the R2 release
// mirror and caches it under ~/.bitrise-xcelerate/toolchain/. Honours
// BITRISE_XCELERATE_TRAMPOLINE_PATH for dev-loop overrides.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// EnvOverride lets dev loops point at a local trampoline build without going
// through R2.
const EnvOverride = "BITRISE_XCELERATE_TRAMPOLINE_PATH"

// R2BaseURL is the public read URL of the release bucket.
const R2BaseURL = "https://pub-a484c7653eeba8c8c00a4bf3967860a3.r2.dev/build-cache-cli-releases"

// httpTimeout caps the trampoline download so a hung R2 can't block activation.
const httpTimeout = 30 * time.Second

// Deps bundles the OS/network seams; tests swap atoms.
type Deps struct {
	Get      func(ctx context.Context, url string) ([]byte, error)
	Now      func() time.Time
	Env      func(key string) string
	MkdirAll func(path string, perm os.FileMode) error
	Stat     func(path string) (os.FileInfo, error)
}

// Default returns a Deps bundle wired to the real network + filesystem.
func Default() Deps {
	return Deps{
		Get:      httpGet,
		Now:      time.Now,
		Env:      os.Getenv,
		MkdirAll: os.MkdirAll,
		Stat:     os.Stat,
	}
}

// EnsureForVersion returns a path to a trampoline binary for the given CLI
// version. Resolution order:
//  1. BITRISE_XCELERATE_TRAMPOLINE_PATH (if set + executable).
//  2. ~/.bitrise-xcelerate/toolchain/trampoline-<version>-universal when its
//     sha matches the published checksum.
//  3. Download trampoline + checksums from R2, verify, atomic-write, return.
func EnsureForVersion(ctx context.Context, cliVersion string, p paths.Paths) (string, error) {
	return ensure(ctx, cliVersion, p, Default())
}

func ensure(ctx context.Context, cliVersion string, p paths.Paths, d Deps) (string, error) {
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

	target := p.TrampolineBinary(cliVersion)
	if err := d.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("mkdir trampoline cache dir: %w", err)
	}

	published, err := fetchChecksums(ctx, d, cliVersion)
	if err != nil {
		return "", fmt.Errorf("fetch trampoline checksums for v%s: %w", cliVersion, err)
	}
	expectedSHA, ok := published[universalFilename(cliVersion)]
	if !ok {
		return "", fmt.Errorf("checksums file lists no entry for trampoline v%s", cliVersion)
	}

	if existing, ok := d.Stat(target); ok == nil && !existing.IsDir() {
		got, err := fileSHA256(target)
		if err == nil && got == expectedSHA {
			return target, nil
		}
	}

	body, err := d.Get(ctx, binaryURL(cliVersion))
	if err != nil {
		return "", fmt.Errorf("download trampoline: %w", err)
	}

	gotSum := sha256.Sum256(body)
	gotHex := hex.EncodeToString(gotSum[:])
	if gotHex != expectedSHA {
		return "", fmt.Errorf("trampoline sha mismatch: got %s want %s", gotHex, expectedSHA)
	}

	if err := writeAtomic(target, body); err != nil {
		return "", fmt.Errorf("install trampoline: %w", err)
	}

	return target, nil
}

func fetchChecksums(ctx context.Context, d Deps, cliVersion string) (map[string]string, error) {
	body, err := d.Get(ctx, checksumsURL(cliVersion))
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, 3) //nolint:mnd // 3 trampoline binaries per release
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 { //nolint:mnd // "sha  filename" is 2 fields
			continue
		}

		out[parts[1]] = parts[0]
	}

	if len(out) == 0 {
		return nil, errors.New("empty or malformed checksums body")
	}

	return out, nil
}

func binaryURL(cliVersion string) string {
	return R2BaseURL + "/" + universalFilename(cliVersion)
}

func checksumsURL(cliVersion string) string {
	return R2BaseURL + "/trampoline_v" + cliVersion + "_checksums.txt"
}

func universalFilename(cliVersion string) string {
	return "trampoline_v" + cliVersion + "_darwin_universal"
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from paths.TrampolineBinary
	if err != nil {
		return "", err //nolint:wrapcheck // caller forces a re-download on any failure
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

func httpGet(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("new request %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 { //nolint:mnd // 2xx
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	return body, nil
}
