//go:build unit

package ccache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, idleTimeoutFor(tt.envs))
		})
	}
}
