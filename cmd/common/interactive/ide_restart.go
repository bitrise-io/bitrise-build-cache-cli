package interactive

import (
	"context"
	"slices"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ide"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

//nolint:gochecknoglobals // swappable in tests
var detectRunningIDEs = func(ctx context.Context) ([]string, error) {
	return ide.Detector{}.Running(ctx)
}

// warnRestartIDEs asks for a restart of the IDEs that are up: an IDE opened
// before the CLI was installed has a $PATH without it, and the generated Gradle
// init script calls the CLI by name, so its builds fail at configuration time.
func warnRestartIDEs(ctx context.Context, logger log.Logger, tools []string) {
	running, err := detectRunningIDEs(ctx)
	if err != nil {
		logger.Debugf("Could not list running IDEs: %s", err)
	}

	logger.Println()

	if len(running) == 0 {
		logger.Infof("If an IDE was already open before this setup, restart it — IDEs read $PATH at startup and won't find the %s CLI otherwise.", paths.CLIBinaryName)

		return
	}

	subject := "is running — restart it now."
	if len(running) > 1 {
		subject = "are running — restart them now."
	}

	logger.Warnf("⚠️  %s %s", strings.Join(running, " and "), subject)
	logger.Infof("IDEs read $PATH once at startup, so an already-running IDE won't find the %s CLI and your builds will fail with `Cannot run program \"%s\"`.", paths.CLIBinaryName, paths.CLIBinaryName)

	if slices.Contains(tools, string(toolGradle)) {
		logger.Infof("Also run `./gradlew --stop` to drop any Gradle daemon started with the old environment.")
	}
}
