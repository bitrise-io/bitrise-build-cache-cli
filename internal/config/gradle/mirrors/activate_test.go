//go:build unit

package mirrors_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func TestActivate(t *testing.T) {
	mirrorByFlag := func(flag string) mirrors.RepoMirror {
		for _, m := range mirrors.KnownMirrors {
			if m.FlagName == flag {
				return m
			}
		}
		t.Fatalf("mirror with flag %q not found", flag)

		return mirrors.RepoMirror{}
	}

	allMirrors := mirrors.KnownMirrors
	centralOnly := []mirrors.RepoMirror{mirrorByFlag("mavencentral")}
	googleOnly := []mirrors.RepoMirror{mirrorByFlag("google")}

	tests := []struct {
		name             string
		params           mirrors.Params
		expectCreated    bool
		expectContains   []string
		expectNotContain []string
	}{
		{
			name:          "disabled",
			params:        mirrors.Params{Enabled: false, Datacenter: "AMS1", Mirrors: allMirrors},
			expectCreated: false,
		},
		{
			name:          "enabled but no datacenter",
			params:        mirrors.Params{Enabled: true, Datacenter: "", Mirrors: allMirrors},
			expectCreated: false,
		},
		{
			name:          "enabled but no mirrors selected",
			params:        mirrors.Params{Enabled: true, Datacenter: "AMS1", Mirrors: nil},
			expectCreated: false,
		},
		{
			name:          "AMS1 all mirrors",
			params:        mirrors.Params{Enabled: true, Datacenter: "AMS1", Mirrors: allMirrors},
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-ams.services.bitrise.io:8090/maven/central",
				"https://repository-manager-ams.services.bitrise.io:8090/maven/google",
			},
		},
		{
			name:          "IAD1 mavencentral only",
			params:        mirrors.Params{Enabled: true, Datacenter: "IAD1", Mirrors: centralOnly},
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-iad.services.bitrise.io:8090/maven/central",
			},
			expectNotContain: []string{
				"https://repository-manager-iad.services.bitrise.io:8090/maven/google",
			},
		},
		{
			name:          "ORD1 google only",
			params:        mirrors.Params{Enabled: true, Datacenter: "ORD1", Mirrors: googleOnly},
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-ord.services.bitrise.io:8090/maven/google",
			},
			expectNotContain: []string{
				"https://repository-manager-ord.services.bitrise.io:8090/maven/central",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.params.GradleHome = tmpDir

			err := mirrors.Activate(log.NewLogger(), utils.DefaultOsProxy{}, tt.params)
			require.NoError(t, err)

			initFile := filepath.Join(tmpDir, "init.d", mirrors.InitFileName)

			if !tt.expectCreated {
				_, statErr := os.Stat(initFile)
				assert.True(t, os.IsNotExist(statErr), "init script should not be created")

				return
			}

			content, readErr := os.ReadFile(initFile)
			require.NoError(t, readErr, "init script should be created")

			for _, s := range tt.expectContains {
				assert.Contains(t, string(content), s)
			}

			for _, s := range tt.expectNotContain {
				assert.NotContains(t, string(content), s)
			}

			assert.NotContains(t, string(content), "{{ .MirrorURL }}")
			assert.NotContains(t, string(content), "{{ .GradleMatch }}")
		})
	}
}

func TestFilterByFlagNames(t *testing.T) {
	t.Run("empty returns all", func(t *testing.T) {
		got := mirrors.FilterByFlagNames(nil)
		assert.Equal(t, mirrors.KnownMirrors, got)
	})

	t.Run("preserves KnownMirrors order", func(t *testing.T) {
		got := mirrors.FilterByFlagNames([]string{"google", "mavencentral"})
		require.Len(t, got, 2)
		assert.Equal(t, "mavencentral", got[0].FlagName)
		assert.Equal(t, "google", got[1].FlagName)
	})

	t.Run("ignores unknown names", func(t *testing.T) {
		got := mirrors.FilterByFlagNames([]string{"mavencentral", "unknown"})
		require.Len(t, got, 1)
		assert.Equal(t, "mavencentral", got[0].FlagName)
	})
}

func TestDatacenterToRegion(t *testing.T) {
	cases := map[string]string{
		"AMS1": "ams",
		"IAD1": "iad",
		"ORD1": "ord",
		"":     "",
	}

	for in, want := range cases {
		assert.Equal(t, want, mirrors.DatacenterToRegion(in), "input=%q", in)
	}
}
