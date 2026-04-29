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

	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/gradle/mirrors"
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

	assert.Contains(t, string(content), "https://repository-manager-ams.services.bitrise.io:8090/maven/central")
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

	assert.Contains(t, string(content), "https://repository-manager-iad.services.bitrise.io:8090/maven/central")
	assert.Contains(t, string(content), "https://repository-manager-iad.services.bitrise.io:8090/maven/google")
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
