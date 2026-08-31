//go:build unit

package bazelconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate_DropsStoredRepoURLFromGeneratedBlock(t *testing.T) {
	home := t.TempDir()
	bazelrcPath := filepath.Join(home, ".bazelrc")

	current := `build --remote_header='x-repository-url=https://github.com/me/mine.git'
# [start] generated-by-bitrise-build-cache
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-repository-url=https://github.com/org/first.git'
build --bes_header='x-repository-url=https://github.com/org/first.git'
build --bes_header='x-os=Darwin'
# [end] generated-by-bitrise-build-cache
`
	require.NoError(t, os.WriteFile(bazelrcPath, []byte(current), 0o644))
	require.NoError(t, WriteSidecar(home, Sidecar{BazelrcPath: bazelrcPath}))

	require.NoError(t, SidecarMigrator{}.Migrate(home))

	got, err := os.ReadFile(bazelrcPath)
	require.NoError(t, err)
	assert.Equal(t, `build --remote_header='x-repository-url=https://github.com/me/mine.git'
# [start] generated-by-bitrise-build-cache
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-os=Darwin'
# [end] generated-by-bitrise-build-cache
`, string(got))
}

func TestMigrate_LeavesBazelrcUntouchedWhenNothingToDrop(t *testing.T) {
	home := t.TempDir()
	bazelrcPath := filepath.Join(home, ".bazelrc")

	current := `# [start] generated-by-bitrise-build-cache
build --remote_header='x-org-id=WorkspaceIDValue'
# [end] generated-by-bitrise-build-cache
`
	require.NoError(t, os.WriteFile(bazelrcPath, []byte(current), 0o644))
	require.NoError(t, WriteSidecar(home, Sidecar{BazelrcPath: bazelrcPath}))

	require.NoError(t, SidecarMigrator{}.Migrate(home))

	got, err := os.ReadFile(bazelrcPath)
	require.NoError(t, err)
	assert.Equal(t, current, string(got))
}

func TestMigrate_MissingBazelrc_IsNotAnError(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, WriteSidecar(home, Sidecar{BazelrcPath: filepath.Join(home, ".bazelrc")}))

	require.NoError(t, SidecarMigrator{}.Migrate(home))
}

func TestMigrate_NoSidecar_IsNoOp(t *testing.T) {
	require.NoError(t, SidecarMigrator{}.Migrate(t.TempDir()))
}
