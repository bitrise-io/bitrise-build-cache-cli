// Package ide detects IDEs that are currently running. An IDE reads $PATH once at
// startup, so one that was already open when `activate` ran cannot see a freshly
// installed CLI — knowing which ones are up lets the CLI name what to restart.
package ide

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const detectTimeout = 3 * time.Second

// known pairs a display name with the process-argument markers identifying it.
// Markers stay path-anchored so they don't fire on a process that merely mentions
// an IDE — a log tail, an editor open on a file under the IDE's directory.
//
//nolint:gochecknoglobals
var known = []struct {
	name     string
	patterns []string
}{
	{"Android Studio", []string{"android studio.app/contents/macos", "/android-studio/bin/studio", "/bin/studio.sh"}},
	{"IntelliJ IDEA", []string{"intellij idea.app/contents/macos", "/idea-iu", "/idea-ic", "/bin/idea.sh", "/idea/bin/idea"}},
	{"Xcode", []string{"xcode.app/contents/macos/xcode"}},
	{"Visual Studio Code", []string{"visual studio code.app/contents/macos", "/usr/share/code/code", "/bin/code-insiders"}},
	{"Cursor", []string{"cursor.app/contents/macos"}},
}

// Detector lists running IDEs. The zero value works; CommandFunc is for tests.
type Detector struct {
	CommandFunc utils.CommandFunc
}

// Running returns the display names of the IDEs it found, in `known` order.
func (d Detector) Running(ctx context.Context) ([]string, error) {
	commandFunc := d.CommandFunc
	if commandFunc == nil {
		commandFunc = utils.DefaultCommandFunc()
	}

	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	out, err := commandFunc(ctx, "ps", "-Ao", "args=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}

	return match(strings.ToLower(string(out))), nil
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
