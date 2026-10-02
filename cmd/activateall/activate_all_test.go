//go:build unit

package activateall

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

const monitoringOrg = "dbd227a0aeb70859"

type harness struct {
	ran      [][]string
	entitled bool
	asked    bool
	failing  map[string]error
}

func (h *harness) activator(goos string, auto bool, envs map[string]string, cred auth.Credential, found bool, resolveErr error) activator {
	return activator{
		logger:    log.NewLogger(log.WithOutput(io.Discard)),
		envs:      envs,
		goos:      goos,
		autoGated: auto,
		resolve: func(context.Context) (auth.Credential, bool, error) {
			return cred, found, resolveErr
		},
		entitled: func(context.Context) bool {
			h.asked = true

			return h.entitled
		},
		runStep: func(_ context.Context, args []string) error {
			h.ran = append(h.ran, args)

			return h.failing[strings.Join(args, " ")]
		},
	}
}

func names(ran [][]string) []string {
	out := make([]string, 0, len(ran))
	for _, args := range ran {
		out = append(out, strings.Join(args, " "))
	}

	return out
}

func listed(orgs string) map[string]string {
	return map[string]string{configcommon.EnvAutoActivateOrgs: orgs}
}

func TestActivate_AutoRunsForAListedWorkspace(t *testing.T) {
	h := &harness{entitled: true}
	a := h.activator("darwin", true, listed(monitoringOrg), auth.Credential{WorkspaceID: monitoringOrg}, true, nil)

	require.NoError(t, a.activate(context.Background()))

	assert.Equal(t, []string{
		"activate gradle",
		"activate bazel",
		"activate xcode",
		"activate react-native --gradle=false --xcode=false",
	}, names(h.ran))
}

func TestActivate_XcodeIsMacOnly(t *testing.T) {
	h := &harness{entitled: true}
	a := h.activator("linux", true, listed("*"), auth.Credential{WorkspaceID: monitoringOrg}, true, nil)

	require.NoError(t, a.activate(context.Background()))

	assert.Equal(t, []string{
		"activate gradle",
		"activate bazel",
		"activate react-native --gradle=false --xcode=false",
	}, names(h.ran))
}

func TestActivate_AutoSkipsEverythingItShould(t *testing.T) {
	tests := []struct {
		name       string
		envs       map[string]string
		cred       auth.Credential
		found      bool
		resolveErr error
	}{
		{"workspace not on the list", listed("322a005426441b60"), auth.Credential{WorkspaceID: monitoringOrg}, true, nil},
		{"no list at all", map[string]string{}, auth.Credential{WorkspaceID: monitoringOrg}, true, nil},
		{"no credential", listed("*"), auth.Credential{}, false, nil},
		{"credential that cannot be resolved", listed("*"), auth.Credential{}, false, errors.New("malformed token")},
		{"credential without a workspace", listed("*"), auth.Credential{Token: "t"}, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{entitled: true}
			a := h.activator("darwin", true, tt.envs, tt.cred, tt.found, tt.resolveErr)

			require.NoError(t, a.activate(context.Background()))

			assert.Empty(t, h.ran)
			assert.False(t, h.asked, "the entitlement check must not run for a skipped workspace")
		})
	}
}

func TestActivate_ManualRunIgnoresTheOrgList(t *testing.T) {
	h := &harness{entitled: true}
	a := h.activator("darwin", false, map[string]string{}, auth.Credential{}, false, nil)

	require.NoError(t, a.activate(context.Background()))

	assert.Len(t, h.ran, 4)
}

func TestActivate_StopsWhenNotEntitled(t *testing.T) {
	h := &harness{entitled: false}
	a := h.activator("darwin", true, listed(monitoringOrg), auth.Credential{WorkspaceID: monitoringOrg}, true, nil)

	require.NoError(t, a.activate(context.Background()))

	assert.Empty(t, h.ran)
	assert.True(t, h.asked)
}

func TestActivate_OneFailingToolDoesNotStopTheOthers(t *testing.T) {
	h := &harness{entitled: true, failing: map[string]error{"activate bazel": errors.New("exit status 1")}}
	a := h.activator("darwin", true, listed("*"), auth.Credential{WorkspaceID: monitoringOrg}, true, nil)

	err := a.activate(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "activate bazel: exit status 1")
	assert.NotContains(t, err.Error(), "activate gradle")
	assert.Len(t, h.ran, 4)
}
