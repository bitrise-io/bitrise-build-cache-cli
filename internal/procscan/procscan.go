// Package procscan finds processes holding a stale environment. An IDE and a
// Gradle daemon both read $PATH once at startup, so either one that was already
// up when `activate` ran cannot see a freshly installed CLI.
package procscan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const scanTimeout = 3 * time.Second

// The bootstrap class is stable across majors (checked on 4.4.1 and 8.14.5),
// unlike the jar paths around it.
const gradleDaemonMarker = "org.gradle.launcher.daemon.bootstrap.gradledaemon"

// knownIDEs pairs a display name with markers taken from real process lines.
// JetBrains install dirs differ per channel (`idea-IU-<ver>` vs Toolbox's
// `intellij-idea-ultimate`), so on Linux the launcher's own paths.selector
// argument is what holds; macOS runs the JVM inside the app bundle and passes no
// such argument. Markers stay path-anchored to not fire on a process that merely
// mentions an IDE, such as a tail of its log.
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
	var result Result

	for _, entry := range knownIDEs {
		for _, pattern := range entry.patterns {
			if strings.Contains(processList, pattern) {
				result.IDEs = append(result.IDEs, entry.name)

				break
			}
		}
	}

	// A line has to be a JVM as well: the marker alone also matches a shell
	// command that merely names the class, e.g. a grep for it.
	for line := range strings.SplitSeq(processList, "\n") {
		if strings.Contains(line, gradleDaemonMarker) && strings.Contains(line, "java") {
			result.GradleDaemons++
		}
	}

	return result
}
