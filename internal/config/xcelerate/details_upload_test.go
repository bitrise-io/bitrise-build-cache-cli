//go:build unit

package xcelerate_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
)

func TestResolveDetailsUpload(t *testing.T) {
	envCases := []struct {
		name  string
		envs  map[string]string
		truth bool
	}{
		{name: "env unset", envs: map[string]string{}},
		{name: "env 0", envs: map[string]string{xcelerate.EnvDetailsUpload: "0"}},
		{name: "env 1", envs: map[string]string{xcelerate.EnvDetailsUpload: "1"}, truth: true},
		{name: "env true", envs: map[string]string{xcelerate.EnvDetailsUpload: "true"}, truth: true},
	}

	for _, configOn := range []bool{false, true} {
		for _, tc := range envCases {
			t.Run(tc.name, func(t *testing.T) {
				got := xcelerate.ResolveDetailsUpload(xcelerate.Config{DetailsUploadEnabled: configOn}, tc.envs)

				assert.Equal(t, configOn || tc.truth, got, "config=%v", configOn)
			})
		}
	}
}

func TestConfig_DetailsUploadJSON(t *testing.T) {
	data, err := json.Marshal(xcelerate.Config{DetailsUploadEnabled: true})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"detailsUploadEnabled":true`)

	off, err := json.Marshal(xcelerate.Config{})
	require.NoError(t, err)
	assert.NotContains(t, string(off), "detailsUploadEnabled")
}

func TestDefaultParams_DetailsUploadOptIn(t *testing.T) {
	assert.False(t, xcelerate.DefaultParams().DetailsUploadEnabled)
}
