//go:build unit

package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateUnit_xcelerateProxy(t *testing.T) {
	svc := Service{Name: "xcelerate-proxy", Args: []string{"xcelerate", "start-proxy"}}

	got, err := GenerateUnit(svc, "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	assert.Contains(t, got, "[Unit]")
	assert.Contains(t, got, "[Service]")
	assert.Contains(t, got, "[Install]")
	assert.Contains(t, got, "Description=Bitrise Build Cache — xcelerate-proxy")
	assert.Contains(t, got, "ExecStart=/usr/local/bin/bitrise-build-cache xcelerate start-proxy")
	assert.Contains(t, got, "Restart=on-failure")
	assert.Contains(t, got, "WantedBy=default.target")
}

func TestGenerateUnit_quotesPathsWithSpaces(t *testing.T) {
	svc := Service{Name: "ccache-helper", Args: []string{"ccache", "storage-helper", "start"}}

	got, err := GenerateUnit(svc, "/Users/alice with spaces/bin/bitrise-build-cache")
	require.NoError(t, err)

	// The executable path should be double-quoted in ExecStart because it
	// contains whitespace; bare argv tokens stay unquoted.
	require.True(t, strings.Contains(got, `ExecStart="/Users/alice with spaces/bin/bitrise-build-cache" ccache storage-helper start`),
		"actual ExecStart line not matched, got:\n%s", got)
}

func TestGenerateUnit_emptyExecutableErrors(t *testing.T) {
	_, err := GenerateUnit(Service{Name: "x"}, "")
	require.Error(t, err)
}
