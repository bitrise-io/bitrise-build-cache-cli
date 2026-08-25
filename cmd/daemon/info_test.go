//go:build unit

package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

func TestProbeStatus_mapsProbeToHumanString(t *testing.T) {
	assert.Equal(t, statusRunning, probeStatus(daemonpkg.ProbeRunning))
	assert.Equal(t, statusStuck, probeStatus(daemonpkg.ProbeStuck))
	assert.Equal(t, statusStopped, probeStatus(daemonpkg.ProbeStopped))
}
