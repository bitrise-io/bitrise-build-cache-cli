package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// bazelCLIBinOnPATHCheck fires when Bazel has been activated on this machine
// but `bitrise-build-cache` is not on $PATH. Activation would then have taken
// the legacy header-injection branch (bazelrc.gotemplate gates the
// credential-helper branch on clibin.Resolve → $PATH), so remote-cache calls
// carry a stale Bearer instead of resolving through the helper. This is a
// separate signal from the repo-level pin scan: it fires even when no
// .bazelrc line commits the pin, and covers the interactive-dev case where
// the CLI was installed to a shell-scope dir that never made it into a fresh
// terminal's PATH.
func (d *Doctor) bazelCLIBinOnPATHCheck() Check {
	return Check{
		Name: "bazel-clibin-on-path",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Bazel) {
				return Result{State: StateOK, Detail: "skipped (bazel not activated)"}
			}

			if _, err := d.LookPath(paths.CLIBinaryName); err == nil {
				return Result{State: StateOK, Detail: "`" + paths.CLIBinaryName + "` resolves on $PATH"}
			}

			return Result{
				State: StateWarn,
				Detail: "`" + paths.CLIBinaryName + "` is not on $PATH but bazel was activated on this machine — " +
					"generated ~/.bazelrc fell back to a static auth header instead of the credential helper. " +
					"Install the CLI into a dir on $PATH (`~/.local/bin`, `/usr/local/bin`, or `brew install bitrise-io/bitrise-build-cache/bitrise-build-cache`) " +
					"and re-run `bitrise-build-cache activate bazel`.",
			}
		},
	}
}
