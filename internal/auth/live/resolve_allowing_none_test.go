//go:build unit

package live_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func emptyResolver() *live.Resolver {
	return &live.Resolver{Backends: []store.Store{}}
}

// A machine with no credential is an expected state for some callers, not a
// failure.
func TestResolveAllowingNone_UnconfiguredIsNotAnErrorWhenAllowed(t *testing.T) {
	cred, origin, ok, err := emptyResolver().ResolveAllowingNone(t.Context(), map[string]string{}, true)

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, cred.Token)
	assert.Equal(t, auth.BackendNone, origin.Backend)
}

// A half-configured machine must
// be told which half is missing, not handed a generic "no token".
func TestResolveAllowingNone_PreservesTheSpecificError(t *testing.T) {
	envs := map[string]string{auth.EnvAuthToken: "a-token"}

	_, _, ok, err := emptyResolver().ResolveAllowingNone(t.Context(), envs, false)

	require.Error(t, err)
	assert.False(t, ok)
	assert.ErrorIs(t, err, auth.ErrWorkspaceIDNotProvided)
}

// A token that is present but unusable is a real fault and must not be swallowed.
func TestResolveAllowingNone_MalformedCredentialStillFails(t *testing.T) {
	envs := map[string]string{
		auth.EnvAuthToken:   "bad\x01token",
		auth.EnvWorkspaceID: "ws-1",
	}

	_, _, ok, err := emptyResolver().ResolveAllowingNone(t.Context(), envs, true)

	require.Error(t, err)
	assert.False(t, ok)
	assert.ErrorIs(t, err, auth.ErrTokenNonPrintable)
}
