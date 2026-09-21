// Package githubsummary renders the Gradle plugins' end-of-build output as a
// GitHub Actions job summary.
package githubsummary

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

const summaryEnvVar = "GITHUB_STEP_SUMMARY"

var githubSummaryCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "github-summary [log file]",
	Short: "Render the build's cache stats as a GitHub Actions job summary",
	Long: `Render the build's cache stats as a GitHub Actions job summary.

Reads a Gradle build log (or stdin), picks out what the Bitrise plugins printed
at the end of the build -- task hits, cache transfer, the invocation link -- and
appends it as markdown to $GITHUB_STEP_SUMMARY. Outside GitHub Actions, where
that variable is unset, it writes to stdout instead.

The numbers come from the build's own output rather than the analytics API,
because the API ingests them asynchronously and a summary has to be ready the
moment the build ends.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var in io.Reader = cmd.InOrStdin()

		if len(args) == 1 {
			f, err := os.Open(args[0])
			if err != nil {
				return fmt.Errorf("open build log: %w", err)
			}
			defer f.Close()
			in = f
		}

		markdown := Render(Parse(in))

		path := os.Getenv(summaryEnvVar)
		if path == "" {
			fmt.Fprint(cmd.OutOrStdout(), markdown)

			return nil
		}

		// Appended, not written: other steps may have added to the summary, and
		// a job may build more than once.
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open %s: %w", summaryEnvVar, err)
		}
		defer f.Close()

		if _, err := f.WriteString(markdown); err != nil {
			return fmt.Errorf("write %s: %w", summaryEnvVar, err)
		}

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(githubSummaryCmd)
}
