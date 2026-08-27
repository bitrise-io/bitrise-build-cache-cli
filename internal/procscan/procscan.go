// Package procscan finds processes that are holding a stale environment: an IDE
// and a Gradle daemon both read $PATH once at startup, so either one that was
// already up when `activate` ran cannot see a freshly installed CLI — knowing
// which are around lets the CLI name exactly what to restart.
package procscan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const scanTimeout = 3 * time.Second

// gradleDaemonMarker is the daemon's bootstrap class, which every Gradle version
// passes on the java command line.
const gradleDaemonMarker = "org.gradle.launcher.daemon.bootstrap.gradledaemon"

// knownIDEs pairs a display name with the process-argument markers identifying
// it. Every marker was taken from a real process line (macOS app bundles, Linux
// package paths, and the JetBrains launcher's own -Didea.paths.selector, which
// holds across tarball, Toolbox and snap installs). They stay path-anchored so
// they don't fire on a process that merely mentions an IDE — a log tail, an
// editor open on a file under the IDE's directory.
//
//nolint:gochecknoglobals
var knownIDEs = []struct {
	name     string
	patterns []string
}{
	{"Android Studio", []string{"android studio.app/contents/macos", "/android-studio/bin/studio", "didea.paths.selector=androidstudio"}},
	{"IntelliJ IDEA", []string{"intellij idea.app/contents/macos", "/idea-iu", "/idea-ic", "didea.paths.selector=intellijidea"}},
	{"Xcode", []string{"xcode.app/contents/macos/xcode"}},
	{"Visual Studio Code", []string{"visual studio code.app/contents/macos", "/usr/share/code/code"}},
	{"VS Code Insiders", []string{"visual studio code - insiders.app/contents/macos", "/usr/share/code-insiders/code-insiders"}},
	{"Cursor", []string{"cursor.app/contents/macos", "/usr/share/cursor/cursor"}},
}

// Result is what a scan found.
type Result struct {
	// IDEs holds display names, in knownIDEs order.
	IDEs          []string
	GradleDaemons int
}

// Scanner reads the process list. The zero value works; CommandFunc is for tests.
type Scanner struct {
	CommandFunc utils.CommandFunc
}

func (s Scanner) Scan(ctx context.Context) (Result, error) {
	commandFunc := s.CommandFunc
	if commandFunc == nil {
		commandFunc = utils.DefaultCommandFunc()
	}

	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	out, err := commandFunc(ctx, "ps", "-Ao", "args=").CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("list processes: %w", err)
	}

	return scan(strings.ToLower(string(out))), nil
}

func scan(processList string) Result {
	result := Result{}

	for _, entry := range knownIDEs {
		for _, pattern := range entry.patterns {
			if strings.Contains(processList, pattern) {
				result.IDEs = append(result.IDEs, entry.name)

				break
			}
		}
	}

	for line := range strings.SplitSeq(processList, "\n") {
		if strings.Contains(line, gradleDaemonMarker) {
			result.GradleDaemons++
		}
	}

	return result
}
