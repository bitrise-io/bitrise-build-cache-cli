package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func (d *Doctor) xcelerateWrapperPathCheck() Check {
	return Check{
		Name: "xcelerate-wrapper-path",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "resolve home dir: " + err.Error()}
			}

			binDir := paths.FromHome(home).XcelerateBinDir()

			for _, name := range []string{"xcodebuild", "xcrun"} {
				if res, ok := diagnoseWrapperOnPath(name, binDir, d.LookPath); !ok {
					return res
				}
			}

			return Result{State: StateOK, Detail: "xcodebuild, xcrun resolve to the xcelerate wrapper"}
		},
	}
}

func diagnoseWrapperOnPath(name, binDir string, lookPath func(string) (string, error)) (Result, bool) {
	expected := filepath.Join(binDir, name)

	actual, err := lookPath(name)
	if err != nil {
		return Result{State: StateWarn, Detail: fmt.Sprintf("%s not on PATH — open a new terminal or `source ~/.zshrc` so the wrapper installed by `activate xcode` becomes visible", name)}, false
	}

	if pathsEqual(actual, expected) {
		return Result{}, true
	}

	return Result{State: StateWarn, Detail: fmt.Sprintf("%s resolves to %s; open a new terminal or `source ~/.zshrc` — your current shell hasn't picked up the wrapper PATH added by `activate xcode` (expected %s)", name, actual, expected)}, false
}

func pathsEqual(a, b string) bool {
	return resolveSymlinks(a) == resolveSymlinks(b)
}

func resolveSymlinks(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}

	return p
}
