package xcode

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

//nolint:gochecknoglobals
var (
	snapshotBuildDBFlagDerivedData  string
	snapshotBuildDBFlagSnapshotRoot string
)

//nolint:gochecknoglobals
var snapshotBuildDBCmd = &cobra.Command{
	Use:   "snapshot-builddb",
	Short: "Snapshot an Xcode build.db under a versioned per-build-hex store.",
	Long: `snapshot-builddb copies Xcode's live build.db into a per-build-hex directory
under the build-analyzer snapshot root so subsequent 'xcode analyze' runs can
diff signatures against the prior build.

Signal shelf-life: a single Xcode build bumps info.iteration and the prior
classification is wrong afterward. Snapshotting the DB before the next build
preserves the prior-iteration signal so analyze can tell new-task from
signature-mismatch without relying on only the current snapshot.

Missing build.db is a soft-fail (exit 0, stderr warn) — same contract as
'analyze' when DerivedData holds no DB yet. Pass --snapshot-root to override
the default (~/.local/state/xcelerate/build-analyzer/snapshots).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runSnapshotBuildDB(cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func init() {
	xcodeCommand.AddCommand(snapshotBuildDBCmd)

	snapshotBuildDBCmd.Flags().StringVar(&snapshotBuildDBFlagDerivedData, "derived-data", "", "DerivedData path containing Build/Intermediates.noindex/XCBuildData/build.db.")
	snapshotBuildDBCmd.Flags().StringVar(&snapshotBuildDBFlagSnapshotRoot, "snapshot-root", "", "Override the snapshot store root (default: ~/.local/state/xcelerate/build-analyzer/snapshots, env BITRISE_BUILD_ANALYZER_SNAPSHOT_ROOT).")
}

// runSnapshotBuildDB validates the flags, resolves the store root, and defers
// to buildanalyzer.SnapshotBuildDB for the actual VACUUM INTO + prune. Empty
// snapshot path is logged to stderr and exits 0 so a CI hook can schedule this
// defensively on every build without failing the step when the DB isn't there.
func runSnapshotBuildDB(out, stderr io.Writer) error {
	if snapshotBuildDBFlagDerivedData == "" {
		return fmt.Errorf("--derived-data is required")
	}

	storeRoot := snapshotBuildDBFlagSnapshotRoot
	if storeRoot == "" {
		storeRoot = buildanalyzer.DefaultSnapshotRoot()
	}

	path, err := buildanalyzer.SnapshotBuildDB(snapshotBuildDBFlagDerivedData, storeRoot)
	if err != nil {
		return fmt.Errorf("snapshot build.db: %w", err)
	}

	if path == "" {
		fmt.Fprintf(stderr, "snapshot-builddb: no build.db under %s — nothing to snapshot\n", snapshotBuildDBFlagDerivedData)

		return nil
	}

	fmt.Fprintln(out, path)

	return nil
}
