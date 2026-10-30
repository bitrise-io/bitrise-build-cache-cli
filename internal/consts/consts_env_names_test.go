//go:build unit

package consts_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
)

// Rename-safety guard: these env-var constants are the contract between the CLI's exporters and
// the Gradle init-script bodies. The templates read these literals via System.getenv at Gradle
// build time, so a rename on the Go side without a template update would silently break the
// activation flow. This test grep-asserts each constant value is actually present in at least
// one init-script template.
func TestEnvVarConstants_ReferencedByInitScriptTemplates(t *testing.T) {
	t.Parallel()

	// Resolve the repo root from this file's path. runtime.Caller(0) gives
	// internal/consts/consts_env_names_test.go, two levels down from the module root.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	templates := []string{
		filepath.Join(repoRoot, "internal", "config", "gradle", "initd.gradle.kts.gotemplate"),
		filepath.Join(repoRoot, "internal", "config", "gradle", "mirrors", "asset", "gradle-mirrors.init.gradle.kts.gotemplate"),
	}

	templateBodies := make([]string, 0, len(templates))
	for _, path := range templates {
		b, err := os.ReadFile(path)
		require.NoErrorf(t, err, "read template %s", path)
		templateBodies = append(templateBodies, string(b))
	}

	cases := map[string]string{
		"EnvCLIVersion":                          consts.EnvCLIVersion,
		"EnvGradleCommonPluginVersion":           consts.EnvGradleCommonPluginVersion,
		"EnvGradleAnalyticsPluginVersion":        consts.EnvGradleAnalyticsPluginVersion,
		"EnvGradleRemoteCachePluginVersion":      consts.EnvGradleRemoteCachePluginVersion,
		"EnvGradleTestDistributionPluginVersion": consts.EnvGradleTestDistributionPluginVersion,
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var found bool
			for _, body := range templateBodies {
				if strings.Contains(body, value) {
					found = true

					break
				}
			}

			require.Truef(t, found,
				"env var literal %q (%s) not found in any init-script template — rename on Go side without template update?",
				value, name)
		})
	}
}
