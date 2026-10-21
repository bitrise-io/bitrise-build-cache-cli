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
		Diagnose: func(ctx context.Context) Result {
			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: fmt.Sprintf("skipped: %s", err)}
			}

			_, lookErr := d.LookPath(paths.CLIBinaryName)
			cliOnPATH := lookErr == nil

			// A committed pin only breaks builds where the CLI is missing from
			// PATH; when it's there the pin resolves fine and there is nothing
			// to flag.
			if cliOnPATH {
				return Result{State: StateOK, Detail: "no actionable pin (CLI on PATH)"}
			}

			matches, err := bazelconfig.ScanForPinnedHelper(ctx, cwd, d.osProxy(), nil)
			if err != nil {
				return Result{State: StateWarn, Detail: fmt.Sprintf("scan failed: %s", err)}
			}
			if len(matches) == 0 {
				return Result{State: StateOK, Detail: "no repo-level credential-helper pin"}
			}

			prefix := "repo-level Bazel credential-helper pin detected AND `" + paths.CLIBinaryName + "` is not on PATH — every `bazel build` here will fail"

			return Result{
				State:  StateError,
				Detail: prefix + ".\n" + bazelconfig.PinnedHelperWarning(matches),
			}
		},
	}
}
