package doctor

import (
	"context"
	"fmt"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func (d *Doctor) bazelCredHelperCheck() Check {
	return Check{
		Name: "bazel-credhelper",
		Diagnose: func(_ context.Context) Result {
			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: fmt.Sprintf("skipped: %s", err)}
			}

			matches, err := bazelconfig.ScanForPinnedHelper(cwd, d.osProxy())
			if err != nil {
				return Result{State: StateWarn, Detail: fmt.Sprintf("scan failed: %s", err)}
			}
			if len(matches) == 0 {
				return Result{State: StateOK, Detail: "no repo-level credential-helper pin"}
			}

			_, lookErr := d.LookPath(paths.CLIBinaryName)
			cliOnPATH := lookErr == nil

			state := StateWarn
			prefix := "repo-level Bazel credential-helper pin detected"
			if !cliOnPATH {
				state = StateError
				prefix = "repo-level Bazel credential-helper pin detected AND `" + paths.CLIBinaryName + "` is not on PATH — every `bazel build` here will fail"
			}

			return Result{
				State:  state,
				Detail: prefix + ".\n" + bazelconfig.PinnedHelperWarning(matches),
			}
		},
	}
}
