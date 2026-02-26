//go:build unit

package envexport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport_SetsOsEnv(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())
	exporter.Export("TEST_ENVEXPORT_KEY", "test_value")

	assert.Equal(t, "test_value", os.Getenv("TEST_ENVEXPORT_KEY"))
	t.Cleanup(func() { os.Unsetenv("TEST_ENVEXPORT_KEY") })
}

func TestExport_WritesToGitHubEnvFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "github_env")

	exporter := New(map[string]string{
		"GITHUB_ENV": envFile,
	}, log.NewLogger())

	exporter.Export("MY_KEY", "my_value")
	exporter.Export("ANOTHER_KEY", "another_value")

	content, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Equal(t, "MY_KEY=my_value\nANOTHER_KEY=another_value\n", string(content))
}

func TestExport_SkipsGitHubEnvWhenNotSet(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())

	// Should not panic or error
	exporter.Export("SOME_KEY", "some_value")
}

func TestExport_HandlesEnvmanFailureGracefully(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())

	// envman is not installed in test environment — should not panic
	exporter.Export("TEST_ENVMAN_KEY", "test_value")

	// Verify the os.Setenv still worked despite envman failure
	assert.Equal(t, "test_value", os.Getenv("TEST_ENVMAN_KEY"))
	t.Cleanup(func() { os.Unsetenv("TEST_ENVMAN_KEY") })
}
