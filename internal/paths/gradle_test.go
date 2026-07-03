//go:build unit

package paths

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaths_GradleHome(t *testing.T) {
	p := FromHome("/Users/alice")

	assert.Equal(t, filepath.Join("/Users/alice", ".gradle"), p.GradleHome(""))
	assert.Equal(t, filepath.Join("/Users/alice", ".gradle"), p.GradleHome("  "))
	assert.Equal(t, "/g", p.GradleHome("/g"))
}

func TestGradleInitScriptPaths(t *testing.T) {
	assert.Equal(t, filepath.Join("/g", "init.d"), GradleInitDir("/g"))
	assert.Equal(t, filepath.Join("/g", "init.d", GradleInitScriptName), GradleInitScript("/g"))
}
