// Package ide detects IDEs that are currently running. IDEs read $PATH once at
// startup, so one that was already open when `activate` ran cannot see a freshly
// installed CLI and its builds fail — knowing which ones are up lets the CLI say
// exactly what needs restarting.
package ide

import (
	"context"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const detectTimeout = 3 * time.Second

// known maps a display name to the process-argument markers that identify it.
// Markers are matched case-insensitively against the full command line, and are
// specific enough (app bundle executables, launcher scripts) not to fire on an
// unrelated process that merely mentions the IDE in a path.
//
//nolint:gochecknoglobals
var known = []struct {
	name     string
	patterns []string
}{
	{"Android Studio", []string{"android studio.app/contents/macos", "/android-studio/bin/studio", "studio.sh", "studio_main"}},
	{"IntelliJ IDEA", []string{"intellij idea.app/contents/macos", "/idea-iu", "/idea-ic", "/idea/bin/idea", "idea.sh"}},
	{"Xcode", []string{"xcode.app/contents/macos/xcode"}},
	{"Visual Studio Code", []string{"visual studio code.app/contents/macos", "/usr/share/code/code", "code-insiders"}},
	{"Cursor", []string{"cursor.app/contents/macos"}},
}

// Detector lists running IDEs. The zero value works; CommandFunc is for tests.
type Detector struct {
	CommandFunc utils.CommandFunc
}

// Running returns the display names of the IDEs it found, in `known` order. An
// unusable process list yields no names: the detection is a nicety, so a host
// where it doesn't work stays silent rather than guessing.
func (d Detector) Running(ctx context.Context) []string {
	commandFunc := d.CommandFunc
	if commandFunc == nil {
		commandFunc = utils.DefaultCommandFunc()
	}

	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	out, err := commandFunc(ctx, "ps", "-Ao", "args=").CombinedOutput()
	if err != nil {
		return nil
	}

	return match(strings.ToLower(string(out)))
}

func match(processList string) []string {
	var found []string
	for _, entry := range known {
		for _, pattern := range entry.patterns {
			if strings.Contains(processList, pattern) {
				found = append(found, entry.name)

				break
			}
		}
	}

	return found
}
