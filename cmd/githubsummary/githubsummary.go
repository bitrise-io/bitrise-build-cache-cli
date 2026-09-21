// Package githubsummary renders the Gradle plugins' end-of-build output as a
// GitHub Actions job summary.
package githubsummary

import (
	"fmt"
	"os"
	"strings"

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
		in := cmd.InOrStdin()

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

		if err := writeBlock(path, markdown); err != nil {
			return fmt.Errorf("write %s: %w", summaryEnvVar, err)
		}

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(githubSummaryCmd)
}

const (
	blockStart = "<!-- bitrise-build-cache-summary:start -->"
	blockEnd   = "<!-- bitrise-build-cache-summary:end -->"
)

// writeBlock replaces this CLI's own section of the job summary, leaving
// anything else in the file alone.
//
// It has to be idempotent because an included build is a build of its own: it
// applies the same init script and closes its own service, so the hook runs
// once per build. Appending would show the summary as many times as there are
// builds, with the earliest -- and least complete -- render first. Replacing
// means the last writer, which has seen the most output, is what remains.
func writeBlock(path, markdown string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read: %w", err)
	}

	rest := stripBlock(string(existing))
	block := blockStart + "\n" + markdown + blockEnd + "\n"

	if err := os.WriteFile(path, []byte(rest+block), 0o644); err != nil { //nolint:gosec,mnd // the runner owns this file
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

func stripBlock(content string) string {
	start := strings.Index(content, blockStart)
	if start < 0 {
		return content
	}

	end := strings.Index(content, blockEnd)
	if end < 0 || end < start {
		return content[:start]
	}

	return content[:start] + content[end+len(blockEnd):]
}
