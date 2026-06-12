package daemon

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// printPermissionHintIfApplicable inspects err for a wrapped *fs.PathError
// with a permission-denied root cause and, if found, writes an actionable
// remediation block to w. No-op for any other error.
//
// The remediation focuses on the common case: ~/.local/state was created
// by `sudo` somewhere along the line and is now owned by root, so mkdir of
// a subdir under it fails. The hint shows the resolved owner uid when we
// can stat the path's nearest existing ancestor, and offers a `chown` /
// remove-and-retry fix.
//
// Library-level errors are not changed — this stays at the cmd layer so
// the internal/daemon contract (return os/exec errors verbatim) is
// preserved for callers that compose it differently.
func printPermissionHintIfApplicable(w io.Writer, err error) {
	if !isPermissionError(err) {
		return
	}

	path := pathErrorPath(err)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────────────")
	fmt.Fprintln(w, "Permission denied creating CLI state directory.")

	if path != "" {
		fmt.Fprintf(w, "Failed path: %s\n", path)

		if culprit, uid, ok := ownerOfNearestAncestor(path); ok {
			fmt.Fprintf(w, "Nearest existing ancestor %s is owned by uid %d (you are uid %d).\n",
				culprit, uid, os.Geteuid())
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Most likely cause: ~/.local/state or one of its parents was created by `sudo` at some point and is now owned by root.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fix (recommended — preserves anything else under that tree):")
	fmt.Fprintln(w, "  sudo chown -R \"$USER\" ~/.local/state")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Or, if nothing else lives there yet, remove + retry:")
	fmt.Fprintln(w, "  sudo rm -rf ~/.local/state/bitrise-build-cache")
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────────────")
	fmt.Fprintln(w)
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
// the hint file has no transitive deps beyond stdlib.
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
