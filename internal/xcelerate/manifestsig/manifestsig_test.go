//go:build unit

package manifestsig_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/manifestsig"
)

func TestFirstSubmatch_ParsesSignatureTokens(t *testing.T) {
	sig := "Analyze project IceCubesApp with scheme IceCubesApp and Configuration Release"
	assert.Equal(t, "IceCubesApp", manifestsig.FirstSubmatch(manifestsig.Project, sig))
	assert.Equal(t, "Release", manifestsig.FirstSubmatch(manifestsig.Configuration, sig))
	assert.Equal(t, "", manifestsig.FirstSubmatch(manifestsig.Workspace, sig))
}

func TestFirstSubmatch_WorkspaceForm(t *testing.T) {
	sig := "Build workspace Seek.xcworkspace with scheme Seek"
	assert.Equal(t, "Seek.xcworkspace", manifestsig.FirstSubmatch(manifestsig.Workspace, sig))
}

func TestFirstSubmatch_NoMatchReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", manifestsig.FirstSubmatch(manifestsig.Project, "Signing nothing"))
	assert.Equal(t, "", manifestsig.FirstSubmatch(manifestsig.Project, ""))
}

func TestFirstSubmatch_CaseInsensitive(t *testing.T) {
	assert.Equal(t, "Foo", manifestsig.FirstSubmatch(manifestsig.Project, "PROJECT Foo"))
	assert.Equal(t, "Bar", manifestsig.FirstSubmatch(manifestsig.Workspace, "WORKSPACE Bar"))
}
