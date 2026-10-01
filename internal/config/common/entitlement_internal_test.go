//go:build unit

package common

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func credFor(workspaceID string) auth.Credential {
	return auth.Credential{Token: "tok", WorkspaceID: workspaceID}
}

func testLogger() log.Logger { return log.NewLogger() }

// The bypass and the endpoint have to be removed and shipped together. Flipping
// entitlementEndpointShipped without deleting the bypass would leave a live
// env var that silently disables a paying-customer gate, so this fails the
// moment the flip happens and tells the next person what to delete.
func TestEntitlementBypass_MustBeRemovedOnceTheEndpointShips(t *testing.T) {
	if entitlementEndpointShipped {
		t.Fatalf(
			"the entitlement endpoint has shipped — now delete %s, the branch in "+
				"SkipActivationForEntitlement that reads it, and this test (ACI-5515)",
			EnvSkipEntitlementCheck,
		)
	}
}

// Until the endpoint exists the gate must be inert: a check that cannot be made
// is not a "no", or every activation everywhere would refuse to run.
func TestCheckEntitlement_IsUnknownWhileTheEndpointDoesNotExist(t *testing.T) {
	require.False(t, entitlementEndpointShipped, "this test describes the pre-ship state")

	got := CheckEntitlement(t.Context(), BuildToolGradle, "https://example.invalid", credFor("ws-1"), testLogger())

	assert.Equal(t, EntitlementUnknown, got)
}

// Three-valued on purpose: an unreachable website must not read as "no Build
// Cache" and disable caching for a workspace that has it.
func TestSkipActivationForEntitlement_DoesNotSkipOnAnUnknownAnswer(t *testing.T) {
	t.Setenv(EnvSkipEntitlementCheck, "")

	assert.False(t, SkipActivationForEntitlement(t.Context(), BuildToolGradle, "https://example.invalid", credFor("ws-1"), testLogger()))
}

// No workspace means nothing to ask about, which is not a "no".
func TestSkipActivationForEntitlement_DoesNotSkipWithoutAWorkspace(t *testing.T) {
	t.Setenv(EnvSkipEntitlementCheck, "")

	assert.False(t, SkipActivationForEntitlement(t.Context(), BuildToolGradle, "https://example.invalid", credFor(""), testLogger()))
}

func TestSkipActivationForEntitlement_TheBypassSuppressesTheGate(t *testing.T) {
	t.Setenv(EnvSkipEntitlementCheck, "true")

	assert.False(t, SkipActivationForEntitlement(t.Context(), BuildToolGradle, "https://example.invalid", credFor("ws-1"), testLogger()))
}
