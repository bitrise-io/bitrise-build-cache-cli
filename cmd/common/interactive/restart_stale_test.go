//go:build unit

package interactive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/procscan"
)

var _ log.Logger = &recordingLogger{}

// recordingLogger flattens every level into one buffer, so a message can be
// asserted on without pinning which level printed it.
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

func stubScan(t *testing.T, ides []string, gradleDaemons int, err error) {
	t.Helper()

	original := scanStaleProcesses
	scanStaleProcesses = func(context.Context) (procscan.Result, error) {
		return procscan.Result{IDEs: ides, GradleDaemons: gradleDaemons}, err
	}
	t.Cleanup(func() { scanStaleProcesses = original })
}

func TestWarnRestartStale(t *testing.T) {
	t.Run("names the running IDEs and counts the Gradle daemons", func(t *testing.T) {
		stubScan(t, []string{"Android Studio", "Xcode"}, 2, nil)
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolGradle), string(toolXcode)})

		out := logger.String()
		assert.Contains(t, out, "Android Studio and Xcode are running — restart them now.")
		assert.Contains(t, out, `Cannot run program "bitrise-build-cache"`)
		assert.Contains(t, out, "2 Gradle daemons are running — run `./gradlew --stop`")
	})

	t.Run("no Gradle daemon hint when Gradle was not selected", func(t *testing.T) {
		stubScan(t, []string{"Xcode"}, 1, nil)
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolXcode)})

		out := logger.String()
		assert.Contains(t, out, "Xcode is running — restart it now.")
		assert.NotContains(t, out, "./gradlew --stop")
	})

	t.Run("falls back to a single conditional line when no IDE is detected", func(t *testing.T) {
		stubScan(t, nil, 0, nil)
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolGradle)})

		out := logger.String()
		assert.Contains(t, out, "If an IDE was already open before this setup, restart it")
		assert.NotContains(t, out, "./gradlew --stop")
	})

	t.Run("a single running daemon is reported in the singular", func(t *testing.T) {
		stubScan(t, nil, 1, nil)
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolGradle)})

		assert.Contains(t, logger.String(), "1 Gradle daemon is running — run `./gradlew --stop`")
	})

	t.Run("no daemon hint when none is running", func(t *testing.T) {
		stubScan(t, []string{"Android Studio"}, 0, nil)
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolGradle)})

		assert.NotContains(t, logger.String(), "./gradlew --stop")
	})

	t.Run("a failed detection still prints the conditional line", func(t *testing.T) {
		stubScan(t, nil, 0, errors.New("ps: command not found"))
		logger := &recordingLogger{}

		warnRestartStale(context.Background(), logger, []string{string(toolGradle)})

		out := logger.String()
		assert.Contains(t, out, "Could not scan for running IDEs and Gradle daemons")
		assert.Contains(t, out, "If an IDE was already open before this setup, restart it")
	})
}
