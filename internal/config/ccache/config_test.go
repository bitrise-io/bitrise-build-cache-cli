//go:build unit

package ccache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func Test_idleTimeoutFor(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
		want time.Duration
	}{
		{
			name: "local build",
			envs: map[string]string{},
			want: time.Hour,
		},
		{
			name: "bitrise build",
			envs: map[string]string{"BITRISE_IO": "true", "BITRISE_BUILD_SLUG": "some-slug"},
			want: 6 * time.Hour,
		},
		{
			name: "github actions build",
			envs: map[string]string{"GITHUB_ACTIONS": "true"},
			want: 6 * time.Hour,
		},
	}

	osProxy := &mocks.OsProxyMock{HostnameFunc: func() (string, error) { return "laptop.local", nil }}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, idleTimeoutFor(tt.envs, osProxy))
		})
	}
}
