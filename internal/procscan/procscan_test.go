//go:build unit

package procscan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/procscan"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func scannerFor(t *testing.T, out string, cmdErr error) procscan.Scanner {
	t.Helper()

	return procscan.Scanner{
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

func TestScanner_Scan_IDEs(t *testing.T) {
	// Process lines below are trimmed captures of real IDE processes: Ubuntu
	// 26.04 for the Linux ones, macOS for the app bundles.
	tests := []struct {
		name     string
		psOutput string
		expected []string
	}{
		{
			name:     "Android Studio on Linux, launched from its install dir",
			psOutput: "/opt/android-studio/jbr/bin/java -classpath /opt/android-studio/lib/platform-loader.jar -Didea.paths.selector=AndroidStudio2025.1.3 -Didea.platform.prefix=AndroidStudio\n/usr/sbin/cron -f\n",
			expected: []string{"Android Studio"},
		},
		{
			name:     "IntelliJ IDEA on Linux",
			psOutput: "/home/ubuntu/ides/idea-IU-253.28294.334/jbr/bin/java -classpath /home/ubuntu/ides/idea-IU-253.28294.334/lib/platform-loader.jar -Didea.paths.selector=IntelliJIdea2025.3\n/home/ubuntu/ides/idea-IU-253.28294.334/bin/fsnotifier\n",
			expected: []string{"IntelliJ IDEA"},
		},
		{
			name:     "IntelliJ IDEA installed by Toolbox, where the install dir is not idea-IU",
			psOutput: "/home/me/.local/share/JetBrains/Toolbox/apps/intellij-idea-ultimate/jbr/bin/java -Didea.paths.selector=IntelliJIdea2025.3 -classpath /home/me/.local/share/JetBrains/Toolbox/apps/intellij-idea-ultimate/lib/platform-loader.jar\n",
			expected: []string{"IntelliJ IDEA"},
		},
		{
			name:     "IntelliJ IDEA on macOS, where the JVM runs inside the native launcher",
			psOutput: "/Users/me/Applications/IntelliJ IDEA.app/Contents/MacOS/idea\n/Users/me/Applications/IntelliJ IDEA.app/Contents/bin/fsnotifier\n",
			expected: []string{"IntelliJ IDEA"},
		},
		{
			name:     "VS Code and Cursor on Linux are told apart",
			psOutput: "/usr/share/code/code --no-sandbox\n/usr/share/code/code --type=zygote --no-sandbox\n/usr/share/cursor/cursor --no-sandbox\n",
			expected: []string{"Visual Studio Code", "Cursor"},
		},
		{
			name:     "VS Code Insiders on Linux is not reported as stable VS Code",
			psOutput: "/usr/share/code-insiders/code-insiders --no-sandbox\n/usr/share/code-insiders/code-insiders --type=renderer\n",
			expected: []string{"VS Code Insiders"},
		},
		{
			name:     "a versioned Xcode bundle, which is what macOS reports",
			psOutput: "/Applications/Xcode-26.6.0.app/Contents/MacOS/Xcode\n/usr/libexec/opendirectoryd\n",
			expected: []string{"Xcode"},
		},
		{
			name:     "Xcode with launch arguments",
			psOutput: "/Applications/Xcode.app/Contents/MacOS/Xcode -psn_0_167948\n",
			expected: []string{"Xcode"},
		},
		{
			name:     "the Xcodes version manager is not Xcode",
			psOutput: "/Applications/Xcodes.app/Contents/MacOS/Xcodes\n",
			expected: nil,
		},
		{
			name:     "an Xcode helper is not the app itself",
			psOutput: "/Applications/Xcode.app/Contents/SharedFrameworks/DVTInstrumentsFoundation.framework/Resources/DTServiceHub\n",
			expected: nil,
		},
		{
			name:     "VS Code on macOS",
			psOutput: "/Applications/Visual Studio Code.app/Contents/MacOS/Code\n",
			expected: []string{"Visual Studio Code"},
		},
		{
			name:     "several IDEs are reported once each, in a stable order",
			psOutput: "/Applications/Cursor.app/Contents/MacOS/Cursor\n/Applications/Xcode-26.6.0.app/Contents/MacOS/Xcode\n/Applications/Xcode-26.6.0.app/Contents/MacOS/Xcode -psn_0_1\n/Applications/Android Studio.app/Contents/MacOS/studio\n",
			expected: []string{"Android Studio", "Xcode", "Cursor"},
		},
		{
			name: "processes that merely mention an IDE do not count",
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
			got, err := scannerFor(t, tc.psOutput, nil).Scan(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got.IDEs)
		})
	}
}

func TestScanner_Scan_gradleDaemons(t *testing.T) {
	// One real daemon line per Gradle install shape: the wrapper dist and a
	// Homebrew install. The Kotlin daemon is a different process and must not count.
	const daemonLine = "/opt/homebrew/opt/openjdk@17/bin/java -Xmx2g -cp /Users/me/.gradle/wrapper/dists/gradle-8.14.5-bin/abc/gradle-8.14.5/lib/gradle-daemon-main-8.14.5.jar org.gradle.launcher.daemon.bootstrap.GradleDaemon 8.14.5"

	tests := []struct {
		name     string
		psOutput string
		expected int
	}{
		{"no daemon", "/sbin/launchd\n/usr/bin/ssh-agent\n", 0},
		{"one daemon", daemonLine + "\n", 1},
		{"two daemons", daemonLine + "\n" + daemonLine + " --foo\n", 2},
		{
			name:     "a shell command naming the class is not a daemon",
			psOutput: "grep -r org.gradle.launcher.daemon.bootstrap.GradleDaemon /Users/me/src\n",
			expected: 0,
		},
		{
			name:     "the Kotlin compile daemon is not a Gradle daemon",
			psOutput: "/usr/bin/java -cp /Users/me/.gradle/caches/kotlin.jar org.jetbrains.kotlin.daemon.KotlinCompileDaemon\n",
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scannerFor(t, tc.psOutput, nil).Scan(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got.GradleDaemons)
		})
	}
}

func TestScanner_Scan_psFails(t *testing.T) {
	got, err := scannerFor(t, "/Applications/Android Studio.app/Contents/MacOS/studio", errors.New("ps: command not found")).
		Scan(context.Background())
	require.Error(t, err)
	assert.Empty(t, got.IDEs)
}
