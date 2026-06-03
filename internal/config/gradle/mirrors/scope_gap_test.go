//go:build unit

package mirrors_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func TestLogScopeGapWarnings(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		expectWarnings bool
		expectFiles    []string
	}{
		{
			name:           "empty project — no warnings",
			files:          map[string]string{},
			expectWarnings: false,
		},
		{
			name: "settings.gradle.kts with apply(from = ...) — warns",
			files: map[string]string{
				"settings.gradle.kts": `apply(from = "shared.gradle.kts")` + "\n",
			},
			expectWarnings: true,
			expectFiles:    []string{"settings.gradle.kts"},
		},
		{
			name: "settings.gradle.kts without apply from — no warning",
			files: map[string]string{
				"settings.gradle.kts": `pluginManagement {}` + "\n",
			},
			expectWarnings: false,
		},
		{
			name: "Groovy apply from: form — warns",
			files: map[string]string{
				"settings.gradle": `apply from: 'shared.gradle'` + "\n",
			},
			expectWarnings: true,
			expectFiles:    []string{"settings.gradle"},
		},
		{
			name: "app/build.gradle.kts with apply(from = ...) — warns",
			files: map[string]string{
				filepath.Join("app", "build.gradle.kts"): `apply(from = uri("https://example.com/x.gradle.kts"))` + "\n",
			},
			expectWarnings: true,
			expectFiles:    []string{filepath.Join("app", "build.gradle.kts")},
		},
		{
			name: "multiple files with apply from — all reported",
			files: map[string]string{
				"settings.gradle.kts":                    `apply(from = "x.gradle.kts")` + "\n",
				filepath.Join("app", "build.gradle.kts"): `apply(from = "y.gradle.kts")` + "\n",
			},
			expectWarnings: true,
			expectFiles: []string{
				"settings.gradle.kts",
				filepath.Join("app", "build.gradle.kts"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()

			for rel, content := range tt.files {
				abs := filepath.Join(tmp, rel)
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
			}

			logger := &mocks.Logger{}
			logger.On("Warnf", mock.Anything).Return()
			logger.On("Warnf", mock.Anything, mock.Anything).Return()
			logger.On("Warnf", mock.Anything, mock.Anything, mock.Anything).Return()

			mirrors.LogScopeGapWarnings(logger, utils.DefaultOsProxy{}, tmp)

			if !tt.expectWarnings {
				logger.AssertNotCalled(t, "Warnf", mock.Anything)
				logger.AssertNotCalled(t, "Warnf", mock.Anything, mock.Anything)

				return
			}

			calls := warnfCalls(logger)
			joined := joinCalls(calls)

			for _, f := range tt.expectFiles {
				assert.Contains(t, joined, f, "expected warning to mention %q", f)
			}

			assert.Contains(t, joined, "apply(from", "expected primary warning to mention apply(from")
			assert.Contains(t, joined, "pluginManagement", "expected recommendation to mention pluginManagement")
		})
	}
}

func warnfCalls(logger *mocks.Logger) []mock.Call {
	var out []mock.Call

	for _, c := range logger.Calls {
		if c.Method == "Warnf" {
			out = append(out, c)
		}
	}

	return out
}

func joinCalls(calls []mock.Call) string {
	var s string

	for _, c := range calls {
		for _, a := range c.Arguments {
			if str, ok := a.(string); ok {
				s += str + "\n"
			}
		}
	}

	return s
}
