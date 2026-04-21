//go:build unit

package common_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
)

// helperEnvVar gates the helper-process branch of TestMain. When set, the
// test binary re-enters itself as a cobra executor and calls common.Execute()
// — which exits via os.Exit with the status code we want to assert on.
const helperEnvVar = "BBC_STATUS_EXIT_HELPER"

// TestMain lets this test binary double as the CLI binary when helperEnvVar
// is set. Args come via a second env var (newline-joined) so we don't have
// to fight Go's test-flag parsing on os.Args.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnvVar) == "1" {
		argv := strings.Split(os.Getenv(helperEnvVar+"_ARGS"), "\n")
		common.RootCmd.SetArgs(argv)
		common.Execute()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func spawnStatus(t *testing.T, home string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	self, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.Command(self) //nolint:noctx // test helper, no context needed
	cmd.Env = append(os.Environ(),
		helperEnvVar+"=1",
		helperEnvVar+"_ARGS="+strings.Join(append([]string{"status"}, args...), "\n"),
		"HOME="+home,
	)

	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			t.Fatalf("helper process failed to run: %v (stderr=%q)", runErr, errBuf.String())
		}
		exitCode = ee.ExitCode()
	}

	return out.String(), errBuf.String(), exitCode
}

func writeRNFixtureForExit(t *testing.T, home string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, ".bitrise", "cache", "reactnative")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(map[string]bool{"enabled": enabled})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
}

// TestExecute_StatusExitCodes_EndToEnd runs the CLI as a subprocess and
// asserts that common.Execute() translates statusExitError into the right
// os.Exit code. Prior tests exercised HandleStatusExit in isolation — this
// closes the loop on the root-cmd wiring in root.go.
func TestExecute_StatusExitCodes_EndToEnd(t *testing.T) {
	t.Run("feature enabled → exit 0", func(t *testing.T) {
		home := t.TempDir()
		writeRNFixtureForExit(t, home, true)

		_, _, code := spawnStatus(t, home, "--feature=react-native", "--quiet")
		assert.Equal(t, 0, code)
	})

	t.Run("feature disabled → exit 1", func(t *testing.T) {
		home := t.TempDir()

		_, _, code := spawnStatus(t, home, "--feature=react-native", "--quiet")
		assert.Equal(t, 1, code)
	})

	t.Run("unknown feature → exit 2", func(t *testing.T) {
		home := t.TempDir()

		_, stderr, code := spawnStatus(t, home, "--feature=bazel", "--quiet")
		assert.Equal(t, 2, code)
		assert.Contains(t, strings.ToLower(stderr), "unknown feature")
	})

	t.Run("quiet without feature → exit 2", func(t *testing.T) {
		home := t.TempDir()

		_, stderr, code := spawnStatus(t, home, "--quiet")
		assert.Equal(t, 2, code)
		assert.Contains(t, stderr, "--quiet")
	})

	t.Run("default table output → exit 0", func(t *testing.T) {
		home := t.TempDir()

		stdout, _, code := spawnStatus(t, home)
		assert.Equal(t, 0, code)
		assert.Contains(t, stdout, "gradle")
	})
}
