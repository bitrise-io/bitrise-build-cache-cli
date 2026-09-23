//go:build unit

package bazel_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bazelpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/bazel"
)

func TestDeactivator_DryRun_LeavesBazelrcUntouched(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	bazelrc := filepath.Join(tmpHome, ".bazelrc")
	seed := "# [start] generated-by-bitrise-build-cache\nbuild --remote_cache=x\n# [end] generated-by-bitrise-build-cache\n"
	require.NoError(t, os.WriteFile(bazelrc, []byte(seed), 0o644))

	d := bazelpkg.NewDeactivator(bazelpkg.DeactivatorParams{
		DryRun: true,
		Logger: log.NewLogger(),
	})
	require.NoError(t, d.Deactivate(context.Background()))

	got, err := os.ReadFile(bazelrc) //nolint:gosec // seeded tmp path
	require.NoError(t, err)
	assert.Equal(t, seed, string(got))
}
