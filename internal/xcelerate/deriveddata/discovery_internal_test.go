//go:build unit

package deriveddata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchesProject(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "App.xcworkspace")
	require.NoError(t, os.MkdirAll(real, 0o755))

	link := filepath.Join(tmp, "AppLink.xcworkspace")
	require.NoError(t, os.Symlink(real, link))

	dead := filepath.Join(tmp, "Dead.xcworkspace")
	require.NoError(t, os.Symlink(filepath.Join(tmp, "missing-target"), dead))

	tests := []struct {
		name      string
		candidate string
		hint      string
		want      bool
	}{
		{"both empty", "", "", true},
		{"exact match", "/a/b/App.xcworkspace", "/a/b/App.xcworkspace", true},
		{"trailing slash tolerance", "/a/b/App.xcworkspace/", "/a/b/App.xcworkspace", true},
		{"case insensitive", "/a/b/APP.xcworkspace", "/a/b/app.xcworkspace", true},
		{"different projects", "/a/b/App.xcworkspace", "/a/b/Other.xcworkspace", false},
		{"symlink resolves both sides to same path", link, real, true},
		{"dead symlink falls back to clean compare", dead, dead, true},
		{"dead symlink mismatch", dead, filepath.Join(tmp, "other-dead"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesProject(tt.candidate, tt.hint))
		})
	}
}
