//go:build unit

package wrap

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeCommandContext returns a CommandContext that runs `/bin/sh -c "exit N"`
// where N is taken from `behaviour` keyed by the kind of probe ("version" or
// "status"). Missing keys default to exit 0. Using /bin/sh keeps the test
// portable across macOS (no /bin/true) and Linux.
func fakeCommandContext(behaviour map[string]int) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		key := "default"
		for _, a := range args {
			if a == "--feature=react-native" {
				key = "status"

				break
			}
			if a == "--version" {
				key = "version"

				break
			}
		}

		code := behaviour[key]

		return exec.CommandContext(ctx, "/bin/sh", "-c", fmt.Sprintf("exit %d", code)) //nolint:gosec
	}
}

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

func TestDetect_VersionProbeFailureZeroes(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath: func(string) (string, error) { return "/cli", nil },
		Getenv:   func(string) string { return "" },
		CommandContext: fakeCommandContext(map[string]int{
			"version": 1,
		}),
	})

	assert.Equal(t, Detection{}, det, "version probe failure must produce empty Detection (no CLIPath)")
}

func TestDetect_StatusFailureKeepsCLIPathButNotEnabled(t *testing.T) {
	// Status probe must produce a non-(exit-1) failure so queryRNEnabled does
	// not interpret it as "disabled". Easiest way: a binary that does not
	// exist — *exec.Error, not *exec.ExitError.
	commandContext := func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		for _, a := range args {
			if a == "--feature=react-native" {
				return exec.CommandContext(ctx, "/nonexistent/bin/that-does-not-exist") //nolint:gosec
			}
		}

		// Version probe — succeed.
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0") //nolint:gosec
	}

	det := Detect(context.Background(), DetectParams{
		LookPath:       func(string) (string, error) { return "/cli", nil },
		Getenv:         func(string) string { return "" },
		CommandContext: commandContext,
	})

	assert.Equal(t, "/cli", det.CLIPath)
	assert.False(t, det.ReactNativeEnabled, "probe failure must not flag RN as enabled")
}

func TestDetect_StatusExit0MeansEnabled(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath:       func(string) (string, error) { return "/cli", nil },
		Getenv:         func(string) string { return "" },
		CommandContext: fakeCommandContext(map[string]int{}),
	})

	assert.Equal(t, "/cli", det.CLIPath)
	assert.True(t, det.ReactNativeEnabled)
}

func TestDetect_StatusExit1MeansDisabled(t *testing.T) {
	det := Detect(context.Background(), DetectParams{
		LookPath: func(string) (string, error) { return "/cli", nil },
		Getenv:   func(string) string { return "" },
		CommandContext: fakeCommandContext(map[string]int{
			"status": 1,
		}),
	})

	assert.Equal(t, "/cli", det.CLIPath)
	assert.False(t, det.ReactNativeEnabled, "exit 1 means RN cache is not activated")
}

func TestDetect_DefaultTimeoutsApplied(t *testing.T) {
	// Just exercise the zero-timeout path to confirm Detect doesn't hang and
	// returns within reasonable wall time.
	start := time.Now()
	_ = Detect(context.Background(), DetectParams{
		LookPath:       func(string) (string, error) { return "/cli", nil },
		Getenv:         func(string) string { return "" },
		CommandContext: fakeCommandContext(map[string]int{}),
	})
	assert.Less(t, time.Since(start), 5*time.Second, "default-timeout path must not hang")
}
