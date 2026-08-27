package interactive

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/procscan"
)

//nolint:gochecknoglobals // swappable in tests
var scanStaleProcesses = func(ctx context.Context) (procscan.Result, error) {
	return procscan.Scanner{}.Scan(ctx)
}

// warnRestartStale asks for a restart of whatever is still holding the old
// environment: an IDE or a Gradle daemon started before the CLI was installed has
// a $PATH without it, and the generated Gradle init script calls the CLI by name,
// so its builds fail at configuration time.
func warnRestartStale(ctx context.Context, logger log.Logger, tools []string) {
	found, err := scanStaleProcesses(ctx)
	if err != nil {
		logger.Debugf("Could not scan for running IDEs and Gradle daemons: %s", err)
	}

	logger.Println()

	if len(found.IDEs) == 0 {
		logger.Infof("If an IDE was already open before this setup, restart it — IDEs read $PATH at startup and won't find the %s CLI otherwise.", paths.CLIBinaryName)
	} else {
		subject := "is running — restart it now."
		if len(found.IDEs) > 1 {
			subject = "are running — restart them now."
		}

		logger.Warnf("⚠️  %s %s", strings.Join(found.IDEs, " and "), subject)
		logger.Infof("IDEs read $PATH once at startup, so an already-running IDE won't find the %s CLI and your builds will fail with `Cannot run program \"%s\"`.", paths.CLIBinaryName, paths.CLIBinaryName)
	}

	if found.GradleDaemons > 0 && slices.Contains(tools, string(toolGradle)) {
		logger.Warnf("⚠️  %s running — run `./gradlew --stop` to drop the old environment.", gradleDaemonCount(found.GradleDaemons))
	}
}

func gradleDaemonCount(n int) string {
	if n == 1 {
		return "1 Gradle daemon is"
	}

	return strconv.Itoa(n) + " Gradle daemons are"
}
