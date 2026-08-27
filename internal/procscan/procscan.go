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
//
// execs must end where the executable path does, so a versioned bundle
// (Xcode-26.6.0.app — what macOS reports even via the Xcode.app symlink) matches
// while Xcodes.app does not. args match anywhere, for markers a boundary would
// cut off: version-suffixed install dirs, and the JetBrains paths.selector
// argument, the only signal that holds across Linux install channels.
//
//nolint:gochecknoglobals
var knownIDEs = []struct {
	name  string
	execs []string
	args  []string
}{
	{
		name: "Android Studio",
		args: []string{"android studio.app/contents/macos", "/android-studio/bin/studio", "didea.paths.selector=androidstudio"},
	},
	{
		name: "IntelliJ IDEA",
		args: []string{"intellij idea.app/contents/macos", "/idea-iu", "/idea-ic", "didea.paths.selector=intellijidea"},
	},
	{
		name:  "Xcode",
		execs: []string{".app/contents/macos/xcode"},
	},
	{
		name:  "Visual Studio Code",
		execs: []string{"visual studio code.app/contents/macos/code", "/usr/share/code/code"},
	},
	{
		name:  "VS Code Insiders",
		execs: []string{"visual studio code - insiders.app/contents/macos/code - insiders", "/usr/share/code-insiders/code-insiders"},
	},
	{
		name: "Cursor",
		args: []string{"cursor.app/contents/macos", "/usr/share/cursor/cursor"},
	},
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
		if ideRunning(processList, entry.execs, entry.args) {
			result.IDEs = append(result.IDEs, entry.name)
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

func ideRunning(processList string, execs, args []string) bool {
	for _, arg := range args {
		if strings.Contains(processList, arg) {
			return true
		}
	}

	for line := range strings.SplitSeq(processList, "\n") {
		for _, exe := range execs {
			if endsAtTokenBoundary(line, exe) {
				return true
			}
		}
	}

	return false
}

// endsAtTokenBoundary reports whether marker ends where the executable path does:
// at the end of the line, or where its arguments start.
func endsAtTokenBoundary(line, marker string) bool {
	for rest := line; ; {
		i := strings.Index(rest, marker)
		if i < 0 {
			return false
		}

		after := rest[i+len(marker):]
		if after == "" || strings.HasPrefix(after, " ") {
			return true
		}

		rest = rest[i+1:]
	}
}
