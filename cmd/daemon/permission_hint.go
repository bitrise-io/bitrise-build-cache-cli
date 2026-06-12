package daemon

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/bitrise-io/go-utils/v2/log"
)

// printPermissionHintIfApplicable inspects err for a wrapped *fs.PathError
// with a permission-denied root cause and, if found, writes an actionable
// remediation block via the logger. No-op for any other error.
//
// The remediation focuses on the common case: ~/.local/state was created
// by `sudo` somewhere along the line and is now owned by root, so mkdir of
// a subdir under it fails. The hint shows the resolved owner uid when we
// can stat the path's nearest existing ancestor, and offers a `chown` /
// remove-and-retry fix.
//
// Output goes through logger.Printf (raw, no level prefix) so the box
// dividers stay aligned — Donef / Warnf would prefix each line and break
// the visual banner. The hint itself reads as a "warning" semantically;
// the upstream caller's raw error still surfaces separately.
//
// Library-level errors are not changed — this stays at the cmd layer so
// the internal/daemon contract (return os/exec errors verbatim) is
// preserved for callers that compose it differently.
func printPermissionHintIfApplicable(logger log.Logger, err error) {
	if !isPermissionError(err) {
		return
	}

	path := pathErrorPath(err)

	logger.Println()
	logger.Printf("─────────────────────────────────────────────────────────────────")
	logger.Printf("Permission denied creating CLI state directory.")

	if path != "" {
		logger.Printf("Failed path: %s", path)

		if culprit, uid, ok := ownerOfNearestAncestor(path); ok {
			logger.Printf("Nearest existing ancestor %s is owned by uid %d (you are uid %d).",
				culprit, uid, os.Geteuid())
		}
	}

	logger.Println()
	logger.Printf("Most likely cause: ~/.local/state or one of its parents was created by `sudo` at some point and is now owned by root.")
	logger.Println()
	logger.Printf("Fix (recommended — preserves anything else under that tree):")
	logger.Printf("  sudo chown -R \"$USER\" ~/.local/state")
	logger.Println()
	logger.Printf("Or, if nothing else lives there yet, remove + retry:")
	logger.Printf("  sudo rm -rf ~/.local/state/bitrise-build-cache")
	logger.Printf("─────────────────────────────────────────────────────────────────")
	logger.Println()
}

// isPermissionError returns true if err's chain contains an
// EACCES / EPERM. errors.Is(err, fs.ErrPermission) is the canonical check;
// keep it in one helper so future cmd subcommands can reuse.
func isPermissionError(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM))
}

// pathErrorPath unwraps err's chain looking for the first *fs.PathError /
// *os.PathError and returns its Path. Empty string when no path is in the
// chain — the caller falls back to a generic hint.
func pathErrorPath(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Path
	}

	return ""
}

// ownerOfNearestAncestor walks up from path until it finds an existing
// directory, stats it, and returns its owner uid (POSIX). Returns ok=false
// on non-POSIX OS or when no ancestor stats cleanly. Used purely for the
// human-readable hint — never on the hot path.
func ownerOfNearestAncestor(path string) (string, int, bool) {
	cur := path
	for cur != "" && cur != "/" {
		info, err := os.Stat(cur)
		if err == nil {
			sys, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return "", 0, false
			}

			return cur, int(sys.Uid), true
		}

		// Drop the last path component and retry.
		next := pathDir(cur)
		if next == cur {
			return "", 0, false
		}

		cur = next
	}

	return "", 0, false
}

// pathDir is filepath.Dir without the path/filepath import — kept tiny so
// the hint file has no transitive deps beyond stdlib + the project logger.
func pathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}

			return p[:i]
		}
	}

	return "."
}
