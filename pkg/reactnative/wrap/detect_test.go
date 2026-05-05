//go:build unit

package wrap

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetect_OptOutSkipsEverything(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		Getenv: func(string) string { return "0" },
	})

	assert.Equal(t, Detection{}, det)
}

func TestDetect_NoCLIOnPath(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Getenv:   func(string) string { return "" },
	})

	assert.Equal(t, Detection{}, det)
}

func TestDetect_RNEnabled(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath:             func(string) (string, error) { return "/cli", nil },
		Getenv:               func(string) string { return "" },
		IsReactNativeEnabled: func() bool { return true },
	})

	assert.Equal(t, "/cli", det.CLIPath)
	assert.True(t, det.ReactNativeEnabled)
}

func TestDetect_RNDisabledKeepsCLIPath(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath:             func(string) (string, error) { return "/cli", nil },
		Getenv:               func(string) string { return "" },
		IsReactNativeEnabled: func() bool { return false },
	})

	assert.Equal(t, "/cli", det.CLIPath, "CLIPath surfaces even when RN is disabled so callers can reuse the lookup")
	assert.False(t, det.ReactNativeEnabled)
}

// recordingLogger satisfies the package's small Logger interface and captures
// Debugf / Warnf calls so the skip-path debug logs can be asserted on.
type recordingLogger struct {
	debugs []string
	warns  []string
}

func (l *recordingLogger) Debugf(format string, args ...any) {
	l.debugs = append(l.debugs, fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Warnf(format string, args ...any) {
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}

func TestDetect_DebugLogsOnSkipPaths(t *testing.T) {
	t.Run("opt-out env logs debug", func(t *testing.T) {
		logger := &recordingLogger{}
		Detect(context.Background(), DetectParams{
			Getenv: func(string) string { return "0" },
			Logger: logger,
		})
		assert.Len(t, logger.debugs, 1)
		assert.Contains(t, logger.debugs[0], OptOutEnv)
	})

	t.Run("missing CLI logs debug", func(t *testing.T) {
		logger := &recordingLogger{}
		Detect(context.Background(), DetectParams{
			LookPath: func(string) (string, error) { return "", errors.New("not found") },
			Getenv:   func(string) string { return "" },
			Logger:   logger,
		})
		assert.Len(t, logger.debugs, 1)
		assert.Contains(t, logger.debugs[0], "not on PATH")
	})

	t.Run("RN cache disabled logs debug", func(t *testing.T) {
		logger := &recordingLogger{}
		Detect(context.Background(), DetectParams{
			LookPath:             func(string) (string, error) { return "/cli", nil },
			Getenv:               func(string) string { return "" },
			Logger:               logger,
			IsReactNativeEnabled: func() bool { return false },
		})
		assert.Len(t, logger.debugs, 1)
		assert.Contains(t, logger.debugs[0], "not activated")
	})
}
