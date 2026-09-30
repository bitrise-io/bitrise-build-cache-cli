package xcode

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Activation normally publishes this through envman, which belongs to a build
// and does not exist at preboot. The path itself is machine-scoped, so a
// warmed-up VM does know it — it just has no way to hand it to a build that has
// not started yet. Printing it lets whatever does own the VM's environment
// (the preboot startup script, or its emulation in the e2e) publish it, without
// anyone writing the path out a second time.
//
//nolint:gochecknoglobals
var derivedDataPathCmd = &cobra.Command{
	Use:           "derived-data-path",
	Short:         "Print the DerivedData root the xcodebuild wrapper relocates builds into",
	Hidden:        true,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Spawned from a shell that is about to use the value; the version nudge and
	// the stored-auth hydrate would be network work for a path lookup.
	PersistentPreRun: func(*cobra.Command, []string) {},
	RunE: func(cmd *cobra.Command, _ []string) error {
		p, err := paths.Default()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}

		if _, err := fmt.Fprintln(cmd.OutOrStdout(), p.XcodeManagedDerivedDataRoot()); err != nil {
			return fmt.Errorf("write derived data path: %w", err)
		}

		return nil
	},
}

func init() {
	xcelerateCommand.AddCommand(derivedDataPathCmd)
}
