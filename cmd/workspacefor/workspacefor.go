// Package workspacefor wires the `bitrise-build-cache workspace-for`
// subcommand: given a directory, walk up looking for the project marker
// and print the workspace slug. Intended for shell-out consumers like
// the Gradle init script and the Bazel credential helper.
package workspacefor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Exit codes: 0 = matched, 1 = read/parse error, 2 = no marker found.
const (
	exitReadError = 1
	exitNoMarker  = 2
)

// errNoMarker signals that no .bitrise-build-cache.json was found while
// walking up from the requested directory. Read errors are distinct.
var errNoMarker = errors.New("no project marker found")

// result is the outcome of a successful lookup.
type result struct {
	Workspace  string
	MarkerPath string
}

//nolint:gochecknoglobals
var (
	flagPath string
	flagJSON bool
)

//nolint:gochecknoglobals
var workspaceForCmd = &cobra.Command{
	Use:   "workspace-for",
	Short: "Print the Bitrise Build Cache workspace slug for a directory",
	Long: `Walk up from --path (default: current directory) looking for a
.bitrise-build-cache.json project marker. Print the workspace slug on
match (exit 0). Exit 2 when no marker is found. Exit 1 on read/parse
error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Shell-out consumers (Gradle init, Bazel helper) spawn this hot;
	// skip the root's stored-auth hydration.
	PersistentPreRun: func(*cobra.Command, []string) {},
	RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd.OutOrStdout(), cmd.ErrOrStderr(), utils.DefaultOsProxy{}, flagPath, flagJSON)
	},
}

func init() {
	workspaceForCmd.Flags().StringVar(&flagPath, "path", ".", "Directory to walk up from (default: current directory)")
	workspaceForCmd.Flags().BoolVar(&flagJSON, "json", false, "Emit JSON with workspace + markerPath instead of the bare slug")

	common.RootCmd.AddCommand(workspaceForCmd)
}

func run(out, errOut io.Writer, osProxy utils.OsProxy, path string, asJSON bool) error {
	res, err := resolveForPath(path, osProxy)
	switch {
	case errors.Is(err, errNoMarker):
		return common.NewExitError(exitNoMarker) //nolint:wrapcheck // sentinel exit-code error, must reach Execute unchanged
	case err != nil:
		fmt.Fprintf(errOut, "error: %s\n", err.Error())

		return common.NewExitError(exitReadError) //nolint:wrapcheck // sentinel exit-code error, must reach Execute unchanged
	}

	if asJSON {
		enc := json.NewEncoder(out)
		if err := enc.Encode(map[string]string{
			"workspace":  res.Workspace,
			"markerPath": res.MarkerPath,
		}); err != nil {
			return fmt.Errorf("encode workspace-for JSON: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintln(out, res.Workspace); err != nil {
		return fmt.Errorf("write workspace slug: %w", err)
	}

	return nil
}

// resolveForPath walks up from path (which may be relative — resolved
// against CWD) looking for the marker. Returns errNoMarker when nothing
// is found; wraps read/parse errors otherwise.
func resolveForPath(path string, osProxy utils.OsProxy) (result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return result{}, fmt.Errorf("resolve absolute path: %w", err)
	}

	markerPath, marker, err := configcommon.WalkUpFindMarker(abs, osProxy)
	if err != nil {
		return result{}, err //nolint:wrapcheck // WalkUpFindMarker errors already include path + phase (read/parse/validate)
	}
	if marker == nil {
		return result{}, errNoMarker
	}

	return result{Workspace: marker.Workspace, MarkerPath: markerPath}, nil
}
