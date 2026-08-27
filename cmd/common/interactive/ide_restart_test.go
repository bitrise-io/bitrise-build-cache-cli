//go:build unit

package interactive

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// recordingLogger collects every formatted line so assertions can look at the
// whole output regardless of which level printed it.
type recordingLogger struct {
	lines []string
}

func (l *recordingLogger) record(format string, v ...interface{}) {
	l.lines = append(l.lines, fmt.Sprintf(format, v...))
}

func (l *recordingLogger) String() string { return strings.Join(l.lines, "\n") }

func (l *recordingLogger) Infof(format string, v ...interface{})   { l.record(format, v...) }
func (l *recordingLogger) Warnf(format string, v ...interface{})   { l.record(format, v...) }
func (l *recordingLogger) Printf(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) Donef(format string, v ...interface{})   { l.record(format, v...) }
func (l *recordingLogger) Debugf(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) Errorf(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) TInfof(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) TWarnf(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) TPrintf(format string, v ...interface{}) { l.record(format, v...) }
func (l *recordingLogger) TDonef(format string, v ...interface{})  { l.record(format, v...) }
func (l *recordingLogger) TDebugf(format string, v ...interface{}) { l.record(format, v...) }
func (l *recordingLogger) TErrorf(format string, v ...interface{}) { l.record(format, v...) }
func (l *recordingLogger) Println()                                {}
func (l *recordingLogger) EnableDebugLog(bool)                     {}

func stubIDEDetector(t *testing.T, running []string) {
	t.Helper()

	original := ideDetector
	ideDetector = func(context.Context) []string { return running }
	t.Cleanup(func() { ideDetector = original })
}

func TestWarnRestartIDEs(t *testing.T) {
	t.Run("names the running IDEs and mentions the Gradle daemon", func(t *testing.T) {
		stubIDEDetector(t, []string{"Android Studio", "Xcode"})
		logger := &recordingLogger{}

		warnRestartIDEs(context.Background(), logger, []string{string(toolGradle), string(toolXcode)})

		out := logger.String()
		assert.Contains(t, out, "Android Studio and Xcode are running — restart them now.")
		assert.Contains(t, out, `Cannot run program "bitrise-build-cache"`)
		assert.Contains(t, out, "./gradlew --stop")
	})

	t.Run("no Gradle daemon hint when Gradle was not selected", func(t *testing.T) {
		stubIDEDetector(t, []string{"Xcode"})
		logger := &recordingLogger{}

		warnRestartIDEs(context.Background(), logger, []string{string(toolXcode)})

		out := logger.String()
		assert.Contains(t, out, "Xcode is running — restart it now.")
		assert.NotContains(t, out, "./gradlew --stop")
	})

	t.Run("falls back to a single conditional line when no IDE is detected", func(t *testing.T) {
		stubIDEDetector(t, nil)
		logger := &recordingLogger{}

		warnRestartIDEs(context.Background(), logger, []string{string(toolGradle)})

		out := logger.String()
		assert.Contains(t, out, "If an IDE was already open before this setup, restart it")
		assert.NotContains(t, out, "./gradlew --stop")
	})
}
