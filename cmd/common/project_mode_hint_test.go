//go:build unit

package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestPrintOptInGateHintIfGated(t *testing.T) {
	cases := []struct {
		name       string
		mode       machineconfig.Mode
		plantAt    string // "cwd", "ancestor", or "" for no marker
		flag       string
		wantPrints bool
	}{
		{name: "mode=always, no marker — no hint", mode: machineconfig.ModeAlways, wantPrints: false},
		{name: "mode=opt-in, marker at cwd — no hint", mode: machineconfig.ModeOptIn, plantAt: "cwd", wantPrints: false},
		{name: "mode=opt-in, ancestor marker — no hint", mode: machineconfig.ModeOptIn, plantAt: "ancestor", wantPrints: false},
		{name: "mode=opt-in, no marker — hint printed", mode: machineconfig.ModeOptIn, wantPrints: true},
		{name: "flag overrides stored to always — no hint", mode: machineconfig.ModeOptIn, flag: string(machineconfig.ModeAlways), wantPrints: false},
		{name: "flag overrides stored to opt-in — hint printed", mode: machineconfig.ModeAlways, flag: string(machineconfig.ModeOptIn), wantPrints: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			if tc.mode == machineconfig.ModeOptIn {
				writeMachineOptIn(t, home)
			}

			root := filepath.Join(home, "proj")
			cwd := root
			if tc.plantAt == "ancestor" {
				cwd = filepath.Join(root, "nested")
			}
			require.NoError(t, os.MkdirAll(cwd, 0o755))

			switch tc.plantAt {
			case "cwd":
				require.NoError(t, os.WriteFile(filepath.Join(cwd, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
			case "ancestor":
				require.NoError(t, os.WriteFile(filepath.Join(root, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
			}
			t.Chdir(cwd)

			logger := &mocks.Logger{}
			logger.On("Printf", mock.Anything, mock.Anything).Return()

			PrintOptInGateHintIfGated(logger, utils.DefaultOsProxy{}, tc.flag)

			if tc.wantPrints {
				logger.AssertCalled(t, "Printf", mock.Anything, mock.Anything)
			} else {
				logger.AssertNotCalled(t, "Printf", mock.Anything, mock.Anything)
			}
		})
	}
}

func TestPrintOptInGateHintIfGated_NilLoggerIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintOptInGateHintIfGated(nil, utils.DefaultOsProxy{}, "")
	})
}

func TestPrintOptInGateHintIfGated_NilOsProxyIsNoop(t *testing.T) {
	logger := &mocks.Logger{}
	PrintOptInGateHintIfGated(logger, nil, "")
	logger.AssertNotCalled(t, "Printf")
}

func writeMachineOptIn(t *testing.T, home string) {
	t.Helper()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BitriseCacheRoot(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{"project_mode":"opt-in"}`), 0o644))
}
