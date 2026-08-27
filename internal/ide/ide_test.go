//go:build unit

package ide_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ide"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func detectorFor(t *testing.T, out string, cmdErr error) ide.Detector {
	t.Helper()

	return ide.Detector{
		CommandFunc: func(_ context.Context, command string, args ...string) utils.Command {
			assert.Equal(t, "ps", command)
			assert.Equal(t, []string{"-Ao", "args="}, args)

			return &utilsMocks.CommandMock{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte(out), cmdErr
				},
			}
		},
	}
}

func TestDetector_Running(t *testing.T) {
	tests := []struct {
		name     string
		psOutput string
		expected []string
	}{
		{
			name:     "Android Studio on macOS",
			psOutput: "/Applications/Android Studio.app/Contents/MacOS/studio\n/usr/sbin/cfprefsd\n",
			expected: []string{"Android Studio"},
		},
		{
			name:     "Android Studio on Linux",
			psOutput: "/opt/android-studio/bin/studio.sh\n",
			expected: []string{"Android Studio"},
		},
		{
			name:     "several IDEs are reported in a stable order",
			psOutput: "/Applications/Cursor.app/Contents/MacOS/Cursor\n/Applications/IntelliJ IDEA.app/Contents/MacOS/idea\n/Applications/Android Studio.app/Contents/MacOS/studio\n",
			expected: []string{"Android Studio", "IntelliJ IDEA", "Cursor"},
		},
		{
			name:     "each IDE is reported once, however many processes it has",
			psOutput: "/Applications/Xcode.app/Contents/MacOS/Xcode\n/Applications/Xcode.app/Contents/MacOS/Xcode --foo\n",
			expected: []string{"Xcode"},
		},
		{
			name: "an unrelated process mentioning an IDE path does not count",
			psOutput: "tail -f /Users/me/Library/Logs/Android Studio/idea.log\n" +
				"grep -r studio.sh /Users/me/src\n" +
				"vim /Users/me/notes/code-insiders.md\n",
			expected: nil,
		},
		{
			name:     "no IDE running",
			psOutput: "/sbin/launchd\n/usr/bin/ssh-agent\n",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := detectorFor(t, tc.psOutput, nil).Running(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestDetector_Running_psFails(t *testing.T) {
	got, err := detectorFor(t, "/Applications/Android Studio.app/Contents/MacOS/studio", errors.New("ps: command not found")).
		Running(context.Background())
	require.Error(t, err)
	assert.Nil(t, got)
}
