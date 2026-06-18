// Package permhint surfaces a chown remediation block when a CLI subcommand
// hits an EACCES / EPERM while writing under ~/.local/state.
package permhint

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/bitrise-io/go-utils/v2/log"
)

// PrintIfApplicable inspects err and emits a chown remediation hint when err is a permission-denied PathError.
// logger.Printf is intentional — Donef/Warnf would prefix each line and break the banner.
func PrintIfApplicable(logger log.Logger, err error) {
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

func isPermissionError(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM))
}

func pathErrorPath(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Path
	}

	return ""
}

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

		next := pathDir(cur)
		if next == cur {
			return "", 0, false
		}

		cur = next
	}

	return "", 0, false
}

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
