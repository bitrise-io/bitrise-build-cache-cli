//go:build unit

package mirrors_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/gradle/mirrors"
)

func boolPtr(b bool) *bool { return &b }

func TestActivator_Activate_ExplicitParamsWin(t *testing.T) {
	tmpDir := t.TempDir()

	// Env says disabled; explicit Enabled=true should override and write the script.
	a := mirrors.NewActivator(mirrors.ActivatorParams{
		GradleHome:    tmpDir,
		SelectedFlags: []string{"mavencentral"},
		Datacenter:    "AMS1",
		Enabled:       boolPtr(true),
		Envs: map[string]string{
			mirrorsconfig.EnabledEnvKey:    "false",
			mirrorsconfig.DatacenterEnvKey: "ZZZ9",
		},
		Logger: log.NewLogger(),
	})

	require.NoError(t, a.Activate(context.Background()))

	initFile := filepath.Join(tmpDir, "init.d", mirrorsconfig.InitFileName)
	content, err := os.ReadFile(initFile)
	require.NoError(t, err)

	assert.Contains(t, string(content), "https://repository-manager.services.bitrise.io:8090/maven/central")
	assert.NotContains(t, string(content), "zzz")
}

func TestActivator_Activate_FallsBackToEnv(t *testing.T) {
	tmpDir := t.TempDir()

	a := mirrors.NewActivator(mirrors.ActivatorParams{
		GradleHome: tmpDir,
		Envs: map[string]string{
			mirrorsconfig.EnabledEnvKey:    "true",
			mirrorsconfig.DatacenterEnvKey: "IAD1",
		},
		Logger: log.NewLogger(),
	})

	require.NoError(t, a.Activate(context.Background()))

	initFile := filepath.Join(tmpDir, "init.d", mirrorsconfig.InitFileName)
	content, err := os.ReadFile(initFile)
	require.NoError(t, err)

	assert.Contains(t, string(content), "https://repository-manager.services.bitrise.io:8090/maven/central")
	assert.Contains(t, string(content), "https://repository-manager.services.bitrise.io:8090/maven/google")
}

func TestActivator_Activate_DisabledNoOp(t *testing.T) {
	tmpDir := t.TempDir()

	a := mirrors.NewActivator(mirrors.ActivatorParams{
		GradleHome: tmpDir,
		Enabled:    boolPtr(false),
		Datacenter: "AMS1",
		Logger:     log.NewLogger(),
	})

	require.NoError(t, a.Activate(context.Background()))

	_, err := os.Stat(filepath.Join(tmpDir, "init.d", mirrorsconfig.InitFileName))
	assert.True(t, os.IsNotExist(err))
}

func TestActivator_Activate_EnabledButNoDatacenter(t *testing.T) {
	tmpDir := t.TempDir()

	a := mirrors.NewActivator(mirrors.ActivatorParams{
		GradleHome: tmpDir,
		Enabled:    boolPtr(true),
		Envs:       map[string]string{},
		Logger:     log.NewLogger(),
	})

	require.NoError(t, a.Activate(context.Background()))

	_, err := os.Stat(filepath.Join(tmpDir, "init.d", mirrorsconfig.InitFileName))
	assert.True(t, os.IsNotExist(err))
}

// Mirrors path only exports BITRISE_BUILD_CACHE_CLI_VERSION — the plugin version env vars are
// the full `activate gradle` command's job. Enforced here so the two activators don't drift.
func TestActivator_Activate_ExportsCLIVersionButNotPluginVersions(t *testing.T) {
	tmpDir := t.TempDir()

	// Clear any pre-existing env vars so the test is self-contained.
	for _, k := range []string{
		consts.EnvCLIVersion,
		consts.EnvGradleCommonPluginVersion,
		consts.EnvGradleAnalyticsPluginVersion,
		consts.EnvGradleRemoteCachePluginVersion,
		consts.EnvGradleTestDistributionPluginVersion,
	} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}

	a := mirrors.NewActivator(mirrors.ActivatorParams{
		GradleHome: tmpDir,
		Enabled:    boolPtr(true),
		Datacenter: "AMS1",
		Envs:       map[string]string{},
		Logger:     log.NewLogger(),
	})

	require.NoError(t, a.Activate(context.Background()))

	assert.NotEmpty(t, os.Getenv(consts.EnvCLIVersion),
		"mirrors activator must export BITRISE_BUILD_CACHE_CLI_VERSION — the mirrors init script reads it via System.getenv")
	t.Cleanup(func() { os.Unsetenv(consts.EnvCLIVersion) })

	for _, k := range []string{
		consts.EnvGradleCommonPluginVersion,
		consts.EnvGradleAnalyticsPluginVersion,
		consts.EnvGradleRemoteCachePluginVersion,
		consts.EnvGradleTestDistributionPluginVersion,
	} {
		assert.Emptyf(t, os.Getenv(k),
			"mirrors activator must NOT export %s — only the full gradle activator exports plugin version env vars", k)
	}
}
