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
	cases := []struct {
		name     string
		configOn bool
		envs     map[string]string
		want     bool
	}{
		{name: "config off, env unset", envs: map[string]string{}},
		{name: "config on, env unset", configOn: true, envs: map[string]string{}, want: true},
		{name: "config off, env 1", envs: map[string]string{xcelerate.EnvDetailsUpload: "1"}, want: true},
		{name: "config off, env true", envs: map[string]string{xcelerate.EnvDetailsUpload: "true"}, want: true},
		{name: "config on, env 0", configOn: true, envs: map[string]string{xcelerate.EnvDetailsUpload: "0"}},
		{name: "config on, env false", configOn: true, envs: map[string]string{xcelerate.EnvDetailsUpload: "false"}},
		{name: "config on, env empty", configOn: true, envs: map[string]string{xcelerate.EnvDetailsUpload: ""}, want: true},
		{name: "config on, env garbage", configOn: true, envs: map[string]string{xcelerate.EnvDetailsUpload: "yes please"}, want: true},
		{name: "config off, env garbage", envs: map[string]string{xcelerate.EnvDetailsUpload: "yes please"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := xcelerate.ResolveDetailsUpload(xcelerate.Config{DetailsUploadEnabled: tc.configOn}, tc.envs)

			assert.Equal(t, tc.want, got)
		})
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
