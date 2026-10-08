// Package trampoline implements the compiler shim installed over
// io.bitrise.cas.xctoolchain/usr/bin/{swiftc,clang,swift}. Kept lean: no
// imports from pkg/* or other internal/xcelerate/* subpackages so the shipped
// trampoline binary stays small and starts fast.
package trampoline

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// toolchainBundleID must match paths.XcodeToolchainBundleID; duplicated here so
// trampoline/ does not need to import internal/paths.
const toolchainBundleID = "io.bitrise.cas.xctoolchain"

const (
	defaultDeveloperDir = "/Applications/Xcode.app/Contents/Developer"
	xcrunPath           = "/usr/bin/xcrun"
	xcodeSelectPath     = "/usr/bin/xcode-select"
)

// cacheDirEnv overrides the directory where xcrun fallback results get cached.
// Honoured only in tests; defaults to /tmp.
const cacheDirEnv = "BITRISE_TRAMPOLINE_CACHE_DIR"

// resolveDeps is the set of OS seams the resolver depends on; swapped by tests.
type resolveDeps struct {
	developerDir   func() string
	stat           func(string) (os.FileInfo, error)
	xcrun          func(name string) (string, error)
	xcodeSelectMTS func() (time.Time, bool)
	readFile       func(path string) ([]byte, error)
	writeFile      func(path string, data []byte) error
	cacheDir       func() string
	now            func() time.Time
}

//nolint:gochecknoglobals // production dependency bundle; tests swap atoms.
var defaultDeps = resolveDeps{
	developerDir: func() string {
		if v := os.Getenv("DEVELOPER_DIR"); v != "" {
			return v
		}

		return defaultDeveloperDir
	},
	stat: os.Stat,
	xcrun: func(name string) (string, error) {
		//nolint:gosec // name is argv[0] basename, within {swiftc,clang,swift}.
		out, err := exec.Command(xcrunPath, "-f", name).Output()
		if err != nil {
			return "", fmt.Errorf("xcrun -f %s: %w", name, err)
		}

		return strings.TrimSpace(string(out)), nil
	},
	xcodeSelectMTS: func() (time.Time, bool) {
		//nolint:gosec // fixed path.
		out, err := exec.Command(xcodeSelectPath, "-p").Output()
		if err != nil {
			return time.Time{}, false
		}
		dir := strings.TrimSpace(string(out))
		fi, err := os.Stat(dir)
		if err != nil {
			return time.Time{}, false
		}

		return fi.ModTime(), true
	},
	readFile:  os.ReadFile,
	writeFile: writeFileAtomic,
	cacheDir: func() string {
		if v := os.Getenv(cacheDirEnv); v != "" {
			return v
		}

		return "/tmp"
	},
	now: time.Now,
}

// ResolveReal returns the real toolchain binary for the given shim basename.
// Primary path: $DEVELOPER_DIR/Toolchains/XcodeDefault.xctoolchain/usr/bin/<name>.
// Fallback: `xcrun -f <name>`, cached under /tmp with mtime-based invalidation.
// Hard-pins against self-reference: a resolved path inside the CAS toolchain
// bundle returns an error so the trampoline cannot re-enter itself.
func ResolveReal(name string) (string, error) {
	return resolveReal(name, defaultDeps)
}

func resolveReal(name string, d resolveDeps) (string, error) {
	primary := filepath.Join(d.developerDir(), "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin", name)
	if isExecutable(primary, d.stat) && !isSelfReference(primary) {
		return primary, nil
	}

	if cached, ok := readCache(name, d); ok && isExecutable(cached, d.stat) && !isSelfReference(cached) {
		return cached, nil
	}

	resolved, err := d.xcrun(name)
	if err != nil {
		return "", err
	}
	if resolved == "" {
		return "", errors.New("xcrun returned empty path")
	}
	if isSelfReference(resolved) {
		return "", fmt.Errorf("refusing self-reference resolved=%s", resolved)
	}

	writeCache(name, resolved, d)

	return resolved, nil
}

// isSelfReference returns true if path lives inside the CAS toolchain bundle,
// which would cause the trampoline to re-exec itself.
func isSelfReference(path string) bool {
	return strings.Contains(path, toolchainBundleID)
}

func isExecutable(path string, stat func(string) (os.FileInfo, error)) bool {
	fi, err := stat(path)
	if err != nil {
		return false
	}

	return fi.Mode().IsRegular() && (fi.Mode().Perm()&0o111) != 0
}

type cacheRecord struct {
	resolvedPath string
	devDirMTime  time.Time
}

func readCache(name string, d resolveDeps) (string, bool) {
	body, err := d.readFile(cachePath(name, d))
	if err != nil {
		return "", false
	}

	rec, ok := parseCache(body)
	if !ok {
		return "", false
	}

	currentMTime, ok := d.xcodeSelectMTS()
	if !ok {
		return "", false
	}
	if !currentMTime.Equal(rec.devDirMTime) {
		return "", false
	}

	return rec.resolvedPath, true
}

func writeCache(name, resolved string, d resolveDeps) {
	mts, ok := d.xcodeSelectMTS()
	if !ok {
		return
	}

	line := fmt.Sprintf("%s\n%d\n", resolved, mts.UnixNano())
	_ = d.writeFile(cachePath(name, d), []byte(line))
}

func parseCache(body []byte) (cacheRecord, bool) {
	parts := strings.SplitN(strings.TrimSpace(string(body)), "\n", 2) //nolint:mnd // 2-line format
	if len(parts) != 2 {
		return cacheRecord{}, false
	}

	ns, err := parseInt64(parts[1])
	if err != nil {
		return cacheRecord{}, false
	}

	return cacheRecord{resolvedPath: parts[0], devDirMTime: time.Unix(0, ns)}, true
}

func cachePath(name string, d resolveDeps) string {
	return filepath.Join(d.cacheDir(), "bitrise-tc-realpath-"+name)
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, err //nolint:wrapcheck // caller treats as parse failure
	}

	return n, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err //nolint:wrapcheck // caller logs opaque failure
	}

	return os.Rename(tmp, path) //nolint:wrapcheck
}
