package interactive

import (
	"context"
	"slices"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ide"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// ideDetector is swappable in tests.
//
//nolint:gochecknoglobals
var ideDetector = func(ctx context.Context) []string {
	return ide.Detector{}.Running(ctx)
}

// warnRestartIDEs tells the user to restart the IDEs that are up. An IDE started
// before the CLI was installed has a $PATH without it, and the generated Gradle
// init script calls the CLI by name, so its builds fail at configuration time
// with `Cannot run program "bitrise-build-cache"`.
func warnRestartIDEs(ctx context.Context, logger log.Logger, tools []string) {
	running := ideDetector(ctx)
	gradleSelected := slices.Contains(tools, string(toolGradle))

	logger.Println()

	if len(running) == 0 {
		logger.Infof("If an IDE was already open before this setup, restart it — IDEs read $PATH at startup and won't find the %s CLI otherwise.", paths.CLIBinaryName)

		return
	}

	logger.Warnf("⚠️  %s %s running — restart %s now.", strings.Join(running, " and "), isAre(len(running)), itThem(len(running)))
	logger.Infof("IDEs read $PATH once at startup, so an already-running IDE won't find the %s CLI and your builds will fail with `Cannot run program \"%s\"`.", paths.CLIBinaryName, paths.CLIBinaryName)

	if gradleSelected {
		logger.Infof("Also run `./gradlew --stop` to drop any Gradle daemon started with the old environment.")
	}
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}

	return "are"
}

func itThem(n int) string {
	if n == 1 {
		return "it"
	}

	return "them"
}
